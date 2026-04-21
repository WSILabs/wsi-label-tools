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

- [ ] **Cut `v0.1.0`.** `git tag v0.1.0 && git push origin v0.1.0`. Verifies
  the goreleaser pipeline end-to-end and puts downloadable Linux/macOS/Windows
  binaries on the Releases page. Small risk: `goreleaser` is only exercised
  for the first time here, so watch the Actions run. If it fails, iterate on
  a `v0.1.0-rc1` tag and re-tag.
- [ ] **Windows/macOS build coverage in CI.** Today `ci.yml` only runs on
  `ubuntu-latest`. Add a matrix that does `go build` on `windows-latest` and
  `macos-latest` (tests can stay Linux-only — goreleaser cross-compiles
  from Linux, this is a cheap smoke check that platform-specific code
  paths still compile). Catches regressions before they land in a tag.
- [ ] **Wire the deferred replace flags.** `--bg=RRGGBB`, `--rotate=0|90|180|270`,
  `--resize=fit|stretch|none`, `--force`. All parsed today in
  `internal/cli/replace.go` but not threaded into `aperio.backend.ReplaceLabel`.
  Modest UX gain with low implementation cost.
- [ ] **Strengthen `cross_validate.py` label-pixel check.** Currently asserts
  non-trivial variance. Upgrade to: decode the output label via libtiff,
  compare to the input label after the known aspect-fit resize + Aperio BG
  letterbox, require pixel equality within a small tolerance (LZW is
  lossless but resize interpolation isn't bit-exact across runs). Would
  pin the LZW class-of-bug fix definitively.

## Pathology workflow features (days)

Work driven by real lab needs, not speculative. Pick based on which
scanners and which PHI exposures you actually encounter.

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
