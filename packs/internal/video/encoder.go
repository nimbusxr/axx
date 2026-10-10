package video

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"runtime"
	"time"
	"unsafe"
)

// OpenH264's C structures, as its codec_app_def.h lays them out on 64-bit
// machines (v2.6.0).

// sliceArgument is SSliceArgument.
type sliceArgument struct {
	mode           int32
	num            uint32
	mbNum          [35]uint32 // MAX_SLICES_NUM_TMP
	sizeConstraint uint32
}

// spatialLayerConfig is SSpatialLayerConfig.
type spatialLayerConfig struct {
	width, height                                        int32
	frameRate                                            float32
	bitrate, maxBitrate                                  int32
	profile, level                                       int32
	dLayerQP                                             int32
	slice                                                sliceArgument
	videoSignalTypePresent                               bool
	videoFormat                                          uint8
	fullRange, colorDescriptionPresent                   bool
	colorPrimaries, transferCharacteristics, colorMatrix uint8
	aspectRatioPresent                                   bool
	aspectRatio                                          int32
	aspectRatioExtWidth, aspectRatioExtHeight            uint16
}

// encParamExt is SEncParamExt.
type encParamExt struct {
	usageType                                                           int32
	width, height                                                       int32
	targetBitrate                                                       int32
	rcMode                                                              int32
	maxFrameRate                                                        float32
	temporalLayerNum, spatialLayerNum                                   int32
	spatialLayers                                                       [4]spatialLayerConfig
	complexityMode                                                      int32
	intraPeriod                                                         uint32
	numRefFrame                                                         int32
	spsPpsIDStrategy                                                    int32
	prefixNalAddingCtrl, enableSSEI, simulcastAVC                       bool
	paddingFlag                                                         int32
	entropyCodingModeFlag                                               int32
	enableFrameSkip                                                     bool
	maxBitrate                                                          int32
	maxQP, minQP                                                        int32
	maxNalSize                                                          uint32
	enableLongTermReference                                             bool
	ltrRefNum                                                           int32
	ltrMarkPeriod                                                       uint32
	multipleThreadIdc                                                   uint16
	useLoadBalancing                                                    bool
	loopFilterDisableIdc, loopFilterAlphaC0Offset, loopFilterBetaOffset int32
	enableDenoise, enableBackgroundDetection, enableAdaptiveQuant       bool
	enableFrameCroppingFlag, enableSceneChangeDetect, isLosslessLink    bool
	fixRCOverShoot                                                      bool
	idrBitrateRatio                                                     int32
	psnrY, psnrU, psnrV                                                 bool
}

// sourcePicture is SSourcePicture.
type sourcePicture struct {
	colorFormat         int32
	stride              [4]int32
	data                [4]uintptr
	width, height       int32
	timeStamp           int64
	psnrY, psnrU, psnrV bool
}

// layerBSInfo is SLayerBSInfo: a layer's NAL units, each with its start code.
type layerBSInfo struct {
	temporalID, spatialID, qualityID uint8
	frameType                        int32
	layerType                        uint8
	subSeqID                         int32
	nalCount                         int32
	nalLengths                       uintptr // int*
	bsBuf                            uintptr // unsigned char*
	psnr                             [3]float32
}

// frameBSInfo is SFrameBSInfo.
type frameBSInfo struct {
	layerNum  int32
	layers    [128]layerBSInfo
	frameType int32
	frameSize int32
	timeStamp int64
}

// vtable is ISVCEncoderVtbl.
type vtable struct {
	initialize, initializeExt, getDefaultParams, uninitialize uintptr
	encodeFrame, encodeParameterSets, forceIntraFrame         uintptr
	setOption, getOption                                      uintptr
}

const (
	screenContentRealTime = 1  // EUsageType SCREEN_CONTENT_REAL_TIME
	rcQualityMode         = 0  // RC_QUALITY_MODE
	formatI420            = 23 // videoFormatI420
	frameIDR              = 1  // videoFrameTypeIDR
	constantID            = 0  // CONSTANT_ID
	frameSkip             = 4  // videoFrameTypeSkip

	optionDataFormat = 0  // ENCODER_OPTION_DATAFORMAT
	optionTraceLevel = 25 // ENCODER_OPTION_TRACE_LEVEL
)

// Quality is how finely a video keeps what changed: the quantizers its frames
// take, from the finest to the coarsest; higher is smaller and blurrier.
type Quality struct{ MinQP, MaxQP int32 }

var (
	// ScreenQuality is for a desktop's screen, captured as it is: what
	// changed stays sharp (text, a pointer, a stroke), and a screen's still
	// parts cost next to nothing anyway.
	ScreenQuality = Quality{MinQP: 10, MaxQP: 28}
	// StreamQuality is for a phone's screen as a stream sends it, each frame
	// a JPEG: kept finer, every frame would keep the JPEG's noise, at four
	// times the size, for no text more readable.
	StreamQuality = Quality{MinQP: 22, MaxQP: 32}
)

// Sample is one encoded frame: its NAL units, each after its length in 4
// bytes, as an MP4 file keeps them.
type Sample struct {
	Data []byte
	At   time.Duration
	Key  bool
}

// Clip is what one encoder encoded: frames of one size, with the parameter
// sets that decode them.
type Clip struct {
	Width, Height int
	SPS, PPS      []byte
	Samples       []Sample
	// End is when the clip's last frame stops showing.
	End time.Duration
}

// Encoder encodes frames of one size as H.264.
type Encoder struct {
	lib           *library
	enc           uintptr
	vt            *vtable
	width, height int
	yuv           []byte
	pic           *sourcePicture
	info          *frameBSInfo
	pin           runtime.Pinner
	clip          Clip
}

// NewEncoder is an encoder of frames width by height pixels (made even), at
// up to fps frames a second, of a screen's quality.
func NewEncoder(width, height int, fps float64) (*Encoder, error) {
	return NewEncoderOf(width, height, fps, ScreenQuality)
}

// NewEncoderOf is an encoder of frames of that quality.
func NewEncoderOf(width, height int, fps float64, q Quality) (*Encoder, error) {
	lib, err := load()
	if err != nil {
		return nil, err
	}
	width, height = width&^1, height&^1
	if width < 16 || height < 16 {
		return nil, fmt.Errorf("a video's frames are at least 16 by 16 pixels, not %d by %d", width, height)
	}
	e := &Encoder{lib: lib, width: width, height: height}
	e.clip.Width, e.clip.Height = width, height
	encp := new(uintptr)
	e.pin.Pin(encp)
	if r := call(lib.create, uintptr(unsafe.Pointer(encp))); r != 0 || *encp == 0 {
		e.pin.Unpin()
		return nil, fmt.Errorf("OpenH264 made no encoder (%d)", r)
	}
	e.enc = *encp
	e.vt = (*vtable)(cPointer(*(*uintptr)(cPointer(e.enc))))
	quiet := new(int32)
	e.option(optionTraceLevel, unsafe.Pointer(quiet))
	p := new(encParamExt)
	e.pin.Pin(p)
	if r := call(e.vt.getDefaultParams, e.enc, uintptr(unsafe.Pointer(p))); r != 0 {
		e.Close()
		return nil, fmt.Errorf("OpenH264 gave no settings (%d)", r)
	}
	bitrate := int32(width * height * 8) //nolint:gosec // a screen's size
	p.usageType = screenContentRealTime
	p.width, p.height = int32(width), int32(height) //nolint:gosec // a screen's size
	p.targetBitrate, p.maxBitrate = bitrate, bitrate
	p.rcMode = rcQualityMode
	p.maxFrameRate = float32(fps)
	p.temporalLayerNum, p.spatialLayerNum = 1, 1
	p.intraPeriod = uint32(max(1, fps*10)) // a key frame every 10 seconds or so, to seek to
	p.multipleThreadIdc = 1
	// One parameter set for the whole video: an MP4 file keeps one, and each
	// key frame refers to it.
	p.spsPpsIDStrategy = constantID
	// What changed stays sharp: text, a pointer, a stroke. A screen's still
	// parts cost next to nothing anyway.
	p.enableFrameSkip = false
	p.enableBackgroundDetection, p.enableAdaptiveQuant, p.enableDenoise = false, false, false
	p.maxQP, p.minQP = q.MaxQP, q.MinQP
	l := &p.spatialLayers[0]
	l.width, l.height, l.frameRate = p.width, p.height, p.maxFrameRate
	l.bitrate, l.maxBitrate = bitrate, bitrate
	// BT.709, as toI420 converts.
	l.videoSignalTypePresent, l.videoFormat, l.fullRange = true, 5, false
	l.colorDescriptionPresent, l.colorPrimaries, l.transferCharacteristics, l.colorMatrix = true, 1, 1, 1
	if r := call(e.vt.initializeExt, e.enc, uintptr(unsafe.Pointer(p))); r != 0 {
		e.Close()
		return nil, fmt.Errorf("OpenH264 did not take the video's settings (%d)", r)
	}
	format := new(int32)
	*format = formatI420
	e.option(optionDataFormat, unsafe.Pointer(format))

	e.yuv = make([]byte, width*height*3/2)
	e.pin.Pin(&e.yuv[0])
	e.pic = &sourcePicture{colorFormat: formatI420, width: int32(width), height: int32(height)} //nolint:gosec // a screen's size
	e.pic.stride = [4]int32{int32(width), int32(width / 2), int32(width / 2)}                   //nolint:gosec // a screen's size
	base := uintptr(unsafe.Pointer(&e.yuv[0]))
	e.pic.data = [4]uintptr{base, base + uintptr(width*height), base + uintptr(width*height+width*height/4)}
	e.pin.Pin(e.pic)
	e.info = new(frameBSInfo)
	e.pin.Pin(e.info)
	return e, nil
}

func (e *Encoder) option(id int32, v unsafe.Pointer) {
	var pin runtime.Pinner
	pin.Pin(v)
	defer pin.Unpin()
	call(e.vt.setOption, e.enc, uintptr(id), uintptr(v)) //nolint:gosec // an enum
}

// Size is the frames' size, as encoded.
func (e *Encoder) Size() (int, int) { return e.width, e.height }

// Encode encodes a frame, shown from at on; a frame of another size is fitted
// in. A frame the encoder skips adds nothing.
func (e *Encoder) Encode(img image.Image, at time.Duration) error {
	if e.enc == 0 {
		return errors.New("the encoder is closed")
	}
	toI420(img, e.yuv, e.width, e.height)
	e.pic.timeStamp = at.Milliseconds()
	*e.info = frameBSInfo{}
	if r := call(e.vt.encodeFrame, e.enc, uintptr(unsafe.Pointer(e.pic)), uintptr(unsafe.Pointer(e.info))); r != 0 {
		return fmt.Errorf("OpenH264 could not encode a frame (%d)", r)
	}
	if e.info.frameType == frameSkip {
		return nil
	}
	var data bytes.Buffer
	for i := range int(e.info.layerNum) {
		l := &e.info.layers[i]
		lengths := unsafe.Slice((*int32)(cPointer(l.nalLengths)), l.nalCount)
		total := 0
		for _, n := range lengths {
			total += int(n)
		}
		buf := unsafe.Slice((*byte)(cPointer(l.bsBuf)), total)
		for _, n := range lengths {
			nal := stripStartCode(buf[:n])
			buf = buf[n:]
			if len(nal) == 0 {
				continue
			}
			switch nal[0] & 0x1f {
			case 7: // a sequence parameter set, the same at each key frame
				if e.clip.SPS == nil {
					e.clip.SPS = bytes.Clone(nal)
				}
			case 8: // a picture parameter set, likewise
				if e.clip.PPS == nil {
					e.clip.PPS = bytes.Clone(nal)
				}
			default:
				var n [4]byte
				binary.BigEndian.PutUint32(n[:], uint32(len(nal))) //nolint:gosec // a NAL unit's size
				data.Write(n[:])
				data.Write(nal)
			}
		}
	}
	if data.Len() == 0 {
		return nil
	}
	e.clip.Samples = append(e.clip.Samples, Sample{Data: data.Bytes(), At: at, Key: e.info.frameType == frameIDR})
	return nil
}

// cPointer is the pointer to memory the encoder owns, outside Go's heap,
// that it handed back as an address.
func cPointer(addr uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&addr))
}

// stripStartCode is a NAL unit without its Annex B start code.
func stripStartCode(b []byte) []byte {
	switch {
	case len(b) >= 4 && b[0] == 0 && b[1] == 0 && b[2] == 0 && b[3] == 1:
		return b[4:]
	case len(b) >= 3 && b[0] == 0 && b[1] == 0 && b[2] == 1:
		return b[3:]
	}
	return b
}

// Finish closes the encoder, and is what it encoded, its last frame showing
// until end.
func (e *Encoder) Finish(end time.Duration) Clip {
	e.Close()
	c := e.clip
	c.End = end
	return c
}

// Close frees the encoder.
func (e *Encoder) Close() {
	if e.enc == 0 {
		return
	}
	call(e.vt.uninitialize, e.enc)
	call(e.lib.destroy, e.enc)
	e.enc = 0
	e.pin.Unpin()
}
