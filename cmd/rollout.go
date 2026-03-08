package cmd

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alaxay8/khelper/internal/kube"
	"github.com/alaxay8/khelper/pkg/output"
	"github.com/spf13/cobra"
)

func newRolloutCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rollout",
		Short: "Manage deployment/statefulset rollouts",
	}

	cmd.AddCommand(
		newRolloutStatusCmd(),
		newRolloutHistoryCmd(),
		newRolloutUndoCmd(),
	)

	return cmd
}

func newRolloutStatusCmd() *cobra.Command {
	var kind string
	var pick int
	var allNamespaces bool

	cmd := &cobra.Command{
		Use:   "status <target>",
		Short: "Show rollout status for a workload",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if isPodKind(kind) {
				return NewExitError(ExitCodeUsage, "rollout status supports only deployment or statefulset")
			}

			bundle, err := kube.NewClientBundle(Config())
			if err != nil {
				return WrapExitError(ExitCodeGeneral, err, "initialize kubernetes client")
			}

			namespaceScope := bundle.Namespace
			if allNamespaces {
				namespaceScope = kube.NamespaceAll
			}

			resolver := kube.NewResolver(bundle.Clientset)
			workload, err := resolveRestartWorkload(cmd.Context(), resolver, namespaceScope, args[0], kind, pick)
			if err != nil {
				return err
			}

			status, err := kube.GetRolloutStatus(cmd.Context(), bundle.Clientset, workload.Namespace, workload.Kind, workload.Name)
			if err != nil {
				return WrapExitError(ExitCodeGeneral, err, "get rollout status for %s/%s", workload.Kind, workload.Name)
			}

			if Config().Output == "json" {
				if err := output.PrintJSON(cmd.OutOrStdout(), status); err != nil {
					return WrapExitError(ExitCodeGeneral, err, "write JSON output")
				}
				return nil
			}

			headers := []string{"KIND", "NAME", "REVISION", "UPDATED", "READY", "AVAILABLE", "STATUS"}
			if allNamespaces {
				headers = append([]string{"NAMESPACE"}, headers...)
			}

			table := output.NewTable(headers...)
			revision := status.CurrentRevision
			if revision == "" {
				revision = "-"
			}
			if workload.Kind == kube.KindStatefulSet {
				if status.UpdateRevision != "" {
					revision = fmt.Sprintf("%s/%s", emptyAsDash(status.CurrentRevision), status.UpdateRevision)
				} else {
					revision = emptyAsDash(status.CurrentRevision)
				}
			}

			updated := fmt.Sprintf("%d/%d", status.UpdatedReplicas, status.DesiredReplicas)
			ready := fmt.Sprintf("%d/%d", status.ReadyReplicas, status.DesiredReplicas)
			available := fmt.Sprintf("%d/%d", status.AvailableReplicas, status.DesiredReplicas)
			if workload.Kind == kube.KindStatefulSet {
				available = "-"
			}

			state := "progressing"
			if status.Complete {
				state = "complete"
			}

			row := []string{workload.Kind, workload.Name, revision, updated, ready, available, state}
			if allNamespaces {
				row = append([]string{workload.Namespace}, row...)
			}
			table.AddRow(row...)

			if err := table.Render(cmd.OutOrStdout()); err != nil {
				return WrapExitError(ExitCodeGeneral, err, "render table")
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&kind, "kind", "", "Target kind override: deployment|statefulset")
	cmd.Flags().IntVar(&pick, "pick", 0, "Pick match number when multiple targets are found (1-based)")
	cmd.Flags().BoolVarP(&allNamespaces, "all-namespaces", "A", false, "Search target across all namespaces")

	return cmd
}

func newRolloutHistoryCmd() *cobra.Command {
	var kind string
	var pick int
	var allNamespaces bool

	cmd := &cobra.Command{
		Use:   "history <target>",
		Short: "Show rollout history for a workload",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if isPodKind(kind) {
				return NewExitError(ExitCodeUsage, "rollout history supports only deployment or statefulset")
			}

			bundle, err := kube.NewClientBundle(Config())
			if err != nil {
				return WrapExitError(ExitCodeGeneral, err, "initialize kubernetes client")
			}

			namespaceScope := bundle.Namespace
			if allNamespaces {
				namespaceScope = kube.NamespaceAll
			}

			resolver := kube.NewResolver(bundle.Clientset)
			workload, err := resolveRestartWorkload(cmd.Context(), resolver, namespaceScope, args[0], kind, pick)
			if err != nil {
				return err
			}

			history, err := kube.ListRolloutHistory(cmd.Context(), bundle.Clientset, workload.Namespace, workload.Kind, workload.Name)
			if err != nil {
				return WrapExitError(ExitCodeGeneral, err, "get rollout history for %s/%s", workload.Kind, workload.Name)
			}

			if Config().Output == "json" {
				if err := output.PrintJSON(cmd.OutOrStdout(), history); err != nil {
					return WrapExitError(ExitCodeGeneral, err, "write JSON output")
				}
				return nil
			}

			headers := []string{"REVISION", "CURRENT", "AGE", "IMAGES", "CHANGE-CAUSE"}
			if allNamespaces {
				headers = append([]string{"NAMESPACE"}, headers...)
			}
			table := output.NewTable(headers...)

			if len(history) == 0 {
				row := []string{"-", "-", "-", "-", "No revisions found"}
				if allNamespaces {
					row = append([]string{workload.Namespace}, row...)
				}
				table.AddRow(row...)
			} else {
				for i := range history {
					entry := history[i]
					revision := "-"
					if entry.Revision > 0 {
						revision = strconv.FormatInt(entry.Revision, 10)
					}

					current := ""
					if entry.Current {
						current = "yes"
					}

					age := "-"
					if !entry.CreatedAt.IsZero() {
						age = humanDurationSince(entry.CreatedAt)
					}

					images := "-"
					if len(entry.Images) > 0 {
						images = strings.Join(entry.Images, ",")
					}

					changeCause := emptyAsDash(entry.ChangeCause)

					row := []string{revision, current, age, images, changeCause}
					if allNamespaces {
						row = append([]string{workload.Namespace}, row...)
					}
					table.AddRow(row...)
				}
			}

			if err := table.Render(cmd.OutOrStdout()); err != nil {
				return WrapExitError(ExitCodeGeneral, err, "render table")
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&kind, "kind", "", "Target kind override: deployment|statefulset")
	cmd.Flags().IntVar(&pick, "pick", 0, "Pick match number when multiple targets are found (1-based)")
	cmd.Flags().BoolVarP(&allNamespaces, "all-namespaces", "A", false, "Search target across all namespaces")

	return cmd
}

func newRolloutUndoCmd() *cobra.Command {
	var kind string
	var pick int
	var allNamespaces bool
	var toRevision int64
	var timeout time.Duration

	cmd := &cobra.Command{
		Use:   "undo <target>",
		Short: "Rollback workload to a previous rollout revision",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if isPodKind(kind) {
				return NewExitError(ExitCodeUsage, "rollout undo supports only deployment or statefulset")
			}
			if toRevision < 0 {
				return NewExitError(ExitCodeUsage, "--to-revision must be >= 0")
			}

			bundle, err := kube.NewClientBundle(Config())
			if err != nil {
				return WrapExitError(ExitCodeGeneral, err, "initialize kubernetes client")
			}

			namespaceScope := bundle.Namespace
			if allNamespaces {
				namespaceScope = kube.NamespaceAll
			}

			resolver := kube.NewResolver(bundle.Clientset)
			workload, err := resolveRestartWorkload(cmd.Context(), resolver, namespaceScope, args[0], kind, pick)
			if err != nil {
				return err
			}

			result, err := kube.UndoRollout(cmd.Context(), bundle.Clientset, workload.Namespace, workload.Kind, workload.Name, toRevision, timeout, cmd.OutOrStdout())
			if err != nil {
				return WrapExitError(ExitCodeGeneral, err, "undo rollout for %s/%s", workload.Kind, workload.Name)
			}

			if Config().Output == "json" {
				if err := output.PrintJSON(cmd.OutOrStdout(), result); err != nil {
					return WrapExitError(ExitCodeGeneral, err, "write JSON output")
				}
				return nil
			}

			if result.FromRevision > 0 {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Rolled back %s/%s in namespace %s from revision %d to %d.\n", result.Kind, result.Name, result.Namespace, result.FromRevision, result.ToRevision)
			} else {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Rolled back %s/%s in namespace %s to revision %d.\n", result.Kind, result.Name, result.Namespace, result.ToRevision)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&kind, "kind", "", "Target kind override: deployment|statefulset")
	cmd.Flags().IntVar(&pick, "pick", 0, "Pick match number when multiple targets are found (1-based)")
	cmd.Flags().BoolVarP(&allNamespaces, "all-namespaces", "A", false, "Search target across all namespaces")
	cmd.Flags().Int64Var(&toRevision, "to-revision", 0, "Roll back to specific revision (default: previous)")
	cmd.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "Rollout wait timeout (set 0 to skip waiting)")

	return cmd
}

func isPodKind(kind string) bool {
	kind = strings.ToLower(strings.TrimSpace(kind))
	switch kind {
	case kube.KindPod, "po", "pods":
		return true
	default:
		return false
	}
}

func emptyAsDash(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	return value
}

func formatUpdatedImages(updated map[string]string) string {
	if len(updated) == 0 {
		return ""
	}

	names := make([]string, 0, len(updated))
	for name := range updated {
		names = append(names, name)
	}
	sort.Strings(names)

	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("%s=%s", name, updated[name]))
	}
	return strings.Join(parts, ",")
}
