package aperio

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cornish/wsi-label-tools/internal/tifflow"
	"github.com/cornish/wsi-label-tools/internal/wsi"
)

func TestPickTargetDims_UsesExistingLabel(t *testing.T) {
	p := filepath.Join("..", "..", "sample_svs", "CMU-1-Small-Region.svs")
	if _, err := os.Stat(p); err != nil {
		t.Skipf("sample missing: %v", err)
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	file, err := tifflow.Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	labelIdx := FindLabelIFD(file)
	if labelIdx < 0 {
		t.Fatal("expected a label IFD in CMU fixture")
	}

	w, h := pickTargetDims(file, labelIdx, wsi.ReplaceOptions{})
	// The CMU-1-Small-Region label is 387x463 (portrait). The point of
	// the dim-preservation change is we target THAT, not the 1200x848
	// Aperio canonical default.
	if w != 387 || h != 463 {
		t.Errorf("auto target = %dx%d, want 387x463", w, h)
	}
}

func TestPickTargetDims_ExplicitOverride(t *testing.T) {
	p := filepath.Join("..", "..", "sample_svs", "CMU-1-Small-Region.svs")
	if _, err := os.Stat(p); err != nil {
		t.Skipf("sample missing: %v", err)
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	file, err := tifflow.Parse(f)
	if err != nil {
		t.Fatal(err)
	}

	w, h := pickTargetDims(file, FindLabelIFD(file), wsi.ReplaceOptions{
		TargetWidth:  800,
		TargetHeight: 600,
	})
	if w != 800 || h != 600 {
		t.Errorf("override = %dx%d, want 800x600", w, h)
	}
}

func TestPickTargetDims_FallsBackToDefaultWhenNoLabel(t *testing.T) {
	// Synthetic: no label IFD in a bare tifflow.File is simulated by
	// passing labelIdx=-1 and a non-nil file with an empty IFD list.
	file := &tifflow.File{}
	w, h := pickTargetDims(file, -1, wsi.ReplaceOptions{})
	if w != AperioLabelWidth || h != AperioLabelHeight {
		t.Errorf("default = %dx%d, want %dx%d", w, h, AperioLabelWidth, AperioLabelHeight)
	}
}
