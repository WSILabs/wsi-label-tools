# Band 0 — Redact Core Refactor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the Aperio-only `internal/aperio` + `internal/wsi.Backend` structure with a format-neutral `internal/redact` core (role-keyed `Target`, classifier registry, splice orchestration) plus an `internal/label` builder and an SVS classifier, and stand up an `opentile-go` test-only oracle — all with **zero behavior change** (existing tests + `cross_validate.py` stay green).

**Architecture:** A classifier registry (`image.RegisterFormat`-style `init()` self-registration) detects the format from an already-parsed `*tifflow.File` and returns role-keyed `Target`s. The `redact` orchestrator picks the label `Target` and drives the existing `tifflow.Splice` machinery. Format-specific label-IFD construction and defaults live behind the `Classifier` interface; SVS is the only implementation in this band. opentile-go is imported only by a separate nested test module.

**Tech Stack:** Go 1.26, `internal/tifflow` (existing byte-splicer), `github.com/hhrutter/lzw`, `golang.org/x/image`; `github.com/wsilabs/opentile-go` (test-only, `CGO_ENABLED=0`).

**Refactor-TDD note:** This is a refactor of working, tested code. The existing test suite is the regression safety net. Per task: make the change, then run `go build ./...` and `go test ./...` (plus the cross-validation gate in Task 8). New logic (core types, classifier, orchestrator, oracle) gets new tests written first where practical. Moved code keeps its existing tests, relocated and mechanically adapted.

**Module path:** `github.com/cornish/wsi-label-tools`

---

## File structure (end state of Band 0)

```
internal/label/                  NEW — format-neutral label building
  options.go                     ResizeMode + consts, PrepareOptions, ErrAspectMismatch
  resize.go                      Prepare, FitTo, rotate (moved from aperio/resize.go)
  label.go                       encode strips / predictor / LZW + Aperio constants (moved)
  build_label.go                 BuildAperioLabelIFD (moved, renamed)
  *_test.go                      moved + adapted from aperio
internal/redact/                 NEW — orchestrator + core types
  target.go                      Role, Representation, Target
  options.go                     ReplaceOptions, Result, ResizeMode alias, errors
  classifier.go                  Classifier interface + registry
  redact.go                      Replace, Strip, LabelDims, pickTargetDims
  format/
    svs.go                       SVS classifier + init() registration
    svs_test.go                  detect/classify tests (moved from aperio/detect_test.go)
  pick_target_dims_test.go       package redact (internal) — uses a stub classifier
  redact_test.go                 package redact_test (external) — end-to-end via format
cmd/wsi-label/main.go            MODIFY — blank-import format for registration
internal/cli/replace.go          MODIFY — call redact.Replace
internal/cli/strip.go            MODIFY — call redact.Strip
internal/cli/label_dims.go       MODIFY — call redact.LabelDims
internal/aperio/                 DELETED
internal/wsi/                    DELETED
test/oracle/                     NEW — separate nested module
  go.mod                         requires opentile-go + main module
  classify_oracle_test.go        cross-validate redact classification vs opentile-go
```

---

## Task 1: Create `internal/label` package (move label building out of aperio)

**Files:**
- Create: `internal/label/options.go`
- Create: `internal/label/label.go` (move of `internal/aperio/label.go`)
- Create: `internal/label/resize.go` (move of `internal/aperio/resize.go`)
- Create: `internal/label/build_label.go` (move of `internal/aperio/build_label.go`)

- [ ] **Step 1: Create `internal/label/options.go`**

```go
// Package label builds format-neutral replacement label images and the
// format-specific TIFF IFDs that carry them.
package label

import (
	"errors"
	"image/color"
)

// ResizeMode selects how a caller-supplied label is fit to target dims.
type ResizeMode int

const (
	// ResizeFit preserves aspect ratio, letterboxing with BG.
	ResizeFit ResizeMode = iota
	// ResizeStretch scales non-uniformly to fill the target exactly.
	ResizeStretch
	// ResizeNone rejects input not already at target dims.
	ResizeNone
)

// PrepareOptions are the knobs for Prepare.
type PrepareOptions struct {
	BG     color.RGBA
	Rotate int
	Resize ResizeMode
	Force  bool
}

// ErrAspectMismatch is returned by Prepare when ResizeFit sees a source whose
// aspect ratio differs from the target by more than the safety threshold and
// Force is not set.
var ErrAspectMismatch = errors.New("label: aspect ratio mismatch (use force to override)")
```

- [ ] **Step 2: Move `aperio/label.go` → `internal/label/label.go`**

```bash
git mv internal/aperio/label.go internal/label/label.go
```

Then edit `internal/label/label.go`: change the package clause `package aperio` → `package label`. No other changes — it has no `wsi` import. (Keeps `EncodeLabelStrips`, `AperioLabelWidth/Height`, `AperioRowsPerStrip`, `AperioLabelDescription`, `AperioLabelBG`, and the unexported helpers.)

- [ ] **Step 3: Move `aperio/resize.go` → `internal/label/resize.go` and drop the wsi dependency**

```bash
git mv internal/aperio/resize.go internal/label/resize.go
```

Edit `internal/label/resize.go`:
- Package clause `package aperio` → `package label`.
- Remove the import line `"github.com/cornish/wsi-label-tools/internal/wsi"`.
- Rename the function `PrepareLabel` → `Prepare` and change its signature param type `opts wsi.ReplaceOptions` → `opts PrepareOptions`.
- Replace `wsi.ResizeNone` → `ResizeNone`, `wsi.ResizeStretch` → `ResizeStretch`, `wsi.ResizeFit` → `ResizeFit`, `wsi.ErrAspectMismatch` → `ErrAspectMismatch`.

The resulting function header:

```go
func Prepare(src image.Image, width, height int, opts PrepareOptions) (*image.RGBA, error) {
```

- [ ] **Step 4: Move `aperio/build_label.go` → `internal/label/build_label.go` and rename the builder**

```bash
git mv internal/aperio/build_label.go internal/label/build_label.go
```

Edit `internal/label/build_label.go`:
- Package clause `package aperio` → `package label`.
- Rename the exported function `BuildLabelIFD` → `BuildAperioLabelIFD` (the doc comment too). Body unchanged.

- [ ] **Step 5: Build the label package**

Run: `go build ./internal/label/`
Expected: PASS (compiles). The old `internal/aperio` will NOT compile yet — that is fixed in Task 7. Do not run `go build ./...` here.

- [ ] **Step 6: Commit**

```bash
git add internal/label/
git commit -m "label: extract format-neutral label builder from aperio"
```

---

## Task 2: Create `internal/redact` core types and registry

**Files:**
- Create: `internal/redact/target.go`
- Create: `internal/redact/options.go`
- Create: `internal/redact/classifier.go`

- [ ] **Step 1: Create `internal/redact/target.go`**

```go
// Package redact locates and removes/replaces the PHI-bearing label image in
// a WSI file. Format-specific knowledge lives behind the Classifier interface;
// the byte-level write is delegated to internal/tifflow.
package redact

import "image"

// Role identifies the kind of associated image a Target refers to. Only
// RoleLabel is acted on today; the others exist so classifiers can report
// them (and future work can redact them) without a rearchitecture.
type Role int

const (
	RoleLabel Role = iota
	RoleOverview
	RoleThumbnail
)

func (r Role) String() string {
	switch r {
	case RoleLabel:
		return "label"
	case RoleOverview:
		return "overview"
	case RoleThumbnail:
		return "thumbnail"
	default:
		return "unknown"
	}
}

// Representation describes how a target image is physically stored, which
// selects the redaction strategy.
type Representation int

const (
	// SeparableIFD: the image is its own IFD with its own byte range; it can
	// be removed/replaced by a byte-exact splice.
	SeparableIFD Representation = iota
	// EmbeddedRegion: the image is a region of a larger associated image and
	// must be redacted by decode/blank/re-encode. (Not produced in Band 0.)
	EmbeddedRegion
)

// Target is one classified associated image.
type Target struct {
	Role           Role
	Representation Representation
	// IFDIndex is the chain index of the target IFD (SeparableIFD). It is
	// passed directly as tifflow.SpliceParams.TargetIdx.
	IFDIndex int
	// HostIFDIndex / Region describe an EmbeddedRegion (unused in Band 0).
	HostIFDIndex int
	Region       image.Rectangle
	// Width, Height are the target image's pixel dimensions.
	Width, Height int
}
```

- [ ] **Step 2: Create `internal/redact/options.go`**

```go
package redact

import (
	"errors"
	"image"
	"image/color"

	"github.com/cornish/wsi-label-tools/internal/label"
)

// ResizeMode is re-exported from internal/label so CLI and callers depend
// only on redact.
type ResizeMode = label.ResizeMode

const (
	ResizeFit     = label.ResizeFit
	ResizeStretch = label.ResizeStretch
	ResizeNone    = label.ResizeNone
)

var (
	// ErrNoLabel is returned when a redaction target of RoleLabel is required
	// but the file has none.
	ErrNoLabel = errors.New("redact: file has no label")
	// ErrFormatUnsupported is returned when no registered classifier matches.
	ErrFormatUnsupported = errors.New("redact: format not recognized")
)

// ReplaceOptions bundles per-call knobs for Replace.
type ReplaceOptions struct {
	Image         image.Image
	BG            color.RGBA
	Rotate        int
	Resize        ResizeMode
	Force         bool
	StrictReplace bool
	Fsync         bool
	TargetWidth   int
	TargetHeight  int
}

// Result carries metadata about a completed Replace.
type Result struct {
	// Added is true when the file had no label and one was added.
	Added bool
}
```

- [ ] **Step 3: Create `internal/redact/classifier.go`**

```go
package redact

import (
	"image"
	"image/color"

	"github.com/cornish/wsi-label-tools/internal/tifflow"
)

// Classifier is implemented by each format backend. Detect and Classify
// operate on an already-parsed file (the orchestrator parses once). The
// Build/Default methods supply the format-specific replacement-label spec.
type Classifier interface {
	Name() string
	Detect(file *tifflow.File) bool
	Classify(file *tifflow.File) ([]Target, error)
	BuildLabelIFD(img image.Image) *tifflow.ReplacementIFD
	DefaultLabelDims() (w, h int)
	DefaultLabelBG() color.RGBA
}

var classifiers []Classifier

// Register adds a classifier to the registry. Format packages call this from
// init(); the binary blank-imports those packages to trigger registration.
func Register(c Classifier) { classifiers = append(classifiers, c) }

// detectClassifier returns the first registered classifier that matches.
func detectClassifier(file *tifflow.File) (Classifier, bool) {
	for _, c := range classifiers {
		if c.Detect(file) {
			return c, true
		}
	}
	return nil, false
}
```

- [ ] **Step 4: Build the redact package (types only)**

Run: `go build ./internal/redact/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/redact/target.go internal/redact/options.go internal/redact/classifier.go
git commit -m "redact: core types, options, and classifier registry"
```

---

## Task 3: Create the `redact` orchestrator

**Files:**
- Create: `internal/redact/redact.go`
- Test: `internal/redact/pick_target_dims_test.go`

- [ ] **Step 1: Create `internal/redact/redact.go`**

```go
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
		if opts.StrictReplace {
			return nil, ErrNoLabel
		}
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
```

- [ ] **Step 2: Write the failing test for `pickTargetDims` (internal, with a stub classifier)**

Create `internal/redact/pick_target_dims_test.go`:

```go
package redact

import (
	"image"
	"image/color"
	"testing"

	"github.com/cornish/wsi-label-tools/internal/tifflow"
)

// stubClassifier supplies deterministic defaults for unit-testing the
// orchestrator without a real format.
type stubClassifier struct{}

func (stubClassifier) Name() string                                       { return "stub" }
func (stubClassifier) Detect(*tifflow.File) bool                          { return false }
func (stubClassifier) Classify(*tifflow.File) ([]Target, error)          { return nil, nil }
func (stubClassifier) BuildLabelIFD(image.Image) *tifflow.ReplacementIFD { return nil }
func (stubClassifier) DefaultLabelDims() (int, int)                       { return 1200, 848 }
func (stubClassifier) DefaultLabelBG() color.RGBA                         { return color.RGBA{} }

func TestPickTargetDims_ExplicitOverride(t *testing.T) {
	w, h := pickTargetDims(&Target{Width: 100, Height: 50}, stubClassifier{},
		ReplaceOptions{TargetWidth: 640, TargetHeight: 480})
	if w != 640 || h != 480 {
		t.Fatalf("override: got %dx%d, want 640x480", w, h)
	}
}

func TestPickTargetDims_UsesExistingLabel(t *testing.T) {
	w, h := pickTargetDims(&Target{Width: 387, Height: 463}, stubClassifier{}, ReplaceOptions{})
	if w != 387 || h != 463 {
		t.Fatalf("existing label: got %dx%d, want 387x463", w, h)
	}
}

func TestPickTargetDims_FallsBackToDefault(t *testing.T) {
	w, h := pickTargetDims(nil, stubClassifier{}, ReplaceOptions{})
	if w != 1200 || h != 848 {
		t.Fatalf("default: got %dx%d, want 1200x848", w, h)
	}
}
```

- [ ] **Step 3: Run the test**

Run: `go test ./internal/redact/ -run TestPickTargetDims -v`
Expected: PASS (3 subtests). If it fails to compile, fix the orchestrator/types first.

- [ ] **Step 4: Commit**

```bash
git add internal/redact/redact.go internal/redact/pick_target_dims_test.go
git commit -m "redact: orchestrator (Replace/Strip/LabelDims) + pickTargetDims tests"
```

---

## Task 4: SVS classifier in `internal/redact/format`

**Files:**
- Create: `internal/redact/format/svs.go`
- Test: `internal/redact/format/svs_test.go` (ported from `internal/aperio/detect_test.go`)

- [ ] **Step 1: Create `internal/redact/format/svs.go`**

```go
// Package format holds per-format detectors/classifiers. Each registers
// itself with internal/redact via init(); the binary blank-imports this
// package to make the formats available.
package format

import (
	"image"
	"image/color"
	"strings"

	"github.com/cornish/wsi-label-tools/internal/label"
	"github.com/cornish/wsi-label-tools/internal/redact"
	"github.com/cornish/wsi-label-tools/internal/tifflow"
)

func init() { redact.Register(svs{}) }

// svs is the Aperio SVS classifier. Detection keys on IFD 0's
// ImageDescription "Aperio" prefix; label/overview are located by the
// OpenSlide-compatible rule: a non-tiled IFD other than IFD 1 (thumbnail)
// whose ImageDescription second line starts with "label"/"macro".
type svs struct{}

func (svs) Name() string { return "aperio" }

func (svs) Detect(file *tifflow.File) bool {
	if len(file.IFDs) == 0 {
		return false
	}
	desc, ok := file.IFDs[0].StringValue(tifflow.TagImageDescription)
	if !ok {
		return false
	}
	return strings.HasPrefix(desc, "Aperio")
}

func (s svs) Classify(file *tifflow.File) ([]redact.Target, error) {
	var targets []redact.Target
	if t, ok := s.find(file, redact.RoleLabel, "label"); ok {
		targets = append(targets, t)
	}
	if t, ok := s.find(file, redact.RoleOverview, "macro"); ok {
		targets = append(targets, t)
	}
	return targets, nil
}

// find returns the first associated IFD whose ImageDescription second line
// starts with prefix, as a Target of the given role.
func (svs) find(file *tifflow.File, role redact.Role, prefix string) (redact.Target, bool) {
	for i, ifd := range file.IFDs {
		if i == 1 || hasTileTags(ifd) {
			continue
		}
		desc, ok := ifd.StringValue(tifflow.TagImageDescription)
		if !ok {
			continue
		}
		lines := strings.SplitN(desc, "\n", 3)
		if len(lines) < 2 || !strings.HasPrefix(lines[1], prefix) {
			continue
		}
		w, _ := ifd.UintValue(tifflow.TagImageWidth, file.Header.ByteOrder)
		h, _ := ifd.UintValue(tifflow.TagImageLength, file.Header.ByteOrder)
		return redact.Target{
			Role: role, Representation: redact.SeparableIFD,
			IFDIndex: i, Width: int(w), Height: int(h),
		}, true
	}
	return redact.Target{}, false
}

func (svs) BuildLabelIFD(img image.Image) *tifflow.ReplacementIFD {
	return label.BuildAperioLabelIFD(img)
}

func (svs) DefaultLabelDims() (int, int) { return label.AperioLabelWidth, label.AperioLabelHeight }
func (svs) DefaultLabelBG() color.RGBA   { return label.AperioLabelBG }

func hasTileTags(ifd *tifflow.IFD) bool {
	for _, e := range ifd.Entries {
		if e.Tag == tifflow.TagTileOffsets {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Port the detect test to `internal/redact/format/svs_test.go`**

```bash
git mv internal/aperio/detect_test.go internal/redact/format/svs_test.go
```

Edit `internal/redact/format/svs_test.go`:
- Package clause `package aperio` → `package format`.
- The sample dir is now three levels up: change `filepath.Join("..", "..", "sample_svs")` → `filepath.Join("..", "..", "..", "sample_svs")` (both occurrences).
- The current test calls `Detect(p)` (path-based). The new classifier's `Detect` takes a `*tifflow.File`. Update each call site to parse first and pass the file. Add the import `"github.com/cornish/wsi-label-tools/internal/tifflow"` and a small helper at the top of the test file:

```go
func detectPath(t *testing.T, p string) bool {
	t.Helper()
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	file, err := tifflow.Parse(f)
	if err != nil {
		return false // non-TIFF / bad magic → not SVS
	}
	return svs{}.Detect(file)
}
```

Then replace the body's `ok, err := Detect(p)` style calls with `ok := detectPath(t, p)` and drop the now-unused error handling. For the `TestDetect_NotTIFF` case, `detectPath` returns false on parse failure, which is the expected result.

- [ ] **Step 3: Run the SVS classifier tests**

Run: `go test ./internal/redact/format/ -v`
Expected: PASS (detect over sample files; not-TIFF returns false).

- [ ] **Step 4: Commit**

```bash
git add internal/redact/format/
git commit -m "redact/format: SVS classifier (detect + label/overview classify)"
```

---

## Task 5: Port end-to-end replace/strip + label tests to the new packages

**Files:**
- Create: `internal/redact/redact_test.go` (external `package redact_test`, ported from `aperio/backend_test.go` + `aperio/roundtrip_test.go`)
- Move: `aperio/label_test.go`, `aperio/build_label_test.go`, `aperio/resize_test.go` → `internal/label/`
- Delete: `internal/aperio/pick_target_dims_test.go` (superseded by Task 3's test)

- [ ] **Step 1: Move the label-building tests into `internal/label`**

```bash
git mv internal/aperio/label_test.go internal/label/label_test.go
git mv internal/aperio/build_label_test.go internal/label/build_label_test.go
git mv internal/aperio/resize_test.go internal/label/resize_test.go
```

Edit all three:
- Package clause `package aperio` → `package label`.
- In `resize_test.go`: replace `PrepareLabel(` → `Prepare(`; replace every `wsi.ReplaceOptions{` → `PrepareOptions{`; replace `wsi.ResizeNone/Stretch/Fit` → `ResizeNone/Stretch/Fit`; replace `wsi.ErrAspectMismatch` → `ErrAspectMismatch`; remove the `internal/wsi` import.
- In `build_label_test.go`: replace `BuildLabelIFD(` → `BuildAperioLabelIFD(`.
- `label_test.go` needs no symbol changes (it tests `EncodeLabelStrips`/predictor helpers) beyond the package clause.

- [ ] **Step 2: Create `internal/redact/redact_test.go` (external test package, end-to-end)**

This replaces `aperio/backend_test.go` and `aperio/roundtrip_test.go`. It is `package redact_test` so it may import both `redact` and `format` without an import cycle. The blank import of `format` registers the SVS classifier.

```go
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
	return filepath.Join("..", "..", "sample_svs", name)
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
		Image: newTestLabel(), Resize: redact.ResizeNone, Fsync: false,
	})
	if err != nil {
		t.Fatalf("Replace: %v", err)
	}
	if res.Added {
		t.Fatalf("expected replace, not add")
	}
	// Output must still parse and still carry a label.
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
	dir := filepath.Join("..", "..", "sample_svs")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("no sample dir: %v", err)
	}
	// Pre-render a PNG label on disk is unnecessary; use the in-memory image.
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
		})
	}
}

// assertPrefixIdentical verifies bytes [0, cutoff) match, where cutoff is the
// label IFD's start in the input (the splice guarantees the prefix is copied
// verbatim).
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
	// Compute the minimum start offset across the label IFD's own ranges.
	// Use the label IFD record offset as a conservative comparison bound.
	var cutoff uint64 = 1 << 62
	for i, ifd := range file.IFDs {
		desc, _ := ifd.StringValue(tifflow.TagImageDescription)
		lines := strings.SplitN(desc, "\n", 3)
		if len(lines) >= 2 && strings.HasPrefix(lines[1], "label") && i != 1 {
			if ifd.Offset < cutoff {
				cutoff = ifd.Offset
			}
		}
	}
	if cutoff == 1<<62 {
		return // no label in this sample; nothing to compare
	}
	inBytes := make([]byte, cutoff)
	outBytes := make([]byte, cutoff)
	if _, err := fIn.ReadAt(inBytes, 0); err != nil {
		t.Fatalf("read in prefix: %v", err)
	}
	fOut, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer fOut.Close()
	if _, err := fOut.ReadAt(outBytes, 0); err != nil {
		t.Fatalf("read out prefix: %v", err)
	}
	if !bytes.Equal(inBytes, outBytes) {
		t.Fatalf("prefix [0,%d) differs between input and output", cutoff)
	}
}

// keep png import referenced for callers that pre-render labels to disk.
var _ = png.Encode
```

> Note: the original `roundtrip_test.go` rendered a PNG and shelled the label through the backend; this port keeps the same coverage (replace runs on every sample + prefix-identity invariant) using the in-memory image. If the original asserted additional properties (e.g. tiffinfo decode), preserve those by porting the corresponding assertions here. (The deep tiffinfo/openslide decode check is also covered by `cross_validate.py` in Task 8.)

- [ ] **Step 3: Delete the superseded pick-target-dims test**

```bash
git rm internal/aperio/pick_target_dims_test.go
```

- [ ] **Step 4: Build the label + redact tests (aperio still present, may not build yet)**

Run: `go test ./internal/label/ ./internal/redact/...`
Expected: PASS. (Do not run `./...` yet — `internal/aperio` and the CLI still reference removed symbols until Tasks 6–7.)

- [ ] **Step 5: Commit**

```bash
git add internal/label/ internal/redact/ internal/aperio/
git commit -m "test: port label + end-to-end replace/strip tests to new packages"
```

---

## Task 6: Rewire the CLI onto `internal/redact`

**Files:**
- Modify: `internal/cli/replace.go`
- Modify: `internal/cli/strip.go`
- Modify: `internal/cli/label_dims.go`
- Modify: `cmd/wsi-label/main.go`

- [ ] **Step 1: Rewrite `internal/cli/replace.go` dispatch**

Replace the imports block `"github.com/cornish/wsi-label-tools/internal/aperio"` and `"github.com/cornish/wsi-label-tools/internal/wsi"` with `"github.com/cornish/wsi-label-tools/internal/redact"`, add `"errors"` to imports, and replace the dispatch section (the block from `// Dispatch to aperio backend...` through the `return nil` at the end of the function) with:

```go
	// Dispatch through the format-neutral redact orchestrator.
	opts := redact.ReplaceOptions{
		Image:         img,
		BG:            bgColor,
		Rotate:        *rotate,
		Resize:        resizeMode,
		Force:         *force,
		StrictReplace: *strict,
		Fsync:         *fsyncFlag,
		TargetWidth:   tw,
		TargetHeight:  th,
	}
	res, err := redact.Replace(in, out, opts)
	if errors.Is(err, redact.ErrFormatUnsupported) {
		return fmt.Errorf("%w: unrecognized WSI format", ErrFormatUnrecognized)
	}
	if errors.Is(err, redact.ErrNoLabel) {
		return fmt.Errorf("%w", ErrStrictReplace)
	}
	if err != nil {
		return err
	}
	if res.Added {
		if !*quiet {
			fmt.Fprintf(stderr, "wsi-label: no existing label; added one to %s\n", out)
		}
		return fmt.Errorf("%w: %s", ErrLabelAdded, out)
	}
	if !*quiet {
		fmt.Fprintf(stdout, "wsi-label: replaced label: %s → %s\n", in, out)
	}
	return nil
```

Also update `parseResizeMode` to return `redact.ResizeMode` and use the redact consts:

```go
func parseResizeMode(s string) (redact.ResizeMode, error) {
	switch strings.ToLower(s) {
	case "fit":
		return redact.ResizeFit, nil
	case "stretch":
		return redact.ResizeStretch, nil
	case "none":
		return redact.ResizeNone, nil
	default:
		return 0, fmt.Errorf("must be fit, stretch, or none; got %q", s)
	}
}
```

(`parseHexRGB` and `parseLabelDims` are unchanged.)

- [ ] **Step 2: Rewrite `internal/cli/strip.go` dispatch**

Replace the `aperio` import with `redact` + add `"errors"`, and replace the block from `b := aperio.New()` through the function's end with:

```go
	err = redact.Strip(in, out, *fsyncFlag)
	if errors.Is(err, redact.ErrFormatUnsupported) {
		return fmt.Errorf("%w: unrecognized WSI format", ErrFormatUnrecognized)
	}
	if errors.Is(err, redact.ErrNoLabel) {
		return fmt.Errorf("%w: no label IFD in %s", ErrUnreadable, in)
	}
	if err != nil {
		return err
	}
	if !*quiet {
		fmt.Fprintf(stdout, "wsi-label: stripped label: %s → %s\n", in, out)
	}
	return nil
```

- [ ] **Step 3: Rewrite `internal/cli/label_dims.go`**

Replace the `aperio` + `tifflow` imports with `redact` (+ keep `errors`), and replace the body after argument parsing (`path := fs.Arg(0)` onward) with:

```go
	path := fs.Arg(0)
	w, h, err := redact.LabelDims(path)
	if errors.Is(err, redact.ErrFormatUnsupported) {
		return fmt.Errorf("%w: unrecognized WSI format", ErrFormatUnrecognized)
	}
	if errors.Is(err, redact.ErrNoLabel) {
		return fmt.Errorf("%w: no label IFD in %s", ErrUnreadable, path)
	}
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnreadable, err)
	}
	fmt.Fprintf(stdout, "%dx%d\n", w, h)
	return nil
```

Add `"errors"` to the import list; remove `"os"` and `"github.com/cornish/wsi-label-tools/internal/tifflow"` and `"github.com/cornish/wsi-label-tools/internal/aperio"` if now unused.

- [ ] **Step 4: Blank-import the format package in `cmd/wsi-label/main.go`**

Add to the import block:

```go
	_ "github.com/cornish/wsi-label-tools/internal/redact/format"
```

This triggers SVS registration in the binary. (`inspect.go` is unchanged — its `identifyRole` is display-only and depends only on `tifflow`.)

- [ ] **Step 5: Build the CLI and main (aperio still present)**

Run: `go build ./internal/cli/ ./cmd/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/replace.go internal/cli/strip.go internal/cli/label_dims.go cmd/wsi-label/main.go
git commit -m "cli: route replace/strip/label-dims through redact; register formats"
```

---

## Task 7: Delete `internal/aperio` and `internal/wsi`

**Files:**
- Delete: `internal/aperio/` (remaining files: `backend.go`, `detect.go`, `backend_test.go`, `roundtrip_test.go`)
- Delete: `internal/wsi/`

- [ ] **Step 1: Remove the dead packages**

```bash
git rm -r internal/aperio internal/wsi
```

(`detect.go`'s `FindLabelIFD`/`FindMacroIFD` logic now lives in `format/svs.go`; `backend.go`/`detect.go`'s detection is in the classifier; `backend_test.go`/`roundtrip_test.go` were superseded by `redact_test.go` in Task 5.)

- [ ] **Step 2: Full build**

Run: `go build ./...`
Expected: PASS — no references to the deleted packages remain.

- [ ] **Step 3: Full test suite**

Run: `go test ./...`
Expected: PASS for `internal/label`, `internal/redact`, `internal/redact/format`, `internal/cli`, `internal/tifflow`, `internal/atomic`.

- [ ] **Step 4: Confirm no lingering references**

Run: `grep -rn "internal/aperio\|internal/wsi\b" --include="*.go" .`
Expected: no output.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "aperio,wsi: remove — superseded by redact core + label + format/svs"
```

---

## Task 8: Behavior-unchanged gate (cross_validate.py)

**Files:** none (verification only)

- [ ] **Step 1: Build the binary**

Run: `go build -o /tmp/wsi-label ./cmd/wsi-label`
Expected: PASS.

- [ ] **Step 2: Run the cross-validation script**

The script takes the binary path as its sole argument (see `ci.yml`: `python scripts/cross_validate.py ./wsi-label`). It uses the committed `sample_svs/CMU-1-Small-Region.svs` fixture. Locally this needs `tiffinfo` (libtiff-tools), `libopenslide`, and the Python deps `openslide-python tifffile imagecodecs numpy pillow`.

Run: `python3 scripts/cross_validate.py /tmp/wsi-label`
Expected: PASS — openslide round-trip + strict `tiffinfo -D` decode succeed, pixel-exact label round-trip confirmed, identical to pre-refactor behavior. (If system deps are unavailable locally, rely on the CI `test` job; do not mark this step done without seeing a PASS somewhere.)

- [ ] **Step 3: Manual smoke of the three subcommands**

Run:
```bash
/tmp/wsi-label inspect sample_svs/CMU-1-Small-Region.svs
/tmp/wsi-label label-dims sample_svs/CMU-1-Small-Region.svs
/tmp/wsi-label strip sample_svs/CMU-1-Small-Region.svs /tmp/stripped.svs && /tmp/wsi-label inspect /tmp/stripped.svs
```
Expected: `inspect` lists IFDs with roles; `label-dims` prints `WxH`; `strip` writes output whose inspect shows no `label` role.

- [ ] **Step 4: Commit (if the script or CI command needed a tweak; otherwise skip)**

```bash
git add -A
git commit -m "ci: confirm cross_validate passes against redact core"
```

---

## Task 9: opentile-go classification oracle (separate nested module)

**Files:**
- Create: `test/oracle/go.mod`
- Create: `test/oracle/classify_oracle_test.go`

- [ ] **Step 1: Create the nested module**

Create `test/oracle/go.mod`:

```
module github.com/cornish/wsi-label-tools/test/oracle

go 1.26.1

require (
	github.com/cornish/wsi-label-tools v0.0.0
	github.com/wsilabs/opentile-go v0.36.0
)

replace github.com/cornish/wsi-label-tools => ../..
```

- [ ] **Step 2: Create the oracle test**

Create `test/oracle/classify_oracle_test.go`:

```go
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
	dir := filepath.Join("..", "..", "sample_svs")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("no sample dir: %v", err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".svs") {
			continue
		}
		t.Run(e.Name(), func(t *testing.T) {
			p := filepath.Join(dir, e.Name())
			ours := hasLabelOurs(t, p)
			theirs := hasLabelOpentile(t, p)
			if ours != theirs {
				t.Fatalf("label-presence mismatch: ours=%v opentile=%v", ours, theirs)
			}
		})
	}
}

// TestLabelOffsetAgrees, where opentile-go exposes AssociatedIFDOffset (SVS),
// asserts the located label IFD offset matches ours.
func TestLabelOffsetAgrees(t *testing.T) {
	dir := filepath.Join("..", "..", "sample_svs")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("no sample dir: %v", err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".svs") {
			continue
		}
		t.Run(e.Name(), func(t *testing.T) {
			p := filepath.Join(dir, e.Name())

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

// labelIFDOffset returns the file offset of the IFD our SVS classifier would
// pick as the label, using the same rule as format/svs.go.
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
```

- [ ] **Step 3: Tidy and run the oracle module with CGo off**

Run:
```bash
cd test/oracle && GOFLAGS=-mod=mod CGO_ENABLED=0 go mod tidy && CGO_ENABLED=0 go test ./... -v
```
Expected: `go.sum` is written; `TestLabelPresenceAgrees` and `TestLabelOffsetAgrees` PASS over the sample SVS files.

- [ ] **Step 4: Confirm the main module is unaffected**

Run: `cd ../.. && go list -m all | grep opentile-go || echo "opentile-go NOT in main module (correct)"`
Expected: `opentile-go NOT in main module (correct)` — the nested module's deps do not enter the root module graph.

- [ ] **Step 5: Commit**

```bash
git add test/oracle/
git commit -m "test/oracle: opentile-go classification cross-validation (CGO off, nested module)"
```

---

## Task 10: Wire the oracle into CI

**Files:**
- Modify: `.github/workflows/ci.yml`

- [ ] **Step 1: Add an oracle job**

The CMU fixture is committed under `sample_svs/`, so no fetch step is needed. Add this job to `.github/workflows/ci.yml` under `jobs:` (matching the existing `go-version-file: go.mod` + `cache: true` style):

```yaml
  oracle:
    name: opentile-go classification oracle (linux)
    runs-on: ubuntu-latest
    timeout-minutes: 10
    steps:
      - uses: actions/checkout@v4
      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
          cache: true
      - name: Cross-validate classification against opentile-go (CGo off)
        working-directory: test/oracle
        env:
          CGO_ENABLED: '0'
        run: go test ./... -count=1 -v
```

The nested module's `go.mod` (Task 9) pins opentile-go and `replace`s the main module to `../..`, so `setup-go` with `go-version-file: go.mod` (the root file) still selects a compatible toolchain. `CGO_ENABLED=0` forces opentile-go's pure-Go stubs (classification only; no native decode).

- [ ] **Step 2: Validate the workflow locally (syntax)**

Run: `python3 -c "import yaml,sys; yaml.safe_load(open('.github/workflows/ci.yml'))" && echo OK`
Expected: `OK` (valid YAML).

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci: run opentile-go classification oracle (CGO_ENABLED=0)"
```

---

## Task 11: Docs — README supported formats + ROADMAP

**Files:**
- Modify: `README.md`
- Modify: `docs/ROADMAP.md`

- [ ] **Step 1: Note the architecture in README**

In `README.md`, under the existing architecture/extensibility bullet, confirm the wording still holds (the `wsi.Backend` mention is now `redact.Classifier`). Replace the Features bullet:

```
- **Extensible** — architecture isolates Aperio-specific logic behind a `wsi.Backend` interface; adding Hamamatsu NDPI, Leica SCN, etc. is a drop-in, not a rewrite.
```

with:

```
- **Extensible** — format detection/classification lives behind a `redact.Classifier` registry (one `init()` registration per format); adding OME-TIFF, Philips, NDPI, etc. is a drop-in, not a rewrite.
```

The "Why a separate tool?" and "Pure Go" claims are unchanged and remain accurate (opentile-go is test-only).

- [ ] **Step 2: Update ROADMAP**

In `docs/ROADMAP.md`, under the NDPI/format-coverage band, add a line marking Band 0 done and pointing at this plan and the spec:

```
- [x] **Band 0 — redact core refactor.** Replaced `internal/aperio` +
  `wsi.Backend` with the format-neutral `internal/redact` core (role-keyed
  targets, classifier registry) + `internal/label` builder + `format/svs`
  classifier, plus an opentile-go classification oracle (test-only). See
  `docs/superpowers/plans/2026-06-06-band0-redact-core-refactor.md` and
  `docs/superpowers/specs/2026-06-06-multi-format-label-redaction-design.md`.
  Behavior unchanged; cross_validate green.
```

- [ ] **Step 3: Commit**

```bash
git add README.md docs/ROADMAP.md
git commit -m "docs: redact.Classifier extensibility + Band 0 roadmap entry"
```

---

## Done criteria for Band 0

- `go build ./...` and `go test ./...` pass on the main module (two pure-Go deps unchanged).
- `cd test/oracle && CGO_ENABLED=0 go test ./...` passes; opentile-go is absent from the main module graph.
- `scripts/cross_validate.py` passes — behavior identical to pre-refactor.
- `internal/aperio` and `internal/wsi` are gone; no references remain.
- The next plan (Band 1) adds `format/generictiff.go`, `format/ometiff.go`, `format/philips.go` as classifier + oracle-test additions over this machinery.
