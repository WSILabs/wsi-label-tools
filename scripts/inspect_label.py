#!/usr/bin/env python3
"""Extract + measure the label IFD from a WSI file.

Usage:
    inspect_label.py <wsi-file> [<output.png>]

Finds the IFD whose ImageDescription second line starts with 'label',
decodes it, writes a PNG if requested, and prints width, height, and
aspect ratio.

Intended as a diagnostic: run before + after `wsi-label replace` and
visually compare the two PNGs.
"""

from __future__ import annotations

import sys
from pathlib import Path

import numpy as np
import tifffile
from PIL import Image


def extract_label(svs: Path) -> tuple[np.ndarray | None, str]:
    """Return (label_array, image_description) for the first 'label' IFD, or (None, '') if absent."""
    with tifffile.TiffFile(str(svs)) as tf:
        for page in tf.pages:
            desc_tag = page.tags.get("ImageDescription")
            if desc_tag is None:
                continue
            desc = str(desc_tag.value)
            lines = desc.splitlines()
            if len(lines) >= 2 and lines[1].startswith("label"):
                return page.asarray(), desc
    return None, ""


def describe(arr: np.ndarray) -> str:
    h, w = arr.shape[:2]
    channels = arr.shape[2] if arr.ndim == 3 else 1
    aspect = w / h
    orientation = "landscape" if aspect > 1 else "portrait" if aspect < 1 else "square"
    return (
        f"  width   : {w} px\n"
        f"  height  : {h} px\n"
        f"  aspect  : {aspect:.3f} ({orientation})\n"
        f"  channels: {channels}\n"
        f"  dtype   : {arr.dtype}"
    )


def main(argv: list[str]) -> int:
    if len(argv) < 2 or len(argv) > 3:
        print(__doc__, file=sys.stderr)
        return 2
    svs = Path(argv[1])
    out_png = Path(argv[2]) if len(argv) >= 3 else None

    arr, desc = extract_label(svs)
    if arr is None:
        print(f"{svs}: no label IFD found", file=sys.stderr)
        return 1

    print(f"File: {svs}")
    print(f"ImageDescription (label IFD):")
    for line in desc.splitlines():
        print(f"    {line!r}")
    print("Pixel data:")
    print(describe(arr))

    if out_png is not None:
        Image.fromarray(arr).save(out_png)
        print(f"\nWrote: {out_png} ({out_png.stat().st_size:,} bytes)")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
