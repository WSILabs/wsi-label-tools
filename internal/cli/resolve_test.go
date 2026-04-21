package cli

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveOutputPath(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "sample.svs")
	if err := os.WriteFile(input, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	t.Run("derived, no conflict", func(t *testing.T) {
		got, err := ResolveOutputPath(input, "", false)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		want := filepath.Join(dir, "sample_relabeled.svs")
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("derived, conflict numbers up", func(t *testing.T) {
		// Create sample_relabeled.svs and _1.svs; expect _2.svs.
		for _, name := range []string{"sample_relabeled.svs", "sample_relabeled_1.svs"} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
				t.Fatalf("setup: %v", err)
			}
		}
		got, err := ResolveOutputPath(input, "", false)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		want := filepath.Join(dir, "sample_relabeled_2.svs")
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("explicit, does not exist", func(t *testing.T) {
		explicit := filepath.Join(dir, "explicit.svs")
		got, err := ResolveOutputPath(input, explicit, false)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if got != explicit {
			t.Errorf("got %q, want %q", got, explicit)
		}
	})

	t.Run("explicit exists, no overwrite", func(t *testing.T) {
		explicit := filepath.Join(dir, "exists.svs")
		if err := os.WriteFile(explicit, []byte("x"), 0o644); err != nil {
			t.Fatalf("setup: %v", err)
		}
		_, err := ResolveOutputPath(input, explicit, false)
		if !errors.Is(err, ErrOutputExists) {
			t.Errorf("err = %v, want ErrOutputExists", err)
		}
	})

	t.Run("explicit exists, overwrite", func(t *testing.T) {
		explicit := filepath.Join(dir, "ow.svs")
		if err := os.WriteFile(explicit, []byte("x"), 0o644); err != nil {
			t.Fatalf("setup: %v", err)
		}
		got, err := ResolveOutputPath(input, explicit, true)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if got != explicit {
			t.Errorf("got %q, want %q", got, explicit)
		}
	})

	t.Run("same file rejected", func(t *testing.T) {
		_, err := ResolveOutputPath(input, input, true)
		if !errors.Is(err, ErrUsage) {
			t.Errorf("err = %v, want ErrUsage", err)
		}
	})
}
