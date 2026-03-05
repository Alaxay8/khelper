package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

func newCompletionInstallCmd() *cobra.Command {
	var shellFlag string
	var pathFlag string

	cmd := &cobra.Command{
		Use:     "completion-install [shell]",
		Short:   "Install shell completion to a standard path",
		Aliases: []string{"comp-install"},
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			shellName, err := resolveShell(shellFlag, args)
			if err != nil {
				return NewExitError(ExitCodeUsage, err.Error())
			}

			installPath := pathFlag
			if installPath == "" {
				installPath, err = defaultCompletionInstallPath(shellName)
				if err != nil {
					return NewExitError(ExitCodeUsage, err.Error())
				}
			}

			installPath, err = expandHome(installPath)
			if err != nil {
				return WrapExitError(ExitCodeGeneral, err, "resolve completion path")
			}

			if err := os.MkdirAll(filepath.Dir(installPath), 0o755); err != nil {
				return WrapExitError(ExitCodeGeneral, err, "create completion directory")
			}

			f, err := os.OpenFile(installPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
			if err != nil {
				return WrapExitError(ExitCodeGeneral, err, "open completion file")
			}
			defer f.Close()

			if err := writeShellCompletion(cmd.Root(), shellName, f); err != nil {
				return WrapExitError(ExitCodeGeneral, err, "generate completion script")
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Installed %s completion to %s\n", shellName, installPath)
			printCompletionHint(cmd.OutOrStdout(), shellName, installPath)
			return nil
		},
	}

	cmd.Flags().StringVar(&shellFlag, "shell", "", "Shell name: bash|zsh|fish|powershell")
	cmd.Flags().StringVar(&pathFlag, "path", "", "Custom completion file path")
	return cmd
}

func resolveShell(shellFlag string, args []string) (string, error) {
	shellName := shellFlag
	if len(args) > 0 {
		shellName = args[0]
	}
	if shellName == "" {
		detected, err := detectShellFromEnv()
		if err != nil {
			return "", err
		}
		shellName = detected
	}
	shellName = normalizeShellName(shellName)
	if shellName == "" {
		return "", fmt.Errorf("unsupported shell (supported: bash|zsh|fish|powershell)")
	}
	return shellName, nil
}

func detectShellFromEnv() (string, error) {
	shell := strings.TrimSpace(os.Getenv("SHELL"))
	if shell == "" {
		return "", fmt.Errorf("cannot detect shell from SHELL; pass it explicitly, e.g. 'khelper completion-install bash'")
	}
	return shell, nil
}

func normalizeShellName(name string) string {
	switch strings.ToLower(filepath.Base(strings.TrimSpace(name))) {
	case "bash":
		return "bash"
	case "zsh":
		return "zsh"
	case "fish":
		return "fish"
	case "powershell", "pwsh":
		return "powershell"
	default:
		return ""
	}
}

func defaultCompletionInstallPath(shellName string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", fmt.Errorf("cannot determine home directory; use --path")
	}

	switch shellName {
	case "bash":
		return filepath.Join(home, ".local", "share", "bash-completion", "completions", "khelper"), nil
	case "zsh":
		return filepath.Join(home, ".zfunc", "_khelper"), nil
	case "fish":
		return filepath.Join(home, ".config", "fish", "completions", "khelper.fish"), nil
	case "powershell":
		if runtime.GOOS != "windows" {
			return "", fmt.Errorf("default PowerShell completion path is not defined on %s; use --path", runtime.GOOS)
		}
		return filepath.Join(home, "Documents", "PowerShell", "Completions", "khelper.ps1"), nil
	default:
		return "", fmt.Errorf("unsupported shell %q", shellName)
	}
}

func expandHome(path string) (string, error) {
	if path == "" || path[0] != '~' {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", fmt.Errorf("cannot resolve '~' in path %q", path)
	}
	if path == "~" {
		return home, nil
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:]), nil
	}
	return "", fmt.Errorf("unsupported home expansion in path %q", path)
}

func writeShellCompletion(root *cobra.Command, shellName string, w io.Writer) error {
	switch shellName {
	case "bash":
		return root.GenBashCompletionV2(w, true)
	case "zsh":
		return root.GenZshCompletion(w)
	case "fish":
		return root.GenFishCompletion(w, true)
	case "powershell":
		return root.GenPowerShellCompletionWithDesc(w)
	default:
		return fmt.Errorf("unsupported shell %q", shellName)
	}
}

func printCompletionHint(w io.Writer, shellName, installPath string) {
	switch shellName {
	case "bash":
		fmt.Fprintf(w, "Open a new shell, or run:\nsource %q\n", installPath)
	case "zsh":
		fmt.Fprintln(w, "Ensure ~/.zfunc is in fpath and run 'autoload -Uz compinit && compinit' (or open a new shell).")
	case "fish":
		fmt.Fprintln(w, "Open a new fish shell session to load completion automatically.")
	case "powershell":
		fmt.Fprintf(w, "Reload your PowerShell profile and dot-source the file if needed: . %q\n", installPath)
	}
}
