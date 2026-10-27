package onnx

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nimbusxr/axx/packs/models/core/contract"
)

// processorFile is a Hugging Face model's image processor's configuration,
// which its folder keeps beside its model, or in the folder above it
// (onnx/model.onnx, as transformers.js and Optimum have it).
const processorFile = "preprocessor_config.json"

// readProcessor gives the model's image inputs the fitting its image
// processor does, when its folder has one.
func readProcessor(c *contract.Contract, path string) error {
	dir := filepath.Dir(path)
	for _, d := range []string{dir, filepath.Dir(dir)} {
		if b, err := os.ReadFile(filepath.Join(d, processorFile)); err == nil {
			if err := imageProcessor(c, b); err != nil {
				return fmt.Errorf("%s: %w", filepath.Join(d, processorFile), err)
			}
			return nil
		}
	}
	return nil
}

// imageProcessor fits the contract's image inputs as an image processor's
// configuration says the model's pictures are fitted: in RGB, as Hugging
// Face's processors give them; stretched to its size (a size of a height and
// a width, or one number without a crop: ViT's, BiRefNet's); or its shorter
// side resized and its middle cropped (a shortest edge, or one number, and a
// crop: ConvNeXT's, CLIP's). A crop keeps its share of the middle. A
// processor that does not resize leaves the inputs as they are.
func imageProcessor(c *contract.Contract, config []byte) error {
	var p struct {
		DoResize     *bool           `json:"do_resize"`
		Size         json.RawMessage `json:"size"`
		DoCenterCrop bool            `json:"do_center_crop"`
		CropSize     json.RawMessage `json:"crop_size"`
	}
	if err := json.Unmarshal(config, &p); err != nil {
		return err
	}
	if p.DoResize != nil && !*p.DoResize {
		return nil
	}
	size, square := sides(p.Size)
	crop, cropSquare := sides(p.CropSize)
	kept := max(crop.Height, cropSquare)
	fit := contract.Image{Channels: "RGB"}
	var side int
	switch {
	case size.Height > 0 && size.Width > 0, square > 0 && !p.DoCenterCrop:
		// Stretched to the size; with a crop, its middle kept.
		fit.Resize, side = "stretch", max(size.Height, square)
	case size.ShortestEdge > 0, square > 0:
		// The shorter side resized, its middle cropped.
		fit.Resize, side = "center-crop", max(size.ShortestEdge, square)
		if !p.DoCenterCrop || kept == 0 {
			return nil
		}
	default:
		return nil
	}
	if p.DoCenterCrop && kept > 0 && kept < side {
		fit.Crop = float64(kept) / float64(side)
	}
	for i := range c.Inputs {
		if img := c.Inputs[i].Image; img != nil {
			img.Channels, img.Resize, img.Crop = fit.Channels, fit.Resize, fit.Crop
		}
	}
	return nil
}

// processorSize is a size of an image processor's configuration.
type processorSize struct {
	Height       int `json:"height"`
	Width        int `json:"width"`
	ShortestEdge int `json:"shortest_edge"`
}

// sides reads a size: an object of its sides, or one number (a square's, or
// an older processor's shorter side).
func sides(raw json.RawMessage) (processorSize, int) {
	var s processorSize
	if json.Unmarshal(raw, &s) == nil {
		return s, 0
	}
	var n int
	_ = json.Unmarshal(raw, &n)
	return processorSize{}, n
}
