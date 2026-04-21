# wsi-label

CLI for replacing or stripping the label image inside whole-slide image files. Aperio SVS supported today; architected for additional WSI formats.

## Build

    go build -o wsi-label ./cmd/wsi-label

## Usage

    wsi-label inspect input.svs
    wsi-label replace input.svs mylabel.png           # → input_relabeled.svs
    wsi-label replace input.svs mylabel.png out.svs   # → out.svs
    wsi-label replace --overwrite input.svs mylabel.png out.svs
    wsi-label strip input.svs                         # → input_relabeled.svs
    wsi-label strip input.svs stripped.svs

## Exit codes

| Code | Meaning |
|------|---------|
| 0    | Label replaced or stripped. |
| 2    | Usage error / bad flags / --strict-replace triggered. |
| 3    | Input unreadable or unrecognized. |
| 4    | Output exists (use --overwrite). |
| 5    | File has unexpected TIFF layout; refusing to proceed. |
| 10   | Success, but label was added rather than replaced. |

## Design & Roadmap

- Design: `docs/superpowers/specs/2026-04-20-wsi-label-design.md`
- Roadmap: `docs/ROADMAP.md`
