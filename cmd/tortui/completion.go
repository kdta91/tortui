package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
)

// completionShells lists the shells `tortui completion` can target, in the
// order they are listed in `tortui completion --help`. Keep in sync with
// completionUsage below and with the archive-shipped names goreleaser packs
// (see Makefile's `completions` target and T-092's release notes).
var completionShells = []string{"bash", "zsh", "fish", "powershell"}

// globalFlagSet returns the same flag.FlagSet main() parses top-level flags
// with, minus actually parsing anything. Completion generation and any
// future `--help` output both walk this set rather than hand-maintaining a
// second list that can drift from the real flags (T-092 acceptance:
// completions "generated from the flag set").
func globalFlagSet(out *os.File) *flag.FlagSet {
	fs := flag.NewFlagSet("tortui", flag.ContinueOnError)
	fs.SetOutput(out)
	fs.Bool("version", false, "print version information and exit")
	fs.Bool("demo", false, "run the TUI against a fake engine and fixture indexer")
	fs.String("config", "", "path to config.toml (overrides the default XDG location)")
	fs.String("log-level", "", "override the configured log level (debug, info, warn, error)")
	fs.String("log-file", "", "override the configured log file path")

	return fs
}

// doctorFlagSet mirrors runDoctor's flag set for the same reason.
func doctorFlagSet(out *os.File) *flag.FlagSet {
	fs := flag.NewFlagSet("tortui doctor", flag.ContinueOnError)
	fs.SetOutput(out)
	fs.String("config", "", "path to config.toml (overrides the default XDG location)")

	return fs
}

// tortuiSubcommands lists the non-flag subcommands completions must offer
// alongside the global flags: `doctor` (internal/doctor) and `completion`
// (this file). Kept as a var, not inlined at each call site, so a future
// subcommand needs one edit here rather than one per shell generator.
var tortuiSubcommands = []string{"doctor", "completion"}

// flagNames returns every flag in fs as its double-dash CLI form, sorted,
// e.g. []string{"--config", "--demo", ...}. Shared by every shell generator
// below so the flag list itself has exactly one source of truth.
func flagNames(fs *flag.FlagSet) []string {
	var names []string
	fs.VisitAll(func(f *flag.Flag) {
		names = append(names, "--"+f.Name)
	})
	sort.Strings(names)

	return names
}

// runCompletion implements `tortui completion <shell>`: print a completion
// script for the requested shell to stdout. It takes no other input besides
// the shell name — the flag/subcommand lists it walks come from the same
// FlagSet constructors main() and runDoctor use, so a future flag needs no
// matching edit here (T-092).
func runCompletion(args []string, out *os.File) int {
	if len(args) != 1 {
		if _, err := fmt.Fprintf(out, "tortui completion: expected exactly one shell argument (%s)\n", strings.Join(completionShells, ", ")); err != nil {
			return 1
		}

		return 2
	}

	shell := args[0]

	global := flagNames(globalFlagSet(out))
	doctor := flagNames(doctorFlagSet(out))

	var script string
	switch shell {
	case "bash":
		script = bashCompletion(global, doctor)
	case "zsh":
		script = zshCompletion(global, doctor)
	case "fish":
		script = fishCompletion(global, doctor)
	case "powershell":
		script = powershellCompletion(global, doctor)
	default:
		if _, err := fmt.Fprintf(out, "tortui completion: unknown shell %q (want one of: %s)\n", shell, strings.Join(completionShells, ", ")); err != nil {
			return 1
		}

		return 2
	}

	if _, err := fmt.Fprint(out, script); err != nil {
		return 1
	}

	return 0
}

func bashCompletion(global, doctor []string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# tortui bash completion\n")
	fmt.Fprintf(&b, "# Install: source this file, or place it under bash-completion's\n")
	fmt.Fprintf(&b, "# completions directory (e.g. /etc/bash_completion.d/tortui or\n")
	fmt.Fprintf(&b, "# $(brew --prefix)/etc/bash_completion.d/tortui).\n")
	fmt.Fprintf(&b, "_tortui_completions() {\n")
	fmt.Fprintf(&b, "  local cur prev words\n")
	fmt.Fprintf(&b, "  cur=\"${COMP_WORDS[COMP_CWORD]}\"\n")
	fmt.Fprintf(&b, "  prev=\"${COMP_WORDS[COMP_CWORD-1]}\"\n")
	fmt.Fprintf(&b, "  if [ \"$prev\" = \"completion\" ]; then\n")
	fmt.Fprintf(&b, "    words=\"%s\"\n", strings.Join(completionShells, " "))
	fmt.Fprintf(&b, "  elif [ \"$COMP_CWORD\" -eq 1 ]; then\n")
	fmt.Fprintf(&b, "    words=\"%s %s\"\n", strings.Join(tortuiSubcommands, " "), strings.Join(global, " "))
	fmt.Fprintf(&b, "  else\n")
	fmt.Fprintf(&b, "    words=\"%s\"\n", strings.Join(doctor, " "))
	fmt.Fprintf(&b, "  fi\n")
	fmt.Fprintf(&b, "  COMPREPLY=($(compgen -W \"$words\" -- \"$cur\"))\n")
	fmt.Fprintf(&b, "}\n")
	fmt.Fprintf(&b, "complete -F _tortui_completions tortui\n")

	return b.String()
}

func zshCompletion(global, doctor []string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "#compdef tortui\n")
	fmt.Fprintf(&b, "# tortui zsh completion\n")
	fmt.Fprintf(&b, "# Install: place on your $fpath as _tortui (e.g. copy to a directory\n")
	fmt.Fprintf(&b, "# already on $fpath, then run 'compinit').\n")
	fmt.Fprintf(&b, "_tortui() {\n")
	fmt.Fprintf(&b, "  local -a subcommands globalflags doctorflags\n")
	fmt.Fprintf(&b, "  subcommands=(%s)\n", strings.Join(tortuiSubcommands, " "))
	fmt.Fprintf(&b, "  globalflags=(%s)\n", strings.Join(global, " "))
	fmt.Fprintf(&b, "  doctorflags=(%s)\n", strings.Join(doctor, " "))
	fmt.Fprintf(&b, "  if (( CURRENT == 2 )); then\n")
	fmt.Fprintf(&b, "    compadd -- $subcommands $globalflags\n")
	fmt.Fprintf(&b, "  elif [[ ${words[2]} == completion ]]; then\n")
	fmt.Fprintf(&b, "    compadd -- %s\n", strings.Join(completionShells, " "))
	fmt.Fprintf(&b, "  elif [[ ${words[2]} == doctor ]]; then\n")
	fmt.Fprintf(&b, "    compadd -- $doctorflags\n")
	fmt.Fprintf(&b, "  fi\n")
	fmt.Fprintf(&b, "}\n")
	fmt.Fprintf(&b, "_tortui\n")

	return b.String()
}

func fishCompletion(global, doctor []string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# tortui fish completion\n")
	fmt.Fprintf(&b, "# Install: save as ~/.config/fish/completions/tortui.fish\n")
	fmt.Fprintf(&b, "complete -c tortui -f\n")

	for _, sub := range tortuiSubcommands {
		fmt.Fprintf(&b, "complete -c tortui -n '__fish_use_subcommand' -a %s\n", sub)
	}

	for _, f := range global {
		fmt.Fprintf(&b, "complete -c tortui -n '__fish_use_subcommand' -l %s\n", strings.TrimPrefix(f, "--"))
	}

	for _, f := range doctor {
		fmt.Fprintf(&b, "complete -c tortui -n '__fish_seen_subcommand_from doctor' -l %s\n", strings.TrimPrefix(f, "--"))
	}

	for _, shell := range completionShells {
		fmt.Fprintf(&b, "complete -c tortui -n '__fish_seen_subcommand_from completion' -a %s\n", shell)
	}

	return b.String()
}

func powershellCompletion(global, doctor []string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# tortui PowerShell completion\n")
	fmt.Fprintf(&b, "# Install: add to your $PROFILE, e.g.:\n")
	fmt.Fprintf(&b, "#   tortui completion powershell | Out-File -Append $PROFILE\n")
	fmt.Fprintf(&b, "Register-ArgumentCompleter -Native -CommandName tortui -ScriptBlock {\n")
	fmt.Fprintf(&b, "    param($wordToComplete, $commandAst, $cursorPosition)\n")
	fmt.Fprintf(&b, "    $tokens = $commandAst.CommandElements | ForEach-Object { $_.ToString() }\n")
	fmt.Fprintf(&b, "    $candidates = @()\n")
	fmt.Fprintf(&b, "    if ($tokens.Count -le 2) {\n")
	fmt.Fprintf(&b, "        $candidates = @(%s)\n", quotedPSList(append(append([]string{}, tortuiSubcommands...), global...)))
	fmt.Fprintf(&b, "    } elseif ($tokens[1] -eq 'completion') {\n")
	fmt.Fprintf(&b, "        $candidates = @(%s)\n", quotedPSList(completionShells))
	fmt.Fprintf(&b, "    } elseif ($tokens[1] -eq 'doctor') {\n")
	fmt.Fprintf(&b, "        $candidates = @(%s)\n", quotedPSList(doctor))
	fmt.Fprintf(&b, "    }\n")
	fmt.Fprintf(&b, "    $candidates | Where-Object { $_ -like \"$wordToComplete*\" } | ForEach-Object {\n")
	fmt.Fprintf(&b, "        [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)\n")
	fmt.Fprintf(&b, "    }\n")
	fmt.Fprintf(&b, "}\n")

	return b.String()
}

// quotedPSList renders items as a PowerShell array literal's contents,
// e.g. []string{"a", "b"} -> "'a', 'b'".
func quotedPSList(items []string) string {
	quoted := make([]string, len(items))
	for i, item := range items {
		quoted[i] = "'" + item + "'"
	}

	return strings.Join(quoted, ", ")
}
