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

var classifiers []Classifier

// Register adds a classifier to the registry. Format packages call this from
// init(); the binary blank-imports those packages to trigger registration.
func Register(c Classifier) { classifiers = append(classifiers, c) }

// detectClassifier returns the first registered classifier that matches.
func detectClassifier(file *tifflow.File) (Classifier, bool) {
	for _, c := range classifiers {
		if c.Detect(file) {
			return c, true
		}
	}
	return nil, false
}
