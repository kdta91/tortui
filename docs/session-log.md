# Session log

One line per task: date, task ID, outcome. This is what a human reads to catch up
(AGENT.md §11). Earlier tasks predate this file; their outcomes are in each task's
`notes:` block in `TASK_TRACKER.md` and in the Decision Log.

| Date | Task | Outcome |
|---|---|---|
| 2026-09-17 | T-031 | **BLOCKED** before any code. `github.com/anacrolix/torrent v1.61.0` pulls seven *further* MPL-2.0 modules (`anacrolix/dht/v2`, `generics`, `log`, `multiless`, `sync`, `upnp`, `utp`), which `scripts/check-license-scope.sh` rejects because DEC-098 admitted MPL-2.0 for `anacrolix/torrent` alone; and `github.com/go-llsqlite/adapter` carries no license file at all, which `go-licenses check` rejects on `GOOS=darwin`. No GPL/AGPL anywhere in the tree. Needs an owner decision on both points — see the Blocked section. `go.mod` unchanged. |
