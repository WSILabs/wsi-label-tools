package aperio

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetect_SampleFiles(t *testing.T) {
	dir := filepath.Join("..", "..", "sample_svs")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("sample_svs not available: %v", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".svs" {
			continue
		}
		p := filepath.Join(dir, e.Name())
		ok, err := Detect(p)
		if err != nil {
			t.Errorf("%s: err %v", e.Name(), err)
			continue
		}
		if !ok {
			t.Errorf("%s: Detect = false, want true", e.Name())
		}
	}
}

func TestDetect_NotTIFF(t *testing.T) {
	p := filepath.Join(t.TempDir(), "nope.svs")
	_ = os.WriteFile(p, []byte("not a tiff"), 0o644)
	ok, err := Detect(p)
	if err != nil {
		t.Errorf("err: %v", err)
	}
	if ok {
		t.Errorf("Detect = true, want false")
	}
}
