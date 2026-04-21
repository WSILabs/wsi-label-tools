package aperio

import (
	"fmt"
	"image"
	"os"

	"github.com/tcornish/wsi-label/internal/tifflow"
	"github.com/tcornish/wsi-label/internal/wsi"
)

type backend struct{}

// New returns an Aperio wsi.Backend.
func New() wsi.Backend { return &backend{} }

func (backend) Name() string { return "aperio" }

func (backend) Detect(path string) (bool, error) { return Detect(path) }

func (backend) ReplaceLabel(inPath, outPath string, img image.Image, strictReplace, fsync bool) (*wsi.ReplaceResult, error) {
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
	resized := FitTo(img, AperioLabelWidth, AperioLabelHeight, AperioLabelBG)
	rep := BuildLabelIFD(resized)

	params := tifflow.SpliceParams{
		InPath: inPath, OutPath: outPath, File: file,
		Replacement: rep, Fsync: fsync,
	}
	result := &wsi.ReplaceResult{}

	if labelIdx >= 0 {
		params.Mode = tifflow.SpliceReplace
		params.TargetIdx = labelIdx
	} else {
		if strictReplace {
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
