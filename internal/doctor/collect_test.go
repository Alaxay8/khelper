package doctor

import (
	"context"
	"testing"
	"time"

	"github.com/alaxay8/khelper/internal/kube"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestCollectResolvesDeploymentAndFiltersRelatedEvents(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	namespace := "shop"
	labels := map[string]string{"app": "payment"}
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "payment", Namespace: namespace, Labels: labels},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(2),
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "nginx"}}},
			},
		},
		Status: appsv1.DeploymentStatus{ReadyReplicas: 1, AvailableReplicas: 1},
	}

	oldStart := metav1.NewTime(now.Add(-10 * time.Minute))
	newStart := metav1.NewTime(now.Add(-2 * time.Minute))
	podOld := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "payment-old", Namespace: namespace, Labels: labels, UID: "pod-old", CreationTimestamp: metav1.NewTime(now.Add(-10 * time.Minute))},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "nginx"}}},
		Status: corev1.PodStatus{
			Phase:     corev1.PodRunning,
			StartTime: &oldStart,
			ContainerStatuses: []corev1.ContainerStatus{{
				Name:  "app",
				Ready: true,
			}},
		},
	}
	podNew := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "payment-new", Namespace: namespace, Labels: labels, UID: "pod-new", CreationTimestamp: metav1.NewTime(now.Add(-2 * time.Minute))},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "nginx"}}},
		Status: corev1.PodStatus{
			Phase:     corev1.PodRunning,
			StartTime: &newStart,
			ContainerStatuses: []corev1.ContainerStatus{{
				Name:  "app",
				Ready: true,
			}},
		},
	}

	recentEvent := &corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Name: "payment-recent", Namespace: namespace},
		InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "payment-new", UID: "pod-new", Namespace: namespace},
		Type:           corev1.EventTypeWarning,
		Reason:         "BackOff",
		Message:        "Back-off restarting failed container",
		LastTimestamp:  metav1.NewTime(now.Add(-5 * time.Minute)),
	}
	oldEvent := &corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Name: "payment-old-event", Namespace: namespace},
		InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "payment-old", UID: "pod-old", Namespace: namespace},
		Type:           corev1.EventTypeWarning,
		Reason:         "BackOff",
		Message:        "Back-off restarting failed container",
		LastTimestamp:  metav1.NewTime(now.Add(-3 * time.Hour)),
	}
	otherEvent := &corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Name: "other-workload", Namespace: namespace},
		InvolvedObject: corev1.ObjectReference{Kind: "Deployment", Name: "checkout", Namespace: namespace},
		Type:           corev1.EventTypeWarning,
		Reason:         "ScalingReplicaSet",
		Message:        "Scaled up",
		LastTimestamp:  metav1.NewTime(now.Add(-2 * time.Minute)),
	}

	client := fake.NewSimpleClientset(dep, podOld, podNew, recentEvent, oldEvent, otherEvent)
	bundle := &kube.ClientBundle{Clientset: client, Namespace: namespace}

	snapshot, err := Collect(context.Background(), bundle, "payment", CollectOptions{
		Since:    time.Hour,
		LogsTail: 0,
		Now:      now,
	})
	if err != nil {
		t.Fatalf("collect returned error: %v", err)
	}

	if snapshot.Workload.Kind != kube.KindDeployment {
		t.Fatalf("expected resolved kind %q, got %q", kube.KindDeployment, snapshot.Workload.Kind)
	}
	if snapshot.Workload.Name != "payment" {
		t.Fatalf("expected resolved workload name payment, got %q", snapshot.Workload.Name)
	}
	if len(snapshot.Pods) != 2 {
		t.Fatalf("expected 2 pods in snapshot, got %d", len(snapshot.Pods))
	}
	if snapshot.SelectedPod == nil || snapshot.SelectedPod.Name != "payment-new" {
		t.Fatalf("expected selected pod payment-new, got %+v", snapshot.SelectedPod)
	}
	if len(snapshot.Events) != 1 {
		t.Fatalf("expected 1 related event after since-filter, got %d", len(snapshot.Events))
	}
	if snapshot.Events[0].Name != "payment-recent" {
		t.Fatalf("expected recent payment event, got %s", snapshot.Events[0].Name)
	}
}

func int32Ptr(value int32) *int32 {
	return &value
}
