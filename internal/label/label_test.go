package label

import (
	"bytes"
	"image"
	"image/color"
	"testing"

	"github.com/hhrutter/lzw"
)

func TestEncodeStrip_RoundTrip(t *testing.T) {
	// Make a 1200x2 strip of random-ish data.
	img := image.NewRGBA(image.Rect(0, 0, 1200, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 1200; x++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(x * 3), uint8(y * 50), 255})
		}
	}
	raw := rgbStripBytes(img)
	pre := applyPredictor2(raw, 1200, 2)
	compressed := encodeLZW(pre)

	// Decode and undo predictor.
	dec := lzw.NewReader(bytes.NewReader(compressed), true)
	defer dec.Close()
	decoded := make([]byte, len(pre))
	if _, err := io_readFull(dec, decoded); err != nil {
		t.Fatalf("lzw decode: %v", err)
	}
	undone := undoPredictor2(decoded, 1200, 2)
	if !bytes.Equal(undone, raw) {
		t.Errorf("round-trip mismatch: first diff at %d", firstDiff(undone, raw))
	}
}

// Replacement for io.ReadFull to avoid adding an import dance in the test.
func io_readFull(r interface{ Read(p []byte) (int, error) }, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := r.Read(buf[total:])
		total += n
		if err != nil {
			if total == len(buf) {
				return total, nil
			}
			return total, err
		}
	}
	return total, nil
}

func firstDiff(a, b []byte) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return -1
}

// undoPredictor2 is the inverse of applyPredictor2 for test purposes.
func undoPredictor2(p []byte, w, h int) []byte {
	out := make([]byte, len(p))
	stride := w * 3
	for y := 0; y < h; y++ {
		row := p[y*stride : (y+1)*stride]
		orow := out[y*stride : (y+1)*stride]
		copy(orow[:3], row[:3])
		for x := 1; x < w; x++ {
			for c := 0; c < 3; c++ {
				orow[x*3+c] = orow[(x-1)*3+c] + row[x*3+c]
			}
		}
	}
	return out
}
