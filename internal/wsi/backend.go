// Package wsi defines the format-neutral Backend interface and errors.
package wsi

import (
	"errors"
	"image"
	"image/color"
)

var (
	ErrNoLabel           = errors.New("wsi: file has no label IFD")
	ErrFormatUnsupported = errors.New("wsi: format not recognized")
	ErrAspectMismatch    = errors.New("wsi: label aspect ratio mismatch (use --force to override)")
)

// ResizeMode selects how a caller-supplied label is fit into the format's
// target dimensions.
type ResizeMode int

const (
	// ResizeFit preserves aspect ratio, letterboxing with the background
	// color where the input and target aspects differ.
	ResizeFit ResizeMode = iota
	// ResizeStretch scales non-uniformly to fill the target exactly.
	ResizeStretch
	// ResizeNone rejects any input not already at the target dimensions.
	ResizeNone
)

// ReplaceOptions bundles per-call knobs for ReplaceLabel.
type ReplaceOptions struct {
	// Image is the caller-supplied label source (any decodable format).
	Image image.Image

	// BG is the letterbox fill color used by ResizeFit. Ignored otherwise.
	BG color.RGBA

	// Rotate applies a rotation to the image before resize, in degrees
	// (0, 90, 180, or 270). Other values are rejected as a usage error.
	Rotate int

	// Resize selects the fit/stretch/none policy.
	Resize ResizeMode

	// Force bypasses safety checks — today, only the aspect-ratio
	// sanity check on ResizeFit. Callers should only set this when
	// they've verified the input is intended.
	Force bool

	// StrictReplace makes a missing label IFD a hard error rather than
	// an add-and-warn.
	StrictReplace bool

	// Fsync forces an fsync before the atomic rename.
	Fsync bool

	// TargetWidth and TargetHeight override the target dimensions the
	// label is fit to. Zero means "auto": use the existing label's
	// dimensions if there is one, else fall back to the backend's
	// default (1200x848 for Aperio).
	TargetWidth  int
	TargetHeight int
}

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
	// replaced by (or added from) opts.Image.
	ReplaceLabel(inPath, outPath string, opts ReplaceOptions) (*ReplaceResult, error)
	// StripLabel produces an output file at outPath with any label IFD
	// removed.
	StripLabel(inPath, outPath string, fsync bool) error
}
