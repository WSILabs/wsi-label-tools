package format

import (
	"image"
	"image/color"
	"strings"

	"github.com/cornish/wsi-label-tools/internal/label"
	"github.com/cornish/wsi-label-tools/internal/redact"
	"github.com/cornish/wsi-label-tools/internal/tifflow"
)

func init() { redact.Register(bif{}) }

// bifLabelDivisor: the label is the top 1/3 of the overview (the physical
// 25mm-of-75mm label zone).
const bifLabelDivisor = 3

// bif classifies Ventana BIF. Label = top 1/3 of the overview (IFD 0). Detect:
// BigTIFF + IFD0 XMP (tag 700) contains "<iScan". Redacted by the Region
// strategy (top band). Both DP200 and legacy iScan variants.
type bif struct{}

func (bif) Name() string { return "bif" }

func (bif) Detect(file *tifflow.File) bool {
	if !file.Header.BigTIFF || len(file.IFDs) == 0 {
		return false
	}
	xmp, ok := readXMP(file.IFDs[0])
	return ok && strings.Contains(xmp, "<iScan")
}

func (bif) Classify(file *tifflow.File) ([]redact.Target, error) {
	ifd := file.IFDs[0]
	w, _ := ifd.UintValue(tifflow.TagImageWidth, file.Header.ByteOrder)
	h, _ := ifd.UintValue(tifflow.TagImageLength, file.Header.ByteOrder)
	if w == 0 || h == 0 {
		return nil, nil
	}
	bandH := int(h) / bifLabelDivisor
	if bandH == 0 {
		return nil, nil
	}
	return []redact.Target{{
		Role:           redact.RoleLabel,
		Representation: redact.EmbeddedRegion,
		HostIFDIndex:   0,
		Region:         image.Rect(0, 0, int(w), bandH),
		Width:          int(w),
		Height:         bandH,
	}}, nil
}

func (bif) BuildLabelIFD(img image.Image) *tifflow.ReplacementIFD {
	return label.BuildAperioLabelIFD(img) // vestigial; Region path composites opts.Image
}

func (bif) DefaultLabelDims() (int, int) { return label.AperioLabelWidth, label.AperioLabelHeight }
func (bif) DefaultLabelBG() color.RGBA   { return label.AperioLabelBG }

// readXMP returns the IFD0 XMP blob (tag 700, TypeByte) as a string.
// StringValue cannot be used here because tag 700 is TypeByte, not TypeASCII;
// RawValue returns the cached bytes regardless of type.
func readXMP(ifd *tifflow.IFD) (string, bool) {
	b, ok := ifd.RawValue(700)
	if !ok {
		return "", false
	}
	return string(b), true
}
