package redact

import (
	"fmt"
	"os"

	"github.com/cornish/wsi-label-tools/internal/label"
	"github.com/cornish/wsi-label-tools/internal/tifflow"
)

func parseFile(inPath string) (*tifflow.File, error) {
	f, err := os.Open(inPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return tifflow.Parse(f)
}

func findRole(targets []Target, role Role) *Target {
	for i := range targets {
		if targets[i].Role == role {
			return &targets[i]
		}
	}
	return nil
}

// pickTargetDims resolves the final (width, height) for a replacement label:
// explicit override, else the existing label's dims, else the classifier's
// default.
func pickTargetDims(labelT *Target, c Classifier, opts ReplaceOptions) (int, int) {
	if opts.TargetWidth > 0 && opts.TargetHeight > 0 {
		return opts.TargetWidth, opts.TargetHeight
	}
	if labelT != nil && labelT.Width > 0 && labelT.Height > 0 {
		return labelT.Width, labelT.Height
	}
	return c.DefaultLabelDims()
}

// Replace writes outPath with the label replaced by (or added from) opts.Image.
func Replace(inPath, outPath string, opts ReplaceOptions) (*Result, error) {
	if opts.Image == nil {
		return nil, fmt.Errorf("redact: replace: opts.Image is nil")
	}
	file, err := parseFile(inPath)
	if err != nil {
		return nil, err
	}
	c, ok := detectClassifier(file)
	if !ok {
		return nil, ErrFormatUnsupported
	}
	targets, err := c.Classify(file)
	if err != nil {
		return nil, err
	}
	labelT := findRole(targets, RoleLabel)
	if labelT == nil && opts.StrictReplace {
		return nil, ErrNoLabel
	}

	if opts.BG == (ReplaceOptions{}).BG {
		opts.BG = c.DefaultLabelBG()
	}
	w, h := pickTargetDims(labelT, c, opts)
	prepared, err := label.Prepare(opts.Image, w, h, label.PrepareOptions{
		BG:     opts.BG,
		Rotate: opts.Rotate,
		Resize: opts.Resize,
		Force:  opts.Force,
	})
	if err != nil {
		return nil, err
	}
	rep := c.BuildLabelIFD(prepared)

	params := tifflow.SpliceParams{
		InPath: inPath, OutPath: outPath, File: file,
		Replacement: rep, Fsync: opts.Fsync,
	}
	result := &Result{}
	if labelT != nil {
		params.Mode = tifflow.SpliceReplace
		params.TargetIdx = labelT.IFDIndex
	} else {
		result.Added = true
		if macroT := findRole(targets, RoleOverview); macroT != nil {
			params.Mode = tifflow.SpliceInsertBefore
			params.TargetIdx = macroT.IFDIndex
		} else {
			params.Mode = tifflow.SpliceAppend
		}
	}
	if err := tifflow.Splice(params); err != nil {
		return nil, err
	}
	return result, nil
}

// Strip writes outPath with the label IFD removed.
func Strip(inPath, outPath string, fsync bool) error {
	file, err := parseFile(inPath)
	if err != nil {
		return err
	}
	c, ok := detectClassifier(file)
	if !ok {
		return ErrFormatUnsupported
	}
	targets, err := c.Classify(file)
	if err != nil {
		return err
	}
	labelT := findRole(targets, RoleLabel)
	if labelT == nil {
		return ErrNoLabel
	}
	return tifflow.Splice(tifflow.SpliceParams{
		InPath: inPath, OutPath: outPath, File: file,
		Mode: tifflow.SpliceRemove, TargetIdx: labelT.IFDIndex, Fsync: fsync,
	})
}

// LabelDims returns the existing label's pixel dimensions.
func LabelDims(inPath string) (int, int, error) {
	file, err := parseFile(inPath)
	if err != nil {
		return 0, 0, err
	}
	c, ok := detectClassifier(file)
	if !ok {
		return 0, 0, ErrFormatUnsupported
	}
	targets, err := c.Classify(file)
	if err != nil {
		return 0, 0, err
	}
	labelT := findRole(targets, RoleLabel)
	if labelT == nil {
		return 0, 0, ErrNoLabel
	}
	return labelT.Width, labelT.Height, nil
}
