# Band 3 — Ventana BIF label region Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Redact the BIF label — the **top 1/3 of the overview image (IFD 0)**, which is the physical 25 mm-of-75 mm label zone — by blanking (strip) or compositing a new label (replace) into that region. Reuses the Band 2 Region strategy, extended to BIF's overview codecs. **Label image only — no XMP/metadata scrub (out of charter).**

**Architecture:** A `format/bif.go` classifier detects BIF and emits a `RoleLabel` `EmbeddedRegion` target with `Region = Rect(0, 0, W, H/3)`, host = IFD 0 (the overview). The Region strategy (`strategy_region.go`) is generalized from NDPI's single-baseline-JPEG-strip assumption to also decode/re-encode BIF's overview layouts: **uncompressed multi-strip RGB** (DP 200) and **single-tile *abbreviated* JPEG** (legacy OS-1 — tables in the `JPEGTables` tag). Writing reuses `tifflow.SpliceReplaceStripData`, extended to optionally drop a tag (`JPEGTables`, when a re-encoded JPEG becomes self-contained).

**Tech Stack:** Go 1.26; `internal/tifflow` (strip/tile substitution + tag-drop); `internal/redact` (Region strategy); stdlib `image/jpeg`; opentile-go (test oracle — note: opentile exposes no BIF `label`, so BIF gets no presence-oracle until [WSILabs/opentile-go#19] lands; validation is e2e + local).

**Scope:** Both variants. **DP 200 (Ventana-1, 217 MB)** → wsi-fixtures v3, CI-covered. **Legacy OS-1 (3.6 GB)** → local-only (sibling `opentile-go/sample_files`, skip in CI). Multi-tile/multi-strip *JPEG* overviews beyond these two shapes → clean "unsupported BIF overview layout" error (defer).

**Module path:** `github.com/cornish/wsi-label-tools`. **Branch:** `band3-bif`.

**Reference spec:** `docs/superpowers/specs/2026-06-06-multi-format-label-redaction-design.md` (BIF section, amended 2026-06-07).

**Grounding facts (probed):**
- Detect: BigTIFF + XMP tag 700 contains `<iScan`. Overview = IFD 0 (`ImageDescription` "Label Image"/"Label_Image").
- OS-1: overview tiled abbreviated JPEG, **1 tile** 1008×3008, has `JPEGTables` (347). Label ≈ top 1000 rows (≈1/3).
- Ventana-1: overview **uncompressed** (comp 1), **58 strips**, 1251×3685, RGB. Label visibly top ~20% (inside top 1/3).
- Pyramid (IFD 2+) is PHI-free. IFD 1 (OS-1 thumbnail / Ventana-1 probability) is not a label — leave it.

---

## Task 1: tifflow — drop-tag option for strip/tile substitution

**Files:** Modify `internal/tifflow/splice.go`; Test `internal/tifflow/splice_test.go`

When we re-encode an abbreviated-JPEG overview as a self-contained JPEG, the original `JPEGTables` (347) tag must be removed (else readers see duplicate tables). Extend `SpliceReplaceStripData` to drop caller-named tags from the re-emitted target IFD.

- [ ] **Step 1: Failing test** — append to `splice_test.go`. Build a small stripped TIFF (x/image/tiff), parse, splice with `DropTags` naming a tag the IFD has (e.g. a tag you confirm is present — use `TagRowsPerStrip`(278) or add a synthetic; simplest: assert that after substitution with `DropTags: []uint16{TagPredictor}` (317, if present) or any present non-essential tag, the output IFD no longer contains that tag). Concretely, pick a tag the x/image writer emits (read the parsed IFD's entries to choose one that's safe to drop, e.g. `TagRowsPerStrip` is essential — instead use a tag that exists and is non-structural; if none, add the drop-tag assertion against `TagImageDescription` after first injecting one). Keep it simple:

```go
func TestSpliceReplaceStripData_DropTags(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 16, 8))
	var buf bytes.Buffer
	if err := xtiff.Encode(&buf, src, &xtiff.Options{Compression: xtiff.Uncompressed}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	inPath := filepath.Join(dir, "in.tiff")
	os.WriteFile(inPath, buf.Bytes(), 0o644)
	f, _ := os.Open(inPath)
	file, err := Parse(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	// Confirm the writer emitted a Predictor or pick any present tag to drop.
	var dropTag uint16 = TagPhotometric // present in baseline RGB TIFF, safe to drop for this test
	found := false
	for _, e := range file.IFDs[0].Entries {
		if e.Tag == dropTag {
			found = true
		}
	}
	if !found {
		t.Skipf("writer did not emit tag %d; adjust test", dropTag)
	}
	offs, _ := file.IFDs[0].UintArray(TagStripOffsets, file.Header.ByteOrder)
	newStrips := make([][]byte, len(offs))
	for i := range newStrips {
		newStrips[i] = []byte("X")
	}
	outPath := filepath.Join(dir, "out.tiff")
	if err := Splice(SpliceParams{
		InPath: inPath, OutPath: outPath, File: file,
		Mode: SpliceReplaceStripData, TargetIdx: 0, StripData: newStrips,
		DropTags: []uint16{dropTag},
	}); err != nil {
		t.Fatalf("Splice: %v", err)
	}
	g, _ := os.Open(outPath)
	defer g.Close()
	out, err := Parse(g)
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	for _, e := range out.IFDs[0].Entries {
		if e.Tag == dropTag {
			t.Fatalf("tag %d should have been dropped", dropTag)
		}
	}
}
```

- [ ] **Step 2: Run → FAIL** (`SpliceParams.DropTags` undefined).

- [ ] **Step 3: Implement.** Add `DropTags []uint16` to `SpliceParams` (doc: tags to omit from the re-emitted target IFD, for SpliceReplaceStripData). In `reemitIFD`'s override path, when emitting entries, skip any entry whose tag is in `DropTags`, and **decrement the emitted entry count accordingly** (the IFD entry-count header must match the number of entries actually written). READ `reemitIFD` to find where the entry count is written and where entries are looped; filter the entry list up front (`entries := filterDropped(ifd.Entries, dropTags)`) and use its length for the count and the loop. Thread `DropTags` from `doSplice` into `reemitIFD` (add a param, nil for non-override callers / existing modes).

- [ ] **Step 4: Verify + commit.** `go test ./internal/tifflow/ -count=1` (new test + all existing pass), `go build ./...`, `gofmt -l internal/tifflow/`.
```bash
git add internal/tifflow/splice.go internal/tifflow/splice_test.go
git commit -m "tifflow: DropTags option for SpliceReplaceStripData (drop JPEGTables on JPEG re-encode)"
```

---

## Task 2: Region strategy — generalize host decode/encode for BIF codecs

**Files:** Modify `internal/redact/strategy_region.go`; Test `internal/redact/strategy_region_test.go` (new)

Today `hostJPEG` assumes a single baseline-JPEG strip (NDPI). Generalize to decode and re-encode the host overview across: uncompressed multi-strip RGB; baseline JPEG single strip; single-tile abbreviated JPEG (merge `JPEGTables`).

- [ ] **Step 1: Implement the generalized host decode/encode.** Replace `hostJPEG`/`writeRegion` with codec-aware versions. Key pieces:

```go
// hostInfo captures the host IFD's pixel layout for decode + re-encode.
type hostInfo struct {
	w, h, spp int
	comp      uint64   // 1 = uncompressed, 7 = JPEG
	tiled     bool
	tileW, tileH int
	rowsPerStrip int
	offs, cnts []uint64
	jpegTables []byte // tag 347, if present (abbreviated JPEG)
}

func readHostInfo(inPath string, file *tifflow.File, idx int) (*hostInfo, error) { /* read tags + offsets/counts + JPEGTables bytes */ }

// decodeHost returns the host overview as an RGBA image.
func decodeHost(inPath string, file *tifflow.File, hi *hostInfo) (*image.RGBA, error) {
	// comp==1: assemble raw RGB strips → RGBA.
	// comp==7: for each strip/tile, mergeAbbrev(tileBytes, hi.jpegTables) if jpegTables != nil, jpeg.Decode, draw into canvas.
	//   single strip/tile → whole image; multi-tile → grid (ceil(w/tileW) across); multi-strip JPEG → vertical stack by rowsPerStrip.
	//   Unsupported combos → error "unsupported BIF overview layout".
}

// mergeAbbrev builds a self-contained JPEG from abbreviated tile/strip bytes +
// the JPEGTables blob (SOI + tables-from-JPEGTables + body-after-SOI).
func mergeAbbrev(body, tables []byte) []byte { /* as proven during extraction */ }

// encodeHost re-encodes the modified image in the host's codec and splices it
// into outPath via SpliceReplaceStripData.
func encodeHost(inPath, outPath string, file *tifflow.File, idx int, hi *hostInfo, img *image.RGBA, fsync bool) error {
	if hi.comp == 1 {
		// Serialize RGBA → raw RGB (spp), re-split into the SAME strip layout
		// (rowsPerStrip), substitute all strips (lengths unchanged). No tag drop.
		return tifflow.Splice(tifflow.SpliceParams{InPath: inPath, OutPath: outPath, File: file,
			Mode: tifflow.SpliceReplaceStripData, TargetIdx: idx, StripData: rawStrips, Fsync: fsync})
	}
	// comp == 7 (JPEG): re-encode the WHOLE image as one self-contained baseline
	// JPEG, write it as the single strip/tile, and DROP JPEGTables (now redundant).
	// Only the single-strip / single-tile case is supported here.
	if len(hi.offs) != 1 {
		return fmt.Errorf("redact: unsupported multi-unit JPEG overview (re-tiling not supported)")
	}
	var jb bytes.Buffer
	jpeg.Encode(&jb, img, &jpeg.Options{Quality: 90})
	drop := []uint16(nil)
	if hi.jpegTables != nil {
		drop = []uint16{tifflow.TagJPEGTables}
	}
	return tifflow.Splice(tifflow.SpliceParams{InPath: inPath, OutPath: outPath, File: file,
		Mode: tifflow.SpliceReplaceStripData, TargetIdx: idx, StripData: [][]byte{jb.Bytes()},
		DropTags: drop, Fsync: fsync})
}
```

Then rewrite `regionStrip`/`regionReplace` to use `readHostInfo` → `decodeHost` → modify region → `encodeHost`. (The `Target.Region` + blank/composite logic from Band 2 is unchanged.) Keep the NDPI path working: NDPI's macro is a single baseline-JPEG strip with no JPEGTables → `decodeHost` decodes it (single strip, comp 7, no tables) and `encodeHost` re-encodes single-strip JPEG (no drop) — same result as before. **Verify the NDPI e2e still passes after the refactor.**

- [ ] **Step 2: Unit test the codecs** (synthetic, fast). Create `strategy_region_test.go` with: (a) an uncompressed stripped TIFF (x/image, `Compression: Uncompressed`) → decodeHost returns correct dims/pixels; (b) a baseline-JPEG single-strip → decodeHost works. (Abbreviated-JPEG decode is covered by the OS-1 e2e in Task 5, local-only.)

- [ ] **Step 3: Verify NDPI unaffected + commit.** `go test ./internal/redact/... -count=1` (incl. `TestNDPI_StripAndReplace` — must still pass), `go build ./...`, `gofmt -l`.
```bash
git add internal/redact/strategy_region.go internal/redact/strategy_region_test.go
git commit -m "redact: generalize Region strategy host decode/encode (uncompressed strips, abbreviated JPEG)"
```

---

## Task 3: BIF classifier

**Files:** Create `internal/redact/format/bif.go`, `internal/redact/format/bif_test.go`

- [ ] **Step 1: Implement `format/bif.go`.**

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

func init() { redact.Register(bif{}) }

const bifLabelFrac = 3 // label = top 1/H of the overview (25mm of 75mm)

// bif classifies Ventana BIF. The label is the top 1/3 (label zone) of the
// overview (IFD 0). Detect: BigTIFF + an IFD whose XMP (tag 700) contains
// "<iScan". Redacted by the Region strategy (top band).
type bif struct{}

func (bif) Name() string { return "bif" }

func (bif) Detect(file *tifflow.File) bool {
	if !file.Header.BigTIFF || len(file.IFDs) == 0 {
		return false
	}
	// XMP (700) on IFD0 contains "<iScan".
	for _, e := range file.IFDs[0].Entries {
		if e.Tag == 700 {
			if x, ok := file.IFDs[0].XMPString(); ok && strings.Contains(x, "<iScan") {
				return true
			}
		}
	}
	return false
}

func (bif) Classify(file *tifflow.File) ([]redact.Target, error) {
	// Overview = IFD 0 (Label_Image). Label = top 1/3.
	ifd := file.IFDs[0]
	w, _ := ifd.UintValue(tifflow.TagImageWidth, file.Header.ByteOrder)
	h, _ := ifd.UintValue(tifflow.TagImageLength, file.Header.ByteOrder)
	if w == 0 || h == 0 {
		return nil, nil
	}
	bandH := int(h) / bifLabelFrac
	return []redact.Target{{
		Role:           redact.RoleLabel,
		Representation: redact.EmbeddedRegion,
		HostIFDIndex:   0,
		Region:         image.Rect(0, 0, int(w), bandH),
		Width:          int(w),
		Height:         bandH,
	}}, nil
}

func (bif) BuildLabelIFD(img image.Image) *tifflow.ReplacementIFD {
	return label.BuildAperioLabelIFD(img) // vestigial; Region path composites opts.Image
}

func (bif) DefaultLabelDims() (int, int) { return label.AperioLabelWidth, label.AperioLabelHeight }
func (bif) DefaultLabelBG() color.RGBA   { return label.AperioLabelBG }
```

This needs a helper to read the XMP (tag 700) bytes as a string. Add to `internal/tifflow/ifd.go`:
```go
// XMPString returns the tag-700 (XMP) value as a string. The value may be
// out-of-line; callers that only have the parsed IFD use this for inline/
// short XMP. For out-of-line XMP, prefer reading e.DataOffset/DataSize from
// the file — but Detect only needs a substring match, so expose the raw bytes.
```
Actually, the parsed IFD does not hold out-of-line bytes (only DataOffset/DataSize). `XMPString()` cannot read the file. So **Detect must take the file path** OR the classifier must read tag 700 bytes from the file. Simplest: give the BIF `Detect` access by having it read tag 700's out-of-line bytes — but `Classifier.Detect(file *tifflow.File)` has no reader. RESOLUTION: the parser already fetches small out-of-line values eagerly? Check `internal/tifflow/parse.go` / `ifd.go` — if `StringValue`/inline-value fetching covers tag 700 when small (OS-1 XMP=330B, Ventana=813B), then add an `XMPString()` that returns the fetched value. If the parser does NOT retain out-of-line bytes, add a one-time eager fetch of tag 700 during parse (it's tiny), stored on the entry, exposed via `XMPString()`. Pick whichever fits the existing parser; the BIF Detect needs the `<iScan>` substring from tag 700.

> Implementation note: verify how the parser handles out-of-line values. Earlier probes read tag-700 bytes via `e.DataOffset`/`e.DataSize` from the open file. The cleanest minimal change: during `Parse`, for tag 700 specifically (or all out-of-line values ≤ a small cap), eagerly read the bytes into the entry (e.g. `e.InlineValue`), and have `XMPString()`/`StringValue` return them. Confirm `StringValue` already does this for ImageDescription (it must, since `Detect` for SVS/Philips reads desc) — if so, tag 700 likely needs the same eager fetch; extend it.

- [ ] **Step 2: Unit test `bif_test.go`** (local-only OS-1 via sibling + CI Ventana-1 via sample_files):
```go
package format

import (
	"path/filepath"
	"testing"

	"github.com/cornish/wsi-label-tools/internal/redact"
)

func TestBIF_DetectAndRegion(t *testing.T) {
	cases := []struct{ path string; w, h int }{
		{filepath.Join("..", "..", "..", "sample_files", "bif", "Ventana-1.bif"), 1251, 3685},
		{filepath.Join("..", "..", "..", "..", "opentile-go", "sample_files", "bif", "OS-1.bif"), 1008, 3008},
	}
	for _, c := range cases {
		file := parseSample(t, c.path) // skips if absent
		b := bif{}
		if !b.Detect(file) {
			t.Fatalf("%s: not detected as BIF", c.path)
		}
		targets, err := b.Classify(file)
		if err != nil || len(targets) != 1 {
			t.Fatalf("%s: classify=%+v err=%v", c.path, targets, err)
		}
		tg := targets[0]
		if tg.Role != redact.RoleLabel || tg.Representation != redact.EmbeddedRegion || tg.HostIFDIndex != 0 {
			t.Fatalf("%s: got %+v", c.path, tg)
		}
		if tg.Width != c.w || tg.Height != c.h/3 {
			t.Fatalf("%s: region %dx%d, want %dx%d", c.path, tg.Width, tg.Height, c.w, c.h/3)
		}
	}
}
```

- [ ] **Step 3: Verify + commit.** `go build ./... && go test ./internal/redact/format/ -run TestBIF -v -count=1` (Ventana-1 from sample_files runs; OS-1 runs if sibling present, else skips). `gofmt -l`.
```bash
git add internal/redact/format/bif.go internal/redact/format/bif_test.go internal/tifflow/ifd.go
git commit -m "redact/format: BIF classifier (detect <iScan>; overview top-1/3 EmbeddedRegion target)"
```

---

## Task 4: wsi-fixtures v3 (add Ventana-1.bif) + consume it

**Repo:** `/Volumes/Ext/GitHub/wsi-fixtures` (separate) — controller publishes the release; subagent prepares.

- [ ] **Step 1 (subagent): prepare the fixture.** Mirror the Band 1 procedure: copy `/Volumes/Ext/GitHub/opentile-go/sample_files/bif/Ventana-1.bif` (217 MB) → `fixtures/bif/Ventana-1.bif`; add `.LICENSE` (CC0, copy existing) + `.PROVENANCE` (source `https://openslide.cs.cmu.edu/download/openslide-testdata/Ventana/Ventana-1.bif`, sha256, date, CC0 — match existing PROVENANCE key format); add a `manifest.json` entry (`tarball_asset: bif.tar`); run `scripts/verify.sh` (PASS) + `scripts/pack.sh` (produces `dist/bif.tar`). LOCAL commit only. STOP before push/release.
- [ ] **Step 2 (controller): publish v3.** Review, `git push`, then `gh release create v3 dist/*.tar --repo WSILabs/wsi-fixtures --title "v3 — add bif" --notes "Adds Ventana-1.bif (DP200, CC0)."`. (Immutable — verify assets after.)
- [ ] **Step 3 (wsi-label-tools): bump fixtures to v3.** Update the CI "Download fixtures" step and `scripts/fetch-fixtures.sh` from `v2` → `v3` (both jobs). Re-fetch locally: `./scripts/fetch-fixtures.sh`. Commit.
```bash
git add .github/workflows/ci.yml scripts/fetch-fixtures.sh
git commit -m "ci: bump wsi-fixtures to v3 (adds bif/Ventana-1.bif)"
```

---

## Task 5: BIF end-to-end (region blanked, pyramid untouched)

**Files:** Modify `internal/redact/redact_test.go`

- [ ] **Step 1: e2e for both variants** (Ventana-1 from sample_files = CI; OS-1 from sibling = local, skip-if-absent). For each: `redact.Strip` then `redact.Replace`; assert (a) output re-opens (`redact.LabelDims` resolves, or re-Parse + classify succeeds), (b) the pyramid (IFD 2+) tile/strip data is byte-identical (adapt the NDPI `assertNDPIPyramidIdentical` idea: compare max pyramid-pixel-end of IFDs 2+ — careful, BIF IFD records may interleave; compare each pyramid IFD's tile/strip byte-ranges individually for robustness), and (c) the overview's pixel data **changed** (top-1/3 redacted) — re-decode the overview and check the top band differs from the input's top band (non-vacuous). For OS-1, also assert the output overview decodes **without** needing JPEGTables (self-contained after re-encode) — i.e. `JPEGTables` tag is gone and stdlib `jpeg.Decode` of the tile succeeds.

```go
func TestBIF_StripAndReplace(t *testing.T) {
	cases := []string{
		filepath.Join("..", "..", "sample_files", "bif", "Ventana-1.bif"),
		filepath.Join("..", "..", "..", "opentile-go", "sample_files", "bif", "OS-1.bif"),
	}
	for _, src := range cases {
		if _, err := os.Stat(src); err != nil {
			t.Logf("skip %s: %v", src, err)
			continue
		}
		t.Run(filepath.Base(src), func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "stripped.bif")
			if err := redact.Strip(src, out, false); err != nil {
				t.Fatalf("Strip: %v", err)
			}
			if _, _, err := redact.LabelDims(out); err != nil {
				t.Fatalf("output not re-openable: %v", err)
			}
			assertBIFPyramidIdentical(t, src, out)        // IFD2+ tile data unchanged
			assertBIFOverviewChanged(t, src, out)         // overview top band redacted (non-vacuous)

			rep := filepath.Join(t.TempDir(), "relabeled.bif")
			if _, err := redact.Replace(src, rep, redact.ReplaceOptions{Image: newTestLabel()}); err != nil {
				t.Fatalf("Replace: %v", err)
			}
			assertBIFPyramidIdentical(t, src, rep)
		})
	}
}
```
Implement `assertBIFPyramidIdentical` (compare each IFD≥2 tile/strip byte-range input vs output — they're copied verbatim by the splice since they precede the overview... NOTE: BIF puts the overview at IFD 0, *before* the pyramid in IFD order, but the pyramid's tile DATA may be earlier or later in the file than the overview's strip data. The splice cutoff = overview IFD0's data start; everything before is copied verbatim, everything after (incl. pyramid data if it's after) is re-emitted. So compare pyramid tile/strip BYTES by reading them at their (possibly relocated) offsets in BOTH files — read via each file's own parsed offsets, compare the bytes). Implement robustly: parse both in/out, for each pyramid IFD read its tile/strip bytes from the respective file, `bytes.Equal`. And `assertBIFOverviewChanged`: decode IFD0 overview top band in both (using the same decode path), assert different.

> This is the critical correctness gate. If pyramid bytes differ or the overview top band is unchanged, there's a real bug — report it, don't weaken.

- [ ] **Step 2: Verify + commit.** `go test ./internal/redact/ -run TestBIF -v -count=1` (Ventana-1 runs in CI scope; OS-1 runs locally). Full `go test ./... -count=1`.
```bash
git add internal/redact/redact_test.go
git commit -m "test: BIF strip+replace e2e (top-1/3 overview redacted, pyramid byte-identical); both variants"
```

---

## Task 6: Docs

**Files:** `README.md`, `docs/ROADMAP.md`

- [ ] **Step 1: README supported-formats** — change Ventana BIF row to:
```
| Ventana BIF | ✅ | Region strategy (blank/composite the overview's top-1/3 label band); DP200 + legacy |
```
- [ ] **Step 2: ROADMAP** — mark Band 3 done:
```
- [x] **Band 3 — BIF (Region, top-1/3 overview).** Label = top 1/3 of the
  overview (25mm/75mm zone); region blank/composite; pyramid untouched.
  Region strategy extended for uncompressed-strip + abbreviated-JPEG overviews;
  tifflow DropTags. DP200 CI-covered (wsi-fixtures v3); legacy OS-1 local-only
  (3.6 GB). Label image only — no metadata scrub (out of charter). See
  `docs/superpowers/plans/2026-06-07-band3-bif-region.md`.
```
- [ ] **Step 3: Final verify + commit.** `go build ./... && go vet ./... && go test ./... -count=1`; oracle `CGO_ENABLED=0 go test ./...`; `gofmt -l internal/ test/`.
```bash
git add README.md docs/ROADMAP.md
git commit -m "docs: BIF supported (Region top-1/3); Band 3 roadmap"
```

---

## Self-review checklist
- **Spec coverage:** detect+classify top-1/3 (T3) ✓; Region codec generalization — uncompressed strips + abbreviated JPEG (T2) ✓; tifflow DropTags for JPEGTables (T1) ✓; strip+replace both variants e2e (T5) ✓; DP200 CI / OS-1 local fixtures (T4/T5) ✓; label-image-only, no XMP scrub ✓; docs (T6) ✓.
- **NDPI not regressed:** T2 refactor keeps NDPI's single-strip-baseline-JPEG path working (verify `TestNDPI_StripAndReplace` passes after T2).
- **No oracle for BIF** (opentile exposes no BIF label) — validation is e2e + local + visual; noted, with upstream #19 to add it later.
- **Purity:** stdlib jpeg + x/image only; opentile-go test-only; main module two deps.
- **Type consistency:** `SpliceParams.DropTags`, `hostInfo`, `decodeHost`/`encodeHost`/`mergeAbbrev`, `XMPString`, `bifLabelFrac` used consistently.

## Done criteria
- `go build/vet/test ./...` green; oracle green; main module two pure-Go deps.
- `wsi-label strip`/`replace` redact the top-1/3 label band of BIF overviews (Ventana-1 in CI; OS-1 locally), pyramid byte-identical, output re-opens; OS-1 output overview is self-contained JPEG (JPEGTables dropped).
- wsi-fixtures v3 published with bif; CI bumped to v3.
