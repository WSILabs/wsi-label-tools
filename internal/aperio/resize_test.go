package aperio

import (
	"image"
	"image/color"
	"testing"
)

func TestFitTo_ExactDims(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 1200, 848))
	src.Set(0, 0, color.RGBA{255, 0, 0, 255})
	out := FitTo(src, 1200, 848, color.RGBA{245, 245, 230, 255})
	if out.Bounds().Dx() != 1200 || out.Bounds().Dy() != 848 {
		t.Fatalf("dims = %v, want 1200x848", out.Bounds())
	}
	if out.At(0, 0) != (color.RGBA{255, 0, 0, 255}) {
		t.Errorf("pixel (0,0) not preserved")
	}
}

func TestFitTo_Letterboxed(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 100, 100)) // 1:1 into 1200x848
	for x := 0; x < 100; x++ {
		for y := 0; y < 100; y++ {
			src.Set(x, y, color.RGBA{0, 0, 255, 255})
		}
	}
	bg := color.RGBA{245, 245, 230, 255}
	out := FitTo(src, 1200, 848, bg)
	// Corners must be bg (letterbox), center is blue.
	if out.At(0, 0) != bg {
		t.Errorf("corner not bg: %v", out.At(0, 0))
	}
	cx, cy := 1200/2, 848/2
	c := out.At(cx, cy)
	if r, g, b, _ := c.RGBA(); b <= r || b <= g {
		t.Errorf("center pixel not blue-dominant: %v", c)
	}
}
