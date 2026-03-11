package kube

import (
	"context"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const ClearTargetEvicted = "evicted"

type ClearPodResult struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Reason    string `json:"reason"`
	Action    string `json:"action"`
}

type ClearResult struct {
	Target    string           `json:"target"`
	Namespace string           `json:"namespace"`
	DryRun    bool             `json:"dryRun"`
	Matched   int              `json:"matched"`
	Deleted   int              `json:"deleted"`
	Pods      []ClearPodResult `json:"pods,omitempty"`
}

func ListEvictedPods(ctx context.Context, client kubernetes.Interface, namespace string) ([]corev1.Pod, error) {
	scope := normalizeClearNamespace(namespace)
	listNamespace := scope
	if scope == NamespaceAll {
		listNamespace = metav1.NamespaceAll
	}

	podList, err := client.CoreV1().Pods(listNamespace).List(ctx, metav1.ListOptions{
		FieldSelector: "status.phase=Failed",
	})
	if err != nil {
		return nil, fmt.Errorf("list pods: %w", err)
	}

	evicted := make([]corev1.Pod, 0, len(podList.Items))
	for i := range podList.Items {
		pod := podList.Items[i]
		if !isEvictedPod(pod) {
			continue
		}
		evicted = append(evicted, pod)
	}

	sort.SliceStable(evicted, func(i, j int) bool {
		if evicted[i].Namespace == evicted[j].Namespace {
			return evicted[i].Name < evicted[j].Name
		}
		return evicted[i].Namespace < evicted[j].Namespace
	})

	return evicted, nil
}

func ClearEvictedPods(ctx context.Context, client kubernetes.Interface, namespace string, dryRun bool) (*ClearResult, error) {
	scope := normalizeClearNamespace(namespace)

	pods, err := ListEvictedPods(ctx, client, scope)
	if err != nil {
		return nil, err
	}

	result := &ClearResult{
		Target:    ClearTargetEvicted,
		Namespace: scope,
		DryRun:    dryRun,
		Matched:   len(pods),
		Pods:      make([]ClearPodResult, 0, len(pods)),
	}

	for i := range pods {
		pod := pods[i]
		entry := ClearPodResult{
			Namespace: pod.Namespace,
			Name:      pod.Name,
			Reason:    pod.Status.Reason,
		}

		if dryRun {
			entry.Action = "would-delete"
			result.Pods = append(result.Pods, entry)
			continue
		}

		if err := client.CoreV1().Pods(pod.Namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{}); err != nil {
			if apierrors.IsNotFound(err) {
				entry.Action = "already-gone"
				result.Pods = append(result.Pods, entry)
				continue
			}
			return nil, fmt.Errorf("delete pod %s/%s: %w", pod.Namespace, pod.Name, err)
		}

		entry.Action = "deleted"
		result.Deleted++
		result.Pods = append(result.Pods, entry)
	}

	return result, nil
}

func normalizeClearNamespace(namespace string) string {
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		return "default"
	}
	return namespace
}

func isEvictedPod(pod corev1.Pod) bool {
	return strings.EqualFold(strings.TrimSpace(pod.Status.Reason), "Evicted")
}
