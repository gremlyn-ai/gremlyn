package cli

import (
	"fmt"
	"runtime/debug"
	"strings"

	"github.com/spf13/cobra"
)

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

func NewVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the gremlyn version",
		Run: func(cmd *cobra.Command, args []string) {
			info, ok := debug.ReadBuildInfo()
			v, c, d := resolveVersion(Version, Commit, BuildDate, info, ok)
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "gremlyn version %s (commit %s, built %s)\n", v, c, d)
		},
	}
}

func resolveVersion(version, commit, date string, info *debug.BuildInfo, ok bool) (v, c, d string) {
	v, c, d = version, commit, date
	if version != "dev" || !ok || info == nil {
		return v, c, d
	}
	if mv := info.Main.Version; mv != "" && mv != "(devel)" {
		v = strings.TrimPrefix(mv, "v")
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			if len(s.Value) >= 7 {
				c = s.Value[:7]
			}
		case "vcs.time":
			d = s.Value
		}
	}
	return v, c, d
}
