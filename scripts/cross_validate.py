#!/usr/bin/env python3
"""Cross-validate wsi-label output against external readers.

For each sample SVS in sample_svs/, runs `wsi-label replace --resize=none`
with a synthetic 1200x848 label and then performs four independent checks:

  1. libtiff strict decode (tiffinfo -D): opens every IFD AND decodes every
     strip/tile. Fails on warnings like "Using code not yet in table", which
     indicate TIFF-spec-non-conforming LZW output that strict downstream
     tools (e.g., openslide's deepzoom_tile.py via TIFFRGBAImageGet) reject.

  2. tifffile pyramid dims: asserts the output's tiled pyramid levels match
     the input's exactly (the splice must not touch pyramid pixels).

  3. openslide associated-image lookup: asserts the output has a "label"
     associated image that is non-empty and non-uniform (not black, not
     all-background-color).

  4. Pixel-exact label round-trip via tifffile: decodes the label IFD and
     compares it to the original input label bytes. LZW + predictor-2 is
     lossless, and writing at the exact target dims with --resize=none
     means no resampling; any pixel drift is a real decoder bug.

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


def existing_label_dims(svs: Path) -> tuple[int, int] | None:
    """Return (width, height) of the file's label IFD, or None if absent."""
    with tifffile.TiffFile(str(svs)) as tf:
        for page in tf.pages:
            desc_tag = page.tags.get("ImageDescription")
            if desc_tag is None:
                continue
            lines = str(desc_tag.value).splitlines()
            if len(lines) >= 2 and lines[1].startswith("label"):
                return int(page.imagewidth), int(page.imagelength)
    return None


def make_label(tmpdir: Path, width: int, height: int) -> tuple[Path, np.ndarray]:
    """Build a synthetic noise label at (width, height).

    Caller sizes the label to match the target SVS's existing label IFD so
    --resize=none applies (no resampling), which in turn means the LZW +
    predictor-2 round-trip is bit-exact lossless.
    """
    rng = np.random.default_rng(42)
    arr = rng.integers(0, 256, size=(height, width, 3), dtype=np.uint8)
    p = tmpdir / f"label_{width}x{height}.png"
    Image.fromarray(arr).save(p)
    return p, arr


def read_label_pixels(path: Path) -> np.ndarray | None:
    """Decode the label IFD's pixels via tifffile and return them as an array.

    Finds the IFD whose ImageDescription second line starts with 'label'.
    Returns None if no such IFD exists.
    """
    with tifffile.TiffFile(str(path)) as tf:
        for page in tf.pages:
            desc = page.tags.get("ImageDescription")
            if desc is None:
                continue
            lines = str(desc.value).splitlines()
            if len(lines) >= 2 and lines[1].startswith("label"):
                return page.asarray()
    return None


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
    tmpdir: Path,
    tiffinfo: str | None,
) -> tuple[bool, str]:
    # Size the synthetic label to match the existing label IFD so wsi-label's
    # dim-preserving path picks the same target and --resize=none holds.
    dims = existing_label_dims(sample)
    if dims is None:
        return False, "no label IFD in input (test corpus should always have one)"
    label, label_truth = make_label(tmpdir, dims[0], dims[1])

    out = tmpdir / f"out_{sample.name}"
    # --resize=none forces the "exact target dims" path so the round-trip
    # through LZW + predictor-2 is bit-exact lossless.
    result = subprocess.run(
        [
            str(exe), "replace", "--overwrite", "--resize", "none",
            str(sample), str(label), str(out),
        ],
        capture_output=True,
    )
    if result.returncode not in (0, 10):
        return False, f"wsi-label exit {result.returncode}: {result.stderr.decode().strip()}"

    # 1. libtiff strict decode (skipped if tiffinfo isn't installed).
    if tiffinfo:
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

    # 4. Pixel-exact label round-trip via tifffile's independent decoder.
    #    Since we wrote the label at exact target dims with --resize=none,
    #    LZW + predictor-2 round-trips losslessly.
    decoded = read_label_pixels(out)
    if decoded is None:
        return False, "no label IFD found by tifffile"
    if decoded.shape != label_truth.shape:
        return False, f"label shape {decoded.shape} != truth {label_truth.shape}"
    if not np.array_equal(decoded, label_truth):
        diff = np.abs(decoded.astype(int) - label_truth.astype(int))
        return False, (
            f"label pixels differ from input: max |Δ|={diff.max()}, "
            f"mean |Δ|={diff.mean():.2f}, {(diff != 0).sum()} pixels off"
        )

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
            "WARNING: 'tiffinfo' not on PATH — skipping the strict libtiff\n"
            "         decode check. openslide + pyramid-dims checks will\n"
            "         still run, but TIFF-spec violations that openslide\n"
            "         silently accepts (e.g., non-conforming LZW streams)\n"
            "         will not be caught. Install with:\n"
            "           apt-get install libtiff-tools       # Debian/Ubuntu\n"
            "           brew install libtiff                # macOS\n"
            "           dnf install libtiff-tools           # Fedora\n",
            file=sys.stderr,
        )

    TMP.mkdir(exist_ok=True)

    samples = sorted(SAMPLES.glob("*.svs"))
    if not samples:
        print(f"No .svs files found in {SAMPLES}", file=sys.stderr)
        return 2

    failures = 0
    for s in samples:
        ok, msg = validate_one(exe, s, TMP, tiffinfo)
        status = "OK " if ok else "FAIL"
        print(f"[{status}] {s.name}: {msg}")
        if not ok:
            failures += 1

    coverage = "full" if tiffinfo else "DEGRADED (no tiffinfo — libtiff strict check skipped)"
    print(f"\n{len(samples) - failures}/{len(samples)} samples passed. Coverage: {coverage}")
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
