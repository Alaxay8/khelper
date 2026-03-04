package kube

import (
	"context"
	"fmt"
	"io"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

func ExecInPod(
	ctx context.Context,
	restCfg *rest.Config,
	client kubernetes.Interface,
	namespace,
	podName,
	container string,
	command []string,
	tty bool,
	stdin io.Reader,
	stdout io.Writer,
	stderr io.Writer,
) error {
	if len(command) == 0 {
		return fmt.Errorf("exec command is required")
	}

	req := client.CoreV1().RESTClient().Post().
		Namespace(namespace).
		Resource("pods").
		Name(podName).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: container,
			Command:   command,
			Stdin:     stdin != nil,
			Stdout:    stdout != nil,
			Stderr:    stderr != nil,
			TTY:       tty,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(restCfg, "POST", req.URL())
	if err != nil {
		return fmt.Errorf("create SPDY executor: %w", err)
	}

	if err := exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:             stdin,
		Stdout:            stdout,
		Stderr:            stderr,
		Tty:               tty,
		TerminalSizeQueue: nil,
	}); err != nil {
		return fmt.Errorf("exec command %q in pod %s/%s: %w", strings.Join(command, " "), namespace, podName, err)
	}

	return nil
}

func DetectShell(
	ctx context.Context,
	restCfg *rest.Config,
	client kubernetes.Interface,
	namespace,
	podName,
	container string,
) (string, error) {
	checks := []struct {
		shell string
		cmd   []string
	}{
		{shell: "bash", cmd: []string{"bash", "-lc", "exit 0"}},
		{shell: "sh", cmd: []string{"sh", "-lc", "exit 0"}},
	}

	var lastErr error
	for _, check := range checks {
		err := ExecInPod(ctx, restCfg, client, namespace, podName, container, check.cmd, false, nil, io.Discard, io.Discard)
		if err == nil {
			return check.shell, nil
		}
		lastErr = err
	}

	if lastErr != nil {
		return "", fmt.Errorf("could not detect shell (tried bash then sh): %w", lastErr)
	}
	return "", fmt.Errorf("could not detect shell")
}
