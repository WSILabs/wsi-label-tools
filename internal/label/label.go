package label

import (
	"bytes"
	"image"
	"image/color"

	"github.com/hhrutter/lzw"
)

// Target dimensions for Aperio SVS labels.
const (
	AperioLabelWidth   = 1200
	AperioLabelHeight  = 848
	AperioRowsPerStrip = 2
)

// AperioLabelBG is the default letterbox fill color.
var AperioLabelBG = color.RGBA{245, 245, 230, 255}

// rgbStripBytes returns tightly packed 8-bit RGB bytes for the given image,
// stride = width*3, no alpha.
func rgbStripBytes(img image.Image) []byte {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	out := make([]byte, w*h*3)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, bl, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			out[(y*w+x)*3+0] = uint8(r >> 8)
			out[(y*w+x)*3+1] = uint8(g >> 8)
			out[(y*w+x)*3+2] = uint8(bl >> 8)
		}
	}
	return out
}

// applyPredictor2 performs TIFF horizontal differencing on RGB bytes.
// First pixel of each row is unchanged; subsequent pixels store the
// difference from the previous pixel, per channel, mod 256.
func applyPredictor2(rgb []byte, w, h int) []byte {
	out := make([]byte, len(rgb))
	stride := w * 3
	for y := 0; y < h; y++ {
		row := rgb[y*stride : (y+1)*stride]
		orow := out[y*stride : (y+1)*stride]
		copy(orow[:3], row[:3])
		for x := 1; x < w; x++ {
			for c := 0; c < 3; c++ {
				orow[x*3+c] = row[x*3+c] - row[(x-1)*3+c]
			}
		}
	}
	return out
}

// encodeLZW compresses data with TIFF 6.0 flavored LZW.
//
// Uses github.com/hhrutter/lzw with oneOff=true (the "early-change" variant
// libtiff and Pillow produce). Go stdlib compress/lzw in MSB mode is close
// but not byte-for-byte compatible; libtiff decodes a stdlib stream with a
// "Using code not yet in table" warning that propagates to strict callers
// like TIFFRGBAImageGet.
func encodeLZW(data []byte) []byte {
	var buf bytes.Buffer
	w := lzw.NewWriter(&buf, true)
	_, _ = w.Write(data)
	_ = w.Close()
	return buf.Bytes()
}

// EncodeLabelStrips splits an Aperio-dimensioned RGB image into
// AperioRowsPerStrip-row strips, applies predictor 2, and LZW-compresses
// each strip. Returns one compressed blob per strip.
func EncodeLabelStrips(img image.Image) [][]byte {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	raw := rgbStripBytes(img)
	stride := w * 3

	numStrips := (h + AperioRowsPerStrip - 1) / AperioRowsPerStrip
	strips := make([][]byte, numStrips)
	for s := 0; s < numStrips; s++ {
		y0 := s * AperioRowsPerStrip
		y1 := y0 + AperioRowsPerStrip
		if y1 > h {
			y1 = h
		}
		stripRaw := raw[y0*stride : y1*stride]
		stripPre := applyPredictor2(stripRaw, w, y1-y0)
		strips[s] = encodeLZW(stripPre)
	}
	return strips
}
