# Band 1 — generic-TIFF + Philips splice classifiers Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `generic-TIFF` and `Philips-TIFF` to wsi-label's redaction coverage (both `strip` and `replace`), staying pure-Go and splice-only; migrate CI fixtures to the shared `wsilabs/wsi-fixtures` repo and retire the committed `sample_svs/`.

**Architecture:** Two new `redact.Classifier` implementations registered into the Band 0 registry — Philips as a normal (vendor) classifier, generic-TIFF as a **fallback** (it must lose to SVS/Philips). The shared label-pixel encoder in `internal/label` is generalized so each format stamps its own re-classification marker (generic-TIFF: private tag 65080="label"; Philips: a "Label" ImageDescription). Classification is cross-validated against `opentile-go` in the test-only oracle module. Fixtures come from a new `wsi-fixtures` `v2` release, downloaded in CI.

**Tech Stack:** Go 1.26, `internal/tifflow` (our splicer), `internal/label`, `internal/redact`; `github.com/wsilabs/opentile-go` (test-only oracle, `CGO_ENABLED=0`); `wsilabs/wsi-fixtures` (CC0 fixture corpus, `gh release` download).

**Refactor-TDD note:** New classifiers get oracle + unit tests written alongside. The behavior gate is `cross_validate.py` (now able to run on generic-TIFF too). Local dev points tests at `sample_files/` (downloaded or the sibling `opentile-go/sample_files`), skipping when absent.

**Module path:** `github.com/cornish/wsi-label-tools`

**Reference spec:** `docs/superpowers/specs/2026-06-06-multi-format-label-redaction-design.md` (amended 2026-06-07).

**Branch:** create `band1-generic-philips` off `main` before Phase 1.

---

## File structure (end state of Band 1)

```
internal/tifflow/tags.go              MODIFY — add TagSoftware (305), TagWSIImageType (65080)
internal/redact/classifier.go         MODIFY — add RegisterFallback + fallback tier in detectClassifier
internal/label/build_label.go         MODIFY — generalize: BuildStripLabelIFD(img, desc, extra...); keep BuildAperioLabelIFD as wrapper
internal/redact/format/philips.go     NEW    — Philips classifier (vendor) + init() Register
internal/redact/format/philips_test.go NEW   — unit test (detect/classify on sample)
internal/redact/format/generictiff.go NEW    — generic-TIFF classifier (fallback) + init() RegisterFallback
internal/redact/format/generictiff_test.go NEW — unit test
test/oracle/classify_oracle_test.go   MODIFY — extend cross-validation to philips + generic-tiff corpus
scripts/cross_validate.py             MODIFY — accept generic-tiff sample dir; keep SVS path
.github/workflows/ci.yml              MODIFY — download wsi-fixtures v2 into sample_files/; point test+oracle at it
sample_svs/                           DELETED — replaced by downloaded fixtures
internal/redact/redact_test.go        MODIFY — point svsSample at sample_files/svs/...
internal/redact/format/svs_test.go    MODIFY — point sample dir at sample_files/svs/...
README.md / docs/ROADMAP.md           MODIFY — formats table + roadmap

# In the sibling wsilabs/wsi-fixtures repo (separate git repo):
fixtures/philips-tiff/Philips-4.tiff            NEW (+ .LICENSE + .PROVENANCE)
fixtures/generic-tiff/CMU-1-Small-Region.stripped.tiff  NEW (+ .LICENSE + .PROVENANCE)
manifest.json                                   MODIFY
dist/philips-tiff.tar, dist/generic-tiff.tar    NEW (via pack.sh)
# → publish release v2
```

---

# Phase 1 — Core prep (tifflow tags, registry fallback, label builder)

## Task 1: Add `TagSoftware` and `TagWSIImageType` to tifflow

**Files:** Modify `internal/tifflow/tags.go`

- [ ] **Step 1: Add the two tag constants**

In the `const (...)` tag block, add (keep the existing numeric grouping; place sensibly):

```go
	TagSoftware         = 305
```
(after `TagRowsPerStrip = 278` / wherever 305 fits ascending — put it after `TagStripByteCounts = 279`), and at the end of the block:

```go
	TagWSIImageType     = 65080 // wsitools/COG-WSI private: ASCII role ("label"/"overview"/...)
```

- [ ] **Step 2: Build**

Run: `cd /Volumes/Ext/GitHub/wsi-label-tools && go build ./internal/tifflow/`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/tifflow/tags.go
git commit -m "tifflow: add TagSoftware (305) and TagWSIImageType (65080)"
```

---

## Task 2: Add a fallback tier to the classifier registry

**Files:** Modify `internal/redact/classifier.go`; Test `internal/redact/classifier_test.go` (new)

Generic-TIFF will claim almost any linear TIFF, so it must be tried only after vendor classifiers (SVS, Philips) decline. Mirror opentile-go's `RegisterFallback`.

- [ ] **Step 1: Write the failing test**

Create `internal/redact/classifier_test.go`:

```go
package redact

import (
	"image"
	"image/color"
	"testing"

	"github.com/cornish/wsi-label-tools/internal/tifflow"
)

// fakeClassifier matches when match==true; records its name.
type fakeClassifier struct {
	name  string
	match bool
}

func (f fakeClassifier) Name() string                                     { return f.name }
func (f fakeClassifier) Detect(*tifflow.File) bool                        { return f.match }
func (fakeClassifier) Classify(*tifflow.File) ([]Target, error)          { return nil, nil }
func (fakeClassifier) BuildLabelIFD(image.Image) *tifflow.ReplacementIFD { return nil }
func (fakeClassifier) DefaultLabelDims() (int, int)                       { return 0, 0 }
func (fakeClassifier) DefaultLabelBG() color.RGBA                         { return color.RGBA{} }

func TestDetect_FallbackTriedLast(t *testing.T) {
	// Save and restore registry.
	savedV, savedF := classifiers, fallbacks
	defer func() { classifiers, fallbacks = savedV, savedF }()
	classifiers, fallbacks = nil, nil

	RegisterFallback(fakeClassifier{name: "fallback", match: true})
	Register(fakeClassifier{name: "vendor", match: true})

	c, ok := detectClassifier(&tifflow.File{})
	if !ok || c.Name() != "vendor" {
		t.Fatalf("expected vendor to win over fallback, got ok=%v name=%v", ok, c)
	}
}

func TestDetect_FallbackUsedWhenNoVendorMatches(t *testing.T) {
	savedV, savedF := classifiers, fallbacks
	defer func() { classifiers, fallbacks = savedV, savedF }()
	classifiers, fallbacks = nil, nil

	RegisterFallback(fakeClassifier{name: "fallback", match: true})
	Register(fakeClassifier{name: "vendor", match: false})

	c, ok := detectClassifier(&tifflow.File{})
	if !ok || c.Name() != "fallback" {
		t.Fatalf("expected fallback, got ok=%v name=%v", ok, c)
	}
}
```

- [ ] **Step 2: Run test to confirm it fails to compile**

Run: `go test ./internal/redact/ -run TestDetect_Fallback`
Expected: FAIL — `RegisterFallback` and `fallbacks` undefined.

- [ ] **Step 3: Implement the fallback tier**

In `internal/redact/classifier.go`, replace the registry section:

```go
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

with:

```go
var (
	classifiers []Classifier // vendor classifiers, tried first
	fallbacks   []Classifier // greedy classifiers (e.g. generic-TIFF), tried last
)

// Register adds a vendor classifier. Format packages call this from init();
// the binary blank-imports those packages to trigger registration.
func Register(c Classifier) { classifiers = append(classifiers, c) }

// RegisterFallback adds a classifier that is only consulted after every
// vendor classifier has declined. Used by greedy matchers like generic-TIFF
// that would otherwise shadow more specific formats.
func RegisterFallback(c Classifier) { fallbacks = append(fallbacks, c) }

// detectClassifier returns the first vendor classifier that matches, else the
// first matching fallback.
func detectClassifier(file *tifflow.File) (Classifier, bool) {
	for _, c := range classifiers {
		if c.Detect(file) {
			return c, true
		}
	}
	for _, c := range fallbacks {
		if c.Detect(file) {
			return c, true
		}
	}
	return nil, false
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/redact/ -run TestDetect_Fallback -v`
Expected: PASS (both subtests). Also `go test ./internal/redact/... -count=1` stays green.

- [ ] **Step 5: Commit**

```bash
git add internal/redact/classifier.go internal/redact/classifier_test.go
git commit -m "redact: add fallback classifier tier (vendor-first, fallback-last)"
```

---

## Task 3: Generalize the label builder

**Files:** Modify `internal/label/build_label.go`; Test `internal/label/build_label_test.go`

Today `BuildAperioLabelIFD` hardcodes the Aperio ImageDescription. Generalize so each format supplies its own description + optional extra tags (generic-TIFF stamps tag 65080="label").

- [ ] **Step 1: Write the failing test**

Append to `internal/label/build_label_test.go`:

```go
func TestBuildStripLabelIFD_DescAndExtraTag(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 64, 48))
	rep := BuildStripLabelIFD(img, "hello\nlabel 64x48",
		tifflow.OutTag{
			Tag: tifflow.TagWSIImageType, Type: tifflow.TypeASCII,
			Count: uint64(len("label") + 1), Inline: false,
			Bytes: append([]byte("label"), 0),
		})
	// ImageDescription must be the supplied string.
	var gotDesc string
	var hasWSIType bool
	for _, tg := range rep.Tags {
		if tg.Tag == tifflow.TagImageDescription {
			gotDesc = string(tg.Bytes)
		}
		if tg.Tag == tifflow.TagWSIImageType {
			hasWSIType = true
		}
	}
	if gotDesc != "hello\nlabel 64x48\x00" {
		t.Fatalf("desc = %q", gotDesc)
	}
	if !hasWSIType {
		t.Fatalf("extra tag 65080 not present")
	}
	// Tags must remain in ascending tag-number order (TIFF requirement).
	for i := 1; i < len(rep.Tags); i++ {
		if rep.Tags[i].Tag < rep.Tags[i-1].Tag {
			t.Fatalf("tags not ascending at %d: %d before %d", i, rep.Tags[i-1].Tag, rep.Tags[i].Tag)
		}
	}
}
```

(`tifflow` is already imported in this test file via build_label_test.go's package; if not, add it.)

- [ ] **Step 2: Run to confirm fail**

Run: `go test ./internal/label/ -run TestBuildStripLabelIFD`
Expected: FAIL — `BuildStripLabelIFD` undefined.

- [ ] **Step 3: Refactor `build_label.go`**

In `internal/label/build_label.go`, rename the body of `BuildAperioLabelIFD` into a general function that takes the description and extra tags, and make `BuildAperioLabelIFD` a thin wrapper. Replace the current `BuildAperioLabelIFD` function with:

```go
// BuildStripLabelIFD produces a ReplacementIFD for the given RGB image as an
// LZW + predictor-2, 8-bit RGB, multi-strip label. `desc` becomes the
// ImageDescription. `extra` tags (which MUST have tag numbers greater than
// TagPredictor=317 to preserve ascending order) are appended verbatim —
// used to stamp format re-classification markers (e.g. tag 65080).
func BuildStripLabelIFD(img image.Image, desc string, extra ...tifflow.OutTag) *tifflow.ReplacementIFD {
	b := img.Bounds()
	width, height := b.Dx(), b.Dy()
	strips := EncodeLabelStrips(img)

	stripByteCounts := make([]byte, 0, len(strips)*4)
	for _, s := range strips {
		lb := make([]byte, 4)
		binary.LittleEndian.PutUint32(lb, uint32(len(s)))
		stripByteCounts = append(stripByteCounts, lb...)
	}
	stripOffsetsPlaceholder := make([]byte, len(strips)*4)

	shortLE := func(v uint16) []byte {
		bb := make([]byte, 4)
		binary.LittleEndian.PutUint16(bb, v)
		return bb
	}
	longLE := func(v uint32) []byte {
		bb := make([]byte, 4)
		binary.LittleEndian.PutUint32(bb, v)
		return bb
	}

	bps := make([]byte, 6)
	binary.LittleEndian.PutUint16(bps[0:2], 8)
	binary.LittleEndian.PutUint16(bps[2:4], 8)
	binary.LittleEndian.PutUint16(bps[4:6], 8)

	tags := []tifflow.OutTag{
		{Tag: tifflow.TagNewSubfileType, Type: tifflow.TypeLong, Count: 1, Inline: true, Bytes: longLE(1)},
		{Tag: tifflow.TagImageWidth, Type: tifflow.TypeLong, Count: 1, Inline: true, Bytes: longLE(uint32(width))},
		{Tag: tifflow.TagImageLength, Type: tifflow.TypeLong, Count: 1, Inline: true, Bytes: longLE(uint32(height))},
		{Tag: tifflow.TagBitsPerSample, Type: tifflow.TypeShort, Count: 3, Inline: false, Bytes: bps},
		{Tag: tifflow.TagCompression, Type: tifflow.TypeShort, Count: 1, Inline: true, Bytes: shortLE(5)},
		{Tag: tifflow.TagPhotometric, Type: tifflow.TypeShort, Count: 1, Inline: true, Bytes: shortLE(2)},
		{Tag: tifflow.TagImageDescription, Type: tifflow.TypeASCII, Count: uint64(len(desc) + 1), Inline: false, Bytes: append([]byte(desc), 0)},
		{Tag: tifflow.TagStripOffsets, Type: tifflow.TypeLong, Count: uint64(len(strips)), Inline: false, Bytes: stripOffsetsPlaceholder, ResolvesToOffset: true, OffsetRefs: iotaSlice(len(strips))},
		{Tag: tifflow.TagSamplesPerPixel, Type: tifflow.TypeShort, Count: 1, Inline: true, Bytes: shortLE(3)},
		{Tag: tifflow.TagRowsPerStrip, Type: tifflow.TypeShort, Count: 1, Inline: true, Bytes: shortLE(uint16(AperioRowsPerStrip))},
		{Tag: tifflow.TagStripByteCounts, Type: tifflow.TypeLong, Count: uint64(len(strips)), Inline: false, Bytes: stripByteCounts},
		{Tag: tifflow.TagPlanarConfig, Type: tifflow.TypeShort, Count: 1, Inline: true, Bytes: shortLE(1)},
		{Tag: tifflow.TagPredictor, Type: tifflow.TypeShort, Count: 1, Inline: true, Bytes: shortLE(2)},
	}
	tags = append(tags, extra...)

	return &tifflow.ReplacementIFD{Tags: tags, StripData: strips}
}

// BuildAperioLabelIFD produces an Aperio-flavored label IFD (Aperio
// ImageDescription with the "label WxH" second line OpenSlide keys on).
func BuildAperioLabelIFD(img image.Image) *tifflow.ReplacementIFD {
	b := img.Bounds()
	return BuildStripLabelIFD(img, aperioDescription(b.Dx(), b.Dy()))
}
```

(Keep the existing `aperioDescription` and `iotaSlice` helpers.)

- [ ] **Step 4: Run tests**

Run: `go test ./internal/label/ -count=1 -v`
Expected: PASS — the new test plus all existing label tests (BuildAperioLabelIFD still works via the wrapper).

- [ ] **Step 5: Commit**

```bash
git add internal/label/build_label.go internal/label/build_label_test.go
git commit -m "label: generalize to BuildStripLabelIFD(desc, extra...); Aperio is a wrapper"
```

---

# Phase 2 — Fixtures (wsi-fixtures v2 + wsi-label-tools migration)

## Task 4: Add Philips + generic-TIFF fixtures to wsi-fixtures and publish v2

**Repo:** `/Volumes/Ext/GitHub/wsi-fixtures` (SEPARATE git repo — commit/release there, not in wsi-label-tools)

Source files exist locally at `/Volumes/Ext/GitHub/opentile-go/sample_files/{philips-tiff/Philips-4.tiff, generic-tiff/CMU-1-Small-Region.stripped.tiff}`. Both trace to OpenSlide CMU/Philips test data (CC0 / public-domain). `CMU-1-Small-Region.stripped.tiff` is a CC0-derived generic-TIFF whose "Aperio" prefix was rewritten (so it is NOT detected as SVS) and which retains a label + macro.

- [ ] **Step 1: Copy the fixture binaries into place**

```bash
cd /Volumes/Ext/GitHub/wsi-fixtures
mkdir -p fixtures/philips-tiff fixtures/generic-tiff
cp /Volumes/Ext/GitHub/opentile-go/sample_files/philips-tiff/Philips-4.tiff fixtures/philips-tiff/Philips-4.tiff
cp /Volumes/Ext/GitHub/opentile-go/sample_files/generic-tiff/CMU-1-Small-Region.stripped.tiff fixtures/generic-tiff/CMU-1-Small-Region.stripped.tiff
```

- [ ] **Step 2: Compute sizes + sha256 (needed for manifest + PROVENANCE)**

```bash
cd /Volumes/Ext/GitHub/wsi-fixtures
for f in fixtures/philips-tiff/Philips-4.tiff fixtures/generic-tiff/CMU-1-Small-Region.stripped.tiff; do
  echo "$f  size=$(stat -f%z "$f")  sha256=$(shasum -a 256 "$f" | cut -d' ' -f1)"
done
```
Record both `size` and `sha256` values — used verbatim below.

- [ ] **Step 3: Write the LICENSE sidecars**

Both originate from OpenSlide test data, which OpenSlide distributes as public-domain / CC0. Copy the exact CC0 text already used by the SVS fixture so wording matches:

```bash
cd /Volumes/Ext/GitHub/wsi-fixtures
cp fixtures/svs/CMU-1-Small-Region.svs.LICENSE fixtures/philips-tiff/Philips-4.tiff.LICENSE
cp fixtures/svs/CMU-1-Small-Region.svs.LICENSE fixtures/generic-tiff/CMU-1-Small-Region.stripped.tiff.LICENSE
```

- [ ] **Step 4: Write the PROVENANCE sidecars**

Create `fixtures/philips-tiff/Philips-4.tiff.PROVENANCE` (fill in `<sha256>`/`<date>` from Step 2 / today):

```
source_url: https://openslide.cs.cmu.edu/download/openslide-testdata/Philips-TIFF/Philips-4.tiff
retrieval_date: 2026-06-07
sha256: <sha256 from step 2>
license_spdx: CC0-1.0
citation: OpenSlide test data, Philips-TIFF set (public domain).
notes: Philips-TIFF whole-slide image used to test Philips label classification + splice.
```

Create `fixtures/generic-tiff/CMU-1-Small-Region.stripped.tiff.PROVENANCE`:

```
source_url: https://openslide.cs.cmu.edu/download/openslide-testdata/Aperio/CMU-1-Small-Region.svs (derived)
retrieval_date: 2026-06-07
sha256: <sha256 from step 2>
license_spdx: CC0-1.0
citation: Derived from OpenSlide CMU-1-Small-Region.svs (public domain) via opentile-go scripts/regen-generic-tiff.py — stripped to a generic TIFF with the vendor ImageDescription prefix rewritten so it is not detected as Aperio SVS. Retains label + macro associated images.
notes: Generic-TIFF fixture for label classification (heuristic + tag 65080) and splice.
```

- [ ] **Step 5: Update `manifest.json`**

Add two entries to the `fixtures` array (use the size/sha256 from Step 2; mirror the existing entries' shape exactly):

```json
    {
      "path": "fixtures/philips-tiff/Philips-4.tiff",
      "size": <size>,
      "sha256": "<sha256>",
      "source_url": "https://openslide.cs.cmu.edu/download/openslide-testdata/Philips-TIFF/Philips-4.tiff",
      "retrieval_date": "2026-06-07",
      "license_spdx": "CC0-1.0",
      "license_file": "fixtures/philips-tiff/Philips-4.tiff.LICENSE",
      "tarball_asset": "philips-tiff.tar"
    },
    {
      "path": "fixtures/generic-tiff/CMU-1-Small-Region.stripped.tiff",
      "size": <size>,
      "sha256": "<sha256>",
      "source_url": "https://openslide.cs.cmu.edu/download/openslide-testdata/Aperio/CMU-1-Small-Region.svs",
      "retrieval_date": "2026-06-07",
      "license_spdx": "CC0-1.0",
      "license_file": "fixtures/generic-tiff/CMU-1-Small-Region.stripped.tiff.LICENSE",
      "tarball_asset": "generic-tiff.tar"
    }
```

- [ ] **Step 6: Verify + pack**

```bash
cd /Volumes/Ext/GitHub/wsi-fixtures
./scripts/verify.sh    # sha256 must match manifest — PASS
./scripts/pack.sh      # produces dist/philips-tiff.tar, dist/generic-tiff.tar (+ existing)
ls -la dist/
```
Expected: verify passes; `dist/philips-tiff.tar` and `dist/generic-tiff.tar` produced. If `pack.sh` only packs formats it knows, read it first and confirm it globs all `fixtures/<format>/` dirs (it should); adjust only if it has a hardcoded list.

- [ ] **Step 7: Commit + publish release v2**

```bash
cd /Volumes/Ext/GitHub/wsi-fixtures
git add -A
git commit -m "fixtures: add Philips-4 (philips-tiff) + CMU-1-Small-Region.stripped (generic-tiff)"
git push
gh release create v2 dist/*.tar \
  --repo wsilabs/wsi-fixtures \
  --title "v2 — add philips-tiff + generic-tiff" \
  --notes "Adds Philips-4.tiff and CMU-1-Small-Region.stripped.tiff (both CC0). Includes svs.tar, ndpi.tar, philips-tiff.tar, generic-tiff.tar."
```
Expected: release `v2` created with all four `*.tar` assets. (Releases are immutable — verify the asset list before/after.)

> NOTE: This is an outward, immutable publish. Before running Step 7, re-confirm verify.sh passed and the manifest sha256s match. If you lack `gh` auth/permissions, STOP after Step 6 and report — the controller will publish.

---

## Task 5: Migrate wsi-label-tools CI + tests to wsi-fixtures; retire sample_svs/

**Files:** Modify `.github/workflows/ci.yml`, `internal/redact/redact_test.go`, `internal/redact/format/svs_test.go`, `scripts/cross_validate.py`; delete `sample_svs/`.

The new convention: a `sample_files/<format>/<name>` tree, populated in CI by downloading wsi-fixtures `v2`, and locally by the developer (download or symlink the sibling `opentile-go/sample_files`). Tests skip when a fixture is absent.

- [ ] **Step 1: Add the fixtures-download step to CI**

In `.github/workflows/ci.yml`, in BOTH the `test` job and the `oracle` job, add a download step BEFORE the test/cross-validate steps (after Go setup):

```yaml
      - name: Download fixtures
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: |
          mkdir -p sample_files
          cd sample_files
          gh release download v2 --repo wsilabs/wsi-fixtures --pattern '*.tar'
          for t in *.tar; do tar xf "$t" && rm "$t"; done
          ls -R .
```
This yields `sample_files/svs/CMU-1-Small-Region.svs`, `sample_files/philips-tiff/Philips-4.tiff`, `sample_files/generic-tiff/CMU-1-Small-Region.stripped.tiff`, `sample_files/ndpi/CMU-1.ndpi`.

- [ ] **Step 2: Repoint the SVS tests off sample_svs/ onto sample_files/svs/**

In `internal/redact/redact_test.go`, change `svsSample`:

```go
func svsSample(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join("..", "..", "sample_files", "svs", name)
}
```
and `TestReplace_AllSamples`'s dir:

```go
	dir := filepath.Join("..", "..", "sample_files", "svs")
```

In `internal/redact/format/svs_test.go`, change the sample dir (3 levels up):

```go
	dir := filepath.Join("..", "..", "..", "sample_files", "svs")
```
(every occurrence). These tests already `t.Skip` / handle a missing dir; confirm they skip cleanly when `sample_files/` is absent.

- [ ] **Step 3: Repoint the oracle at sample_files/svs/**

In `test/oracle/classify_oracle_test.go`, change the SVS sample dir:

```go
	dir := filepath.Join("..", "..", "sample_files", "svs")
```
(both test functions).

- [ ] **Step 4: Update cross_validate.py to use sample_files/svs**

In `scripts/cross_validate.py`, change the hardcoded `sample_svs` directory reference to `sample_files/svs`. (Read the script; it has a constant or default dir for the SVS corpus — repoint it. Keep the binary-path argument behavior unchanged.)

- [ ] **Step 5: Delete the committed SVS corpus**

```bash
cd /Volumes/Ext/GitHub/wsi-label-tools
git rm -r sample_svs
```

- [ ] **Step 6: Add a developer convenience for local fixtures**

Create `scripts/fetch-fixtures.sh` (so `go test` has fixtures locally without a manual download):

```bash
#!/usr/bin/env bash
# Populate ./sample_files from the wsilabs/wsi-fixtures v2 release (CI parity).
# Requires gh auth. Alternatively symlink a local opentile-go/sample_files.
set -euo pipefail
cd "$(dirname "$0")/.."
mkdir -p sample_files && cd sample_files
gh release download v2 --repo wsilabs/wsi-fixtures --pattern '*.tar'
for t in *.tar; do tar xf "$t" && rm "$t"; done
ls -R .
```
```bash
chmod +x scripts/fetch-fixtures.sh
```
Add `sample_files/` to `.gitignore`:
```bash
printf '\n# Downloaded WSI fixtures (see scripts/fetch-fixtures.sh)\nsample_files/\n' >> .gitignore
```

- [ ] **Step 7: Verify locally (with fixtures present)**

```bash
./scripts/fetch-fixtures.sh   # or: ln -s ../opentile-go/sample_files sample_files
go build ./... && go test ./... -count=1 2>&1 | grep -E "ok|FAIL"
(cd test/oracle && CGO_ENABLED=0 go test ./... -count=1)
go build -o /tmp/wsi-b1 ./cmd/wsi-label && python3 scripts/cross_validate.py /tmp/wsi-b1   # if deps available
```
Expected: all green; SVS end-to-end + oracle now run against `sample_files/svs/CMU-1-Small-Region.svs`. With `sample_files/` absent, `go test ./...` still builds and the fixture-dependent tests skip.

- [ ] **Step 8: Validate the workflow YAML + commit**

```bash
python3 -c "import yaml; yaml.safe_load(open('.github/workflows/ci.yml')); print('OK')"
git add -A
git commit -m "ci,test: consume wsilabs/wsi-fixtures v2; retire committed sample_svs/"
```

---

# Phase 3 — generic-TIFF classifier (strip + replace)

## Task 6: `format/generictiff.go` + unit test

**Files:** Create `internal/redact/format/generictiff.go`, `internal/redact/format/generictiff_test.go`

- [ ] **Step 1: Implement the classifier**

Create `internal/redact/format/generictiff.go`:

```go
package format

import (
	"image"
	"image/color"

	"github.com/cornish/wsi-label-tools/internal/label"
	"github.com/cornish/wsi-label-tools/internal/redact"
	"github.com/cornish/wsi-label-tools/internal/tifflow"
)

func init() { redact.RegisterFallback(genericTIFF{}) }

// generic-TIFF threshold constants, ported verbatim from opentile-go
// formats/generictiff/classifier.go (v0.36.0).
const (
	gtLabelMaxDim     = 1500
	gtMacroAspect     = 2.0
	gtMacroMinDim     = 1000
	gtMacroMaxDim     = 5000
	gtThumbMaxDim     = 1500
	gtTiledThumbRatio = 0.001 // <0.1% baseline area
	gtTiledOverRatio  = 0.01  // <1% baseline area
	compLZW           = 5
	compJPEG          = 7
)

// genericTIFF is the fallback classifier for plain pyramidal TIFFs that no
// vendor classifier claimed. It is registered via RegisterFallback so SVS /
// Philips win first.
type genericTIFF struct{}

func (genericTIFF) Name() string { return "generic-tiff" }

// Detect: a linear TIFF (already parsed) with at least one tiled IFD (a
// pyramid baseline) and at least one non-tiled associated IFD candidate. We
// keep this permissive on purpose — it only runs after vendor classifiers
// decline.
func (genericTIFF) Detect(file *tifflow.File) bool {
	tiled, strip := 0, 0
	for _, ifd := range file.IFDs {
		if isTiled(ifd) {
			tiled++
		} else {
			strip++
		}
	}
	return tiled >= 1 && len(file.IFDs) >= 2 && strip >= 1
}

func (g genericTIFF) Classify(file *tifflow.File) ([]redact.Target, error) {
	baseW, baseH := g.baselineDims(file)
	baseArea := float64(baseW) * float64(baseH)

	var targets []redact.Target
	for i, ifd := range file.IFDs {
		role, ok := g.classifyIFD(file, ifd, baseArea)
		if !ok {
			continue
		}
		w, _ := ifd.UintValue(tifflow.TagImageWidth, file.Header.ByteOrder)
		h, _ := ifd.UintValue(tifflow.TagImageLength, file.Header.ByteOrder)
		targets = append(targets, redact.Target{
			Role: role, Representation: redact.SeparableIFD,
			IFDIndex: i, Width: int(w), Height: int(h),
		})
	}
	return targets, nil
}

// baselineDims returns the dimensions of the largest tiled IFD (the L0
// baseline), used for tiled associated-image area ratios.
func (genericTIFF) baselineDims(file *tifflow.File) (int, int) {
	bw, bh := 0, 0
	for _, ifd := range file.IFDs {
		if !isTiled(ifd) {
			continue
		}
		w, _ := ifd.UintValue(tifflow.TagImageWidth, file.Header.ByteOrder)
		h, _ := ifd.UintValue(tifflow.TagImageLength, file.Header.ByteOrder)
		if int(w)*int(h) > bw*bh {
			bw, bh = int(w), int(h)
		}
	}
	return bw, bh
}

// classifyIFD returns a redact.Role for an associated (non-pyramid) IFD, or
// ok=false if the IFD is a pyramid level or unclassifiable. tag 65080 is
// authoritative; otherwise the opentile heuristic ladder applies.
func (g genericTIFF) classifyIFD(file *tifflow.File, ifd *tifflow.IFD, baseArea float64) (redact.Role, bool) {
	// Authoritative private tag 65080.
	if wt, ok := ifd.StringValue(tifflow.TagWSIImageType); ok {
		switch wt {
		case "label":
			return redact.RoleLabel, true
		case "macro", "overview":
			return redact.RoleOverview, true
		case "thumbnail":
			return redact.RoleThumbnail, true
		case "pyramid":
			return 0, false
		}
		// unknown value → fall through to heuristic
	}

	w, _ := ifd.UintValue(tifflow.TagImageWidth, file.Header.ByteOrder)
	h, _ := ifd.UintValue(tifflow.TagImageLength, file.Header.ByteOrder)
	wi, hi := int(w), int(h)
	comp, _ := ifd.UintValue(tifflow.TagCompression, file.Header.ByteOrder)

	if !isTiled(ifd) {
		larger, smaller := wi, hi
		if hi > wi {
			larger, smaller = hi, wi
		}
		switch {
		case comp == compLZW && wi < gtLabelMaxDim && hi < gtLabelMaxDim:
			return redact.RoleLabel, true
		case comp == compJPEG && larger >= gtMacroMinDim && larger <= gtMacroMaxDim &&
			smaller > 0 && float64(larger)/float64(smaller) >= gtMacroAspect:
			return redact.RoleOverview, true
		case comp == compJPEG && wi < gtThumbMaxDim && hi < gtThumbMaxDim:
			return redact.RoleThumbnail, true
		}
		return 0, false
	}

	// Tiled associated images by area ratio (excludes the baseline itself,
	// whose ratio is 1.0).
	if baseArea > 0 {
		ratio := (float64(wi) * float64(hi)) / baseArea
		switch {
		case ratio < gtTiledThumbRatio:
			return redact.RoleThumbnail, true
		case ratio < gtTiledOverRatio:
			return redact.RoleOverview, true
		}
	}
	return 0, false
}

func (genericTIFF) BuildLabelIFD(img image.Image) *tifflow.ReplacementIFD {
	// Stamp the authoritative tag 65080="label" so re-open classifies it
	// regardless of heuristics. The LZW/stripped/<1500px body also satisfies
	// the heuristic-label rule as a belt-and-suspenders fallback.
	return label.BuildStripLabelIFD(img, "wsi-label\nlabel", tifflow.OutTag{
		Tag:   tifflow.TagWSIImageType,
		Type:  tifflow.TypeASCII,
		Count: uint64(len("label") + 1),
		Bytes: append([]byte("label"), 0),
	})
}

func (genericTIFF) DefaultLabelDims() (int, int) { return label.AperioLabelWidth, label.AperioLabelHeight }
func (genericTIFF) DefaultLabelBG() color.RGBA   { return label.AperioLabelBG }
```

`isTiled` already exists in `format` (svs.go defines `hasTileTags`); reuse it. If the existing helper is named `hasTileTags`, call that instead of `isTiled`, or add a small `func isTiled(ifd *tifflow.IFD) bool { return hasTileTags(ifd) }` alias — pick one name and use it consistently across svs.go and generictiff.go. (Recommended: keep `hasTileTags` and use it in both.)

- [ ] **Step 2: Write the unit test**

Create `internal/redact/format/generictiff_test.go`:

```go
package format

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cornish/wsi-label-tools/internal/redact"
	"github.com/cornish/wsi-label-tools/internal/tifflow"
)

func parseSample(t *testing.T, p string) *tifflow.File {
	t.Helper()
	if _, err := os.Stat(p); err != nil {
		t.Skipf("fixture absent: %v", err)
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	file, err := tifflow.Parse(f)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return file
}

func TestGenericTIFF_DetectAndLabel(t *testing.T) {
	p := filepath.Join("..", "..", "..", "sample_files", "generic-tiff", "CMU-1-Small-Region.stripped.tiff")
	file := parseSample(t, p)

	g := genericTIFF{}
	// SVS must NOT claim it (prefix was rewritten); generic-TIFF should.
	if (svs{}).Detect(file) {
		t.Fatalf("svs wrongly detected the stripped generic-TIFF")
	}
	if !g.Detect(file) {
		t.Fatalf("genericTIFF did not detect the stripped generic-TIFF")
	}
	targets, err := g.Classify(file)
	if err != nil {
		t.Fatal(err)
	}
	var label *redact.Target
	for i := range targets {
		if targets[i].Role == redact.RoleLabel {
			label = &targets[i]
		}
	}
	if label == nil {
		t.Fatalf("no label classified; targets=%+v", targets)
	}
	if label.Width != 387 || label.Height != 463 {
		t.Fatalf("label dims = %dx%d, want 387x463", label.Width, label.Height)
	}
}
```

- [ ] **Step 3: Run**

Run: `go test ./internal/redact/format/ -run TestGenericTIFF -v` (needs `sample_files/generic-tiff/...`; skips if absent).
Expected: PASS when fixtures present.

- [ ] **Step 4: Build full + commit**

```bash
go build ./... && go test ./internal/redact/... -count=1
git add internal/redact/format/generictiff.go internal/redact/format/generictiff_test.go
git commit -m "redact/format: generic-TIFF classifier (tag 65080 + heuristic ladder), strip+replace"
```

---

## Task 7: generic-TIFF end-to-end (strip + replace) + oracle cross-validation

**Files:** Modify `test/oracle/classify_oracle_test.go`; add end-to-end coverage in `internal/redact/redact_test.go`.

- [ ] **Step 1: End-to-end strip + replace test**

Append to `internal/redact/redact_test.go`:

```go
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
```

NOTE: `assertPrefixIdentical` currently searches for the label IFD by the Aperio `"label"` 2nd-line marker. The generic-TIFF stripped fixture *does* retain `"label 387x463"` second-line descriptions, so it will find the label IFD. If a future fixture lacks that marker, generalize `assertPrefixIdentical` to locate the label via `redact`'s classification instead — but for this fixture it works as-is. Verify the test passes (non-vacuous: it must find the label IFD and compare bytes).

- [ ] **Step 2: Extend the oracle to the generic-TIFF corpus**

In `test/oracle/classify_oracle_test.go`, generalize the presence/offset tests to iterate multiple format dirs. Replace the single-dir loops with a table:

```go
var corpus = []struct {
	dir string // under sample_files/
	ext string
}{
	{"svs", ".svs"},
	{"generic-tiff", ".tiff"},
}
```
and in `TestLabelPresenceAgrees` / `TestLabelOffsetAgrees`, loop over `corpus`, building `dir := filepath.Join("..", "..", "sample_files", c.dir)` and matching `c.ext`. Keep the existing per-file logic. (Philips is validated locally too — see Task 9 — but include `{"philips-tiff", ".tiff"}` only if `AssociatedIFDOffset` agreement holds; for presence it should. If Philips offset disagreement appears, scope the offset test to svs+generic and keep Philips in the presence test.)

- [ ] **Step 3: Run oracle (CGo off)**

Run: `(cd test/oracle && CGO_ENABLED=0 go test ./... -count=1 -v)` with `sample_files/` populated.
Expected: presence + offset agree on svs + generic-tiff (skips per-file where opentile exposes no offset).

- [ ] **Step 4: cross_validate on generic-TIFF (if deps available)**

If `scripts/cross_validate.py` can be pointed at a generic-tiff dir, run it; otherwise rely on the oracle + end-to-end test. (Optional — note in the commit if not run locally.)

- [ ] **Step 5: Commit**

```bash
go test ./... -count=1
git add internal/redact/redact_test.go test/oracle/classify_oracle_test.go
git commit -m "test: generic-TIFF strip+replace end-to-end + oracle cross-validation"
```

---

# Phase 4 — Philips classifier (strip + replace)

## Task 8: `format/philips.go` + unit test

**Files:** Create `internal/redact/format/philips.go`, `internal/redact/format/philips_test.go`

- [ ] **Step 1: Implement the classifier**

Create `internal/redact/format/philips.go`:

```go
package format

import (
	"image"
	"image/color"
	"strings"

	"github.com/cornish/wsi-label-tools/internal/label"
	"github.com/cornish/wsi-label-tools/internal/redact"
	"github.com/cornish/wsi-label-tools/internal/tifflow"
)

func init() { redact.Register(philips{}) }

const (
	philipsSoftwarePrefix    = "Philips DP"
	philipsDescriptionSuffix = "</DataObject>"
)

// philips classifies Philips-TIFF. Detect: IFD0 Software starts "Philips DP"
// AND IFD0 ImageDescription (trimmed) ends "</DataObject>". Classify:
// non-tiled pages by ImageDescription substring ("Label"/"Macro"/"Thumbnail",
// case-sensitive, first match wins) — ported from opentile-go
// formats/philipstiff (v0.36.0).
type philips struct{}

func (philips) Name() string { return "philips" }

func (philips) Detect(file *tifflow.File) bool {
	if len(file.IFDs) == 0 {
		return false
	}
	sw, ok := file.IFDs[0].StringValue(tifflow.TagSoftware)
	if !ok || !strings.HasPrefix(sw, philipsSoftwarePrefix) {
		return false
	}
	desc, ok := file.IFDs[0].StringValue(tifflow.TagImageDescription)
	if !ok {
		return false
	}
	return strings.HasSuffix(strings.TrimSpace(desc), philipsDescriptionSuffix)
}

func (philips) Classify(file *tifflow.File) ([]redact.Target, error) {
	roleFor := func(desc string) (redact.Role, bool) {
		switch {
		case strings.Contains(desc, "Label"):
			return redact.RoleLabel, true
		case strings.Contains(desc, "Macro"):
			return redact.RoleOverview, true
		case strings.Contains(desc, "Thumbnail"):
			return redact.RoleThumbnail, true
		}
		return 0, false
	}
	seen := map[redact.Role]bool{}
	var targets []redact.Target
	for i, ifd := range file.IFDs {
		if i == 0 || hasTileTags(ifd) {
			continue // IFD0 is the tiled baseline; pyramid levels are tiled
		}
		desc, ok := ifd.StringValue(tifflow.TagImageDescription)
		if !ok {
			continue
		}
		role, ok := roleFor(desc)
		if !ok || seen[role] {
			continue
		}
		seen[role] = true
		w, _ := ifd.UintValue(tifflow.TagImageWidth, file.Header.ByteOrder)
		h, _ := ifd.UintValue(tifflow.TagImageLength, file.Header.ByteOrder)
		targets = append(targets, redact.Target{
			Role: role, Representation: redact.SeparableIFD,
			IFDIndex: i, Width: int(w), Height: int(h),
		})
	}
	return targets, nil
}

func (philips) BuildLabelIFD(img image.Image) *tifflow.ReplacementIFD {
	// A minimal ImageDescription containing "Label" so opentile/Philips
	// re-classifies the replaced IFD as the label on re-open.
	return label.BuildStripLabelIFD(img, "Label")
}

func (philips) DefaultLabelDims() (int, int) { return label.AperioLabelWidth, label.AperioLabelHeight }
func (philips) DefaultLabelBG() color.RGBA   { return label.AperioLabelBG }
```

- [ ] **Step 2: Write the unit test**

Create `internal/redact/format/philips_test.go`:

```go
package format

import (
	"path/filepath"
	"testing"

	"github.com/cornish/wsi-label-tools/internal/redact"
)

func TestPhilips_DetectAndLabel(t *testing.T) {
	p := filepath.Join("..", "..", "..", "sample_files", "philips-tiff", "Philips-4.tiff")
	file := parseSample(t, p) // helper from generictiff_test.go (skips if absent)

	ph := philips{}
	if !ph.Detect(file) {
		t.Fatalf("philips did not detect Philips-4.tiff")
	}
	// generic-TIFF is a fallback; philips (vendor) must win in the registry,
	// but here we assert detection directly.
	targets, err := ph.Classify(file)
	if err != nil {
		t.Fatal(err)
	}
	var hasLabel bool
	for _, tg := range targets {
		if tg.Role == redact.RoleLabel {
			hasLabel = true
		}
	}
	if !hasLabel {
		t.Fatalf("no label classified for Philips-4.tiff; targets=%+v", targets)
	}
}
```

- [ ] **Step 3: Run**

Run: `go test ./internal/redact/format/ -run TestPhilips -v` (skips if `sample_files/philips-tiff/` absent).
Expected: PASS when fixtures present.

- [ ] **Step 4: Build + commit**

```bash
go build ./... && go test ./internal/redact/... -count=1
git add internal/redact/format/philips.go internal/redact/format/philips_test.go
git commit -m "redact/format: Philips classifier (Software+DataObject detect, substring classify), strip+replace"
```

---

## Task 9: Philips end-to-end + oracle (local), registry ordering check

**Files:** Modify `internal/redact/redact_test.go`, `test/oracle/classify_oracle_test.go`.

- [ ] **Step 1: End-to-end strip + replace for Philips**

Append to `internal/redact/redact_test.go`:

```go
func TestPhilips_StripAndReplace(t *testing.T) {
	src := filepath.Join("..", "..", "sample_files", "philips-tiff", "Philips-4.tiff")
	if _, err := os.Stat(src); err != nil {
		t.Skipf("fixture absent: %v", err)
	}
	stripOut := filepath.Join(t.TempDir(), "stripped.tiff")
	if err := redact.Strip(src, stripOut, false); err != nil {
		t.Fatalf("Strip: %v", err)
	}
	if _, _, err := redact.LabelDims(stripOut); err == nil {
		t.Fatalf("expected no label after strip")
	}
	repOut := filepath.Join(t.TempDir(), "relabeled.tiff")
	if _, err := redact.Replace(src, repOut, redact.ReplaceOptions{
		Image: newTestLabel(), Resize: redact.ResizeStretch,
	}); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	if _, _, err := redact.LabelDims(repOut); err != nil {
		t.Fatalf("output has no label after replace: %v", err)
	}
}
```

NOTE on `assertPrefixIdentical`: Philips label descriptions are not the Aperio `"label"` 2nd-line form, so the prefix-identity helper (which finds the label by that marker) would early-return for Philips. That's why this test omits it. If you want prefix-identity coverage for Philips, generalize `assertPrefixIdentical` to locate the label IFD via `redact` classification (open file → detect → classify → RoleLabel IFDIndex → use `file.Ranges.MinOffsetOfOwnersAtOrAfter(idx)`); optional, not required for Band 1.

- [ ] **Step 2: Registry-ordering integration check**

Append to `internal/redact/redact_test.go` (this confirms the fallback wiring end-to-end: Philips wins over generic-TIFF on a real Philips file):

```go
func TestPhilips_NotClaimedAsGeneric(t *testing.T) {
	src := filepath.Join("..", "..", "sample_files", "philips-tiff", "Philips-4.tiff")
	if _, err := os.Stat(src); err != nil {
		t.Skipf("fixture absent: %v", err)
	}
	// redact.Replace routes through the registry; if generic-TIFF shadowed
	// Philips, the label IFD chosen could differ. We assert the label is
	// found and replace round-trips (a coarse but real integration signal).
	out := filepath.Join(t.TempDir(), "p.tiff")
	if _, err := redact.Replace(src, out, redact.ReplaceOptions{Image: newTestLabel(), Resize: redact.ResizeStretch}); err != nil {
		t.Fatalf("Replace via registry: %v", err)
	}
}
```

- [ ] **Step 3: Add Philips to the oracle presence test**

In `test/oracle/classify_oracle_test.go`, add `{"philips-tiff", ".tiff"}` to the `corpus` table used by `TestLabelPresenceAgrees` (our label presence must match opentile's). For `TestLabelOffsetAgrees`, include Philips only if opentile exposes `AssociatedIFDOffset` for it; if it returns `ok=false` (likely — only svs+generic-tiff implement it in v0.36), the per-file `t.Skip` already handles it gracefully — leave the table shared.

- [ ] **Step 4: Run everything (fixtures present)**

```bash
go test ./... -count=1 2>&1 | grep -E "ok|FAIL"
(cd test/oracle && CGO_ENABLED=0 go test ./... -count=1 -v)
```
Expected: all green; oracle presence agrees on svs + generic-tiff + philips.

- [ ] **Step 5: Commit**

```bash
git add internal/redact/redact_test.go test/oracle/classify_oracle_test.go
git commit -m "test: Philips strip+replace end-to-end + registry-order + oracle presence"
```

---

# Phase 5 — Docs

## Task 10: README + ROADMAP

**Files:** Modify `README.md`, `docs/ROADMAP.md`

- [ ] **Step 1: Update the README supported-formats table**

Replace the table rows so it reads:

```
| Format | Status | Notes |
|--------|--------|-------|
| Aperio SVS | ✅ V1 | Classic TIFF + BigTIFF |
| generic-TIFF | ✅ | tag 65080 / heuristic; pure splice |
| Philips-TIFF | ✅ | `Software`+`</DataObject>` detect; pure splice |
| Hamamatsu NDPI | 🛣️ Band 2 | Region strategy (decode macro, blank, re-encode) |
| Ventana BIF | 🛣️ Band 2 | overview-role splice |
| Leica SCN | 🛣️ Parked | overview-role + SCN-XML |
| OME-TIFF | ❌ out of charter | SubIFD pyramid — needs file-rewrite (use wsitools) |
| Zeiss CZI / MRXS | ❌ | non-TIFF / directory-based |
```

- [ ] **Step 2: Update ROADMAP**

In `docs/ROADMAP.md`, add under the format-coverage band:

```
- [x] **Band 1 — generic-TIFF + Philips (splice).** Both detect + classify
  (porting opentile-go signals) with strip + replace; fixtures migrated to
  `wsilabs/wsi-fixtures` (v2), committed `sample_svs/` retired. See
  `docs/superpowers/plans/2026-06-07-band1-generic-philips-splice.md`.
- [ ] **Band 2 — BIF + NDPI.** Opens the overview role (BIF) and the Region
  strategy + tifflow hardening (NDPI). OME-TIFF/COG-WSI are out of charter
  (SubIFD / COG-compliance).
```

- [ ] **Step 3: Final full verification + commit**

```bash
go build ./... && go vet ./... && go test ./... -count=1 2>&1 | grep -E "ok|FAIL"
(cd test/oracle && CGO_ENABLED=0 go test ./... -count=1 | tail -1)
git add README.md docs/ROADMAP.md
git commit -m "docs: generic-TIFF + Philips supported; Band 1 roadmap"
```

---

## Self-review checklist (run after writing, fix inline)

- **Spec coverage:** generic-TIFF (Task 6/7) ✓, Philips (Task 8/9) ✓, strip+replace both ✓, fallback ordering (Task 2) ✓, fixture migration + sample_svs retirement (Task 4/5) ✓, oracle extension (Task 7/9) ✓, docs (Task 10) ✓. OME-TIFF/COG-WSI explicitly out (docs) ✓.
- **Type consistency:** `hasTileTags` (svs.go) reused in generictiff.go/philips.go — pick one name; `BuildStripLabelIFD(img, desc, extra...)` signature used by all three formats; `RegisterFallback`/`fallbacks` names consistent between classifier.go and the test.
- **No placeholders:** every code step has full code. The only `<...>` are the size/sha256 values in Task 4, which are computed in that task's Step 2.
- **Risk:** Philips replace writes an LZW/RGB label with a "Label" description into a Philips file; the oracle presence test validates opentile still classifies it. If a strict Philips reader rejects the re-encoded label, that's acceptable for a redaction tool (pyramid untouched) — but flag if the oracle disagrees.

---

## Done criteria for Band 1

- `go build ./... && go vet ./... && go test ./...` green (fixtures via `scripts/fetch-fixtures.sh`).
- `test/oracle` (CGO off) agrees with opentile-go on label presence across svs + generic-tiff + philips.
- `cross_validate.py` still green on SVS (and generic-TIFF if run).
- CI downloads `wsi-fixtures` v2; `sample_svs/` removed; main module still two pure-Go deps.
- `wsi-label replace`/`strip` work on real generic-TIFF and Philips files.
