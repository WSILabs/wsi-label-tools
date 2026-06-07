package redact

import (
	"image"
	"image/color"
	"testing"

	"github.com/cornish/wsi-label-tools/internal/tifflow"
)

// fakeClassifier matches when match==true; records its name.
type fakeClassifier struct {
	name  string
	match bool
}

func (f fakeClassifier) Name() string                                     { return f.name }
func (f fakeClassifier) Detect(*tifflow.File) bool                        { return f.match }
func (fakeClassifier) Classify(*tifflow.File) ([]Target, error)          { return nil, nil }
func (fakeClassifier) BuildLabelIFD(image.Image) *tifflow.ReplacementIFD { return nil }
func (fakeClassifier) DefaultLabelDims() (int, int)                       { return 0, 0 }
func (fakeClassifier) DefaultLabelBG() color.RGBA                         { return color.RGBA{} }

func TestDetect_FallbackTriedLast(t *testing.T) {
	savedV, savedF := classifiers, fallbacks
	defer func() { classifiers, fallbacks = savedV, savedF }()
	classifiers, fallbacks = nil, nil

	RegisterFallback(fakeClassifier{name: "fallback", match: true})
	Register(fakeClassifier{name: "vendor", match: true})

	c, ok := detectClassifier(&tifflow.File{})
	if !ok || c.Name() != "vendor" {
		t.Fatalf("expected vendor to win over fallback, got ok=%v name=%v", ok, c)
	}
}

func TestDetect_FallbackUsedWhenNoVendorMatches(t *testing.T) {
	savedV, savedF := classifiers, fallbacks
	defer func() { classifiers, fallbacks = savedV, savedF }()
	classifiers, fallbacks = nil, nil

	RegisterFallback(fakeClassifier{name: "fallback", match: true})
	Register(fakeClassifier{name: "vendor", match: false})

	c, ok := detectClassifier(&tifflow.File{})
	if !ok || c.Name() != "fallback" {
		t.Fatalf("expected fallback, got ok=%v name=%v", ok, c)
	}
}
