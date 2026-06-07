package format

import (
	"image"
	"image/color"

	"github.com/cornish/wsi-label-tools/internal/label"
	"github.com/cornish/wsi-label-tools/internal/redact"
	"github.com/cornish/wsi-label-tools/internal/tifflow"
)

func init() { redact.RegisterFallback(genericTIFF{}) }

// generic-TIFF threshold constants, ported verbatim from opentile-go
// formats/generictiff/classifier.go (v0.36.0).
const (
	gtLabelMaxDim     = 1500
	gtMacroAspect     = 2.0
	gtMacroMinDim     = 1000
	gtMacroMaxDim     = 5000
	gtThumbMaxDim     = 1500
	gtTiledThumbRatio = 0.001 // <0.1% baseline area
	gtTiledOverRatio  = 0.01  // <1% baseline area
	compLZW           = 5
	compJPEG          = 7
)

// genericTIFF is the fallback classifier for plain pyramidal TIFFs that no
// vendor classifier claimed. Registered via RegisterFallback so SVS / Philips
// win first.
type genericTIFF struct{}

func (genericTIFF) Name() string { return "generic-tiff" }

// Detect: a parsed TIFF with at least one tiled IFD (a pyramid baseline) and
// at least one non-tiled associated candidate. Permissive on purpose — only
// runs after vendor classifiers decline.
func (genericTIFF) Detect(file *tifflow.File) bool {
	tiled, strip := 0, 0
	for _, ifd := range file.IFDs {
		if hasTileTags(ifd) {
			tiled++
		} else {
			strip++
		}
	}
	return tiled >= 1 && len(file.IFDs) >= 2 && strip >= 1
}

func (g genericTIFF) Classify(file *tifflow.File) ([]redact.Target, error) {
	baseW, baseH := g.baselineDims(file)
	baseArea := float64(baseW) * float64(baseH)

	var targets []redact.Target
	for i, ifd := range file.IFDs {
		role, ok := g.classifyIFD(file, ifd, baseArea)
		if !ok {
			continue
		}
		w, _ := ifd.UintValue(tifflow.TagImageWidth, file.Header.ByteOrder)
		h, _ := ifd.UintValue(tifflow.TagImageLength, file.Header.ByteOrder)
		targets = append(targets, redact.Target{
			Role: role, Representation: redact.SeparableIFD,
			IFDIndex: i, Width: int(w), Height: int(h),
		})
	}
	return targets, nil
}

// baselineDims returns the dimensions of the largest tiled IFD (the L0
// baseline), used for tiled associated-image area ratios.
func (genericTIFF) baselineDims(file *tifflow.File) (int, int) {
	bw, bh := 0, 0
	for _, ifd := range file.IFDs {
		if !hasTileTags(ifd) {
			continue
		}
		w, _ := ifd.UintValue(tifflow.TagImageWidth, file.Header.ByteOrder)
		h, _ := ifd.UintValue(tifflow.TagImageLength, file.Header.ByteOrder)
		if int(w)*int(h) > bw*bh {
			bw, bh = int(w), int(h)
		}
	}
	return bw, bh
}

// classifyIFD returns a redact.Role for an associated (non-pyramid) IFD, or
// ok=false if the IFD is a pyramid level or unclassifiable. tag 65080 is
// authoritative; otherwise the opentile heuristic ladder applies.
func (g genericTIFF) classifyIFD(file *tifflow.File, ifd *tifflow.IFD, baseArea float64) (redact.Role, bool) {
	if wt, ok := ifd.StringValue(tifflow.TagWSIImageType); ok {
		switch wt {
		case "label":
			return redact.RoleLabel, true
		case "macro", "overview":
			return redact.RoleOverview, true
		case "thumbnail":
			return redact.RoleThumbnail, true
		case "pyramid":
			return 0, false
		}
	}

	w, _ := ifd.UintValue(tifflow.TagImageWidth, file.Header.ByteOrder)
	h, _ := ifd.UintValue(tifflow.TagImageLength, file.Header.ByteOrder)
	wi, hi := int(w), int(h)
	comp, _ := ifd.UintValue(tifflow.TagCompression, file.Header.ByteOrder)

	if !hasTileTags(ifd) {
		larger, smaller := wi, hi
		if hi > wi {
			larger, smaller = hi, wi
		}
		switch {
		case comp == compLZW && wi < gtLabelMaxDim && hi < gtLabelMaxDim:
			return redact.RoleLabel, true
		case comp == compJPEG && larger >= gtMacroMinDim && larger <= gtMacroMaxDim &&
			smaller > 0 && float64(larger)/float64(smaller) >= gtMacroAspect:
			return redact.RoleOverview, true
		case comp == compJPEG && wi < gtThumbMaxDim && hi < gtThumbMaxDim:
			return redact.RoleThumbnail, true
		}
		return 0, false
	}

	if baseArea > 0 {
		ratio := (float64(wi) * float64(hi)) / baseArea
		switch {
		case ratio < gtTiledThumbRatio:
			return redact.RoleThumbnail, true
		case ratio < gtTiledOverRatio:
			return redact.RoleOverview, true
		}
	}
	return 0, false
}

func (genericTIFF) BuildLabelIFD(img image.Image) *tifflow.ReplacementIFD {
	// Stamp the authoritative tag 65080="label" so re-open classifies it
	// regardless of heuristics. The LZW/stripped/<1500px body also satisfies
	// the heuristic-label rule as a belt-and-suspenders fallback.
	return label.BuildStripLabelIFD(img, "wsi-label\nlabel", tifflow.OutTag{
		Tag:   tifflow.TagWSIImageType,
		Type:  tifflow.TypeASCII,
		Count: uint64(len("label") + 1),
		Bytes: append([]byte("label"), 0),
	})
}

func (genericTIFF) DefaultLabelDims() (int, int) {
	return label.AperioLabelWidth, label.AperioLabelHeight
}
func (genericTIFF) DefaultLabelBG() color.RGBA { return label.AperioLabelBG }
