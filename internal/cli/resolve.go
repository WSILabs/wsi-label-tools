package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ResolveOutputPath returns the final output path for a replace/strip run.
//
//   - If out is empty, derive "<stem>_relabeled<ext>" next to input. If that
//     exists, try "_relabeled_1", "_2", ... until a free name is found (unless
//     overwrite is true, in which case the bare derived name is clobbered).
//   - If out is non-empty, use it. If it exists and overwrite is false,
//     return ErrOutputExists.
//   - In every case, if the resolved path equals the input path, return
//     ErrUsage.
func ResolveOutputPath(input, out string, overwrite bool) (string, error) {
	absInput, err := filepath.Abs(input)
	if err != nil {
		return "", fmt.Errorf("abs input: %w", err)
	}

	var resolved string
	if out == "" {
		resolved = derivePath(absInput, overwrite)
	} else {
		absOut, err := filepath.Abs(out)
		if err != nil {
			return "", fmt.Errorf("abs out: %w", err)
		}
		if _, err := os.Stat(absOut); err == nil && !overwrite {
			return "", fmt.Errorf("%w: %s", ErrOutputExists, absOut)
		}
		resolved = absOut
	}

	if resolved == absInput {
		return "", fmt.Errorf("%w: input and output paths are the same", ErrUsage)
	}
	return resolved, nil
}

func derivePath(absInput string, overwrite bool) string {
	dir := filepath.Dir(absInput)
	base := filepath.Base(absInput)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)

	first := filepath.Join(dir, stem+"_relabeled"+ext)
	if overwrite {
		return first
	}
	if _, err := os.Stat(first); os.IsNotExist(err) {
		return first
	}
	for i := 1; ; i++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s_relabeled_%d%s", stem, i, ext))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}
