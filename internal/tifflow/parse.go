package tifflow

import (
	"fmt"
	"io"
)

// File is a parsed TIFF / BigTIFF file structure (metadata only, no pixel data).
type File struct {
	Header *Header
	IFDs   []*IFD
	// ChainOrder maps IFD offset → chain index (0-based). Useful for
	// computing "IFDs after label in chain" without re-scanning.
	ChainOrder map[uint64]int
	Ranges     *RangeMap
}

// Parse walks the IFD chain from the header. It does not dereference
// SubIFDs (tag 330); if any IFD has SubIFDs, it returns ErrSubIFDs.
func Parse(r io.ReaderAt) (*File, error) {
	sz := int64(1 << 62)
	if sizer, ok := r.(interface{ Size() int64 }); ok {
		sz = sizer.Size()
	}
	sr := io.NewSectionReader(r, 0, sz)
	h, err := ParseHeader(sr)
	if err != nil {
		return nil, err
	}
	f := &File{Header: h, ChainOrder: map[uint64]int{}, Ranges: &RangeMap{}}
	// Header owns its own range (no IFD owns it, so use owner = -1).
	if err := f.Ranges.Add(Range{Start: 0, End: uint64(HeaderSize(h.BigTIFF)), Owner: -1, What: "header"}); err != nil {
		return nil, err
	}

	seen := map[uint64]bool{}
	offset := h.FirstIFDOffset
	for offset != 0 {
		if seen[offset] {
			return nil, fmt.Errorf("%w: IFD chain loop at offset %d", ErrUnexpectedLayout, offset)
		}
		seen[offset] = true
		ifd, err := ReadIFD(r, h, offset)
		if err != nil {
			return nil, fmt.Errorf("read IFD at %d: %w", offset, err)
		}
		for _, e := range ifd.Entries {
			if e.Tag == TagSubIFDs {
				return nil, ErrSubIFDs
			}
		}
		owner := len(f.IFDs)
		f.ChainOrder[offset] = owner
		f.IFDs = append(f.IFDs, ifd)

		// Add IFD record range.
		if err := f.Ranges.Add(Range{
			Start: ifd.Offset,
			End:   ifd.Offset + ifd.RecordLength,
			Owner: owner,
			What:  "ifd",
		}); err != nil {
			return nil, fmt.Errorf("ifd %d range: %w", owner, err)
		}

		// Add out-of-line ranges for tags.
		for _, e := range ifd.Entries {
			if e.Inline || e.DataSize == 0 {
				continue
			}
			if err := f.Ranges.Add(Range{
				Start: e.DataOffset,
				End:   e.DataOffset + e.DataSize,
				Owner: owner,
				What:  fmt.Sprintf("tag%d", e.Tag),
			}); err != nil {
				return nil, fmt.Errorf("ifd %d tag %d range: %w", owner, e.Tag, err)
			}
		}

		// Add strip / tile data ranges.
		offs, offsOK := ifd.UintArray(TagStripOffsets, h.ByteOrder)
		if !offsOK {
			offs, offsOK = ifd.UintArray(TagTileOffsets, h.ByteOrder)
		}
		counts, countsOK := ifd.UintArray(TagStripByteCounts, h.ByteOrder)
		if !countsOK {
			counts, countsOK = ifd.UintArray(TagTileByteCounts, h.ByteOrder)
		}
		if offsOK && countsOK {
			if len(offs) != len(counts) {
				return nil, fmt.Errorf("%w: ifd %d has %d strip offsets but %d byte counts",
					ErrUnexpectedLayout, owner, len(offs), len(counts))
			}
			for i, off := range offs {
				if counts[i] == 0 {
					continue
				}
				if err := f.Ranges.Add(Range{
					Start: off,
					End:   off + counts[i],
					Owner: owner,
					What:  fmt.Sprintf("strip[%d]", i),
				}); err != nil {
					return nil, fmt.Errorf("ifd %d strip %d: %w", owner, i, err)
				}
			}
		}

		offset = ifd.NextOffset
	}
	return f, nil
}
