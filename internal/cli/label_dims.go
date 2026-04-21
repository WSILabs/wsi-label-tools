package cli

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/cornish/wsi-label-tools/internal/aperio"
	"github.com/cornish/wsi-label-tools/internal/tifflow"
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

	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnreadable, err)
	}
	defer f.Close()

	file, err := tifflow.Parse(f)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnexpectedLayout, err)
	}
	idx := aperio.FindLabelIFD(file)
	if idx < 0 {
		return fmt.Errorf("%w: no label IFD in %s", ErrUnreadable, path)
	}
	ifd := file.IFDs[idx]
	w, wok := ifd.UintValue(tifflow.TagImageWidth, file.Header.ByteOrder)
	h, hok := ifd.UintValue(tifflow.TagImageLength, file.Header.ByteOrder)
	if !wok || !hok {
		return fmt.Errorf("%w: label IFD missing width/height tags", ErrUnexpectedLayout)
	}
	fmt.Fprintf(stdout, "%dx%d\n", w, h)
	return nil
}
