package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/alexey/khelper/internal/kube"
	"github.com/spf13/cobra"
)

func newLogsCmd() *cobra.Command {
	var follow bool
	var sinceStr string
	var tail int64
	var container string
	var allContainers bool
	var kind string
	var pick int

	cmd := &cobra.Command{
		Use:   "logs <target>",
		Short: "Show logs for a target",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := args[0]

			if container != "" && allContainers {
				return NewExitError(ExitCodeUsage, "--container and --all-containers cannot be used together")
			}

			since := time.Duration(0)
			if strings.TrimSpace(sinceStr) != "" {
				parsed, err := time.ParseDuration(sinceStr)
				if err != nil {
					return NewExitError(ExitCodeUsage, fmt.Sprintf("invalid --since value %q: %v", sinceStr, err))
				}
				if parsed < 0 {
					return NewExitError(ExitCodeUsage, "--since must be a non-negative duration")
				}
				since = parsed
			}

			bundle, err := kube.NewClientBundle(Config())
			if err != nil {
				return WrapExitError(ExitCodeGeneral, err, "initialize kubernetes client")
			}

			resolver := kube.NewResolver(bundle.Clientset)
			resolved, err := resolver.ResolvePod(cmd.Context(), bundle.Namespace, target, kind, pick)
			if err != nil {
				return err
			}

			if resolved.Warning != "" {
				fmt.Fprintf(cmd.ErrOrStderr(), "WARNING: %s\n", resolved.Warning)
			}

			if err := kube.StreamPodLogs(cmd.Context(), bundle.Clientset, resolved.Pod, kube.PodLogsOptions{
				Follow:        follow,
				Since:         since,
				Tail:          tail,
				Container:     container,
				AllContainers: allContainers,
				Out:           cmd.OutOrStdout(),
			}); err != nil {
				return WrapExitError(ExitCodeGeneral, err, "stream logs")
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&follow, "follow", false, "Stream logs")
	cmd.Flags().StringVar(&sinceStr, "since", "", "Return logs newer than a relative duration like 10m or 1h")
	cmd.Flags().Int64Var(&tail, "tail", 200, "Number of recent log lines to show")
	cmd.Flags().StringVar(&container, "container", "", "Container name")
	cmd.Flags().BoolVar(&allContainers, "all-containers", false, "Show logs for all containers")
	cmd.Flags().StringVar(&kind, "kind", "", "Target kind override: deployment|statefulset|pod")
	cmd.Flags().IntVar(&pick, "pick", 0, "Pick match number when multiple targets are found (1-based)")

	return cmd
}
