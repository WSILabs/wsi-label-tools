package label

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"

	xdraw "golang.org/x/image/draw"
)

// aspectMismatchThreshold: if source and target aspect ratios differ by
// more than this factor, ResizeFit refuses without --force.
const aspectMismatchThreshold = 2.0

// Prepare transforms a caller-supplied image into the width×height
// RGBA canvas this backend needs. Applies rotation first, then resize
// according to opts.Resize, with opts.BG used for letterbox and opts.Force
// bypassing the aspect-mismatch safety check.
func Prepare(src image.Image, width, height int, opts PrepareOptions) (*image.RGBA, error) {
	rotated, err := rotate(src, opts.Rotate)
	if err != nil {
		return nil, err
	}

	sb := rotated.Bounds()
	sw, sh := sb.Dx(), sb.Dy()

	switch opts.Resize {
	case ResizeNone:
		if sw != width || sh != height {
			return nil, fmt.Errorf(
				"label is %dx%d but target is %dx%d and --resize=none",
				sw, sh, width, height,
			)
		}
		dst := image.NewRGBA(image.Rect(0, 0, width, height))
		draw.Draw(dst, dst.Bounds(), rotated, sb.Min, draw.Src)
		return dst, nil

	case ResizeStretch:
		dst := image.NewRGBA(image.Rect(0, 0, width, height))
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), rotated, sb, xdraw.Over, nil)
		return dst, nil

	case ResizeFit:
		fallthrough
	default:
		if !opts.Force {
			srcAspect := float64(sw) / float64(sh)
			dstAspect := float64(width) / float64(height)
			ratio := math.Max(srcAspect/dstAspect, dstAspect/srcAspect)
			if ratio > aspectMismatchThreshold {
				return nil, fmt.Errorf(
					"%w: source %dx%d (aspect %.2f), target %dx%d (aspect %.2f), ratio %.2f > %.1f",
					ErrAspectMismatch,
					sw, sh, srcAspect, width, height, dstAspect, ratio, aspectMismatchThreshold,
				)
			}
		}
		return FitTo(rotated, width, height, opts.BG), nil
	}
}

// FitTo returns a width×height RGBA image containing src aspect-preservingly
// scaled to fit, with letterbox area filled with bg.
func FitTo(src image.Image, width, height int, bg color.RGBA) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(dst, dst.Bounds(), &image.Uniform{C: bg}, image.Point{}, draw.Src)

	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	if sw == width && sh == height {
		draw.Draw(dst, dst.Bounds(), src, sb.Min, draw.Src)
		return dst
	}

	scale := float64(width) / float64(sw)
	if s := float64(height) / float64(sh); s < scale {
		scale = s
	}
	dw := int(float64(sw) * scale)
	dh := int(float64(sh) * scale)
	ox := (width - dw) / 2
	oy := (height - dh) / 2
	target := image.Rect(ox, oy, ox+dw, oy+dh)

	xdraw.CatmullRom.Scale(dst, target, src, sb, xdraw.Over, nil)
	return dst
}

// rotate returns src rotated by degrees (0/90/180/270 only). Rotations
// other than 0 produce a new image; 0 returns src unchanged.
func rotate(src image.Image, degrees int) (image.Image, error) {
	switch degrees {
	case 0:
		return src, nil
	case 90, 180, 270:
		return rotateExact(src, degrees), nil
	default:
		return nil, fmt.Errorf("rotate must be one of 0/90/180/270, got %d", degrees)
	}
}

// rotateExact rotates src by exactly 90, 180, or 270 degrees clockwise.
// No interpolation — pixels are remapped 1:1.
func rotateExact(src image.Image, degrees int) *image.RGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()

	var dst *image.RGBA
	switch degrees {
	case 90:
		dst = image.NewRGBA(image.Rect(0, 0, h, w))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				dst.Set(h-1-y, x, src.At(b.Min.X+x, b.Min.Y+y))
			}
		}
	case 180:
		dst = image.NewRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				dst.Set(w-1-x, h-1-y, src.At(b.Min.X+x, b.Min.Y+y))
			}
		}
	case 270:
		dst = image.NewRGBA(image.Rect(0, 0, h, w))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				dst.Set(y, w-1-x, src.At(b.Min.X+x, b.Min.Y+y))
			}
		}
	}
	return dst
}
