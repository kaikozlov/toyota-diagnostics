# Diagnostic database

`catalog.sqlite` is the database `toyo` reads. It ships through Git LFS; if
it's a small text pointer, run `git lfs pull`.

SHA-256 `ff09ef63a171bf7c8386435bf9f83c4a7b84af28aa605733423244060eee1091`
(344,793,088 bytes).

Use `--db PATH` or `TOYO_CATALOG` to read a different file.

When you replace it, commit only the final file. Git LFS keeps every revision.
No Toyota DLL, DDB, or EXE files are included.
