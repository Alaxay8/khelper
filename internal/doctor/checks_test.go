package doctor

import (
	"strings"
	"testing"
	"time"

	"github.com/alaxay8/khelper/internal/kube"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestChecksHealthySnapshotHasNoIssues(t *testing.T) {
	t.Parallel()

	labels := map[string]string{"app": "payment"}
	snapshot := &Snapshot{
		Namespace: "shop",
		Workload:  kube.WorkloadRef{Kind: kube.KindDeployment, Name: "payment"},
		Deployment: &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "payment", Namespace: "shop", Labels: labels},
			Spec:       appsv1.DeploymentSpec{Replicas: int32Ptr(1)},
			Status:     appsv1.DeploymentStatus{ReadyReplicas: 1, AvailableReplicas: 1},
		},
		Pods: []corev1.Pod{healthyPod("shop", "payment-0")},
	}

	findings := Evaluate(snapshot, DefaultRules())
	if HasIssues(findings) {
		t.Fatalf("expected no issues for healthy snapshot, got %+v", findings)
	}
	if len(findings) != 0 {
		t.Fatalf("expected no findings, got %+v", findings)
	}
}

func TestChecksDetectCrashLoop(t *testing.T) {
	t.Parallel()

	pod := healthyPod("shop", "payment-0")
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{{
		Name:         "app",
		RestartCount: 6,
		State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{
			Reason:  "CrashLoopBackOff",
			Message: "back-off 5m0s restarting failed container",
		}},
	}}

	snapshot := &Snapshot{
		Namespace: "shop",
		Workload:  kube.WorkloadRef{Kind: kube.KindPod, Name: pod.Name},
		Pods:      []corev1.Pod{pod},
	}

	findings := Evaluate(snapshot, DefaultRules())
	if !hasFinding(findings, "container-state", SeverityError) {
		t.Fatalf("expected container-state error finding, got %+v", findings)
	}
}

func TestChecksDetectPendingUnschedulable(t *testing.T) {
	t.Parallel()

	pod := healthyPod("shop", "payment-0")
	pod.Status.Phase = corev1.PodPending
	pod.Status.Conditions = []corev1.PodCondition{{
		Type:    corev1.PodScheduled,
		Status:  corev1.ConditionFalse,
		Reason:  "Unschedulable",
		Message: "0/5 nodes are available: 5 Insufficient memory",
	}}

	event := corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Name: "payment-unsched", Namespace: "shop"},
		InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: pod.Name, Namespace: "shop"},
		Type:           corev1.EventTypeWarning,
		Reason:         "FailedScheduling",
		Message:        "0/5 nodes are available: 5 Insufficient memory",
	}

	snapshot := &Snapshot{
		Namespace: "shop",
		Workload:  kube.WorkloadRef{Kind: kube.KindPod, Name: pod.Name},
		Pods:      []corev1.Pod{pod},
		Events:    []corev1.Event{event},
	}

	findings := Evaluate(snapshot, DefaultRules())
	if !hasFinding(findings, "pending-unschedulable", SeverityError) {
		t.Fatalf("expected pending-unschedulable error finding, got %+v", findings)
	}
	if !hasFinding(findings, "warning-events", SeverityWarning) {
		t.Fatalf("expected warning-events finding, got %+v", findings)
	}
}

func TestChecksNoEventsDoesNotProduceWarningEventFinding(t *testing.T) {
	t.Parallel()

	snapshot := &Snapshot{
		Namespace: "shop",
		Workload:  kube.WorkloadRef{Kind: kube.KindPod, Name: "payment-0"},
		Pods:      []corev1.Pod{healthyPod("shop", "payment-0")},
		Events:    nil,
	}

	findings := Evaluate(snapshot, DefaultRules())
	if hasFinding(findings, "warning-events", SeverityWarning) {
		t.Fatalf("did not expect warning-events finding, got %+v", findings)
	}
}

func TestChecksDetectImagePullBackOffInInitContainer(t *testing.T) {
	t.Parallel()

	pod := healthyPod("shop", "payment-0")
	pod.Status.InitContainerStatuses = []corev1.ContainerStatus{{
		Name:         "init-config",
		RestartCount: 1,
		State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{
			Reason:  "ImagePullBackOff",
			Message: "Back-off pulling image",
		}},
	}}

	snapshot := &Snapshot{
		Namespace: "shop",
		Workload:  kube.WorkloadRef{Kind: kube.KindPod, Name: pod.Name},
		Pods:      []corev1.Pod{pod},
	}

	findings := Evaluate(snapshot, DefaultRules())
	finding, ok := findFinding(findings, "container-state", SeverityError)
	if !ok {
		t.Fatalf("expected container-state error finding, got %+v", findings)
	}
	if !strings.Contains(finding.Action, "imagePullSecrets") {
		t.Fatalf("expected image pull action guidance, got %q", finding.Action)
	}
}

func TestChecksDetectOOMKilledFromLastTerminationState(t *testing.T) {
	t.Parallel()

	pod := healthyPod("shop", "payment-0")
	finishedAt := metav1.NewTime(time.Now().UTC().Add(-3 * time.Minute))
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{{
		Name:         "app",
		Ready:        true,
		RestartCount: restartWarningThreshold,
		State:        corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
		LastTerminationState: corev1.ContainerState{
			Terminated: &corev1.ContainerStateTerminated{
				Reason:     "OOMKilled",
				ExitCode:   137,
				FinishedAt: finishedAt,
			},
		},
	}}

	snapshot := &Snapshot{
		Namespace: "shop",
		Workload:  kube.WorkloadRef{Kind: kube.KindPod, Name: pod.Name},
		Pods:      []corev1.Pod{pod},
	}

	findings := Evaluate(snapshot, DefaultRules())
	if !hasFinding(findings, "oom-killed", SeverityError) {
		t.Fatalf("expected oom-killed error finding, got %+v", findings)
	}
	if !hasFinding(findings, "frequent-restarts", SeverityWarning) {
		t.Fatalf("expected frequent-restarts warning finding, got %+v", findings)
	}
}

func TestChecksDetectProbeFailureFromUnhealthyEvent(t *testing.T) {
	t.Parallel()

	event := corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Name: "payment-probe", Namespace: "shop"},
		InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "payment-0", Namespace: "shop"},
		Type:           corev1.EventTypeWarning,
		Reason:         "Unhealthy",
		Message:        "Readiness probe failed: timeout",
	}

	snapshot := &Snapshot{
		Namespace: "shop",
		Workload:  kube.WorkloadRef{Kind: kube.KindPod, Name: "payment-0"},
		Events:    []corev1.Event{event},
	}

	findings := Evaluate(snapshot, DefaultRules())
	if !hasFinding(findings, "probe-failure", SeverityWarning) {
		t.Fatalf("expected probe-failure warning finding, got %+v", findings)
	}
}

func healthyPod(namespace, name string) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "nginx"}}},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{{
				Name:         "app",
				Ready:        true,
				RestartCount: 0,
				State:        corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
			}},
		},
	}
}

func hasFinding(findings []Finding, check string, severity Severity) bool {
	for _, finding := range findings {
		if finding.Check == check && finding.Severity == severity {
			return true
		}
	}
	return false
}

func findFinding(findings []Finding, check string, severity Severity) (Finding, bool) {
	for _, finding := range findings {
		if finding.Check == check && finding.Severity == severity {
			return finding, true
		}
	}
	return Finding{}, false
}
