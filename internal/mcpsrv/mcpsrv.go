// Package mcpsrv is the stdio MCP server. The package is not called `mcp` so
// it does not collide with the SDK package it uses.
package mcpsrv

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Conte777/autogit/internal/app"
)

// CommitInput is the `commit` tool's argument object.
type CommitInput struct {
	RepoPath  string `json:"repoPath" jsonschema:"absolute path to the repository, or any directory inside it"`
	StageMode string `json:"stageMode,omitempty" jsonschema:"staged (default), all (git add -A first) or tracked (git add -u first)"`
	DryRun    bool   `json:"dryRun,omitempty" jsonschema:"generate the message and return it without committing"`
}

// BranchInput is the `branch` tool's argument object.
type BranchInput struct {
	RepoPath    string `json:"repoPath" jsonschema:"absolute path to the repository, or any directory inside it"`
	Ticket      string `json:"ticket,omitempty" jsonschema:"optional ticket id; becomes the branch prefix"`
	Description string `json:"description,omitempty" jsonschema:"short free-text description; omit to infer one from the diff"`
}

// Builder opens an App for one repository. It is a function so the server owns
// no state between calls.
type Builder func(ctx context.Context, repoPath string) (*app.App, error)

// Server serves the two tools over stdio.
type Server struct {
	build Builder

	// Handlers run concurrently, and two commits into one repository would
	// fight over the index, so calls are serialised per repository.
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// New builds a server.
func New(build Builder) *Server {
	return &Server{build: build, locks: map[string]*sync.Mutex{}}
}

// Register wires the tools onto an MCP server.
func (s *Server) Register(m *mcp.Server) {
	mcp.AddTool(m, &mcp.Tool{
		Name: "commit",
		Description: "Commit the staged changes with a message generated and validated " +
			"server-side from the diff. You do not write or pass the message.\n\n" +
			"Use ONLY when the user explicitly asks to commit. Committing on a " +
			"protected branch is not possible through this tool at all — the user " +
			"has to do it from the terminal with `autogit commit --force`.",
	}, s.commit)

	mcp.AddTool(m, &mcp.Tool{
		Name: "branch",
		Description: "Create and switch to a new branch named <prefix>/<slug>. The slug is " +
			"generated server-side: pass a short human description, not a slug.\n\n" +
			"Use ONLY when the user explicitly asks for a new branch.",
	}, s.branch)
}

func (s *Server) commit(ctx context.Context, _ *mcp.CallToolRequest, in CommitInput) (
	*mcp.CallToolResult, any, error,
) {
	return s.run(ctx, in.RepoPath, func(a *app.App) (string, error) {
		// No allowProtectedBranch parameter exists on purpose: a model must not
		// be able to talk itself into committing on main.
		result, err := a.Commit(ctx, app.CommitRequest{
			Stage:   app.ParseStageMode(in.StageMode),
			Preview: in.DryRun,
		})
		if err != nil {
			return "", err
		}
		return result.Summary(app.SummaryAgent), nil
	})
}

func (s *Server) branch(ctx context.Context, _ *mcp.CallToolRequest, in BranchInput) (
	*mcp.CallToolResult, any, error,
) {
	return s.run(ctx, in.RepoPath, func(a *app.App) (string, error) {
		result, err := a.Branch(ctx, app.BranchRequest{Ticket: in.Ticket, Description: in.Description})
		if err != nil {
			return "", err
		}
		return result.Summary(), nil
	})
}

// run holds the per-repository lock, recovers panics and turns every failure
// into a tool result. A panic would otherwise kill the process, and Claude Code
// does not restart stdio servers.
func (s *Server) run(ctx context.Context, repoPath string, fn func(*app.App) (string, error)) (
	result *mcp.CallToolResult, _ any, _ error,
) {
	defer func() {
		if r := recover(); r != nil {
			result = errorResult(fmt.Sprintf("internal error: %v\n\n%s", r, debug.Stack()))
		}
	}()

	if repoPath == "" {
		return errorResult("repoPath is required"), nil, nil
	}

	unlock := s.lock(repoPath)
	defer unlock()

	a, err := s.build(ctx, repoPath)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}
	text, err := fn(a)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, nil, nil
}

func (s *Server) lock(repoPath string) func() {
	s.mu.Lock()
	m, ok := s.locks[repoPath]
	if !ok {
		m = &sync.Mutex{}
		s.locks[repoPath] = m
	}
	s.mu.Unlock()

	m.Lock()
	return m.Unlock
}

func errorResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}
}
