package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/cornish/wsi-label-tools/internal/redact"
)

// RunLabelDims prints the existing label IFD's dimensions for a WSI, as
// "WxH\n" to stdout. Prints nothing and exits 1 if the file has no label
// IFD. Primarily for scripting — tools like label-slides.py query this
// to size their rendering canvas to match the replacement target.
func RunLabelDims(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("label-dims", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", ErrUsage, err)
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("%w: label-dims takes one positional argument (input path)", ErrUsage)
	}
	path := fs.Arg(0)
	w, h, err := redact.LabelDims(path)
	if errors.Is(err, redact.ErrFormatUnsupported) {
		return fmt.Errorf("%w: unrecognized WSI format", ErrFormatUnrecognized)
	}
	if errors.Is(err, redact.ErrNoLabel) {
		return fmt.Errorf("%w: no label IFD in %s", ErrUnreadable, path)
	}
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnexpectedLayout, err)
	}
	fmt.Fprintf(stdout, "%dx%d\n", w, h)
	return nil
}
