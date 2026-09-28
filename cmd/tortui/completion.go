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

// globalFlags holds the *bool/*string cells flag.FlagSet.Parse fills in for
// tortui's top-level flags. run() reads these after Parse instead of
// defining its own second set of fs.Bool/fs.String calls, so this struct
// (built by globalFlagSet below) is the *only* place a global flag is
// declared. Completion generation walks the same FlagSet, so the two can
// never drift the way a hand-duplicated list could (see
// TestRunUsesGlobalFlagSet, which fails if run() ever stops calling
// globalFlagSet).
type globalFlags struct {
	version  *bool
	demo     *bool
	ascii    *bool
	config   *string
	logLevel *string
	logFile  *string
}

// globalFlagSet builds tortui's top-level flag.FlagSet and returns it
// alongside pointers to every flag's value. Both run() (main.go) and
// runCompletion (this file) call this one constructor — run() to parse argv
// and read the results, runCompletion only to enumerate flag names — so a
// flag added, renamed, or removed here is automatically reflected in both
// argument parsing and every generated completion script (T-092 acceptance:
// completions "generated from the flag set").
func globalFlagSet(out *os.File) (*flag.FlagSet, *globalFlags) {
	fs := flag.NewFlagSet("tortui", flag.ContinueOnError)
	fs.SetOutput(out)

	g := &globalFlags{}
	g.version = fs.Bool("version", false, "print version information and exit")
	g.demo = fs.Bool("demo", false, "run the TUI against a fake engine and fixture indexer — zero network, zero writes outside a temp dir (AGENT.md §15)")
	g.ascii = fs.Bool("ascii", false, "use ASCII glyphs instead of Unicode block characters (same as ascii = true in config.toml)")
	g.config = fs.String("config", "", "path to config.toml (overrides the default XDG location)")
	g.logLevel = fs.String("log-level", "", "override the configured log level (debug, info, warn, error)")
	g.logFile = fs.String("log-file", "", "override the configured log file path")

	return fs, g
}

// doctorFlags is globalFlags' counterpart for `tortui doctor` (runDoctor in
// doctor.go), built by doctorFlagSet below for the same single-source-of-truth
// reason.
type doctorFlags struct {
	config *string
}

// doctorFlagSet mirrors globalFlagSet for runDoctor's flag set.
func doctorFlagSet(out *os.File) (*flag.FlagSet, *doctorFlags) {
	fs := flag.NewFlagSet("tortui doctor", flag.ContinueOnError)
	fs.SetOutput(out)

	d := &doctorFlags{}
	d.config = fs.String("config", "", "path to config.toml (overrides the default XDG location)")

	return fs, d
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
// the shell name — the flag/subcommand lists it walks come from the very
// same globalFlagSet/doctorFlagSet constructors run() and runDoctor() parse
// argv with, so a future flag needs no matching edit here (T-092).
func runCompletion(args []string, out *os.File) int {
	if len(args) != 1 {
		if _, err := fmt.Fprintf(out, "tortui completion: expected exactly one shell argument (%s)\n", strings.Join(completionShells, ", ")); err != nil {
			return 1
		}

		return 2
	}

	shell := args[0]

	globalFS, _ := globalFlagSet(out)
	doctorFS, _ := doctorFlagSet(out)
	global := flagNames(globalFS)
	doctor := flagNames(doctorFS)

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
