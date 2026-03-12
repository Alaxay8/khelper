package kube

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8stesting "k8s.io/client-go/testing"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	metricsfake "k8s.io/metrics/pkg/client/clientset/versioned/fake"
)

func TestListPodMetricsNormalizesNamespaceAllMarker(t *testing.T) {
	t.Parallel()

	client := metricsfake.NewSimpleClientset()
	gotNamespace := ""
	client.PrependReactor("list", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		gotNamespace = action.GetNamespace()
		return true, &metricsv1beta1.PodMetricsList{}, nil
	})

	if _, err := ListPodMetrics(context.Background(), client, NamespaceAll); err != nil {
		t.Fatalf("ListPodMetrics returned error: %v", err)
	}

	if gotNamespace != metav1.NamespaceAll {
		t.Fatalf("expected metrics list namespace %q, got %q", metav1.NamespaceAll, gotNamespace)
	}
}

func TestListPodMetricsKeepsExplicitNamespace(t *testing.T) {
	t.Parallel()

	client := metricsfake.NewSimpleClientset()
	gotNamespace := ""
	client.PrependReactor("list", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		gotNamespace = action.GetNamespace()
		return true, &metricsv1beta1.PodMetricsList{}, nil
	})

	if _, err := ListPodMetrics(context.Background(), client, "shop"); err != nil {
		t.Fatalf("ListPodMetrics returned error: %v", err)
	}

	if gotNamespace != "shop" {
		t.Fatalf("expected metrics list namespace %q, got %q", "shop", gotNamespace)
	}
}
