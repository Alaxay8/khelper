package cmd

import "testing"

func TestParseImageAssignments(t *testing.T) {
	t.Parallel()

	assignments, err := parseImageAssignments([]string{"app=nginx:1.27", "sidecar=busybox:1.37"})
	if err != nil {
		t.Fatalf("parseImageAssignments returned error: %v", err)
	}

	if assignments["app"] != "nginx:1.27" {
		t.Fatalf("expected app assignment, got %+v", assignments)
	}
	if assignments["sidecar"] != "busybox:1.37" {
		t.Fatalf("expected sidecar assignment, got %+v", assignments)
	}
}

func TestParseImageAssignmentsRejectsInvalidFormat(t *testing.T) {
	t.Parallel()

	if _, err := parseImageAssignments([]string{"app"}); err == nil {
		t.Fatal("expected parse error for invalid assignment format")
	}
}

func TestParseImageAssignmentsRejectsDuplicateContainer(t *testing.T) {
	t.Parallel()

	if _, err := parseImageAssignments([]string{"app=nginx:1.27", "app=nginx:1.28"}); err == nil {
		t.Fatal("expected parse error for duplicate container assignment")
	}
}
