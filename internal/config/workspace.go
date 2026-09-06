package config

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// Workspace scopes the settings to a directory tree.
type Workspace struct {
	Path string `json:"path" jsonschema:"directory whose repositories the rule applies to"`

	raw json.RawMessage
}

func (w Workspace) MarshalJSON() ([]byte, error) {
	if len(w.raw) == 0 {
		return json.Marshal(struct {
			Path string `json:"path"`
		}{w.Path})
	}
	return w.raw, nil
}

func (w *Workspace) UnmarshalJSON(b []byte) error {
	var head struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(b, &head); err != nil {
		return err
	}
	w.Path = head.Path
	w.raw = append(json.RawMessage(nil), b...)
	return nil
}

var errNestedWorkspace = errors.New("workspaces does not nest inside a workspace rule")

// settings is the rule without `path`: the keys that decode over the config
// exactly the way the global file's own keys did.
func (w Workspace) settings() ([]byte, error) {
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(w.raw, &keys); err != nil {
		return nil, err
	}
	for key := range keys {
		if strings.EqualFold(key, "workspaces") {
			return nil, errNestedWorkspace
		}
		if strings.EqualFold(key, "path") {
			delete(keys, key)
		}
	}
	return json.Marshal(keys)
}

// applyWorkspaces layers the rules whose path covers the repository, shallowest
// first, so a deeper directory refines a shallower one. Every rule is decoded
// whether it matches or not: a typo in a rule that is dormant today is still a
// typo, and unknown keys are an error at every layer.
func applyWorkspaces(cfg *Config, path, repoRoot string) error {
	if len(cfg.Workspaces) == 0 {
		return nil
	}
	dir := filepath.Dir(path)
	root := absPath(repoRoot, cfg.env)

	type match struct {
		rule     Workspace
		settings []byte
		depth    int
	}
	var matches []match

	for i, rule := range cfg.Workspaces {
		settings, err := rule.settings()
		if err != nil {
			return configErr("%s: workspaces[%d]: %v", path, i, err)
		}
		if err := decodeStrict(settings, &Config{}); err != nil {
			return configErr("%s: workspaces[%d]: %v", path, i, err)
		}
		if rule.Path == "" {
			return configErr("%s: workspaces[%d]: path is required", path, i)
		}
		if depth, ok := workspaceCovers(resolvePath(rule.Path, dir, cfg.env), root); ok {
			matches = append(matches, match{rule: rule, settings: settings, depth: depth})
		}
	}

	sort.SliceStable(matches, func(i, j int) bool { return matches[i].depth < matches[j].depth })
	for _, m := range matches {
		if err := decodeStrict(m.settings, cfg); err != nil {
			return configErr("%s: workspaces %s: %v", path, m.rule.Path, err)
		}
		takePresetLayer(cfg, dir)
		cfg.workspaceMatches = append(cfg.workspaceMatches, m.rule.Path)
	}
	return nil
}

// workspaceCovers reports whether repoRoot lies under root, and how deep the
// form of root that matched is. Both sides are tried as written and with their
// symlinks resolved, because a rule may name a directory that does not exist
// yet while the repository path reaches the same place through a link. The
// depth comes from the form that matched, so a shallow directory reached
// through a long symlink does not sort as the more specific rule.
func workspaceCovers(root, repoRoot string) (int, bool) {
	if root == "" || repoRoot == "" {
		return 0, false
	}
	for _, r := range pathForms(root) {
		for _, repo := range pathForms(repoRoot) {
			if segments := covers(r, repo); segments > 0 {
				return segments, true
			}
		}
	}
	return 0, false
}

// covers returns the number of segments of root when repoRoot lies under it,
// and 0 otherwise. The comparison is per segment, so a rule for ~/Work/friday
// does not cover ~/Work/friday-releases.
func covers(root, repoRoot string) int {
	rootSegs := pathSegments(root)
	repoSegs := pathSegments(repoRoot)
	if len(repoSegs) < len(rootSegs) {
		return 0
	}
	for i, seg := range rootSegs {
		if !sameSegment(seg, repoSegs[i]) {
			return 0
		}
	}
	return len(rootSegs)
}

func pathForms(path string) []string {
	clean := filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(clean); err == nil && resolved != clean {
		return []string{clean, resolved}
	}
	return []string{clean}
}

func pathSegments(path string) []string {
	segs := strings.Split(filepath.ToSlash(filepath.Clean(path)), "/")
	if len(segs) > 1 && segs[len(segs)-1] == "" {
		return segs[:len(segs)-1]
	}
	return segs
}

// caseInsensitivePaths follows the default filesystem: APFS is
// case-insensitive, so a rule written ~/Work still covers ~/work.
const caseInsensitivePaths = runtime.GOOS == "darwin"

func sameSegment(a, b string) bool {
	if caseInsensitivePaths {
		return strings.EqualFold(a, b)
	}
	return a == b
}
