# Multi-format label redaction — Design

**Date:** 2026-06-06
**Status:** Draft for review
**Author:** brainstormed with the user, 2026-06-06
**Supersedes/extends:** [`2026-04-20-wsi-label-design.md`](2026-04-20-wsi-label-design.md) (Aperio-only V1)

## Purpose

Extend `wsi-label` from Aperio SVS only to the broader set of WSI formats,
while keeping the tool **unconditionally pure Go** (no CGo, no native
libraries) and never decoding the pyramid. The label associated-image —
the camera photo of the slide's physical label, which typically carries
patient identifiers — is the redaction target.

The driving constraints, established during brainstorming:

- This repo's reason to exist is being a **CGo-free, dependency-light,
  static-binary** label tool. The larger `wsilabs/wsitools` toolkit covers
  full WSI decoding/conversion (with CGo codecs); this tool covers the
  subset of work that can be done as a byte-level edit with, at most,
  pure-Go decode of a single small associated image.
- `wsilabs/opentile-go` is the wsilabs **standard of truth for format
  reading**. We honor that without taking a runtime dependency on it: our
  per-format classification is validated against opentile-go as a
  **test-time oracle** (see "opentile-go as oracle").

## Scope

**Redaction target:** the **label** only, for now. The design is built
around a role-keyed target abstraction so that redacting other associated
images (macro/overview, thumbnail) later is an additive change, not a
rearchitecture.

**Formats in scope now** (each has a redactable label):

| Format | Label representation | Strategy |
|--------|----------------------|----------|
| Aperio SVS | real label IFD | Splice (rebase existing code) |
| generic-TIFF | label IFD (heuristic / tag 65080) | Splice |
| OME-TIFF | label IFD (OME-XML `Name="label"`) | Splice |
| Philips-TIFF | label IFD (ImageDescription `"Label"`) | Splice |
| Hamamatsu NDPI | synthesized: left-crop of the macro | Region |

**Formats deferred** (no distinct label — the patient-identifying image is
published only as "overview"; these need the future other-associated-images
door):

- Ventana BIF — IFD 0 is structurally `Label_Image` but opentile-go
  surfaces it as `"overview"`.
- Leica SCN — only full-slide auxiliaries, all surfaced as `"overview"`.

When the role door opens, BIF and SCN become "classify + act on the
overview role," using the same two strategies.

## Non-goals

- Reading or rendering pyramid pixel data. (Unchanged from V1.)
- Decoding any pyramid-level tile. The Region strategy decodes only a
  single small associated image (the NDPI macro), via Go's stdlib JPEG.
- Acting on macro/overview or thumbnail today (door left open).
- A runtime dependency on opentile-go. It is a test-only oracle.
- Generating label text / fonts. The caller supplies a pre-rendered image
  (the Python driver renders it). Unchanged from V1.
- Lossless transform-domain JPEG crop (what opentile-go uses libjpeg-turbo
  for). We re-encode the macro in the Region strategy; redaction is
  inherently lossy on that image and that is acceptable.
- GUI. CLI only.

## Two redaction strategies

The central architectural fact: once the label is located, *writing* the
redaction takes one of two forms, selected by how the label is physically
stored — not by which format it is.

### Strategy 1 — Splice (byte-exact, no decode)

For formats where the label is its own separable IFD (SVS, generic-TIFF,
OME-TIFF, Philips). This is the V1 approach generalized: copy the prefix
byte-identical, re-emit the tail with the label IFD removed (strip) or
replaced (replace), patch one next-IFD pointer. The label's bytes are
opaque — codec-independent, decode-free, bit-exact everywhere except the
label.

**Properties:** no decode, codec-independent, bit-exact outside the label,
no dead-byte PHI. The strongest guarantees; preferred whenever applicable.

### Strategy 2 — Region (pure-Go decode → blank/composite → re-encode → splice)

For formats where the label has no separable byte range because it is a
*region of a larger associated image* (NDPI: the label is the left ~30% of
the macro JPEG). Flow:

1. Locate the macro IFD and its single JPEG strip (offset + length).
2. Read the macro JPEG bytes (a single small strip).
3. `image/jpeg.Decode` → paint the label region (fill with background, or
   composite a supplied anonymized label) → `image/jpeg.Encode`.
4. Splice the new JPEG blob in via the existing `tifflow` machinery,
   fixing `StripByteCounts` and downstream offsets.

This is the first time the tool decodes an existing image from the input.
It is still pure Go (stdlib JPEG), still never touches the pyramid.

**Properties:** pure Go *only if the target image uses a stdlib-decodable
codec* (baseline JPEG — true for NDPI's macro). Lossy on that image
(re-encoded). The one genuinely out-of-scope case for the whole tool: a
redaction target stored in a codec Go's stdlib cannot decode (JP2/HTJ2K).
That case belongs to wsitools.

For NDPI, redacting the label means **blanking the label region** of the
macro and re-encoding. Removing the macro/overview outright is a separate
action on the overview role — deferred behind the role door, not part of
this scope (the macro *is* a separable IFD, so it will be a Splice when
that door opens).

## Architecture

```
cmd/wsi-label/                CLI entry point, format-agnostic, thin.
internal/redact/              Orchestrator + the new core.
  redact.go                   Open → detect format → classify (all roles)
                              → select role Label → pick strategy by the
                              target's representation → execute.
  target.go                   type Role (Label | Overview | Thumbnail)
                              type Target {
                                Role
                                Representation                 // SeparableIFD | EmbeddedRegion
                                IFDOffset, ByteLen  uint64     // SeparableIFD
                                HostIFDOffset       uint64     // EmbeddedRegion (the macro)
                                Region              image.Rect // EmbeddedRegion
                                Dims                image.Point
                                Codec               // for replace/encode
                              }
  format/                     Per-format detect + classify (OUR pure-Go code).
    format.go                 type Classifier interface {
                                Name() string
                                Detect(*tifflow.File) bool
                                Classify(*tifflow.File) ([]Target, error)  // all roles
                              }
    registry.go               Ordered list of classifiers; first Detect wins.
    svs.go                    Aperio: ImageDescription prefix + NewSubfileType bits.
    generictiff.go            Heuristic ladder + private tag 65080.
    ometiff.go                OME-XML <Image Name=...> parse.
    philips.go                Software=Philips DP + desc substring.
    ndpi.go                   Tags 65420/65421; label = macro left-crop.
  strategy/
    splice.go                 Strategy 1 (delegates to tifflow).
    region.go                 Strategy 2 (image/jpeg + tifflow).
  label/                      Format-neutral label build (lifted from aperio).
    build.go, resize.go       resize / rotate / letterbox / encode.
internal/tifflow/             Byte-exact IFD splice + offset fixups. UNCHANGED.
internal/atomic/              Temp-file + fsync + rename. UNCHANGED.
test/oracle/                  SEPARATE nested module (own go.mod).
  go.mod                      requires opentile-go + the main module.
  classify_oracle_test.go     Cross-validate our Classify() vs opentile-go
                              on the fixture corpus. Built CGO_ENABLED=0.
```

**`internal/aperio` dissolves.** SVS becomes `format/svs.go` (detect +
classify) plus the Splice strategy. Its valuable parts — existing-label
dims preservation, parchment-background letterboxing — move into `label/`
and a small per-format defaults table.

**`internal/wsi.Backend` dissolves.** The per-format interface is replaced
by the thinner `format.Classifier` (detect + classify only — no write).
Writing is unified in `strategy/`. Format variation lives only in
classification; strategy is chosen by the target's representation.

**Everything stays `internal/`.** The clean core + thin CLI is the door
left open for wsitools to import us later; we promote a package to a public
path only when a real importer exists. No abstraction is added speculatively
for wsitools.

## Per-format classification signals

Ported from opentile-go v0.36.0's mature readers and validated against it.
Adopt opentile-go's **v0.15 `macro` → `overview` naming** from the start.
All signals are readable from TIFF structure with no pixel decode.

**SVS (Aperio)** — Detect: page 0 `ImageDescription` (270) begins
`"Aperio"`. Classify: structural — up to 2 trailing non-tiled/reduced
pages (`NewSubfileType` bit 0 set); `NewSubfileType == 9` → macro/overview,
otherwise → label. (Current code's "ImageDescription line 2 == label" is a
compatible signal and is retained as the SVS rule.)

**generic-TIFF** — Detect: valid TIFF passing pyramid validation. Classify,
first match wins: private tag **65080** (`label`/`macro`/`overview`/
`thumbnail`) is authoritative when present; else the heuristic ladder —
(1) stripped + LZW(5) + W<1500 & H<1500 → label; (2) stripped + JPEG(7) +
aspect ≥ 2.0 + 1000 ≤ maxDim ≤ 5000 → overview; (3) stripped + JPEG +
W<1500 & H<1500 → thumbnail; (4) tiled + area < 0.1% baseline → thumbnail;
(5) tiled + area < 1% baseline → overview; else → associated. These
thresholds are empirical — the oracle test guards them.

**OME-TIFF** — Detect: page 0 desc (trimmed) ends `"OME>"`. Classify: parse
OME-XML; `<Image Name>` exact (trimmed) match — `"label"` → label,
`"macro"` → overview, `"thumbnail"` → thumbnail; other names → pyramid.

**Philips-TIFF** — Detect: `Software` (305) begins `"Philips DP"` **and**
desc (trimmed) ends `"</DataObject>"`. Classify: non-tiled pages by desc
substring — contains `"Label"` → label, `"Macro"` → overview,
`"Thumbnail"` → thumbnail.

**NDPI** — Detect: page 0 has tag **65420** (FileFormat) and tag 271
(Make). Classify by tag **65421** (Magnification FLOAT): `> 0` → pyramid
level; `== -1.0` → macro (overview, single JPEG strip); `== -2.0` → map.
**Label** has no IFD: it is the left `0.3 × macroWidth` region of the macro
(full height). We need no MCU precision (we re-encode), so we blank a
generous left region.

## opentile-go as oracle

opentile-go is imported **only** by the `test/oracle` nested module, never
by the shipping binary. This keeps the main `go.mod` at its two pure-Go
deps (`hhrutter/lzw`, `golang.org/x/image`) and preserves the
unconditionally-pure-Go, CGo-free property the README claims.

- The oracle module declares its own `require github.com/wsilabs/opentile-go`
  plus the main module, and is built/run **`CGO_ENABLED=0`** (opentile-go's
  CGo decoders are build-tag gated `cgo && !nocgo`; with CGo off, pure-Go
  stubs compile and only the format-reading/classification paths we need
  are exercised — never pixel decode).
- The test opens each fixture with opentile-go, enumerates
  `Associated()`/`Type()` (+ `AssociatedIFDOffset` where available), and
  asserts our `Classify()` agrees on which IFD/region is the label (and the
  other roles). Divergence fails CI.
- Run as a dedicated CI job. Default `go test ./...` on the main module
  stays dep-light and does not require opentile-go.

When opentile-go lacks information we need (e.g. `AssociatedIFDOffset`
returns `ok=false` for NDPI/OME/Philips/Leica/BIF today), the policy is to
**file an upstream issue** and add it to opentile-go rather than work around
it locally — reading knowledge lives in the cornerstone. Note this is only
relevant to the oracle's cross-check; our own classifiers locate IFDs via
`tifflow` regardless.

## Reliability invariants

Strategy 1 inherits all V1 invariants verbatim (prefix byte-identity, no
out-of-line data past cutoff, non-overlapping ranges, read-only input,
atomic output, distinct in/out paths). See the V1 spec.

Strategy 2 adds:

1. **Pyramid untouched.** Only the macro IFD's single strip is rewritten;
   `[0, cutoff)` before the macro stays bit-identical, the pyramid is never
   read or written.
2. **No dead-byte PHI.** The original macro bytes (which contain the label
   region) never appear in the output — the re-encoded macro replaces them
   via the same copy-prefix/re-emit-tail splice, leaving no recoverable
   remnant.
3. **Decode-target codec is gated.** If the macro is not a stdlib-decodable
   codec, the tool refuses (clear "out of pure-Go scope; use wsitools"
   error) rather than emitting a partial result.

## CLI

Subcommands unchanged in shape (`inspect`, `replace`, `strip`,
`label-dims`); they become format-agnostic over `internal/redact`:

- `inspect` lists IFDs with all classified roles (label/overview/thumbnail)
  — useful now even though only label is actioned.
- `replace` / `strip` act on the **label** role. For NDPI, `replace`
  composites the anonymized label into the macro's label region and
  re-encodes; `strip` blanks that region. (Removing the whole macro is an
  overview-role action, deferred.)
- Existing replace flags (`--overwrite`, `--strict-replace`, `--label-dims`,
  `--resize`, `--rotate`, `--bg`, `--force`, `--fsync`, `-q`) carry over.

Exit codes from V1 are preserved; a new code is added for "redaction target
stored in a non-pure-Go-decodable codec" (Strategy 2 gate).

## Sequencing

- **Band 0 — keystone refactor.** Stand up `redact` + `format` +
  `strategy/splice` + `label`; rebase SVS onto it; dissolve
  `internal/aperio` and `internal/wsi`; add the `test/oracle` module and CI
  cross-validation. Behavior unchanged; existing tests + `cross_validate.py`
  stay green. This is where the architecture and most risk live — if SVS
  can't cleanly rebase, the design is wrong.
- **Band 1 — splice formats.** Add `format/generictiff.go`, then
  `ometiff.go`, `philips.go`. Each is a classifier + oracle test; pure
  additions over Band 0's machinery.
- **Band 2 — region strategy.** Add `strategy/region.go` and
  `format/ndpi.go`.
- **Parked behind the role door:** Ventana BIF, Leica SCN (need
  overview-role actioning).

## Documentation impact

- README "Why a separate tool?" section (commit `c0448de`) **stays valid** —
  the binary remains pure-Go with two deps; opentile-go is test-only. Update
  the supported-formats table to reflect SVS/generic-TIFF/OME-TIFF/Philips
  (label, splice) + NDPI (label, region), and note BIF/SCN as roadmap.
- ROADMAP: mark the NDPI/format-coverage band against this spec.

## Open questions

- **generic-TIFF threshold portability.** The empirical thresholds
  (1500px, 2.0 aspect, 0.1%/1% area) were tuned on opentile-go's corpus.
  The oracle test will reveal whether our fixtures agree; if real labels
  exceed 1500px we raise `labelMaxDim`.
- **NDPI macro-offset sourcing.** We locate the macro IFD via `tifflow`;
  whether to additionally file an upstream `AssociatedIFDOffset` for NDPI
  is a cornerstone-hygiene question, not a blocker for us.
