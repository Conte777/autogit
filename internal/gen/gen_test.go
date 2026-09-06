package gen_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Conte777/autogit/internal/gen"
	"github.com/Conte777/autogit/internal/provider/mock"
)

// wantPrefix accepts anything starting with "feat: ".
type wantPrefix struct{}

func (wantPrefix) Check(raw string) (string, []string) {
	v := strings.TrimSpace(raw)
	if strings.HasPrefix(v, "feat: ") {
		return v, nil
	}
	return v, []string{"must start with `feat: `"}
}

func req(p gen.Provider, attempts int) gen.Request {
	_ = p
	return gen.Request{
		System:    "you are a commit message generator",
		Prompt:    "here is the diff",
		Validator: wantPrefix{},
		Attempts:  attempts,
	}
}

func TestGenerateValidOnFirstTry(t *testing.T) {
	p := &mock.Provider{Replies: []string{"feat: add thing"}}

	got, err := gen.Generate(context.Background(), p, req(p, 3))
	if err != nil {
		t.Fatal(err)
	}
	if got.Value != "feat: add thing" || got.Attempts != 1 {
		t.Errorf("Generate() = %+v, want {feat: add thing 1}", got)
	}
	if p.Sessions != 1 || p.Closes != 1 {
		t.Errorf("sessions=%d closes=%d, want 1 and 1", p.Sessions, p.Closes)
	}
	if p.Systems[0] != "you are a commit message generator" {
		t.Errorf("system prompt = %q", p.Systems[0])
	}
}

func TestGenerateRecoversWithinOneSession(t *testing.T) {
	p := &mock.Provider{Replies: []string{"nope", "still nope", "feat: add thing"}}

	got, err := gen.Generate(context.Background(), p, req(p, 3))
	if err != nil {
		t.Fatal(err)
	}
	if got.Attempts != 3 {
		t.Errorf("Attempts = %d, want 3", got.Attempts)
	}
	if p.Sessions != 1 {
		t.Fatalf("sessions = %d, want all three turns in one session", p.Sessions)
	}

	turns := p.SessionTurns(0)
	if len(turns) != 3 {
		t.Fatalf("turns = %d, want 3", len(turns))
	}
	if turns[0] != "here is the diff" {
		t.Errorf("first turn = %q, want the prompt", turns[0])
	}
	for _, turn := range turns[1:] {
		if !strings.Contains(turn, "must start with `feat: `") {
			t.Errorf("correction turn does not carry the problem:\n%s", turn)
		}
	}
}

func TestGenerateExhaustsAttempts(t *testing.T) {
	p := &mock.Provider{Replies: []string{"one", "two", "three"}}

	_, err := gen.Generate(context.Background(), p, req(p, 3))

	var fail *gen.FailureError
	if !errors.As(err, &fail) {
		t.Fatalf("err = %v, want *gen.FailureError", err)
	}
	if fail.Attempts != 3 {
		t.Errorf("Attempts = %d, want 3", fail.Attempts)
	}
	if fail.Last != "three" {
		t.Errorf("Last = %q, want the last candidate", fail.Last)
	}
	if len(fail.Problems) == 0 {
		t.Error("Problems is empty; the user cannot tell what went wrong")
	}
	if p.Closes != 1 {
		t.Errorf("closes = %d, want exactly 1", p.Closes)
	}
}

func TestGenerateProviderStartErrorIsTerminal(t *testing.T) {
	boom := errors.New("claude: command not found")
	p := &mock.Provider{StartErr: boom}

	_, err := gen.Generate(context.Background(), p, req(p, 3))

	var pe *gen.ProviderError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v, want *gen.ProviderError", err)
	}
	if !errors.Is(err, boom) {
		t.Errorf("err does not wrap the transport error: %v", err)
	}
}

func TestGenerateProviderSendErrorIsTerminal(t *testing.T) {
	boom := errors.New("connection reset")
	p := &mock.Provider{Replies: []string{"nope"}, SendErr: boom}

	_, err := gen.Generate(context.Background(), p, req(p, 5))

	var pe *gen.ProviderError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v, want *gen.ProviderError", err)
	}
	if len(p.SessionTurns(0)) != 2 {
		t.Errorf("turns = %d; a provider error must stop the loop at once", len(p.SessionTurns(0)))
	}
	if p.Closes != 1 {
		t.Errorf("closes = %d, want exactly 1", p.Closes)
	}
}

func TestGenerateHonoursCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	p := &mock.Provider{
		Replies: []string{"nope", "feat: too late"},
		Hook: func(turn int, _ string) {
			if turn == 1 {
				cancel()
			}
		},
	}

	_, err := gen.Generate(ctx, p, req(p, 3))

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if p.Closes != 1 {
		t.Errorf("closes = %d, want exactly 1 even on cancellation", p.Closes)
	}
}

// panicValidator lets us assert Close still runs when the loop unwinds.
type panicValidator struct{}

func (panicValidator) Check(string) (string, []string) { panic("boom") }

func TestGenerateClosesSessionOnPanic(t *testing.T) {
	p := &mock.Provider{Replies: []string{"anything"}}

	func() {
		defer func() {
			if recover() == nil {
				t.Error("panic did not propagate")
			}
		}()
		r := req(p, 1)
		r.Validator = panicValidator{}
		_, _ = gen.Generate(context.Background(), p, r)
	}()

	if p.Closes != 1 {
		t.Errorf("closes = %d, want exactly 1 after a panic", p.Closes)
	}
}

func TestGenerateCustomCorrection(t *testing.T) {
	p := &mock.Provider{Replies: []string{"nope", "feat: ok"}}
	r := req(p, 2)
	r.Correction = func(problems []string) string { return "FIXIT: " + strings.Join(problems, ",") }

	if _, err := gen.Generate(context.Background(), p, r); err != nil {
		t.Fatal(err)
	}
	if turns := p.SessionTurns(0); !strings.HasPrefix(turns[1], "FIXIT: ") {
		t.Errorf("second turn = %q, want the custom correction", turns[1])
	}
}

func TestGenerateReportsATimeoutAsAProviderFailure(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	p := &mock.Provider{Replies: []string{"feat: too late"}}

	_, err := gen.Generate(ctx, p, req(p, 3))

	var provErr *gen.ProviderError
	if !errors.As(err, &provErr) {
		t.Fatalf("err = %v (%T), want a *gen.ProviderError", err, err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want it to wrap context.DeadlineExceeded", err)
	}
}

func TestLastCandidateRendersTheRejectedText(t *testing.T) {
	err := &gen.FailureError{Provider: "mock", Attempts: 3, Last: "Feat: Add Thing", Problems: []string{"lowercase"}}
	if got := gen.LastCandidate(err); !strings.Contains(got, "Feat: Add Thing") {
		t.Errorf("LastCandidate() = %q, want the rejected candidate", got)
	}
	if got := gen.LastCandidate(fmt.Errorf("wrapped: %w", err)); !strings.Contains(got, "Feat: Add Thing") {
		t.Errorf("LastCandidate() = %q, want the rejected candidate through a wrapper", got)
	}
	if got := gen.LastCandidate(&gen.FailureError{Attempts: 1, Problems: []string{"empty"}}); got != "" {
		t.Errorf("LastCandidate() = %q, want empty when the model said nothing", got)
	}
	if got := gen.LastCandidate(errors.New("plain")); got != "" {
		t.Errorf("LastCandidate() = %q, want empty for an ordinary error", got)
	}
}

// A model that ignores the instruction answers with the whole diff, and the
// hook feeds what it renders back into the conversation.
func TestLastCandidateBoundsARunawayAnswer(t *testing.T) {
	flood := strings.Repeat("ы", 5000)
	got := gen.LastCandidate(&gen.FailureError{Attempts: 3, Last: flood, Problems: []string{"too long"}})
	if len([]rune(got)) > 500 {
		t.Errorf("LastCandidate() rendered %d runes, want a bounded line", len([]rune(got)))
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("LastCandidate() = %q, want the cut marked", got[max(0, len(got)-20):])
	}
	if !utf8.ValidString(got) {
		t.Error("LastCandidate() cut a rune in half")
	}
}

func TestExplainJoinsTheErrorAndTheCandidate(t *testing.T) {
	err := &gen.FailureError{Attempts: 3, Last: "Feat: Add Thing", Problems: []string{"lowercase"}}
	want := "no valid output after 3 attempts: lowercase\nlast candidate: Feat: Add Thing"
	if got := gen.Explain(err); got != want {
		t.Errorf("Explain() = %q, want %q", got, want)
	}
	if got := gen.Explain(errors.New("nothing staged")); got != "nothing staged" {
		t.Errorf("Explain() = %q, want the error alone", got)
	}
}
