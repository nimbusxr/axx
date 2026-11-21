package video

import (
	"errors"
	"image"
	"io"
	"slices"
	"time"
)

// Remake writes an MP4 file's H.264 video to out as another, of frames width by height
// (made even): each frame drawn by draw from the video's own, as it shows, at its time.
func Remake(data []byte, out io.Writer, width, height int, q Quality, draw func(dst, src *image.YCbCr)) error {
	t, err := ReadMP4(data)
	if err != nil {
		return err
	}
	return remake(t, out, width, height, q, draw, nil)
}

// KeyFrames writes an MP4 file's H.264 video to out as another whose every frame is a key
// frame: of the same size, shown the same way up and at the same times. A player shows any
// frame of it without decoding those before it, which a video as cameras and encoders write
// them does not allow: there, most frames are what changed since the one before.
func KeyFrames(data []byte, out io.Writer, q Quality) error {
	t, err := ReadMP4(data)
	if err != nil {
		return err
	}
	q.KeyFrames = 1
	return remake(t, out, t.Width, t.Height, q, func(dst, src *image.YCbCr) {
		for _, p := range [3]struct {
			dst, src []byte
			ds, ss   int
			half     int
		}{{dst.Y, src.Y, dst.YStride, src.YStride, 1}, {dst.Cb, src.Cb, dst.CStride, src.CStride, 2}, {dst.Cr, src.Cr, dst.CStride, src.CStride, 2}} {
			w := min(dst.Rect.Dx(), src.Rect.Dx()) / p.half
			h := min(dst.Rect.Dy(), src.Rect.Dy()) / p.half
			for y := range h {
				copy(p.dst[y*p.ds:y*p.ds+w], p.src[y*p.ss:y*p.ss+w])
			}
		}
	}, t.Matrix)
}

// remake is Remake of a track already read, its frames turned for a player as matrix says.
func remake(t *Track, out io.Writer, width, height int, q Quality, draw func(dst, src *image.YCbCr), matrix []byte) error {
	if len(t.Samples) == 0 {
		return errors.New("its video has no frames")
	}
	dec, err := NewDecoder()
	if err != nil {
		return err
	}
	defer dec.Close()
	fps := 30.0
	if t.Duration > 0 {
		fps = float64(len(t.Samples)) / t.Duration.Seconds()
	}
	enc, err := NewEncoderOf(width, height, fps, q)
	if err != nil {
		return err
	}
	defer enc.Close()
	w, h := enc.Size()
	// The frames come out of the decoder in the order they show, so the k-th is shown at the
	// k-th of the times, in order.
	times := make([]time.Duration, len(t.Samples))
	for i, s := range t.Samples {
		times[i] = s.At
	}
	slices.Sort(times)
	shown := 0
	emit := func(src *image.YCbCr) error {
		if src == nil || shown >= len(times) {
			return nil
		}
		dst := image.NewYCbCr(image.Rect(0, 0, w, h), image.YCbCrSubsampleRatio420)
		draw(dst, src)
		err := enc.Encode(dst, times[shown])
		shown++
		return err
	}
	var sets []byte
	for _, nal := range append(slices.Clone(t.SPS), t.PPS...) {
		sets = append(append(sets, 0, 0, 0, 1), nal...)
	}
	if _, err := dec.Decode(sets); err != nil {
		return err
	}
	for _, s := range t.Samples {
		au, err := annexB(s.Data, t.LengthSize)
		if err != nil {
			return err
		}
		frame, err := dec.Decode(au)
		if err != nil {
			return err
		}
		if err := emit(frame); err != nil {
			return err
		}
	}
	for {
		frame, err := dec.Flush()
		if err != nil {
			return err
		}
		if frame == nil {
			break
		}
		if err := emit(frame); err != nil {
			return err
		}
	}
	if shown == 0 {
		return errors.New("its video has no frame OpenH264 could decode")
	}
	clip := enc.Finish(t.Duration)
	clip.Matrix = matrix
	return WriteMP4(out, []Clip{clip})
}

// Frames are the frames of an MP4 file's H.264 video, in the order they show: what a test
// reads a video back with.
func Frames(data []byte) ([]*image.YCbCr, error) {
	t, err := ReadMP4(data)
	if err != nil {
		return nil, err
	}
	dec, err := NewDecoder()
	if err != nil {
		return nil, err
	}
	defer dec.Close()
	var sets []byte
	for _, nal := range append(slices.Clone(t.SPS), t.PPS...) {
		sets = append(append(sets, 0, 0, 0, 1), nal...)
	}
	if _, err := dec.Decode(sets); err != nil {
		return nil, err
	}
	var out []*image.YCbCr
	for _, s := range t.Samples {
		au, err := annexB(s.Data, t.LengthSize)
		if err != nil {
			return nil, err
		}
		frame, err := dec.Decode(au)
		if err != nil {
			return nil, err
		}
		if frame != nil {
			out = append(out, frame)
		}
	}
	for {
		frame, err := dec.Flush()
		if err != nil {
			return nil, err
		}
		if frame == nil {
			return out, nil
		}
		out = append(out, frame)
	}
}

// annexB is a sample's NAL units, each after its length, as a stream of them each after a
// start code.
func annexB(sample []byte, lengthSize int) ([]byte, error) {
	var out []byte
	for len(sample) > 0 {
		if len(sample) < lengthSize {
			return nil, errors.New("a frame of the video is cut short")
		}
		var n int
		for _, b := range sample[:lengthSize] {
			n = n<<8 | int(b)
		}
		sample = sample[lengthSize:]
		if n > len(sample) {
			return nil, errors.New("a frame of the video is cut short")
		}
		out = append(append(out, 0, 0, 0, 1), sample[:n]...)
		sample = sample[n:]
	}
	return out, nil
}
