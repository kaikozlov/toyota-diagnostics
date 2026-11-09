This repo is toyo-cli, a diagnostics CLI and TUI for Toyota vehicles. The executable is `toyo`.

The project is self-contained. Everything `toyo` needs lives in this repo, and the code and docs stand on their own.

We're building a tool for people working on their own cars. Every change should make it simpler, clearer, and nicer to use. First use should be obvious, and output should read at a glance. Nothing is specific to one car; search and defaults work across models and generations.

Keep it tidy. There is one implementation of each thing, and it's the current one. No backwards compatibility, no versioning, no fallback paths, nothing "legacy". Every shim we keep is something the next change has to work around.

## Layout

- Everything is Go under `cmd/toyo/` and `internal/`.
- `catalog/catalog.sqlite` (Git LFS) is the only diagnostic data, and commands read it directly. Don't add other catalogs or copies.
- The only Python is the raw-CAN Panda adapter in `internal/transport/panda_adapter.py`. It moves frames and nothing else. Offline commands and J2534 never need Python.

## Sharp edges

- Live commands talk to a real car. Keep the existing gates (`--execute`, `--force`) and don't loosen them. Don't add guards, timers, or retries "just to be safe" either.
- Fake-transport tests don't tell us how the car behaves. Hardware behavior is verified on hardware.
- On a comma device, reuse `/usr/local/venv` and `/data/openpilot`; don't create another virtualenv.

## Building things

Look for what already owns a thing before adding something new, and reuse it. Do what was asked, and when you replace something, migrate its callers and delete the old path. Git keeps the history.

Run `go test ./...` before you push, then run `toyo` in a real terminal and look at it. A passing test doesn't mean it's good to use.

Tests are for things that can break: decoding, request building, safety gates, search ranking. Docs and wording need none.

## Writing it down

- The README is for users: what it is and how to run it.
- Don't write verbose documentation that buries the lede. State your point and be done with it. That applies to comments, help text, and commit messages too.
- Don't feign rigor with "shows x, not y" or "y is still not verified". Nobody asked about y. State x and stop.
- Don't write process notes, ledgers, or changelogs into the repo.
