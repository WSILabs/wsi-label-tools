package cli

import (
	"flag"
	"fmt"
	"image"
	"io"
	"os"
	"strconv"
	"strings"

	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/tiff"

	"github.com/cornish/wsi-label-tools/internal/aperio"
	"github.com/cornish/wsi-label-tools/internal/wsi"
)

func RunReplace(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("replace", flag.ContinueOnError)
	fs.SetOutput(stderr)
	overwrite := fs.Bool("overwrite", false, "allow clobbering existing output")
	strict := fs.Bool("strict-replace", false, "fail if no label IFD exists")
	quiet := fs.Bool("q", false, "silence non-error stderr output")
	fsyncFlag := fs.Bool("fsync", true, "fsync output before rename")
	bg := fs.String("bg", "F5F5E6", "letterbox background color RRGGBB")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", ErrUsage, err)
	}
	if fs.NArg() < 2 || fs.NArg() > 3 {
		return fmt.Errorf("%w: replace takes <wsi-in> <label-image> [<wsi-out>]", ErrUsage)
	}
	in := fs.Arg(0)
	labelPath := fs.Arg(1)
	outExplicit := ""
	if fs.NArg() == 3 {
		outExplicit = fs.Arg(2)
	}

	out, err := ResolveOutputPath(in, outExplicit, *overwrite)
	if err != nil {
		return err
	}

	// Decode label.
	lf, err := os.Open(labelPath)
	if err != nil {
		return fmt.Errorf("%w: label: %v", ErrUnreadable, err)
	}
	defer lf.Close()
	img, _, err := image.Decode(lf)
	if err != nil {
		return fmt.Errorf("%w: decode label: %v", ErrUnreadable, err)
	}

	bgColor, err := parseHexRGB(*bg)
	if err != nil {
		return fmt.Errorf("%w: --bg: %v", ErrUsage, err)
	}
	_ = bgColor // currently aperio.AperioLabelBG is used; future: thread through.

	// Dispatch to aperio backend (registry has one entry today).
	b := aperio.New()
	ok, derr := b.Detect(in)
	if derr != nil {
		return fmt.Errorf("%w: %v", ErrUnreadable, derr)
	}
	if !ok {
		return fmt.Errorf("%w: not an Aperio SVS", ErrFormatUnrecognized)
	}
	res, err := b.ReplaceLabel(in, out, img, *strict, *fsyncFlag)
	if err != nil {
		return err
	}
	if res.Added {
		if !*quiet {
			fmt.Fprintf(stderr, "wsi-label: no existing label; added one to %s\n", out)
		}
		return fmt.Errorf("%w: %s", ErrLabelAdded, out)
	}
	if !*quiet {
		fmt.Fprintf(stdout, "wsi-label: replaced label: %s → %s\n", in, out)
	}
	_ = wsi.ErrNoLabel // keep import live for error-type checks in Run
	return nil
}

func parseHexRGB(s string) (struct{ R, G, B uint8 }, error) {
	var rgb struct{ R, G, B uint8 }
	s = strings.TrimPrefix(s, "#")
	if len(s) != 6 {
		return rgb, fmt.Errorf("expected 6 hex digits, got %q", s)
	}
	n, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return rgb, err
	}
	rgb.R = uint8(n >> 16)
	rgb.G = uint8(n >> 8)
	rgb.B = uint8(n)
	return rgb, nil
}
