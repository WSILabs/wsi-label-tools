package format

import (
	"path/filepath"
	"testing"

	"github.com/cornish/wsi-label-tools/internal/redact"
)

func TestNDPI_DetectAndRegion(t *testing.T) {
	p := filepath.Join("..", "..", "..", "sample_files", "ndpi", "CMU-1.ndpi")
	file := parseSample(t, p)
	n := ndpi{}
	if !n.Detect(file) {
		t.Fatalf("ndpi did not detect CMU-1.ndpi")
	}
	targets, err := n.Classify(file)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 {
		t.Fatalf("want 1 target, got %+v", targets)
	}
	tg := targets[0]
	if tg.Role != redact.RoleLabel || tg.Representation != redact.EmbeddedRegion {
		t.Fatalf("want RoleLabel/EmbeddedRegion, got %+v", tg)
	}
	if tg.Height != 408 || tg.Width < 300 || tg.Width > 400 {
		t.Fatalf("region = %dx%d, want ~357x408", tg.Width, tg.Height)
	}
}
