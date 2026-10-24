// Package tesseract writes stubs as Tesseract's traineddata: an LSTM
// recognizer, its unicharset and recoder, and the language config Tesseract
// reads when it loads them. No part of the stub calls Tesseract at build time.
package tesseract

import (
	"bytes"
	"encoding/binary"
)

// The traineddata components an LSTM recognizer has (TessdataType). A stub
// has no dictionaries: its recoder is a pass-through, made when the model is
// loaded (LSTMRecognizer::LoadRecoder).
const (
	componentConfig     = 0
	componentLSTM       = 17
	componentUnicharset = 21
	componentRecoder    = 22
	componentVersion    = 23
	componentCount      = 24
)

// traineddata combines components in Tesseract's traineddata container: a
// uint32 count, an int64 offset for every component (-1 when absent), then
// their bytes, all little-endian (TessdataManager::Serialize).
func traineddata(components map[int][]byte) []byte {
	var b bytes.Buffer
	_ = binary.Write(&b, binary.LittleEndian, uint32(componentCount))
	offset := int64(4 + 8*componentCount)
	for i := range componentCount {
		data := components[i]
		if len(data) == 0 {
			_ = binary.Write(&b, binary.LittleEndian, int64(-1))
			continue
		}
		_ = binary.Write(&b, binary.LittleEndian, offset)
		offset += int64(len(data))
	}
	for i := range componentCount {
		b.Write(components[i])
	}
	return b.Bytes()
}

// model is a traineddata whose language config reads the whole picture as
// one raw line (PSM_RAW_LINE), so the LSTM sees the image without Tesseract's
// layout analysis. An application that sets its own page segmentation mode
// overrides this, as it does with a real model's config.
func model(lstm, unicharset []byte) []byte {
	return traineddata(map[int][]byte{
		componentConfig:     []byte("tessedit_pageseg_mode 13\ninvert_threshold 0\n"),
		componentLSTM:       lstm,
		componentUnicharset: unicharset,
		// The component must be present, but is not read: training_flags does
		// not request a serialized recoder, so the loader makes a pass-through.
		componentRecoder: {0},
		componentVersion: []byte("5.5.2:axx"),
	})
}
