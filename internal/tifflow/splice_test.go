package tifflow

import (
	"bytes"
	"encoding/binary"
	"image"
	"os"
	"path/filepath"
	"testing"

	xtiff "golang.org/x/image/tiff"
)

// buildTinyTIFF creates a tiny classic-TIFF file with N strip-based IFDs, each
// with one trivial strip (4 bytes). Returns the path.
func buildTinyTIFF(t *testing.T, n int) string {
	t.Helper()
	order := binary.LittleEndian
	var buf bytes.Buffer
	buf.Write([]byte{'I', 'I', 42, 0})
	// First IFD offset filled at end (we write sequentially).
	_ = binary.Write(&buf, order, uint32(8))

	// For each IFD we emit: IFD record (entry count + 3 entries + next),
	// followed by strip data.
	// Entries per IFD:
	//   ImageWidth=1 (LONG inline)
	//   StripOffsets -> offset just after IFD record
	//   StripByteCounts=4 (LONG inline)
	// IFD size: 2 + 3*12 + 4 = 42 bytes. Strip = 4 bytes. Total per IFD = 46 bytes.
	const ifdSize = 42
	const stripSize = 4
	const perIFD = ifdSize + stripSize

	for i := 0; i < n; i++ {
		base := 8 + i*perIFD
		// Entry count
		_ = binary.Write(&buf, order, uint16(3))
		// ImageWidth: tag=256 LONG count=1 value=1
		_ = binary.Write(&buf, order, uint16(256))
		_ = binary.Write(&buf, order, uint16(4))
		_ = binary.Write(&buf, order, uint32(1))
		_ = binary.Write(&buf, order, uint32(1))
		// StripOffsets: tag=273 LONG count=1 value=base+ifdSize
		_ = binary.Write(&buf, order, uint16(273))
		_ = binary.Write(&buf, order, uint16(4))
		_ = binary.Write(&buf, order, uint32(1))
		_ = binary.Write(&buf, order, uint32(uint32(base+ifdSize)))
		// StripByteCounts: tag=279 LONG count=1 value=4
		_ = binary.Write(&buf, order, uint16(279))
		_ = binary.Write(&buf, order, uint16(4))
		_ = binary.Write(&buf, order, uint32(1))
		_ = binary.Write(&buf, order, uint32(stripSize))
		// next-IFD
		nextOff := uint32(0)
		if i < n-1 {
			nextOff = uint32(base + perIFD)
		}
		_ = binary.Write(&buf, order, nextOff)
		// Strip data
		buf.Write([]byte{byte(i), byte(i), byte(i), byte(i)})
	}

	dir := t.TempDir()
	p := filepath.Join(dir, "tiny.tif")
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return p
}

func TestSplice_RemoveMiddle(t *testing.T) {
	in := buildTinyTIFF(t, 3)
	out := filepath.Join(t.TempDir(), "out.tif")
	f, err := os.Open(in)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	file, err := Parse(f)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	f.Close()

	if err := Splice(SpliceParams{
		InPath: in, OutPath: out, File: file,
		Mode: SpliceRemove, TargetIdx: 1,
	}); err != nil {
		t.Fatalf("Splice: %v", err)
	}

	// Reopen and verify: 2 IFDs remain, strips 0 and 2.
	fo, err := os.Open(out)
	if err != nil {
		t.Fatalf("open out: %v", err)
	}
	defer fo.Close()
	outFile, err := Parse(fo)
	if err != nil {
		t.Fatalf("parse out: %v", err)
	}
	if len(outFile.IFDs) != 2 {
		t.Errorf("IFDs = %d, want 2", len(outFile.IFDs))
	}
}

func TestSplice_PrefixByteIdentical(t *testing.T) {
	in := buildTinyTIFF(t, 3)
	out := filepath.Join(t.TempDir(), "out.tif")
	f, _ := os.Open(in)
	file, err := Parse(f)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	f.Close()

	// Remove IFD 2 (last). Cutoff is at the start of IFD 2.
	if err := Splice(SpliceParams{
		InPath: in, OutPath: out, File: file,
		Mode: SpliceRemove, TargetIdx: 2,
	}); err != nil {
		t.Fatalf("Splice: %v", err)
	}

	// Prefix bytes [0, cutoff) must match input. Cutoff = IFD 2 offset.
	cutoff := file.IFDs[2].Offset
	inBytes, _ := os.ReadFile(in)
	outBytes, _ := os.ReadFile(out)
	// Allow one next-IFD pointer in prefix to differ (IFD 1's next).
	// Zero it out before compare.
	iNext := file.IFDs[1].NextPointerOffset
	if iNext+4 <= cutoff {
		inCopy := append([]byte(nil), inBytes[:cutoff]...)
		outCopy := append([]byte(nil), outBytes[:cutoff]...)
		for i := uint64(0); i < 4; i++ {
			inCopy[iNext+i] = 0
			outCopy[iNext+i] = 0
		}
		if !bytes.Equal(inCopy, outCopy) {
			t.Errorf("prefix (minus next-IFD pointer) differs at byte %d", firstDiff(inCopy, outCopy))
		}
	}
}

func TestSpliceReplaceStripData(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 16, 8))
	for i := range src.Pix {
		src.Pix[i] = byte(i)
	}
	var buf bytes.Buffer
	if err := xtiff.Encode(&buf, src, &xtiff.Options{Compression: xtiff.Uncompressed}); err != nil {
		t.Fatalf("encode: %v", err)
	}
	dir := t.TempDir()
	inPath := filepath.Join(dir, "in.tiff")
	if err := os.WriteFile(inPath, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(inPath)
	if err != nil {
		t.Fatal(err)
	}
	file, err := Parse(f)
	f.Close()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// One payload per strip the writer produced.
	offsIn, _ := file.IFDs[0].UintArray(TagStripOffsets, file.Header.ByteOrder)
	newStrips := make([][]byte, len(offsIn))
	for i := range newStrips {
		newStrips[i] = []byte("REDACTED-STRIP-DATA-" + string(rune('A'+i)))
	}
	outPath := filepath.Join(dir, "out.tiff")
	if err := Splice(SpliceParams{
		InPath: inPath, OutPath: outPath, File: file,
		Mode: SpliceReplaceStripData, TargetIdx: 0, StripData: newStrips,
	}); err != nil {
		t.Fatalf("Splice: %v", err)
	}
	g, err := os.Open(outPath)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	out, err := Parse(g)
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	w, _ := out.IFDs[0].UintValue(TagImageWidth, out.Header.ByteOrder)
	h, _ := out.IFDs[0].UintValue(TagImageLength, out.Header.ByteOrder)
	if w != 16 || h != 8 {
		t.Fatalf("dims changed: %dx%d", w, h)
	}
	offs, _ := out.IFDs[0].UintArray(TagStripOffsets, out.Header.ByteOrder)
	cnts, _ := out.IFDs[0].UintArray(TagStripByteCounts, out.Header.ByteOrder)
	if len(offs) != len(newStrips) || len(cnts) != len(newStrips) {
		t.Fatalf("strip table length changed: offs=%d cnts=%d want %d", len(offs), len(cnts), len(newStrips))
	}
	for i := range newStrips {
		if cnts[i] != uint64(len(newStrips[i])) {
			t.Fatalf("strip %d count = %d, want %d", i, cnts[i], len(newStrips[i]))
		}
		got := make([]byte, cnts[i])
		if _, err := g.ReadAt(got, int64(offs[i])); err != nil {
			t.Fatal(err)
		}
		if string(got) != string(newStrips[i]) {
			t.Fatalf("strip %d data = %q, want %q", i, got, newStrips[i])
		}
	}
}

// buildShortByteCountsTIFF hand-builds a minimal classic little-endian TIFF
// whose StripByteCounts entry is TIFF type SHORT (3, 2 bytes) with 2 strips.
// This exercises the override-path type-promotion fix: before the fix, the
// override path wrote a LONG-width value blob while the entry header still
// declared SHORT, corrupting the file.
//
// Layout:
//
//	[0]   header (8 bytes): II, 42, firstIFD=8
//	[8]   IFD: count(2) + 8 entries*12 + next(4) = 102 bytes -> ends at 110
//	[110] StripOffsets value blob (LONG*2 = 8 bytes, out-of-line)
//	[118] strip 0 payload (4 bytes)
//	[122] strip 1 payload (4 bytes)
func buildShortByteCountsTIFF(t *testing.T, dir string) (path string, strip0, strip1 []byte) {
	t.Helper()
	order := binary.LittleEndian

	strip0 = []byte{0x11, 0x22, 0x33, 0x44}
	strip1 = []byte{0x55, 0x66, 0x77, 0x88}

	const ifdStart = 8
	const ifdSize = 2 + 8*12 + 4                // = 102
	const stripOffsetsBlob = ifdStart + ifdSize // 110
	const strip0Off = stripOffsetsBlob + 8      // 118
	const strip1Off = strip0Off + 4             // 122

	var buf bytes.Buffer
	buf.Write([]byte{'I', 'I', 42, 0})
	_ = binary.Write(&buf, order, uint32(ifdStart))

	// Entry count.
	_ = binary.Write(&buf, order, uint16(8))

	writeEntry := func(tag uint16, typ uint16, count uint32, inlineVal uint32) {
		_ = binary.Write(&buf, order, tag)
		_ = binary.Write(&buf, order, typ)
		_ = binary.Write(&buf, order, count)
		_ = binary.Write(&buf, order, inlineVal)
	}

	// ImageWidth=2 (SHORT inline)
	writeEntry(256, 3, 1, 2)
	// ImageLength=1 (SHORT inline)
	writeEntry(257, 3, 1, 1)
	// BitsPerSample=8 (SHORT inline)
	writeEntry(258, 3, 1, 8)
	// Compression=1 (none) (SHORT inline)
	writeEntry(259, 3, 1, 1)
	// Photometric=1 (BlackIsZero) (SHORT inline)
	writeEntry(262, 3, 1, 1)
	// StripOffsets: LONG count=2, value -> out-of-line blob at stripOffsetsBlob
	writeEntry(273, 4, 2, stripOffsetsBlob)
	// StripByteCounts: *** SHORT count=2 *** inline (2*2=4 bytes fits the field):
	// strip lengths 4 and 4 packed little-endian into the 4-byte value field.
	{
		_ = binary.Write(&buf, order, uint16(279))
		_ = binary.Write(&buf, order, uint16(3)) // SHORT
		_ = binary.Write(&buf, order, uint32(2)) // count
		_ = binary.Write(&buf, order, uint16(len(strip0)))
		_ = binary.Write(&buf, order, uint16(len(strip1)))
	}
	// RowsPerStrip=1 (SHORT inline)
	writeEntry(278, 3, 1, 1)

	// next-IFD = 0
	_ = binary.Write(&buf, order, uint32(0))

	// StripOffsets value blob (LONG*2).
	_ = binary.Write(&buf, order, uint32(strip0Off))
	_ = binary.Write(&buf, order, uint32(strip1Off))

	// Strip payloads.
	buf.Write(strip0)
	buf.Write(strip1)

	path = filepath.Join(dir, "short_bc.tiff")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path, strip0, strip1
}

// TestSpliceReplaceStripData_ShortByteCounts is a regression test for the
// override-path SHORT-type corruption bug. It builds a classic TIFF with
// SHORT-typed StripByteCounts and 2 strips, replaces the strips with longer
// payloads via SpliceReplaceStripData, and asserts the output decodes
// correctly (counts/offsets/bytes), which only holds once the offsets/counts
// entries are promoted to LONG so the declared type matches the value width.
func TestSpliceReplaceStripData_ShortByteCounts(t *testing.T) {
	dir := t.TempDir()
	inPath, _, _ := buildShortByteCountsTIFF(t, dir)

	f, err := os.Open(inPath)
	if err != nil {
		t.Fatal(err)
	}
	file, err := Parse(f)
	f.Close()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	// Sanity: the input really uses SHORT StripByteCounts with 2 strips.
	bcEntry := findEntry(file.IFDs[0], TagStripByteCounts)
	if bcEntry == nil || bcEntry.Type != TypeShort {
		t.Fatalf("test setup: StripByteCounts type = %v, want SHORT(3)", bcEntry.Type)
	}
	offsIn, _ := file.IFDs[0].UintArray(TagStripOffsets, file.Header.ByteOrder)
	if len(offsIn) != 2 {
		t.Fatalf("test setup: want 2 strips, got %d", len(offsIn))
	}

	// New payloads of differing (and larger) lengths.
	newStrips := [][]byte{
		[]byte("NEW-STRIP-ZERO-0000"),
		[]byte("NEW-STRIP-ONE"),
	}
	outPath := filepath.Join(dir, "out_short_bc.tiff")
	if err := Splice(SpliceParams{
		InPath: inPath, OutPath: outPath, File: file,
		Mode: SpliceReplaceStripData, TargetIdx: 0, StripData: newStrips,
	}); err != nil {
		t.Fatalf("Splice: %v", err)
	}

	g, err := os.Open(outPath)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	out, err := Parse(g)
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}

	// The offsets/counts entries must now be promoted to LONG (classic).
	bcOut := findEntry(out.IFDs[0], TagStripByteCounts)
	if bcOut == nil || bcOut.Type != TypeLong {
		t.Fatalf("StripByteCounts type after splice = %v, want LONG(4)", bcOut.Type)
	}
	soOut := findEntry(out.IFDs[0], TagStripOffsets)
	if soOut == nil || soOut.Type != TypeLong {
		t.Fatalf("StripOffsets type after splice = %v, want LONG(4)", soOut.Type)
	}

	offs, _ := out.IFDs[0].UintArray(TagStripOffsets, out.Header.ByteOrder)
	cnts, _ := out.IFDs[0].UintArray(TagStripByteCounts, out.Header.ByteOrder)
	if len(offs) != len(newStrips) || len(cnts) != len(newStrips) {
		t.Fatalf("strip table length: offs=%d cnts=%d want %d", len(offs), len(cnts), len(newStrips))
	}
	for i := range newStrips {
		if cnts[i] != uint64(len(newStrips[i])) {
			t.Fatalf("strip %d count = %d, want %d", i, cnts[i], len(newStrips[i]))
		}
		got := make([]byte, cnts[i])
		if _, err := g.ReadAt(got, int64(offs[i])); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, newStrips[i]) {
			t.Fatalf("strip %d data = %q, want %q", i, got, newStrips[i])
		}
	}
}

func firstDiff(a, b []byte) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return -1
}
