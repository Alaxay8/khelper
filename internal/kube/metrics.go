package kube

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metricsclient "k8s.io/metrics/pkg/client/clientset/versioned"
)

var ErrMetricsUnavailable = errors.New("metrics API unavailable")

type PodMetricSummary struct {
	Name     string `json:"name"`
	CPUMilli int64  `json:"cpuMilli"`
	MemoryMi int64  `json:"memoryMi"`
}

type NodeMetricSummary struct {
	Name     string `json:"name"`
	CPUMilli int64  `json:"cpuMilli"`
	MemoryMi int64  `json:"memoryMi"`
}

func ListPodMetrics(ctx context.Context, client metricsclient.Interface, namespace string) ([]PodMetricSummary, error) {
	metricsNamespace := normalizeMetricsNamespace(namespace)
	metrics, err := client.MetricsV1beta1().PodMetricses(metricsNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, normalizeMetricsError(err)
	}

	out := make([]PodMetricSummary, 0, len(metrics.Items))
	for _, item := range metrics.Items {
		var cpuMilli int64
		var memBytes int64
		for _, c := range item.Containers {
			cpuMilli += c.Usage.Cpu().MilliValue()
			memBytes += c.Usage.Memory().Value()
		}
		out = append(out, PodMetricSummary{
			Name:     item.Name,
			CPUMilli: cpuMilli,
			MemoryMi: memBytes / (1024 * 1024),
		})
	}

	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})

	return out, nil
}

func ListNodeMetrics(ctx context.Context, client metricsclient.Interface) ([]NodeMetricSummary, error) {
	metrics, err := client.MetricsV1beta1().NodeMetricses().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, normalizeMetricsError(err)
	}

	out := make([]NodeMetricSummary, 0, len(metrics.Items))
	for _, item := range metrics.Items {
		out = append(out, NodeMetricSummary{
			Name:     item.Name,
			CPUMilli: item.Usage.Cpu().MilliValue(),
			MemoryMi: item.Usage.Memory().Value() / (1024 * 1024),
		})
	}

	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})

	return out, nil
}

func normalizeMetricsError(err error) error {
	if err == nil {
		return nil
	}

	if apierrors.IsNotFound(err) || apierrors.IsServiceUnavailable(err) {
		return fmt.Errorf("%w: %v", ErrMetricsUnavailable, err)
	}

	s := strings.ToLower(err.Error())
	if strings.Contains(s, "metrics.k8s.io") && strings.Contains(s, "not found") {
		return fmt.Errorf("%w: %v", ErrMetricsUnavailable, err)
	}
	if strings.Contains(s, "the server could not find the requested resource") {
		return fmt.Errorf("%w: %v", ErrMetricsUnavailable, err)
	}

	return fmt.Errorf("query metrics API: %w", err)
}

func normalizeMetricsNamespace(namespace string) string {
	if strings.TrimSpace(namespace) == NamespaceAll {
		return metav1.NamespaceAll
	}
	return namespace
}
