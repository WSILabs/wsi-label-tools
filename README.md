# wsi-label-tools

**Redact the label image in whole-slide imaging (WSI) files — fast, reliable, pure Go.**

![Go 1.22+](https://img.shields.io/badge/go-1.22%2B-00ADD8?logo=go&logoColor=white)
![License MIT](https://img.shields.io/badge/license-MIT-22c55e)
![Platforms](https://img.shields.io/badge/platforms-linux%20%7C%20macOS%20%7C%20windows-6366f1)
![Release](https://img.shields.io/badge/release-v0.1.1-0ea5e9)
![Pure Go](https://img.shields.io/badge/pure_Go-no_CGo-00ADD8)
![Status](https://img.shields.io/badge/status-pre--release-f59e0b)

`wsi-label` is a command-line tool that replaces or strips the **label image** inside digital pathology slide files — the small on-screen sticker photo that typically carries patient identifiers (PHI). The tool never decodes the pyramid image data, so it's fast even on multi-gigabyte slides, and it produces output that's bit-identical to the input everywhere except the label. A companion Python driver (`label-slides.py`) renders anonymized labels from CSV-driven or command-line metadata and batches the replacement.

Use cases: **de-identification for data sharing**, **anonymization before publication**, **scrubbing PHI from research datasets**, and **bulk label updates** in pathology pipelines.

## Why a separate tool?

The label-handling capability here also lives inside [wsitools](https://github.com/wsilabs/wsitools), a larger WSI toolkit. This repo exists on purpose, not by accident. `wsitools` is pure-Go *capable*, but its default build enables CGo-backed codec encoders (AVIF, JPEG-XL, WebP, HTJ2K, libjpeg-turbo) that link native libraries — you only get a dependency-free wsitools by opting out via build tags. `wsi-label`, by contrast, is **unconditionally pure Go**: two pure-Go dependencies, zero CGo, no build flags to remember. That single property is the whole point — it cross-compiles to every platform from one machine, ships as a self-contained static binary with no `.so`/`.dll` to install, and presents a tiny audit and maintenance surface. So the rule of thumb is: **reach for `wsi-label` when you just need to redact or swap labels and want a drop-in binary; reach for `wsitools` when you need full whole-slide decoding and conversion.** The overlap is intentional duplication, and it stays here as long as staying dependency-free is worth it.

## Features

- **Replace or strip** the label associated-image in an SVS file, without touching pyramid pixel data.
- **Preserves original label geometry** — matches the existing label's dimensions and orientation (works on 1200×848 landscape Aperio output *and* older portrait ScanScope files).
- **BigTIFF-aware** — handles both classic TIFF (magic 42) and BigTIFF (magic 43) transparently.
- **Codec-independent** — JPEG, JPEG2000, and any other tile codec pass through as opaque byte ranges.
- **Pure Go** — no CGo, no libtiff/libopenslide runtime dependency. One static binary per platform.
- **Atomic writes** — temp-file + fsync + rename; killed runs never leave a half-written output.
- **Extensible** — format detection/classification lives behind a `redact.Classifier` registry (one `init()` registration per format); adding OME-TIFF, Philips, NDPI, etc. is a drop-in, not a rewrite.

## Install

Download the archive for your platform from the [Releases page](https://github.com/cornish/wsi-label-tools/releases/latest), extract, and move the `wsi-label` binary onto your `PATH`.

    # Linux amd64 example
    curl -L https://github.com/cornish/wsi-label-tools/releases/latest/download/wsi-label_0.1.1_linux_amd64.tar.gz | tar xz
    sudo mv wsi-label /usr/local/bin/
    wsi-label --version

Or build from source (requires Go 1.22+):

    go build -o wsi-label ./cmd/wsi-label

## Usage

```
wsi-label inspect    <wsi-in>                                   # list IFDs + roles
wsi-label replace    <wsi-in> <label-image> [<wsi-out>] [flags] # swap label
wsi-label strip      <wsi-in>               [<wsi-out>] [flags] # remove label
wsi-label label-dims <wsi-in>                                   # print "WxH"
```

Replace / strip flags:

- `--overwrite` — clobber existing output (otherwise a numbered suffix is picked).
- `--strict-replace` — hard-fail if the input has no label (otherwise a label is added, and the tool exits 10).
- `--label-dims WxH` — force a specific target size (default: match existing label, else 1200×848).
- `--resize fit|stretch|none` — how to fit an input that isn't already at target dims. Default `fit`.
- `--rotate 0|90|180|270` — rotate input label before storing.
- `--bg RRGGBB` — letterbox fill for `--resize=fit`. Default `F5F5E6` (Aperio parchment).
- `--force` — bypass the >2× aspect-ratio safety check.
- `--fsync=false` — skip the fsync before rename (speed, not durability).
- `-q` — silence non-error stderr output.

### Batch driver

`label-slides.py` is a Python wrapper over `wsi-label replace` that handles label rendering and batch execution:

    # Interactive — pick files + enter label text
    ./label-slides.py --svs-dir /path/to/slides

    # One label applied to every file (quick PHI scrub)
    ./label-slides.py --text "ANON-001" *.svs

    # Per-file labels from a spreadsheet
    ./label-slides.py --csv labels.csv --svs-dir /path/to/slides -j 4

    # Strip labels entirely (no rendering)
    ./label-slides.py --strip *.svs

Dependencies: `pillow`, and optionally `questionary` for the arrow-key TUI.

## Exit codes

| Code | Meaning |
|------|---------|
| 0    | Label replaced or stripped. |
| 2    | Usage error / bad flags / `--strict-replace` triggered. |
| 3    | Input unreadable or unrecognized. |
| 4    | Output exists (use `--overwrite`). |
| 5    | File has unexpected TIFF layout; refusing to proceed. |
| 10   | Success, but label was added rather than replaced. |

## Reliability

The tool is built around a handful of checked invariants rather than heuristics:

1. **Prefix byte-identity.** Bytes `[0, cutoff)` of the output equal the input byte-for-byte — the pyramid, pyramid tile offsets, thumbnail, and anything else before the label are untouched. Directly testable with `cmp`.
2. **No pyramid pixel decode.** Tiles are copied as opaque byte ranges. Whether they're JPEG, JPEG2000, or anything else is irrelevant.
3. **No dead-byte PHI.** The old label's bytes never make it into the output file. No post-hoc scrubbing needed.
4. **Cutoff violation refuses, never corrupts.** If a file's byte layout doesn't match the IFD chain order (pathological but legal TIFF), the tool fails loudly with exit 5 rather than silently corrupting a tile array.
5. **TIFF 6.0 compliant LZW.** Uses `github.com/hhrutter/lzw` with `oneOff=true`; output decodes cleanly through `libtiff`, not just lenient readers.

Validated against an openslide-python round-trip plus a strict `tiffinfo -D` decode on every commit; see [`scripts/cross_validate.py`](scripts/cross_validate.py).

## Supported formats

| Format | Status | Notes |
|--------|--------|-------|
| Aperio SVS | ✅ V1 | Classic TIFF + BigTIFF |
| generic-TIFF | ✅ | tag 65080 / heuristic; pure splice |
| Philips-TIFF | ✅ | `Software` prefix detect; pure splice (label CI fixture pending) |
| Hamamatsu NDPI | ✅ | Region strategy (blank/composite the macro's label crop); pure-Go JPEG |
| Ventana BIF | 🛣️ Band 3 | overview-role splice |
| Leica SCN | 🛣️ Parked | overview-role + SCN-XML |
| OME-TIFF | ❌ out of charter | SubIFD pyramid — needs file-rewrite (use wsitools) |
| Zeiss CZI / MRXS | ❌ | non-TIFF / directory-based |

See [`docs/ROADMAP.md`](docs/ROADMAP.md).

## Design

- Design doc: [`docs/superpowers/specs/2026-04-20-wsi-label-design.md`](docs/superpowers/specs/2026-04-20-wsi-label-design.md)
- Implementation plan: [`docs/superpowers/plans/2026-04-20-wsi-label.md`](docs/superpowers/plans/2026-04-20-wsi-label.md)

## Keywords

*whole-slide imaging · WSI · digital pathology · Aperio · SVS · TIFF · BigTIFF · label replacement · label redaction · de-identification · deidentification · PHI removal · anonymization · slide anonymizer · openslide · ImageScope · Grundium · ScanScope · digital pathology pipeline · pathology informatics · pathology data sharing · HIPAA-adjacent · pure Go*
