package tifflow

import (
	"os"
	"path/filepath"
	"testing"
)

// A small SVS sample expected to exist for tests; skip if not present.
func svsSample(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join("..", "..", "sample_svs", name)
	if _, err := os.Stat(p); err != nil {
		t.Skipf("sample %s not available: %v", name, err)
	}
	return p
}

func TestParse_CMU1SmallRegion(t *testing.T) {
	p := svsSample(t, "CMU-1-Small-Region.svs")
	f, err := os.Open(p)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()

	file, err := Parse(f)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(file.IFDs) < 2 {
		t.Errorf("expected at least 2 IFDs, got %d", len(file.IFDs))
	}
	if file.Header == nil {
		t.Fatalf("nil header")
	}
	// At least one IFD should have an ImageDescription.
	found := false
	for _, ifd := range file.IFDs {
		if _, ok := ifd.StringValue(TagImageDescription); ok {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("no IFD has ImageDescription")
	}
}

func TestParse_RangesPopulated(t *testing.T) {
	p := svsSample(t, "CMU-1-Small-Region.svs")
	f, err := os.Open(p)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()

	file, err := Parse(f)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if file.Ranges == nil || len(file.Ranges.All()) == 0 {
		t.Fatal("no ranges populated")
	}
	// Each IFD must have at least one range.
	for i := range file.IFDs {
		if file.Ranges.MinOffsetOfOwner(i) == ^uint64(0) {
			t.Errorf("IFD %d has no ranges", i)
		}
	}
}
