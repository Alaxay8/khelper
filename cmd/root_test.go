package cmd

import (
	"testing"

	"github.com/alaxay8/khelper/internal/kube"
)

func TestExitCodeInvalidPickMapsToUsage(t *testing.T) {
	t.Parallel()

	err := &kube.InvalidPickError{Pick: 7, Max: 3}
	if got := exitCode(err); got != ExitCodeUsage {
		t.Fatalf("expected exit code %d, got %d", ExitCodeUsage, got)
	}
}

func TestExitCodeWrappedNotFoundMapsToNotFound(t *testing.T) {
	t.Parallel()

	err := WrapExitError(ExitCodeGeneral, &kube.NotFoundError{
		Namespace: "shop",
		Target:    "payment",
		Kind:      kube.KindDeployment,
	}, "fetch workload")

	if got := exitCode(err); got != ExitCodeNotFound {
		t.Fatalf("expected exit code %d, got %d", ExitCodeNotFound, got)
	}
}

func TestExitCodeWrappedMetricsUnavailableMapsToUnavailable(t *testing.T) {
	t.Parallel()

	err := WrapExitError(ExitCodeGeneral, kube.ErrMetricsUnavailable, "query metrics")
	if got := exitCode(err); got != ExitCodeUnavailable {
		t.Fatalf("expected exit code %d, got %d", ExitCodeUnavailable, got)
	}
}

func TestExitCodeExplicitExitErrorKeepsExplicitCode(t *testing.T) {
	t.Parallel()

	err := WrapExitError(ExitCodeUsage, &kube.NotFoundError{
		Namespace: "shop",
		Target:    "payment",
		Kind:      kube.KindDeployment,
	}, "explicit usage error")

	if got := exitCode(err); got != ExitCodeUsage {
		t.Fatalf("expected exit code %d, got %d", ExitCodeUsage, got)
	}
}
