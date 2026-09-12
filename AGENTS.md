# Repository instructions

This repository owns the standalone Toyota diagnostics runtime and CLI.

## Boundaries

- Runtime/library code lives in `toyota_diag/`; the installed CLI is `toyota`.
- Clean Toyota metadata under `toyota_diag/data/` is generated from the reverse-engineering work in `ghidra_rh850_analysis`. Do not hand-edit generated Toyota registry/bundle data here.
- Do not add Toyota DLL/DDB/EXE binaries or other proprietary source artifacts. This repository consumes clean derived metadata only.
- Keep openpilot-specific integration optional. Offline catalog/search/decode functionality must not require an openpilot checkout. Direct live Panda access may use the `live` extra; managed `pandad` reuse is allowed only through lazy imports.

## Development

- Install/sync with `uv sync --extra live` when live Panda support is needed; `uv sync` is sufficient for offline development.
- Run tests with `uv run pytest`.
- Prefer the smallest targeted test while iterating, then run the full suite before committing changes that affect shared runtime behavior.

## Provenance

The initial standalone source was extracted from `kaikozlov/kai-openpilot` branch `kai` at commit `7cde0135351f298b9a9d84344b5f685fde5a6005` on 2026-09-12. Subsequent development belongs here rather than in an openpilot branch unless it is genuinely openpilot integration glue.
