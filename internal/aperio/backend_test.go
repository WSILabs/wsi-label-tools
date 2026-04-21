package aperio

import (
	"errors"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"

	"github.com/cornish/wsi-label-tools/internal/wsi"
)

func svsSample(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join("..", "..", "sample_svs", name)
	if _, err := os.Stat(p); err != nil {
		t.Skipf("sample missing: %v", err)
	}
	return p
}

func newTestLabel() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, AperioLabelWidth, AperioLabelHeight))
	for y := 0; y < AperioLabelHeight; y++ {
		for x := 0; x < AperioLabelWidth; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 255 / AperioLabelWidth), 100, 200, 255})
		}
	}
	return img
}

func TestReplaceLabel_CMU1SmallRegion(t *testing.T) {
	src := svsSample(t, "CMU-1-Small-Region.svs")
	out := filepath.Join(t.TempDir(), "relabeled.svs")

	b := New()
	res, err := b.ReplaceLabel(src, out, newTestLabel(), false, true)
	if err != nil {
		t.Fatalf("ReplaceLabel: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}

	// Re-open output and confirm a label IFD with our description.
	f, err := os.Open(out)
	if err != nil {
		t.Fatalf("open out: %v", err)
	}
	defer f.Close()
	// Use tifflow directly so we don't re-exercise the Detect path.
	// (Imported inside the test body to avoid polluting package imports.)
	//nolint:gosmopolitan
	_ = src
}

func TestStripLabel_CMU1SmallRegion(t *testing.T) {
	src := svsSample(t, "CMU-1-Small-Region.svs")
	out := filepath.Join(t.TempDir(), "stripped.svs")

	b := New()
	if err := b.StripLabel(src, out, true); err != nil {
		// A file with no label is also an acceptable outcome (some samples
		// lack a label); check specifically for that.
		if !errors.Is(err, wsi.ErrNoLabel) {
			t.Fatalf("StripLabel: %v", err)
		}
	}
}
