package format

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cornish/wsi-label-tools/internal/tifflow"
)

func detectPath(t *testing.T, p string) bool {
	t.Helper()
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	file, err := tifflow.Parse(f)
	if err != nil {
		if errors.Is(err, tifflow.ErrBadMagic) {
			return false // not a TIFF → not SVS (expected for the negative case)
		}
		t.Errorf("Parse %s: %v", p, err)
		return false
	}
	return svs{}.Detect(file)
}

func TestDetect_SampleFiles(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "sample_files", "svs")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("sample_files/svs not available: %v", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".svs" {
			continue
		}
		p := filepath.Join(dir, e.Name())
		if !detectPath(t, p) {
			t.Errorf("%s: Detect = false, want true", e.Name())
		}
	}
}

func TestDetect_NotTIFF(t *testing.T) {
	p := filepath.Join(t.TempDir(), "nope.svs")
	_ = os.WriteFile(p, []byte("not a tiff"), 0o644)
	if detectPath(t, p) {
		t.Errorf("Detect = true, want false")
	}
}
