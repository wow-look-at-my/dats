package cmd

import (
	"fmt"
	"runtime/debug"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the dats version",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Fprintf(cmd.OutOrStdout(), "dats %s\n", versionString())
	},
}

// versionString derives a version string from the build info embedded in the
// running binary.
func versionString() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	return versionFromBuildInfo(info)
}

// versionFromBuildInfo prefers the main module version (set for `go install
// module@version` builds), falling back to the VCS revision the binary was
// built from, marked "+dirty" for modified trees. It is split from
// versionString so the derivation is unit-testable with a fake BuildInfo.
func versionFromBuildInfo(info *debug.BuildInfo) string {
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}

	revision := ""
	dirty := false
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			dirty = setting.Value == "true"
		}
	}
	if revision == "" {
		return "unknown"
	}
	if len(revision) > 12 {
		revision = revision[:12]
	}
	if dirty {
		revision += "+dirty"
	}
	return revision
}

func init() {
	// Also expose the same string as `dats --version` (cobra adds the flag
	// whenever Version is non-empty), formatted identically to `dats version`.
	rootCmd.Version = versionString()
	rootCmd.SetVersionTemplate("dats {{.Version}}\n")

	rootCmd.AddCommand(versionCmd)
}
