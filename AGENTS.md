# Repository instructions

This repository owns toyo-cli, an independent diagnostics runtime and CLI for Toyota vehicles.

## Boundaries

- `./toyo` is the only public CLI. Application logic, decoding, planning, sessions, ISO-TP/UDS and J2534 live in Go under `cmd/toyo/` and `internal/`.
- The only Python is the private raw-CAN adapter embedded from `internal/transport/panda_adapter.py`. It delegates USB/SPI and managed pandad access to upstream Panda/openpilot. Do not put Toyota metadata, diagnostic protocols or application behavior in this adapter.
- `catalog/catalog.sqlite`, tracked with Git LFS, is the single shipped source of diagnostic metadata. All commands read it directly. Do not add parallel JSON/ZIP catalogs, extracted copies, or dependencies on another checkout. Search projections and indexes belong in the same database.
- Do not commit third-party binaries.
- Offline commands and J2534 must not require Python, Panda or an openpilot checkout. Panda imports stay lazy; managed pandad safety/ownership checks belong at the raw-CAN boundary.

## Development

- Run `go test ./...`; build with `go build -o build/toyo ./cmd/toyo`. Transport adapter tests use fake upstream modules and skip if Python is unavailable.
- Direct Panda uses an existing Python environment with upstream Panda installed. `TOYO_PYTHON` (alias `TOYOTA_PYTHON`) selects it.
- Comma/AGNOS reuses `/usr/local/venv` and `/data/openpilot`; do not create a second diagnostics virtualenv.
- Prefer targeted tests while iterating, then run the full suite for shared runtime changes. Hardware behavior must be verified separately from fake transport tests.

## Catalog updates

The database holds the facts the CLI uses: decoder parameters, requests and sessions, safety and refusal conditions, vehicle routing and topology, descriptions, units, ranges and choices. Keep unsupported and ambiguous states rather than only executable ones. Keep unknown field bytes, including zeros and presence, until their meaning is established.

Store each fact once and reference it from documents and search. `--json` exposes complete diagnostic records. Do not drop a field because of its name (`raw`, `offset`, `source`, `plugin_binding`); check what uses it.

`item_occurrences` owns search source locations and refers to shared `search_records`/text; `items` is the view the full-text index reads. Search entries point into source documents rather than storing payload copies. Owner ranges/counts and ECU query projections are derived indexes. A storage-only change must preserve occurrences and ranking. Test relationship checks, search behavior, cold/hot inspection and bounded-memory runtime use. Never substitute a partial development database.
