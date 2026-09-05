package git

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	UnsetRepoLocation()
	os.Exit(m.Run())
}
