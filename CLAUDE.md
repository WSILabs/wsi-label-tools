# CLAUDE.md

Guidance for working in `wsi-label-tools`. Keep changes consistent with what's below.

## What this is

A CLI (`wsi-label`) that **redacts or replaces the label image** in whole-slide-imaging files. Pure Go, **two runtime deps only** (`hhrutter/lzw`, `golang.org/x/image`), no CGo, static binaries. It never decodes the multi-gigabyte pyramid.

**Scope is the label IMAGE (pixels) — NOT de-identification.** Do not add metadata/XMP/barcode scrubbing (e.g. BIF `Barcode1D/2D`, `UserName`), DICOM tag stripping, etc. That's a separate concern, explicitly out of charter (standing directive). The README/spec keep de-id in their *use-case* framing only.

## Architecture

- `internal/redact` — the core. `Classifier` interface + a two-tier registry: `Register` (vendor, tried first) and `RegisterFallback` (greedy, e.g. generic-TIFF, tried last). The orchestrator (`redact.go`: `Replace`/`Strip`/`LabelDims`) parses once, detects the format, classifies, and dispatches on the target's **`Representation`**.
- A `Target` has a `Role` (Label / Overview / Thumbnail — only **Label** is acted on today) and a `Representation`:
  - **`SeparableIFD`** → **Splice strategy**: the label is its own IFD; remove/replace it as opaque bytes via `internal/tifflow`. Byte-exact outside the label, codec-independent.
  - **`EmbeddedRegion`** → **Region strategy** (`strategy_region.go`): the label is a fixed crop of a small associated image (NDPI macro left ~30%, BIF overview top 1/3); decode it (stdlib `image/jpeg` or raw RGB), blank/composite the region, re-encode, splice the new bytes back. Pyramid still untouched.
- `internal/redact/format/*.go` — one classifier per format (`svs`, `generictiff`, `philips`, `ndpi`, `bif`), each self-registering in `init()`. Signals are ported from opentile-go; the binary blank-imports the package in `cmd/wsi-label/main.go`.
- `internal/label` — format-neutral label builder (`BuildStripLabelIFD(img, desc, extra...)`, resize/rotate/letterbox, LZW+predictor2 encode).
- `internal/tifflow` — our pure-Go TIFF/BigTIFF parser + splicer. Splice modes: `SpliceReplace/InsertBefore/Append/Remove` and `SpliceReplaceStripData` (re-emit an IFD preserving tags, substituting strip/tile data; `DropTags` drops a tag e.g. `JPEGTables`). **Rejects SubIFD trees** (`ErrSubIFDs`) — that's why OME-TIFF is out of charter.
- `internal/cli` — thin layer over `redact`. `internal/atomic` — temp+fsync+rename.

### Adding a format
Write `format/<name>.go`: `Detect(*tifflow.File) bool` + `Classify(*tifflow.File) ([]redact.Target, error)` + the label-build/default methods; `Register` (or `RegisterFallback` for greedy matchers). Splice formats emit a `SeparableIFD` target (the label IFD index); region formats emit an `EmbeddedRegion` target (host IFD + region rect). Add an oracle case and an e2e test.

## Testing & the oracle

- `go test ./...` for the main module. New code gets an e2e (strip+replace, assert pyramid byte-identity + the target actually changed — non-vacuous).
- `test/oracle/` is a **separate nested module** (its own `go.mod`, `replace ../..`) that cross-validates our classification against **opentile-go** — the wsilabs standard of truth for format *reading*. Run it `CGO_ENABLED=0` (opentile's CGo decoders fall back to pure-Go stubs): `cd test/oracle && CGO_ENABLED=0 go test ./...`. **opentile-go must never enter the main module's `go.mod`** (keep the two-deps binary). When opentile lacks something (e.g. a BIF label crop), file an upstream issue rather than working around it.

## Fixtures

CI fixtures come from the shared `WSILabs/wsi-fixtures` repo (download-from-release). `scripts/fetch-fixtures.sh` and CI pull a pinned `vN` into `sample_files/<format>/<name>`; tests **skip when absent** so `go test ./...` builds without a download. When adding a fixture: add it to wsi-fixtures (sidecars + manifest + `verify.sh`/`pack.sh`), publish a **new immutable `vN+1` release**, then bump `ci.yml` + `fetch-fixtures.sh`. Huge files stay out — e.g. **BIF `OS-1.bif` (3.6 GB) is local-only** (tests read it from the sibling `../opentile-go/sample_files/`, skip in CI).

## Gotchas

- Never decode the pyramid; decode only the small label-bearing associated image (region strategy), and only if it's a stdlib-decodable codec (baseline JPEG / uncompressed). Non-decodable → error.
- BIF overviews: legacy OS-1 is **abbreviated JPEG** (tables in the `JPEGTables` tag — merge them to decode; re-encode self-contained + `DropTags` it). DP200 Ventana-1 is uncompressed multi-strip.
- `tifflow.reemitIFD` reads large offset/count arrays out-of-line from the file (the parse cache caps at 4 KB) — needed for tail IFDs with big tile tables.
- Out of charter: OME-TIFF (SubIFD), COG-WSI (COG-compliance), de-id metadata.

## Design docs
Specs and per-band plans live in `docs/superpowers/specs/` and `docs/superpowers/plans/`. Bands done: 0 (core), 1 (generic-TIFF + Philips), 2 (NDPI region), 3 (BIF region). Parked: Leica SCN.
