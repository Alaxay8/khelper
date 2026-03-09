package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/alaxay8/khelper/internal/kube"
	"github.com/alaxay8/khelper/pkg/output"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newClearCmd() *cobra.Command {
	var allNamespaces bool
	var dryRun bool
	var yes bool

	cmd := &cobra.Command{
		Use:   "clear [target]",
		Short: "Delete transient Kubernetes objects (default: evicted pods)",
		Long: strings.TrimSpace(`
Delete transient Kubernetes objects.

Current supported target:
  - evicted: pods with status.reason=Evicted

By default, "khelper clear" is equivalent to "khelper clear evicted".

Safety behavior:
  - --dry-run shows what would be deleted without deleting anything.
  - -A/--all-namespaces requires confirmation unless --yes is passed.
`),
		Example: strings.TrimSpace(`
  # Default target (evicted pods) in current namespace
  khelper clear

  # Explicit target
  khelper clear evicted

  # Show what would be deleted
  khelper clear --dry-run

  # Cross-namespace cleanup with confirmation
  khelper clear -A

  # Non-interactive cross-namespace cleanup
  khelper clear -A --yes
`),
		ValidArgs: []string{kube.ClearTargetEvicted},
		Args:      cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target, err := parseClearTarget(args)
			if err != nil {
				return err
			}

			bundle, err := newClientBundle()
			if err != nil {
				return err
			}

			namespaceScope := resolveNamespaceScope(bundle.Namespace, allNamespaces)

			if allNamespaces && !dryRun && !yes {
				if !isTerminalInput(cmd.InOrStdin()) {
					return NewExitError(ExitCodeUsage, "--all requires confirmation; re-run with --yes or use --dry-run")
				}

				confirmed, err := confirmClearAllNamespaces(cmd.InOrStdin(), cmd.ErrOrStderr())
				if err != nil {
					return WrapExitError(ExitCodeGeneral, err, "read confirmation")
				}
				if !confirmed {
					return NewExitError(ExitCodeUsage, "operation canceled")
				}
			}

			result, err := runClearTarget(cmd.Context(), bundle, target, namespaceScope, dryRun)
			if err != nil {
				return err
			}

			if handled, err := writeJSONIfRequested(cmd, result); err != nil {
				return err
			} else if handled {
				return nil
			}

			if len(result.Pods) > 0 {
				if err := renderClearTable(cmd.OutOrStdout(), result, allNamespaces); err != nil {
					return WrapExitError(ExitCodeGeneral, err, "render table")
				}
			}

			scope := clearScopeLabel(result.Namespace)
			if result.Matched == 0 {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "No evicted pods found in %s.\n", scope)
				return nil
			}

			if result.DryRun {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Dry-run: %d evicted pod(s) would be deleted in %s.\n", result.Matched, scope)
				return nil
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Deleted %d evicted pod(s) in %s.\n", result.Deleted, scope)
			return nil
		},
	}

	cmd.Flags().BoolVarP(&allNamespaces, "all-namespaces", "A", false, "Clear across all namespaces")
	cmd.Flags().BoolVar(&allNamespaces, "all", false, "Alias for --all-namespaces")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be deleted without deleting")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation for --all/--all-namespaces")

	return cmd
}

func parseClearTarget(args []string) (string, error) {
	if len(args) == 0 {
		return kube.ClearTargetEvicted, nil
	}

	target := strings.ToLower(strings.TrimSpace(args[0]))
	switch target {
	case "", kube.ClearTargetEvicted:
		return kube.ClearTargetEvicted, nil
	default:
		return "", NewExitError(ExitCodeUsage, fmt.Sprintf("unsupported clear target %q (allowed: evicted)", args[0]))
	}
}

func runClearTarget(ctx context.Context, bundle *kube.ClientBundle, target, namespaceScope string, dryRun bool) (*kube.ClearResult, error) {
	switch target {
	case kube.ClearTargetEvicted:
		result, err := kube.ClearEvictedPods(ctx, bundle.Clientset, namespaceScope, dryRun)
		if err != nil {
			return nil, WrapExitError(ExitCodeGeneral, err, "clear evicted pods")
		}
		return result, nil
	default:
		return nil, NewExitError(ExitCodeUsage, fmt.Sprintf("unsupported clear target %q", target))
	}
}

func renderClearTable(w io.Writer, result *kube.ClearResult, includeNamespace bool) error {
	headers := []string{"NAME", "REASON", "ACTION"}
	if includeNamespace {
		headers = append([]string{"NAMESPACE"}, headers...)
	}

	table := output.NewTable(headers...)
	for i := range result.Pods {
		pod := result.Pods[i]
		row := []string{pod.Name, pod.Reason, pod.Action}
		if includeNamespace {
			row = append([]string{pod.Namespace}, row...)
		}
		table.AddRow(row...)
	}

	return table.Render(w)
}

func clearScopeLabel(scope string) string {
	if scope == kube.NamespaceAll {
		return "all namespaces"
	}
	return fmt.Sprintf("namespace %q", scope)
}

func confirmClearAllNamespaces(in io.Reader, out io.Writer) (bool, error) {
	_, _ = fmt.Fprint(out, "This will delete evicted pods across all namespaces. Continue? [y/N]: ")

	reader := bufio.NewReader(in)
	answer, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}

	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes", nil
}

func isTerminalInput(r io.Reader) bool {
	file, ok := r.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(file.Fd()))
}
