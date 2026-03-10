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
