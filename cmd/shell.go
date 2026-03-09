package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/alaxay8/khelper/internal/kube"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
)

func newShellCmd() *cobra.Command {
	var container string
	var shellCmd string
	var tty bool
	var kind string
	var pick int

	cmd := &cobra.Command{
		Use:   "shell <target>",
		Short: "Open an interactive shell in a target pod",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := args[0]
			bundle, err := newClientBundle()
			if err != nil {
				return err
			}

			resolver := kube.NewResolver(bundle.Clientset)
			resolved, err := resolver.ResolvePod(cmd.Context(), bundle.Namespace, target, kind, pick)
			if err != nil {
				return err
			}
			if resolved.Warning != "" {
				fmt.Fprintf(cmd.ErrOrStderr(), "WARNING: %s\n", resolved.Warning)
			}

			chosenContainer, err := pickContainerForExec(resolved.Pod, container)
			if err != nil {
				return NewExitError(ExitCodeUsage, err.Error())
			}

			selectedShell := strings.TrimSpace(shellCmd)
			if selectedShell != "" {
				if selectedShell != "bash" && selectedShell != "sh" {
					return NewExitError(ExitCodeUsage, "--command must be either bash or sh")
				}
			} else {
				selectedShell, err = kube.DetectShell(cmd.Context(), bundle.RESTConfig, bundle.Clientset, resolved.Pod.Namespace, resolved.Pod.Name, chosenContainer)
				if err != nil {
					return WrapExitError(ExitCodeGeneral, err, "detect default shell")
				}
			}

			if err := kube.ExecInPod(
				cmd.Context(),
				bundle.RESTConfig,
				bundle.Clientset,
				resolved.Pod.Namespace,
				resolved.Pod.Name,
				chosenContainer,
				[]string{selectedShell},
				tty,
				os.Stdin,
				cmd.OutOrStdout(),
				cmd.ErrOrStderr(),
			); err != nil {
				return WrapExitError(ExitCodeGeneral, err, "exec into pod")
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&container, "container", "", "Container name")
	cmd.Flags().StringVar(&shellCmd, "command", "", "Shell command to run (bash|sh)")
	cmd.Flags().BoolVar(&tty, "tty", true, "Allocate a TTY")
	addTargetResolveFlags(cmd, &kind, &pick, nil, kindFlagHelpWithPod, false)

	return cmd
}

func pickContainerForExec(pod *corev1.Pod, container string) (string, error) {
	container = strings.TrimSpace(container)
	if container != "" {
		if !kube.PodHasContainer(pod, container) {
			return "", fmt.Errorf("container %q not found in pod %s", container, pod.Name)
		}
		return container, nil
	}

	if len(pod.Spec.Containers) == 0 {
		return "", fmt.Errorf("pod %s has no containers", pod.Name)
	}
	if len(pod.Spec.Containers) > 1 {
		return "", fmt.Errorf("pod %s has multiple containers; use --container", pod.Name)
	}
	return pod.Spec.Containers[0].Name, nil
}
