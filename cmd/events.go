package cmd

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/alaxay8/khelper/internal/kube"
	"github.com/alaxay8/khelper/pkg/output"
	"github.com/spf13/cobra"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type eventSummary struct {
	LastSeen string `json:"lastSeen,omitempty"`
	Type     string `json:"type"`
	Reason   string `json:"reason"`
	Object   string `json:"object"`
	Count    int32  `json:"count"`
	Message  string `json:"message"`
}

type eventScope struct {
	workloadKind    string
	workloadName    string
	podNames        map[string]struct{}
	podUIDs         map[string]struct{}
	replicaSetNames map[string]struct{}
	podNamePrefixes []string
}

func newEventsCmd() *cobra.Command {
	var kind string
	var pick int
	var sinceStr string
	var warningsOnly bool

	cmd := &cobra.Command{
		Use:   "events <target>",
		Short: "Show recent Kubernetes events related to a target",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := args[0]

			since := time.Hour
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
			workload, err := resolver.ResolveWorkload(cmd.Context(), bundle.Namespace, target, kind, pick)
			if err != nil {
				return err
			}

			pods, err := listPodsForWorkload(cmd.Context(), bundle, workload)
			if err != nil {
				return err
			}

			replicaSetNames, err := relatedReplicaSetNames(cmd.Context(), bundle, workload)
			if err != nil {
				return WrapExitError(ExitCodeGeneral, err, "list related replica sets")
			}

			scope := buildEventScope(workload, pods, replicaSetNames)
			eventList, err := bundle.Clientset.CoreV1().Events(bundle.Namespace).List(cmd.Context(), metav1.ListOptions{})
			if err != nil {
				return WrapExitError(ExitCodeGeneral, err, "list events")
			}

			now := time.Now()
			filtered := filterRelatedEvents(eventList.Items, scope, since, now, warningsOnly)
			summaries := summarizeEvents(filtered)

			if Config().Output == "json" {
				if err := output.PrintJSON(cmd.OutOrStdout(), summaries); err != nil {
					return WrapExitError(ExitCodeGeneral, err, "write JSON output")
				}
				return nil
			}

			table := output.NewTable("AGE", "TYPE", "REASON", "OBJECT", "COUNT", "MESSAGE")
			if len(filtered) == 0 {
				table.AddRow("-", "INFO", "summary", workload.Kind+"/"+workload.Name, "-", "No related events found")
			} else {
				for i := range filtered {
					ev := filtered[i]
					ts := eventTimestamp(ev)
					age := "unknown"
					if !ts.IsZero() {
						age = humanDurationSince(ts)
					}
					table.AddRow(
						age,
						strings.ToUpper(strings.TrimSpace(ev.Type)),
						doctorCell(ev.Reason),
						eventObject(ev),
						fmt.Sprintf("%d", ev.Count),
						doctorCell(ev.Message),
					)
				}
			}

			if err := table.Render(cmd.OutOrStdout()); err != nil {
				return WrapExitError(ExitCodeGeneral, err, "render table")
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&kind, "kind", "", "Target kind override: deployment|statefulset|pod")
	cmd.Flags().IntVar(&pick, "pick", 0, "Pick match number when multiple targets are found (1-based)")
	cmd.Flags().StringVar(&sinceStr, "since", "1h", "Show events newer than this duration (e.g. 30m, 2h)")
	cmd.Flags().BoolVar(&warningsOnly, "warnings-only", false, "Show only Warning events")

	return cmd
}

func relatedReplicaSetNames(ctx context.Context, bundle *kube.ClientBundle, workload kube.WorkloadRef) (map[string]struct{}, error) {
	names := make(map[string]struct{})
	if workload.Kind != kube.KindDeployment || workload.Selector == "" {
		return names, nil
	}

	replicaSets, err := bundle.Clientset.AppsV1().ReplicaSets(bundle.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: workload.Selector,
	})
	if err != nil {
		return nil, err
	}

	for i := range replicaSets.Items {
		rs := replicaSets.Items[i]
		if replicaSetOwnedByDeployment(rs, workload.Name) {
			names[rs.Name] = struct{}{}
		}
	}

	return names, nil
}

func replicaSetOwnedByDeployment(rs appsv1.ReplicaSet, deploymentName string) bool {
	for _, owner := range rs.OwnerReferences {
		if strings.EqualFold(owner.Kind, "Deployment") && owner.Name == deploymentName {
			return true
		}
	}
	return strings.HasPrefix(rs.Name, deploymentName+"-")
}

func buildEventScope(workload kube.WorkloadRef, pods []corev1.Pod, replicaSetNames map[string]struct{}) eventScope {
	scope := eventScope{
		workloadKind:    normalizeEventKind(workload.Kind),
		workloadName:    workload.Name,
		podNames:        make(map[string]struct{}, len(pods)),
		podUIDs:         make(map[string]struct{}, len(pods)),
		replicaSetNames: replicaSetNames,
		podNamePrefixes: make([]string, 0, len(replicaSetNames)+1),
	}

	for i := range pods {
		pod := pods[i]
		scope.podNames[pod.Name] = struct{}{}
		if pod.UID != "" {
			scope.podUIDs[string(pod.UID)] = struct{}{}
		}
	}

	switch workload.Kind {
	case kube.KindDeployment:
		for rsName := range replicaSetNames {
			scope.podNamePrefixes = append(scope.podNamePrefixes, rsName+"-")
		}
		// Fallback for events that reference pods from rollout history when old RS is already gone.
		scope.podNamePrefixes = append(scope.podNamePrefixes, workload.Name+"-")
	case kube.KindStatefulSet:
		scope.podNamePrefixes = append(scope.podNamePrefixes, workload.Name+"-")
	}

	return scope
}

func filterRelatedEvents(events []corev1.Event, scope eventScope, since time.Duration, now time.Time, warningsOnly bool) []corev1.Event {
	var cutoff time.Time
	if since > 0 {
		cutoff = now.Add(-since)
	}

	filtered := make([]corev1.Event, 0, len(events))
	for i := range events {
		event := events[i]
		if warningsOnly && !isWarningEventType(event.Type) {
			continue
		}
		if !isEventInScope(event, scope) {
			continue
		}

		ts := eventTimestamp(event)
		if !cutoff.IsZero() && !ts.IsZero() && ts.Before(cutoff) {
			continue
		}

		filtered = append(filtered, event)
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		left := eventTimestamp(filtered[i])
		right := eventTimestamp(filtered[j])
		if left.Equal(right) {
			return filtered[i].Name < filtered[j].Name
		}
		if left.IsZero() {
			return false
		}
		if right.IsZero() {
			return true
		}
		return left.After(right)
	})

	return filtered
}

func isEventInScope(event corev1.Event, scope eventScope) bool {
	kind := normalizeEventKind(event.InvolvedObject.Kind)
	name := strings.TrimSpace(event.InvolvedObject.Name)
	uid := string(event.InvolvedObject.UID)

	if kind == scope.workloadKind && name == scope.workloadName {
		return true
	}

	if kind == "pod" {
		if _, ok := scope.podNames[name]; ok {
			return true
		}
		if uid != "" {
			if _, ok := scope.podUIDs[uid]; ok {
				return true
			}
		}
		for i := range scope.podNamePrefixes {
			if strings.HasPrefix(name, scope.podNamePrefixes[i]) {
				return true
			}
		}
	}

	if kind == "replicaset" {
		if _, ok := scope.replicaSetNames[name]; ok {
			return true
		}
	}

	return false
}

func summarizeEvents(events []corev1.Event) []eventSummary {
	summaries := make([]eventSummary, 0, len(events))
	for i := range events {
		event := events[i]
		ts := eventTimestamp(event)
		row := eventSummary{
			Type:    strings.TrimSpace(event.Type),
			Reason:  strings.TrimSpace(event.Reason),
			Object:  eventObject(event),
			Count:   event.Count,
			Message: doctorCell(event.Message),
		}
		if !ts.IsZero() {
			row.LastSeen = ts.UTC().Format(time.RFC3339)
		}
		summaries = append(summaries, row)
	}
	return summaries
}

func normalizeEventKind(value string) string {
	kind := strings.ToLower(strings.TrimSpace(value))
	switch kind {
	case "deployment", "deployments", "deployment.apps":
		return "deployment"
	case "statefulset", "statefulsets", "statefulset.apps":
		return "statefulset"
	case "pod", "pods":
		return "pod"
	case "replicaset", "replicasets", "replicaset.apps":
		return "replicaset"
	default:
		return kind
	}
}

func isWarningEventType(eventType string) bool {
	return strings.EqualFold(strings.TrimSpace(eventType), corev1.EventTypeWarning)
}

func eventObject(event corev1.Event) string {
	kind := normalizeEventKind(event.InvolvedObject.Kind)
	if kind == "" {
		kind = "object"
	}
	name := strings.TrimSpace(event.InvolvedObject.Name)
	if name == "" {
		name = "-"
	}
	return kind + "/" + name
}

func eventTimestamp(event corev1.Event) time.Time {
	if !event.EventTime.IsZero() {
		return event.EventTime.Time
	}
	if event.Series != nil && !event.Series.LastObservedTime.IsZero() {
		return event.Series.LastObservedTime.Time
	}
	if !event.LastTimestamp.IsZero() {
		return event.LastTimestamp.Time
	}
	if !event.FirstTimestamp.IsZero() {
		return event.FirstTimestamp.Time
	}
	return event.CreationTimestamp.Time
}
