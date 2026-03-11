package cmd

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/alaxay8/khelper/internal/kube"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

func TestRestartUsesResolvedWorkloadNamespace(t *testing.T) {
	previousBundleFactory := newRestartClientBundle
	previousRestart := rolloutRestartFn
	t.Cleanup(func() {
		newRestartClientBundle = previousBundleFactory
		rolloutRestartFn = previousRestart
	})

	client := fake.NewSimpleClientset(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "payment",
			Namespace: "shop",
		},
	})

	newRestartClientBundle = func() (*kube.ClientBundle, error) {
		return &kube.ClientBundle{
			Clientset: client,
			Namespace: kube.NamespaceAll,
		}, nil
	}

	capturedNamespace := ""
	rolloutRestartFn = func(
		_ context.Context,
		_ kubernetes.Interface,
		namespace, _, _ string,
		_ time.Duration,
		_ io.Writer,
	) error {
		capturedNamespace = namespace
		return nil
	}

	command := newRestartCmd()
	command.SetOut(io.Discard)
	command.SetErr(io.Discard)
	command.SetArgs([]string{"payment", "--kind", "deployment"})

	if err := command.Execute(); err != nil {
		t.Fatalf("restart command returned error: %v", err)
	}

	if capturedNamespace != "shop" {
		t.Fatalf("expected restart namespace shop, got %q", capturedNamespace)
	}
}
