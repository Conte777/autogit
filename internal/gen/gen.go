// Package gen is the generation core: ask a model, check the answer, ask it to
// fix what is wrong, repeat. It knows nothing about git, transports or message
// formats — those arrive as a Provider and a Validator.
package gen

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Provider opens sessions with a model.
type Provider interface {
	Name() string
	Start(ctx context.Context, system string) (Session, error)
}

// Session is one dialogue. Send posts a turn and waits for the reply; a live
// process or a replayed history is the adapter's choice. Corrections must land
// in the same session: a fresh claude-cli process costs ~4.5s of setup.
type Session interface {
	Send(ctx context.Context, text string) (string, error)
	Close() error
}

// Validator canonicalises raw model output and lists what is wrong with it.
// The value comes back even when problems is non-empty, so the caller can show
// the user the last rejected candidate.
type Validator interface {
	Check(raw string) (value string, problems []string)
}

// Request is one generation job.
type Request struct {
	System    string
	Prompt    string // the first turn
	Validator Validator
	Attempts  int
	// Correction renders the follow-up turn. Nil uses DefaultCorrection.
	Correction func(problems []string) string
}

// Result is a value that passed validation.
type Result struct {
	Value    string
	Attempts int
}

// ProviderError means the transport failed. It is terminal by design: retrying
// a broken transport belongs in the adapter, not in this loop.
type ProviderError struct {
	Provider string
	Op       string
	Err      error
}

func (e *ProviderError) Error() string {
	return fmt.Sprintf("provider %s: %s: %v", e.Provider, e.Op, e.Err)
}

func (e *ProviderError) Unwrap() error { return e.Err }

// FailureError means the model never produced a valid answer.
type FailureError struct {
	Provider string
	Attempts int
	Last     string
	Problems []string
}

func (e *FailureError) Error() string {
	return fmt.Sprintf("no valid output after %d attempts: %s", e.Attempts, strings.Join(e.Problems, "; "))
}

// maxCandidate bounds how much of a rejected candidate an error report
// carries. A model that ignores the instruction answers with a page of prose,
// and the hook feeds its blocking message straight back into the conversation.
const maxCandidate = 400

// Explain renders an error together with the candidate it rejected last, ready
// for a surface that hands the whole text to a model.
func Explain(err error) string {
	if candidate := LastCandidate(err); candidate != "" {
		return err.Error() + "\n" + candidate
	}
	return err.Error()
}

// LastCandidate renders the labelled line for the candidate a failed
// generation rejected last, or "" for any other error. Without it a caller
// learns why the answer was wrong but never what the answer was.
func LastCandidate(err error) string {
	var failure *FailureError
	if !errors.As(err, &failure) || failure.Last == "" {
		return ""
	}
	last := []rune(failure.Last)
	if len(last) > maxCandidate {
		return "last candidate: " + string(last[:maxCandidate]) + "…"
	}
	return "last candidate: " + failure.Last
}

// DefaultCorrection is the follow-up sent after a rejected candidate.
func DefaultCorrection(problems []string) string {
	var b strings.Builder
	b.WriteString("Your previous answer was rejected:\n")
	for _, p := range problems {
		b.WriteString("- ")
		b.WriteString(p)
		b.WriteByte('\n')
	}
	b.WriteString("\nReturn ONLY the corrected output: no quotes, no markdown fences, no explanation.")
	return b.String()
}

// Generate runs the ask-check-correct loop in a single session.
func Generate(ctx context.Context, p Provider, r Request) (Result, error) {
	if r.Attempts < 1 {
		r.Attempts = 1
	}
	correction := r.Correction
	if correction == nil {
		correction = DefaultCorrection
	}

	session, err := p.Start(ctx, r.System)
	if err != nil {
		return Result{}, &ProviderError{Provider: p.Name(), Op: "start", Err: err}
	}
	// Deferred so a panic or an early return still closes it: a leaked
	// claude-cli process outlives autogit otherwise.
	defer func() { _ = session.Close() }()

	turn := r.Prompt
	var last string
	var problems []string

	for attempt := 1; attempt <= r.Attempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return Result{}, &ProviderError{Provider: p.Name(), Op: "send", Err: err}
		}
		raw, err := session.Send(ctx, turn)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				err = ctxErr
			}
			return Result{}, &ProviderError{Provider: p.Name(), Op: "send", Err: err}
		}

		last, problems = r.Validator.Check(raw)
		if len(problems) == 0 {
			return Result{Value: last, Attempts: attempt}, nil
		}
		turn = correction(problems)
	}

	return Result{}, &FailureError{
		Provider: p.Name(),
		Attempts: r.Attempts,
		Last:     last,
		Problems: problems,
	}
}
