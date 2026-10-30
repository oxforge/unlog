package cmd

import (
	"fmt"
	"runtime/debug"
	"strings"

	"github.com/spf13/cobra"
)

// Version is set by release builds (-ldflags -X, see .goreleaser.yaml).
var Version = "dev"

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version of unlog",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("unlog %s\n", version())
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}

// version is the version this binary reports: the one a release build set,
// or else the module version go recorded at build time.
func version() string {
	var module string
	if info, ok := debug.ReadBuildInfo(); ok {
		module = info.Main.Version
	}
	return versionFrom(Version, module)
}

// versionFrom picks between the version a release build set and the module
// version go recorded: v0.3.0 for go install ...@v0.3.0, a pseudo-version for
// go build in a git checkout, "(devel)" without one. The leading v is
// dropped, as GoReleaser drops it.
func versionFrom(set, module string) string {
	if set != "dev" || module == "" || module == "(devel)" {
		return set
	}
	return strings.TrimPrefix(module, "v")
}
