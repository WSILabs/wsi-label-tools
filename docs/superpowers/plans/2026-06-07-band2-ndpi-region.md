# Band 2 — NDPI Region strategy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Redact the NDPI label — which has no IFD of its own; it's the **left ~30% of the macro image** — by decoding the macro JPEG, blanking (strip) or compositing an anonymized label (replace) into that region, re-encoding, and splicing the new JPEG back in while preserving the macro IFD's NDPI tags. Pure-Go (stdlib `image/jpeg`), pyramid never touched.

**Architecture:** Introduce the **Region strategy** alongside the existing Splice strategy, selected by `Target.Representation` (`SeparableIFD` → Splice; `EmbeddedRegion` → Region). The NDPI classifier emits a `RoleLabel` target with `EmbeddedRegion` (host = macro IFD, region = left crop). The Region write path needs a new `tifflow` capability: **re-emit a target IFD preserving its tags but substituting its strip data** (reuses the existing reemit/splice machinery). No role door (NDPI acts on `RoleLabel`, just via the Region path).

**Tech Stack:** Go 1.26, `internal/tifflow` (splice + new strip-substitution), `internal/redact`, stdlib `image/jpeg`; `opentile-go` (test-only oracle, presence only — NDPI exposes no IFD offset). Fixture: `sample_files/ndpi/CMU-1.ndpi` (in wsi-fixtures v2; parses cleanly in tifflow).

**Scope guardrail:** NDPI files our `tifflow.Parse` already handles (CMU-1.ndpi does). Broader NDPI tifflow hardening (nonstandard tag types / offset reuse seen on *other* samples) is explicitly **out of scope** — surface a clean error, defer to its own task with fixtures.

**Module path:** `github.com/cornish/wsi-label-tools`. **Branch:** create `band2-ndpi-region` off `main`.

**Reference spec:** `docs/superpowers/specs/2026-06-06-multi-format-label-redaction-design.md`.

**Grounding facts (probed from CMU-1.ndpi):** 5 IFDs, classic TIFF. IFDs 0–3 = pyramid (tag 65421 magnification > 0); **IFD 4 = macro, 1191×408, single JPEG strip, tag 65421 == -1.0**. Tag 65420 (FileFormat) present on all IFDs. Tag 65421 is an inline FLOAT32. opentile's synthesized label is 352×408 ≈ left 0.3 × 1191.

---

## File structure (end state)

```
internal/tifflow/ifd.go          MODIFY — add FloatValue(tag) (inline FLOAT32 reader)
internal/tifflow/splice.go       MODIFY — add SpliceReplaceStripData mode + reemit-with-override
internal/tifflow/splice_test.go  MODIFY — strip-substitution test (synthetic stripped TIFF)
internal/redact/redact.go        MODIFY — dispatch Splice vs Region on Target.Representation
internal/redact/strategy_region.go  NEW — Region strategy: decode→blank/composite→encode→splice
internal/redact/format/ndpi.go   NEW — NDPI classifier (detect + macro→EmbeddedRegion label target)
internal/redact/format/ndpi_test.go NEW — detect/classify unit test
internal/redact/redact_test.go   MODIFY — NDPI strip+replace e2e
test/oracle/classify_oracle_test.go MODIFY — add ndpi to presence corpus
README.md / docs/ROADMAP.md      MODIFY — NDPI done
```

---

## Task 1: tifflow FLOAT32 tag reader

**Files:** Modify `internal/tifflow/ifd.go`; Test `internal/tifflow/ifd_test.go`

NDPI's magnification tag (65421) is an inline FLOAT (TIFF type 11). tifflow has `UintValue`/`StringValue` but no float reader.

- [ ] **Step 1: Write the failing test**

Append to `internal/tifflow/ifd_test.go`:

```go
func TestFloatValue_InlineFloat32(t *testing.T) {
	// Build an IFD entry: tag 65421, type FLOAT (11), count 1, inline value -1.0.
	le := binary.LittleEndian
	raw := make([]byte, 4)
	le.PutUint32(raw, math.Float32bits(-1.0))
	ifd := &IFD{Entries: []IFDEntry{{
		Tag: 65421, Type: TypeFloat, Count: 1, Inline: true, RawValueField: raw,
	}}}
	got, ok := ifd.FloatValue(65421, le)
	if !ok || got != -1.0 {
		t.Fatalf("FloatValue = %v, ok=%v; want -1.0,true", got, ok)
	}
	if _, ok := ifd.FloatValue(99999, le); ok {
		t.Fatalf("missing tag should return ok=false")
	}
}
```

Ensure the test file imports `encoding/binary` and `math`.

- [ ] **Step 2: Run to confirm fail**

Run: `cd /Volumes/Ext/GitHub/wsi-label-tools && go test ./internal/tifflow/ -run TestFloatValue`
Expected: FAIL — `FloatValue` undefined.

- [ ] **Step 3: Implement `FloatValue`**

Add to `internal/tifflow/ifd.go` (near `UintValue`). First confirm the `IFDEntry` field that holds the inline value bytes (the codebase uses `RawValueField` for inline entries — verify by reading `ifd.go`; if the field name differs, use the actual one):

```go
// FloatValue returns the value of an inline FLOAT (type 11) tag as float64.
// Only handles count==1 inline floats (sufficient for NDPI magnification).
func (ifd *IFD) FloatValue(tag uint16, order binary.ByteOrder) (float64, bool) {
	for _, e := range ifd.Entries {
		if e.Tag != tag {
			continue
		}
		if e.Type != TypeFloat || e.Count != 1 || !e.Inline || len(e.RawValueField) < 4 {
			return 0, false
		}
		return float64(math.Float32frombits(order.Uint32(e.RawValueField[:4]))), true
	}
	return 0, false
}
```

Add `"math"` to `ifd.go` imports if not present.

- [ ] **Step 4: Run + commit**

Run: `go test ./internal/tifflow/ -run TestFloatValue -v` → PASS; `go build ./...` → PASS.
```bash
git add internal/tifflow/ifd.go internal/tifflow/ifd_test.go
git commit -m "tifflow: add FloatValue (inline FLOAT32 tag reader) for NDPI magnification"
```

---

## Task 2: tifflow strip-data substitution splice

**Files:** Modify `internal/tifflow/splice.go`; Test `internal/tifflow/splice_test.go`

Add a splice mode that re-emits the target IFD **preserving all its tags** but with its strip data replaced by caller-supplied bytes (and `StripByteCounts` updated). This is the Region strategy's write path; it reuses the existing prefix-copy + reemit machinery.

**Design:** add a new `SpliceMode` and a `StripData [][]byte` input; the target IFD is re-emitted (not replaced with a new IFD) via the existing `reemitIFD`, extended to accept override strip bytes.

- [ ] **Step 1: Write the failing test (synthetic stripped TIFF, no giant fixture)**

Append to `internal/tifflow/splice_test.go`. This builds a small stripped TIFF with `golang.org/x/image/tiff` (already a dependency), parses it, substitutes the strip data, and verifies the result:

```go
func TestSpliceReplaceStripData(t *testing.T) {
	// Build a small stripped RGB TIFF in memory.
	src := image.NewRGBA(image.Rect(0, 0, 16, 8))
	for i := range src.Pix {
		src.Pix[i] = byte(i)
	}
	var buf bytes.Buffer
	if err := xtiff.Encode(&buf, src, &xtiff.Options{Compression: xtiff.Uncompressed}); err != nil {
		t.Fatalf("encode: %v", err)
	}
	dir := t.TempDir()
	inPath := filepath.Join(dir, "in.tiff")
	if err := os.WriteFile(inPath, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	// Parse and locate the single image IFD (index 0) and its strip layout.
	f, err := os.Open(inPath)
	if err != nil {
		t.Fatal(err)
	}
	file, err := Parse(f)
	f.Close()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	// New strip data: a different length payload.
	newStrips := [][]byte{[]byte("REDACTED-STRIP-DATA-XYZ")}

	outPath := filepath.Join(dir, "out.tiff")
	if err := Splice(SpliceParams{
		InPath: inPath, OutPath: outPath, File: file,
		Mode: SpliceReplaceStripData, TargetIdx: 0, StripData: newStrips,
	}); err != nil {
		t.Fatalf("Splice: %v", err)
	}

	// Re-parse the output; the IFD's dims/tags must be preserved and its strip
	// bytes must equal newStrips.
	g, err := os.Open(outPath)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	out, err := Parse(g)
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	w, _ := out.IFDs[0].UintValue(TagImageWidth, out.Header.ByteOrder)
	h, _ := out.IFDs[0].UintValue(TagImageLength, out.Header.ByteOrder)
	if w != 16 || h != 8 {
		t.Fatalf("dims changed: %dx%d", w, h)
	}
	offs, _ := out.IFDs[0].UintArray(TagStripOffsets, out.Header.ByteOrder)
	cnts, _ := out.IFDs[0].UintArray(TagStripByteCounts, out.Header.ByteOrder)
	if len(offs) != 1 || len(cnts) != 1 || cnts[0] != uint64(len(newStrips[0])) {
		t.Fatalf("strip table not updated: offs=%v cnts=%v", offs, cnts)
	}
	got := make([]byte, cnts[0])
	if _, err := g.ReadAt(got, int64(offs[0])); err != nil {
		t.Fatal(err)
	}
	if string(got) != string(newStrips[0]) {
		t.Fatalf("strip data = %q, want %q", got, newStrips[0])
	}
}
```

Add imports to `splice_test.go` as needed: `bytes`, `image`, `os`, `path/filepath`, `testing`, and `xtiff "golang.org/x/image/tiff"`. (x/image is already required by the module.)

> Note: x/image/tiff writes a single-strip or few-strip uncompressed TIFF; the test asserts the strip table has exactly the strips the writer produced. If x/image emits multiple strips, set `newStrips` to match that count (one payload per original strip), or set `RowsPerStrip` via options to force a single strip. Adjust the test to the writer's actual strip count — keep the assertion (counts updated, bytes match) intact.

- [ ] **Step 2: Run to confirm fail**

Run: `go test ./internal/tifflow/ -run TestSpliceReplaceStripData`
Expected: FAIL — `SpliceReplaceStripData` undefined and `SpliceParams.StripData` missing.

- [ ] **Step 3: Implement**

In `internal/tifflow/splice.go`:

1. Add the mode constant to the `SpliceMode` block:
```go
	// SpliceReplaceStripData re-emits TargetIdx preserving all its tags but
	// with its strip/tile data replaced by SpliceParams.StripData (one entry
	// per strip), updating StripByteCounts. Used by the Region strategy.
	SpliceReplaceStripData
```

2. Add a field to `SpliceParams`:
```go
	// StripData, for SpliceReplaceStripData, is the replacement strip data
	// (one []byte per strip) for the target IFD.
	StripData [][]byte
```

3. In `doSplice`, handle the new mode in the `switch p.Mode` that computes `firstTail` (treat like `SpliceReplace`: `firstTail = p.TargetIdx + 1`; `minTailOwner = p.TargetIdx` for the dominance/cutoff check), and in the emit switch, instead of emitting a `Replacement`, re-emit the target IFD with the override strips. The cleanest wiring: in the loop that emits IFDs, when `p.Mode == SpliceReplaceStripData`, call a reemit that takes the override for the target. Concretely, replace the per-mode emit section so that for `SpliceReplaceStripData` it emits the target via `reemitIFDOverride(out, in, p.File, p.TargetIdx, p.StripData)` first, then the tail (`firstTail..n`) via the normal `reemitIFD`.

4. Add `reemitIFDOverride` — a copy of `reemitIFD` that, instead of reading the original strip bytes from `in`, writes `override[i]` for each strip, and computes the new `StripByteCounts` from `len(override[i])`. The simplest faithful implementation: generalize `reemitIFD` to take an `override [][]byte` parameter (nil = current behavior) and:
   - When writing strip data: if `override != nil`, write `override[i]` and record `newCount[i] = len(override[i])`; else read+copy from input as today.
   - When emitting the `StripByteCounts`/`TileByteCounts` entry: if `override != nil`, write a freshly-built value (LONG array, or LONG8 for BigTIFF) of the new counts — inline if it fits the value field (single strip with a 32-bit count does), else out-of-line. Mirror the existing strip-offsets value-field logic for sizing/placement.
   - Keep all other entries (incl. NDPI private tags) copied verbatim, exactly as `reemitIFD` already does.
   Then make `reemitIFD(out, in, file, idx)` call `reemitIFDOverride(out, in, file, idx, nil)` so existing callers are unchanged.

Implement against the test; the test defines correctness (dims/tags preserved, strip table updated, bytes match, output re-parses). Run `go test ./internal/tifflow/ -run TestSpliceReplaceStripData -v` until PASS, plus the full `go test ./internal/tifflow/ -count=1` to confirm existing splice tests still pass (the `reemitIFD` refactor must not regress them).

- [ ] **Step 4: Commit**

```bash
git add internal/tifflow/splice.go internal/tifflow/splice_test.go
git commit -m "tifflow: SpliceReplaceStripData — re-emit IFD preserving tags, substitute strip data"
```

---

## Task 3: orchestrator dispatch on Representation

**Files:** Modify `internal/redact/redact.go`; Test via existing redact tests (unchanged behavior for SeparableIFD).

Currently `Replace`/`Strip` always splice. Make them dispatch on the located target's `Representation`.

- [ ] **Step 1: Extract the splice path and add Region dispatch**

In `redact.go`, after `labelT := findRole(targets, RoleLabel)` and the existing label-found / strict / add logic, the actual write currently calls `tifflow.Splice`. Refactor so that **when `labelT != nil && labelT.Representation == EmbeddedRegion`**, the Region strategy handles both Replace and Strip; otherwise the existing Splice path runs.

Concretely, in `Replace`, replace the final splice section with:

```go
	if labelT != nil && labelT.Representation == EmbeddedRegion {
		return regionReplace(inPath, outPath, file, c, labelT, opts)
	}
	// ... existing SeparableIFD splice path (SpliceReplace / insert / append) ...
```

and in `Strip`:

```go
	if labelT.Representation == EmbeddedRegion {
		return regionStrip(inPath, outPath, file, labelT, fsync)
	}
	// ... existing SpliceRemove path ...
```

`regionReplace`/`regionStrip` live in `strategy_region.go` (Task 4). Keep the `EmbeddedRegion` branches before the SeparableIFD logic. For `Replace` with an EmbeddedRegion target there is always a target (NDPI always has a macro), so `result.Added` is false.

- [ ] **Step 2: Verify no regression**

Run: `go test ./internal/redact/... -count=1` (SeparableIFD formats unaffected — all SVS/generic/Philips tests still pass). It won't build until Task 4 adds `regionReplace`/`regionStrip`; if you want this task to compile standalone, add temporary stub functions in `strategy_region.go` returning `fmt.Errorf("not implemented")` and flesh them out in Task 4. (Recommended: do Tasks 3+4 together and commit once.)

- [ ] **Step 3: Commit (with Task 4)**

Commit together with Task 4 so the tree always builds.

---

## Task 4: Region strategy

**Files:** Create `internal/redact/strategy_region.go`

Decode the host (macro) JPEG, blank (strip) or composite the anonymized label (replace) into the target region, re-encode JPEG, and splice via `SpliceReplaceStripData`.

- [ ] **Step 1: Implement**

```go
package redact

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"os"

	xdraw "golang.org/x/image/draw"

	"github.com/cornish/wsi-label-tools/internal/tifflow"
)

// hostJPEG reads and decodes the host IFD's single JPEG strip.
func hostJPEG(inPath string, file *tifflow.File, hostIdx int) (image.Image, []uint64, []uint64, error) {
	ifd := file.IFDs[hostIdx]
	offs, ok := ifd.UintArray(tifflow.TagStripOffsets, file.Header.ByteOrder)
	cnts, ok2 := ifd.UintArray(tifflow.TagStripByteCounts, file.Header.ByteOrder)
	if !ok || !ok2 || len(offs) != 1 || len(cnts) != 1 {
		return nil, nil, nil, fmt.Errorf("redact: region host IFD is not a single strip")
	}
	f, err := os.Open(inPath)
	if err != nil {
		return nil, nil, nil, err
	}
	defer f.Close()
	raw := make([]byte, cnts[0])
	if _, err := f.ReadAt(raw, int64(offs[0])); err != nil {
		return nil, nil, nil, fmt.Errorf("redact: read host strip: %w", err)
	}
	img, err := jpeg.Decode(bytes.NewReader(raw))
	if err != nil {
		// The macro must be a stdlib-decodable codec (baseline JPEG). If not,
		// region redaction is out of pure-Go scope.
		return nil, nil, nil, fmt.Errorf("redact: cannot decode host image (region redaction needs baseline JPEG): %w", err)
	}
	return img, offs, cnts, nil
}

// encodeRegionResult re-encodes the modified host image as JPEG and splices it
// into outPath, preserving the host IFD's tags.
func writeRegion(inPath, outPath string, file *tifflow.File, hostIdx int, modified image.Image, fsync bool) error {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, modified, &jpeg.Options{Quality: 90}); err != nil {
		return fmt.Errorf("redact: encode host: %w", err)
	}
	return tifflow.Splice(tifflow.SpliceParams{
		InPath: inPath, OutPath: outPath, File: file,
		Mode: tifflow.SpliceReplaceStripData, TargetIdx: hostIdx,
		StripData: [][]byte{buf.Bytes()}, Fsync: fsync,
	})
}

// toRGBA returns a mutable RGBA copy of img.
func toRGBA(img image.Image) *image.RGBA {
	b := img.Bounds()
	dst := image.NewRGBA(b)
	draw.Draw(dst, b, img, b.Min, draw.Src)
	return dst
}

// regionStrip blanks the target region of the host image with the default BG.
func regionStrip(inPath, outPath string, file *tifflow.File, t *Target, fsync bool) error {
	img, _, _, err := hostJPEG(inPath, file, t.HostIFDIndex)
	if err != nil {
		return err
	}
	dst := toRGBA(img)
	bg := image.NewUniform(parchment) // neutral fill
	draw.Draw(dst, t.Region.Add(img.Bounds().Min), bg, image.Point{}, draw.Src)
	return writeRegion(inPath, outPath, file, t.HostIFDIndex, dst, fsync)
}

// regionReplace composites a prepared label image into the target region.
func regionReplace(inPath, outPath string, file *tifflow.File, c Classifier, t *Target, opts ReplaceOptions) (*Result, error) {
	if opts.BG == (ReplaceOptions{}).BG {
		opts.BG = c.DefaultLabelBG()
	}
	img, _, _, err := hostJPEG(inPath, file, t.HostIFDIndex)
	if err != nil {
		return nil, err
	}
	dst := toRGBA(img)
	region := t.Region.Add(img.Bounds().Min)
	// Scale the supplied label to fill the region (stretch — the region is the
	// label's footprint within the macro).
	xdraw.CatmullRom.Scale(dst, region, opts.Image, opts.Image.Bounds(), xdraw.Over, nil)
	if err := writeRegion(inPath, outPath, file, t.HostIFDIndex, dst, opts.Fsync); err != nil {
		return nil, err
	}
	return &Result{}, nil
}

// parchment is the neutral fill for blanked label regions.
var parchment = colorRGBA(0xF5, 0xF5, 0xE6, 0xFF)
```

Add a small helper (or reuse an existing color constructor). If `colorRGBA` doesn't exist, replace `parchment` with `color.RGBA{0xF5, 0xF5, 0xE6, 0xFF}` and import `image/color`. Keep it simple.

> Region coordinates: `Target.Region` is relative to the host image's `(0,0)`; `.Add(img.Bounds().Min)` adapts to the decoded image's bounds (normally `(0,0)`).

- [ ] **Step 2: Build + commit (Tasks 3 + 4 together)**

Run: `go build ./...` and `go test ./internal/redact/... -count=1` (existing SeparableIFD tests still green).
```bash
git add internal/redact/redact.go internal/redact/strategy_region.go
git commit -m "redact: Region strategy (decode→blank/composite→re-encode→strip-substitute) + Representation dispatch"
```

---

## Task 5: NDPI classifier

**Files:** Create `internal/redact/format/ndpi.go`, `internal/redact/format/ndpi_test.go`

- [ ] **Step 1: Implement**

```go
package format

import (
	"image"
	"image/color"

	"github.com/cornish/wsi-label-tools/internal/label"
	"github.com/cornish/wsi-label-tools/internal/redact"
	"github.com/cornish/wsi-label-tools/internal/tifflow"
)

func init() { redact.Register(ndpi{}) }

const (
	ndpiTagFileFormat   = 65420 // FileFormat (presence marker)
	ndpiTagMagnification = 65421 // FLOAT; -1.0 = macro, -2.0 = map, >0 = level
	tagMake             = 271
	ndpiLabelCropFrac   = 0.30 // label = left 30% of the macro
)

// ndpi classifies Hamamatsu NDPI. The label has no IFD: it is the left ~30% of
// the macro (the page with magnification == -1.0). We emit a RoleLabel target
// with EmbeddedRegion (host = macro IFD, region = left crop), redacted by the
// Region strategy.
type ndpi struct{}

func (ndpi) Name() string { return "ndpi" }

func (ndpi) Detect(file *tifflow.File) bool {
	if len(file.IFDs) == 0 {
		return false
	}
	ifd0 := file.IFDs[0]
	hasFormat := false
	for _, e := range ifd0.Entries {
		if e.Tag == ndpiTagFileFormat {
			hasFormat = true
		}
	}
	_, hasMake := ifd0.StringValue(tagMake)
	return hasFormat && hasMake
}

func (n ndpi) Classify(file *tifflow.File) ([]redact.Target, error) {
	macroIdx := n.macroIFD(file)
	if macroIdx < 0 {
		return nil, nil
	}
	ifd := file.IFDs[macroIdx]
	w, _ := ifd.UintValue(tifflow.TagImageWidth, file.Header.ByteOrder)
	h, _ := ifd.UintValue(tifflow.TagImageLength, file.Header.ByteOrder)
	cropW := int(float64(w) * ndpiLabelCropFrac)
	if cropW <= 0 {
		return nil, nil
	}
	return []redact.Target{{
		Role:           redact.RoleLabel,
		Representation: redact.EmbeddedRegion,
		HostIFDIndex:   macroIdx,
		Region:         image.Rect(0, 0, cropW, int(h)),
		Width:          cropW,
		Height:         int(h),
	}}, nil
}

// macroIFD returns the index of the macro page (magnification == -1.0), or -1.
func (ndpi) macroIFD(file *tifflow.File) int {
	for i, ifd := range file.IFDs {
		if mag, ok := ifd.FloatValue(ndpiTagMagnification, file.Header.ByteOrder); ok && mag == -1.0 {
			return i
		}
	}
	return -1
}

// BuildLabelIFD is unused for NDPI (Region strategy composites into the macro),
// but Classifier requires it. Return a neutral Aperio-style label so the
// interface is satisfied; the Region path uses opts.Image directly.
func (ndpi) BuildLabelIFD(img image.Image) *tifflow.ReplacementIFD {
	return label.BuildAperioLabelIFD(img)
}

func (ndpi) DefaultLabelDims() (int, int) { return label.AperioLabelWidth, label.AperioLabelHeight }
func (ndpi) DefaultLabelBG() color.RGBA   { return label.AperioLabelBG }
```

> `BuildLabelIFD` is vestigial for NDPI (the Region strategy never calls it — it composites `opts.Image` into the macro). It's only present to satisfy the interface. If, during Band 2, you prefer to make `BuildLabelIFD` optional on the interface, that's a reasonable small refactor — but not required.

- [ ] **Step 2: Unit test**

Create `internal/redact/format/ndpi_test.go`:

```go
package format

import (
	"path/filepath"
	"testing"

	"github.com/cornish/wsi-label-tools/internal/redact"
)

func TestNDPI_DetectAndRegion(t *testing.T) {
	p := filepath.Join("..", "..", "..", "sample_files", "ndpi", "CMU-1.ndpi")
	file := parseSample(t, p) // helper from generictiff_test.go; skips if absent
	n := ndpi{}
	if !n.Detect(file) {
		t.Fatalf("ndpi did not detect CMU-1.ndpi")
	}
	targets, err := n.Classify(file)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 {
		t.Fatalf("want 1 target, got %+v", targets)
	}
	tg := targets[0]
	if tg.Role != redact.RoleLabel || tg.Representation != redact.EmbeddedRegion {
		t.Fatalf("want RoleLabel/EmbeddedRegion, got %+v", tg)
	}
	// Macro is 1191x408; label region is left 30% (~357) x full height.
	if tg.Height != 408 || tg.Width < 300 || tg.Width > 400 {
		t.Fatalf("region = %dx%d, want ~357x408", tg.Width, tg.Height)
	}
}
```

- [ ] **Step 3: Verify + commit**

Run: `./scripts/fetch-fixtures.sh` (if needed), then `go build ./... && go test ./internal/redact/format/ -run TestNDPI -v -count=1` → PASS (skips if fixture absent). `gofmt -l internal/`.
```bash
git add internal/redact/format/ndpi.go internal/redact/format/ndpi_test.go
git commit -m "redact/format: NDPI classifier (macro magnification -1.0 → EmbeddedRegion label target)"
```

---

## Task 6: NDPI end-to-end + oracle presence

**Files:** Modify `internal/redact/redact_test.go`, `test/oracle/classify_oracle_test.go`

- [ ] **Step 1: End-to-end strip + replace**

Append to `internal/redact/redact_test.go`:

```go
func TestNDPI_StripAndReplace(t *testing.T) {
	src := filepath.Join("..", "..", "sample_files", "ndpi", "CMU-1.ndpi")
	if _, err := os.Stat(src); err != nil {
		t.Skipf("fixture absent: %v", err)
	}

	// strip: blank the label region of the macro; output must re-open and
	// LabelDims must still resolve (the macro/region still exists).
	stripOut := filepath.Join(t.TempDir(), "stripped.ndpi")
	if err := redact.Strip(src, stripOut, false); err != nil {
		t.Fatalf("Strip: %v", err)
	}
	if _, _, err := redact.LabelDims(stripOut); err != nil {
		t.Fatalf("LabelDims(stripped) failed — output not re-openable: %v", err)
	}

	// replace: composite a test label into the region.
	repOut := filepath.Join(t.TempDir(), "relabeled.ndpi")
	if _, err := redact.Replace(src, repOut, redact.ReplaceOptions{Image: newTestLabel()}); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	// Pyramid byte-identity: bytes before the macro strip are untouched.
	assertNDPIPyramidIdentical(t, src, repOut)
}

// assertNDPIPyramidIdentical verifies the prefix up to the macro strip is
// byte-identical (the pyramid was not touched).
func assertNDPIPyramidIdentical(t *testing.T, in, out string) {
	t.Helper()
	fIn, err := os.Open(in)
	if err != nil { t.Fatal(err) }
	defer fIn.Close()
	file, err := tifflow.Parse(fIn)
	if err != nil { t.Fatalf("parse: %v", err) }
	// macro = page with the largest IFD index whose magnification is -1.0;
	// find its strip offset.
	var cutoff uint64 = 1 << 62
	for _, ifd := range file.IFDs {
		if mag, ok := ifd.FloatValue(65421, file.Header.ByteOrder); ok && mag == -1.0 {
			offs, _ := ifd.UintArray(tifflow.TagStripOffsets, file.Header.ByteOrder)
			if len(offs) == 1 && offs[0] < cutoff {
				cutoff = offs[0]
			}
		}
	}
	if cutoff == 1<<62 { t.Skip("no macro strip located") }
	inB := make([]byte, cutoff)
	if _, err := fIn.ReadAt(inB, 0); err != nil { t.Fatal(err) }
	fOut, err := os.Open(out)
	if err != nil { t.Fatal(err) }
	defer fOut.Close()
	outB := make([]byte, cutoff)
	if _, err := fOut.ReadAt(outB, 0); err != nil { t.Fatal(err) }
	if !bytes.Equal(inB, outB) {
		t.Fatalf("pyramid prefix [0,%d) differs after NDPI region redaction", cutoff)
	}
}
```

Ensure `redact_test.go` imports `bytes` and `tifflow` (it already imports tifflow + bytes from Band 1). `FloatValue` is exported from tifflow (Task 1).

> Note: the macro strip is the LAST data blob in CMU-1.ndpi, so `cutoff` ≈ 198 MB and the prefix compare reads the whole pyramid — slow but correct. If too slow in practice, compare a bounded window just before `cutoff` instead; keep it non-vacuous.

- [ ] **Step 2: Oracle presence for NDPI**

In `test/oracle/classify_oracle_test.go`, add `{"ndpi", ".ndpi"}` to the `corpus` table. `TestLabelPresenceAgrees` then checks CMU-1.ndpi: opentile reports a `label` (synthesized), and our classifier returns a RoleLabel target → both present → agree. (Our `LabelDims` returns the region dims for NDPI; presence = a RoleLabel target exists.) `TestLabelOffsetAgrees` will skip NDPI (opentile `AssociatedIFDOffset` ok=false → no offset to compare).

> Verify the presence semantics line up: `hasLabelOurs` uses `redact.LabelDims` (ok if our NDPI classify yields a RoleLabel with positive dims). `hasLabelOpentile` checks `Type()=="label"`. Both should be true for CMU-1.ndpi.

- [ ] **Step 3: Verify + commit**

Run: `go test ./... -count=1` and `(cd test/oracle && CGO_ENABLED=0 go test ./... -count=1 -v | grep -E "ndpi|PASS|FAIL|SKIP")`. The NDPI e2e + oracle presence must RUN and PASS (fixtures present).
```bash
git add internal/redact/redact_test.go test/oracle/classify_oracle_test.go
git commit -m "test: NDPI strip+replace e2e (pyramid byte-identity) + oracle presence"
```

---

## Task 7: Docs

**Files:** Modify `README.md`, `docs/ROADMAP.md`

- [ ] **Step 1: README supported-formats table** — change the NDPI row to:
```
| Hamamatsu NDPI | ✅ | Region strategy (blank/composite the macro's label crop); pure-Go JPEG |
```
(Leave BIF as Band 3, OME-TIFF/COG out of charter.)

- [ ] **Step 2: ROADMAP** — mark Band 2 done:
```
- [x] **Band 2 — NDPI (Region strategy).** Label = left ~30% of the macro;
  decode→blank/composite→re-encode→strip-substitution splice; pyramid
  untouched. New tifflow `SpliceReplaceStripData`. See
  `docs/superpowers/plans/2026-06-07-band2-ndpi-region.md`.
- [ ] **Band 3 — BIF (region + XMP scrub).** Overview label-band blanking +
  XMP barcode/metadata scrubbing (a new capability); needs its own design.
```

- [ ] **Step 3: Final verification + commit**
```bash
go build ./... && go vet ./... && go test ./... -count=1 2>&1 | grep -E "ok|FAIL"
(cd test/oracle && CGO_ENABLED=0 go test ./... -count=1 | tail -1)
gofmt -l internal/ test/
git add README.md docs/ROADMAP.md
git commit -m "docs: NDPI supported (Region strategy); Band 2 roadmap"
```

---

## Self-review checklist

- **Spec coverage:** Region strategy (T2/T3/T4) ✓, NDPI classifier with EmbeddedRegion (T5) ✓, strip + replace (T4/T6) ✓, Representation dispatch (T3) ✓, oracle presence (T6) ✓, docs (T7) ✓. tifflow hardening for other NDPI variants explicitly deferred ✓.
- **Type consistency:** `SpliceReplaceStripData` + `SpliceParams.StripData` used consistently (tifflow ↔ region strategy); `Target.HostIFDIndex`/`Region`/`EmbeddedRegion` (defined in Band 0 target.go) consumed by ndpi.go + strategy_region.go; `FloatValue` signature matches between ifd.go and ndpi.go/test.
- **No placeholders:** Task 2's intricate emit is specified test-first (the test defines correctness) with a precise implementation approach — appropriate for the one genuinely tricky piece; everything else is complete code.
- **Pure-Go / purity:** Region uses stdlib `image/jpeg` + `golang.org/x/image/draw` (already a dep). No new runtime deps; opentile-go stays test-only. The macro-not-baseline-JPEG case errors cleanly (out of pure-Go scope).

## Done criteria
- `go build/vet/test ./...` green; oracle (CGo off) green; main module still two pure-Go deps.
- `wsi-label strip`/`replace` redact CMU-1.ndpi's label region; pyramid bytes byte-identical; output re-opens.
- README/ROADMAP updated; BIF remains Band 3.
