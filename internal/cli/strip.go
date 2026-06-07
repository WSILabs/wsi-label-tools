package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/cornish/wsi-label-tools/internal/redact"
)

func RunStrip(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("strip", flag.ContinueOnError)
	fs.SetOutput(stderr)
	overwrite := fs.Bool("overwrite", false, "allow clobbering existing output")
	quiet := fs.Bool("q", false, "silence non-error stderr output")
	fsyncFlag := fs.Bool("fsync", true, "fsync output before rename")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", ErrUsage, err)
	}
	if fs.NArg() < 1 || fs.NArg() > 2 {
		return fmt.Errorf("%w: strip takes <wsi-in> [<wsi-out>]", ErrUsage)
	}
	in := fs.Arg(0)
	outExplicit := ""
	if fs.NArg() == 2 {
		outExplicit = fs.Arg(1)
	}
	out, err := ResolveOutputPath(in, outExplicit, *overwrite)
	if err != nil {
		return err
	}
	err = redact.Strip(in, out, *fsyncFlag)
	if errors.Is(err, redact.ErrFormatUnsupported) {
		return fmt.Errorf("%w: unrecognized WSI format", ErrFormatUnrecognized)
	}
	if errors.Is(err, redact.ErrNoLabel) {
		return fmt.Errorf("%w: no label IFD in %s", ErrUnreadable, in)
	}
	if err != nil {
		return err
	}
	if !*quiet {
		fmt.Fprintf(stdout, "wsi-label: stripped label: %s → %s\n", in, out)
	}
	return nil
}
