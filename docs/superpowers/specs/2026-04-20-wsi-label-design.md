# wsi-label — Design

**Date:** 2026-04-20
**Status:** Draft for review
**Author:** brainstormed with the user, 2026-04-20

## Purpose

Replace (or strip) the "label" associated-image inside a whole-slide-image (WSI) file — the small camera photo of the slide's physical label sticker, which typically contains patient identifiers.

Replaces an existing C tool (`replaceLabelInSvs.c` / `ReplaceLabelInSvs.exe`) that is unreliable on some inputs. The new tool must:

- Be implemented in Go, for easy cross-platform distribution.
- Be as fast as practical and highly reliable.
- Work regardless of SVS file size, from small single-IFD slides up through multi-gigabyte BigTIFF.
- Work regardless of whether optional associated images (label, macro, thumbnail) are present.
- Also support removing the label outright.
- Prefer pure Go (no CGo) where practical.
- Be architected so that adding other WSI formats (Hamamatsu NDPI, Leica SCN, etc.) later is a drop-in, not a rewrite — hence the `wsi` naming rather than `svs`.

Today's scope: Aperio SVS only.

## Non-goals

- Reading or rendering pixel data from the pyramid.
- Re-compressing or re-encoding the base image or pyramid levels.
- Generating label text / rendering fonts. The caller provides a pre-rendered image; `wsi-label` decodes and re-encodes it to the format's label spec.
- Supporting non-TIFF-based WSI formats (DICOM WSI, Zeiss CZI, 3DHISTECH MRXS) today. The architecture leaves room; the code does not.
- GUI. CLI only.

## Core approach

**Copy prefix byte-identical; re-emit tail.**

Given an SVS file, find the label IFD in the TIFF directory chain. Compute a **cutoff** offset = the minimum file offset of any byte owned by the label IFD or any IFD after it in the chain. Then:

1. Copy bytes `[0, cutoff)` from input to output, unchanged. This preserves the baseline IFD, thumbnail IFD, and all pyramid IFDs with their tile data at bit-identical offsets.
2. Write a new label IFD + its strip data immediately after the prefix.
3. Re-emit every IFD that came after the label in the chain (typically just the macro IFD, if present), with internal offsets rebased to its new position. Bulk strip/tile data is copied verbatim from the input.
4. Patch exactly one `next-IFD` pointer inside the copied prefix — the one that originally pointed at the label — to point at the new label's position.
5. `fsync` and rename the temp output into place.

Rationale vs alternatives:

- **vs. full rewrite (the C tool's approach):** full rewrite re-emits gigabytes of pyramid tile data through the user-space program, giving the tool many opportunities to subtly corrupt a tag. This approach never touches pyramid bytes.
- **vs. in-place surgical edit with offset patching:** in-place editing leaves the old label's bytes as unreferenced dead space in the file, recoverable by `strings`, `binwalk`, or forensic tools. For a label that contains patient identifiers, that's a PHI leak. The "copy prefix, re-emit tail" approach doesn't leave dead space by construction — the old label's bytes never make it into the output file.
- **vs. in-place edit of the original file:** fastest, but destroys the input and offers no rollback on failure. Non-starter for medical data.

IO cost is essentially the same as `cp` — one sequential read and one sequential write of the input. Disk-throughput-bound.

## Reliability invariants

The tool is designed around a small number of invariants that are checked, not assumed:

1. **Label is identified by `ImageDescription`, never by IFD ordinal.** OpenSlide does the same (`src/openslide-vendor-aperio.c` in the OpenSlide repo): label = any stripped IFD other than IFD 1 whose `ImageDescription` second line starts with `label`. Position in the chain is informational only; the Aperio spec documents label/macro as "at the end" but tools don't enforce that.
2. **The output's prefix `[0, cutoff)` is bit-identical to the input.** Directly testable via `cmp`. This is the strongest single guarantee that the pyramid was not disturbed.
3. **No IFD stores out-of-line data past the cutoff.** Verified during parse. If any earlier IFD (baseline, thumbnail, pyramid level) has a `StripOffsets` array, `ImageDescription`, `JPEGTables`, or other out-of-line blob physically located past the label IFD's start, the tool refuses to run (exit 5). In practice Aperio writes files in chain order and this never trips; the check exists to catch pathological cases before they become silent corruption.
4. **All recorded byte ranges are non-overlapping.** A byte-range overlap in parsing means the parser is wrong; the tool refuses to proceed.
5. **Input is opened read-only and never modified.** Enforced by opening with `O_RDONLY`. Output is written to a temp file and renamed atomically.
6. **Input and output paths must resolve to different files.** `wsi-label` refuses to run otherwise (matches the original C tool's invariant, prevents a class of footguns).

## Architecture

```
cmd/wsi-label/             CLI entry point, format-agnostic.
internal/wsi/              Format-neutral interfaces.
  backend.go               type Backend interface {
                             Detect(path) (bool, error)
                             ReplaceLabel(in, out string, label image.Image) error
                             StripLabel(in, out string) error
                           }
  registry.go              Probes backends in order; returns first match.
internal/tifflow/          Shared TIFF / BigTIFF primitives. Vendor-agnostic.
  parse.go                 Reads header, walks IFD chain, builds range map.
  ranges.go                Range map: all byte ranges owned by the file,
                           indexed by owning IFD. Overlap detection.
  splice.go                Splice(in, out, labelIFDIdx, newIFD, newStrips):
                           copies prefix, writes replacement, re-emits tail.
internal/aperio/           Aperio SVS backend.
  backend.go               Implements wsi.Backend. Composes tifflow + label.
  detect.go                File extension + first IFD's ImageDescription
                           starts with "Aperio".
  label.go                 EncodeLabel(img image.Image) → tifflow.ReplacementIFD
                           with Aperio-specific tags and LZW+predictor-2 data.
  resize.go                Aspect-preserving fit to 1200x848 with fill color.
```

Boundaries that matter:

- `tifflow` knows nothing about labels, macros, or Aperio. It reads and writes TIFF structure at byte level. A future Hamamatsu backend reuses it unchanged.
- `wsi.Backend` is the single seam between CLI and format-specific code. Adding a backend = one package + one registry entry.
- Label encoding is a per-backend concern. Aperio's 1200×848 LZW-predictor-2 spec lives in `internal/aperio/label.go`. A future Hamamatsu backend with JPEG-encoded labels at different dimensions owns its own encoder.

Today the registry has one entry (`aperio`), so CLI calls `aperio.New()` directly. The `wsi.Backend` interface is still defined now, so the registry refactor when we add a second backend is mechanical.

## CLI

Binary name: `wsi-label`.

```
wsi-label replace <wsi-in> <label-image> [<wsi-out>] [flags]
wsi-label strip   <wsi-in>               [<wsi-out>] [flags]
wsi-label inspect <wsi-in>                           [flags]
```

All three subcommands are explicit; there is no default subcommand.

### Output path resolution (replace, strip)

- If `<wsi-out>` is omitted, derive as `<input-stem>_relabeled<ext>` in the same directory as the input.
- If the derived path exists, try `<stem>_relabeled_1<ext>`, `<stem>_relabeled_2<ext>`, ... first free wins.
- If `<wsi-out>` is explicitly provided and exists, error out (exit 4) unless `--overwrite`.
- `--overwrite` applies to both explicit and derived paths. When set, no numbering — just clobber.
- `<wsi-in>` and the resolved output path must differ. Tool refuses to proceed otherwise.

### Label input (replace)

- Accepts PNG, JPEG, TIFF, or any format Go's `image` stdlib + `golang.org/x/image` can decode.
- Resize policy (`--resize=fit|stretch|none`, default `fit`):
  - `fit`: aspect-preserving fit to the backend's target dimensions, with fill color for letterbox area.
  - `stretch`: non-uniform scale to exact target dimensions.
  - `none`: reject if dimensions don't match exactly.
- `--bg=RRGGBB` — fill color for `fit` letterboxing. Default `F5F5E6` (parchment; matches `fake_label.py`).
- `--rotate=0|90|180|270` — rotate input image before encoding. Default 0.
- `--force` — allow aspect-ratio mismatches >2×. Default: reject such inputs as probable user error.

### Missing-label behavior (replace)

If the input file has no label IFD:

- Default: **add a label**. Insertion position:
  - If a macro IFD exists, insert immediately before it (matches Aperio's documented "label then macro at end of chain" ordering).
  - Otherwise, insert at the end of the chain (after the last IFD, whose `next-IFD` pointer is currently 0).
  Warn on stderr. Exit 10 (success, but label was added rather than replaced).
- `--strict-replace`: hard-fail (exit 2) if no label IFD exists. Intended for pipelines where "no existing label" is definitionally a pipeline bug.
- `--quiet`: silences the stderr warning but preserves the exit code.

When adding a label, the cutoff is computed from the **insertion position** rather than the (nonexistent) label IFD: cutoff = min(start offset of every byte range owned by the IFD at the insertion position or later in the chain). If inserting at the very end (no macro, no label), cutoff = end-of-file and the tail re-emission step is a no-op — just write the new label IFD at EOF and patch the previous tail IFD's `next-IFD` pointer.

### Exit codes

| Code | Meaning |
|---|---|
| 0 | Success: label replaced (or stripped). |
| 2 | Usage error / bad flags / `--strict-replace` triggered. |
| 3 | Input file unreadable or unrecognized format. |
| 4 | Output path conflict, no `--overwrite`. |
| 5 | IFD structure rejected (cutoff invariant violated, overlaps, etc.). |
| 10 | Success, but label was added rather than replaced. |

### Global flags

- `-v` / `--verbose` — log IFD map before and after, cutoff computation, byte counts.
- `-q` / `--quiet` — only errors to stderr.
- `--fsync=true|false` — default true. Off for throwaway runs where durability isn't needed.
- `--overwrite` — allow clobbering existing output.

### `inspect` subcommand

Read-only diagnostic. Prints:

- Format (TIFF / BigTIFF), byte order, file size.
- Every IFD: index, offset, whether tiled/stripped, dimensions, compression, `ImageDescription` first line, identified role (baseline / thumbnail / pyramid level / label / macro / unknown).
- The cutoff offset that `replace`/`strip` would use.
- Any invariant violations (overlaps, pyramid data past cutoff, etc.) — as warnings, not errors. `inspect` never fails on structural issues; it reports them.

## Parsing and byte-range accounting (`tifflow`)

On open, `tifflow` produces a complete map of every byte owned by the TIFF structure:

1. Parse header. Detect classic TIFF (`II*\0` / `MM\0*`) vs BigTIFF (`II+\0` / `MM\0+`). Record variant — output uses the same variant as input.
2. Walk IFD chain from the header's first-IFD offset. For each IFD, record:
   - The IFD record's byte range `[offset, offset+length)`.
   - Every tag: `(tagID, type, count, inline-value-or-offset)`. For tags with out-of-line values (anything larger than 4 bytes in classic TIFF / 8 bytes in BigTIFF), the out-of-line byte range.
   - `StripOffsets`+`StripByteCounts` or `TileOffsets`+`TileByteCounts`: resolve to a list of strip/tile data byte ranges.
   - The location of this IFD's `next-IFD` pointer (needed later for patching) and its current value.
3. If any IFD has `SubIFDs` (tag 330), fail loudly — Aperio SVS doesn't use them; a future Hamamatsu backend will handle them.
4. Sort all recorded ranges by start offset. Assert no overlaps — overlap ⇒ parser is wrong, exit 5.
5. Gaps between ranges are tolerated (some vendors leave padding) but recorded as "unaccounted bytes" for `--verbose` reporting.

### Cutoff computation

```
label_ifd = IFD satisfying:
    - stripped (not tiled)
    - not IFD 0 (baseline)
    - not IFD 1 (thumbnail — hardcoded by OpenSlide convention)
    - ImageDescription second line starts with "label"

cutoff = min(start offset of every range owned by label_ifd
             OR any IFD after label_ifd in chain)

for each range owned by an IFD BEFORE label_ifd:
    if range.start >= cutoff:
        fail with ErrUnexpectedLayout (exit 5)
```

### Tail re-emission (`tifflow.Splice`)

For each IFD after the label in the chain (macro, typically; usually zero IFDs in practice):

1. Choose new offsets for: the IFD record, each out-of-line blob (ImageDescription, JPEGTables, `StripOffsets` array, `StripByteCounts` array, etc.), and each strip/tile data block.
2. Emit out-of-line blobs first at their new offsets.
3. Bulk-copy strip/tile data from input offsets to new output offsets (no decode, no re-encode).
4. Emit the IFD record with all tags pointing at the new offsets. `next-IFD` pointer set to the next tail IFD's new position, or 0 for the last.

The last step of the whole operation: patch the predecessor's `next-IFD` pointer in the already-copied prefix to point at the new label IFD's offset. Single small write at a known location.

## Label encoding (`internal/aperio`)

Target spec (matches the original label IFD that OpenSlide and Aperio tools expect):

| Tag | Value |
|---|---|
| `NewSubfileType` (254) | 1 (reduced-resolution) |
| `ImageWidth` (256) | 1200 |
| `ImageLength` (257) | 848 |
| `BitsPerSample` (258) | [8, 8, 8] |
| `Compression` (259) | 5 (LZW) |
| `Photometric` (262) | 2 (RGB) |
| `ImageDescription` (270) | `"Aperio Image Library v12.4.7\nlabel 1200x848"` |
| `SamplesPerPixel` (277) | 3 |
| `RowsPerStrip` (278) | 2 |
| `StripOffsets` (273) | filled at emit time |
| `StripByteCounts` (279) | filled at emit time |
| `PlanarConfig` (284) | 1 (chunky) |
| `Predictor` (317) | 2 (horizontal differencing) |

Pipeline:

1. Decode caller-supplied image to `image.RGBA`.
2. Resize per `--resize` policy to 1200×848.
3. Apply predictor 2: for each row, for each channel, `row[i][c] = raw[i][c] - raw[i-1][c]` (mod 256, first pixel unchanged).
4. For each strip of 2 rows, LZW-compress with Go stdlib `compress/lzw` (MSB order, 8-bit literal width).
5. Emit tags + strip data via `tifflow.Splice`'s replacement-IFD interface.

**LZW compatibility note:** Go's `compress/lzw` in MSB mode implements the TIFF 6.0 "early-change" variant, which is what libtiff and Pillow produce. Will verify empirically by round-tripping through `openslide-python` and `tifffile` during testing. If stdlib misbehaves, fallback is `github.com/hhrutter/lzw` (pure Go, TIFF-tuned) — still no CGo.

## Atomicity and error handling

- **Temp file + rename.** Output path `foo_relabeled.svs` → write to `foo_relabeled.svs.tmp-<pid>-<nanos>` in the same directory. On success: `fsync` → `rename` (atomic on same filesystem). On any error: `os.Remove` the temp, return.
- **`fsync` before rename.** `*os.File.Sync()` is `fsync(2)` on Unix, `FlushFileBuffers` on Windows. `--fsync=false` skips.
- **Signal handling.** SIGINT/SIGTERM → clean up temp, exit non-zero. SIGKILL leaves a `.tmp-*` orphan, identifiable by prefix.
- **Typed errors.** `tifflow.ErrUnexpectedLayout`, `tifflow.ErrOverlap`, `wsi.ErrNoLabel`, `wsi.ErrFormatUnrecognized`, `aperio.ErrInvalidLabel`. CLI maps error types → exit codes. Tests check exit codes, not error strings.
- **Logging.** Stderr for human-readable logs. Stdout reserved for machine output (future `--json` on `inspect`).

## Testing

### Sample corpus

`sample_svs/` contains OpenSlide test data (`CMU-*.svs`, `JP2K-*.svs`) — older Aperio ScanScope files from <https://openslide.org/demo/>, CC-BY 3.0, ranging from ~2 MB to ~390 MB. Includes JPEG-compressed tiles (`CMU-1.svs`, `CMU-2.svs`, `CMU-3.svs`) and JPEG2000-compressed tiles (`CMU-1-JP2K-33005.svs`, `JP2K-33003-*.svs`). The JPEG2000 samples specifically exercise the invariant that `wsi-label` never decodes pyramid pixels — tiles are copied as opaque byte ranges regardless of codec. Only `CMU-1-Small-Region.svs` is checked in; the rest are fetched on demand (see `sample_svs/README.md`).

### Acceptance tests

1. **Golden structural tests** on every sample file in `sample_svs/`: parse, print IFD map + cutoff, commit expected output. Regressions in the parser show up as golden diffs.
2. **Round-trip through real readers.** For each sample: `wsi-label replace <sample> <testlabel.png> <out>`, then:
   - `openslide-python`: assert `associated_images["label"]` matches input label pixels; `macro` / `thumbnail` unchanged; level dimensions + sampled base-level reads identical to input.
   - `tifffile`: independent cross-check of label bytes and IFD tags.
3. **Strip-label test.** `wsi-label strip`: no label IFD in output; macro/thumbnail/pyramid intact; output bytes do not contain the original label's ImageDescription signature.
4. **Byte-identity check.** `cmp` input and output over `[0, cutoff)` — must be equal for every sample.
5. **Label encoder unit tests.** Encode known image, decode via Go TIFF reader + Python `tifffile`, assert pixel equality. Edge cases: solid color, noise, non-target dimensions.
6. **CLI integration tests.** All exit codes (0, 2, 3, 4, 5, 10) exercised. Output path resolution: explicit, derived, conflict-with-suffix, `--overwrite`. Missing-label: default (exit 10), `--strict-replace` (exit 2), `--quiet`.
7. **No-label-IFD source.** Either a synthetic TIFF built in tests or a manually-prepared sample. Exercises both the add-label path and `--strict-replace`.

Not doing today: fuzzing the TIFF parser, large-file stress tests beyond the sample corpus.

## Known risks

1. **Go stdlib LZW early-change variant.** If `compress/lzw` produces output that some Aperio reader rejects, swap to `github.com/hhrutter/lzw`. Validated during testing, not at spec time.
2. **Vendor quirks in out-of-line data placement.** If a real-world SVS violates the chain-order layout assumption, the cutoff invariant catches it and `wsi-label` refuses to run (exit 5) rather than silently corrupt. User sees an actionable error and can file a sample.
3. **SubIFDs in future formats.** Aperio doesn't use them; Hamamatsu and others might. Failing loudly on detection today keeps the door clean for the future backend.
4. **Color profile preservation.** Aperio label IFDs don't carry ICC profiles today; we match that. If future scanner output requires a specific color profile on the label, that's a later refinement.

## Out of scope for today, architected-for later

- Other WSI backends (Hamamatsu NDPI, Leica SCN, Ventana BIF, generic tiled TIFF).
- Label rendering (text → image). Caller's job.
- GUI / web UI.
- Batch mode (the Python orchestrator does this; `wsi-label` stays single-file-per-invocation).
- `--json` output on `inspect`.
- DICOM WSI / Zeiss CZI / MRXS — require non-TIFF engines; separate effort.

## Repository hygiene

- Sample SVS files (`sample_svs/*.svs`) must never be committed to git. `.gitignore` will exclude the directory.
- No git repository initialization until the user explicitly requests it.
