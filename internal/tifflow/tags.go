package tifflow

// TIFF tag IDs used in this package.
const (
	TagNewSubfileType   = 254
	TagImageWidth       = 256
	TagImageLength      = 257
	TagBitsPerSample    = 258
	TagCompression      = 259
	TagPhotometric      = 262
	TagImageDescription = 270
	TagStripOffsets     = 273
	TagSamplesPerPixel  = 277
	TagRowsPerStrip     = 278
	TagStripByteCounts  = 279
	TagSoftware         = 305
	TagPlanarConfig     = 284
	TagTileWidth        = 322
	TagTileLength       = 323
	TagTileOffsets      = 324
	TagTileByteCounts   = 325
	TagSubIFDs          = 330
	TagJPEGTables       = 347
	TagPredictor        = 317
	TagICCProfile       = 34675
	TagWSIImageType     = 65080 // wsitools/COG-WSI private: ASCII role ("label"/"overview"/...)
)

// TIFF tag types (TIFF 6.0 + BigTIFF extensions).
type TagType uint16

const (
	TypeByte      TagType = 1
	TypeASCII     TagType = 2
	TypeShort     TagType = 3
	TypeLong      TagType = 4
	TypeRational  TagType = 5
	TypeSByte     TagType = 6
	TypeUndefined TagType = 7
	TypeSShort    TagType = 8
	TypeSLong     TagType = 9
	TypeSRational TagType = 10
	TypeFloat     TagType = 11
	TypeDouble    TagType = 12
	TypeLong8     TagType = 16 // BigTIFF
	TypeSLong8    TagType = 17 // BigTIFF
	TypeIFD8      TagType = 18 // BigTIFF
)

// Size returns the byte size of a single value of this type.
func (t TagType) Size() int {
	switch t {
	case TypeByte, TypeASCII, TypeSByte, TypeUndefined:
		return 1
	case TypeShort, TypeSShort:
		return 2
	case TypeLong, TypeSLong, TypeFloat:
		return 4
	case TypeRational, TypeSRational, TypeDouble, TypeLong8, TypeSLong8, TypeIFD8:
		return 8
	default:
		return 0
	}
}
