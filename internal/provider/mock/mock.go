// Package mock is a scripted gen.Provider for tests.
package mock

import (
	"context"
	"errors"
	"sync"

	"github.com/Conte777/autogit/internal/gen"
)

// Provider replays a fixed script of replies and records what it was asked.
type Provider struct {
	Replies  []string // one per Send, in order
	StartErr error
	SendErr  error // returned instead of Replies[n] once the script runs out
	Hook     func(turn int, text string)

	mu       sync.Mutex
	Systems  []string   // one entry per Start
	Turns    [][]string // one slice per session
	Closes   int
	Sessions int
}

func (p *Provider) Name() string { return "mock" }

// Start opens a session. Every session shares the reply script, so a test can
// assert that all turns of one generation went to the same session.
func (p *Provider) Start(_ context.Context, system string) (gen.Session, error) {
	if p.StartErr != nil {
		return nil, p.StartErr
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Systems = append(p.Systems, system)
	p.Turns = append(p.Turns, nil)
	p.Sessions++
	return &session{p: p, index: len(p.Turns) - 1}, nil
}

type session struct {
	p      *Provider
	index  int
	closed bool
}

func (s *session) Send(ctx context.Context, text string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	s.p.mu.Lock()
	s.p.Turns[s.index] = append(s.p.Turns[s.index], text)
	turn := len(s.p.Turns[s.index])
	replies := s.p.Replies
	sendErr := s.p.SendErr
	hook := s.p.Hook
	s.p.mu.Unlock()

	if hook != nil {
		hook(turn, text)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if sendErr != nil && turn > len(replies) {
		return "", sendErr
	}
	if turn > len(replies) {
		return "", errors.New("mock: reply script exhausted")
	}
	return replies[turn-1], nil
}

func (s *session) Close() error {
	if s.closed {
		return errors.New("mock: Close called twice")
	}
	s.closed = true
	s.p.mu.Lock()
	defer s.p.mu.Unlock()
	s.p.Closes++
	return nil
}

// SessionTurns returns the turns sent to session n.
func (p *Provider) SessionTurns(n int) []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if n >= len(p.Turns) {
		return nil
	}
	return p.Turns[n]
}
