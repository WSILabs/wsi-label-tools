package atomic

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteAtomic_Success(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "out.bin")

	err := WriteAtomic(target, func(w *os.File) error {
		_, err := w.Write([]byte("hello"))
		return err
	}, true)
	if err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("content = %q, want %q", got, "hello")
	}

	// No leftover temp files.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "out.bin.tmp-") {
			t.Errorf("leftover temp file: %s", e.Name())
		}
	}
}

func TestWriteAtomic_CleansUpOnError(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "out.bin")

	wantErr := "boom"
	err := WriteAtomic(target, func(w *os.File) error {
		_, _ = w.Write([]byte("partial"))
		return &errString{wantErr}
	}, true)
	if err == nil || err.Error() != wantErr {
		t.Fatalf("err = %v, want %q", err, wantErr)
	}

	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("target exists after error: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "out.bin.tmp-") {
			t.Errorf("leftover temp file: %s", e.Name())
		}
	}
}

type errString struct{ s string }

func (e *errString) Error() string { return e.s }
