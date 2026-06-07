package format

import (
	"image"
	"image/color"

	"github.com/cornish/wsi-label-tools/internal/label"
	"github.com/cornish/wsi-label-tools/internal/redact"
	"github.com/cornish/wsi-label-tools/internal/tifflow"
)

func init() { redact.Register(ndpi{}) }

const (
	ndpiTagFileFormat    = 65420 // FileFormat (presence marker)
	ndpiTagMagnification = 65421 // FLOAT; -1.0 = macro, -2.0 = map, >0 = level
	ndpiTagMake          = 271
	ndpiLabelCropFrac    = 0.30 // label = left 30% of the macro
)

// ndpi classifies Hamamatsu NDPI. The label has no IFD: it is the left ~30% of
// the macro (the page with magnification == -1.0). We emit a RoleLabel target
// with EmbeddedRegion (host = macro IFD, region = left crop), redacted by the
// Region strategy.
type ndpi struct{}

func (ndpi) Name() string { return "ndpi" }

func (ndpi) Detect(file *tifflow.File) bool {
	if len(file.IFDs) == 0 {
		return false
	}
	ifd0 := file.IFDs[0]
	hasFormat := false
	for _, e := range ifd0.Entries {
		if e.Tag == ndpiTagFileFormat {
			hasFormat = true
		}
	}
	_, hasMake := ifd0.StringValue(ndpiTagMake)
	return hasFormat && hasMake
}

func (n ndpi) Classify(file *tifflow.File) ([]redact.Target, error) {
	macroIdx := n.macroIFD(file)
	if macroIdx < 0 {
		return nil, nil
	}
	ifd := file.IFDs[macroIdx]
	w, _ := ifd.UintValue(tifflow.TagImageWidth, file.Header.ByteOrder)
	h, _ := ifd.UintValue(tifflow.TagImageLength, file.Header.ByteOrder)
	cropW := int(float64(w) * ndpiLabelCropFrac)
	if cropW <= 0 {
		return nil, nil
	}
	return []redact.Target{{
		Role:           redact.RoleLabel,
		Representation: redact.EmbeddedRegion,
		HostIFDIndex:   macroIdx,
		Region:         image.Rect(0, 0, cropW, int(h)),
		Width:          cropW,
		Height:         int(h),
	}}, nil
}

func (ndpi) macroIFD(file *tifflow.File) int {
	for i, ifd := range file.IFDs {
		if mag, ok := ifd.FloatValue(ndpiTagMagnification, file.Header.ByteOrder); ok && mag == -1.0 {
			return i
		}
	}
	return -1
}

// BuildLabelIFD is vestigial for NDPI (the Region strategy composites
// opts.Image into the macro and never calls this); present to satisfy the
// Classifier interface.
func (ndpi) BuildLabelIFD(img image.Image) *tifflow.ReplacementIFD {
	return label.BuildAperioLabelIFD(img)
}

func (ndpi) DefaultLabelDims() (int, int) { return label.AperioLabelWidth, label.AperioLabelHeight }
func (ndpi) DefaultLabelBG() color.RGBA   { return label.AperioLabelBG }
