package aperio

import (
	"image"
	"image/color"
	"image/draw"

	xdraw "golang.org/x/image/draw"
)

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
