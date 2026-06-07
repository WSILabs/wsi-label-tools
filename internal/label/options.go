// Package label builds format-neutral replacement label images and the
// format-specific TIFF IFDs that carry them.
package label

import (
	"errors"
	"image/color"
)

// ResizeMode selects how a caller-supplied label is fit to target dims.
type ResizeMode int

const (
	// ResizeFit preserves aspect ratio, letterboxing with BG.
	ResizeFit ResizeMode = iota
	// ResizeStretch scales non-uniformly to fill the target exactly.
	ResizeStretch
	// ResizeNone rejects input not already at target dims.
	ResizeNone
)

// PrepareOptions are the knobs for Prepare.
type PrepareOptions struct {
	BG     color.RGBA
	Rotate int
	Resize ResizeMode
	Force  bool
}

// ErrAspectMismatch is returned by Prepare when ResizeFit sees a source whose
// aspect ratio differs from the target by more than the safety threshold and
// Force is not set.
var ErrAspectMismatch = errors.New("label: aspect ratio mismatch (use force to override)")
