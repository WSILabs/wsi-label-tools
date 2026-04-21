package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/cornish/wsi-label-tools/internal/tifflow"
)

func RunInspect(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("inspect", flag.ContinueOnError)
	fs.SetOutput(stderr)
	verbose := fs.Bool("v", false, "verbose: include every tag")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", ErrUsage, err)
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("%w: inspect takes one positional argument (input path)", ErrUsage)
	}
	path := fs.Arg(0)

	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnreadable, err)
	}
	defer f.Close()

	file, err := tifflow.Parse(f)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnexpectedLayout, err)
	}

	fmt.Fprintf(stdout, "File: %s\n", path)
	fmt.Fprintf(stdout, "Format: %s, byte order: %s\n",
		formatName(file.Header.BigTIFF), orderName(file.Header.ByteOrder))
	fmt.Fprintf(stdout, "IFDs: %d\n", len(file.IFDs))
	for i, ifd := range file.IFDs {
		role := identifyRole(i, ifd, file.Header.ByteOrder)
		desc, _ := ifd.StringValue(tifflow.TagImageDescription)
		descFirst := strings.SplitN(desc, "\n", 2)[0]
		if len(descFirst) > 60 {
			descFirst = descFirst[:57] + "..."
		}
		w, _ := ifd.UintValue(tifflow.TagImageWidth, file.Header.ByteOrder)
		h, _ := ifd.UintValue(tifflow.TagImageLength, file.Header.ByteOrder)
		fmt.Fprintf(stdout, "  [%d] offset=%d role=%s dims=%dx%d desc=%q\n",
			i, ifd.Offset, role, w, h, descFirst)
		if *verbose {
			for _, e := range ifd.Entries {
				fmt.Fprintf(stdout, "       tag=%d type=%d count=%d inline=%v\n",
					e.Tag, e.Type, e.Count, e.Inline)
			}
		}
	}
	return nil
}

func formatName(big bool) string {
	if big {
		return "BigTIFF"
	}
	return "TIFF"
}

func orderName(o interface{}) string {
	return fmt.Sprintf("%v", o)
}

// identifyRole is a best-effort role assignment matching OpenSlide's logic
// for Aperio SVS. It lives in cli for display purposes; wsi-label's actual
// label location is computed in the aperio backend.
func identifyRole(idx int, ifd *tifflow.IFD, order interface{}) string {
	desc, _ := ifd.StringValue(tifflow.TagImageDescription)
	lines := strings.SplitN(desc, "\n", 3)
	second := ""
	if len(lines) >= 2 {
		second = lines[1]
	}
	switch {
	case idx == 0:
		return "baseline"
	case idx == 1 && !hasTiles(ifd):
		return "thumbnail"
	case strings.HasPrefix(second, "label"):
		return "label"
	case strings.HasPrefix(second, "macro"):
		return "macro"
	case hasTiles(ifd):
		return "pyramid"
	default:
		return "unknown"
	}
}

func hasTiles(ifd *tifflow.IFD) bool {
	for _, e := range ifd.Entries {
		if e.Tag == tifflow.TagTileOffsets {
			return true
		}
	}
	return false
}
