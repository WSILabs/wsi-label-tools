package format

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cornish/wsi-label-tools/internal/redact"
	"github.com/cornish/wsi-label-tools/internal/tifflow"
)

func parseSample(t *testing.T, p string) *tifflow.File {
	t.Helper()
	if _, err := os.Stat(p); err != nil {
		t.Skipf("fixture absent: %v", err)
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	file, err := tifflow.Parse(f)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return file
}

func TestGenericTIFF_DetectAndLabel(t *testing.T) {
	p := filepath.Join("..", "..", "..", "sample_files", "generic-tiff", "CMU-1-Small-Region.stripped.tiff")
	file := parseSample(t, p)

	g := genericTIFF{}
	if (svs{}).Detect(file) {
		t.Fatalf("svs wrongly detected the stripped generic-TIFF")
	}
	if !g.Detect(file) {
		t.Fatalf("genericTIFF did not detect the stripped generic-TIFF")
	}
	targets, err := g.Classify(file)
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
		t.Fatalf("no label classified; targets=%+v", targets)
	}
	if lbl.Width != 387 || lbl.Height != 463 {
		t.Fatalf("label dims = %dx%d, want 387x463", lbl.Width, lbl.Height)
	}
}
