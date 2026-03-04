package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/alaxay8/khelper/internal/kube"
	"github.com/spf13/cobra"
)

func newRestartCmd() *cobra.Command {
	var timeout time.Duration
	var kind string
	var pick int

	cmd := &cobra.Command{
		Use:   "restart <target>",
		Short: "Rollout restart a deployment/statefulset",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := args[0]
			kind = strings.ToLower(strings.TrimSpace(kind))
			if kind == kube.KindPod || kind == "po" || kind == "pods" {
				return NewExitError(ExitCodeUsage, "restart supports only deployment or statefulset")
			}

			bundle, err := kube.NewClientBundle(Config())
			if err != nil {
				return WrapExitError(ExitCodeGeneral, err, "initialize kubernetes client")
			}

			resolver := kube.NewResolver(bundle.Clientset)
			workload, err := resolveRestartWorkload(cmd.Context(), resolver, bundle.Namespace, target, kind, pick)
			if err != nil {
				return err
			}

			if workload.Kind != kube.KindDeployment && workload.Kind != kube.KindStatefulSet {
				return NewExitError(ExitCodeUsage, fmt.Sprintf("restart supports only deployment/statefulset, got %s/%s", workload.Kind, workload.Name))
			}

			if err := kube.RolloutRestart(cmd.Context(), bundle.Clientset, bundle.Namespace, workload.Kind, workload.Name, timeout, cmd.OutOrStdout()); err != nil {
				return WrapExitError(ExitCodeGeneral, err, "restart %s/%s", workload.Kind, workload.Name)
			}
			return nil
		},
	}

	cmd.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "Rollout wait timeout")
	cmd.Flags().StringVar(&kind, "kind", "", "Target kind override: deployment|statefulset")
	cmd.Flags().IntVar(&pick, "pick", 0, "Pick match number when multiple targets are found (1-based)")

	return cmd
}

func resolveRestartWorkload(ctx context.Context, resolver *kube.Resolver, namespace, target, kind string, pick int) (kube.WorkloadRef, error) {
	if kind != "" {
		return resolver.ResolveWorkload(ctx, namespace, target, kind, pick)
	}

	dep, err := resolver.ResolveWorkload(ctx, namespace, target, kube.KindDeployment, pick)
	if err == nil {
		return dep, nil
	}

	var notFound *kube.NotFoundError
	if !errors.As(err, &notFound) {
		return kube.WorkloadRef{}, err
	}

	sts, err := resolver.ResolveWorkload(ctx, namespace, target, kube.KindStatefulSet, pick)
	if err == nil {
		return sts, nil
	}

	if errors.As(err, &notFound) {
		return kube.WorkloadRef{}, &kube.NotFoundError{Namespace: namespace, Target: target, Kind: "deployment/statefulset"}
	}
	return kube.WorkloadRef{}, err
}
