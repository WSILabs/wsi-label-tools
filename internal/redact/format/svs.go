// Package format holds per-format detectors/classifiers. Each registers
// itself with internal/redact via init(); the binary blank-imports this
// package to make the formats available.
package format

import (
	"image"
	"image/color"
	"strings"

	"github.com/cornish/wsi-label-tools/internal/label"
	"github.com/cornish/wsi-label-tools/internal/redact"
	"github.com/cornish/wsi-label-tools/internal/tifflow"
)

func init() { redact.Register(svs{}) }

// svs is the Aperio SVS classifier. Detection keys on IFD 0's
// ImageDescription "Aperio" prefix; label/overview are located by the
// OpenSlide-compatible rule: a non-tiled IFD other than IFD 1 (thumbnail)
// whose ImageDescription second line starts with "label"/"macro".
type svs struct{}

func (svs) Name() string { return "aperio" }

func (svs) Detect(file *tifflow.File) bool {
	if len(file.IFDs) == 0 {
		return false
	}
	desc, ok := file.IFDs[0].StringValue(tifflow.TagImageDescription)
	if !ok {
		return false
	}
	return strings.HasPrefix(desc, "Aperio")
}

func (s svs) Classify(file *tifflow.File) ([]redact.Target, error) {
	var targets []redact.Target
	if t, ok := s.find(file, redact.RoleLabel, "label"); ok {
		targets = append(targets, t)
	}
	if t, ok := s.find(file, redact.RoleOverview, "macro"); ok {
		targets = append(targets, t)
	}
	return targets, nil
}

// find returns the first associated IFD whose ImageDescription second line
// starts with prefix, as a Target of the given role.
func (svs) find(file *tifflow.File, role redact.Role, prefix string) (redact.Target, bool) {
	for i, ifd := range file.IFDs {
		if i == 1 || hasTileTags(ifd) {
			continue
		}
		desc, ok := ifd.StringValue(tifflow.TagImageDescription)
		if !ok {
			continue
		}
		lines := strings.SplitN(desc, "\n", 3)
		if len(lines) < 2 || !strings.HasPrefix(lines[1], prefix) {
			continue
		}
		w, _ := ifd.UintValue(tifflow.TagImageWidth, file.Header.ByteOrder)
		h, _ := ifd.UintValue(tifflow.TagImageLength, file.Header.ByteOrder)
		return redact.Target{
			Role: role, Representation: redact.SeparableIFD,
			IFDIndex: i, Width: int(w), Height: int(h),
		}, true
	}
	return redact.Target{}, false
}

func (svs) BuildLabelIFD(img image.Image) *tifflow.ReplacementIFD {
	return label.BuildAperioLabelIFD(img)
}

func (svs) DefaultLabelDims() (int, int) { return label.AperioLabelWidth, label.AperioLabelHeight }
func (svs) DefaultLabelBG() color.RGBA   { return label.AperioLabelBG }

func hasTileTags(ifd *tifflow.IFD) bool {
	for _, e := range ifd.Entries {
		if e.Tag == tifflow.TagTileOffsets {
			return true
		}
	}
	return false
}
