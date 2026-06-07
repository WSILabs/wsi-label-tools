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

func firstDiff(a, b []byte) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return -1
}
