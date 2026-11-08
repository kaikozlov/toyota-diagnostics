# toyo-cli

Independent open-source diagnostics for Toyota vehicles. Not affiliated with
or endorsed by Toyota Motor Corporation.

`toyo` reads a recovered Techstream/GTS+ catalog: ECUs, Data List signals,
trouble codes, Active Tests, vehicles, and related records. This repository
does not ship Toyota DLL, DDB, or EXE files, and it does not ship the SQLite
catalog. You generate that catalog from the sibling `ghidra_rh850_analysis`
tree.

Offline catalog work is local. It needs no car, no Panda, and no Python.
Catalog browsing, decoding, planning, and live diagnostics all run in Go
over the same SQLite catalog. Python is used only by the private raw-CAN
Panda adapter; J2534 is native Go.

## Run it

You need Go 1.27.3. From this checkout, `./toyo` builds `build/toyo` the first
time you run it.

Put the generated catalog at `build/catalog.sqlite` (ignored, not source).
From `ghidra_rh850_analysis` next to this tree:

```bash
tools/gts bundle sqlite --out ../toyota-diagnostics/build/catalog.sqlite
```

If the catalog lives somewhere else, pass `--db PATH` or set `TOYO_CATALOG`.
SQLite-only live use has explicit `--vehicle`, exported request addresses,
inferred RX (`TX+8`), and no session/support tables. A matching `--bundle`
or `TOYO_BUNDLE` (same schema and release) overlays those lost fields,
including recovered response/legislated addresses on matching route keys.
Unmatched bundles are rejected. `TOYO_REGION` selects `NA`, `EU`, or `JP`.
The older `TOYOTA_*` names for these settings still work.

`toyo help` works without a catalog.

```bash
./toyo help
./toyo                 # interactive explorer
./toyo search LTA
./toyo frc
./toyo frc data steer
./toyo vehicle
```

`toyo help COMMAND` has the rest of the commands and their flags. Search rows
include a `#number` you can open with `toyo inspect`; those numbers can change
when the catalog is rebuilt, so prefer `toyo ECU IDENT` in anything you keep.

In the explorer, up and down move the list, enter opens an ECU or record, and
esc steps back. Tab or a click on a chip chooses a kind filter. Left and right
change the filter only when the search box is empty.

`toyo completion zsh` (or `bash`, or `fish`) prints a completion script; see
`toyo help completion`.

## Live hardware

Commands listed under Live in `toyo help` — monitor, scan, DTC, Active Tests,
and the rest — need a vehicle link. They are not required for browsing the
catalog. Offline commands and J2534 need no Python. For Panda, point
`TOYO_PYTHON` (or the older `TOYOTA_PYTHON`) at an environment that already
has upstream Panda. On a comma, that adapter reuses `/usr/local/venv` and
`/data/openpilot`; do not create another diagnostics virtualenv.

Catalogs and plans stay offline. Live commands can transmit or write to the
vehicle. `toyo dtc clear` clears codes when you run it. Active Tests,
Customize, and utilities are a dry run unless you pass `--execute`. Mutating
raw requests need `--force`.

Transport options, recovered GTS+ coverage, and the longer live command set
are in `toyo help COMMAND` and [`docs/GTS_PARITY.md`](docs/GTS_PARITY.md).

## Development

See [`AGENTS.md`](AGENTS.md) for repository layout and how to run
`go test ./...`.
