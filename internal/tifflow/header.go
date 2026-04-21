package tifflow

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Header is the parsed TIFF / BigTIFF header.
type Header struct {
	ByteOrder      binary.ByteOrder
	BigTIFF        bool
	FirstIFDOffset uint64
}

// ParseHeader reads the 8-byte classic or 16-byte BigTIFF header from r.
func ParseHeader(r io.Reader) (*Header, error) {
	var magic [4]byte
	if _, err := io.ReadFull(r, magic[:]); err != nil {
		return nil, fmt.Errorf("read magic: %w", err)
	}
	var order binary.ByteOrder
	switch {
	case magic[0] == 'I' && magic[1] == 'I':
		order = binary.LittleEndian
	case magic[0] == 'M' && magic[1] == 'M':
		order = binary.BigEndian
	default:
		return nil, ErrBadMagic
	}
	version := order.Uint16(magic[2:4])
	h := &Header{ByteOrder: order}
	switch version {
	case 42:
		var off uint32
		if err := binary.Read(r, order, &off); err != nil {
			return nil, fmt.Errorf("read first IFD offset: %w", err)
		}
		h.FirstIFDOffset = uint64(off)
	case 43:
		h.BigTIFF = true
		var offsetSize uint16
		var reserved uint16
		if err := binary.Read(r, order, &offsetSize); err != nil {
			return nil, fmt.Errorf("read offset size: %w", err)
		}
		if offsetSize != 8 {
			return nil, fmt.Errorf("%w: BigTIFF offset size = %d, want 8", ErrBadMagic, offsetSize)
		}
		if err := binary.Read(r, order, &reserved); err != nil {
			return nil, fmt.Errorf("read reserved: %w", err)
		}
		if reserved != 0 {
			return nil, fmt.Errorf("%w: BigTIFF reserved = %d, want 0", ErrBadMagic, reserved)
		}
		if err := binary.Read(r, order, &h.FirstIFDOffset); err != nil {
			return nil, fmt.Errorf("read first IFD offset: %w", err)
		}
	default:
		return nil, fmt.Errorf("%w: version = %d", ErrBadMagic, version)
	}
	return h, nil
}

// EmitHeader writes the header in the same variant as h describes.
func EmitHeader(w io.Writer, h *Header) error {
	var magic [4]byte
	if h.ByteOrder == binary.LittleEndian {
		magic[0], magic[1] = 'I', 'I'
	} else {
		magic[0], magic[1] = 'M', 'M'
	}
	if h.BigTIFF {
		h.ByteOrder.PutUint16(magic[2:4], 43)
	} else {
		h.ByteOrder.PutUint16(magic[2:4], 42)
	}
	if _, err := w.Write(magic[:]); err != nil {
		return err
	}
	if h.BigTIFF {
		if err := binary.Write(w, h.ByteOrder, uint16(8)); err != nil {
			return err
		}
		if err := binary.Write(w, h.ByteOrder, uint16(0)); err != nil {
			return err
		}
		return binary.Write(w, h.ByteOrder, h.FirstIFDOffset)
	}
	return binary.Write(w, h.ByteOrder, uint32(h.FirstIFDOffset))
}

// HeaderSize returns the number of bytes the header occupies in the file.
func HeaderSize(bigTIFF bool) int64 {
	if bigTIFF {
		return 16
	}
	return 8
}
