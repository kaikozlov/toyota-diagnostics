# Repository instructions

This repository owns toyo-cli, an independent diagnostics runtime and CLI for Toyota vehicles.

## Boundaries

- `./toyo` is the only public CLI. Application logic, decoding, planning, sessions, ISO-TP/UDS and J2534 live in Go under `cmd/toyo/` and `internal/`.
- The only Python is the private raw-CAN adapter embedded from `internal/transport/panda_adapter.py`. It delegates USB/SPI and managed pandad access to upstream Panda/openpilot. Do not put Toyota metadata, diagnostic protocols or application behavior in this adapter.
- `catalog/catalog.sqlite`, tracked with Git LFS, is the single shipped source of diagnostic metadata. All commands read it directly. Do not add parallel JSON/ZIP catalogs, extracted copies, or dependencies on another checkout. Search projections and indexes belong in the same database.
- Do not add Toyota DLL/DDB/EXE binaries or other proprietary source artifacts.
- Offline commands and J2534 must not require Python, Panda or an openpilot checkout. Panda imports stay lazy; managed pandad safety/ownership checks belong at the raw-CAN boundary.

## Development

- Run `go test ./...`; build with `go build -o build/toyo ./cmd/toyo`. Transport adapter tests use fake upstream modules and skip if Python is unavailable.
- Direct Panda uses an existing Python environment with upstream Panda installed. `TOYO_PYTHON` (alias `TOYOTA_PYTHON`) selects it.
- Comma/AGNOS reuses `/usr/local/venv` and `/data/openpilot`; do not create a second diagnostics virtualenv.
- Prefer targeted tests while iterating, then run the full suite for shared runtime changes. Hardware behavior must be verified separately from fake transport tests.

## Catalog updates

The database is a diagnostic product, not a research-export archive. Retain decoder parameters, requests and sessions, safety/refusal conditions, vehicle routing and topology, descriptions, units, ranges, choices and meaningful offline diagnostic knowledge. Preserve unsupported and ambiguous states rather than advertising only executable successes. Exclude extraction/query traces, DLL filesystem inventories and vendor-record reconstruction data that do not serve those features. Keep compact release information and actionable origin IDs where useful. Preserve explicit unknown or uninterpreted field bytes, including zeros and presence, until their meaning is established; they are not extraction provenance. Check source coverage against original decoded records and literal source-byte expectations, not only against the product projection itself.

Store semantic facts once and reference them from document structure and search. Do not add vendor-byte decoders merely to reproduce an upstream research payload. `--json` exposes complete diagnostic records, not the research extraction graph. A field is not disposable solely because its name is `raw`, `offset`, `source` or `plugin_binding`: trace its actual consumers and meaning. Test retained diagnostic behavior and useful inspection output, not parity with research-only metadata.

`item_occurrences` owns search source locations and refers to shared `search_records`/text; `items` is the view the full-text index reads. Search entries point into source documents rather than storing payload copies. Owner ranges/counts and ECU query projections are derived indexes. A storage-only change must preserve diagnostic occurrences and ranking. A deliberate product-scope change may remove research-only search entries; test retained semantic coverage and useful matching rather than pinning obsolete research counts. Test missing relationship detection, search behavior, cold/hot inspection and bounded-memory runtime use. Never substitute a partial development database.
