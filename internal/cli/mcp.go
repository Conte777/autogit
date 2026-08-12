package cli

import (
	"context"
	"io"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/Conte777/autogit/internal/app"
	"github.com/Conte777/autogit/internal/mcpsrv"
	"github.com/Conte777/autogit/internal/ui"
)

func mcpCmd(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Run the MCP server on stdio",
		Long: "Exposes `commit` and `branch` to an agent. Register it with:\n" +
			"  claude mcp add --scope user autogit -- autogit mcp",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMCP(cmd.Context(), g)
		},
	}
}

func runMCP(ctx context.Context, g *globals) error {
	// The spec forbids anything on stdout that is not an MCP message. Take the
	// real stdout for the transport and point os.Stdout at stderr, so a stray
	// write from anywhere — ours or a library's — cannot corrupt the stream.
	transport := &mcp.IOTransport{
		Reader: os.Stdin,
		Writer: nopCloser{os.Stdout},
	}
	os.Stdout = os.Stderr

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "autogit",
		Version: version,
	}, nil)

	mcpsrv.New(func(ctx context.Context, repoPath string) (*app.App, error) {
		local := *g
		local.repo = repoPath
		local.noInput = true
		return build(ctx, &local, app.SurfaceMCP, ui.Noop{})
	}).Register(server)

	return server.Run(ctx, transport)
}

// nopCloser keeps the session from closing the process's real stdout.
type nopCloser struct{ io.Writer }

func (nopCloser) Close() error { return nil }
