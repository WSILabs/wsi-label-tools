package label

import (
	"errors"
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
	if out.At(0, 0) != bg {
		t.Errorf("corner not bg: %v", out.At(0, 0))
	}
	cx, cy := 1200/2, 848/2
	c := out.At(cx, cy)
	if r, g, b, _ := c.RGBA(); b <= r || b <= g {
		t.Errorf("center pixel not blue-dominant: %v", c)
	}
}

func TestPrepareLabel_ResizeNone_ExactDims(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, AperioLabelWidth, AperioLabelHeight))
	out, err := Prepare(src, AperioLabelWidth, AperioLabelHeight, PrepareOptions{
		Resize: ResizeNone,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.Bounds().Dx() != AperioLabelWidth || out.Bounds().Dy() != AperioLabelHeight {
		t.Errorf("dims = %v", out.Bounds())
	}
}

func TestPrepareLabel_ResizeNone_WrongDimsRejects(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 100, 100))
	_, err := Prepare(src, AperioLabelWidth, AperioLabelHeight, PrepareOptions{
		Resize: ResizeNone,
	})
	if err == nil {
		t.Fatalf("expected error for mismatched dims under --resize=none")
	}
}

func TestPrepareLabel_ResizeStretch(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 100, 100))
	out, err := Prepare(src, AperioLabelWidth, AperioLabelHeight, PrepareOptions{
		Resize: ResizeStretch,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.Bounds().Dx() != AperioLabelWidth || out.Bounds().Dy() != AperioLabelHeight {
		t.Errorf("dims = %v", out.Bounds())
	}
}

func TestPrepareLabel_AspectMismatchRejected(t *testing.T) {
	// 10000x10 is an extreme aspect (1000:1) vs Aperio's ~1.42:1 target.
	src := image.NewRGBA(image.Rect(0, 0, 10000, 10))
	_, err := Prepare(src, AperioLabelWidth, AperioLabelHeight, PrepareOptions{
		Resize: ResizeFit,
		BG:     color.RGBA{245, 245, 230, 255},
	})
	if !errors.Is(err, ErrAspectMismatch) {
		t.Errorf("err = %v, want ErrAspectMismatch", err)
	}
}

func TestPrepareLabel_AspectMismatchBypassedWithForce(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 10000, 10))
	out, err := Prepare(src, AperioLabelWidth, AperioLabelHeight, PrepareOptions{
		Resize: ResizeFit,
		BG:     color.RGBA{245, 245, 230, 255},
		Force:  true,
	})
	if err != nil {
		t.Fatalf("expected success with --force, got %v", err)
	}
	if out.Bounds().Dx() != AperioLabelWidth || out.Bounds().Dy() != AperioLabelHeight {
		t.Errorf("dims = %v", out.Bounds())
	}
}

func TestPrepareLabel_Rotate90_Accepted(t *testing.T) {
	// Square source: rotation doesn't change the aspect ratio, so the fit
	// path accepts it against Aperio's 1.42:1 target.
	src := image.NewRGBA(image.Rect(0, 0, 200, 200))
	out, err := Prepare(src, AperioLabelWidth, AperioLabelHeight, PrepareOptions{
		Resize: ResizeFit,
		BG:     color.RGBA{245, 245, 230, 255},
		Rotate: 90,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.Bounds().Dx() != AperioLabelWidth || out.Bounds().Dy() != AperioLabelHeight {
		t.Errorf("dims = %v", out.Bounds())
	}
}

func TestPrepareLabel_RotateInvalid(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 100, 100))
	_, err := Prepare(src, AperioLabelWidth, AperioLabelHeight, PrepareOptions{
		Resize: ResizeFit,
		BG:     color.RGBA{245, 245, 230, 255},
		Rotate: 45,
	})
	if err == nil {
		t.Errorf("expected error for rotate=45")
	}
}

func TestRotateExact_180_IsInvolution(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 4, 3))
	for y := 0; y < 3; y++ {
		for x := 0; x < 4; x++ {
			src.Set(x, y, color.RGBA{uint8(x), uint8(y), 100, 255})
		}
	}
	once := rotateExact(src, 180)
	twice := rotateExact(once, 180)
	for y := 0; y < 3; y++ {
		for x := 0; x < 4; x++ {
			if twice.At(x, y) != src.At(x, y) {
				t.Fatalf("(%d,%d): got %v, want %v", x, y, twice.At(x, y), src.At(x, y))
			}
		}
	}
}
