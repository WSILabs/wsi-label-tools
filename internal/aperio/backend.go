package aperio

import (
	"fmt"
	"os"

	"github.com/cornish/wsi-label-tools/internal/tifflow"
	"github.com/cornish/wsi-label-tools/internal/wsi"
)

type backend struct{}

// New returns an Aperio wsi.Backend.
func New() wsi.Backend { return &backend{} }

func (backend) Name() string { return "aperio" }

func (backend) Detect(path string) (bool, error) { return Detect(path) }

// pickTargetDims resolves the final (width, height) for a replacement label.
//
// Precedence:
//  1. Explicit override via opts.TargetWidth/Height (both must be positive).
//  2. Existing label IFD's dimensions, if a label already exists in the file.
//  3. Aperio canonical default (1200x848).
//
// This preserves orientation and aspect on files whose native label isn't
// the canonical 1200x848 landscape (e.g., older ScanScope-era portrait labels).
func pickTargetDims(file *tifflow.File, labelIdx int, opts wsi.ReplaceOptions) (int, int) {
	if opts.TargetWidth > 0 && opts.TargetHeight > 0 {
		return opts.TargetWidth, opts.TargetHeight
	}
	if labelIdx >= 0 {
		ifd := file.IFDs[labelIdx]
		w, wok := ifd.UintValue(tifflow.TagImageWidth, file.Header.ByteOrder)
		h, hok := ifd.UintValue(tifflow.TagImageLength, file.Header.ByteOrder)
		if wok && hok && w > 0 && h > 0 {
			return int(w), int(h)
		}
	}
	return AperioLabelWidth, AperioLabelHeight
}

func (backend) ReplaceLabel(inPath, outPath string, opts wsi.ReplaceOptions) (*wsi.ReplaceResult, error) {
	if opts.Image == nil {
		return nil, fmt.Errorf("aperio: replace: opts.Image is nil")
	}

	// Default BG to Aperio parchment if caller didn't set one.
	if opts.BG == (wsi.ReplaceOptions{}).BG {
		opts.BG = AperioLabelBG
	}

	f, err := os.Open(inPath)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	defer f.Close()
	file, err := tifflow.Parse(f)
	if err != nil {
		return nil, err
	}

	labelIdx := FindLabelIFD(file)
	targetW, targetH := pickTargetDims(file, labelIdx, opts)

	prepared, err := PrepareLabel(opts.Image, targetW, targetH, opts)
	if err != nil {
		return nil, err
	}

	rep := BuildLabelIFD(prepared)

	params := tifflow.SpliceParams{
		InPath: inPath, OutPath: outPath, File: file,
		Replacement: rep, Fsync: opts.Fsync,
	}
	result := &wsi.ReplaceResult{}

	if labelIdx >= 0 {
		params.Mode = tifflow.SpliceReplace
		params.TargetIdx = labelIdx
	} else {
		if opts.StrictReplace {
			return nil, fmt.Errorf("aperio: %w", wsi.ErrNoLabel)
		}
		result.Added = true
		macroIdx := FindMacroIFD(file)
		if macroIdx >= 0 {
			params.Mode = tifflow.SpliceInsertBefore
			params.TargetIdx = macroIdx
		} else {
			params.Mode = tifflow.SpliceAppend
		}
	}

	if err := tifflow.Splice(params); err != nil {
		return nil, err
	}
	return result, nil
}

func (backend) StripLabel(inPath, outPath string, fsync bool) error {
	f, err := os.Open(inPath)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer f.Close()
	file, err := tifflow.Parse(f)
	if err != nil {
		return err
	}
	labelIdx := FindLabelIFD(file)
	if labelIdx < 0 {
		return fmt.Errorf("aperio: %w", wsi.ErrNoLabel)
	}
	return tifflow.Splice(tifflow.SpliceParams{
		InPath: inPath, OutPath: outPath, File: file,
		Mode: tifflow.SpliceRemove, TargetIdx: labelIdx, Fsync: fsync,
	})
}
