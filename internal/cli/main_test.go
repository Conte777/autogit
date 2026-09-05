package cli

import (
	"os"
	"testing"

	"github.com/Conte777/autogit/internal/git"
)

func TestMain(m *testing.M) {
	git.UnsetRepoLocation()
	os.Exit(m.Run())
}
