package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/alaxay8/khelper/internal/kube"
	"github.com/alaxay8/khelper/pkg/output"
	"github.com/spf13/cobra"
)

const (
	pickFlagHelp                 = "Pick match number when multiple targets are found (1-based)"
	allNamespacesSearchFlagHelp  = "Search target across all namespaces"
	kindFlagHelpWithPod          = "Target kind override: deployment|statefulset|pod"
	kindFlagHelpWorkloadOnly     = "Target kind override: deployment|statefulset"
	defaultKubernetesClientError = "initialize kubernetes client"
)

func newClientBundle() (*kube.ClientBundle, error) {
	bundle, err := kube.NewClientBundle(Config())
	if err != nil {
		return nil, WrapExitError(ExitCodeGeneral, err, defaultKubernetesClientError)
	}
	return bundle, nil
}

func resolveNamespaceScope(namespace string, allNamespaces bool) string {
	if allNamespaces {
		return kube.NamespaceAll
	}
	return namespace
}

func writeJSONIfRequested(cmd *cobra.Command, payload any) (bool, error) {
	if Config().Output != "json" {
		return false, nil
	}

	if err := output.PrintJSON(cmd.OutOrStdout(), payload); err != nil {
		return false, WrapExitError(ExitCodeGeneral, err, "write JSON output")
	}
	return true, nil
}

func parseNonNegativeDurationFlag(flagName, raw string, defaultValue time.Duration) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultValue, nil
	}

	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return 0, NewExitError(ExitCodeUsage, fmt.Sprintf("invalid --%s value %q: %v", flagName, raw, err))
	}
	if parsed < 0 {
		return 0, NewExitError(ExitCodeUsage, fmt.Sprintf("--%s must be a non-negative duration", flagName))
	}
	return parsed, nil
}

func addTargetResolveFlags(cmd *cobra.Command, kind *string, pick *int, allNamespaces *bool, kindHelp string, includeAllNamespaces bool) {
	if kind != nil {
		cmd.Flags().StringVar(kind, "kind", "", kindHelp)
	}
	if pick != nil {
		cmd.Flags().IntVar(pick, "pick", 0, pickFlagHelp)
	}
	if includeAllNamespaces && allNamespaces != nil {
		cmd.Flags().BoolVarP(allNamespaces, "all-namespaces", "A", false, allNamespacesSearchFlagHelp)
	}
}
