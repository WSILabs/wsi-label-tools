package format

import (
	"path/filepath"
	"testing"

	"github.com/cornish/wsi-label-tools/internal/redact"
)

// CI-available: Philips-4 (wsi-fixtures v2) detects + has an overview, no label.
func TestPhilips_DetectAndClassify(t *testing.T) {
	p := filepath.Join("..", "..", "..", "sample_files", "philips-tiff", "Philips-4.tiff")
	file := parseSample(t, p) // helper in generictiff_test.go; skips if absent
	ph := philips{}
	if !ph.Detect(file) {
		t.Fatalf("philips did not detect Philips-4.tiff")
	}
	targets, err := ph.Classify(file)
	if err != nil {
		t.Fatal(err)
	}
	var hasOverview, hasLabel bool
	for _, tg := range targets {
		switch tg.Role {
		case redact.RoleOverview:
			hasOverview = true
		case redact.RoleLabel:
			hasLabel = true
		}
	}
	if !hasOverview {
		t.Fatalf("expected overview target; got %+v", targets)
	}
	if hasLabel {
		t.Fatalf("Philips-4 has no label, but one was classified: %+v", targets)
	}
}

// Local-only: the only label-bearing Philips sample (Philips-3) is 3.1GB and
// not in wsi-fixtures. Validates the classifier locates the label. Runs
// against a sibling opentile-go checkout; skips otherwise. A small reduced
// fixture will replace this later (wsitools to provide).
func TestPhilips_LabelLocal(t *testing.T) {
	p := filepath.Join("..", "..", "..", "..", "opentile-go", "sample_files", "philips-tiff", "Philips-3.tiff")
	file := parseSample(t, p)
	ph := philips{}
	if !ph.Detect(file) {
		t.Fatalf("philips did not detect Philips-3.tiff")
	}
	targets, err := ph.Classify(file)
	if err != nil {
		t.Fatal(err)
	}
	var lbl *redact.Target
	for i := range targets {
		if targets[i].Role == redact.RoleLabel {
			lbl = &targets[i]
		}
	}
	if lbl == nil {
		t.Fatalf("no label classified for Philips-3.tiff; targets=%+v", targets)
	}
	if lbl.Width != 387 || lbl.Height != 403 {
		t.Fatalf("label dims = %dx%d, want 387x403", lbl.Width, lbl.Height)
	}
}
