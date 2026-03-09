package kube

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestListEvictedPodsFiltersAndSorts(t *testing.T) {
	t.Parallel()

	client := fake.NewSimpleClientset(
		testPodWithReason("shop", "payment-2", "Evicted"),
		testPodWithReason("shop", "payment-1", "Evicted"),
		testPodWithReason("ops", "cache-1", "Running"),
		testPodWithReason("ops", "cache-0", "Evicted"),
	)

	pods, err := ListEvictedPods(context.Background(), client, NamespaceAll)
	if err != nil {
		t.Fatalf("ListEvictedPods returned error: %v", err)
	}

	if len(pods) != 3 {
		t.Fatalf("expected 3 evicted pods, got %d", len(pods))
	}

	want := []struct {
		namespace string
		name      string
	}{
		{namespace: "ops", name: "cache-0"},
		{namespace: "shop", name: "payment-1"},
		{namespace: "shop", name: "payment-2"},
	}

	for i := range want {
		if pods[i].Namespace != want[i].namespace || pods[i].Name != want[i].name {
			t.Fatalf("unexpected pod at index %d: got %s/%s, want %s/%s", i, pods[i].Namespace, pods[i].Name, want[i].namespace, want[i].name)
		}
	}
}

func TestClearEvictedPodsDryRunDoesNotDelete(t *testing.T) {
	t.Parallel()

	client := fake.NewSimpleClientset(
		testPodWithReason("shop", "payment-evicted", "Evicted"),
		testPodWithReason("shop", "payment-running", "Running"),
	)

	result, err := ClearEvictedPods(context.Background(), client, "shop", true)
	if err != nil {
		t.Fatalf("ClearEvictedPods returned error: %v", err)
	}

	if result.Target != "evicted" {
		t.Fatalf("expected target evicted, got %q", result.Target)
	}
	if !result.DryRun {
		t.Fatal("expected dry-run result")
	}
	if result.Matched != 1 {
		t.Fatalf("expected 1 matched pod, got %d", result.Matched)
	}
	if result.Deleted != 0 {
		t.Fatalf("expected 0 deleted pods in dry-run, got %d", result.Deleted)
	}
	if len(result.Pods) != 1 {
		t.Fatalf("expected 1 pod result, got %d", len(result.Pods))
	}
	if result.Pods[0].Action != "would-delete" {
		t.Fatalf("expected action would-delete, got %q", result.Pods[0].Action)
	}

	podList, err := client.CoreV1().Pods("shop").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list pods after dry-run: %v", err)
	}
	if len(podList.Items) != 2 {
		t.Fatalf("expected 2 pods to remain after dry-run, got %d", len(podList.Items))
	}
}

func TestClearEvictedPodsDeletesPods(t *testing.T) {
	t.Parallel()

	client := fake.NewSimpleClientset(
		testPodWithReason("shop", "payment-evicted", "Evicted"),
		testPodWithReason("shop", "payment-running", "Running"),
	)

	result, err := ClearEvictedPods(context.Background(), client, "shop", false)
	if err != nil {
		t.Fatalf("ClearEvictedPods returned error: %v", err)
	}

	if result.DryRun {
		t.Fatal("expected non-dry-run result")
	}
	if result.Matched != 1 {
		t.Fatalf("expected 1 matched pod, got %d", result.Matched)
	}
	if result.Deleted != 1 {
		t.Fatalf("expected 1 deleted pod, got %d", result.Deleted)
	}
	if len(result.Pods) != 1 {
		t.Fatalf("expected 1 pod result, got %d", len(result.Pods))
	}
	if result.Pods[0].Action != "deleted" {
		t.Fatalf("expected action deleted, got %q", result.Pods[0].Action)
	}

	podList, err := client.CoreV1().Pods("shop").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list pods after delete: %v", err)
	}
	if len(podList.Items) != 1 {
		t.Fatalf("expected 1 pod to remain after delete, got %d", len(podList.Items))
	}
	if podList.Items[0].Name != "payment-running" {
		t.Fatalf("expected payment-running to remain, got %s", podList.Items[0].Name)
	}
}

func TestClearEvictedPodsDoesNotCountAlreadyGoneAsDeleted(t *testing.T) {
	t.Parallel()

	client := fake.NewSimpleClientset(
		testPodWithReason("shop", "payment-evicted", "Evicted"),
	)
	client.PrependReactor("delete", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		deleteAction, ok := action.(k8stesting.DeleteAction)
		if !ok {
			t.Fatalf("unexpected action type %T", action)
		}
		return true, nil, apierrors.NewNotFound(schema.GroupResource{Resource: "pods"}, deleteAction.GetName())
	})

	result, err := ClearEvictedPods(context.Background(), client, "shop", false)
	if err != nil {
		t.Fatalf("ClearEvictedPods returned error: %v", err)
	}

	if result.Matched != 1 {
		t.Fatalf("expected 1 matched pod, got %d", result.Matched)
	}
	if result.Deleted != 0 {
		t.Fatalf("expected 0 deleted pods for already-gone pod, got %d", result.Deleted)
	}
	if len(result.Pods) != 1 {
		t.Fatalf("expected 1 pod result, got %d", len(result.Pods))
	}
	if result.Pods[0].Action != "already-gone" {
		t.Fatalf("expected action already-gone, got %q", result.Pods[0].Action)
	}
}

func testPodWithReason(namespace, name, reason string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Status: corev1.PodStatus{
			Reason: reason,
		},
	}
}
