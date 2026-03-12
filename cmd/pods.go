package cmd

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alaxay8/khelper/internal/kube"
	"github.com/alaxay8/khelper/pkg/output"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type podSummary struct {
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
	Ready     string `json:"ready"`
	Status    string `json:"status"`
	Restarts  int32  `json:"restarts"`
	Age       string `json:"age"`
	Node      string `json:"node,omitempty"`
}

func newPodsCmd() *cobra.Command {
	var wide bool
	var kind string
	var pick int
	var allNamespaces bool

	cmd := &cobra.Command{
		Use:   "pods <target>",
		Short: "List pods for a target",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := args[0]

			bundle, err := newClientBundle()
			if err != nil {
				return err
			}

			namespaceScope := resolveNamespaceScope(bundle.Namespace, allNamespaces)

			resolver := kube.NewResolver(bundle.Clientset)
			workload, err := resolver.ResolveWorkload(cmd.Context(), namespaceScope, target, kind, pick)
			if err != nil {
				return err
			}

			pods, err := listPodsForWorkload(cmd.Context(), bundle, workload)
			if err != nil {
				return err
			}
			if len(pods) == 0 {
				return &kube.NotFoundError{Namespace: workload.Namespace, Target: target, Kind: kube.KindPod}
			}

			summaries := make([]podSummary, 0, len(pods))
			for _, pod := range pods {
				summary := podSummary{
					Name:     pod.Name,
					Ready:    podReady(pod),
					Status:   podStatus(pod),
					Restarts: podRestarts(pod),
					Age:      humanDurationSince(pod.CreationTimestamp.Time),
					Node:     pod.Spec.NodeName,
				}
				if allNamespaces {
					summary.Namespace = pod.Namespace
				}
				summaries = append(summaries, summary)
			}

			if handled, err := writeJSONIfRequested(cmd, summaries); err != nil {
				return err
			} else if handled {
				return nil
			}

			enableColor := output.IsTerminal(cmd.OutOrStdout())
			headers := []string{"NAME", "READY", "STATUS", "RESTARTS", "AGE"}
			if allNamespaces {
				headers = append([]string{"NAMESPACE"}, headers...)
			}
			if wide {
				headers = append(headers, "NODE")
			}
			table := output.NewTable(headers...)
			for _, s := range summaries {
				status := output.ColorizeStatus(s.Status, enableColor)
				row := []string{s.Name, s.Ready, status, strconv.Itoa(int(s.Restarts)), s.Age}
				if allNamespaces {
					row = append([]string{s.Namespace}, row...)
				}
				if wide {
					row = append(row, s.Node)
				}
				table.AddRow(row...)
			}
			if err := table.Render(cmd.OutOrStdout()); err != nil {
				return WrapExitError(ExitCodeGeneral, err, "render table")
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&wide, "wide", false, "Include node column")
	addTargetResolveFlags(cmd, &kind, &pick, &allNamespaces, kindFlagHelpWithPod, true)

	return cmd
}

func listPodsForWorkload(ctx context.Context, bundle *kube.ClientBundle, workload kube.WorkloadRef) ([]corev1.Pod, error) {
	namespace := workload.Namespace
	if namespace == "" {
		namespace = bundle.Namespace
	}

	switch workload.Kind {
	case kube.KindPod:
		pod, err := bundle.Clientset.CoreV1().Pods(namespace).Get(ctx, workload.Name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return nil, &kube.NotFoundError{Namespace: namespace, Target: workload.Name, Kind: kube.KindPod}
			}
			return nil, WrapExitError(ExitCodeGeneral, err, "get pod %s", workload.Name)
		}
		return []corev1.Pod{*pod}, nil
	default:
		if workload.Selector == "" {
			return nil, NewExitError(ExitCodeGeneral, fmt.Sprintf("workload %s/%s has no pod selector", workload.Kind, workload.Name))
		}
		list, err := bundle.Clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: workload.Selector})
		if err != nil {
			return nil, WrapExitError(ExitCodeGeneral, err, "list pods with selector %q", workload.Selector)
		}
		pods := append([]corev1.Pod(nil), list.Items...)
		sort.SliceStable(pods, func(i, j int) bool {
			return pods[i].Name < pods[j].Name
		})
		return pods, nil
	}
}

func podReady(pod corev1.Pod) string {
	total := len(pod.Spec.Containers)
	ready := 0
	for _, s := range pod.Status.ContainerStatuses {
		if s.Ready {
			ready++
		}
	}
	return fmt.Sprintf("%d/%d", ready, total)
}

func podStatus(pod corev1.Pod) string {
	if pod.DeletionTimestamp != nil {
		return "Terminating"
	}

	if pod.Status.Reason != "" {
		return pod.Status.Reason
	}

	for _, s := range pod.Status.InitContainerStatuses {
		if s.State.Waiting != nil && s.State.Waiting.Reason != "" {
			return s.State.Waiting.Reason
		}
		if s.State.Terminated != nil {
			reason := strings.TrimSpace(s.State.Terminated.Reason)
			if reason != "" && !strings.EqualFold(reason, "Completed") {
				return reason
			}
		}
	}

	for _, s := range pod.Status.ContainerStatuses {
		if s.State.Waiting != nil && s.State.Waiting.Reason != "" {
			return s.State.Waiting.Reason
		}
		if s.State.Terminated != nil && s.State.Terminated.Reason != "" {
			return s.State.Terminated.Reason
		}
	}

	if pod.Status.Phase == "" {
		return "Unknown"
	}
	return string(pod.Status.Phase)
}

func podRestarts(pod corev1.Pod) int32 {
	var total int32
	for _, s := range pod.Status.ContainerStatuses {
		total += s.RestartCount
	}
	for _, s := range pod.Status.InitContainerStatuses {
		total += s.RestartCount
	}
	return total
}

func humanDurationSince(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	d := time.Since(t)
	if d < 0 {
		d = 0
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	if d < 30*24*time.Hour {
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
	if d < 365*24*time.Hour {
		return fmt.Sprintf("%dmo", int(d.Hours()/(24*30)))
	}
	return fmt.Sprintf("%dy", int(d.Hours()/(24*365)))
}
