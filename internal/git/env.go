package git

import (
	"os"
	"strings"
)

// Git exports these into every hook it runs, and they override cmd.Dir.
// GIT_CONFIG_GLOBAL and GIT_CONFIG_SYSTEM are absent on purpose: they pick
// which config files git reads, not which repository it acts on.
var locationVars = []string{
	"GIT_DIR",
	"GIT_COMMON_DIR",
	"GIT_WORK_TREE",
	"GIT_INDEX_FILE",
	"GIT_INDEX_VERSION",
	"GIT_OBJECT_DIRECTORY",
	"GIT_ALTERNATE_OBJECT_DIRECTORIES",
	"GIT_NAMESPACE",
	"GIT_PREFIX",
	"GIT_CEILING_DIRECTORIES",
	"GIT_DISCOVERY_ACROSS_FILESYSTEM",
	"GIT_QUARANTINE_PATH",
	"GIT_INTERNAL_SUPER_PREFIX",
	"GIT_CONFIG",
	"GIT_CONFIG_PARAMETERS",
	"GIT_CONFIG_COUNT",
}

var configOverridePrefixes = []string{"GIT_CONFIG_KEY_", "GIT_CONFIG_VALUE_"}

// UnsetRepoLocation drops the variables that point git at a repository,
// process-wide. Call it from TestMain: lefthook runs `go test ./...` from the
// pre-push hook, and an inherited GIT_DIR beats the cmd.Dir of every temporary
// repository the suite builds — Repo included, since it inherits the
// environment whole on purpose (ADR-0001). Production code must not call it.
func UnsetRepoLocation() {
	for _, name := range locationVars {
		_ = os.Unsetenv(name)
	}
	for _, kv := range os.Environ() {
		name, _, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		for _, prefix := range configOverridePrefixes {
			if strings.HasPrefix(name, prefix) {
				_ = os.Unsetenv(name)
			}
		}
	}
}
