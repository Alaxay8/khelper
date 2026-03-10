package kube

import (
	"context"
	"errors"
	"io"
	"reflect"
	"testing"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func TestDetectShellWithRunnerPrefersBash(t *testing.T) {
	t.Parallel()

	var commands [][]string
	runner := func(
		_ context.Context,
		_ *rest.Config,
		_ kubernetes.Interface,
		_, _, _ string,
		command []string,
		_ bool,
		_ io.Reader,
		_ io.Writer,
		_ io.Writer,
	) error {
		commands = append(commands, append([]string(nil), command...))
		return nil
	}

	shell, err := detectShellWithRunner(context.Background(), nil, nil, "shop", "payment", "app", runner)
	if err != nil {
		t.Fatalf("detectShellWithRunner returned error: %v", err)
	}
	if shell != "bash" {
		t.Fatalf("expected detected shell bash, got %q", shell)
	}

	want := [][]string{{"bash", "-c", "exit 0"}}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("unexpected commands: got %v, want %v", commands, want)
	}
}

func TestDetectShellWithRunnerFallsBackToSh(t *testing.T) {
	t.Parallel()

	bashErr := errors.New("bash missing")
	var commands [][]string
	runner := func(
		_ context.Context,
		_ *rest.Config,
		_ kubernetes.Interface,
		_, _, _ string,
		command []string,
		_ bool,
		_ io.Reader,
		_ io.Writer,
		_ io.Writer,
	) error {
		commands = append(commands, append([]string(nil), command...))
		if len(commands) == 1 {
			return bashErr
		}
		return nil
	}

	shell, err := detectShellWithRunner(context.Background(), nil, nil, "shop", "payment", "app", runner)
	if err != nil {
		t.Fatalf("detectShellWithRunner returned error: %v", err)
	}
	if shell != "sh" {
		t.Fatalf("expected detected shell sh, got %q", shell)
	}

	want := [][]string{
		{"bash", "-c", "exit 0"},
		{"sh", "-c", "exit 0"},
	}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("unexpected commands: got %v, want %v", commands, want)
	}
}

func TestDetectShellWithRunnerReturnsErrorWhenNoShellFound(t *testing.T) {
	t.Parallel()

	runner := func(
		_ context.Context,
		_ *rest.Config,
		_ kubernetes.Interface,
		_, _, _ string,
		_ []string,
		_ bool,
		_ io.Reader,
		_ io.Writer,
		_ io.Writer,
	) error {
		return errors.New("not found")
	}

	_, err := detectShellWithRunner(context.Background(), nil, nil, "shop", "payment", "app", runner)
	if err == nil {
		t.Fatal("expected error when no shell is available")
	}
}
