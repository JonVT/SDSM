#!/usr/bin/env python3
"""
Check which /api routes registered in cmd/sdsm/main.go have no references in the UI layer.
Usage:
    python tools/check_api_ui_usage.py
"""
from __future__ import annotations

import re
import sys
from dataclasses import dataclass, field
from pathlib import Path
from typing import Iterable, List

REPO_ROOT = Path(__file__).resolve().parents[1]
MAIN_FILE = REPO_ROOT / "cmd" / "sdsm" / "main.go"
UI_DIRS = [REPO_ROOT / "ui", REPO_ROOT / "webassets"]
TEXT_EXTENSIONS = {
    ".html",
    ".htm",
    ".js",
    ".ts",
    ".tsx",
    ".css",
    ".scss",
    ".go",
    ".json",
    ".md",
    ".txt",
    ".tmpl",
}

ROUTE_RE = re.compile(r"api\.([A-Z]+)\s*\(\s*\"([^\"]+)\"")
PARAM_RE = re.compile(r"\\:([a-zA-Z0-9_]+)")


@dataclass
class Endpoint:
    method: str
    path: str
    line: int
    regex: re.Pattern | None = None
    used: bool = False
    matches: List[Path] = field(default_factory=list)

    @property
    def full_path(self) -> str:
        # API group is mounted at /api
        return f"/api{self.path}"

    def build_regex(self) -> re.Pattern:
        escaped = re.escape(self.full_path)
        # Replace param segments like \:server_id with a loose matcher for any non-/ characters
        pattern = PARAM_RE.sub(r"[^/]+", escaped)
        # Allow wildcard segments (if any) to match lazily
        pattern = pattern.replace(r"\*", r"[^\s]*")
        return re.compile(pattern)


def parse_endpoints() -> List[Endpoint]:
    if not MAIN_FILE.exists():
        raise SystemExit(f"Cannot locate main.go at {MAIN_FILE}")
    content = MAIN_FILE.read_text(encoding="utf-8")
    endpoints: List[Endpoint] = []
    for match in ROUTE_RE.finditer(content):
        method, path = match.groups()
        line = content.count("\n", 0, match.start()) + 1
        endpoints.append(Endpoint(method=method, path=path, line=line))
    # Deduplicate by method+path while keeping first occurrence (line info).
    unique: dict[tuple[str, str], Endpoint] = {}
    for ep in endpoints:
        key = (ep.method, ep.path)
        if key not in unique:
            unique[key] = ep
    endpoints = list(unique.values())
    for ep in endpoints:
        ep.regex = ep.build_regex()
    return endpoints


def iter_ui_files() -> Iterable[Path]:
    for base in UI_DIRS:
        if not base.exists():
            continue
        for path in base.rglob("*"):
            if path.is_dir():
                continue
            if path.suffix.lower() not in TEXT_EXTENSIONS:
                continue
            yield path


def scan_usage(endpoints: List[Endpoint]) -> None:
    files = list(iter_ui_files())
    for file_path in files:
        try:
            text = file_path.read_text(encoding="utf-8")
        except UnicodeDecodeError:
            continue
        rel = file_path.relative_to(REPO_ROOT)
        for ep in endpoints:
            if ep.used:
                continue
            if ep.regex and ep.regex.search(text):
                ep.used = True
                ep.matches.append(rel)


def main() -> None:
    endpoints = parse_endpoints()
    if not endpoints:
        print("No API endpoints parsed.")
        return
    scan_usage(endpoints)
    unused = [ep for ep in endpoints if not ep.used]
    used = len(endpoints) - len(unused)

    print(f"Total API endpoints found: {len(endpoints)}")
    print(f"Used by UI artifacts:      {used}")
    print(f"Unused in UI (potential):  {len(unused)}")
    print()
    if unused:
        print("Endpoints without matches in ui/* or webassets/*:")
        for ep in unused:
            print(f"  - {ep.method:<6} {ep.full_path} (defined line {ep.line})")
        print()
        print(
            "NOTE: Some endpoints might be consumed by non-UI clients (CLI, integrations).\n"
            "Use this report as a starting point and double-check before removing anything."
        )
    else:
        print("All parsed endpoints have at least one UI reference.")


if __name__ == "__main__":
    main()
