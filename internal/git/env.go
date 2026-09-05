package git

import (
	"os"
	"strings"
)

// Git exports these into every hook it runs, and they override cmd.Dir: a git
// command inheriting them operates on the hook's repository, not the directory
// it was pointed at. autogit runs from hooks, and so does its own test suite.
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

// Environ builds the environment for a git subprocess: the caller's, minus the
// variables that would redirect git away from the directory it was given, plus
// extra. Credential and transport settings such as GIT_SSH_COMMAND survive.
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
