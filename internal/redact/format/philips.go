package format

import (
	"image"
	"image/color"
	"strings"

	"github.com/cornish/wsi-label-tools/internal/label"
	"github.com/cornish/wsi-label-tools/internal/redact"
	"github.com/cornish/wsi-label-tools/internal/tifflow"
)

func init() { redact.Register(philips{}) }

const philipsSoftwarePrefix = "Philips DP"

// philips classifies Philips-TIFF. Detect: IFD0 Software starts "Philips DP"
// (real Philips files have an empty or absent ImageDescription in IFD0).
// Classify: non-tiled pages by ImageDescription substring ("Label"/"Macro"/
// "Thumbnail", case-sensitive, first match per role wins) — ported from
// opentile-go formats/philipstiff (v0.36.0).
type philips struct{}

func (philips) Name() string { return "philips" }

func (philips) Detect(file *tifflow.File) bool {
	if len(file.IFDs) == 0 {
		return false
	}
	sw, ok := file.IFDs[0].StringValue(tifflow.TagSoftware)
	return ok && strings.HasPrefix(sw, philipsSoftwarePrefix)
}

func (philips) Classify(file *tifflow.File) ([]redact.Target, error) {
	roleFor := func(desc string) (redact.Role, bool) {
		switch {
		case strings.Contains(desc, "Label"):
			return redact.RoleLabel, true
		case strings.Contains(desc, "Macro"):
			return redact.RoleOverview, true
		case strings.Contains(desc, "Thumbnail"):
			return redact.RoleThumbnail, true
		}
		return 0, false
	}
	seen := map[redact.Role]bool{}
	var targets []redact.Target
	for i, ifd := range file.IFDs {
		if i == 0 || hasTileTags(ifd) {
			continue
		}
		desc, ok := ifd.StringValue(tifflow.TagImageDescription)
		if !ok {
			continue
		}
		role, ok := roleFor(desc)
		if !ok || seen[role] {
			continue
		}
		seen[role] = true
		w, _ := ifd.UintValue(tifflow.TagImageWidth, file.Header.ByteOrder)
		h, _ := ifd.UintValue(tifflow.TagImageLength, file.Header.ByteOrder)
		targets = append(targets, redact.Target{
			Role: role, Representation: redact.SeparableIFD,
			IFDIndex: i, Width: int(w), Height: int(h),
		})
	}
	return targets, nil
}

func (philips) BuildLabelIFD(img image.Image) *tifflow.ReplacementIFD {
	// A minimal ImageDescription containing "Label" so opentile/Philips
	// re-classifies the replaced IFD as the label on re-open.
	return label.BuildStripLabelIFD(img, "Label")
}

func (philips) DefaultLabelDims() (int, int) { return label.AperioLabelWidth, label.AperioLabelHeight }
func (philips) DefaultLabelBG() color.RGBA   { return label.AperioLabelBG }
