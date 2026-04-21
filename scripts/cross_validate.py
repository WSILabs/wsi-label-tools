#!/usr/bin/env python3
"""Cross-validate wsi-label output against external readers.

For each sample SVS in sample_svs/, runs `wsi-label replace` with a synthetic
label and then performs three independent checks:

  1. libtiff strict decode (tiffinfo -D): opens every IFD AND decodes every
     strip/tile. Fails on warnings like "Using code not yet in table", which
     indicate TIFF-spec-non-conforming LZW output that strict downstream
     tools (e.g., openslide's deepzoom_tile.py via TIFFRGBAImageGet) reject.

  2. tifffile pyramid dims: asserts the output's tiled pyramid levels match
     the input's exactly (the splice must not touch pyramid pixels).

  3. openslide associated-image lookup: asserts the output has a "label"
     associated image that is non-empty and non-uniform (not black, not
     all-background-color).

  Usage:
      python scripts/cross_validate.py path/to/wsi-label

Exit 0 on all-pass, 1 on any failure, 2 on bad usage.
"""

from __future__ import annotations

import shutil
import subprocess
import sys
from pathlib import Path

import numpy as np
import openslide
import tifffile
from PIL import Image

HERE = Path(__file__).resolve().parent.parent
SAMPLES = HERE / "sample_svs"
TMP = HERE / ".tmp_crossvalidate"

# libtiff stderr patterns that indicate a real problem. We'd rather be noisy
# than miss a spec-violating stream.
LIBTIFF_BAD_PATTERNS = (
    "Using code not yet in table",
    "Corrupt LZW",
    "Truncated TIFF",
    "Error",
    "Invalid",
    "Bogus",
)


def pyramid_dims(path: Path) -> list[tuple[int, int]]:
    with tifffile.TiffFile(str(path)) as tf:
        return [
            (page.imagewidth, page.imagelength) for page in tf.pages if page.is_tiled
        ]


def make_label(tmpdir: Path) -> Path:
    # Non-uniform content so we can detect silent all-zero decode output.
    rng = np.random.default_rng(42)
    arr = rng.integers(0, 256, size=(400, 600, 3), dtype=np.uint8)
    p = tmpdir / "label.png"
    Image.fromarray(arr).save(p)
    return p


def tiffinfo_decodes_cleanly(tiffinfo: str, path: Path) -> tuple[bool, str]:
    """Run tiffinfo -D (decodes every strip/tile) and scan for warnings."""
    result = subprocess.run(
        [tiffinfo, "-D", str(path)],
        capture_output=True,
        text=True,
    )
    if result.returncode != 0:
        return False, f"tiffinfo exit {result.returncode}: {result.stderr.strip()}"
    combined = result.stdout + "\n" + result.stderr
    for pat in LIBTIFF_BAD_PATTERNS:
        if pat in combined:
            # Surface the offending line for diagnostics.
            offenders = [
                line for line in combined.splitlines() if pat in line
            ]
            return False, f"libtiff warning: {offenders[0]!r}"
    return True, "libtiff clean"


def label_is_non_trivial(arr: np.ndarray) -> bool:
    """Heuristic: a successfully decoded label has pixel variance.

    Our synthetic input label is random noise; after aspect-preserving
    resize + LZW round-trip it won't have zero variance unless decode
    produced a solid color (i.e., failed silently).
    """
    return bool(arr.size > 0 and arr.std() > 5.0)


def validate_one(
    exe: Path,
    sample: Path,
    label: Path,
    tmpdir: Path,
    tiffinfo: str,
) -> tuple[bool, str]:
    out = tmpdir / f"out_{sample.name}"
    result = subprocess.run(
        [str(exe), "replace", "--overwrite", str(sample), str(label), str(out)],
        capture_output=True,
    )
    if result.returncode not in (0, 10):
        return False, f"wsi-label exit {result.returncode}: {result.stderr.decode().strip()}"

    # 1. libtiff strict decode.
    ok, msg = tiffinfo_decodes_cleanly(tiffinfo, out)
    if not ok:
        return False, msg

    # 2. pyramid dims unchanged.
    in_dims = pyramid_dims(sample)
    out_dims = pyramid_dims(out)
    if in_dims != out_dims:
        return False, f"pyramid dims differ: {in_dims} vs {out_dims}"

    # 3. openslide sees a usable label.
    try:
        slide = openslide.OpenSlide(str(out))
    except openslide.OpenSlideError as e:
        return False, f"openslide open: {e}"
    try:
        if "label" not in slide.associated_images:
            return False, "no 'label' associated image"
        label_img = np.asarray(slide.associated_images["label"].convert("RGB"))
        if not label_is_non_trivial(label_img):
            return False, f"label is trivial (std={label_img.std():.2f})"
    finally:
        slide.close()

    return True, "ok"


def main() -> int:
    if len(sys.argv) < 2:
        print("usage: cross_validate.py <wsi-label-binary>", file=sys.stderr)
        return 2
    exe = Path(sys.argv[1]).resolve()
    if not exe.is_file():
        print(f"ERROR: {exe} not found", file=sys.stderr)
        return 2

    tiffinfo = shutil.which("tiffinfo")
    if not tiffinfo:
        print(
            "ERROR: 'tiffinfo' not on PATH. Install libtiff-tools "
            "(apt: libtiff-tools, brew: libtiff).",
            file=sys.stderr,
        )
        return 2

    TMP.mkdir(exist_ok=True)
    label = make_label(TMP)

    samples = sorted(SAMPLES.glob("*.svs"))
    if not samples:
        print(f"No .svs files found in {SAMPLES}", file=sys.stderr)
        return 2

    failures = 0
    for s in samples:
        ok, msg = validate_one(exe, s, label, TMP, tiffinfo)
        status = "OK " if ok else "FAIL"
        print(f"[{status}] {s.name}: {msg}")
        if not ok:
            failures += 1

    print(f"\n{len(samples) - failures}/{len(samples)} samples passed.")
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
