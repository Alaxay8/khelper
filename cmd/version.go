package cmd

import (
	"fmt"
	"os"
	"runtime/debug"
	"time"

	"github.com/spf13/cobra"
)

var (
	Version   = "v0.1.1"
	Commit    = "none"
	BuildDate = "unknown"
)

func newVersionCmd() *cobra.Command {
	initBuildInfoDefaults()
	return &cobra.Command{
		Use:   "version",
		Short: "Print build version information",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintf(cmd.OutOrStdout(), "version: %s\ncommit: %s\nbuildDate: %s\n", Version, Commit, BuildDate)
			return nil
		},
	}
}

func initBuildInfoDefaults() {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		setFallbackBuildDate()
		return
	}
	if Version == "dev" && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		Version = bi.Main.Version
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			if Commit == "none" && s.Value != "" {
				if len(s.Value) > 7 {
					Commit = s.Value[:7]
				} else {
					Commit = s.Value
				}
			}
		case "vcs.time":
			if BuildDate == "unknown" && s.Value != "" {
				BuildDate = s.Value
			}
		}
	}
	setFallbackBuildDate()
}

func setFallbackBuildDate() {
	if BuildDate != "unknown" {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		BuildDate = time.Now().UTC().Format(time.RFC3339)
		return
	}
	fi, err := os.Stat(exe)
	if err != nil {
		BuildDate = time.Now().UTC().Format(time.RFC3339)
		return
	}
	BuildDate = fi.ModTime().UTC().Format(time.RFC3339)
}
