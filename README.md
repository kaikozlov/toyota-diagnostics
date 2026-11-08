# toyo-cli

Independent open-source diagnostics for Toyota vehicles. Not affiliated with
or endorsed by Toyota Motor Corporation.

`toyo` reads a recovered Techstream/GTS+ catalog: ECUs, Data List signals,
trouble codes, Active Tests, vehicles, and related records. This repository
does not ship Toyota DLL, DDB, or EXE files, and it does not ship the SQLite
catalog. You generate that catalog from the sibling `ghidra_rh850_analysis`
tree.

Offline catalog work is local. It needs no car, no Panda, and no Python. Live
commands use the same CLI when you want to talk to a vehicle; those are
optional and need extra setup plus hardware.

## Run it

You need Go. From this checkout, `./toyo` builds `build/toyo` the first time
you run it.

Put the generated catalog at `build/catalog.sqlite` (ignored, not source).
From `ghidra_rh850_analysis` next to this tree:

```bash
tools/gts bundle sqlite --out ../toyota-diagnostics/build/catalog.sqlite
```

If the catalog lives somewhere else, pass `--db PATH` or set `TOYO_CATALOG`.
`TOYO_REGION` selects `NA`, `EU`, or `JP`. The older `TOYOTA_*` names for
these settings still work.

`toyo help` works without a catalog.

```bash
./toyo help
./toyo                 # interactive explorer
./toyo search LTA
./toyo frc
./toyo frc data steer
./toyo vehicle
```

Those examples match `toyo help`. `toyo help COMMAND` has the rest of the
commands and their flags. Search rows include a `#number` you can open with
`toyo inspect`; those numbers can change when the catalog is rebuilt, so
prefer `toyo ECU IDENT` in anything you keep.

In the explorer, type to search, press enter to open an ECU, and press esc to
step back. Arrow keys, tab, or a click choose a kind filter.

`toyo completion zsh` (or `bash`, or `fish`) installs tab completion.

## Live hardware

Commands listed under Live in `toyo help` — monitor, scan, DTC, Active Tests,
and the rest — need a vehicle link. They are not required for browsing the
catalog. On a workstation, `uv sync --extra live` adds direct Panda support.

Looking at plans and catalogs does not talk to the car. Commands that change
vehicle state require an explicit `--execute`.

Transport options, recovered GTS+ coverage, and the longer live command set
are in `toyo help COMMAND` and [`docs/GTS_PARITY.md`](docs/GTS_PARITY.md).

## Development

See [`AGENTS.md`](AGENTS.md) for repository layout and how to run
`go test ./...` and `uv run pytest`.
