package cli

import (
	"errors"
	"testing"
)

func TestExitCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, 0},
		{"usage", ErrUsage, 2},
		{"strict replace", ErrStrictReplace, 2},
		{"format unrecognized", ErrFormatUnrecognized, 3},
		{"unreadable", ErrUnreadable, 3},
		{"output exists", ErrOutputExists, 4},
		{"layout", ErrUnexpectedLayout, 5},
		{"label added", ErrLabelAdded, 10},
		{"wrapped strict", &wrapped{ErrStrictReplace}, 2},
		{"unknown", errors.New("mystery"), 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExitCode(tc.err); got != tc.want {
				t.Errorf("ExitCode(%v) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}

type wrapped struct{ inner error }

func (w *wrapped) Error() string { return "wrapped: " + w.inner.Error() }
func (w *wrapped) Unwrap() error { return w.inner }
