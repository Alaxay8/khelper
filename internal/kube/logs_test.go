package kube

import (
	"bytes"
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSelectLogContainersDefaultsToSingleContainer(t *testing.T) {
	t.Parallel()

	pod := testPodWithContainers("payment-0", "app")

	containers, err := selectLogContainers(pod, "", false)
	if err != nil {
		t.Fatalf("selectLogContainers returned error: %v", err)
	}
	if len(containers) != 1 || containers[0] != "app" {
		t.Fatalf("unexpected containers: %+v", containers)
	}
}

func TestSelectLogContainersErrorsForMultipleContainersWithoutSelection(t *testing.T) {
	t.Parallel()

	pod := testPodWithContainers("payment-0", "app", "sidecar")

	if _, err := selectLogContainers(pod, "", false); err == nil {
		t.Fatal("expected error for multi-container pod without --container/--all-containers")
	}
}

func TestSelectLogContainersReturnsAllWhenRequested(t *testing.T) {
	t.Parallel()

	pod := testPodWithContainers("payment-0", "app", "sidecar")

	containers, err := selectLogContainers(pod, "", true)
	if err != nil {
		t.Fatalf("selectLogContainers returned error: %v", err)
	}
	if len(containers) != 2 || containers[0] != "app" || containers[1] != "sidecar" {
		t.Fatalf("unexpected containers: %+v", containers)
	}
}

func TestSelectLogContainersRejectsUnknownContainer(t *testing.T) {
	t.Parallel()

	pod := testPodWithContainers("payment-0", "app")

	if _, err := selectLogContainers(pod, "worker", false); err == nil {
		t.Fatal("expected error for unknown container")
	}
}

func TestPodHasContainer(t *testing.T) {
	t.Parallel()

	pod := testPodWithContainers("payment-0", "app", "sidecar")

	if !PodHasContainer(pod, "app") {
		t.Fatal("expected app container to exist")
	}
	if PodHasContainer(pod, "worker") {
		t.Fatal("did not expect worker container to exist")
	}
}

func TestCopyLogStreamWithoutPrefix(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	if err := copyLogStream(strings.NewReader("line1\nline2\n"), &out, ""); err != nil {
		t.Fatalf("copyLogStream returned error: %v", err)
	}
	if got := out.String(); got != "line1\nline2\n" {
		t.Fatalf("unexpected output: %q", got)
	}
}

func TestCopyLogStreamWithPrefix(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	if err := copyLogStream(strings.NewReader("line1\nline2\nlast"), &out, "[pod/app] "); err != nil {
		t.Fatalf("copyLogStream returned error: %v", err)
	}
	if got := out.String(); got != "[pod/app] line1\n[pod/app] line2\n[pod/app] last" {
		t.Fatalf("unexpected output: %q", got)
	}
}

func TestStreamPodLogsValidatesInputs(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	if err := StreamPodLogs(context.Background(), nil, nil, PodLogsOptions{Out: &out}); err == nil {
		t.Fatal("expected error for nil pod")
	}

	pod := testPodWithContainers("payment-0", "app")
	if err := StreamPodLogs(context.Background(), nil, pod, PodLogsOptions{}); err == nil {
		t.Fatal("expected error for nil output writer")
	}
}

func testPodWithContainers(name string, containerNames ...string) *corev1.Pod {
	containers := make([]corev1.Container, 0, len(containerNames))
	for _, containerName := range containerNames {
		containers = append(containers, corev1.Container{Name: containerName, Image: "nginx"})
	}

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "shop"},
		Spec: corev1.PodSpec{
			Containers: containers,
		},
	}
}
