#!/usr/bin/env python3
"""Move src/**/*_test.go to src/test/** and convert to external tests with dot-import."""
from __future__ import annotations

import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SRC = ROOT / "src"
DST = SRC / "test"
MODULE = "golang.zx2c4.com/wireguard/src"
SKIP = {SRC / "format_test.go"}


def transform(content: str, import_path: str) -> str:
    # Split off leading build tags + license comments before "package"
    m_pkg = re.search(r"(?m)^package\s+(\w+)\s*$", content)
    if not m_pkg:
        raise ValueError("no package clause")
    old_pkg = m_pkg.group(1)
    new_pkg = old_pkg if old_pkg.endswith("_test") else old_pkg + "_test"
    header = content[: m_pkg.start()]
    after = content[m_pkg.end() :]  # starts with newline typically
    after = after.lstrip("\n")

    # Parse imports
    imports: list[str] = []
    body = after
    if body.startswith("import ("):
        end = body.find("\n)")
        if end < 0:
            raise ValueError("unclosed import block")
        block = body[len("import (") : end]
        body = body[end + len("\n)") :].lstrip("\n")
        for line in block.splitlines():
            line = line.strip()
            if line:
                imports.append(line)
    elif body.startswith("import "):
        sm = re.match(r'import\s+((?:\.\s+)?"[^"]+")\s*\n', body)
        if not sm:
            raise ValueError(f"bad single import: {body[:80]!r}")
        imports.append(sm.group(1).strip())
        body = body[sm.end() :].lstrip("\n")

    # Ensure dot-import of package under test
    has = any(import_path in x for x in imports)
    if not has:
        imports.insert(0, f'. "{import_path}"')
    elif not any(x.startswith(". ") and import_path in x for x in imports):
        # has regular import — upgrade to dot import
        imports = [x for x in imports if import_path not in x]
        imports.insert(0, f'. "{import_path}"')

    out = header
    if header and not header.endswith("\n"):
        out += "\n"
    out += f"package {new_pkg}\n\n"
    out += "import (\n"
    for imp in imports:
        out += f"\t{imp}\n"
    out += ")\n\n"
    out += body
    if not out.endswith("\n"):
        out += "\n"
    return out


def main() -> int:
    moved = 0
    for path in sorted(SRC.rglob("*_test.go")):
        if DST in path.parents or path == DST / path.name:
            continue
        try:
            path.relative_to(DST)
            continue
        except ValueError:
            pass
        if path in SKIP:
            continue
        rel = path.relative_to(SRC)
        parent = rel.parent
        if str(parent) == ".":
            continue
        import_path = f"{MODULE}/{parent.as_posix()}"
        dest = DST / rel
        dest.parent.mkdir(parents=True, exist_ok=True)
        text = path.read_text(encoding="utf-8")
        try:
            new_text = transform(text, import_path)
        except ValueError as e:
            print(f"FAIL {rel}: {e}", file=sys.stderr)
            return 1
        dest.write_text(new_text, encoding="utf-8", newline="\n")
        path.unlink()
        moved += 1
        print(f"moved {rel.as_posix()}")
    fmt = SRC / "format_test.go"
    if fmt.exists():
        fmt.unlink()
        print("removed src/format_test.go")
    print(f"done: {moved} files")
    return 0


if __name__ == "__main__":
    sys.exit(main())
