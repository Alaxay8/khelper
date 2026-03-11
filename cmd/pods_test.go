package cmd

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
)

func TestPodStatusDoesNotReportCompletedForFinishedInitContainer(t *testing.T) {
	t.Parallel()

	pod := corev1.Pod{
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			InitContainerStatuses: []corev1.ContainerStatus{{
				Name: "init",
				State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
					Reason:   "Completed",
					ExitCode: 0,
				}},
			}},
			ContainerStatuses: []corev1.ContainerStatus{{
				Name:  "app",
				Ready: true,
				State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
			}},
		},
	}

	if got := podStatus(pod); got != "Running" {
		t.Fatalf("expected Running status, got %q", got)
	}
}

func TestHumanDurationSinceFutureTimestampIsClamped(t *testing.T) {
	t.Parallel()

	if got := humanDurationSince(time.Now().Add(10 * time.Second)); got != "0s" {
		t.Fatalf("expected clamped future age 0s, got %q", got)
	}
}
