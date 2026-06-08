package format

import (
	"path/filepath"
	"testing"

	"github.com/cornish/wsi-label-tools/internal/redact"
)

func TestBIF_DetectAndRegion(t *testing.T) {
	cases := []struct {
		path string
		w, h int
	}{
		{filepath.Join("..", "..", "..", "sample_files", "bif", "Ventana-1.bif"), 1251, 3685},
		{filepath.Join("..", "..", "..", "..", "opentile-go", "sample_files", "bif", "OS-1.bif"), 1008, 3008},
	}
	for _, c := range cases {
		file := parseSample(t, c.path) // helper in generictiff_test.go; skips if absent
		b := bif{}
		if !b.Detect(file) {
			t.Fatalf("%s: not detected as BIF", c.path)
		}
		targets, err := b.Classify(file)
		if err != nil || len(targets) != 1 {
			t.Fatalf("%s: classify=%+v err=%v", c.path, targets, err)
		}
		tg := targets[0]
		if tg.Role != redact.RoleLabel || tg.Representation != redact.EmbeddedRegion || tg.HostIFDIndex != 0 {
			t.Fatalf("%s: got %+v", c.path, tg)
		}
		if tg.Width != c.w || tg.Height != c.h/3 {
			t.Fatalf("%s: region %dx%d, want %dx%d", c.path, tg.Width, tg.Height, c.w, c.h/3)
		}
	}
}
