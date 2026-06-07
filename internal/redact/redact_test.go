package redact_test

import (
	"bytes"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cornish/wsi-label-tools/internal/redact"
	_ "github.com/cornish/wsi-label-tools/internal/redact/format"
	"github.com/cornish/wsi-label-tools/internal/tifflow"
)

func svsSample(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join("..", "..", "sample_files", "svs", name)
}

// newTestLabel returns a small solid image to use as a replacement label.
func newTestLabel() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 1200, 848))
	for y := 0; y < 848; y++ {
		for x := 0; x < 1200; x++ {
			img.Set(x, y, color.RGBA{200, 100, 50, 255})
		}
	}
	return img
}

func TestReplace_CMU1SmallRegion(t *testing.T) {
	src := svsSample(t, "CMU-1-Small-Region.svs")
	if _, err := os.Stat(src); err != nil {
		t.Skipf("sample not present: %v", err)
	}
	out := filepath.Join(t.TempDir(), "relabeled.svs")
	res, err := redact.Replace(src, out, redact.ReplaceOptions{
		Image: newTestLabel(), Resize: redact.ResizeNone,
		TargetWidth: 1200, TargetHeight: 848, Fsync: false,
	})
	if err != nil {
		t.Fatalf("Replace: %v", err)
	}
	if res.Added {
		t.Fatalf("expected replace, not add")
	}
	w, h, err := redact.LabelDims(out)
	if err != nil {
		t.Fatalf("LabelDims(out): %v", err)
	}
	if w != 1200 || h != 848 {
		t.Fatalf("label dims: got %dx%d want 1200x848", w, h)
	}
}

func TestStrip_CMU1SmallRegion(t *testing.T) {
	src := svsSample(t, "CMU-1-Small-Region.svs")
	if _, err := os.Stat(src); err != nil {
		t.Skipf("sample not present: %v", err)
	}
	out := filepath.Join(t.TempDir(), "stripped.svs")
	if err := redact.Strip(src, out, false); err != nil {
		t.Fatalf("Strip: %v", err)
	}
	if _, _, err := redact.LabelDims(out); err == nil {
		t.Fatalf("expected no label after strip")
	}
}

// TestReplace_AllSamples runs replace against every sample SVS and verifies
// the prefix-before-label is byte-identical to the input (the core invariant).
func TestReplace_AllSamples(t *testing.T) {
	dir := filepath.Join("..", "..", "sample_files", "svs")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("no sample dir: %v", err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".svs") {
			continue
		}
		t.Run(e.Name(), func(t *testing.T) {
			in := filepath.Join(dir, e.Name())
			out := filepath.Join(t.TempDir(), "out.svs")
			if _, err := redact.Replace(in, out, redact.ReplaceOptions{
				Image: newTestLabel(), Resize: redact.ResizeStretch,
			}); err != nil {
				t.Fatalf("Replace %s: %v", e.Name(), err)
			}
			assertPrefixIdentical(t, in, out)
			if _, _, err := redact.LabelDims(out); err != nil {
				t.Fatalf("output %s has no label after replace: %v", e.Name(), err)
			}
		})
	}
}

func TestGenericTIFF_StripAndReplace(t *testing.T) {
	src := filepath.Join("..", "..", "sample_files", "generic-tiff", "CMU-1-Small-Region.stripped.tiff")
	if _, err := os.Stat(src); err != nil {
		t.Skipf("fixture absent: %v", err)
	}

	// strip: output must have no label.
	stripOut := filepath.Join(t.TempDir(), "stripped.tiff")
	if err := redact.Strip(src, stripOut, false); err != nil {
		t.Fatalf("Strip: %v", err)
	}
	if _, _, err := redact.LabelDims(stripOut); err == nil {
		t.Fatalf("expected no label after strip")
	}

	// replace: output must still classify a label.
	repOut := filepath.Join(t.TempDir(), "relabeled.tiff")
	if _, err := redact.Replace(src, repOut, redact.ReplaceOptions{
		Image: newTestLabel(), Resize: redact.ResizeStretch,
	}); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	if _, _, err := redact.LabelDims(repOut); err != nil {
		t.Fatalf("output has no label after replace: %v", err)
	}
	assertPrefixIdentical(t, src, repOut)
}

// assertPrefixIdentical verifies that the portion of the output file that was
// copied verbatim from the input is byte-identical. The splice preserves
// [0, cutoff) from the input, then patches the predecessor IFD's next-pointer
// field at predecessorNextPtrLoc to point to the new label IFD. We therefore
// check two sub-ranges:
//   - [0, predecessorNextPtrLoc) — untouched
//   - [predecessorNextPtrLoc+ptrSize, cutoff) — untouched bytes after the patch
func assertPrefixIdentical(t *testing.T, in, out string) {
	t.Helper()
	fIn, err := os.Open(in)
	if err != nil {
		t.Fatal(err)
	}
	defer fIn.Close()
	file, err := tifflow.Parse(fIn)
	if err != nil {
		t.Fatalf("parse in: %v", err)
	}
	// Find the label IFD index.
	labelIdx := -1
	for i, ifd := range file.IFDs {
		desc, _ := ifd.StringValue(tifflow.TagImageDescription)
		lines := strings.SplitN(desc, "\n", 3)
		if len(lines) >= 2 && strings.HasPrefix(lines[1], "label") {
			labelIdx = i
			break
		}
	}
	if labelIdx < 0 {
		return // no label in this sample; nothing to compare
	}
	// Use the same cutoff the splice uses.
	cutoff := file.Ranges.MinOffsetOfOwnersAtOrAfter(labelIdx)
	if cutoff == 0 || cutoff == ^uint64(0) {
		return
	}
	// Size of next-IFD pointer field.
	ptrSize := uint64(4)
	if file.Header.BigTIFF {
		ptrSize = 8
	}
	// Where the predecessor's next-pointer sits (Splice patches this).
	var patchAt uint64
	if labelIdx == 0 {
		if file.Header.BigTIFF {
			patchAt = 8
		} else {
			patchAt = 4
		}
	} else {
		patchAt = file.IFDs[labelIdx-1].NextPointerOffset
	}

	fOut, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer fOut.Close()

	readSeg := func(f *os.File, start, end uint64) []byte {
		if end <= start {
			return nil
		}
		buf := make([]byte, end-start)
		if _, err := f.ReadAt(buf, int64(start)); err != nil {
			t.Fatalf("ReadAt [%d,%d): %v", start, end, err)
		}
		return buf
	}

	// Segment before the patched pointer.
	if patchAt > 0 {
		inSeg := readSeg(fIn, 0, patchAt)
		outSeg := readSeg(fOut, 0, patchAt)
		if !bytes.Equal(inSeg, outSeg) {
			t.Fatalf("prefix [0,%d) differs between input and output", patchAt)
		}
	}
	// Segment after the patched pointer, up to cutoff.
	afterPatch := patchAt + ptrSize
	if afterPatch < cutoff {
		inSeg := readSeg(fIn, afterPatch, cutoff)
		outSeg := readSeg(fOut, afterPatch, cutoff)
		if !bytes.Equal(inSeg, outSeg) {
			t.Fatalf("prefix [%d,%d) (after patched pointer) differs", afterPatch, cutoff)
		}
	}
}

func TestNDPI_StripAndReplace(t *testing.T) {
	src := filepath.Join("..", "..", "sample_files", "ndpi", "CMU-1.ndpi")
	if _, err := os.Stat(src); err != nil {
		t.Skipf("fixture absent: %v", err)
	}

	// strip: blank the label region of the macro; output must re-open and the
	// label region (macro) must still resolve.
	stripOut := filepath.Join(t.TempDir(), "stripped.ndpi")
	if err := redact.Strip(src, stripOut, false); err != nil {
		t.Fatalf("Strip: %v", err)
	}
	if _, _, err := redact.LabelDims(stripOut); err != nil {
		t.Fatalf("LabelDims(stripped) failed — output not re-openable: %v", err)
	}
	assertNDPIPyramidIdentical(t, src, stripOut)
	assertMacroChanged(t, src, stripOut)

	// replace: composite a test label into the region.
	repOut := filepath.Join(t.TempDir(), "relabeled.ndpi")
	if _, err := redact.Replace(src, repOut, redact.ReplaceOptions{Image: newTestLabel()}); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	assertNDPIPyramidIdentical(t, src, repOut)
	assertMacroChanged(t, src, repOut)
}

// assertMacroChanged verifies the macro strip bytes differ (redaction occurred).
func assertMacroChanged(t *testing.T, in, out string) {
	t.Helper()
	readMacro := func(path string) []byte {
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		file, err := tifflow.Parse(f)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		for _, ifd := range file.IFDs {
			if mag, ok := ifd.FloatValue(65421, file.Header.ByteOrder); ok && mag == -1.0 {
				offs, _ := ifd.UintArray(tifflow.TagStripOffsets, file.Header.ByteOrder)
				cnts, _ := ifd.UintArray(tifflow.TagStripByteCounts, file.Header.ByteOrder)
				if len(offs) == 1 && len(cnts) == 1 {
					b := make([]byte, cnts[0])
					if _, err := f.ReadAt(b, int64(offs[0])); err != nil {
						t.Fatal(err)
					}
					return b
				}
			}
		}
		t.Fatal("macro strip not found")
		return nil
	}
	if bytes.Equal(readMacro(in), readMacro(out)) {
		t.Fatalf("macro strip unchanged — redaction did not modify the macro")
	}
}

// assertNDPIPyramidIdentical verifies the prefix up to the macro strip is
// byte-identical (the pyramid was not touched by the region edit).
func assertNDPIPyramidIdentical(t *testing.T, in, out string) {
	t.Helper()
	fIn, err := os.Open(in)
	if err != nil {
		t.Fatal(err)
	}
	defer fIn.Close()
	file, err := tifflow.Parse(fIn)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var cutoff uint64
	for _, ifd := range file.IFDs {
		mag, ok := ifd.FloatValue(65421, file.Header.ByteOrder)
		if ok && mag == -1.0 {
			continue // skip the macro itself
		}
		offs, o := ifd.UintArray(tifflow.TagStripOffsets, file.Header.ByteOrder)
		cnts, c := ifd.UintArray(tifflow.TagStripByteCounts, file.Header.ByteOrder)
		if o && c && len(offs) == 1 && len(cnts) == 1 {
			if end := offs[0] + cnts[0]; end > cutoff {
				cutoff = end
			}
		}
	}
	if cutoff == 0 {
		t.Skip("no pyramid strips located")
	}
	inB := make([]byte, cutoff)
	if _, err := fIn.ReadAt(inB, 0); err != nil {
		t.Fatal(err)
	}
	fOut, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer fOut.Close()
	outB := make([]byte, cutoff)
	if _, err := fOut.ReadAt(outB, 0); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(inB, outB) {
		t.Fatalf("pyramid prefix [0,%d) differs after NDPI region redaction", cutoff)
	}
}
