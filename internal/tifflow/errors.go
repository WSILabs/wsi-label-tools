package tifflow

import "errors"

var (
	ErrBadMagic         = errors.New("tifflow: bad TIFF magic")
	ErrUnexpectedLayout = errors.New("tifflow: unexpected TIFF byte layout")
	ErrOverlap          = errors.New("tifflow: overlapping byte ranges in file")
	ErrSubIFDs          = errors.New("tifflow: SubIFDs not supported")
	ErrUnknownType      = errors.New("tifflow: unknown tag type")
)
