package tifflow

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

func TestParseHeader_ClassicLittle(t *testing.T) {
	var b bytes.Buffer
	b.Write([]byte{'I', 'I', 42, 0})
	_ = binary.Write(&b, binary.LittleEndian, uint32(0x10))

	h, err := ParseHeader(bytes.NewReader(b.Bytes()))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if h.BigTIFF {
		t.Errorf("BigTIFF = true, want false")
	}
	if h.ByteOrder != binary.LittleEndian {
		t.Errorf("ByteOrder = %v, want LittleEndian", h.ByteOrder)
	}
	if h.FirstIFDOffset != 0x10 {
		t.Errorf("FirstIFDOffset = %#x, want 0x10", h.FirstIFDOffset)
	}
}

func TestParseHeader_BigTIFFBig(t *testing.T) {
	var b bytes.Buffer
	b.Write([]byte{'M', 'M', 0, 43})
	_ = binary.Write(&b, binary.BigEndian, uint16(8))
	_ = binary.Write(&b, binary.BigEndian, uint16(0))
	_ = binary.Write(&b, binary.BigEndian, uint64(0x100))

	h, err := ParseHeader(bytes.NewReader(b.Bytes()))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !h.BigTIFF {
		t.Errorf("BigTIFF = false, want true")
	}
	if h.FirstIFDOffset != 0x100 {
		t.Errorf("FirstIFDOffset = %#x, want 0x100", h.FirstIFDOffset)
	}
}

func TestParseHeader_BadMagic(t *testing.T) {
	_, err := ParseHeader(bytes.NewReader([]byte{'X', 'Y', 0, 0}))
	if !errors.Is(err, ErrBadMagic) {
		t.Errorf("err = %v, want ErrBadMagic", err)
	}
}
