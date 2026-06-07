package cli

import (
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	"io"
	"os"
	"strconv"
	"strings"

	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/tiff"

	"github.com/cornish/wsi-label-tools/internal/redact"
)

func RunReplace(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("replace", flag.ContinueOnError)
	fs.SetOutput(stderr)
	overwrite := fs.Bool("overwrite", false, "allow clobbering existing output")
	strict := fs.Bool("strict-replace", false, "fail if no label IFD exists")
	quiet := fs.Bool("q", false, "silence non-error stderr output")
	fsyncFlag := fs.Bool("fsync", true, "fsync output before rename")
	bg := fs.String("bg", "F5F5E6", "letterbox background color RRGGBB (used by --resize=fit)")
	rotate := fs.Int("rotate", 0, "rotate label before resize: 0 / 90 / 180 / 270")
	resize := fs.String("resize", "fit", "resize policy: fit | stretch | none")
	force := fs.Bool("force", false, "bypass the >2x aspect-ratio safety check on --resize=fit")
	labelDims := fs.String("label-dims", "", "override target label dimensions, e.g. '1200x848' (default: match existing label's dims, else backend default)")
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
	resizeMode, err := parseResizeMode(*resize)
	if err != nil {
		return fmt.Errorf("%w: --resize: %v", ErrUsage, err)
	}
	if *rotate != 0 && *rotate != 90 && *rotate != 180 && *rotate != 270 {
		return fmt.Errorf("%w: --rotate must be 0, 90, 180, or 270", ErrUsage)
	}
	tw, th, err := parseLabelDims(*labelDims)
	if err != nil {
		return fmt.Errorf("%w: --label-dims: %v", ErrUsage, err)
	}

	// Dispatch through the format-neutral redact orchestrator.
	opts := redact.ReplaceOptions{
		Image:         img,
		BG:            bgColor,
		Rotate:        *rotate,
		Resize:        resizeMode,
		Force:         *force,
		StrictReplace: *strict,
		Fsync:         *fsyncFlag,
		TargetWidth:   tw,
		TargetHeight:  th,
	}
	res, err := redact.Replace(in, out, opts)
	if errors.Is(err, redact.ErrFormatUnsupported) {
		return fmt.Errorf("%w: unrecognized WSI format", ErrFormatUnrecognized)
	}
	if errors.Is(err, redact.ErrNoLabel) {
		return fmt.Errorf("%w", ErrStrictReplace)
	}
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
	return nil
}

func parseHexRGB(s string) (color.RGBA, error) {
	s = strings.TrimPrefix(s, "#")
	if len(s) != 6 {
		return color.RGBA{}, fmt.Errorf("expected 6 hex digits, got %q", s)
	}
	n, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return color.RGBA{}, err
	}
	return color.RGBA{
		R: uint8(n >> 16),
		G: uint8(n >> 8),
		B: uint8(n),
		A: 255,
	}, nil
}

// parseLabelDims parses "WxH" (or empty, meaning "auto") into width, height.
// Empty string returns (0, 0, nil) which the backend reads as "auto".
func parseLabelDims(s string) (int, int, error) {
	if s == "" {
		return 0, 0, nil
	}
	parts := strings.SplitN(strings.ToLower(s), "x", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("expected WxH, got %q", s)
	}
	w, err := strconv.Atoi(parts[0])
	if err != nil || w <= 0 {
		return 0, 0, fmt.Errorf("bad width %q", parts[0])
	}
	h, err := strconv.Atoi(parts[1])
	if err != nil || h <= 0 {
		return 0, 0, fmt.Errorf("bad height %q", parts[1])
	}
	return w, h, nil
}

func parseResizeMode(s string) (redact.ResizeMode, error) {
	switch strings.ToLower(s) {
	case "fit":
		return redact.ResizeFit, nil
	case "stretch":
		return redact.ResizeStretch, nil
	case "none":
		return redact.ResizeNone, nil
	default:
		return 0, fmt.Errorf("must be fit, stretch, or none; got %q", s)
	}
}
