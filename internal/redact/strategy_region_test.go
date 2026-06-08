package redact

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"

	xtiff "golang.org/x/image/tiff"

	"github.com/cornish/wsi-label-tools/internal/tifflow"
)

// parseTempTIFF writes raw TIFF bytes to a temp file and parses it.
func parseTempTIFF(t *testing.T, raw []byte) (*tifflow.File, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "host.tif")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write temp tiff: %v", err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open temp tiff: %v", err)
	}
	defer f.Close()
	file, err := tifflow.Parse(f)
	if err != nil {
		t.Fatalf("parse temp tiff: %v", err)
	}
	return file, path
}

// TestDecodeHost_Uncompressed builds an uncompressed stripped TIFF via x/image
// and checks decodeHost reproduces dims and a sampled pixel.
func TestDecodeHost_Uncompressed(t *testing.T) {
	const w, h = 16, 12
	src := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			src.Set(x, y, color.RGBA{uint8(x * 10), uint8(y * 10), 100, 255})
		}
	}
	var buf bytes.Buffer
	if err := xtiff.Encode(&buf, src, &xtiff.Options{Compression: xtiff.Uncompressed}); err != nil {
		t.Fatalf("encode uncompressed tiff: %v", err)
	}
	file, path := parseTempTIFF(t, buf.Bytes())

	hi, err := readHostInfo(path, file, 0)
	if err != nil {
		t.Fatalf("readHostInfo: %v", err)
	}
	if hi.w != w || hi.h != h {
		t.Fatalf("dims: got %dx%d want %dx%d", hi.w, hi.h, w, h)
	}
	if hi.comp != 1 {
		t.Fatalf("comp: got %d want 1", hi.comp)
	}

	dst, err := decodeHost(path, file, hi)
	if err != nil {
		t.Fatalf("decodeHost: %v", err)
	}
	if dst.Bounds().Dx() != w || dst.Bounds().Dy() != h {
		t.Fatalf("decoded dims: got %v", dst.Bounds())
	}
	// Sample a pixel (uncompressed: exact).
	got := dst.RGBAAt(5, 7)
	want := color.RGBA{50, 70, 100, 255}
	if got != want {
		t.Fatalf("pixel (5,7): got %v want %v", got, want)
	}
}

// TestDecodeHost_BaselineJPEG hand-builds a single-strip baseline-JPEG classic
// TIFF (compression 7, no JPEGTables) and checks decodeHost dims + an approximate
// sampled pixel.
func TestDecodeHost_BaselineJPEG(t *testing.T) {
	const w, h = 32, 24
	src := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			src.Set(x, y, color.RGBA{200, 60, 30, 255})
		}
	}
	var jbuf bytes.Buffer
	if err := jpeg.Encode(&jbuf, src, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	raw := buildSingleStripJPEGTIFF(w, h, jbuf.Bytes())
	file, path := parseTempTIFF(t, raw)

	hi, err := readHostInfo(path, file, 0)
	if err != nil {
		t.Fatalf("readHostInfo: %v", err)
	}
	if hi.w != w || hi.h != h {
		t.Fatalf("dims: got %dx%d want %dx%d", hi.w, hi.h, w, h)
	}
	if hi.comp != 7 {
		t.Fatalf("comp: got %d want 7", hi.comp)
	}
	if hi.jpegTables != nil {
		t.Fatalf("expected no JPEGTables")
	}

	dst, err := decodeHost(path, file, hi)
	if err != nil {
		t.Fatalf("decodeHost: %v", err)
	}
	if dst.Bounds().Dx() != w || dst.Bounds().Dy() != h {
		t.Fatalf("decoded dims: got %v", dst.Bounds())
	}
	got := dst.RGBAAt(16, 12)
	// JPEG is lossy; check the dominant red channel is in range.
	if got.R < 180 || got.G > 120 || got.B > 90 {
		t.Fatalf("pixel (16,12): got %v, not close to solid red", got)
	}
}

// buildSingleStripJPEGTIFF hand-builds a minimal little-endian classic TIFF with
// a single strip whose data is the supplied baseline-JPEG stream (Compression=7).
func buildSingleStripJPEGTIFF(w, h int, jpegData []byte) []byte {
	const (
		tImageWidth      = 256
		tImageLength     = 257
		tBitsPerSample   = 258
		tCompression     = 259
		tPhotometric     = 262
		tStripOffsets    = 273
		tSamplesPerPixel = 277
		tRowsPerStrip    = 278
		tStripByteCounts = 279
	)
	const (
		typeShort = 3
		typeLong  = 4
	)
	order := binary.LittleEndian

	// Layout: header(8) + IFD(count + N*12 + next) + BitsPerSample blob + strip.
	type entry struct {
		tag, typ uint16
		count    uint32
		val      uint32 // inline value or offset
	}

	// We'll place BitsPerSample (3 SHORTs) out-of-line and the strip after it.
	headerSize := 8
	numEntries := 9
	ifdSize := 2 + numEntries*12 + 4
	bpsOffset := headerSize + ifdSize
	bpsSize := 6 // 3 SHORT
	if bpsSize%2 != 0 {
		bpsSize++
	}
	stripOffset := bpsOffset + bpsSize

	entries := []entry{
		{tImageWidth, typeLong, 1, uint32(w)},
		{tImageLength, typeLong, 1, uint32(h)},
		{tBitsPerSample, typeShort, 3, uint32(bpsOffset)},
		{tCompression, typeShort, 1, 7},
		{tPhotometric, typeShort, 1, 6}, // YCbCr (JPEG)
		{tStripOffsets, typeLong, 1, uint32(stripOffset)},
		{tSamplesPerPixel, typeShort, 1, 3},
		{tRowsPerStrip, typeLong, 1, uint32(h)},
		{tStripByteCounts, typeLong, 1, uint32(len(jpegData))},
	}

	buf := &bytes.Buffer{}
	// Header.
	buf.WriteString("II")
	binary.Write(buf, order, uint16(42))
	binary.Write(buf, order, uint32(headerSize)) // first IFD offset

	// IFD.
	binary.Write(buf, order, uint16(numEntries))
	for _, e := range entries {
		binary.Write(buf, order, e.tag)
		binary.Write(buf, order, e.typ)
		binary.Write(buf, order, e.count)
		if e.typ == typeShort && e.count == 1 {
			// inline SHORT: low 2 bytes carry value, rest zero.
			binary.Write(buf, order, uint16(e.val))
			binary.Write(buf, order, uint16(0))
		} else {
			binary.Write(buf, order, e.val)
		}
	}
	binary.Write(buf, order, uint32(0)) // next IFD

	// BitsPerSample blob: 8,8,8.
	binary.Write(buf, order, uint16(8))
	binary.Write(buf, order, uint16(8))
	binary.Write(buf, order, uint16(8))
	if bpsSize == 6 {
		// no padding needed (6 even)
	}

	// Strip data.
	buf.Write(jpegData)
	return buf.Bytes()
}
