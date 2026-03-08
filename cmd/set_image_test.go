package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/alaxay8/khelper/internal/kube"
)

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

func TestParseSetImageInputShorthand(t *testing.T) {
	t.Parallel()

	input, err := parseSetImageInput([]string{"frontend:v1.0.1"})
	if err != nil {
		t.Fatalf("parseSetImageInput returned error: %v", err)
	}

	if input.Target != "frontend" {
		t.Fatalf("expected target frontend, got %q", input.Target)
	}
	if input.ShorthandTag != "v1.0.1" {
		t.Fatalf("expected shorthand tag v1.0.1, got %q", input.ShorthandTag)
	}
	if input.Kind != "" {
		t.Fatalf("expected empty kind, got %q", input.Kind)
	}
	if len(input.Assignments) != 0 {
		t.Fatalf("expected no explicit assignments, got %+v", input.Assignments)
	}
}

func TestParseSetImageInputExplicitAssignments(t *testing.T) {
	t.Parallel()

	input, err := parseSetImageInput([]string{"frontend", "server=ghcr.io/acme/frontend:v2"})
	if err != nil {
		t.Fatalf("parseSetImageInput returned error: %v", err)
	}

	if input.Target != "frontend" {
		t.Fatalf("expected target frontend, got %q", input.Target)
	}
	if input.ShorthandTag != "" {
		t.Fatalf("expected empty shorthand tag, got %q", input.ShorthandTag)
	}
	if input.Kind != "" {
		t.Fatalf("expected empty kind, got %q", input.Kind)
	}
	if got := input.Assignments["server"]; got != "ghcr.io/acme/frontend:v2" {
		t.Fatalf("expected server assignment, got %+v", input.Assignments)
	}
}

func TestParseSetImageInputShorthandWithKindPrefix(t *testing.T) {
	t.Parallel()

	input, err := parseSetImageInput([]string{"deployment/frontend:v1.0.1"})
	if err != nil {
		t.Fatalf("parseSetImageInput returned error: %v", err)
	}

	if input.Target != "frontend" {
		t.Fatalf("expected target frontend, got %q", input.Target)
	}
	if input.Kind != kube.KindDeployment {
		t.Fatalf("expected kind deployment, got %q", input.Kind)
	}
	if input.ShorthandTag != "v1.0.1" {
		t.Fatalf("expected shorthand tag v1.0.1, got %q", input.ShorthandTag)
	}
}

func TestParseSetImageInputExplicitWithKindPrefix(t *testing.T) {
	t.Parallel()

	input, err := parseSetImageInput([]string{"sts/db", "db=postgres:16.4"})
	if err != nil {
		t.Fatalf("parseSetImageInput returned error: %v", err)
	}

	if input.Target != "db" {
		t.Fatalf("expected target db, got %q", input.Target)
	}
	if input.Kind != kube.KindStatefulSet {
		t.Fatalf("expected kind statefulset, got %q", input.Kind)
	}
}

func TestParseSetImageInputRejectsSingleArgWithoutTag(t *testing.T) {
	t.Parallel()

	if _, err := parseSetImageInput([]string{"frontend"}); err == nil {
		t.Fatal("expected error for single argument without :tag shorthand")
	}
}

func TestParseSetImageInputRejectsInvalidKindQualifiedTarget(t *testing.T) {
	t.Parallel()

	if _, err := parseSetImageInput([]string{"deployment/:v1.0.1"}); err == nil {
		t.Fatal("expected error for invalid kind-qualified target")
	}
}

func TestPromptKindSelection(t *testing.T) {
	t.Parallel()

	input := strings.NewReader("2\n")
	var output bytes.Buffer

	ref, err := promptKindSelection(input, &output, "frontend", []kube.WorkloadRef{
		{Kind: kube.KindDeployment, Name: "frontend", Namespace: "shop"},
		{Kind: kube.KindStatefulSet, Name: "frontend", Namespace: "shop"},
	})
	if err != nil {
		t.Fatalf("promptKindSelection returned error: %v", err)
	}
	if ref.Kind != kube.KindStatefulSet {
		t.Fatalf("expected selected statefulset, got %q", ref.Kind)
	}
}

func TestSetImageCommandHelpArg(t *testing.T) {
	t.Parallel()

	cmd := newSetImageCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected help command to succeed, got error: %v", err)
	}
	if !strings.Contains(out.String(), "Usage:") {
		t.Fatalf("expected help output to contain Usage, got: %s", out.String())
	}
}
