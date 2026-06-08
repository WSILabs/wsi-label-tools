# wsi-label-tools Roadmap

Living document. Status markers:

- `[ ]` not started
- `[~]` in progress / partially done
- `[x]` done

Grouped by horizon rather than strict priority — work within each band in
whatever order the real workload demands.

---

## V1 finish line (hours, not days)

Work that closes out the original spec and produces the first published
binaries.

- [x] **Cut `v0.1.0`.** Shipped 2026-04-21. Five platform archives published
  on the Releases page. Goreleaser pipeline exercised end-to-end successfully.
- [x] **Windows/macOS build coverage in CI.** `ci.yml` now runs a three-OS
  build matrix in addition to the Linux test+cross-validate job.
- [x] **Wire the deferred replace flags.** `--bg`, `--rotate`, `--resize`,
  `--force` now threaded through `wsi.ReplaceOptions` into
  `aperio.PrepareLabel`. Aspect-mismatch check rejects >2× ratios without
  `--force`.
- [x] **Strengthen `cross_validate.py` label-pixel check.** Now writes the
  test label at exact target dims, runs `wsi-label replace --resize=none`,
  decodes the output label via tifffile, and requires `numpy.array_equal`
  against the input. Pixel-exact round-trip confirmed for LZW+predictor-2.

## Pathology workflow features (days)

Work driven by real lab needs, not speculative. Pick based on which
scanners and which PHI exposures you actually encounter.

- [x] **Band 0 — redact core refactor.** Replaced `internal/aperio` +
  `wsi.Backend` with the format-neutral `internal/redact` core (role-keyed
  targets, classifier registry) + `internal/label` builder + `format/svs`
  classifier, plus an opentile-go classification oracle (test-only nested
  module, `CGO_ENABLED=0`). See
  `docs/superpowers/plans/2026-06-06-band0-redact-core-refactor.md` and
  `docs/superpowers/specs/2026-06-06-multi-format-label-redaction-design.md`.
  Behavior unchanged; cross_validate green.
- [x] **Band 1 — generic-TIFF + Philips (splice).** Both detect + classify
  (porting opentile-go signals) with strip + replace; fixtures migrated to
  `wsilabs/wsi-fixtures` (v2), committed `sample_svs/` retired. Philips label
  path validated locally (only the 3.1 GB Philips-3 has a label; a reduced CI
  fixture is pending). See
  `docs/superpowers/plans/2026-06-07-band1-generic-philips-splice.md`.
- [x] **Band 2 — NDPI (Region strategy).** Label = left ~30% of the macro;
  decode → blank/composite → re-encode → strip-substitution splice; pyramid
  pixel data untouched. New tifflow `SpliceReplaceStripData`. See
  `docs/superpowers/plans/2026-06-07-band2-ndpi-region.md`.
- [x] **Band 3 — BIF (Region, top-1/3 overview).** Label = top 1/3 of the
  overview (the 25mm/75mm label zone); region blank/composite; pyramid
  byte-identical. Region strategy extended for uncompressed-strip + abbreviated
  -JPEG overviews; tifflow `DropTags` + out-of-line reemit offset arrays
  (fixes >4 KB tile tables). DP200 CI-covered (wsi-fixtures v3); legacy OS-1
  local-only (3.6 GB). Label image only — no metadata scrub (out of charter).
  See `docs/superpowers/plans/2026-06-07-band3-bif-region.md`.
- [ ] **Hamamatsu NDPI backend.** TIFF-based, reuses `internal/tifflow`.
  Label detection uses Hamamatsu vendor tags (e.g., "Reference" /
  "Overview" image role indicators), not Aperio-style ImageDescription.
  Likely the most-requested second backend.
- [ ] **Leica SCN backend.** TIFF-based. `ImageDescription` is XML; label
  detection parses that XML rather than line-splitting. Same tifflow reuse.
- [ ] **Ventana BIF backend.** TIFF-based. Similar to Aperio in structure.
- [ ] **Macro scrubbing.** The `macro` associated-image is a low-res photo
  of the physical slide; if a label sticker was in frame it can carry PHI
  (text on the label, handwritten marginalia). Add `--macro-policy=keep|strip|blur`
  to `replace` and `strip`. Blur policy would require a pixel decode path,
  which we've avoided — weigh the tradeoff, or stick to keep/strip.
- [ ] **ImageDescription scrubbing.** Aperio writes scanner serial, scan
  date, and operator name into the baseline IFD's ImageDescription. Not
  strictly PHI but identifying. Add an opt-in `--scrub-description` flag
  that rewrites the baseline IFD's ImageDescription, keeping only the
  structural bits (Aperio header, pyramid params) and dropping freeform
  text. Requires IFD rewrite which the splice primitive already supports.
- [ ] **Native batch mode.** `wsi-label batch --csv labels.csv` — move
  what `label-slides.py` does (minus font rendering) into the Go binary so
  sites without Python can deploy a single static binary. Font rendering
  stays in Python; this is for the "I already have 1200×848 PNG labels,
  apply them in bulk" case.
- [ ] **Audit-log option.** `--audit-dir=path/` writes the old label
  image + a JSON manifest (input SHA-256, output SHA-256, timestamp,
  replacement label SHA-256) to the audit dir before replacement. For
  legal hold / records retention. Several-lab-use-case adjacent.

## Bigger lifts (weeks) — only if needed

Only pursue when you have a concrete file the current tool doesn't handle.

- [ ] **DICOM WSI.** Completely different container (DICOM, not TIFF).
  Separate engine under `internal/dicom/`. Same `wsi.Backend` interface.
  Probably needs a DICOM library dep (there are pure-Go options).
- [ ] **Zeiss CZI.** Proprietary container. No good pure-Go parser exists;
  may require CGo against libCZI. Heavy lift.
- [ ] **3DHISTECH MRXS.** Directory-based format (`.mrxs` + `.dat` files).
  Different semantics than single-file splice; need to think about whether
  the primitive is still "replace the label" or becomes "edit the ini".
- [ ] **Fuzz testing** of the TIFF parser (`go test -fuzz=FuzzParse`).
  Diminishing returns given the invariant checks, but defense in depth.
- [ ] **Distribution polish.** Homebrew tap, Scoop bucket, APT repo,
  prominent `go install` instructions. Only valuable once other people
  start using it.
- [ ] **Man pages** for the CLI. goreleaser can generate from a simple
  spec; would be nice for packaged builds.

## Infrastructure / hygiene backlog

Small improvements that don't block anything but are worth doing as you
touch related code.

- [ ] **Signal handling (SIGINT cleanup).** Today a killed mid-run process
  leaves a `.tmp-*` file. Spec flagged this as acceptable for V1; the
  temp-file prefix makes orphans easy to spot. Low priority.
- [ ] **`--json` output on `inspect`.** Spec reserved the slot; easy add.
- [ ] **Goreleaser config review after first tag.** We might want to tune
  archive layout, binary naming, or ship additional artifacts (source tarball,
  .deb, .rpm).
- [ ] **Memory note pruning.** Periodically revisit the project memory in
  `~/.claude/projects/...` — prune entries that have become stale or
  redundant with what's now in the committed code/docs.
