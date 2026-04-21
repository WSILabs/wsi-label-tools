// Package aperio implements the Aperio SVS backend for wsi-label.
package aperio

import (
	"errors"
	"os"
	"strings"

	"github.com/cornish/wsi-label-tools/internal/tifflow"
)

// Detect returns true if the file at path is an Aperio SVS.
// Heuristic: a valid TIFF / BigTIFF whose IFD 0 ImageDescription begins
// with "Aperio".
func Detect(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	file, err := tifflow.Parse(f)
	if err != nil {
		if errors.Is(err, tifflow.ErrBadMagic) {
			return false, nil
		}
		return false, err
	}
	if len(file.IFDs) == 0 {
		return false, nil
	}
	desc, ok := file.IFDs[0].StringValue(tifflow.TagImageDescription)
	if !ok {
		return false, nil
	}
	return strings.HasPrefix(desc, "Aperio"), nil
}

// FindLabelIFD returns the chain-order index of the label IFD, or -1 if
// the file has no label. The rule (matching OpenSlide): non-tiled IFD
// that is not IFD 1 (thumbnail) and whose ImageDescription second line
// begins with "label".
func FindLabelIFD(file *tifflow.File) int {
	for i, ifd := range file.IFDs {
		if i == 1 {
			continue
		}
		if hasTileTags(ifd) {
			continue
		}
		desc, ok := ifd.StringValue(tifflow.TagImageDescription)
		if !ok {
			continue
		}
		lines := strings.SplitN(desc, "\n", 3)
		if len(lines) < 2 {
			continue
		}
		if strings.HasPrefix(lines[1], "label") {
			return i
		}
	}
	return -1
}

// FindMacroIFD returns the chain-order index of the macro IFD, or -1.
func FindMacroIFD(file *tifflow.File) int {
	for i, ifd := range file.IFDs {
		if i == 1 {
			continue
		}
		if hasTileTags(ifd) {
			continue
		}
		desc, ok := ifd.StringValue(tifflow.TagImageDescription)
		if !ok {
			continue
		}
		lines := strings.SplitN(desc, "\n", 3)
		if len(lines) < 2 {
			continue
		}
		if strings.HasPrefix(lines[1], "macro") {
			return i
		}
	}
	return -1
}

func hasTileTags(ifd *tifflow.IFD) bool {
	for _, e := range ifd.Entries {
		if e.Tag == tifflow.TagTileOffsets {
			return true
		}
	}
	return false
}
