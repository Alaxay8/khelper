package cmd

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func maybeAutoInstallCompletion(cmd *cobra.Command) {
	if !shouldAutoInstallCompletion(cmd) {
		return
	}

	shellName := normalizeShellName(os.Getenv("SHELL"))
	if shellName == "" {
		return
	}

	installPath, err := defaultCompletionInstallPath(shellName)
	if err != nil {
		debugf("skip auto completion install: %v", err)
		return
	}

	installPath, err = expandHome(installPath)
	if err != nil {
		debugf("skip auto completion install: %v", err)
		return
	}

	if fi, err := os.Stat(installPath); err == nil && fi.Size() > 0 {
		return
	}

	if err := os.MkdirAll(filepath.Dir(installPath), 0o755); err != nil {
		debugf("skip auto completion install: mkdir %s: %v", filepath.Dir(installPath), err)
		return
	}

	f, err := os.OpenFile(installPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		debugf("skip auto completion install: open %s: %v", installPath, err)
		return
	}
	defer f.Close()

	if err := writeShellCompletion(cmd.Root(), shellName, f); err != nil {
		debugf("skip auto completion install: generate %s completion: %v", shellName, err)
		return
	}

	debugf("auto-installed %s completion to %s", shellName, installPath)
}

func shouldAutoInstallCompletion(cmd *cobra.Command) bool {
	if os.Getenv("KHELPER_AUTO_COMPLETION") == "0" {
		return false
	}
	if os.Getenv("CI") != "" {
		return false
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return false
	}

	switch cmd.Name() {
	case "__complete", "__completeNoDesc", "completion", "completion-install", "comp-install":
		return false
	default:
		return true
	}
}
