# Platforms and terminals

Moved **verbatim** from AGENT.md §14 by T-945 so the operating contract stays short.
AGENT.md keeps the binding summary; this file is the full text and is equally binding.
Read it whenever a task touches this area.

## 14. Cross-platform targets and terminal compatibility

### Support matrix

| OS | Arch | Tier | Meaning |
|---|---|---|---|
| macOS 13+ | arm64, amd64 | 1 | Primary development target. Verified by hand before release. |
| Linux | amd64, arm64 | 1 | Main CI target. Full test suite runs here. |
| Windows 10+ | amd64, arm64 | 1 | CI target. Verified by hand in Windows Terminal before release. |

All three are first-class. A feature that works on two of them is not done. Every task with
OS-visible behaviour needs its Windows path implemented and tested in the same task — not
deferred, not stubbed.

### Shell versus terminal — do not conflate these

The binary is **shell-agnostic**. `zsh`, `bash`, `fish`, `sh`, and `nushell` all launch it the
same way: it reads `argv` and the environment, takes over the TTY, and hands it back on exit.
Nothing in the application may depend on shell features, aliases, functions, or rc files.
There is no per-shell code path and no task should create one.

What actually varies between environments is the **terminal emulator**, and that is where all
compatibility effort goes.

Shell choice *does* matter in exactly four places:

1. **Makefile recipes.** Set `SHELL := /bin/sh` and `.SHELLFLAGS := -eu -c`. macOS ships GNU
   make 3.81, so no `.ONESHELL` (3.82+) and no `$(file ...)` (4.0+).
2. **Scripts and git hooks.** `#!/usr/bin/env sh`, POSIX only. macOS ships **bash 3.2** from
   2007 — no `declare -A`, no `mapfile`, no `${var^^}`, no `&>>`.
3. **Shell completions.** Generate for zsh, bash, and fish from the flag definitions so they
   cannot drift.
4. **Install instructions.** Cover zsh and bash `PATH` setup separately in the README.

### Terminal compatibility requirements

Must render correctly in: Terminal.app, iTerm2, Ghostty, WezTerm, Alacritty, kitty, tmux,
GNU screen, Windows Terminal, the VS Code integrated terminal, and over SSH.

- **Terminal.app is the floor.** `xterm-256color`, no truecolor, limited glyph coverage. If it
  looks wrong there it is wrong, regardless of how it looks in Ghostty.
- Colour comes from the lipgloss/termenv profile. Never emit a hardcoded escape sequence.
  Honour `NO_COLOR` and `CLICOLOR_FORCE`.
- `TERM=dumb` or a non-TTY stdout → do not start the TUI. Print a one-line explanation and
  exit 1. Piping the binary must not produce escape-sequence garbage.
- **Glyph fallback.** Block characters `█░` and the badge `✓` need an ASCII fallback set
  (`#`, `-`, `+`) chosen by capability detection, forceable with `--ascii` and
  `ascii = true` in config.
- **Width measurement uses `rivo/uniseg`.** Torrent titles contain CJK, emoji, and combining
  marks. `len()` and `utf8.RuneCountInString` both produce misaligned tables.
- Under tmux, `TERM=screen-256color` masks truecolor unless the user enables `Tc`. Detect,
  degrade silently, do not nag.
- Enter the alternate screen and enable bracketed paste on start; restore terminal state on
  **every** exit path including `SIGINT`, `SIGTERM`, and panic.

### Per-OS behaviour

**macOS**
- Config at `~/.config/tortui/` (honouring `XDG_CONFIG_HOME` when set), *not*
  `~/Library/Application Support` — see DEC-005.
- Default download dir `~/Downloads/tortui`.
- Open file: `open <path>`. Reveal in Finder: `open -R <path>`.
- Raise the soft file-descriptor limit to the hard limit at startup.
- First bind triggers the macOS firewall prompt — document this in the README so it does not
  read as a hang.
- Downloaded release binaries are quarantined by Gatekeeper; README documents
  `xattr -d com.apple.quarantine ./tortui` until notarisation is in place.

**Linux**
- Native XDG paths. Open file and folder: `xdg-open`.
- Default download dir `~/Downloads/tortui`, falling back to `$XDG_DOWNLOAD_DIR`.

**Windows**
- Config under `%AppData%\tortui\`. Open file: `explorer <path>`. Reveal:
  `explorer /select,<path>`.
- Enable virtual terminal processing at startup. Detect legacy conhost and print a message
  recommending Windows Terminal rather than rendering badly.
- All paths via `path/filepath`. Never concatenate with `/`.

**Invariant:** every OS-specific line lives in `internal/platform` behind `_darwin.go`,
`_linux.go`, `_windows.go` build tags. A `runtime.GOOS` switch anywhere else fails review.
