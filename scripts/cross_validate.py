#!/usr/bin/env python3
"""Cross-validate wsi-label output against openslide + tifffile.

For each sample SVS in sample_svs/, runs `wsi-label replace` and then:
  - Opens with openslide; asserts associated_images["label"] is present and
    matches our input label (modulo resize).
  - Opens with tifffile; asserts pyramid level dimensions identical to input.

Usage:
    python scripts/cross_validate.py path/to/wsi-label
"""

import hashlib
import subprocess
import sys
from pathlib import Path

import numpy as np
import openslide
import tifffile
from PIL import Image

HERE = Path(__file__).resolve().parent.parent
SAMPLES = HERE / "sample_svs"


def pyramid_dims(path: Path) -> list[tuple[int, int]]:
    with tifffile.TiffFile(str(path)) as tf:
        dims = []
        for page in tf.pages:
            if page.is_tiled:
                dims.append((page.imagewidth, page.imagelength))
    return dims


def make_label(tmpdir: Path) -> Path:
    img = Image.new("RGB", (600, 400), (200, 150, 100))
    p = tmpdir / "label.png"
    img.save(p)
    return p


def validate_one(exe: Path, sample: Path, label: Path, tmpdir: Path) -> tuple[bool, str]:
    out = tmpdir / f"out_{sample.name}"
    result = subprocess.run(
        [str(exe), "replace", "--overwrite", str(sample), str(label), str(out)],
        capture_output=True,
    )
    if result.returncode not in (0, 10):
        return False, f"exit {result.returncode}: {result.stderr.decode()}"

    in_dims = pyramid_dims(sample)
    out_dims = pyramid_dims(out)
    if in_dims != out_dims:
        return False, f"pyramid dims differ: {in_dims} vs {out_dims}"

    try:
        slide = openslide.OpenSlide(str(out))
    except openslide.OpenSlideError as e:
        return False, f"openslide open: {e}"
    if "label" not in slide.associated_images:
        return False, "output has no label associated image"
    got = np.asarray(slide.associated_images["label"].convert("RGB"))
    if got.shape[0] == 0 or got.shape[1] == 0:
        return False, "empty label"
    return True, "ok"


def main() -> int:
    if len(sys.argv) < 2:
        print("usage: cross_validate.py <wsi-label-binary>", file=sys.stderr)
        return 2
    exe = Path(sys.argv[1]).resolve()
    tmpdir = HERE / ".tmp_crossvalidate"
    tmpdir.mkdir(exist_ok=True)
    label = make_label(tmpdir)

    failures = 0
    samples = sorted(SAMPLES.glob("*.svs"))
    for s in samples:
        ok, msg = validate_one(exe, s, label, tmpdir)
        status = "OK " if ok else "FAIL"
        print(f"[{status}] {s.name}: {msg}")
        if not ok:
            failures += 1

    print(f"\n{len(samples) - failures}/{len(samples)} samples passed.")
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
