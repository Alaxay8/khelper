package kube

import (
	"context"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestResolveTagImageAssignmentsSingleContainer(t *testing.T) {
	t.Parallel()

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "frontend", Namespace: "shop"},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "server", Image: "ghcr.io/alaxay8/frontend:v7"},
					},
				},
			},
		},
	}

	client := fake.NewSimpleClientset(dep)
	assignments, err := ResolveTagImageAssignments(context.Background(), client, "shop", KindDeployment, "frontend", "frontend", "v1.0.1")
	if err != nil {
		t.Fatalf("ResolveTagImageAssignments returned error: %v", err)
	}

	if got := assignments["server"]; got != "ghcr.io/alaxay8/frontend:v1.0.1" {
		t.Fatalf("expected updated server image, got %+v", assignments)
	}
}

func TestResolveTagImageAssignmentsMultipleContainersByHint(t *testing.T) {
	t.Parallel()

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "frontend", Namespace: "shop"},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "app", Image: "ghcr.io/acme/app:v1"},
						{Name: "sidecar", Image: "ghcr.io/acme/sidecar:v1"},
					},
				},
			},
		},
	}

	client := fake.NewSimpleClientset(dep)
	assignments, err := ResolveTagImageAssignments(context.Background(), client, "shop", KindDeployment, "frontend", "sidecar", "v2")
	if err != nil {
		t.Fatalf("ResolveTagImageAssignments returned error: %v", err)
	}

	if got := assignments["sidecar"]; got != "ghcr.io/acme/sidecar:v2" {
		t.Fatalf("expected sidecar image update, got %+v", assignments)
	}
}

func TestResolveTagImageAssignmentsMultipleContainersWithoutHint(t *testing.T) {
	t.Parallel()

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "frontend", Namespace: "shop"},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "app", Image: "ghcr.io/acme/app:v1"},
						{Name: "sidecar", Image: "ghcr.io/acme/sidecar:v1"},
					},
				},
			},
		},
	}

	client := fake.NewSimpleClientset(dep)
	if _, err := ResolveTagImageAssignments(context.Background(), client, "shop", KindDeployment, "frontend", "frontend", "v2"); err == nil {
		t.Fatal("expected error for multi-container workload without matching hint")
	}
}

func TestResolveTagImageAssignmentsDigestPinnedImage(t *testing.T) {
	t.Parallel()

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "frontend", Namespace: "shop"},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "server", Image: "ghcr.io/alaxay8/frontend@sha256:abcdef"},
					},
				},
			},
		},
	}

	client := fake.NewSimpleClientset(dep)
	_, err := ResolveTagImageAssignments(context.Background(), client, "shop", KindDeployment, "frontend", "frontend", "v1.0.1")
	if err == nil {
		t.Fatal("expected error for digest-pinned image")
	}
	if !strings.Contains(err.Error(), "digest-pinned") {
		t.Fatalf("expected digest-pinned error, got %v", err)
	}
}

func TestImageWithTagKeepsRegistryPort(t *testing.T) {
	t.Parallel()

	got, err := imageWithTag("registry.local:5000/acme/frontend:v7", "v1.0.1")
	if err != nil {
		t.Fatalf("imageWithTag returned error: %v", err)
	}
	if got != "registry.local:5000/acme/frontend:v1.0.1" {
		t.Fatalf("expected registry port to be preserved, got %q", got)
	}
}
