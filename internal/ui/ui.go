// Package ui is the only place allowed to write to the terminal. Everything
// else routes through it, so the MCP server cannot accidentally put a byte on
// stdout and break the JSON-RPC stream.
package ui

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// ErrNoInput means a question was asked where nobody can answer it.
var ErrNoInput = errors.New("no interactive terminal")

// Prompter asks the user a question.
type Prompter interface {
	// Confirm asks a yes/no question. def is the answer for a bare Enter.
	Confirm(question string, def bool) (bool, error)
	// Choose asks for one of several options, each identified by its key.
	Choose(question string, options []Option) (string, error)
	// Interactive reports whether asking anything is possible at all.
	Interactive() bool
}

// Option is one answer of a Choose question.
type Option struct {
	Key   string // the letter the user types
	Label string
}

// UI writes output and, on a terminal, asks questions.
type UI struct {
	out         io.Writer
	err         io.Writer
	in          *bufio.Reader
	interactive bool
	// errTTY is asked separately because interactive says nothing about
	// stderr: `autogit commit 2>run.log` on a live terminal would otherwise
	// write escape sequences into the log file.
	errTTY bool
}

// New builds a UI over the process streams. interactive is forced off when
// either side of the conversation is not a terminal.
func New(out, errw io.Writer, in io.Reader, interactive bool) *UI {
	return &UI{
		out:         out,
		err:         errw,
		in:          bufio.NewReader(in),
		interactive: interactive,
		errTTY:      interactive,
	}
}

// Std builds the UI for a CLI run.
func Std() *UI {
	u := New(os.Stdout, os.Stderr, os.Stdin, IsTerminal(os.Stdout) && IsTerminal(os.Stdin))
	u.errTTY = IsTerminal(os.Stderr)
	return u
}

// IsTerminal reports whether f is a character device.
func IsTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func (u *UI) Interactive() bool { return u.interactive }

// Print writes a line to stdout.
func (u *UI) Print(format string, a ...any) {
	_, _ = fmt.Fprintf(u.out, format+"\n", a...)
}

// Raw writes to stdout without a trailing newline.
func (u *UI) Raw(s string) { _, _ = fmt.Fprint(u.out, s) }

// Warn writes a line to stderr.
func (u *UI) Warn(format string, a ...any) {
	_, _ = fmt.Fprintf(u.err, format+"\n", a...)
}

func (u *UI) Confirm(question string, def bool) (bool, error) {
	if !u.interactive {
		return false, ErrNoInput
	}
	hint := "[y/N]"
	if def {
		hint = "[Y/n]"
	}
	for {
		_, _ = fmt.Fprintf(u.err, "%s %s ", question, hint)
		line, err := u.in.ReadString('\n')
		if err != nil && line == "" {
			return false, ErrNoInput
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "":
			return def, nil
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		}
	}
}

func (u *UI) Choose(question string, options []Option) (string, error) {
	if !u.interactive {
		return "", ErrNoInput
	}
	var keys []string
	for _, o := range options {
		keys = append(keys, o.Key)
	}
	for {
		_, _ = fmt.Fprintln(u.err, question)
		for _, o := range options {
			_, _ = fmt.Fprintf(u.err, "  %s) %s\n", o.Key, o.Label)
		}
		_, _ = fmt.Fprintf(u.err, "[%s] ", strings.Join(keys, "/"))

		line, err := u.in.ReadString('\n')
		if err != nil && line == "" {
			return "", ErrNoInput
		}
		answer := strings.ToLower(strings.TrimSpace(line))
		for _, o := range options {
			if answer == o.Key {
				return o.Key, nil
			}
		}
	}
}

// Noop answers nothing. It is what mcp, hook and --no-input use: there is no
// user attached, so a question there is an error, not a pause.
type Noop struct{}

func (Noop) Confirm(string, bool) (bool, error)      { return false, ErrNoInput }
func (Noop) Choose(string, []Option) (string, error) { return "", ErrNoInput }
func (Noop) Interactive() bool                       { return false }
func (Noop) Start(string) func()                     { return func() {} }
