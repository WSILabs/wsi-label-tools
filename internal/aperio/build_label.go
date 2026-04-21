package aperio

import (
	"encoding/binary"
	"fmt"
	"image"

	"github.com/cornish/wsi-label-tools/internal/tifflow"
)

// aperioDescription returns the ImageDescription ASCII string for a label
// with the given dimensions. Format matches what openslide keys on: line 2
// must start with "label", followed by "WxH".
func aperioDescription(width, height int) string {
	return fmt.Sprintf("Aperio Image Library v12.4.7\nlabel %dx%d", width, height)
}

// BuildLabelIFD produces a ReplacementIFD for the given RGB image. The
// image's own bounds determine the label's ImageWidth/ImageLength and the
// "label WxH" ImageDescription line — callers are expected to have already
// resized/prepared the image to the target dimensions.
func BuildLabelIFD(img image.Image) *tifflow.ReplacementIFD {
	b := img.Bounds()
	width, height := b.Dx(), b.Dy()
	strips := EncodeLabelStrips(img)
	desc := aperioDescription(width, height)

	stripByteCounts := make([]byte, 0, len(strips)*4)
	// Byte counts written as LONG (uint32) — safe since label strips are <<4GB.
	// Written little-endian; the emitter converts to the file's byte order.
	for _, s := range strips {
		lb := make([]byte, 4)
		binary.LittleEndian.PutUint32(lb, uint32(len(s)))
		stripByteCounts = append(stripByteCounts, lb...)
	}

	// StripOffsets: one entry per strip, placeholders (will be filled by emitter).
	stripOffsetsPlaceholder := make([]byte, len(strips)*4)

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

	// BitsPerSample: 3 × SHORT = 6 bytes. Marked not-inline here; the
	// emitter's inline-promotion pass handles BigTIFF's 8-byte value field.
	bps := make([]byte, 6)
	binary.LittleEndian.PutUint16(bps[0:2], 8)
	binary.LittleEndian.PutUint16(bps[2:4], 8)
	binary.LittleEndian.PutUint16(bps[4:6], 8)

	tags := []tifflow.OutTag{
		{Tag: tifflow.TagNewSubfileType, Type: tifflow.TypeLong, Count: 1, Inline: true, Bytes: longLE(1)},
		{Tag: tifflow.TagImageWidth, Type: tifflow.TypeLong, Count: 1, Inline: true, Bytes: longLE(uint32(width))},
		{Tag: tifflow.TagImageLength, Type: tifflow.TypeLong, Count: 1, Inline: true, Bytes: longLE(uint32(height))},
		{Tag: tifflow.TagBitsPerSample, Type: tifflow.TypeShort, Count: 3, Inline: false, Bytes: bps},
		{Tag: tifflow.TagCompression, Type: tifflow.TypeShort, Count: 1, Inline: true, Bytes: shortLE(5)},
		{Tag: tifflow.TagPhotometric, Type: tifflow.TypeShort, Count: 1, Inline: true, Bytes: shortLE(2)},
		{Tag: tifflow.TagImageDescription, Type: tifflow.TypeASCII, Count: uint64(len(desc) + 1), Inline: false, Bytes: append([]byte(desc), 0)},
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
