# Running and verifying a build

Moved **verbatim** from AGENT.md §15 by T-945 so the operating contract stays short.
AGENT.md keeps the binding summary; this file is the full text and is equally binding.
Read it whenever a task touches this area.

## 15. Running and verifying a build

### The four ways to launch

```sh
make build                      # → bin/tortui

./bin/tortui --version          # 1. does it link and run at all
./bin/tortui doctor             # 2. environment report, no TUI, safe to pipe
./bin/tortui --demo             # 3. full UI, fake engine + fake indexer, zero network
make run                        # 4. real engine against ./dev-config.toml sandbox
```

**`doctor`** prints and exits: OS/arch, `TERM`, `COLORTERM`, detected colour profile, Unicode
capability, terminal size, resolved config/state/download paths, file-descriptor limit, and
each configured indexer with a reachability verdict. This is the first thing to run when
something looks wrong, and the first thing to ask a user for in a bug report.

**`--demo`** wires `engine/fake` and a fixture-backed indexer into the real TUI. Search returns
canned results, downloads progress on a simulated clock and include a stall, an error, and a
completion. Every screen, keybind, and dialog is reachable. No network, no disk writes outside
a temp dir, nothing to clean up. **This is the primary way to eyeball the UI after a build** and
the only way an agent can meaningfully self-verify rendering.

**`TORTUI_HOME`** redirects config, state, and downloads under one directory. Use it for any
manual testing so real config is never touched:

```sh
TORTUI_HOME=$(mktemp -d) ./bin/tortui
```

### Manual smoke test after a real build

```sh
make build
./bin/tortui doctor                                   # sanity
./bin/tortui --demo                                   # walk every screen
TORTUI_HOME=./tmp/smoke ./bin/tortui                  # first-run flow, add a source
```

Then one real download against a freely and officially distributed torrent — a current Debian
netinst ISO is the canonical target: small, always well-seeded, and unambiguously legal. Verify
progress advances, `o` opens the file, `f` reveals the folder, `p` pauses and resumes, restart
resumes from existing data, and `x` removes with and without data.

### Terminal matrix pass (before any release)

Run `--demo` in each and confirm alignment, colour, and clean exit:

```sh
./bin/tortui --demo                     # Terminal.app  ← the floor
./bin/tortui --demo                     # iTerm2 or Ghostty
tmux new-session ./bin/tortui --demo    # inside tmux
NO_COLOR=1 ./bin/tortui --demo          # monochrome
TERM=xterm ./bin/tortui --demo          # 16-colour
./bin/tortui --ascii --demo             # glyph fallback
printf '' | ./bin/tortui                # non-TTY → clean refusal, exit 1
```

Resize each to 80×24 and to ~60 columns while running; columns must drop in the documented
order without garbling.

Shell-agnosticism is confirmed once, not per feature:

```sh
zsh  -lc './bin/tortui --version'
bash -lc './bin/tortui --version'
sh   -c  './bin/tortui --version'
```

### Automated

```sh
make check                # fmt + lint + vet + unit tests — the commit gate
go test -race ./...       # concurrency
make cover                # thresholds from §9
make test-integration     # build-tagged, real network, manual
```

TUI rendering is covered by `teatest` golden files driven by `engine/fake`, at 80×24, 120×40,
and 60×20. A golden-file diff is a real failure — inspect the diff, do not regenerate blindly.
