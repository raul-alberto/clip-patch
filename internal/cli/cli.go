// Package cli wires together clipboard input, parsing, planning, preview,
// confirmation and application.
package cli

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"clip-patch/internal/apply"
	"clip-patch/internal/clipboard"
	"clip-patch/internal/parser"
	"clip-patch/internal/patch"
	"clip-patch/internal/preview"
)

// Exit codes.
const (
	ExitOK    = 0
	ExitError = 1 // patch rejected, apply failed, or user declined
	ExitUsage = 2 // invalid command line
)

// MaxInputSize is the largest patch text accepted (clipboard or stdin).
const MaxInputSize = 8 << 20

// Env holds everything Run needs from the outside world, so tests can
// replace it.
type Env struct {
	Name             string
	Version          string
	Stdin            io.Reader
	Stdout, Stderr   io.Writer
	Clipboard        clipboard.Reader
	Interactive      func() (io.ReadCloser, error) // opens a terminal for the y/N prompt
	Getwd            func() (string, error)
	DefaultBackupDir func() (string, error)
	ColorAuto        bool // colour decision for --color=auto
	Ops              apply.Ops
}

type options struct {
	help, version, dryRun, yes, useStdin bool
	format, root, backupDir, color       string
}

func (e Env) name() string {
	if e.Name == "" {
		return "clip-patch"
	}
	return e.Name
}

func usage(name string) string {
	return fmt.Sprintf(`%[1]s - safely apply code patches from your clipboard

USAGE:
    %[1]s [OPTIONS]

OPTIONS:
    -h, --help          Show this help
        --version       Display version
        --dry-run       Preview without modifying files
        --yes           Apply without interactive confirmation
                        (all safety checks and backups still run)
        --format FORMAT Patch format: auto (default), search-replace, unified-diff
        --root PATH     Project root; patches may not touch files outside it
                        (default: current directory)
        --backup-dir PATH
                        Where to store backups of files before they are changed
                        (default: per-user application data directory)
        --stdin         Read the patch from standard input instead of the clipboard
        --color WHEN    auto (default), always or never

EXAMPLES:
    %[1]s                         Read the clipboard, preview, ask, apply
    %[1]s --dry-run               Preview only
    cat patch.diff | %[1]s --stdin --dry-run
    Get-Content patch.diff -Raw | %[1]s --stdin      (PowerShell)

Nothing is sent over the network. Files are never modified without a backup.
`, name)
}

func parseArgs(args []string) (options, error) {
	var o options
	fs := flag.NewFlagSet("clip-patch", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.BoolVar(&o.help, "help", false, "")
	fs.BoolVar(&o.help, "h", false, "")
	fs.BoolVar(&o.version, "version", false, "")
	fs.BoolVar(&o.dryRun, "dry-run", false, "")
	fs.BoolVar(&o.yes, "yes", false, "")
	fs.BoolVar(&o.useStdin, "stdin", false, "")
	fs.StringVar(&o.format, "format", "auto", "")
	fs.StringVar(&o.root, "root", "", "")
	fs.StringVar(&o.backupDir, "backup-dir", "", "")
	fs.StringVar(&o.color, "color", "auto", "")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			o.help = true
			return o, nil
		}
		return o, err
	}
	if fs.NArg() > 0 {
		return o, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	switch o.color {
	case "auto", "always", "never":
	default:
		return o, fmt.Errorf("invalid --color value %q (expected auto, always or never)", o.color)
	}
	if o.dryRun && o.yes {
		return o, errors.New("--dry-run and --yes cannot be combined")
	}
	return o, nil
}

// Run executes the program and returns the process exit code.
func Run(args []string, env Env) int {
	if env.Getwd == nil {
		env.Getwd = os.Getwd
	}
	if env.DefaultBackupDir == nil {
		env.DefaultBackupDir = apply.DefaultBackupDir
	}
	out, errw := env.Stdout, env.Stderr
	name := env.name()

	opts, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(errw, "%s: %v\nTry '%s --help' for usage.\n", name, err, name)
		return ExitUsage
	}
	if opts.help {
		fmt.Fprint(out, usage(name))
		return ExitOK
	}
	if opts.version {
		fmt.Fprintf(out, "%s %s\n", name, env.Version)
		return ExitOK
	}
	format, err := patch.ParseFormat(opts.format)
	if err != nil {
		fmt.Fprintf(errw, "%s: %v\nTry '%s --help' for usage.\n", name, err, name)
		return ExitUsage
	}
	color := env.ColorAuto
	switch opts.color {
	case "always":
		color = true
	case "never":
		color = false
	}

	// 1. Read input.
	var input string
	if opts.useStdin {
		data, err := io.ReadAll(io.LimitReader(env.Stdin, MaxInputSize+1))
		if err != nil {
			printError(errw, fmt.Errorf("cannot read standard input: %w", err), noteNoFiles)
			return ExitError
		}
		if len(data) > MaxInputSize {
			printError(errw, fmt.Errorf("the patch on standard input is larger than %d MiB", MaxInputSize>>20), noteNoFiles)
			return ExitError
		}
		input = string(data)
	} else {
		if env.Clipboard == nil {
			printError(errw, errors.New("no clipboard reader is configured"), noteNoFiles)
			return ExitError
		}
		text, err := env.Clipboard.ReadText()
		if err != nil {
			printError(errw, fmt.Errorf("could not read the clipboard: %w", err), noteNoFiles)
			fmt.Fprintf(errw, "\nTip: pass the patch on standard input instead:\n    cat patch.diff | %s --stdin\n", name)
			return ExitError
		}
		if len(text) > MaxInputSize {
			printError(errw, fmt.Errorf("the clipboard content is larger than %d MiB", MaxInputSize>>20), noteNoFiles)
			return ExitError
		}
		input = text
	}
	if strings.TrimSpace(input) == "" {
		if opts.useStdin {
			printError(errw, &patch.Error{Title: "Standard input is empty.", Reason: "There is no patch to apply."}, noteNoFiles)
		} else {
			printError(errw, &patch.Error{Title: "The clipboard is empty.", Reason: "Copy a patch first, then run the command again."}, noteNoFiles)
		}
		return ExitError
	}

	// 2. Parse.
	p, err := parser.Parse(input, format)
	if err != nil {
		printError(errw, err, noteNoFiles)
		return ExitError
	}

	// 3. Validate and compute everything in memory.
	root := opts.root
	if root == "" {
		root, err = env.Getwd()
		if err != nil {
			printError(errw, fmt.Errorf("cannot determine the current directory: %w", err), noteNoFiles)
			return ExitError
		}
	}
	plan, err := apply.BuildPlan(root, p)
	if err != nil {
		printError(errw, err, noteNoFiles)
		return ExitError
	}

	// 4. Preview.
	preview.Render(out, env.Version, plan, color)

	if len(plan.ChangedFiles()) == 0 {
		fmt.Fprintln(out, "\nThe patch is valid but produces no changes. Nothing to do.")
		return ExitOK
	}
	if opts.dryRun {
		fmt.Fprintln(out, "\nDry run: no files were modified.")
		return ExitOK
	}

	// 5. Confirm.
	if !opts.yes {
		ok, cerr := confirm(env, out)
		if cerr != nil {
			printError(errw, fmt.Errorf("cannot ask for confirmation: %w", cerr), noteNoFiles)
			fmt.Fprintf(errw, "\nRe-run with --dry-run to preview only, or --yes to apply without a prompt.\n")
			return ExitError
		}
		if !ok {
			fmt.Fprintln(out, "\nCancelled. No files were modified.")
			return ExitError
		}
	}

	// 6. Apply.
	backupRoot := opts.backupDir
	if backupRoot == "" {
		backupRoot, err = env.DefaultBackupDir()
		if err != nil {
			printError(errw, err, noteNoFiles)
			return ExitError
		}
	}
	backupRoot, err = filepath.Abs(backupRoot)
	if err != nil {
		printError(errw, fmt.Errorf("invalid backup directory: %w", err), noteNoFiles)
		return ExitError
	}
	res, err := apply.Execute(plan, backupRoot, env.Ops)
	if err != nil {
		var xe *apply.ExecError
		if errors.As(err, &xe) {
			switch {
			case xe.Modified:
				printError(errw, xe.Err, "WARNING: some files may have been modified and could not be restored automatically.")
			case xe.RolledBack:
				printError(errw, xe.Err, "The change was rolled back. All files were restored to their original content.")
			default:
				printError(errw, xe.Err, noteNoFiles)
			}
			if xe.BackupDir != "" {
				fmt.Fprintf(errw, "\nBackup of the original files: %s\n", xe.BackupDir)
			}
		} else {
			printError(errw, err, "Some files may have been modified.")
		}
		return ExitError
	}

	fmt.Fprintf(out, "\nApplied changes to %d file(s):\n", len(res.Written))
	for _, w := range res.Written {
		fmt.Fprintf(out, "  [MODIFIED] %s\n", w)
	}
	fmt.Fprintf(out, "Backup of the originals: %s\n", res.BackupDir)
	return ExitOK
}

func confirm(env Env, out io.Writer) (bool, error) {
	if env.Interactive == nil {
		return false, errors.New("no interactive terminal is available")
	}
	rc, err := env.Interactive()
	if err != nil {
		return false, err
	}
	defer rc.Close()
	fmt.Fprint(out, "\nApply these changes? [y/N] ")
	line, err := bufio.NewReader(rc).ReadString('\n')
	if err != nil && line == "" {
		// EOF or read error without an answer: treat as "no".
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	}
	return false, nil
}

const noteNoFiles = "No files were modified."

// printError renders an error in the documented format. note is the final
// statement about the state of the files.
func printError(w io.Writer, err error, note string) {
	var pe *patch.Error
	if errors.As(err, &pe) {
		fmt.Fprintf(w, "ERROR: %s\n", pe.Title)
		if pe.File != "" || pe.Reason != "" {
			fmt.Fprintln(w)
		}
		if pe.File != "" {
			fmt.Fprintf(w, "File: %s\n", pe.File)
		}
		if pe.Reason != "" {
			fmt.Fprintf(w, "Reason: %s\n", pe.Reason)
		}
		fmt.Fprintf(w, "\n%s\n", note)
		if len(pe.Causes) > 0 {
			fmt.Fprintf(w, "\nPossible causes:\n")
			for _, c := range pe.Causes {
				fmt.Fprintf(w, "- %s\n", c)
			}
		}
		return
	}
	fmt.Fprintf(w, "ERROR: %s\n\n%s\n", capitalize(err.Error()), note)
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
