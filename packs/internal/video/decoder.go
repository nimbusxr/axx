package video

import (
	"errors"
	"fmt"
	"image"
	"runtime"
	"unsafe"
)

// OpenH264's decoder's C structures, as its codec_app_def.h lays them out on
// 64-bit machines (v2.6.0).

// decodingParam is SDecodingParam.
type decodingParam struct {
	fileNameRestructed uintptr
	cpuLoad            uint32
	targetDqLayer      uint8
	_                  [3]byte
	ecActiveIdc        int32
	parseOnly          bool
	_                  [3]byte
	videoPropertySize  uint32
	videoBsType        int32
}

// bufferInfo is SBufferInfo, its UsrData the SSysMEMBuffer of a decoded frame.
type bufferInfo struct {
	bufferStatus          int32
	_                     int32
	inBsTimeStamp         uint64
	outYuvTimeStamp       uint64
	width, height, format int32
	stride                [2]int32
	_                     int32
	dst                   [3]uintptr
}

// decoderVtable is ISVCDecoderVtbl.
type decoderVtable struct {
	initialize, uninitialize                      uintptr
	decodeFrame, decodeFrameNoDelay, decodeFrame2 uintptr
	flushFrame, decodeParser, decodeFrameEx       uintptr
	setOption, getOption                          uintptr
}

const (
	decoderOptionTraceLevel = 9 // DECODER_OPTION_TRACE_LEVEL
	bitstreamAVC            = 0 // VIDEO_BITSTREAM_AVC
)

// Decoder decodes H.264 into frames, in the order they show: Cisco's
// OpenH264, which axx downloads as it does to encode.
type Decoder struct {
	lib  *library
	dec  uintptr
	vt   *decoderVtable
	info *bufferInfo
	dst  *[3]uintptr
	pin  runtime.Pinner
}

// NewDecoder is a decoder of an H.264 stream.
func NewDecoder() (*Decoder, error) {
	lib, err := load()
	if err != nil {
		return nil, err
	}
	d := &Decoder{lib: lib}
	decp := new(uintptr)
	d.pin.Pin(decp)
	if r := call(lib.createDecoder, uintptr(unsafe.Pointer(decp))); r != 0 || *decp == 0 {
		d.pin.Unpin()
		return nil, fmt.Errorf("OpenH264 made no decoder (%d)", r)
	}
	d.dec = *decp
	d.vt = (*decoderVtable)(cPointer(*(*uintptr)(cPointer(d.dec))))
	quiet := new(int32)
	d.pin.Pin(quiet)
	call(d.vt.setOption, d.dec, decoderOptionTraceLevel, uintptr(unsafe.Pointer(quiet)))
	p := &decodingParam{videoBsType: bitstreamAVC}
	d.pin.Pin(p)
	if r := call(d.vt.initialize, d.dec, uintptr(unsafe.Pointer(p))); r != 0 {
		d.Close()
		return nil, fmt.Errorf("OpenH264 did not start a decoder (%d)", r)
	}
	d.info, d.dst = new(bufferInfo), new([3]uintptr)
	d.pin.Pin(d.info)
	d.pin.Pin(d.dst)
	return d, nil
}

// Decode decodes an access unit, its NAL units each after a start code (an
// Annex B stream), and is the frame it finished, if one: a copy, in 4:2:0.
// Frames that B-frames reorder come later, and Flush gives the last.
func (d *Decoder) Decode(au []byte) (*image.YCbCr, error) {
	if d.dec == 0 {
		return nil, errors.New("the decoder is closed")
	}
	if len(au) == 0 {
		return nil, nil
	}
	var pin runtime.Pinner
	pin.Pin(&au[0])
	defer pin.Unpin()
	*d.info, *d.dst = bufferInfo{}, [3]uintptr{}
	if r := call(d.vt.decodeFrameNoDelay, d.dec, uintptr(unsafe.Pointer(&au[0])), uintptr(len(au)), uintptr(unsafe.Pointer(d.dst)), uintptr(unsafe.Pointer(d.info))); r != 0 {
		return nil, fmt.Errorf("OpenH264 could not decode a frame (state %#x)", r)
	}
	return d.frame(), nil
}

// Flush is the next frame the decoder still holds; nil when it holds none.
func (d *Decoder) Flush() (*image.YCbCr, error) {
	if d.dec == 0 {
		return nil, errors.New("the decoder is closed")
	}
	*d.info, *d.dst = bufferInfo{}, [3]uintptr{}
	if r := call(d.vt.flushFrame, d.dec, uintptr(unsafe.Pointer(d.dst)), uintptr(unsafe.Pointer(d.info))); r != 0 {
		return nil, nil
	}
	return d.frame(), nil
}

// frame copies the frame the decoder output, if it did.
func (d *Decoder) frame() *image.YCbCr {
	if d.info.bufferStatus != 1 {
		return nil
	}
	w, h := int(d.info.width), int(d.info.height)
	ys, cs := int(d.info.stride[0]), int(d.info.stride[1])
	img := image.NewYCbCr(image.Rect(0, 0, w, h), image.YCbCrSubsampleRatio420)
	planes := [3]struct {
		dst          []byte
		stride, w, h int
	}{{img.Y, ys, w, h}, {img.Cb, cs, (w + 1) / 2, (h + 1) / 2}, {img.Cr, cs, (w + 1) / 2, (h + 1) / 2}}
	for i, p := range planes {
		src := unsafe.Slice((*byte)(cPointer(d.dst[i])), p.stride*p.h)
		out := img.YStride
		if i > 0 {
			out = img.CStride
		}
		for y := range p.h {
			copy(p.dst[y*out:y*out+p.w], src[y*p.stride:y*p.stride+p.w])
		}
	}
	return img
}

// Close frees the decoder.
func (d *Decoder) Close() {
	if d.dec == 0 {
		return
	}
	call(d.vt.uninitialize, d.dec)
	call(d.lib.destroyDecoder, d.dec)
	d.dec = 0
	d.pin.Unpin()
}
