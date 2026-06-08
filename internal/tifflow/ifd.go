package tifflow

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
)

// IFDEntry is a single tag entry in an IFD.
type IFDEntry struct {
	Tag   uint16
	Type  TagType
	Count uint64

	// RawValueField holds the 4-byte (classic) or 8-byte (BigTIFF) value field
	// exactly as it appeared in the IFD record. For inline values, the bytes
	// encode the value directly. For out-of-line values, it encodes the offset.
	RawValueField []byte

	// Inline is true when the value fits in RawValueField; false when
	// RawValueField encodes an offset to out-of-line data.
	Inline bool

	// DataOffset is the out-of-line offset (when Inline is false).
	DataOffset uint64

	// DataSize is the byte length of the value data (Count * Type.Size()),
	// computed for convenience.
	DataSize uint64

	// InlineValue is the raw out-of-line bytes (when Inline is false,
	// populated by ReadIFD), or a copy of RawValueField trimmed to DataSize
	// (when Inline is true).
	InlineValue []byte
}

// IFD is a parsed TIFF image file directory.
type IFD struct {
	// Offset where this IFD record begins in the file.
	Offset uint64
	// Length of the IFD record in bytes (not including out-of-line data).
	RecordLength uint64
	// Position of the next-IFD pointer inside the file.
	NextPointerOffset uint64
	// Value of the next-IFD pointer (0 = end of chain).
	NextOffset uint64

	Entries []IFDEntry
}

// ReadIFD reads one IFD record at the given offset. It fetches out-of-line
// data for every entry whose DataSize is small enough to be useful for
// parsing (<=4096 bytes), primarily so callers can read ImageDescription.
// Larger out-of-line blobs (big StripOffsets arrays for multi-GB files) are
// NOT read — callers that need them use ReadOutOfLine.
func ReadIFD(r io.ReaderAt, h *Header, offset uint64) (*IFD, error) {
	ifd := &IFD{Offset: offset}

	// Entry count: 2 bytes (classic) or 8 bytes (BigTIFF).
	var countBuf [8]byte
	countSize := 2
	if h.BigTIFF {
		countSize = 8
	}
	if _, err := r.ReadAt(countBuf[:countSize], int64(offset)); err != nil {
		return nil, fmt.Errorf("read entry count: %w", err)
	}
	var count uint64
	if h.BigTIFF {
		count = h.ByteOrder.Uint64(countBuf[:8])
	} else {
		count = uint64(h.ByteOrder.Uint16(countBuf[:2]))
	}

	// Each entry: 12 bytes (classic) or 20 bytes (BigTIFF).
	entrySize := 12
	if h.BigTIFF {
		entrySize = 20
	}
	entriesSize := int(count) * entrySize
	entriesBuf := make([]byte, entriesSize)
	if _, err := r.ReadAt(entriesBuf, int64(offset)+int64(countSize)); err != nil {
		return nil, fmt.Errorf("read entries: %w", err)
	}

	valueFieldSize := 4
	if h.BigTIFF {
		valueFieldSize = 8
	}

	entries := make([]IFDEntry, count)
	for i := uint64(0); i < count; i++ {
		base := int(i) * entrySize
		e := &entries[i]
		e.Tag = h.ByteOrder.Uint16(entriesBuf[base : base+2])
		e.Type = TagType(h.ByteOrder.Uint16(entriesBuf[base+2 : base+4]))
		if h.BigTIFF {
			e.Count = h.ByteOrder.Uint64(entriesBuf[base+4 : base+12])
		} else {
			e.Count = uint64(h.ByteOrder.Uint32(entriesBuf[base+4 : base+8]))
		}
		valStart := base + 4 + (entrySize - 4 - valueFieldSize) + valueFieldSize
		// Above: for classic, base+8..base+12. For BigTIFF, base+12..base+20.
		var rawVal []byte
		if h.BigTIFF {
			rawVal = make([]byte, 8)
			copy(rawVal, entriesBuf[base+12:base+20])
		} else {
			rawVal = make([]byte, 4)
			copy(rawVal, entriesBuf[base+8:base+12])
		}
		e.RawValueField = rawVal
		sz := e.Type.Size()
		if sz == 0 {
			return nil, fmt.Errorf("%w: type=%d", ErrUnknownType, e.Type)
		}
		e.DataSize = e.Count * uint64(sz)
		if e.DataSize <= uint64(valueFieldSize) {
			e.Inline = true
			e.InlineValue = rawVal[:e.DataSize]
		} else {
			e.Inline = false
			if h.BigTIFF {
				e.DataOffset = h.ByteOrder.Uint64(rawVal)
			} else {
				e.DataOffset = uint64(h.ByteOrder.Uint32(rawVal))
			}
			// Fetch out-of-line data if small.
			if e.DataSize <= 4096 {
				buf := make([]byte, e.DataSize)
				if _, err := r.ReadAt(buf, int64(e.DataOffset)); err != nil {
					return nil, fmt.Errorf("read out-of-line tag %d: %w", e.Tag, err)
				}
				e.InlineValue = buf
			}
		}
		_ = valStart
	}
	ifd.Entries = entries
	ifd.RecordLength = uint64(countSize + entriesSize + valueFieldSize)
	// (valueFieldSize bytes = next-IFD pointer)
	nextPointerOffset := int64(offset) + int64(countSize) + int64(entriesSize)
	ifd.NextPointerOffset = uint64(nextPointerOffset)

	nextBuf := make([]byte, valueFieldSize)
	if _, err := r.ReadAt(nextBuf, nextPointerOffset); err != nil {
		return nil, fmt.Errorf("read next IFD: %w", err)
	}
	if h.BigTIFF {
		ifd.NextOffset = h.ByteOrder.Uint64(nextBuf)
	} else {
		ifd.NextOffset = uint64(h.ByteOrder.Uint32(nextBuf))
	}

	return ifd, nil
}

// UintValue returns an unsigned integer value for tag, first element only.
func (ifd *IFD) UintValue(tag uint16, order binary.ByteOrder) (uint64, bool) {
	for _, e := range ifd.Entries {
		if e.Tag != tag {
			continue
		}
		if len(e.InlineValue) == 0 {
			return 0, false
		}
		switch e.Type {
		case TypeByte, TypeSByte, TypeUndefined:
			return uint64(e.InlineValue[0]), true
		case TypeShort, TypeSShort:
			return uint64(order.Uint16(e.InlineValue[:2])), true
		case TypeLong, TypeSLong:
			return uint64(order.Uint32(e.InlineValue[:4])), true
		case TypeLong8, TypeSLong8, TypeIFD8:
			return order.Uint64(e.InlineValue[:8]), true
		}
		return 0, false
	}
	return 0, false
}

// UintArray returns all values of tag as uint64s.
func (ifd *IFD) UintArray(tag uint16, order binary.ByteOrder) ([]uint64, bool) {
	for _, e := range ifd.Entries {
		if e.Tag != tag {
			continue
		}
		buf := e.InlineValue
		if len(buf) == 0 {
			return nil, false
		}
		n := int(e.Count)
		out := make([]uint64, n)
		sz := e.Type.Size()
		for i := 0; i < n; i++ {
			b := buf[i*sz : (i+1)*sz]
			switch e.Type {
			case TypeByte, TypeSByte, TypeUndefined:
				out[i] = uint64(b[0])
			case TypeShort, TypeSShort:
				out[i] = uint64(order.Uint16(b))
			case TypeLong, TypeSLong:
				out[i] = uint64(order.Uint32(b))
			case TypeLong8, TypeSLong8, TypeIFD8:
				out[i] = order.Uint64(b)
			default:
				return nil, false
			}
		}
		return out, true
	}
	return nil, false
}

// FloatValue returns the value of an inline FLOAT (type 11) tag as float64.
// Only handles count==1 inline floats (sufficient for NDPI magnification).
func (ifd *IFD) FloatValue(tag uint16, order binary.ByteOrder) (float64, bool) {
	for _, e := range ifd.Entries {
		if e.Tag != tag {
			continue
		}
		if e.Type != TypeFloat || e.Count != 1 || !e.Inline || len(e.RawValueField) < 4 {
			return 0, false
		}
		return float64(math.Float32frombits(order.Uint32(e.RawValueField[:4]))), true
	}
	return 0, false
}

// StringValue returns the ASCII value of a tag, stripped of trailing NULs.
func (ifd *IFD) StringValue(tag uint16) (string, bool) {
	for _, e := range ifd.Entries {
		if e.Tag != tag {
			continue
		}
		if e.Type != TypeASCII {
			return "", false
		}
		return string(bytes.TrimRight(e.InlineValue, "\x00")), true
	}
	return "", false
}

// RawValue returns the cached value bytes of a tag regardless of TIFF type,
// for small inline or small out-of-line values (DataSize ≤ 4096). ok=false
// if the tag is absent or its value was too large to cache. This is the
// correct accessor for non-ASCII blobs such as XMP (tag 700, TypeByte/
// TypeUndefined) where StringValue returns ok=false due to the type check.
func (ifd *IFD) RawValue(tag uint16) ([]byte, bool) {
	for _, e := range ifd.Entries {
		if e.Tag != tag {
			continue
		}
		if len(e.InlineValue) == 0 {
			return nil, false
		}
		return e.InlineValue, true
	}
	return nil, false
}
