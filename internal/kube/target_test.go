package kube

import (
	"context"
	"errors"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestResolveWorkloadPrefersNameMatch(t *testing.T) {
	t.Parallel()

	client := fake.NewSimpleClientset(
		newDeployment("shop", "payment", map[string]string{"app": "not-payment"}),
		newDeployment("shop", "payment-v2", map[string]string{"app": "payment"}),
	)

	resolver := NewResolver(client)
	ref, err := resolver.ResolveWorkload(context.Background(), "shop", "payment", "", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ref.Kind != KindDeployment {
		t.Fatalf("expected kind %s, got %s", KindDeployment, ref.Kind)
	}
	if ref.Name != "payment" {
		t.Fatalf("expected deployment payment, got %s", ref.Name)
	}
	if ref.MatchRule != "name" {
		t.Fatalf("expected name match rule, got %s", ref.MatchRule)
	}
}

func TestResolveWorkloadDefaultKindPriority(t *testing.T) {
	t.Parallel()

	client := fake.NewSimpleClientset(
		newStatefulSet("shop", "payment-db", map[string]string{"app": "billing"}),
		newPod("shop", "billing-pod", map[string]string{"app": "billing"}, corev1.PodRunning, ptrTime(time.Now().Add(-time.Minute)), time.Now().Add(-time.Minute)),
		newDeployment("shop", "billing-api", map[string]string{"app": "billing"}),
	)

	resolver := NewResolver(client)
	ref, err := resolver.ResolveWorkload(context.Background(), "shop", "billing", "", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ref.Kind != KindDeployment {
		t.Fatalf("expected deployment to win by default priority, got %s", ref.Kind)
	}
	if ref.Name != "billing-api" {
		t.Fatalf("expected billing-api deployment, got %s", ref.Name)
	}
}

func TestResolveWorkloadAmbiguousRequiresPick(t *testing.T) {
	t.Parallel()

	client := fake.NewSimpleClientset(
		newDeployment("shop", "payment-a", map[string]string{"app": "payment"}),
		newDeployment("shop", "payment-b", map[string]string{"app": "payment"}),
	)

	resolver := NewResolver(client)
	_, err := resolver.ResolveWorkload(context.Background(), "shop", "payment", KindDeployment, 0)
	if err == nil {
		t.Fatal("expected ambiguity error")
	}

	var amb *AmbiguousMatchError
	if !errors.As(err, &amb) {
		t.Fatalf("expected AmbiguousMatchError, got %T (%v)", err, err)
	}

	ref, err := resolver.ResolveWorkload(context.Background(), "shop", "payment", KindDeployment, 2)
	if err != nil {
		t.Fatalf("unexpected pick error: %v", err)
	}
	if ref.Name != "payment-b" {
		t.Fatalf("expected pick=2 to return payment-b, got %s", ref.Name)
	}
}

func TestResolveWorkloadAllNamespacesAmbiguousRequiresPick(t *testing.T) {
	t.Parallel()

	client := fake.NewSimpleClientset(
		newDeployment("alpha", "frontend", map[string]string{"app": "frontend-alpha"}),
		newDeployment("shop", "frontend", map[string]string{"app": "frontend-shop"}),
	)

	resolver := NewResolver(client)
	_, err := resolver.ResolveWorkload(context.Background(), NamespaceAll, "frontend", KindDeployment, 0)
	if err == nil {
		t.Fatal("expected ambiguity error across namespaces")
	}

	var amb *AmbiguousMatchError
	if !errors.As(err, &amb) {
		t.Fatalf("expected AmbiguousMatchError, got %T (%v)", err, err)
	}

	ref, err := resolver.ResolveWorkload(context.Background(), NamespaceAll, "frontend", KindDeployment, 2)
	if err != nil {
		t.Fatalf("unexpected pick error: %v", err)
	}
	if ref.Namespace != "shop" {
		t.Fatalf("expected pick=2 to return namespace shop, got %s", ref.Namespace)
	}
}

func TestResolvePodChoosesNewestRunning(t *testing.T) {
	t.Parallel()

	now := time.Now()
	client := fake.NewSimpleClientset(
		newDeployment("shop", "payment", map[string]string{"app": "payment"}),
		newPod("shop", "payment-old-running", map[string]string{"app": "payment"}, corev1.PodRunning, ptrTime(now.Add(-10*time.Minute)), now.Add(-10*time.Minute)),
		newPod("shop", "payment-new-running", map[string]string{"app": "payment"}, corev1.PodRunning, ptrTime(now.Add(-1*time.Minute)), now.Add(-1*time.Minute)),
		newPod("shop", "payment-pending", map[string]string{"app": "payment"}, corev1.PodPending, ptrTime(now), now),
	)

	resolver := NewResolver(client)
	resolved, err := resolver.ResolvePod(context.Background(), "shop", "payment", KindDeployment, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resolved.Pod.Name != "payment-new-running" {
		t.Fatalf("expected newest running pod payment-new-running, got %s", resolved.Pod.Name)
	}
	if resolved.Warning != "" {
		t.Fatalf("expected no warning for running pod selection, got %q", resolved.Warning)
	}
}

func TestResolvePodWarnsWhenNoRunning(t *testing.T) {
	t.Parallel()

	now := time.Now()
	client := fake.NewSimpleClientset(
		newDeployment("shop", "payment", map[string]string{"app": "payment"}),
		newPod("shop", "payment-pending-old", map[string]string{"app": "payment"}, corev1.PodPending, ptrTime(now.Add(-5*time.Minute)), now.Add(-5*time.Minute)),
		newPod("shop", "payment-pending-new", map[string]string{"app": "payment"}, corev1.PodPending, ptrTime(now.Add(-1*time.Minute)), now.Add(-1*time.Minute)),
	)

	resolver := NewResolver(client)
	resolved, err := resolver.ResolvePod(context.Background(), "shop", "payment", KindDeployment, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resolved.Pod.Name != "payment-pending-new" {
		t.Fatalf("expected newest pod payment-pending-new, got %s", resolved.Pod.Name)
	}
	if resolved.Warning == "" {
		t.Fatal("expected warning when no running pods are available")
	}
}

func TestResolvePodAllNamespacesUsesPickedNamespace(t *testing.T) {
	t.Parallel()

	now := time.Now()
	client := fake.NewSimpleClientset(
		newDeployment("alpha", "frontend", map[string]string{"app": "frontend-alpha"}),
		newPod("alpha", "frontend-alpha-pod", map[string]string{"app": "frontend-alpha"}, corev1.PodRunning, ptrTime(now.Add(-2*time.Minute)), now.Add(-2*time.Minute)),
		newDeployment("shop", "frontend", map[string]string{"app": "frontend-shop"}),
		newPod("shop", "frontend-shop-pod", map[string]string{"app": "frontend-shop"}, corev1.PodRunning, ptrTime(now.Add(-1*time.Minute)), now.Add(-1*time.Minute)),
	)

	resolver := NewResolver(client)
	resolved, err := resolver.ResolvePod(context.Background(), NamespaceAll, "frontend", KindDeployment, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resolved.Workload.Namespace != "shop" {
		t.Fatalf("expected workload namespace shop, got %s", resolved.Workload.Namespace)
	}
	if resolved.Pod.Namespace != "shop" {
		t.Fatalf("expected pod namespace shop, got %s", resolved.Pod.Namespace)
	}
	if resolved.Pod.Name != "frontend-shop-pod" {
		t.Fatalf("expected shop pod, got %s", resolved.Pod.Name)
	}
}

func newDeployment(namespace, name string, labels map[string]string) *appsv1.Deployment {
	replicas := int32(1)
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "nginx"}}},
			},
		},
	}
}

func newStatefulSet(namespace, name string, labels map[string]string) *appsv1.StatefulSet {
	replicas := int32(1)
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "nginx"}}},
			},
		},
	}
}

func newPod(namespace, name string, labels map[string]string, phase corev1.PodPhase, startTime *metav1.Time, created time.Time) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels, CreationTimestamp: metav1.NewTime(created)},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "app", Image: "nginx"}},
		},
		Status: corev1.PodStatus{
			Phase:     phase,
			StartTime: startTime,
		},
	}
}

func ptrTime(t time.Time) *metav1.Time {
	mt := metav1.NewTime(t)
	return &mt
}
