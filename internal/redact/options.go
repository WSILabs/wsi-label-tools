package redact

import (
	"errors"
	"image"
	"image/color"

	"github.com/cornish/wsi-label-tools/internal/label"
)

// ResizeMode is re-exported from internal/label so CLI and callers depend
// only on redact.
type ResizeMode = label.ResizeMode

const (
	ResizeFit     = label.ResizeFit
	ResizeStretch = label.ResizeStretch
	ResizeNone    = label.ResizeNone
)

var (
	// ErrNoLabel is returned when a redaction target of RoleLabel is required
	// but the file has none.
	ErrNoLabel = errors.New("redact: file has no label")
	// ErrFormatUnsupported is returned when no registered classifier matches.
	ErrFormatUnsupported = errors.New("redact: format not recognized")
	// ErrAspectMismatch is re-exported from internal/label so callers can
	// detect it via errors.Is without importing label.
	ErrAspectMismatch = label.ErrAspectMismatch
)

// ReplaceOptions bundles per-call knobs for Replace.
type ReplaceOptions struct {
	// Image is the caller-supplied label source (any decodable image).
	Image image.Image
	// BG is the letterbox fill used by ResizeFit. If zero, the classifier's
	// DefaultLabelBG is used.
	BG color.RGBA
	// Rotate rotates the input label before resize: 0, 90, 180, or 270.
	Rotate int
	// Resize selects the fit/stretch/none policy.
	Resize ResizeMode
	// Force bypasses the aspect-ratio safety check on ResizeFit.
	Force bool
	// StrictReplace makes a missing label a hard error rather than add-and-warn.
	StrictReplace bool
	// Fsync forces an fsync before the atomic rename.
	Fsync bool
	// TargetWidth and TargetHeight override the target dimensions. Zero means
	// "auto": existing label's dims, else the classifier default.
	TargetWidth  int
	TargetHeight int
}

// Result carries metadata about a completed Replace.
type Result struct {
	// Added is true when the file had no label and one was added.
	Added bool
}
