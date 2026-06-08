package redact

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"os"

	xdraw "golang.org/x/image/draw"

	"github.com/cornish/wsi-label-tools/internal/tifflow"
)

// parchment is the neutral fill for blanked label regions.
var parchment = color.RGBA{0xF5, 0xF5, 0xE6, 0xFF}

// hostJPEG reads and decodes the host IFD's single JPEG strip.
func hostJPEG(inPath string, file *tifflow.File, hostIdx int) (image.Image, error) {
	ifd := file.IFDs[hostIdx]
	offs, ok := ifd.UintArray(tifflow.TagStripOffsets, file.Header.ByteOrder)
	cnts, ok2 := ifd.UintArray(tifflow.TagStripByteCounts, file.Header.ByteOrder)
	if !ok || !ok2 || len(offs) != 1 || len(cnts) != 1 {
		return nil, fmt.Errorf("redact: region host IFD is not a single strip")
	}
	f, err := os.Open(inPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw := make([]byte, cnts[0])
	if _, err := f.ReadAt(raw, int64(offs[0])); err != nil {
		return nil, fmt.Errorf("redact: read host strip: %w", err)
	}
	img, err := jpeg.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("redact: cannot decode host image (region redaction needs baseline JPEG): %w", err)
	}
	return img, nil
}

// writeRegion re-encodes the modified host image as JPEG and splices it into
// outPath, preserving the host IFD's tags.
func writeRegion(inPath, outPath string, file *tifflow.File, hostIdx int, modified image.Image, fsync bool) error {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, modified, &jpeg.Options{Quality: 90}); err != nil {
		return fmt.Errorf("redact: encode host: %w", err)
	}
	return tifflow.Splice(tifflow.SpliceParams{
		InPath: inPath, OutPath: outPath, File: file,
		Mode: tifflow.SpliceReplaceStripData, TargetIdx: hostIdx,
		StripData: [][]byte{buf.Bytes()}, Fsync: fsync,
	})
}

// toRGBA returns a mutable RGBA copy of img.
func toRGBA(img image.Image) *image.RGBA {
	b := img.Bounds()
	dst := image.NewRGBA(b)
	draw.Draw(dst, b, img, b.Min, draw.Src)
	return dst
}

// regionStrip blanks the target region of the host image with a neutral fill.
func regionStrip(inPath, outPath string, file *tifflow.File, t *Target, fsync bool) error {
	img, err := hostJPEG(inPath, file, t.HostIFDIndex)
	if err != nil {
		return err
	}
	dst := toRGBA(img)
	region := t.Region.Add(img.Bounds().Min)
	draw.Draw(dst, region, image.NewUniform(parchment), image.Point{}, draw.Src)
	return writeRegion(inPath, outPath, file, t.HostIFDIndex, dst, fsync)
}

// regionReplace composites the supplied label image into the target region,
// scaling it to fill the region rectangle exactly. NOTE: for EmbeddedRegion
// targets the geometry knobs (Rotate, Resize, BG, Force) are no-ops — the
// image is stretched to the region's fixed footprint within the host image,
// not letterboxed/rotated. Those options apply only to the Splice path.
func regionReplace(inPath, outPath string, file *tifflow.File, c Classifier, t *Target, opts ReplaceOptions) (*Result, error) {
	img, err := hostJPEG(inPath, file, t.HostIFDIndex)
	if err != nil {
		return nil, err
	}
	dst := toRGBA(img)
	region := t.Region.Add(img.Bounds().Min)
	xdraw.CatmullRom.Scale(dst, region, opts.Image, opts.Image.Bounds(), xdraw.Over, nil)
	if err := writeRegion(inPath, outPath, file, t.HostIFDIndex, dst, opts.Fsync); err != nil {
		return nil, err
	}
	return &Result{}, nil
}
