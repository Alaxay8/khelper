package doctor

import (
	"fmt"
	"strings"

	"github.com/alaxay8/khelper/internal/kube"
	corev1 "k8s.io/api/core/v1"
)

const restartWarningThreshold int32 = 3

func checkContainerWaitingState(snapshot *Snapshot) []Finding {
	findings := make([]Finding, 0)
	reasons := map[string]struct{}{
		"CrashLoopBackOff": {},
		"ImagePullBackOff": {},
		"ErrImagePull":     {},
	}

	for _, pod := range snapshot.Pods {
		for _, status := range pod.Status.InitContainerStatuses {
			findings = append(findings, waitingStateFinding(snapshot, pod, status, reasons)...)
		}
		for _, status := range pod.Status.ContainerStatuses {
			findings = append(findings, waitingStateFinding(snapshot, pod, status, reasons)...)
		}
	}

	return findings
}

func waitingStateFinding(snapshot *Snapshot, pod corev1.Pod, status corev1.ContainerStatus, reasons map[string]struct{}) []Finding {
	if status.State.Waiting == nil {
		return nil
	}
	reason := status.State.Waiting.Reason
	if _, ok := reasons[reason]; !ok {
		return nil
	}

	action := "Inspect logs and recent config/image changes for this container"
	switch reason {
	case "ImagePullBackOff", "ErrImagePull":
		action = "Verify image name/tag, registry reachability, and imagePullSecrets"
	case "CrashLoopBackOff":
		action = "Inspect container logs and startup config to fix repeated crashes"
	}

	evidence := map[string]any{
		"pod":          pod.Name,
		"container":    status.Name,
		"reason":       reason,
		"restartCount": status.RestartCount,
	}
	if status.State.Waiting.Message != "" {
		evidence["stateMessage"] = normalizeSpace(status.State.Waiting.Message)
	}
	evidence = appendLogEvidence(snapshot, pod.Name, status.Name, evidence)

	message := fmt.Sprintf("Container %s is waiting with %s", status.Name, reason)
	if status.State.Waiting.Message != "" {
		message += ": " + normalizeSpace(status.State.Waiting.Message)
	}

	return []Finding{{
		Severity: SeverityError,
		Check:    "container-state",
		Object:   fmt.Sprintf("pod/%s", pod.Name),
		Message:  message,
		Action:   action,
		Evidence: evidence,
	}}
}

func checkOOMAndRestarts(snapshot *Snapshot) []Finding {
	findings := make([]Finding, 0)

	for _, pod := range snapshot.Pods {
		statuses := make([]corev1.ContainerStatus, 0, len(pod.Status.InitContainerStatuses)+len(pod.Status.ContainerStatuses))
		statuses = append(statuses, pod.Status.InitContainerStatuses...)
		statuses = append(statuses, pod.Status.ContainerStatuses...)

		for _, status := range statuses {
			if terminated := oomKilledState(status); terminated != nil {
				evidence := map[string]any{
					"pod":          pod.Name,
					"container":    status.Name,
					"reason":       terminated.Reason,
					"exitCode":     terminated.ExitCode,
					"restartCount": status.RestartCount,
				}
				if !terminated.FinishedAt.IsZero() {
					evidence["finishedAt"] = terminated.FinishedAt.Time.UTC().Format(timeLayout)
				}
				evidence = appendLogEvidence(snapshot, pod.Name, status.Name, evidence)

				findings = append(findings, Finding{
					Severity: SeverityError,
					Check:    "oom-killed",
					Object:   fmt.Sprintf("pod/%s", pod.Name),
					Message:  fmt.Sprintf("Container %s was OOMKilled", status.Name),
					Action:   "Increase memory limits/requests or reduce memory usage in the container",
					Evidence: evidence,
				})
			}

			if status.RestartCount >= restartWarningThreshold {
				evidence := map[string]any{
					"pod":          pod.Name,
					"container":    status.Name,
					"restartCount": status.RestartCount,
					"threshold":    restartWarningThreshold,
				}
				evidence = appendLogEvidence(snapshot, pod.Name, status.Name, evidence)

				findings = append(findings, Finding{
					Severity: SeverityWarning,
					Check:    "frequent-restarts",
					Object:   fmt.Sprintf("pod/%s", pod.Name),
					Message:  fmt.Sprintf("Container %s restarted %d times", status.Name, status.RestartCount),
					Action:   "Inspect logs and probe settings to stabilize startup/runtime behavior",
					Evidence: evidence,
				})
			}
		}
	}

	return findings
}

func oomKilledState(status corev1.ContainerStatus) *corev1.ContainerStateTerminated {
	if status.State.Terminated != nil && status.State.Terminated.Reason == "OOMKilled" {
		return status.State.Terminated
	}
	if status.LastTerminationState.Terminated != nil && status.LastTerminationState.Terminated.Reason == "OOMKilled" {
		return status.LastTerminationState.Terminated
	}
	return nil
}

func checkPendingUnschedulable(snapshot *Snapshot) []Finding {
	findings := make([]Finding, 0)

	for _, pod := range snapshot.Pods {
		if pod.Status.Phase != corev1.PodPending {
			continue
		}

		unschedulable := findPodCondition(pod.Status.Conditions, corev1.PodScheduled, corev1.ConditionFalse)
		if unschedulable != nil && strings.EqualFold(unschedulable.Reason, "Unschedulable") {
			evidence := map[string]any{
				"pod":              pod.Name,
				"phase":            string(pod.Status.Phase),
				"condition":        string(corev1.PodScheduled),
				"conditionReason":  unschedulable.Reason,
				"conditionMessage": normalizeSpace(unschedulable.Message),
			}
			if !unschedulable.LastProbeTime.IsZero() {
				evidence["conditionLastProbe"] = unschedulable.LastProbeTime.Time.UTC().Format(timeLayout)
			}
			findings = append(findings, Finding{
				Severity: SeverityError,
				Check:    "pending-unschedulable",
				Object:   fmt.Sprintf("pod/%s", pod.Name),
				Message:  fmt.Sprintf("Pod is Pending and Unschedulable: %s", normalizeSpace(unschedulable.Message)),
				Action:   "Check node resources, taints/tolerations, node selectors, and affinity constraints",
				Evidence: evidence,
			})
			continue
		}

		findings = append(findings, Finding{
			Severity: SeverityWarning,
			Check:    "pending-pod",
			Object:   fmt.Sprintf("pod/%s", pod.Name),
			Message:  "Pod is Pending and not yet Ready",
			Action:   "Inspect scheduling and container startup events for this pod",
			Evidence: map[string]any{"pod": pod.Name, "phase": string(pod.Status.Phase)},
		})
	}

	return findings
}

func findPodCondition(conditions []corev1.PodCondition, conditionType corev1.PodConditionType, status corev1.ConditionStatus) *corev1.PodCondition {
	for i := range conditions {
		condition := &conditions[i]
		if condition.Type == conditionType && condition.Status == status {
			return condition
		}
	}
	return nil
}

func checkProbeFailures(snapshot *Snapshot) []Finding {
	findings := make([]Finding, 0)

	for _, pod := range snapshot.Pods {
		for _, status := range pod.Status.ContainerStatuses {
			// Running-but-not-ready is a strong status signal for readiness/startup probe issues.
			if status.State.Running == nil || status.Ready {
				continue
			}

			message := fmt.Sprintf("Container %s is running but not Ready", status.Name)
			if status.Started != nil && !*status.Started {
				message = fmt.Sprintf("Container %s has not started successfully (startup probe may be failing)", status.Name)
			}

			evidence := map[string]any{
				"pod":       pod.Name,
				"container": status.Name,
				"ready":     status.Ready,
			}
			if status.Started != nil {
				evidence["started"] = *status.Started
			}
			evidence = appendLogEvidence(snapshot, pod.Name, status.Name, evidence)

			findings = append(findings, Finding{
				Severity: SeverityWarning,
				Check:    "probe-failure",
				Object:   fmt.Sprintf("pod/%s", pod.Name),
				Message:  message,
				Action:   "Review readiness/startup probe endpoints, thresholds, and initial delays",
				Evidence: evidence,
			})
		}
	}

	for _, event := range snapshot.Events {
		if !isWarningEvent(event) {
			continue
		}

		messageLower := strings.ToLower(event.Message)
		reasonLower := strings.ToLower(event.Reason)
		if !(strings.Contains(messageLower, "liveness probe") ||
			strings.Contains(messageLower, "readiness probe") ||
			strings.Contains(messageLower, "startup probe") ||
			strings.Contains(messageLower, "probe failed") ||
			reasonLower == "unhealthy") {
			continue
		}

		evidence := eventEvidence(event)
		evidence = appendLogEvidence(snapshot, event.InvolvedObject.Name, "", evidence)
		findings = append(findings, Finding{
			Severity: SeverityWarning,
			Check:    "probe-failure",
			Object:   eventObject(event),
			Message:  normalizeSpace(event.Reason + ": " + event.Message),
			Action:   "Review liveness/readiness/startup probe endpoints, timeout, and initial delay",
			Evidence: evidence,
		})
	}

	return findings
}

func checkWorkloadReplicas(snapshot *Snapshot) []Finding {
	switch snapshot.Workload.Kind {
	case kube.KindDeployment:
		return checkDeploymentReplicas(snapshot)
	case kube.KindStatefulSet:
		return checkStatefulSetReplicas(snapshot)
	default:
		return nil
	}
}

func checkDeploymentReplicas(snapshot *Snapshot) []Finding {
	dep := snapshot.Deployment
	if dep == nil {
		return nil
	}

	desired := replicasOrDefault(dep.Spec.Replicas, 1)
	if desired == 0 {
		return nil
	}

	ready := dep.Status.ReadyReplicas
	available := dep.Status.AvailableReplicas
	if ready == 0 || available == 0 {
		return []Finding{{
			Severity: SeverityError,
			Check:    "workload-replicas",
			Object:   deploymentObject(dep),
			Message:  fmt.Sprintf("Deployment has no healthy replicas (ready=%d available=%d desired=%d)", ready, available, desired),
			Action:   "Inspect pod failures, rollout status, and warning events for this deployment",
			Evidence: map[string]any{"desired": desired, "ready": ready, "available": available},
		}}
	}

	if ready < desired || available < desired {
		return []Finding{{
			Severity: SeverityWarning,
			Check:    "workload-replicas",
			Object:   deploymentObject(dep),
			Message:  fmt.Sprintf("Deployment replicas are below desired state (ready=%d available=%d desired=%d)", ready, available, desired),
			Action:   "Inspect rollout progress and pod readiness conditions",
			Evidence: map[string]any{"desired": desired, "ready": ready, "available": available},
		}}
	}

	return nil
}

func checkStatefulSetReplicas(snapshot *Snapshot) []Finding {
	sts := snapshot.StatefulSet
	if sts == nil {
		return nil
	}

	desired := replicasOrDefault(sts.Spec.Replicas, 1)
	if desired == 0 {
		return nil
	}

	ready := sts.Status.ReadyReplicas
	if ready == 0 {
		return []Finding{{
			Severity: SeverityError,
			Check:    "workload-replicas",
			Object:   statefulSetObject(sts),
			Message:  fmt.Sprintf("StatefulSet has no ready replicas (ready=%d desired=%d)", ready, desired),
			Action:   "Inspect pod failures, PVC binding, and rollout conditions",
			Evidence: map[string]any{"desired": desired, "ready": ready},
		}}
	}

	if ready < desired {
		return []Finding{{
			Severity: SeverityWarning,
			Check:    "workload-replicas",
			Object:   statefulSetObject(sts),
			Message:  fmt.Sprintf("StatefulSet replicas are below desired state (ready=%d desired=%d)", ready, desired),
			Action:   "Inspect rollout progress and pod readiness conditions",
			Evidence: map[string]any{"desired": desired, "ready": ready},
		}}
	}

	return nil
}

func checkWarningEvents(snapshot *Snapshot) []Finding {
	findings := make([]Finding, 0)
	for _, event := range snapshot.Events {
		if !isWarningEvent(event) {
			continue
		}

		findings = append(findings, Finding{
			Severity: SeverityWarning,
			Check:    "warning-events",
			Object:   eventObject(event),
			Message:  normalizeSpace(event.Reason + ": " + event.Message),
			Action:   "Review this warning event and correlate with pod/workload status",
			Evidence: eventEvidence(event),
		})
	}

	return findings
}

func isWarningEvent(event corev1.Event) bool {
	return strings.EqualFold(strings.TrimSpace(event.Type), corev1.EventTypeWarning)
}

func eventEvidence(event corev1.Event) map[string]any {
	evidence := map[string]any{
		"reason":  event.Reason,
		"type":    event.Type,
		"message": normalizeSpace(event.Message),
		"count":   event.Count,
	}

	timestamp := eventTimestamp(event)
	if !timestamp.IsZero() {
		evidence["timestamp"] = timestamp.UTC().Format(timeLayout)
	}

	if event.FirstTimestamp.Time.Unix() > 0 {
		evidence["firstTimestamp"] = event.FirstTimestamp.Time.UTC().Format(timeLayout)
	}
	if event.LastTimestamp.Time.Unix() > 0 {
		evidence["lastTimestamp"] = event.LastTimestamp.Time.UTC().Format(timeLayout)
	}
	if event.Series != nil && !event.Series.LastObservedTime.IsZero() {
		evidence["lastObserved"] = event.Series.LastObservedTime.Time.UTC().Format(timeLayout)
	}

	return evidence
}

func eventObject(event corev1.Event) string {
	kind := strings.ToLower(strings.TrimSpace(event.InvolvedObject.Kind))
	if kind == "" {
		kind = "object"
	}
	if event.InvolvedObject.Name == "" {
		return kind
	}
	return kind + "/" + event.InvolvedObject.Name
}

func appendLogEvidence(snapshot *Snapshot, podName, containerName string, evidence map[string]any) map[string]any {
	if snapshot == nil || snapshot.LogSnippet == nil {
		return evidence
	}
	if snapshot.LogSnippet.Pod != podName {
		return evidence
	}
	if containerName != "" && snapshot.LogSnippet.Container != "" && snapshot.LogSnippet.Container != containerName {
		return evidence
	}

	if evidence == nil {
		evidence = map[string]any{}
	}
	evidence["logPod"] = snapshot.LogSnippet.Pod
	evidence["logContainer"] = snapshot.LogSnippet.Container
	evidence["logTail"] = snapshot.LogSnippet.Tail
	if snapshot.LogSnippet.Text != "" {
		evidence["logExcerpt"] = snapshot.LogSnippet.Text
	}
	if snapshot.LogSnippet.Error != "" {
		evidence["logError"] = snapshot.LogSnippet.Error
	}
	return evidence
}

const timeLayout = "2006-01-02T15:04:05Z"
