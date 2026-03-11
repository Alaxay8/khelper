package cmd

import (
	"fmt"

	"github.com/alaxay8/khelper/internal/kube"
	"github.com/spf13/cobra"
)

func newLogsCmd() *cobra.Command {
	var follow bool
	var sinceStr string
	var tail int64
	var container string
	var allContainers bool
	var allNamespaces bool
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

			since, err := parseNonNegativeDurationFlag("since", sinceStr, 0)
			if err != nil {
				return err
			}

			bundle, err := newClientBundle()
			if err != nil {
				return err
			}

			namespaceScope := resolveNamespaceScope(bundle.Namespace, allNamespaces)

			resolver := kube.NewResolver(bundle.Clientset)
			resolved, err := resolver.ResolvePod(cmd.Context(), namespaceScope, target, kind, pick)
			if err != nil {
				return err
			}

			if resolved.Warning != "" {
				fmt.Fprintf(cmd.ErrOrStderr(), "WARNING: %s\n", resolved.Warning)
			}

			logClient := bundle.Clientset
			if follow && Config().RequestTimeout > 0 {
				streamBundle, err := newClientBundleWithRequestTimeout(0)
				if err != nil {
					return err
				}
				logClient = streamBundle.Clientset
			}

			if err := kube.StreamPodLogs(cmd.Context(), logClient, resolved.Pod, kube.PodLogsOptions{
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
	addTargetResolveFlags(cmd, &kind, &pick, &allNamespaces, kindFlagHelpWithPod, true)

	return cmd
}
