package git

import (
	"os"
	"strings"
)

// Git exports these into every hook it runs, and they override cmd.Dir.
// GIT_CONFIG_GLOBAL and GIT_CONFIG_SYSTEM are absent on purpose: they pick
// which config files git reads, not which repository it acts on.
var locationVars = map[string]bool{
	"GIT_DIR":                          true,
	"GIT_COMMON_DIR":                   true,
	"GIT_WORK_TREE":                    true,
	"GIT_INDEX_FILE":                   true,
	"GIT_INDEX_VERSION":                true,
	"GIT_OBJECT_DIRECTORY":             true,
	"GIT_ALTERNATE_OBJECT_DIRECTORIES": true,
	"GIT_NAMESPACE":                    true,
	"GIT_PREFIX":                       true,
	"GIT_CEILING_DIRECTORIES":          true,
	"GIT_DISCOVERY_ACROSS_FILESYSTEM":  true,
	"GIT_QUARANTINE_PATH":              true,
	"GIT_INTERNAL_SUPER_PREFIX":        true,
	"GIT_CONFIG":                       true,
	"GIT_CONFIG_PARAMETERS":            true,
	"GIT_CONFIG_COUNT":                 true,
}

var locationPrefixes = []string{"GIT_CONFIG_KEY_", "GIT_CONFIG_VALUE_"}

// Environ builds the environment for a git subprocess that must act on the
// directory it was given and nothing else, plus extra. Transport settings such
// as GIT_SSH_COMMAND survive. It exists for the test suite, which lefthook runs
// from the pre-push hook; Repo inherits instead, for the reason given in env.
func Environ(extra ...string) []string {
	env := make([]string, 0, len(os.Environ())+len(extra))
	for _, kv := range os.Environ() {
		if !redirects(kv) {
			env = append(env, kv)
		}
	}
	return append(env, extra...)
}

func redirects(kv string) bool {
	name, _, ok := strings.Cut(kv, "=")
	if !ok {
		return false
	}
	if locationVars[name] {
		return true
	}
	for _, prefix := range locationPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}
