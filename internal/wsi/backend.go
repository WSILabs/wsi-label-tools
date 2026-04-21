// Package wsi defines the format-neutral Backend interface and errors.
package wsi

import (
	"errors"
	"image"
)

var (
	ErrNoLabel           = errors.New("wsi: file has no label IFD")
	ErrFormatUnsupported = errors.New("wsi: format not recognized")
)

// ReplaceResult carries metadata about a completed replace run.
type ReplaceResult struct {
	// Added is true if the file had no label and one was added.
	Added bool
}

// Backend is the interface implemented by each format backend.
type Backend interface {
	// Name returns a short identifier ("aperio", "hamamatsu", ...).
	Name() string
	// Detect returns true if this backend can handle the file at path.
	Detect(path string) (bool, error)
	// ReplaceLabel produces an output file at outPath with the label
	// replaced by (or added from) img. strictReplace makes missing label
	// a hard error rather than add-and-warn.
	ReplaceLabel(inPath, outPath string, img image.Image, strictReplace, fsync bool) (*ReplaceResult, error)
	// StripLabel produces an output file at outPath with any label IFD
	// removed.
	StripLabel(inPath, outPath string, fsync bool) error
}
