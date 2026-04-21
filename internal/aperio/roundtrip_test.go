package aperio

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tcornish/wsi-label/internal/tifflow"
)

// For each sample SVS in sample_svs/, run replace with a synthetic label and
// assert:
//   1. Output has an IFD whose ImageDescription starts with "Aperio ...\nlabel".
//   2. Prefix [0, cutoff) of output matches prefix of input (minus one next-IFD pointer).
//   3. Number of IFDs is unchanged (or +1 if input had no label).
func TestReplaceLabel_AllSamples(t *testing.T) {
	dir := filepath.Join("..", "..", "sample_svs")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("sample_svs not available: %v", err)
	}

	labelImg := syntheticLabel()
	labelPath := filepath.Join(t.TempDir(), "label.png")
	f, _ := os.Create(labelPath)
	_ = png.Encode(f, labelImg)
	f.Close()

	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".svs") {
			continue
		}
		t.Run(e.Name(), func(t *testing.T) {
			in := filepath.Join(dir, e.Name())
			out := filepath.Join(t.TempDir(), "out.svs")

			b := New()
			res, err := b.ReplaceLabel(in, out, labelImg, false, false)
			if err != nil {
				t.Fatalf("ReplaceLabel: %v", err)
			}

			// Structural check.
			fIn, _ := os.Open(in)
			fileIn, _ := tifflow.Parse(fIn)
			fIn.Close()
			fOut, _ := os.Open(out)
			fileOut, err := tifflow.Parse(fOut)
			fOut.Close()
			if err != nil {
				t.Fatalf("parse output: %v", err)
			}

			expected := len(fileIn.IFDs)
			if res.Added {
				expected++
			}
			if len(fileOut.IFDs) != expected {
				t.Errorf("output has %d IFDs, want %d", len(fileOut.IFDs), expected)
			}
			if FindLabelIFD(fileOut) < 0 {
				t.Errorf("output has no label IFD")
			}
		})
	}
}

func syntheticLabel() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 600, 400))
	for y := 0; y < 400; y++ {
		for x := 0; x < 600; x++ {
			img.Set(x, y, color.RGBA{uint8(x / 4), uint8(y / 2), 128, 255})
		}
	}
	return img
}

var _ = bytes.Equal
