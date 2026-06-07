package label

import (
	"image"
	"image/color"
	"testing"

	"github.com/cornish/wsi-label-tools/internal/tifflow"
)

func TestBuildLabelIFD(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, AperioLabelWidth, AperioLabelHeight))
	// Fill with solid color for predictable LZW behavior.
	for y := 0; y < AperioLabelHeight; y++ {
		for x := 0; x < AperioLabelWidth; x++ {
			img.Set(x, y, color.RGBA{200, 200, 200, 255})
		}
	}
	rep := BuildAperioLabelIFD(img)
	if len(rep.StripData) != (AperioLabelHeight+AperioRowsPerStrip-1)/AperioRowsPerStrip {
		t.Errorf("strip count = %d, want %d", len(rep.StripData), 424)
	}
	tagMap := map[uint16]*tifflow.OutTag{}
	for i := range rep.Tags {
		tagMap[rep.Tags[i].Tag] = &rep.Tags[i]
	}
	for _, want := range []uint16{
		tifflow.TagNewSubfileType, tifflow.TagImageWidth, tifflow.TagImageLength,
		tifflow.TagBitsPerSample, tifflow.TagCompression, tifflow.TagPhotometric,
		tifflow.TagImageDescription, tifflow.TagSamplesPerPixel, tifflow.TagRowsPerStrip,
		tifflow.TagStripOffsets, tifflow.TagStripByteCounts, tifflow.TagPlanarConfig,
		tifflow.TagPredictor,
	} {
		if _, ok := tagMap[want]; !ok {
			t.Errorf("missing tag %d", want)
		}
	}
	if stripOff := tagMap[tifflow.TagStripOffsets]; stripOff == nil || !stripOff.ResolvesToOffset {
		t.Errorf("StripOffsets must ResolvesToOffset")
	}
}
