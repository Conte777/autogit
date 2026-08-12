// Command autogit generates git commit messages and branch names with an LLM.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/Conte777/autogit/internal/cli"
)

// Stamped by GoReleaser; "dev" in a local build.
var (
	version = "dev"
	commit  = ""
	date    = ""
)

func main() { os.Exit(run()) }

// run exists so the signal handler is torn down before os.Exit.
func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return cli.Execute(ctx, cli.Version{Version: version, Commit: commit, Date: date})
}
