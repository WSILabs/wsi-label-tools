package tifflow

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

// OutTag is a tag value to write in a replacement or re-emitted IFD.
// If Data is non-nil, the tag's value is stored out-of-line; if Inline
// is true, Data holds the raw inline value field (length 4 for classic,
// 8 for BigTIFF); otherwise Data is out-of-line bytes.
type OutTag struct {
	Tag   uint16
	Type  TagType
	Count uint64
	// Bytes stores either the inline value field (if Inline) or the
	// out-of-line blob (otherwise).
	Bytes []byte
	// Inline is true when Bytes encodes an inline value field.
	Inline bool

	// ResolvesToOffset, if true, means this tag's value is a file offset
	// (or array of offsets) that must be filled in at emit time. Bytes
	// will be (over)written by the emitter.
	ResolvesToOffset bool
	// OffsetRefs is used by the emitter: for StripOffsets/TileOffsets, the
	// emitter substitutes these with the actual byte offsets of the
	// corresponding strip data blobs.
	OffsetRefs []int // indices into StripData
}

// ReplacementIFD describes a new IFD plus its out-of-line data and strips.
type ReplacementIFD struct {
	Tags      []OutTag
	StripData [][]byte // each strip's compressed bytes
}

// SpliceMode selects the operation performed by Splice.
type SpliceMode int

const (
	// SpliceReplace replaces the target IFD with Replacement.
	SpliceReplace SpliceMode = iota
	// SpliceInsertBefore inserts Replacement just before the target IFD.
	SpliceInsertBefore
	// SpliceAppend appends Replacement as a new IFD at the end of the chain.
	SpliceAppend
	// SpliceRemove removes the target IFD entirely from the chain.
	SpliceRemove
	// SpliceReplaceStripData re-emits the target IFD preserving all of its
	// tags (including private tags) but with its strip/tile data replaced by
	// caller-supplied bytes (StripData), updating StripByteCounts/TileByteCounts
	// to the new lengths.
	SpliceReplaceStripData
)

// SpliceParams is input to Splice.
type SpliceParams struct {
	InPath, OutPath string
	File            *File
	Mode            SpliceMode
	TargetIdx       int // interpretation depends on Mode
	Replacement     *ReplacementIFD
	// StripData is the replacement strip/tile data for the target IFD, one
	// entry per strip/tile, used only by SpliceReplaceStripData.
	StripData [][]byte
	Fsync     bool
}

// Splice performs the prefix-copy + tail-re-emit operation and writes the
// result to OutPath.
func Splice(p SpliceParams) error {
	return doSplice(p)
}

// doSplice implements Splice.
func doSplice(p SpliceParams) error {
	n := len(p.File.IFDs)
	var firstTail int
	switch p.Mode {
	case SpliceReplace:
		if p.TargetIdx < 0 || p.TargetIdx >= n {
			return fmt.Errorf("%w: target idx out of range", ErrUnexpectedLayout)
		}
		firstTail = p.TargetIdx + 1
	case SpliceInsertBefore:
		if p.TargetIdx < 0 || p.TargetIdx > n {
			return fmt.Errorf("%w: target idx out of range", ErrUnexpectedLayout)
		}
		firstTail = p.TargetIdx
	case SpliceAppend:
		firstTail = n
	case SpliceRemove:
		if p.TargetIdx < 0 || p.TargetIdx >= n {
			return fmt.Errorf("%w: target idx out of range", ErrUnexpectedLayout)
		}
		firstTail = p.TargetIdx + 1
	case SpliceReplaceStripData:
		if p.TargetIdx < 0 || p.TargetIdx >= n {
			return fmt.Errorf("%w: target idx out of range", ErrUnexpectedLayout)
		}
		firstTail = p.TargetIdx + 1
	default:
		return fmt.Errorf("unknown splice mode %d", p.Mode)
	}

	// Also include the IFD being replaced/removed itself in the tail
	// for range-dominance checking.
	minTailOwner := firstTail
	if p.Mode == SpliceReplace || p.Mode == SpliceRemove || p.Mode == SpliceReplaceStripData {
		minTailOwner = p.TargetIdx
	}

	var cutoff uint64
	inInfo, err := os.Stat(p.InPath)
	if err != nil {
		return fmt.Errorf("stat input: %w", err)
	}
	fileSize := uint64(inInfo.Size())

	if minTailOwner >= n {
		cutoff = fileSize
	} else {
		cutoff = p.File.Ranges.MinOffsetOfOwnersAtOrAfter(minTailOwner)
	}

	// Validate: no earlier IFD has any range at or after cutoff.
	for i := 0; i < minTailOwner; i++ {
		if r, ok := p.File.Ranges.AnyRangeOfOwnerAtOrAfter(i, cutoff); ok {
			return fmt.Errorf("%w: IFD %d has range %q at offset %d, past cutoff %d",
				ErrUnexpectedLayout, i, r.What, r.Start, cutoff)
		}
	}

	// Open input read-only and output temp.
	in, err := os.Open(p.InPath)
	if err != nil {
		return fmt.Errorf("open input: %w", err)
	}
	defer in.Close()

	outTmp := p.OutPath + fmt.Sprintf(".tmp-%d", os.Getpid())
	out, err := os.OpenFile(outTmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("create output: %w", err)
	}
	committed := false
	defer func() {
		out.Close()
		if !committed {
			os.Remove(outTmp)
		}
	}()

	// Copy prefix [0, cutoff).
	if _, err := io.Copy(out, io.NewSectionReader(in, 0, int64(cutoff))); err != nil {
		return fmt.Errorf("copy prefix: %w", err)
	}

	var emittedIFDOffsets []uint64
	var emittedIFDNextPtrLocs []uint64

	emitReplacement := func(rep *ReplacementIFD) (uint64, uint64, error) {
		return emitReplacementIFD(out, p.File.Header, rep)
	}
	reemitExisting := func(idx int) (uint64, uint64, error) {
		return reemitIFD(out, in, p.File, idx, nil)
	}

	switch p.Mode {
	case SpliceReplace, SpliceInsertBefore:
		recOff, nextPtrLoc, err := emitReplacement(p.Replacement)
		if err != nil {
			return err
		}
		emittedIFDOffsets = append(emittedIFDOffsets, recOff)
		emittedIFDNextPtrLocs = append(emittedIFDNextPtrLocs, nextPtrLoc)
	case SpliceAppend:
		recOff, nextPtrLoc, err := emitReplacement(p.Replacement)
		if err != nil {
			return err
		}
		emittedIFDOffsets = append(emittedIFDOffsets, recOff)
		emittedIFDNextPtrLocs = append(emittedIFDNextPtrLocs, nextPtrLoc)
	case SpliceReplaceStripData:
		recOff, nextPtrLoc, err := reemitIFD(out, in, p.File, p.TargetIdx, p.StripData)
		if err != nil {
			return err
		}
		emittedIFDOffsets = append(emittedIFDOffsets, recOff)
		emittedIFDNextPtrLocs = append(emittedIFDNextPtrLocs, nextPtrLoc)
	case SpliceRemove:
		// No new IFD.
	}

	for i := firstTail; i < n; i++ {
		recOff, nextPtrLoc, err := reemitExisting(i)
		if err != nil {
			return err
		}
		emittedIFDOffsets = append(emittedIFDOffsets, recOff)
		emittedIFDNextPtrLocs = append(emittedIFDNextPtrLocs, nextPtrLoc)
	}

	valueFieldSize := 4
	if p.File.Header.BigTIFF {
		valueFieldSize = 8
	}
	writeOffsetAt := func(f *os.File, loc uint64, val uint64) error {
		buf := make([]byte, valueFieldSize)
		if p.File.Header.BigTIFF {
			p.File.Header.ByteOrder.PutUint64(buf, val)
		} else {
			p.File.Header.ByteOrder.PutUint32(buf, uint32(val))
		}
		_, err := f.WriteAt(buf, int64(loc))
		return err
	}

	// Link emitted IFDs next-to-next.
	for i := 0; i < len(emittedIFDOffsets); i++ {
		var nextVal uint64
		if i+1 < len(emittedIFDOffsets) {
			nextVal = emittedIFDOffsets[i+1]
		}
		if err := writeOffsetAt(out, emittedIFDNextPtrLocs[i], nextVal); err != nil {
			return fmt.Errorf("patch emitted next-IFD: %w", err)
		}
	}

	// Patch predecessor.
	var predecessorNextPtrLoc uint64
	var predecessorNextVal uint64
	switch p.Mode {
	case SpliceReplace, SpliceInsertBefore, SpliceRemove, SpliceReplaceStripData:
		if p.TargetIdx == 0 {
			predecessorNextPtrLoc = headerFirstIFDOffsetLoc(p.File.Header)
		} else {
			predecessorNextPtrLoc = p.File.IFDs[p.TargetIdx-1].NextPointerOffset
		}
	case SpliceAppend:
		if n == 0 {
			predecessorNextPtrLoc = headerFirstIFDOffsetLoc(p.File.Header)
		} else {
			predecessorNextPtrLoc = p.File.IFDs[n-1].NextPointerOffset
		}
	}
	if len(emittedIFDOffsets) > 0 {
		predecessorNextVal = emittedIFDOffsets[0]
	}
	if err := writeOffsetAt(out, predecessorNextPtrLoc, predecessorNextVal); err != nil {
		return fmt.Errorf("patch predecessor next-IFD: %w", err)
	}

	if p.Fsync {
		if err := out.Sync(); err != nil {
			return fmt.Errorf("fsync: %w", err)
		}
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close output: %w", err)
	}
	if err := os.Rename(outTmp, p.OutPath); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	committed = true
	return nil
}

func headerFirstIFDOffsetLoc(h *Header) uint64 {
	if h.BigTIFF {
		return 8
	}
	return 4
}

// emitReplacementIFD writes a ReplacementIFD to out at the current seek
// position. Returns the IFD record's offset and the next-IFD pointer offset.
func emitReplacementIFD(out *os.File, h *Header, rep *ReplacementIFD) (uint64, uint64, error) {
	curOff, err := out.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, 0, err
	}
	cur := uint64(curOff)

	valueFieldSize := 4
	if h.BigTIFF {
		valueFieldSize = 8
	}

	// Promote out-of-line tags to inline when their data fits in the value
	// field. TIFF spec says values that fit MUST be stored inline. Sizes of
	// the value field: 4 bytes (classic) or 8 bytes (BigTIFF).
	for i, t := range rep.Tags {
		if t.Inline {
			continue
		}
		if t.Tag == TagStripOffsets {
			continue
		}
		if len(t.Bytes) <= valueFieldSize {
			rep.Tags[i].Inline = true
		}
	}

	outlineOff := make(map[int]uint64)
	for i, t := range rep.Tags {
		if t.Inline {
			continue
		}
		if t.Tag == TagStripOffsets {
			continue
		}
		outlineOff[i] = cur
		if _, err := out.Write(t.Bytes); err != nil {
			return 0, 0, err
		}
		cur += uint64(len(t.Bytes))
		if cur%2 != 0 {
			if _, err := out.Write([]byte{0}); err != nil {
				return 0, 0, err
			}
			cur++
		}
	}

	// Strip data.
	stripOffsets := make([]uint64, len(rep.StripData))
	for i, s := range rep.StripData {
		stripOffsets[i] = cur
		if _, err := out.Write(s); err != nil {
			return 0, 0, err
		}
		cur += uint64(len(s))
		if cur%2 != 0 {
			if _, err := out.Write([]byte{0}); err != nil {
				return 0, 0, err
			}
			cur++
		}
	}

	// StripOffsets value blob.
	for i, t := range rep.Tags {
		if t.Tag != TagStripOffsets {
			continue
		}
		buf := make([]byte, 4*len(stripOffsets))
		for j, so := range stripOffsets {
			h.ByteOrder.PutUint32(buf[j*4:], uint32(so))
		}
		rep.Tags[i].Bytes = buf
		outlineOff[i] = cur
		if _, err := out.Write(buf); err != nil {
			return 0, 0, err
		}
		cur += uint64(len(buf))
		if cur%2 != 0 {
			if _, err := out.Write([]byte{0}); err != nil {
				return 0, 0, err
			}
			cur++
		}
	}

	ifdOff := cur
	entryCount := uint64(len(rep.Tags))
	countSize := 2
	if h.BigTIFF {
		countSize = 8
	}
	entrySize := 12
	if h.BigTIFF {
		entrySize = 20
	}

	if h.BigTIFF {
		if err := binary.Write(out, h.ByteOrder, entryCount); err != nil {
			return 0, 0, err
		}
	} else {
		if err := binary.Write(out, h.ByteOrder, uint16(entryCount)); err != nil {
			return 0, 0, err
		}
	}

	for i, t := range rep.Tags {
		if err := binary.Write(out, h.ByteOrder, t.Tag); err != nil {
			return 0, 0, err
		}
		if err := binary.Write(out, h.ByteOrder, uint16(t.Type)); err != nil {
			return 0, 0, err
		}
		if h.BigTIFF {
			if err := binary.Write(out, h.ByteOrder, t.Count); err != nil {
				return 0, 0, err
			}
		} else {
			if err := binary.Write(out, h.ByteOrder, uint32(t.Count)); err != nil {
				return 0, 0, err
			}
		}
		valBuf := make([]byte, valueFieldSize)
		if t.Inline {
			copy(valBuf, convertInlineEndian(t.Type, t.Count, t.Bytes, h.ByteOrder, valueFieldSize))
		} else {
			off := outlineOff[i]
			if h.BigTIFF {
				h.ByteOrder.PutUint64(valBuf, off)
			} else {
				h.ByteOrder.PutUint32(valBuf, uint32(off))
			}
		}
		if _, err := out.Write(valBuf); err != nil {
			return 0, 0, err
		}
	}

	nextLoc := ifdOff + uint64(countSize) + entryCount*uint64(entrySize)
	zeroNext := make([]byte, valueFieldSize)
	if _, err := out.Write(zeroNext); err != nil {
		return 0, 0, err
	}

	return ifdOff, nextLoc, nil
}

// convertInlineEndian takes an inline value field encoded little-endian
// (as BuildLabelIFD produces) and re-encodes it in targetOrder.
func convertInlineEndian(tp TagType, count uint64, leBytes []byte, order binary.ByteOrder, valueFieldSize int) []byte {
	out := make([]byte, valueFieldSize)
	sz := tp.Size()
	total := int(count) * sz
	if total > valueFieldSize {
		total = valueFieldSize
	}
	for i := 0; i < int(count); i++ {
		b := leBytes[i*sz : (i+1)*sz]
		var v uint64
		switch sz {
		case 1:
			v = uint64(b[0])
		case 2:
			v = uint64(binary.LittleEndian.Uint16(b))
		case 4:
			v = uint64(binary.LittleEndian.Uint32(b))
		case 8:
			v = binary.LittleEndian.Uint64(b)
		}
		target := out[i*sz : (i+1)*sz]
		switch sz {
		case 1:
			target[0] = byte(v)
		case 2:
			order.PutUint16(target, uint16(v))
		case 4:
			order.PutUint32(target, uint32(v))
		case 8:
			order.PutUint64(target, v)
		}
	}
	return out
}

// reemitIFD re-emits IFD idx from file into out at the current seek position,
// rebasing its out-of-line data and strip data.
//
// If override is non-nil, the IFD's strip/tile payloads are replaced by the
// supplied byte slices (one per strip/tile) instead of being copied from the
// input, and the StripByteCounts/TileByteCounts tag is updated to the new
// lengths. All other tags (including private tags) are copied verbatim. When
// override is nil, the function behaves exactly as before: strip data is copied
// from the input and byte counts are preserved.
func reemitIFD(out *os.File, in *os.File, file *File, idx int, override [][]byte) (uint64, uint64, error) {
	ifd := file.IFDs[idx]
	h := file.Header

	curPos, err := out.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, 0, err
	}
	cur := uint64(curPos)

	newOutlineOff := make(map[int]uint64)
	for i, e := range ifd.Entries {
		if e.Inline || e.DataSize == 0 || e.Tag == TagStripOffsets || e.Tag == TagTileOffsets {
			continue
		}
		// When overriding strip data, the byte-counts tag is rewritten from
		// the new lengths; skip copying its old out-of-line value here.
		if override != nil && (e.Tag == TagStripByteCounts || e.Tag == TagTileByteCounts) {
			continue
		}
		blob := make([]byte, e.DataSize)
		if _, err := in.ReadAt(blob, int64(e.DataOffset)); err != nil {
			return 0, 0, fmt.Errorf("read out-of-line tag %d: %w", e.Tag, err)
		}
		newOutlineOff[i] = cur
		if _, err := out.Write(blob); err != nil {
			return 0, 0, err
		}
		cur += uint64(len(blob))
		if cur%2 != 0 {
			_, _ = out.Write([]byte{0})
			cur++
		}
	}

	var stripTag, countTag uint16
	oldOffsets, ok := ifd.UintArray(TagStripOffsets, h.ByteOrder)
	if ok {
		stripTag, countTag = TagStripOffsets, TagStripByteCounts
	} else {
		oldOffsets, ok = ifd.UintArray(TagTileOffsets, h.ByteOrder)
		stripTag, countTag = TagTileOffsets, TagTileByteCounts
	}
	var newOffsetsBuf []byte
	var newCounts []uint64
	if ok {
		if override != nil && len(override) != len(oldOffsets) {
			return 0, 0, fmt.Errorf("%w: override has %d strips but IFD %d has %d",
				ErrUnexpectedLayout, len(override), idx, len(oldOffsets))
		}
		counts, _ := ifd.UintArray(countTag, h.ByteOrder)
		newOffsets := make([]uint64, len(oldOffsets))
		newCounts = make([]uint64, len(oldOffsets))
		for i, off := range oldOffsets {
			var buf []byte
			if override != nil {
				buf = override[i]
			} else {
				cnt := counts[i]
				if cnt == 0 {
					newOffsets[i] = 0
					newCounts[i] = 0
					continue
				}
				buf = make([]byte, cnt)
				if _, err := in.ReadAt(buf, int64(off)); err != nil {
					return 0, 0, fmt.Errorf("read strip: %w", err)
				}
			}
			newCounts[i] = uint64(len(buf))
			if len(buf) == 0 {
				newOffsets[i] = 0
				continue
			}
			newOffsets[i] = cur
			if _, err := out.Write(buf); err != nil {
				return 0, 0, err
			}
			cur += uint64(len(buf))
			if cur%2 != 0 {
				_, _ = out.Write([]byte{0})
				cur++
			}
		}
		entry := findEntry(ifd, stripTag)
		// In the override path, normalize the offsets/counts to a wide type
		// (LONG for classic, LONG8 for BigTIFF) so the declared entry type
		// always matches the value-blob width. This avoids corruption when the
		// original entry used SHORT (2-byte) values and also handles new strip
		// lengths/offsets that exceed what SHORT could hold. The nil-override
		// path must remain byte-for-byte identical, so it keeps mirroring the
		// original entry's width.
		wide8 := false
		switch {
		case override != nil:
			wide8 = h.BigTIFF
		case entry != nil && (entry.Type == TypeLong8 || entry.Type == TypeIFD8):
			wide8 = true
		}
		if wide8 {
			newOffsetsBuf = make([]byte, 8*len(newOffsets))
			for i, v := range newOffsets {
				h.ByteOrder.PutUint64(newOffsetsBuf[i*8:], v)
			}
		} else {
			newOffsetsBuf = make([]byte, 4*len(newOffsets))
			for i, v := range newOffsets {
				h.ByteOrder.PutUint32(newOffsetsBuf[i*4:], uint32(v))
			}
		}
	}

	valueFieldSize := 4
	if h.BigTIFF {
		valueFieldSize = 8
	}

	// When overriding, build the replacement byte-counts value, mirroring the
	// existing byte-counts entry's type (LONG vs LONG8). Write it out-of-line
	// here if it doesn't fit in the value field.
	var newCountsBuf []byte
	if override != nil && newCounts != nil {
		// Normalize byte counts to the same wide type used for the offsets so
		// the declared entry type matches the value-blob width regardless of the
		// original (possibly SHORT) type, and so new counts above 65535 fit.
		if h.BigTIFF {
			newCountsBuf = make([]byte, 8*len(newCounts))
			for i, v := range newCounts {
				h.ByteOrder.PutUint64(newCountsBuf[i*8:], v)
			}
		} else {
			newCountsBuf = make([]byte, 4*len(newCounts))
			for i, v := range newCounts {
				h.ByteOrder.PutUint32(newCountsBuf[i*4:], uint32(v))
			}
		}
		if len(newCountsBuf) > valueFieldSize {
			for i, e := range ifd.Entries {
				if e.Tag == countTag {
					newOutlineOff[i] = cur
					if _, err := out.Write(newCountsBuf); err != nil {
						return 0, 0, err
					}
					cur += uint64(len(newCountsBuf))
					if cur%2 != 0 {
						_, _ = out.Write([]byte{0})
						cur++
					}
					break
				}
			}
		}
	}

	if newOffsetsBuf != nil {
		for i, e := range ifd.Entries {
			if e.Tag == stripTag {
				if len(newOffsetsBuf) > valueFieldSize {
					newOutlineOff[i] = cur
					if _, err := out.Write(newOffsetsBuf); err != nil {
						return 0, 0, err
					}
					cur += uint64(len(newOffsetsBuf))
					if cur%2 != 0 {
						_, _ = out.Write([]byte{0})
						cur++
					}
				}
				break
			}
		}
	}

	ifdOff := cur
	countSize := 2
	if h.BigTIFF {
		countSize = 8
	}
	entrySize := 12
	if h.BigTIFF {
		entrySize = 20
	}

	if h.BigTIFF {
		_ = binary.Write(out, h.ByteOrder, uint64(len(ifd.Entries)))
	} else {
		_ = binary.Write(out, h.ByteOrder, uint16(len(ifd.Entries)))
	}
	// In the override path the offsets/counts value blobs are emitted at a
	// promoted wide type (LONG classic, LONG8 BigTIFF); the corresponding entry
	// headers must declare that same promoted type so the declared width matches
	// the value blob. promotedType holds that type (zero otherwise).
	promotedType := TypeLong
	if h.BigTIFF {
		promotedType = TypeLong8
	}
	for i, e := range ifd.Entries {
		_ = binary.Write(out, h.ByteOrder, e.Tag)
		emitType := e.Type
		if override != nil && (e.Tag == stripTag || e.Tag == countTag) {
			emitType = promotedType
		}
		_ = binary.Write(out, h.ByteOrder, uint16(emitType))
		if h.BigTIFF {
			_ = binary.Write(out, h.ByteOrder, e.Count)
		} else {
			_ = binary.Write(out, h.ByteOrder, uint32(e.Count))
		}
		valBuf := make([]byte, valueFieldSize)
		switch {
		case e.Tag == stripTag && newOffsetsBuf != nil && len(newOffsetsBuf) <= valueFieldSize:
			copy(valBuf, newOffsetsBuf)
		case override != nil && e.Tag == countTag && newCountsBuf != nil && len(newCountsBuf) <= valueFieldSize:
			copy(valBuf, newCountsBuf)
		case override != nil && e.Tag == countTag && newCountsBuf != nil:
			// Out-of-line byte counts; offset filled below via newOutlineOff.
			if newOff, ok := newOutlineOff[i]; ok {
				if h.BigTIFF {
					h.ByteOrder.PutUint64(valBuf, newOff)
				} else {
					h.ByteOrder.PutUint32(valBuf, uint32(newOff))
				}
			} else {
				return 0, 0, fmt.Errorf("%w: no new offset for byte-counts tag %d on IFD %d", ErrUnexpectedLayout, e.Tag, idx)
			}
		case e.Inline:
			copy(valBuf, e.RawValueField)
		default:
			if newOff, ok := newOutlineOff[i]; ok {
				if h.BigTIFF {
					h.ByteOrder.PutUint64(valBuf, newOff)
				} else {
					h.ByteOrder.PutUint32(valBuf, uint32(newOff))
				}
			} else {
				return 0, 0, fmt.Errorf("%w: no new offset for tag %d on IFD %d", ErrUnexpectedLayout, e.Tag, idx)
			}
		}
		if _, err := out.Write(valBuf); err != nil {
			return 0, 0, err
		}
	}

	cur = ifdOff + uint64(countSize) + uint64(len(ifd.Entries))*uint64(entrySize)
	nextLoc := cur
	zeroNext := make([]byte, valueFieldSize)
	if _, err := out.Write(zeroNext); err != nil {
		return 0, 0, err
	}
	return ifdOff, nextLoc, nil
}

func findEntry(ifd *IFD, tag uint16) *IFDEntry {
	for i := range ifd.Entries {
		if ifd.Entries[i].Tag == tag {
			return &ifd.Entries[i]
		}
	}
	return nil
}

var _ = errors.New
