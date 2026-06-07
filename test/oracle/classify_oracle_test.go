// Package oracle cross-validates wsi-label's pure-Go label classification
// against opentile-go (the wsilabs format-reading standard of truth). It is a
// separate module so opentile-go never enters the shipping binary's go.mod.
// Build/run with CGO_ENABLED=0 — opentile-go's pure-Go stubs cover the
// classification paths; pixel decode (cgo) is never exercised.
package oracle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	ot "github.com/wsilabs/opentile-go"
	_ "github.com/wsilabs/opentile-go/formats/all"

	"github.com/cornish/wsi-label-tools/internal/redact"
	_ "github.com/cornish/wsi-label-tools/internal/redact/format"
	"github.com/cornish/wsi-label-tools/internal/tifflow"
)

var corpus = []struct {
	dir string // subdir under sample_files/
	ext string
}{
	{"svs", ".svs"},
	{"generic-tiff", ".tiff"},
	{"philips-tiff", ".tiff"},
	{"ndpi", ".ndpi"},
}

// hasLabelOurs reports whether our classifier finds a RoleLabel target.
func hasLabelOurs(t *testing.T, path string) bool {
	t.Helper()
	w, h, err := redact.LabelDims(path)
	if err != nil {
		return false
	}
	return w > 0 && h > 0
}

// hasLabelOpentile reports whether opentile-go classifies an associated image
// of Type "label".
func hasLabelOpentile(t *testing.T, path string) bool {
	t.Helper()
	s, err := ot.OpenFile(path)
	if err != nil {
		t.Fatalf("opentile open %s: %v", path, err)
	}
	defer s.Close()
	for _, a := range s.Associated() {
		if a.Type() == "label" {
			return true
		}
	}
	return false
}

// TestLabelPresenceAgrees asserts our label detection matches opentile-go's
// across the sample corpus.
func TestLabelPresenceAgrees(t *testing.T) {
	for _, c := range corpus {
		c := c
		dir := filepath.Join("..", "..", "sample_files", c.dir)
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Logf("skipping %s: no sample dir: %v", c.dir, err)
			continue
		}
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), c.ext) {
				continue
			}
			name := e.Name()
			t.Run(c.dir+"/"+name, func(t *testing.T) {
				p := filepath.Join(dir, name)
				ours := hasLabelOurs(t, p)
				theirs := hasLabelOpentile(t, p)
				if ours != theirs {
					t.Fatalf("label-presence mismatch: ours=%v opentile=%v", ours, theirs)
				}
			})
		}
	}
}

// TestLabelOffsetAgrees, where opentile-go exposes AssociatedIFDOffset,
// asserts the located label IFD offset matches ours.
func TestLabelOffsetAgrees(t *testing.T) {
	for _, c := range corpus {
		c := c
		dir := filepath.Join("..", "..", "sample_files", c.dir)
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Logf("skipping %s: no sample dir: %v", c.dir, err)
			continue
		}
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), c.ext) {
				continue
			}
			name := e.Name()
			t.Run(c.dir+"/"+name, func(t *testing.T) {
				p := filepath.Join(dir, name)

				s, err := ot.OpenFile(p)
				if err != nil {
					t.Fatalf("opentile open: %v", err)
				}
				defer s.Close()
				var wantOff int64 = -1
				for _, a := range s.Associated() {
					if a.Type() != "label" {
						continue
					}
					if off, ok := s.AssociatedIFDOffset(a); ok {
						wantOff = off
					}
				}
				if wantOff < 0 {
					t.Skip("opentile exposes no label IFD offset for this file")
				}

				f, err := os.Open(p)
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()
				file, err := tifflow.Parse(f)
				if err != nil {
					t.Fatalf("tifflow parse: %v", err)
				}
				gotOff := labelIFDOffset(t, file)
				if uint64(wantOff) != gotOff {
					t.Fatalf("label IFD offset: ours=%d opentile=%d", gotOff, wantOff)
				}
			})
		}
	}
}

// labelIFDOffset returns the file offset of the IFD our classifier would
// pick as the label, using the ImageDescription second-line "label" prefix
// rule (which applies to both Aperio SVS and the generic-TIFF fixture that
// retains Aperio-style descriptions).
func labelIFDOffset(t *testing.T, file *tifflow.File) uint64 {
	t.Helper()
	for i, ifd := range file.IFDs {
		if i == 1 {
			continue
		}
		desc, _ := ifd.StringValue(tifflow.TagImageDescription)
		lines := strings.SplitN(desc, "\n", 3)
		if len(lines) >= 2 && strings.HasPrefix(lines[1], "label") {
			return ifd.Offset
		}
	}
	return 0
}
