package redact

import (
	"image"
	"image/color"

	"github.com/cornish/wsi-label-tools/internal/tifflow"
)

// Classifier is implemented by each format backend. Detect and Classify
// operate on an already-parsed file (the orchestrator parses once). The
// Build/Default methods supply the format-specific replacement-label spec.
type Classifier interface {
	Name() string
	Detect(file *tifflow.File) bool
	Classify(file *tifflow.File) ([]Target, error)
	BuildLabelIFD(img image.Image) *tifflow.ReplacementIFD
	DefaultLabelDims() (w, h int)
	DefaultLabelBG() color.RGBA
}

var (
	classifiers []Classifier // vendor classifiers, tried first
	fallbacks   []Classifier // greedy classifiers (e.g. generic-TIFF), tried last
)

// Register adds a vendor classifier. Format packages call this from init();
// the binary blank-imports those packages to trigger registration.
func Register(c Classifier) { classifiers = append(classifiers, c) }

// RegisterFallback adds a classifier that is only consulted after every
// vendor classifier has declined. Used by greedy matchers like generic-TIFF
// that would otherwise shadow more specific formats.
func RegisterFallback(c Classifier) { fallbacks = append(fallbacks, c) }

// detectClassifier returns the first vendor classifier that matches, else the
// first matching fallback.
func detectClassifier(file *tifflow.File) (Classifier, bool) {
	for _, c := range classifiers {
		if c.Detect(file) {
			return c, true
		}
	}
	for _, c := range fallbacks {
		if c.Detect(file) {
			return c, true
		}
	}
	return nil, false
}
