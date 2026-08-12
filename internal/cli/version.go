package cli

import "strings"

// Version is the build stamp handed in by main.
type Version struct {
	Version string
	Commit  string
	Date    string
}

// String renders the `--version` line.
func (v Version) String() string {
	parts := []string{v.Version}
	if v.Commit != "" {
		parts = append(parts, "("+v.Commit+")")
	}
	if v.Date != "" {
		parts = append(parts, v.Date)
	}
	return strings.Join(parts, " ")
}

// version is what the MCP server reports as its implementation version.
var version = "dev"
