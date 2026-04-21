package cli

import (
	"flag"
	"fmt"
	"io"

	"github.com/tcornish/wsi-label/internal/aperio"
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
	b := aperio.New()
	ok, derr := b.Detect(in)
	if derr != nil {
		return fmt.Errorf("%w: %v", ErrUnreadable, derr)
	}
	if !ok {
		return fmt.Errorf("%w: not an Aperio SVS", ErrFormatUnrecognized)
	}
	if err := b.StripLabel(in, out, *fsyncFlag); err != nil {
		return err
	}
	if !*quiet {
		fmt.Fprintf(stdout, "wsi-label: stripped label: %s → %s\n", in, out)
	}
	return nil
}
