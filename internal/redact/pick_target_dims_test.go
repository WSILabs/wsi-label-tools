package redact

import (
	"image"
	"image/color"
	"testing"

	"github.com/cornish/wsi-label-tools/internal/tifflow"
)

// stubClassifier supplies deterministic defaults for unit-testing the
// orchestrator without a real format.
type stubClassifier struct{}

func (stubClassifier) Name() string                                      { return "stub" }
func (stubClassifier) Detect(*tifflow.File) bool                         { return false }
func (stubClassifier) Classify(*tifflow.File) ([]Target, error)          { return nil, nil }
func (stubClassifier) BuildLabelIFD(image.Image) *tifflow.ReplacementIFD { return nil }
func (stubClassifier) DefaultLabelDims() (int, int)                      { return 1200, 848 }
func (stubClassifier) DefaultLabelBG() color.RGBA                        { return color.RGBA{} }

func TestPickTargetDims_ExplicitOverride(t *testing.T) {
	w, h := pickTargetDims(&Target{Width: 100, Height: 50}, stubClassifier{},
		ReplaceOptions{TargetWidth: 640, TargetHeight: 480})
	if w != 640 || h != 480 {
		t.Fatalf("override: got %dx%d, want 640x480", w, h)
	}
}

func TestPickTargetDims_UsesExistingLabel(t *testing.T) {
	w, h := pickTargetDims(&Target{Width: 387, Height: 463}, stubClassifier{}, ReplaceOptions{})
	if w != 387 || h != 463 {
		t.Fatalf("existing label: got %dx%d, want 387x463", w, h)
	}
}

func TestPickTargetDims_FallsBackToDefault(t *testing.T) {
	w, h := pickTargetDims(nil, stubClassifier{}, ReplaceOptions{})
	if w != 1200 || h != 848 {
		t.Fatalf("default: got %dx%d, want 1200x848", w, h)
	}
}

func TestPickTargetDims_HalfOverrideIgnored(t *testing.T) {
	// Only width set → override ignored, falls back to existing label dims.
	w, h := pickTargetDims(&Target{Width: 387, Height: 463}, stubClassifier{},
		ReplaceOptions{TargetWidth: 640})
	if w != 387 || h != 463 {
		t.Fatalf("half override (width only): got %dx%d, want 387x463 (override ignored)", w, h)
	}
	// Only height set, no existing label → falls back to classifier default.
	w, h = pickTargetDims(nil, stubClassifier{}, ReplaceOptions{TargetHeight: 480})
	if w != 1200 || h != 848 {
		t.Fatalf("half override (height only): got %dx%d, want 1200x848 (override ignored)", w, h)
	}
}
