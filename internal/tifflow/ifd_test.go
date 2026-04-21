package tifflow

import (
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// buildClassicIFD writes a tiny classic-TIFF file to a temp file and returns
// its path. Layout:
//
//	header (8 bytes)
//	IFD at offset 8: 2 entries:
//	  ImageWidth (256) LONG = 42
//	  ImageDescription (270) ASCII "hello\x00" stored out-of-line at offset 38
//	next-IFD = 0
func buildClassicIFD(t *testing.T) string {
	t.Helper()
	var buf bytes.Buffer
	order := binary.LittleEndian

	buf.Write([]byte{'I', 'I', 42, 0})
	_ = binary.Write(&buf, order, uint32(8)) // first IFD offset = 8

	// IFD at offset 8:
	_ = binary.Write(&buf, order, uint16(2)) // entry count

	// Entry 1: ImageWidth, LONG, count=1, value=42 (inline)
	_ = binary.Write(&buf, order, uint16(TagImageWidth))
	_ = binary.Write(&buf, order, uint16(TypeLong))
	_ = binary.Write(&buf, order, uint32(1))
	_ = binary.Write(&buf, order, uint32(42))

	// Entry 2: ImageDescription, ASCII, count=6 ("hello\x00"), value=offset 38
	_ = binary.Write(&buf, order, uint16(TagImageDescription))
	_ = binary.Write(&buf, order, uint16(TypeASCII))
	_ = binary.Write(&buf, order, uint32(6))
	_ = binary.Write(&buf, order, uint32(38))

	// next-IFD
	_ = binary.Write(&buf, order, uint32(0))

	// Out-of-line ASCII at offset 38
	buf.Write([]byte{'h', 'e', 'l', 'l', 'o', 0})

	dir := t.TempDir()
	p := filepath.Join(dir, "tiny.tif")
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return p
}

func TestReadIFD_Classic(t *testing.T) {
	p := buildClassicIFD(t)
	f, err := os.Open(p)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()

	h, err := ParseHeader(f)
	if err != nil {
		t.Fatalf("header: %v", err)
	}
	ifd, err := ReadIFD(f, h, h.FirstIFDOffset)
	if err != nil {
		t.Fatalf("ReadIFD: %v", err)
	}

	if ifd.NextOffset != 0 {
		t.Errorf("NextOffset = %d, want 0", ifd.NextOffset)
	}
	if len(ifd.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(ifd.Entries))
	}
	w, ok := ifd.UintValue(TagImageWidth, binary.LittleEndian)
	if !ok || w != 42 {
		t.Errorf("ImageWidth = (%d, %v), want (42, true)", w, ok)
	}
	desc, ok := ifd.StringValue(TagImageDescription)
	if !ok || desc != "hello" {
		t.Errorf("ImageDescription = (%q, %v), want (hello, true)", desc, ok)
	}
}

func TestReadIFD_UnexpectedEOF(t *testing.T) {
	// A header claiming IFD at offset 100 in a 20-byte file.
	p := filepath.Join(t.TempDir(), "short.tif")
	var buf bytes.Buffer
	buf.Write([]byte{'I', 'I', 42, 0})
	_ = binary.Write(&buf, binary.LittleEndian, uint32(100))
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	f, _ := os.Open(p)
	defer f.Close()
	h, _ := ParseHeader(f)
	_, err := ReadIFD(f, h, h.FirstIFDOffset)
	if err == nil || !errorIsEOFish(err) {
		t.Errorf("err = %v, want EOF-ish", err)
	}
}

func errorIsEOFish(err error) bool {
	return err != nil && (err == io.EOF || err == io.ErrUnexpectedEOF ||
		(err.Error() != "" && (contains(err.Error(), "EOF") || contains(err.Error(), "read entries"))))
}

func contains(s, sub string) bool {
	return bytes.Contains([]byte(s), []byte(sub))
}
