# toyo-cli

Open-source diagnostics for Toyota vehicles. Not affiliated with or endorsed by Toyota Motor Corporation.

`toyo` uses a catalog of ECUs, Data List signals, trouble codes, Active Tests, vehicles, and related records.

## Run it

You need Go 1.27.3. From this checkout, `./toyo` builds `build/toyo` the first
time you run it. The database ships through Git LFS; run `git lfs pull` if
you cloned without downloading LFS files. `--db PATH` or `TOYO_CATALOG` picks
another database, and `TOYO_REGION` picks `NA`, `EU`, or `JP`.

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
`TOYO_PYTHON` (or `TOYOTA_PYTHON`) at an environment that already
has upstream Panda. On a comma, that adapter reuses `/usr/local/venv` and
`/data/openpilot`; do not create another diagnostics virtualenv.

Catalogs and plans stay offline. Live commands can transmit or write to the
vehicle. `toyo dtc clear` clears codes when you run it. Active Tests,
Customize, and utilities are a dry run unless you pass `--execute`. Mutating
raw requests need `--force`.

Transport options and the longer live command set are in `toyo help COMMAND`.

## Development

See [`AGENTS.md`](AGENTS.md) for repository layout and how to run
`go test ./...`.
