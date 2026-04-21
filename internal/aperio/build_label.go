package aperio

import (
	"encoding/binary"
	"image"

	"github.com/cornish/wsi-label-tools/internal/tifflow"
)

// BuildLabelIFD produces a ReplacementIFD for the given 1200×848 RGB image.
// Strip offsets are marked ResolvesToOffset so the emitter fills them in.
func BuildLabelIFD(img image.Image) *tifflow.ReplacementIFD {
	strips := EncodeLabelStrips(img)

	stripByteCounts := make([]byte, 0, len(strips)*4)
	// Byte counts written as LONG (uint32) — safe since label strips are <<4GB.
	// The emitter will re-encode these in the file's byte order. For BigTIFF,
	// we use LONG8 (uint64) via the emitter's byte-order-aware path.
	// For simplicity, we write placeholder bytes here and let the emitter
	// re-serialize per file order. We encode as little-endian LONG for now;
	// the emitter rewrites if needed. (TODO: fix for BigTIFF + big-endian.)
	for _, s := range strips {
		b := make([]byte, 4)
		binary.LittleEndian.PutUint32(b, uint32(len(s)))
		stripByteCounts = append(stripByteCounts, b...)
	}

	// StripOffsets: one entry per strip, placeholders (will be filled by emitter).
	stripOffsetsPlaceholder := make([]byte, len(strips)*4)

	// Inline value encoder helper: returns a 4/8 byte buffer with value v
	// encoded at width appropriate for Type. We build small SHORT/LONG
	// inlines here in little-endian; the emitter converts to file order.
	shortLE := func(v uint16) []byte {
		b := make([]byte, 4)
		binary.LittleEndian.PutUint16(b, v)
		return b
	}
	longLE := func(v uint32) []byte {
		b := make([]byte, 4)
		binary.LittleEndian.PutUint32(b, v)
		return b
	}

	// BitsPerSample: 3 × SHORT = 6 bytes — out-of-line in classic TIFF
	// (value field only 4 bytes). Emitter stores it out-of-line.
	bps := make([]byte, 6)
	binary.LittleEndian.PutUint16(bps[0:2], 8)
	binary.LittleEndian.PutUint16(bps[2:4], 8)
	binary.LittleEndian.PutUint16(bps[4:6], 8)

	tags := []tifflow.OutTag{
		{Tag: tifflow.TagNewSubfileType, Type: tifflow.TypeLong, Count: 1, Inline: true, Bytes: longLE(1)},
		{Tag: tifflow.TagImageWidth, Type: tifflow.TypeLong, Count: 1, Inline: true, Bytes: longLE(AperioLabelWidth)},
		{Tag: tifflow.TagImageLength, Type: tifflow.TypeLong, Count: 1, Inline: true, Bytes: longLE(AperioLabelHeight)},
		{Tag: tifflow.TagBitsPerSample, Type: tifflow.TypeShort, Count: 3, Inline: false, Bytes: bps},
		{Tag: tifflow.TagCompression, Type: tifflow.TypeShort, Count: 1, Inline: true, Bytes: shortLE(5)},
		{Tag: tifflow.TagPhotometric, Type: tifflow.TypeShort, Count: 1, Inline: true, Bytes: shortLE(2)},
		{Tag: tifflow.TagImageDescription, Type: tifflow.TypeASCII, Count: uint64(len(AperioLabelDescription) + 1), Inline: false, Bytes: append([]byte(AperioLabelDescription), 0)},
		{Tag: tifflow.TagStripOffsets, Type: tifflow.TypeLong, Count: uint64(len(strips)), Inline: false, Bytes: stripOffsetsPlaceholder, ResolvesToOffset: true, OffsetRefs: iotaSlice(len(strips))},
		{Tag: tifflow.TagSamplesPerPixel, Type: tifflow.TypeShort, Count: 1, Inline: true, Bytes: shortLE(3)},
		{Tag: tifflow.TagRowsPerStrip, Type: tifflow.TypeShort, Count: 1, Inline: true, Bytes: shortLE(uint16(AperioRowsPerStrip))},
		{Tag: tifflow.TagStripByteCounts, Type: tifflow.TypeLong, Count: uint64(len(strips)), Inline: false, Bytes: stripByteCounts},
		{Tag: tifflow.TagPlanarConfig, Type: tifflow.TypeShort, Count: 1, Inline: true, Bytes: shortLE(1)},
		{Tag: tifflow.TagPredictor, Type: tifflow.TypeShort, Count: 1, Inline: true, Bytes: shortLE(2)},
	}

	return &tifflow.ReplacementIFD{
		Tags:      tags,
		StripData: strips,
	}
}

func iotaSlice(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = i
	}
	return out
}
