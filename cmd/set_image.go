package cmd

import (
	"fmt"
	"strings"

	"github.com/alaxay8/khelper/internal/kube"
	"github.com/alaxay8/khelper/pkg/output"
	"github.com/spf13/cobra"
)

func newSetImageCmd() *cobra.Command {
	var kind string
	var pick int
	var allNamespaces bool

	cmd := &cobra.Command{
		Use:   "set-image <target> <container=image> [container=image...]",
		Short: "Update container images for deployment/statefulset",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if isPodKind(kind) {
				return NewExitError(ExitCodeUsage, "set-image supports only deployment or statefulset")
			}

			assignments, err := parseImageAssignments(args[1:])
			if err != nil {
				return NewExitError(ExitCodeUsage, err.Error())
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

			result, err := kube.SetWorkloadImages(cmd.Context(), bundle.Clientset, workload.Namespace, workload.Kind, workload.Name, assignments)
			if err != nil {
				return WrapExitError(ExitCodeGeneral, err, "set image for %s/%s", workload.Kind, workload.Name)
			}

			if Config().Output == "json" {
				if err := output.PrintJSON(cmd.OutOrStdout(), result); err != nil {
					return WrapExitError(ExitCodeGeneral, err, "write JSON output")
				}
				return nil
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Updated %s/%s in namespace %s: %s\n", result.Kind, result.Name, result.Namespace, formatUpdatedImages(result.Updated))
			return nil
		},
	}

	cmd.Flags().StringVar(&kind, "kind", "", "Target kind override: deployment|statefulset")
	cmd.Flags().IntVar(&pick, "pick", 0, "Pick match number when multiple targets are found (1-based)")
	cmd.Flags().BoolVarP(&allNamespaces, "all-namespaces", "A", false, "Search target across all namespaces")

	return cmd
}

func parseImageAssignments(values []string) (map[string]string, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("at least one container=image assignment is required")
	}

	result := make(map[string]string, len(values))
	for _, raw := range values {
		raw = strings.TrimSpace(raw)
		parts := strings.SplitN(raw, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid image assignment %q (expected container=image)", raw)
		}

		container := strings.TrimSpace(parts[0])
		image := strings.TrimSpace(parts[1])
		if container == "" || image == "" {
			return nil, fmt.Errorf("invalid image assignment %q (expected container=image)", raw)
		}
		if _, exists := result[container]; exists {
			return nil, fmt.Errorf("duplicate image assignment for container %q", container)
		}
		result[container] = image
	}

	return result, nil
}
