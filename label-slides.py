"""Generate anonymized label images and swap them into Aperio SVS files.

A driver around the `wsi-label` CLI. Handles label rendering (anonymized text,
sized to Aperio's 1200x848 label slot) and batch execution, including a CSV
pathway intended for lab workflows where the filename → label mapping already
lives in a spreadsheet.

Python dependencies:
    pillow              (required — label rendering)
    questionary         (optional but recommended — nicer interactive prompts;
                         falls back to plain input() when missing or no TTY)

Three input styles:

  1. Interactive (bare invocation):

         ./label-slides.py --svs-dir sample_svs

     Arrow-key checkbox picker over SVS files in the given directory,
     then per-file label lines.

  2. Same label for every file:

         ./label-slides.py --text "Case 12345|HE|2026-04-20" *.svs

  3. Per-file labels from a spreadsheet (CSV, header row optional, first
     column = filename, remaining columns = label lines):

         ./label-slides.py --csv labels.csv --svs-dir sample_svs
         ./label-slides.py --csv labels.csv --svs-dir sample_svs --overwrite -j 4

Stripping labels (no new label — just remove the existing one):

     ./label-slides.py --strip *.svs

Run `./label-slides.py --help` for all flags.
"""

from __future__ import annotations

import argparse
import csv
import os
import shutil
import subprocess
import sys
import tempfile
import time
from concurrent.futures import ThreadPoolExecutor, as_completed
from dataclasses import dataclass, field
from pathlib import Path
from typing import Iterable, Sequence

from PIL import Image, ImageDraw, ImageFont

# questionary gives arrow-key checkbox + confirm prompts. Imported lazily
# inside interactive paths so scripted use (--text / --csv / --strip) doesn't
# require it.
try:
    import questionary
    from questionary import Style
    _QUESTIONARY_OK = True
except ImportError:
    _QUESTIONARY_OK = False

# Aperio label dimensions. The wsi-label tool will resize any input, but
# producing exact dimensions skips a resize pass and keeps text crisp.
LABEL_W = 1200
LABEL_H = 848

# Aperio label background (parchment); matches wsi-label's default.
LABEL_BG = (245, 245, 230)
LABEL_FG = (0, 0, 0)

# wsi-label exit codes we treat as success.
EXIT_OK = 0
EXIT_LABEL_ADDED = 10
EXIT_OK_SET = {EXIT_OK, EXIT_LABEL_ADDED}


# ---------------------------------------------------------------------------
# Data classes
# ---------------------------------------------------------------------------


@dataclass
class Job:
    svs_in: Path
    svs_out: Path
    lines: list[str] = field(default_factory=list)
    strip: bool = False  # True → no label rendering, call `wsi-label strip`


@dataclass
class RunResult:
    job: Job
    returncode: int
    elapsed: float
    stderr: str


# ---------------------------------------------------------------------------
# wsi-label binary discovery
# ---------------------------------------------------------------------------


def find_wsi_label() -> Path:
    """Locate the wsi-label binary. Prefer script-adjacent, fall back to PATH."""
    names = ("wsi-label", "wsi-label.exe")
    here = Path(__file__).resolve().parent
    for name in names:
        candidate = here / name
        if candidate.is_file() and os.access(candidate, os.X_OK):
            return candidate
    for name in names:
        found = shutil.which(name)
        if found:
            return Path(found)
    raise FileNotFoundError(
        "Could not find 'wsi-label' binary. Build it with "
        "'go build -o wsi-label ./cmd/wsi-label' or put it on your PATH."
    )


# ---------------------------------------------------------------------------
# Label rendering
# ---------------------------------------------------------------------------


_FONT_CANDIDATES = (
    "DejaVuSans-Bold.ttf",
    "DejaVuSans.ttf",
    "LiberationSans-Bold.ttf",
    "LiberationSans-Regular.ttf",
    "Arial Bold.ttf",
    "arialbd.ttf",
    "arial.ttf",
    "Helvetica.ttc",
)


def load_font(size: int) -> ImageFont.FreeTypeFont | ImageFont.ImageFont:
    for name in _FONT_CANDIDATES:
        try:
            return ImageFont.truetype(name, size)
        except OSError:
            continue
    return ImageFont.load_default()


def render_label(lines: Sequence[str], rotate: bool) -> Image.Image:
    """Render label text centered on a 1200×848 RGB canvas."""
    if not lines:
        lines = ["(blank)"]

    img = Image.new("RGB", (LABEL_W, LABEL_H), LABEL_BG)
    draw = ImageDraw.Draw(img)
    draw.rectangle([(8, 8), (LABEL_W - 9, LABEL_H - 9)], outline=LABEL_FG, width=3)

    # Font size heuristic: fill ~75% of height, divided among lines.
    n = len(lines)
    target_line_h = max(24, int((LABEL_H * 0.75) / n))
    font = load_font(target_line_h)

    heights: list[int] = []
    total_h = 0
    for line in lines:
        bbox = draw.textbbox((0, 0), line, font=font)
        h = bbox[3] - bbox[1]
        heights.append(h)
        total_h += h
    total_h += 12 * (n - 1)

    y = (LABEL_H - total_h) // 2
    for line, h in zip(lines, heights):
        bbox = draw.textbbox((0, 0), line, font=font)
        w = bbox[2] - bbox[0]
        x = (LABEL_W - w) // 2
        draw.text((x - bbox[0], y - bbox[1]), line, fill=LABEL_FG, font=font)
        y += h + 12

    if rotate:
        img = img.rotate(180)
    return img


# ---------------------------------------------------------------------------
# Input gathering
# ---------------------------------------------------------------------------


# Prompt styling for questionary — muted, consistent with terminal.
_Q_STYLE = Style([
    ("qmark", "fg:#5f87d7 bold"),
    ("question", "bold"),
    ("pointer", "fg:#5f87d7 bold"),
    ("highlighted", "fg:#5f87d7 bold"),
    ("selected", "fg:#5fd7a7"),
    ("separator", "fg:#6c6c6c"),
    ("instruction", "fg:#6c6c6c italic"),
]) if _QUESTIONARY_OK else None


def _has_tty() -> bool:
    return sys.stdin.isatty() and sys.stdout.isatty()


def prompt(msg: str, default: str | None = None) -> str:
    """Single-line text prompt. Uses questionary when available + interactive."""
    if _QUESTIONARY_OK and _has_tty():
        ans = questionary.text(msg, default=default or "", style=_Q_STYLE).ask()
        return (ans or "").strip()
    suffix = f" [{default}]" if default else ""
    val = input(f"{msg}{suffix}: ").strip()
    return val or (default or "")


def yesno(msg: str, default: str = "n") -> bool:
    default_bool = default.lower().startswith("y")
    if _QUESTIONARY_OK and _has_tty():
        return bool(questionary.confirm(msg, default=default_bool, style=_Q_STYLE).ask())
    raw = input(f"{msg} (y/n) [{'y' if default_bool else 'n'}]: ").strip()
    if not raw:
        return default_bool
    return raw.lower().startswith("y")


def gather_lines_interactive(filename: str) -> list[str]:
    """Gather label lines for one file. Blank entry ends input."""
    print(f"\n  Label for {filename} — enter lines (blank to finish):")
    lines: list[str] = []
    while True:
        if _QUESTIONARY_OK and _has_tty():
            line = questionary.text(
                f"line {len(lines) + 1}",
                style=_Q_STYLE,
                qmark="  ·",
            ).ask()
        else:
            line = input(f"  line {len(lines) + 1}: ")
        if line is None or line == "":
            break
        lines.append(line.rstrip())
    return lines or ["(blank label)"]


def pick_svs_files_interactive(search_dirs: Sequence[Path]) -> list[Path]:
    seen: set[Path] = set()
    available: list[Path] = []
    for d in search_dirs:
        if not d.is_dir():
            continue
        for p in sorted(d.glob("*.svs")):
            rp = p.resolve()
            if p.is_file() and rp not in seen:
                seen.add(rp)
                available.append(p)
    if not available:
        path = prompt("Path to input .svs")
        return [Path(path)] if path else []

    # Rich picker via questionary when we have a TTY.
    if _QUESTIONARY_OK and _has_tty():
        choices = [
            questionary.Choice(
                f"{p.name}  ({_human_size(p.stat().st_size)})",
                value=p,
                checked=True,
            )
            for p in available
        ]
        picks = questionary.checkbox(
            "Select SVS files (space=toggle, a=toggle all, enter=confirm)",
            choices=choices,
            style=_Q_STYLE,
        ).ask()
        return list(picks or [])

    # Fallback text picker.
    print("\nSVS files found:")
    for i, f in enumerate(available, 1):
        print(f"  {i:2d}. {f.name}")
    print("   a. all of the above")
    choice = prompt("Pick numbers (comma-separated), 'a' for all, or type paths", "a")
    if choice.lower() == "a":
        return available
    picks: list[Path] = []
    for token in choice.split(","):
        token = token.strip()
        if not token:
            continue
        if token.isdigit() and 1 <= int(token) <= len(available):
            picks.append(available[int(token) - 1])
        else:
            picks.append(Path(token))
    return picks


def _human_size(n: int) -> str:
    for unit in ("B", "KB", "MB", "GB", "TB"):
        if n < 1024:
            return f"{n:.1f} {unit}" if unit != "B" else f"{n} B"
        n /= 1024  # type: ignore[assignment]
    return f"{n:.1f} PB"


def load_csv_labels(path: Path) -> dict[str, list[str]]:
    """Parse a CSV mapping filename → label lines.

    Header row is expected but not strictly required. The first column is
    always the filename; all non-empty remaining columns are label lines in
    order.
    """
    mapping: dict[str, list[str]] = {}
    with path.open(newline="") as f:
        reader = csv.reader(f)
        rows = list(reader)
    if not rows:
        return mapping

    # Heuristic: treat row 0 as a header if its first cell is not a filename
    # (i.e. no .svs extension).
    start = 1 if not rows[0][0].lower().endswith(".svs") else 0
    for row in rows[start:]:
        if not row or not row[0].strip():
            continue
        name = row[0].strip()
        lines = [c.strip() for c in row[1:] if c and c.strip()]
        mapping[name] = lines or ["(blank label)"]
    return mapping


def resolve_one_svs(token: str, search_dirs: Sequence[Path]) -> Path | None:
    """Resolve a single SVS reference against the filesystem.

    Tries, in order:
      1. The token as-is (absolute or CWD-relative).
      2. token joined to each of search_dirs.

    Returns the first existing file or None.
    """
    p = Path(token)
    if p.is_file():
        return p.resolve()
    for d in search_dirs:
        candidate = d / token
        if candidate.is_file():
            return candidate.resolve()
    return None


def resolve_svs_paths(tokens: Iterable[str], search_dirs: Sequence[Path]) -> list[Path]:
    """Resolve CLI path tokens. Raises FileNotFoundError on the first miss."""
    resolved: list[Path] = []
    for tok in tokens:
        found = resolve_one_svs(tok, search_dirs)
        if not found:
            raise FileNotFoundError(f"No such SVS file: {tok}")
        resolved.append(found)
    return resolved


# ---------------------------------------------------------------------------
# Job construction
# ---------------------------------------------------------------------------


def default_out_path(svs_in: Path, suffix: str) -> Path:
    return svs_in.with_name(svs_in.stem + suffix + svs_in.suffix)


def build_jobs(
    svs_files: Sequence[Path],
    *,
    strip: bool,
    text_lines: list[str] | None,
    csv_mapping: dict[str, list[str]] | None,
    interactive_lines: bool,
    out_suffix: str,
) -> list[Job]:
    jobs: list[Job] = []
    for svs in svs_files:
        lines: list[str] = []
        if not strip:
            if text_lines is not None:
                lines = text_lines
            elif csv_mapping is not None:
                # Try exact filename, then basename.
                lines = csv_mapping.get(svs.name) or csv_mapping.get(svs.stem) or []
                if not lines:
                    print(
                        f"WARN: no CSV entry for {svs.name}; using '(blank label)'",
                        file=sys.stderr,
                    )
                    lines = ["(blank label)"]
            elif interactive_lines:
                lines = gather_lines_interactive(svs.name)
                # After the first file, offer "use these lines for all remaining".
                if len(jobs) == 0 and len(svs_files) > 1:
                    if yesno(
                        f"Use these {len(lines)} line(s) for the remaining "
                        f"{len(svs_files) - 1} file(s)?",
                        "n",
                    ):
                        text_lines = lines
            else:
                lines = ["(blank label)"]
        jobs.append(
            Job(
                svs_in=svs,
                svs_out=default_out_path(svs, out_suffix),
                lines=lines,
                strip=strip,
            )
        )
    return jobs


# ---------------------------------------------------------------------------
# Execution
# ---------------------------------------------------------------------------


def run_one(
    job: Job,
    *,
    wsi_label: Path,
    rotate: bool,
    overwrite: bool,
    tmpdir: Path,
) -> RunResult:
    t0 = time.time()

    if job.strip:
        cmd = [str(wsi_label), "strip"]
        if overwrite:
            cmd.append("--overwrite")
        cmd += [str(job.svs_in), str(job.svs_out)]
    else:
        img = render_label(job.lines, rotate=rotate)
        # Unique per-job temp PNG (enables parallel execution without collisions).
        png_path = tmpdir / f"{job.svs_in.stem}.label.png"
        img.save(png_path, format="PNG")
        cmd = [str(wsi_label), "replace"]
        if overwrite:
            cmd.append("--overwrite")
        cmd += [str(job.svs_in), str(png_path), str(job.svs_out)]

    proc = subprocess.run(cmd, capture_output=True, text=True)
    return RunResult(
        job=job,
        returncode=proc.returncode,
        elapsed=time.time() - t0,
        stderr=proc.stderr.strip(),
    )


def describe_exit(code: int) -> str:
    return {
        0: "OK",
        2: "usage error",
        3: "unreadable / unrecognized input",
        4: "output path conflict (use --overwrite)",
        5: "unexpected TIFF layout (refused)",
        10: "OK (label added, not replaced)",
    }.get(code, f"exit {code}")


def run_jobs(
    jobs: list[Job],
    *,
    wsi_label: Path,
    rotate: bool,
    overwrite: bool,
    parallel: int,
) -> int:
    if not jobs:
        return 0
    failures = 0
    n = len(jobs)

    with tempfile.TemporaryDirectory(prefix="label-slides_") as tmp:
        tmpdir = Path(tmp)

        def submit(job: Job) -> RunResult:
            return run_one(
                job, wsi_label=wsi_label, rotate=rotate,
                overwrite=overwrite, tmpdir=tmpdir,
            )

        if parallel <= 1:
            results_iter: Iterable[RunResult] = (submit(j) for j in jobs)
        else:
            pool = ThreadPoolExecutor(max_workers=parallel)
            futures = {pool.submit(submit, j): j for j in jobs}
            results_iter = (f.result() for f in as_completed(futures))

        done = 0
        for res in results_iter:
            done += 1
            tag = describe_exit(res.returncode)
            action = "stripped" if res.job.strip else (
                "added" if res.returncode == EXIT_LABEL_ADDED else "replaced"
            )
            ok = res.returncode in EXIT_OK_SET
            status = f"[{done}/{n}] {res.job.svs_in.name}"
            if ok:
                print(f"{status}: {action} → {res.job.svs_out.name}  ({res.elapsed:.1f}s, {tag})")
            else:
                failures += 1
                print(
                    f"{status}: FAILED ({tag}, {res.elapsed:.1f}s)\n    {res.stderr}",
                    file=sys.stderr,
                )

    print(f"\nDone. {n - failures}/{n} succeeded.")
    return 1 if failures else 0


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------


def build_parser() -> argparse.ArgumentParser:
    p = argparse.ArgumentParser(
        prog="label-slides.py",
        description=(
            "Render anonymized labels and swap them into Aperio SVS files "
            "via wsi-label. Interactive when run bare."
        ),
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    p.add_argument(
        "files",
        nargs="*",
        help="Input SVS file(s). If omitted, runs interactive picker from the script directory.",
    )
    src = p.add_mutually_exclusive_group()
    src.add_argument(
        "--text",
        metavar='"line1|line2|..."',
        help="Apply the same label text to every file. Use '|' between lines.",
    )
    src.add_argument(
        "--csv",
        metavar="PATH",
        type=Path,
        help="Per-file labels from a CSV. Columns: filename,line1,line2,...",
    )
    src.add_argument(
        "--strip",
        action="store_true",
        help="Strip the existing label — don't render a new one.",
    )

    p.add_argument(
        "--suffix",
        default="_relabeled",
        help="Output filename suffix (default: _relabeled).",
    )
    p.add_argument(
        "--rotate",
        choices=("auto", "yes", "no"),
        default="auto",
        help=(
            "Rotate label 180° to match typical Aperio orientation. "
            "'auto' asks in interactive mode, defaults to yes otherwise."
        ),
    )
    p.add_argument(
        "--overwrite",
        action="store_true",
        help="Pass --overwrite through to wsi-label.",
    )
    p.add_argument(
        "-j",
        "--parallel",
        type=int,
        default=1,
        metavar="N",
        help="Run N wsi-label jobs concurrently (default: 1).",
    )
    p.add_argument(
        "--preview",
        action="store_true",
        help="Show each rendered label in a window before running (PIL.Image.show).",
    )
    p.add_argument(
        "--dry-run",
        action="store_true",
        help="Render (and optionally preview) labels but do not call wsi-label.",
    )
    p.add_argument(
        "--svs-dir",
        type=Path,
        metavar="PATH",
        help=(
            "Directory to search for SVS files when they're referenced by "
            "basename (on the command line or in a CSV). Searched after CWD."
        ),
    )
    p.add_argument(
        "--wsi-label",
        type=Path,
        help="Explicit path to the wsi-label binary (default: auto-discover).",
    )
    p.add_argument(
        "-y",
        "--yes",
        action="store_true",
        help="Skip the interactive 'proceed?' confirmation.",
    )
    return p


def resolve_rotate(choice: str, interactive: bool) -> bool:
    if choice == "yes":
        return True
    if choice == "no":
        return False
    # auto
    if interactive:
        return yesno("Rotate labels 180° for typical Aperio orientation?", "y")
    return True


def print_summary(jobs: Sequence[Job], rotate: bool) -> None:
    print("\n=== Summary ===")
    print(f"Rotate: {'yes' if rotate else 'no'}")
    for j in jobs:
        action = "STRIP" if j.strip else "RELABEL"
        print(f"  [{action}] {j.svs_in.name}  →  {j.svs_out.name}")
        if not j.strip:
            for ln in j.lines:
                print(f"           | {ln}")


def maybe_preview(jobs: Sequence[Job], rotate: bool) -> None:
    for j in jobs:
        if j.strip:
            continue
        img = render_label(j.lines, rotate=rotate)
        img.show(title=j.svs_in.name)


# ---------------------------------------------------------------------------
# main
# ---------------------------------------------------------------------------


def main(argv: Sequence[str] | None = None) -> int:
    args = build_parser().parse_args(argv)

    try:
        wsi_label = args.wsi_label.resolve() if args.wsi_label else find_wsi_label()
    except FileNotFoundError as e:
        print(f"ERROR: {e}", file=sys.stderr)
        return 2
    if not wsi_label.is_file():
        print(f"ERROR: wsi-label not found at {wsi_label}", file=sys.stderr)
        return 2

    here = Path(__file__).resolve().parent
    interactive = not args.files and not args.csv

    # Directories to search for SVS files referenced by basename.
    search_dirs: list[Path] = []
    if args.svs_dir:
        search_dirs.append(args.svs_dir.resolve())
    search_dirs.append(here)

    # Resolve input file list.
    if args.csv:
        csv_map = load_csv_labels(args.csv)
        if not csv_map:
            print(f"ERROR: no rows found in {args.csv}", file=sys.stderr)
            return 2
        # If files are listed on CLI, intersect with CSV entries; otherwise
        # use every filename from the CSV.
        if args.files:
            try:
                svs_files = resolve_svs_paths(args.files, search_dirs)
            except FileNotFoundError as e:
                print(f"ERROR: {e}", file=sys.stderr)
                return 2
        else:
            svs_files = []
            for name in csv_map:
                found = resolve_one_svs(name, search_dirs)
                if not found:
                    print(
                        f"WARN: {name} listed in CSV but not found; skipping",
                        file=sys.stderr,
                    )
                    continue
                svs_files.append(found)
    elif args.files:
        try:
            svs_files = resolve_svs_paths(args.files, search_dirs)
        except FileNotFoundError as e:
            print(f"ERROR: {e}", file=sys.stderr)
            return 2
    else:
        svs_files = pick_svs_files_interactive(search_dirs)

    if not svs_files:
        print("No SVS files selected.", file=sys.stderr)
        return 2

    # Resolve text mode.
    text_lines: list[str] | None = None
    if args.text is not None:
        text_lines = [s for s in args.text.split("|") if s != ""]

    rotate = resolve_rotate(args.rotate, interactive)

    jobs = build_jobs(
        svs_files,
        strip=args.strip,
        text_lines=text_lines,
        csv_mapping=load_csv_labels(args.csv) if args.csv else None,
        interactive_lines=interactive and not args.strip,
        out_suffix=args.suffix,
    )

    print_summary(jobs, rotate)

    if args.preview and not args.strip:
        maybe_preview(jobs, rotate)

    if args.dry_run:
        print("\nDry run — no files written.")
        return 0

    if interactive and not args.yes:
        if not yesno("\nProceed?", "y"):
            return 0

    return run_jobs(
        jobs,
        wsi_label=wsi_label,
        rotate=rotate,
        overwrite=args.overwrite,
        parallel=max(1, args.parallel),
    )


if __name__ == "__main__":
    sys.exit(main())
