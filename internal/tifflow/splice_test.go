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

func TestSpliceReplaceStripData_DropTags(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 16, 8))
	var buf bytes.Buffer
	if err := xtiff.Encode(&buf, src, &xtiff.Options{Compression: xtiff.Uncompressed}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	inPath := filepath.Join(dir, "in.tiff")
	os.WriteFile(inPath, buf.Bytes(), 0o644)
	f, _ := os.Open(inPath)
	file, err := Parse(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	var dropTag uint16 = TagPhotometric // present in baseline RGB TIFF
	found := false
	for _, e := range file.IFDs[0].Entries {
		if e.Tag == dropTag {
			found = true
		}
	}
	if !found {
		t.Skipf("writer did not emit tag %d; pick another present tag", dropTag)
	}
	offs, _ := file.IFDs[0].UintArray(TagStripOffsets, file.Header.ByteOrder)
	newStrips := make([][]byte, len(offs))
	for i := range newStrips {
		newStrips[i] = []byte("X")
	}
	outPath := filepath.Join(dir, "out.tiff")
	if err := Splice(SpliceParams{
		InPath: inPath, OutPath: outPath, File: file,
		Mode: SpliceReplaceStripData, TargetIdx: 0, StripData: newStrips,
		DropTags: []uint16{dropTag},
	}); err != nil {
		t.Fatalf("Splice: %v", err)
	}
	g, _ := os.Open(outPath)
	defer g.Close()
	out, err := Parse(g)
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	for _, e := range out.IFDs[0].Entries {
		if e.Tag == dropTag {
			t.Fatalf("tag %d should have been dropped", dropTag)
		}
	}
}

// buildLargeTailArrayTIFF hand-builds a classic little-endian TIFF with two
// IFDs. IFD 0 is a tiny 1-strip image (the splice target). IFD 1 is a tall
// 1xN image with RowsPerStrip=1, giving N strips so its StripOffsets and
// StripByteCounts LONG arrays each exceed 4096 bytes (N=1200 -> 4800 bytes),
// landing in the re-emitted tail beyond the 4096-byte parse cache.
//
// Returns the path and the byte payload used for each tail strip.
func buildLargeTailArrayTIFF(t *testing.T, dir string, nTailStrips int) (path string, tailStrip func(i int) []byte) {
	t.Helper()
	order := binary.LittleEndian

	tailStrip = func(i int) []byte {
		// 3-byte deterministic payload per strip.
		return []byte{byte(i), byte(i >> 8), byte(i * 7)}
	}
	const stripLen = 3

	// IFD 0: 4 entries (ImageWidth, ImageLength, StripOffsets, StripByteCounts).
	// IFD 1: 5 entries (ImageWidth, ImageLength, RowsPerStrip, StripOffsets,
	// StripByteCounts). Its StripOffsets/StripByteCounts are LONG arrays of
	// nTailStrips entries -> out-of-line.
	var buf bytes.Buffer
	buf.Write([]byte{'I', 'I', 42, 0})

	const ifd0Start = 8
	const ifd0Size = 2 + 4*12 + 4 // = 54
	const ifd0StripOff = ifd0Start + ifd0Size

	ifd1Start := uint32(ifd0StripOff + stripLen)
	const ifd1Size = 2 + 5*12 + 4 // = 66
	ifd1OffsetsBlob := ifd1Start + ifd1Size
	ifd1CountsBlob := ifd1OffsetsBlob + uint32(4*nTailStrips)
	ifd1StripsStart := ifd1CountsBlob + uint32(4*nTailStrips)

	_ = binary.Write(&buf, order, uint32(ifd0Start)) // first IFD offset

	writeEntry := func(tag, typ uint16, count, val uint32) {
		_ = binary.Write(&buf, order, tag)
		_ = binary.Write(&buf, order, typ)
		_ = binary.Write(&buf, order, count)
		_ = binary.Write(&buf, order, val)
	}

	// --- IFD 0 ---
	_ = binary.Write(&buf, order, uint16(4))
	writeEntry(256, 4, 1, 1)                    // ImageWidth=1 LONG
	writeEntry(257, 4, 1, 1)                    // ImageLength=1 LONG
	writeEntry(273, 4, 1, uint32(ifd0StripOff)) // StripOffsets
	writeEntry(279, 4, 1, stripLen)             // StripByteCounts
	_ = binary.Write(&buf, order, ifd1Start)    // next IFD -> IFD1
	// IFD0 strip payload.
	buf.Write([]byte{0xAB, 0xCD, 0xEF})

	// --- IFD 1 ---
	_ = binary.Write(&buf, order, uint16(5))
	writeEntry(256, 4, 1, 1)                                 // ImageWidth=1
	writeEntry(257, 4, 1, uint32(nTailStrips))               // ImageLength=N
	writeEntry(278, 4, 1, 1)                                 // RowsPerStrip=1
	writeEntry(273, 4, uint32(nTailStrips), ifd1OffsetsBlob) // StripOffsets array
	writeEntry(279, 4, uint32(nTailStrips), ifd1CountsBlob)  // StripByteCounts array
	_ = binary.Write(&buf, order, uint32(0))                 // next IFD = 0

	// IFD1 StripOffsets blob.
	for i := 0; i < nTailStrips; i++ {
		_ = binary.Write(&buf, order, ifd1StripsStart+uint32(i*stripLen))
	}
	// IFD1 StripByteCounts blob.
	for i := 0; i < nTailStrips; i++ {
		_ = binary.Write(&buf, order, uint32(stripLen))
	}
	// IFD1 strip payloads.
	for i := 0; i < nTailStrips; i++ {
		buf.Write(tailStrip(i))
	}

	path = filepath.Join(dir, "large_tail.tiff")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path, tailStrip
}

// TestSpliceReplaceStripData_LargeTailArrays is a regression test for the bug
// where reemitIFD read a re-emitted (tail) IFD's StripOffsets/StripByteCounts
// via the 4096-byte parse cache, so arrays larger than 4096 bytes came back
// empty and the emit loop failed with "no new offset". IFD 1 here has 1200
// strips -> 4800-byte LONG arrays. After splicing IFD 0's strip data, the tail
// IFD 1 must be intact and byte-identical.
func TestSpliceReplaceStripData_LargeTailArrays(t *testing.T) {
	dir := t.TempDir()
	const nTail = 1200
	inPath, tailStrip := buildLargeTailArrayTIFF(t, dir, nTail)

	f, err := os.Open(inPath)
	if err != nil {
		t.Fatal(err)
	}
	file, err := Parse(f)
	f.Close()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(file.IFDs) != 2 {
		t.Fatalf("want 2 IFDs, got %d", len(file.IFDs))
	}
	// Confirm the tail arrays really exceed the 4096-byte cache (so they were
	// NOT cached during parse) -> exercises the out-of-line read path.
	so := findEntry(file.IFDs[1], TagStripOffsets)
	if so == nil || so.DataSize <= 4096 {
		t.Fatalf("test setup: IFD1 StripOffsets DataSize=%d, want >4096", so.DataSize)
	}
	if len(so.InlineValue) != 0 {
		t.Fatalf("test setup: IFD1 StripOffsets unexpectedly cached during parse")
	}

	// Replace IFD 0's single strip.
	newStrips := [][]byte{[]byte("REDACTED")}
	outPath := filepath.Join(dir, "out_large_tail.tiff")
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
	if len(out.IFDs) != 2 {
		t.Fatalf("output has %d IFDs, want 2", len(out.IFDs))
	}

	// Tail IFD 1 must be intact: byte-counts decode and strip bytes match
	// input. Read the offset/count arrays straight from the output file (not
	// via the 4096-capped parse cache) so this assertion is independent of the
	// fix under test.
	order := out.Header.ByteOrder
	readArr := func(ifd *IFD, tag uint16) []uint64 {
		e := findEntry(ifd, tag)
		if e == nil {
			t.Fatalf("IFD missing tag %d", tag)
		}
		blob := make([]byte, e.DataSize)
		if _, err := g.ReadAt(blob, int64(e.DataOffset)); err != nil {
			t.Fatalf("read tag %d array: %v", tag, err)
		}
		arr := make([]uint64, e.Count)
		for i := range arr {
			arr[i] = uint64(order.Uint32(blob[i*4:]))
		}
		return arr
	}
	offs := readArr(out.IFDs[1], TagStripOffsets)
	if len(offs) != nTail {
		t.Fatalf("IFD1 StripOffsets len=%d want %d", len(offs), nTail)
	}
	cnts := readArr(out.IFDs[1], TagStripByteCounts)
	if len(cnts) != nTail {
		t.Fatalf("IFD1 StripByteCounts len=%d want %d", len(cnts), nTail)
	}
	for i := 0; i < nTail; i++ {
		want := tailStrip(i)
		if cnts[i] != uint64(len(want)) {
			t.Fatalf("tail strip %d count = %d, want %d", i, cnts[i], len(want))
		}
		got := make([]byte, cnts[i])
		if _, err := g.ReadAt(got, int64(offs[i])); err != nil {
			t.Fatalf("read tail strip %d: %v", i, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("tail strip %d = %x, want %x", i, got, want)
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
