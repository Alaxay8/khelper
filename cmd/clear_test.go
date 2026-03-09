package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseClearTargetDefault(t *testing.T) {
	t.Parallel()

	target, err := parseClearTarget(nil)
	if err != nil {
		t.Fatalf("parseClearTarget returned error: %v", err)
	}
	if target != "evicted" {
		t.Fatalf("expected default target evicted, got %q", target)
	}
}

func TestParseClearTargetEvicted(t *testing.T) {
	t.Parallel()

	target, err := parseClearTarget([]string{"evicted"})
	if err != nil {
		t.Fatalf("parseClearTarget returned error: %v", err)
	}
	if target != "evicted" {
		t.Fatalf("expected target evicted, got %q", target)
	}
}

func TestParseClearTargetRejectsUnknownTarget(t *testing.T) {
	t.Parallel()

	if _, err := parseClearTarget([]string{"pods"}); err == nil {
		t.Fatal("expected error for unsupported clear target")
	}
}

func TestConfirmClearAllNamespaces(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "yes short", input: "y\n", want: true},
		{name: "yes long", input: "yes\n", want: true},
		{name: "no", input: "n\n", want: false},
		{name: "empty", input: "\n", want: false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			got, err := confirmClearAllNamespaces(strings.NewReader(tc.input), &out)
			if err != nil {
				t.Fatalf("confirmClearAllNamespaces returned error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("unexpected confirmation result: got %v, want %v", got, tc.want)
			}
			if !strings.Contains(out.String(), "Continue? [y/N]: ") {
				t.Fatalf("expected prompt output, got %q", out.String())
			}
		})
	}
}

func TestClearCommandHelp(t *testing.T) {
	t.Parallel()

	cmd := newClearCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected help command to succeed, got error: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "khelper clear") {
		t.Fatalf("expected help to include command examples, got: %s", got)
	}
	if !strings.Contains(got, "--dry-run") {
		t.Fatalf("expected help to include --dry-run flag, got: %s", got)
	}
	if !strings.Contains(got, "--yes") {
		t.Fatalf("expected help to include --yes flag, got: %s", got)
	}
}
