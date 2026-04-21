# sample_svs

Test corpus for `wsi-label`.

## Committed (1 file, ~2 MB)

- `CMU-1-Small-Region.svs` — public Aperio test slide from the [OpenSlide
  demo data](https://openslide.org/demo/), Carnegie Mellon University,
  [CC Attribution 3.0](https://creativecommons.org/licenses/by/3.0/) licensed.

  Kept in the repo so unit + integration tests run out-of-the-box after
  clone. Every test that depends on it uses `t.Skipf(...)` when it's missing,
  so the suite still passes on a sparse checkout — but you'll skip the only
  real-world structural coverage.

## Fetching the rest of the OpenSlide demo corpus

Anything else you drop in this directory is `.gitignore`'d. For broader
coverage (larger files, JPEG2000-compressed tiles, BigTIFF), fetch from
<https://openslide.org/demo/> — all CC-A 3.0:

    BASE=https://openslide.cs.cmu.edu/download/openslide-testdata/Aperio
    curl -L -o sample_svs/CMU-1.svs              $BASE/CMU-1.svs
    curl -L -o sample_svs/CMU-2.svs              $BASE/CMU-2.svs
    curl -L -o sample_svs/CMU-3.svs              $BASE/CMU-3.svs
    curl -L -o sample_svs/CMU-1-JP2K-33005.svs   $BASE/CMU-1-JP2K-33005.svs
    curl -L -o sample_svs/JP2K-33003-1.svs       $BASE/JP2K-33003-1.svs
    curl -L -o sample_svs/JP2K-33003-2.svs       $BASE/JP2K-33003-2.svs

The JPEG2000 samples specifically exercise the "never decode tile pixels"
invariant — tiles are copied as opaque byte ranges regardless of codec.
