package redact_test

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cornish/wsi-label-tools/internal/redact"
	_ "github.com/cornish/wsi-label-tools/internal/redact/format"
	"github.com/cornish/wsi-label-tools/internal/tifflow"
)

func svsSample(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join("..", "..", "sample_files", "svs", name)
}

// newTestLabel returns a small solid image to use as a replacement label.
func newTestLabel() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 1200, 848))
	for y := 0; y < 848; y++ {
		for x := 0; x < 1200; x++ {
			img.Set(x, y, color.RGBA{200, 100, 50, 255})
		}
	}
	return img
}

func TestReplace_CMU1SmallRegion(t *testing.T) {
	src := svsSample(t, "CMU-1-Small-Region.svs")
	if _, err := os.Stat(src); err != nil {
		t.Skipf("sample not present: %v", err)
	}
	out := filepath.Join(t.TempDir(), "relabeled.svs")
	res, err := redact.Replace(src, out, redact.ReplaceOptions{
		Image: newTestLabel(), Resize: redact.ResizeNone,
		TargetWidth: 1200, TargetHeight: 848, Fsync: false,
	})
	if err != nil {
		t.Fatalf("Replace: %v", err)
	}
	if res.Added {
		t.Fatalf("expected replace, not add")
	}
	w, h, err := redact.LabelDims(out)
	if err != nil {
		t.Fatalf("LabelDims(out): %v", err)
	}
	if w != 1200 || h != 848 {
		t.Fatalf("label dims: got %dx%d want 1200x848", w, h)
	}
}

func TestStrip_CMU1SmallRegion(t *testing.T) {
	src := svsSample(t, "CMU-1-Small-Region.svs")
	if _, err := os.Stat(src); err != nil {
		t.Skipf("sample not present: %v", err)
	}
	out := filepath.Join(t.TempDir(), "stripped.svs")
	if err := redact.Strip(src, out, false); err != nil {
		t.Fatalf("Strip: %v", err)
	}
	if _, _, err := redact.LabelDims(out); err == nil {
		t.Fatalf("expected no label after strip")
	}
}

// TestReplace_AllSamples runs replace against every sample SVS and verifies
// the prefix-before-label is byte-identical to the input (the core invariant).
func TestReplace_AllSamples(t *testing.T) {
	dir := filepath.Join("..", "..", "sample_files", "svs")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("no sample dir: %v", err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".svs") {
			continue
		}
		t.Run(e.Name(), func(t *testing.T) {
			in := filepath.Join(dir, e.Name())
			out := filepath.Join(t.TempDir(), "out.svs")
			if _, err := redact.Replace(in, out, redact.ReplaceOptions{
				Image: newTestLabel(), Resize: redact.ResizeStretch,
			}); err != nil {
				t.Fatalf("Replace %s: %v", e.Name(), err)
			}
			assertPrefixIdentical(t, in, out)
			if _, _, err := redact.LabelDims(out); err != nil {
				t.Fatalf("output %s has no label after replace: %v", e.Name(), err)
			}
		})
	}
}

func TestGenericTIFF_StripAndReplace(t *testing.T) {
	src := filepath.Join("..", "..", "sample_files", "generic-tiff", "CMU-1-Small-Region.stripped.tiff")
	if _, err := os.Stat(src); err != nil {
		t.Skipf("fixture absent: %v", err)
	}

	// strip: output must have no label.
	stripOut := filepath.Join(t.TempDir(), "stripped.tiff")
	if err := redact.Strip(src, stripOut, false); err != nil {
		t.Fatalf("Strip: %v", err)
	}
	if _, _, err := redact.LabelDims(stripOut); err == nil {
		t.Fatalf("expected no label after strip")
	}

	// replace: output must still classify a label.
	repOut := filepath.Join(t.TempDir(), "relabeled.tiff")
	if _, err := redact.Replace(src, repOut, redact.ReplaceOptions{
		Image: newTestLabel(), Resize: redact.ResizeStretch,
	}); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	if _, _, err := redact.LabelDims(repOut); err != nil {
		t.Fatalf("output has no label after replace: %v", err)
	}
	assertPrefixIdentical(t, src, repOut)
}

// assertPrefixIdentical verifies that the portion of the output file that was
// copied verbatim from the input is byte-identical. The splice preserves
// [0, cutoff) from the input, then patches the predecessor IFD's next-pointer
// field at predecessorNextPtrLoc to point to the new label IFD. We therefore
// check two sub-ranges:
//   - [0, predecessorNextPtrLoc) — untouched
//   - [predecessorNextPtrLoc+ptrSize, cutoff) — untouched bytes after the patch
func assertPrefixIdentical(t *testing.T, in, out string) {
	t.Helper()
	fIn, err := os.Open(in)
	if err != nil {
		t.Fatal(err)
	}
	defer fIn.Close()
	file, err := tifflow.Parse(fIn)
	if err != nil {
		t.Fatalf("parse in: %v", err)
	}
	// Find the label IFD index.
	labelIdx := -1
	for i, ifd := range file.IFDs {
		desc, _ := ifd.StringValue(tifflow.TagImageDescription)
		lines := strings.SplitN(desc, "\n", 3)
		if len(lines) >= 2 && strings.HasPrefix(lines[1], "label") {
			labelIdx = i
			break
		}
	}
	if labelIdx < 0 {
		return // no label in this sample; nothing to compare
	}
	// Use the same cutoff the splice uses.
	cutoff := file.Ranges.MinOffsetOfOwnersAtOrAfter(labelIdx)
	if cutoff == 0 || cutoff == ^uint64(0) {
		return
	}
	// Size of next-IFD pointer field.
	ptrSize := uint64(4)
	if file.Header.BigTIFF {
		ptrSize = 8
	}
	// Where the predecessor's next-pointer sits (Splice patches this).
	var patchAt uint64
	if labelIdx == 0 {
		if file.Header.BigTIFF {
			patchAt = 8
		} else {
			patchAt = 4
		}
	} else {
		patchAt = file.IFDs[labelIdx-1].NextPointerOffset
	}

	fOut, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer fOut.Close()

	readSeg := func(f *os.File, start, end uint64) []byte {
		if end <= start {
			return nil
		}
		buf := make([]byte, end-start)
		if _, err := f.ReadAt(buf, int64(start)); err != nil {
			t.Fatalf("ReadAt [%d,%d): %v", start, end, err)
		}
		return buf
	}

	// Segment before the patched pointer.
	if patchAt > 0 {
		inSeg := readSeg(fIn, 0, patchAt)
		outSeg := readSeg(fOut, 0, patchAt)
		if !bytes.Equal(inSeg, outSeg) {
			t.Fatalf("prefix [0,%d) differs between input and output", patchAt)
		}
	}
	// Segment after the patched pointer, up to cutoff.
	afterPatch := patchAt + ptrSize
	if afterPatch < cutoff {
		inSeg := readSeg(fIn, afterPatch, cutoff)
		outSeg := readSeg(fOut, afterPatch, cutoff)
		if !bytes.Equal(inSeg, outSeg) {
			t.Fatalf("prefix [%d,%d) (after patched pointer) differs", afterPatch, cutoff)
		}
	}
}

func TestNDPI_StripAndReplace(t *testing.T) {
	src := filepath.Join("..", "..", "sample_files", "ndpi", "CMU-1.ndpi")
	if _, err := os.Stat(src); err != nil {
		t.Skipf("fixture absent: %v", err)
	}

	// strip: blank the label region of the macro; output must re-open and the
	// label region (macro) must still resolve.
	stripOut := filepath.Join(t.TempDir(), "stripped.ndpi")
	if err := redact.Strip(src, stripOut, false); err != nil {
		t.Fatalf("Strip: %v", err)
	}
	if _, _, err := redact.LabelDims(stripOut); err != nil {
		t.Fatalf("LabelDims(stripped) failed — output not re-openable: %v", err)
	}
	assertNDPIPyramidIdentical(t, src, stripOut)
	assertMacroChanged(t, src, stripOut)

	// replace: composite a test label into the region.
	repOut := filepath.Join(t.TempDir(), "relabeled.ndpi")
	if _, err := redact.Replace(src, repOut, redact.ReplaceOptions{Image: newTestLabel()}); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	assertNDPIPyramidIdentical(t, src, repOut)
	assertMacroChanged(t, src, repOut)
}

// assertMacroChanged verifies the macro strip bytes differ (redaction occurred).
func assertMacroChanged(t *testing.T, in, out string) {
	t.Helper()
	readMacro := func(path string) []byte {
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		file, err := tifflow.Parse(f)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		for _, ifd := range file.IFDs {
			if mag, ok := ifd.FloatValue(65421, file.Header.ByteOrder); ok && mag == -1.0 {
				offs, _ := ifd.UintArray(tifflow.TagStripOffsets, file.Header.ByteOrder)
				cnts, _ := ifd.UintArray(tifflow.TagStripByteCounts, file.Header.ByteOrder)
				if len(offs) == 1 && len(cnts) == 1 {
					b := make([]byte, cnts[0])
					if _, err := f.ReadAt(b, int64(offs[0])); err != nil {
						t.Fatal(err)
					}
					return b
				}
			}
		}
		t.Fatal("macro strip not found")
		return nil
	}
	if bytes.Equal(readMacro(in), readMacro(out)) {
		t.Fatalf("macro strip unchanged — redaction did not modify the macro")
	}
}

// assertNDPIPyramidIdentical verifies the prefix up to the macro strip is
// byte-identical (the pyramid was not touched by the region edit).
func assertNDPIPyramidIdentical(t *testing.T, in, out string) {
	t.Helper()
	fIn, err := os.Open(in)
	if err != nil {
		t.Fatal(err)
	}
	defer fIn.Close()
	file, err := tifflow.Parse(fIn)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var cutoff uint64
	for _, ifd := range file.IFDs {
		mag, ok := ifd.FloatValue(65421, file.Header.ByteOrder)
		if ok && mag == -1.0 {
			continue // skip the macro itself
		}
		offs, o := ifd.UintArray(tifflow.TagStripOffsets, file.Header.ByteOrder)
		cnts, c := ifd.UintArray(tifflow.TagStripByteCounts, file.Header.ByteOrder)
		if o && c && len(offs) == 1 && len(cnts) == 1 {
			if end := offs[0] + cnts[0]; end > cutoff {
				cutoff = end
			}
		}
	}
	if cutoff == 0 {
		t.Skip("no pyramid strips located")
	}
	inB := make([]byte, cutoff)
	if _, err := fIn.ReadAt(inB, 0); err != nil {
		t.Fatal(err)
	}
	fOut, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer fOut.Close()
	outB := make([]byte, cutoff)
	if _, err := fOut.ReadAt(outB, 0); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(inB, outB) {
		t.Fatalf("pyramid prefix [0,%d) differs after NDPI region redaction", cutoff)
	}
}

// TestBIF_StripAndReplace is the end-to-end validation of the BIF Region path
// on real files. For each variant (skip-if-absent) it runs Strip then Replace
// and asserts: the output re-opens, the pyramid (IFD >= 2) tile/strip pixel
// data is byte-identical input vs output, and the overview's top-1/3 band
// actually changed. For OS-1 (abbreviated JPEG) it additionally asserts the
// output overview is self-contained (JPEGTables dropped, stdlib jpeg.Decode
// of the tile succeeds standalone).
func TestBIF_StripAndReplace(t *testing.T) {
	cases := []string{
		filepath.Join("..", "..", "sample_files", "bif", "Ventana-1.bif"),
		filepath.Join("..", "..", "..", "opentile-go", "sample_files", "bif", "OS-1.bif"),
	}
	for _, src := range cases {
		if _, err := os.Stat(src); err != nil {
			t.Logf("skip %s: %v", src, err)
			continue
		}
		t.Run(filepath.Base(src), func(t *testing.T) {
			// STRIP
			out := filepath.Join(t.TempDir(), "stripped.bif")
			if err := redact.Strip(src, out, false); err != nil {
				t.Fatalf("Strip: %v", err)
			}
			if _, _, err := redact.LabelDims(out); err != nil {
				t.Fatalf("output not re-openable: %v", err)
			}
			assertBIFPyramidIdentical(t, src, out)
			assertBIFOverviewTopChanged(t, src, out)
			if filepath.Base(src) == "OS-1.bif" {
				assertOverviewSelfContainedJPEG(t, out)
			}
			// REPLACE
			rep := filepath.Join(t.TempDir(), "relabeled.bif")
			if _, err := redact.Replace(src, rep, redact.ReplaceOptions{Image: newTestLabel()}); err != nil {
				t.Fatalf("Replace: %v", err)
			}
			assertBIFPyramidIdentical(t, src, rep)
			assertBIFOverviewTopChanged(t, src, rep)
		})
	}
}

// readArrayOOL returns the full uint64 array for tag, reading out-of-line from
// f when the value exceeds tifflow's in-memory cache threshold (so UintArray
// returns nothing). OS-1's huge TileOffsets/TileByteCounts arrays hit this.
func readArrayOOL(t *testing.T, f *os.File, ifd *tifflow.IFD, tag uint16, order binary.ByteOrder) []uint64 {
	t.Helper()
	if v, ok := ifd.UintArray(tag, order); ok {
		return v
	}
	for _, e := range ifd.Entries {
		if e.Tag != tag {
			continue
		}
		sz := e.Type.Size()
		if sz == 0 || e.DataSize == 0 {
			return nil
		}
		buf := make([]byte, e.DataSize)
		if e.Inline || len(e.InlineValue) == int(e.DataSize) {
			copy(buf, e.InlineValue)
		} else {
			if _, err := f.ReadAt(buf, int64(e.DataOffset)); err != nil {
				t.Fatalf("read array tag %d out-of-line: %v", tag, err)
			}
		}
		n := int(e.Count)
		out := make([]uint64, n)
		for i := 0; i < n; i++ {
			b := buf[i*sz : (i+1)*sz]
			switch e.Type {
			case tifflow.TypeByte, tifflow.TypeSByte, tifflow.TypeUndefined:
				out[i] = uint64(b[0])
			case tifflow.TypeShort, tifflow.TypeSShort:
				out[i] = uint64(order.Uint16(b))
			case tifflow.TypeLong, tifflow.TypeSLong:
				out[i] = uint64(order.Uint32(b))
			case tifflow.TypeLong8, tifflow.TypeSLong8, tifflow.TypeIFD8:
				out[i] = order.Uint64(b)
			default:
				t.Fatalf("unexpected array type %d for tag %d", e.Type, tag)
			}
		}
		return out
	}
	return nil
}

// bifUnitArrays returns the (offsets, counts) for an IFD's pixel data, using
// the tile arrays when present, else the strip arrays. Both are read out-of-line
// when too large to cache.
func bifUnitArrays(t *testing.T, f *os.File, file *tifflow.File, ifd *tifflow.IFD) (offs, cnts []uint64) {
	t.Helper()
	order := file.Header.ByteOrder
	hasTile := false
	for _, e := range ifd.Entries {
		if e.Tag == tifflow.TagTileOffsets {
			hasTile = true
		}
	}
	if hasTile {
		offs = readArrayOOL(t, f, ifd, tifflow.TagTileOffsets, order)
		cnts = readArrayOOL(t, f, ifd, tifflow.TagTileByteCounts, order)
	} else {
		offs = readArrayOOL(t, f, ifd, tifflow.TagStripOffsets, order)
		cnts = readArrayOOL(t, f, ifd, tifflow.TagStripByteCounts, order)
	}
	return offs, cnts
}

// assertBIFPyramidIdentical asserts that for every IFD index >= 2 the tile/strip
// pixel data is byte-identical between in and out. Offsets may relocate, so each
// IFD's units are read from its OWN parsed offsets. Compared per-unit to bound
// memory (OS-1's level-0 pyramid is multi-GB).
func assertBIFPyramidIdentical(t *testing.T, in, out string) {
	t.Helper()
	fIn, err := os.Open(in)
	if err != nil {
		t.Fatal(err)
	}
	defer fIn.Close()
	fOut, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer fOut.Close()

	parse := func(f *os.File) *tifflow.File {
		file, err := tifflow.Parse(f)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		return file
	}
	inFile := parse(fIn)
	outFile := parse(fOut)
	if len(inFile.IFDs) != len(outFile.IFDs) {
		t.Fatalf("IFD count changed: in=%d out=%d", len(inFile.IFDs), len(outFile.IFDs))
	}

	bufIn := make([]byte, 0, 1<<20)
	bufOut := make([]byte, 0, 1<<20)
	for idx := 2; idx < len(inFile.IFDs); idx++ {
		inOffs, inCnts := bifUnitArrays(t, fIn, inFile, inFile.IFDs[idx])
		outOffs, outCnts := bifUnitArrays(t, fOut, outFile, outFile.IFDs[idx])
		if len(inOffs) != len(outOffs) || len(inCnts) != len(outCnts) {
			t.Fatalf("IFD%d unit count changed: in=%d/%d out=%d/%d", idx, len(inOffs), len(inCnts), len(outOffs), len(outCnts))
		}
		for i := range inOffs {
			if inCnts[i] != outCnts[i] {
				t.Fatalf("IFD%d unit %d byte-count changed: in=%d out=%d", idx, i, inCnts[i], outCnts[i])
			}
			n := inCnts[i]
			if n == 0 {
				continue
			}
			if uint64(cap(bufIn)) < n {
				bufIn = make([]byte, n)
				bufOut = make([]byte, n)
			}
			bi := bufIn[:n]
			bo := bufOut[:n]
			if _, err := fIn.ReadAt(bi, int64(inOffs[i])); err != nil {
				t.Fatalf("read in IFD%d unit %d: %v", idx, i, err)
			}
			if _, err := fOut.ReadAt(bo, int64(outOffs[i])); err != nil {
				t.Fatalf("read out IFD%d unit %d: %v", idx, i, err)
			}
			if !bytes.Equal(bi, bo) {
				t.Fatalf("pyramid IFD%d unit %d differs (in offset=%d, out offset=%d, %d bytes) — region edit corrupted the pyramid", idx, i, inOffs[i], outOffs[i], n)
			}
		}
	}
}

// decodeBIFOverview decodes IFD0 (the overview) into an *image.RGBA, handling
// both uncompressed RGB strips (Ventana-1) and tiled/abbreviated JPEG (OS-1).
func decodeBIFOverview(t *testing.T, path string) *image.RGBA {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	file, err := tifflow.Parse(f)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	order := file.Header.ByteOrder
	ifd := file.IFDs[0]
	w64, _ := ifd.UintValue(tifflow.TagImageWidth, order)
	h64, _ := ifd.UintValue(tifflow.TagImageLength, order)
	w, h := int(w64), int(h64)
	comp, _ := ifd.UintValue(tifflow.TagCompression, order)
	spp := 3
	if v, ok := ifd.UintValue(tifflow.TagSamplesPerPixel, order); ok && v > 0 {
		spp = int(v)
	}
	offs, cnts := bifUnitArrays(t, f, file, ifd)

	switch comp {
	case 1: // uncompressed RGB strips concatenated row-major.
		stride := w * spp
		raw := make([]byte, 0, stride*h)
		for i := range offs {
			b := make([]byte, cnts[i])
			if _, err := f.ReadAt(b, int64(offs[i])); err != nil {
				t.Fatalf("read strip %d: %v", i, err)
			}
			raw = append(raw, b...)
		}
		if len(raw) < stride*h {
			t.Fatalf("uncompressed overview short: %d < %d", len(raw), stride*h)
		}
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			srcRow := raw[y*stride:]
			dstRow := dst.Pix[y*dst.Stride:]
			for x := 0; x < w; x++ {
				s := srcRow[x*spp:]
				d := dstRow[x*4:]
				d[0] = s[0]
				if spp >= 3 {
					d[1] = s[1]
					d[2] = s[2]
				} else {
					d[1] = s[0]
					d[2] = s[0]
				}
				d[3] = 0xFF
			}
		}
		return dst

	case 7: // JPEG (possibly abbreviated + tiled).
		var jpegTables []byte
		for _, e := range ifd.Entries {
			if e.Tag == tifflow.TagJPEGTables && e.DataSize > 0 {
				jpegTables = make([]byte, e.DataSize)
				if e.Inline || len(e.InlineValue) == int(e.DataSize) {
					copy(jpegTables, e.InlineValue)
				} else if _, err := f.ReadAt(jpegTables, int64(e.DataOffset)); err != nil {
					t.Fatalf("read JPEGTables: %v", err)
				}
			}
		}
		decodeUnit := func(i int) image.Image {
			body := make([]byte, cnts[i])
			if _, err := f.ReadAt(body, int64(offs[i])); err != nil {
				t.Fatalf("read JPEG unit %d: %v", i, err)
			}
			merged := mergeAbbrevJPEG(body, jpegTables)
			img, err := jpeg.Decode(bytes.NewReader(merged))
			if err != nil {
				t.Fatalf("decode JPEG unit %d: %v", i, err)
			}
			return img
		}
		if len(offs) == 1 {
			return toRGBATest(decodeUnit(0))
		}
		// Tiled.
		tw64, _ := ifd.UintValue(tifflow.TagTileWidth, order)
		th64, _ := ifd.UintValue(tifflow.TagTileLength, order)
		tw, th := int(tw64), int(th64)
		if tw <= 0 || th <= 0 {
			t.Fatalf("tiled JPEG overview missing tile dims")
		}
		across := (w + tw - 1) / tw
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		for i := range offs {
			if cnts[i] == 0 {
				continue
			}
			img := decodeUnit(i)
			x0 := (i % across) * tw
			y0 := (i / across) * th
			r := image.Rect(x0, y0, x0+tw, y0+th)
			draw.Draw(dst, r, img, img.Bounds().Min, draw.Src)
		}
		return dst

	default:
		t.Fatalf("unsupported overview compression %d", comp)
		return nil
	}
}

// mergeAbbrevJPEG builds a self-contained JPEG from an abbreviated body and a
// JPEGTables blob: SOI + table segments + abbreviated body. Mirrors the
// production mergeAbbrev so the test decodes exactly what the codec path does.
func mergeAbbrevJPEG(body, tables []byte) []byte {
	if len(tables) == 0 || len(body) < 2 {
		return body
	}
	tbl := tables
	if len(tbl) >= 2 && tbl[0] == 0xFF && tbl[1] == 0xD8 {
		tbl = tbl[2:]
	}
	if len(tbl) >= 2 && tbl[len(tbl)-2] == 0xFF && tbl[len(tbl)-1] == 0xD9 {
		tbl = tbl[:len(tbl)-2]
	}
	out := make([]byte, 0, len(body)+len(tbl))
	out = append(out, body[0:2]...)
	out = append(out, tbl...)
	out = append(out, body[2:]...)
	return out
}

// toRGBATest returns a mutable RGBA copy of img with origin at (0,0).
func toRGBATest(img image.Image) *image.RGBA {
	b := img.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), img, b.Min, draw.Src)
	return dst
}

// topBandRGB extracts the [0, H/3) RGB band of an overview as a flat byte slice.
func topBandRGB(img *image.RGBA) []byte {
	b := img.Bounds()
	w := b.Dx()
	band := b.Dy() / 3
	out := make([]byte, 0, w*band*3)
	for y := 0; y < band; y++ {
		row := img.Pix[y*img.Stride:]
		for x := 0; x < w; x++ {
			d := row[x*4:]
			out = append(out, d[0], d[1], d[2])
		}
	}
	return out
}

// assertBIFOverviewTopChanged decodes IFD0 in both files and asserts the top-1/3
// band of the overview actually changed (non-vacuous proof the redaction landed
// on the label region).
func assertBIFOverviewTopChanged(t *testing.T, in, out string) {
	t.Helper()
	inImg := decodeBIFOverview(t, in)
	outImg := decodeBIFOverview(t, out)
	if inImg.Bounds() != outImg.Bounds() {
		t.Fatalf("overview dims changed: in=%v out=%v", inImg.Bounds(), outImg.Bounds())
	}
	if bytes.Equal(topBandRGB(inImg), topBandRGB(outImg)) {
		t.Fatalf("overview top-1/3 band unchanged — redaction did not modify the label region")
	}
}

// assertOverviewSelfContainedJPEG asserts that out's IFD0 overview is a
// self-contained JPEG: the JPEGTables (347) tag is gone and stdlib jpeg.Decode
// of its single tile succeeds with no table merge.
func assertOverviewSelfContainedJPEG(t *testing.T, out string) {
	t.Helper()
	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	file, err := tifflow.Parse(f)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	ifd := file.IFDs[0]
	for _, e := range ifd.Entries {
		if e.Tag == tifflow.TagJPEGTables {
			t.Fatalf("output overview still has JPEGTables (347) — not self-contained")
		}
	}
	offs, cnts := bifUnitArrays(t, f, file, ifd)
	if len(offs) != 1 {
		t.Fatalf("expected single-tile overview, got %d units", len(offs))
	}
	body := make([]byte, cnts[0])
	if _, err := f.ReadAt(body, int64(offs[0])); err != nil {
		t.Fatalf("read overview tile: %v", err)
	}
	if _, err := jpeg.Decode(bytes.NewReader(body)); err != nil {
		t.Fatalf("stdlib jpeg.Decode of overview tile failed (not self-contained): %v", err)
	}
}
