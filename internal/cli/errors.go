package cli

import "errors"

// Sentinel errors. CLI maps these to exit codes.
var (
	ErrUsage              = errors.New("usage error")
	ErrStrictReplace      = errors.New("no existing label (--strict-replace)")
	ErrFormatUnrecognized = errors.New("unrecognized WSI format")
	ErrUnreadable         = errors.New("input unreadable")
	ErrOutputExists       = errors.New("output path exists (use --overwrite)")
	ErrUnexpectedLayout   = errors.New("input file has unexpected TIFF layout")
	ErrLabelAdded         = errors.New("label added rather than replaced")
)

// ExitCode returns the process exit code for a completed run. Nil → 0.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	switch {
	case errors.Is(err, ErrUsage), errors.Is(err, ErrStrictReplace):
		return 2
	case errors.Is(err, ErrUnreadable), errors.Is(err, ErrFormatUnrecognized):
		return 3
	case errors.Is(err, ErrOutputExists):
		return 4
	case errors.Is(err, ErrUnexpectedLayout):
		return 5
	case errors.Is(err, ErrLabelAdded):
		return 10
	default:
		return 1
	}
}
