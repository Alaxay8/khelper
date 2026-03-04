package kube

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

type PodLogsOptions struct {
	Follow        bool
	Since         time.Duration
	Tail          int64
	Container     string
	AllContainers bool
	Out           io.Writer
}

func StreamPodLogs(ctx context.Context, client kubernetes.Interface, pod *corev1.Pod, opts PodLogsOptions) error {
	if pod == nil {
		return fmt.Errorf("pod is required")
	}
	if opts.Out == nil {
		return fmt.Errorf("output writer is required")
	}

	containers, err := selectLogContainers(pod, opts.Container, opts.AllContainers)
	if err != nil {
		return err
	}

	if len(containers) == 1 {
		return streamContainerLogs(ctx, client, pod.Namespace, pod.Name, containers[0], opts, "")
	}

	if opts.Follow {
		return streamContainersInParallel(ctx, client, pod, containers, opts)
	}

	for _, c := range containers {
		prefix := fmt.Sprintf("[%s/%s] ", pod.Name, c)
		if err := streamContainerLogs(ctx, client, pod.Namespace, pod.Name, c, opts, prefix); err != nil {
			return err
		}
	}

	return nil
}

func selectLogContainers(pod *corev1.Pod, container string, all bool) ([]string, error) {
	if pod == nil {
		return nil, fmt.Errorf("pod is required")
	}

	container = strings.TrimSpace(container)

	if all {
		containers := make([]string, 0, len(pod.Spec.Containers))
		for _, c := range pod.Spec.Containers {
			containers = append(containers, c.Name)
		}
		if len(containers) == 0 {
			return nil, fmt.Errorf("pod %s has no containers", pod.Name)
		}
		return containers, nil
	}

	if container != "" {
		if !PodHasContainer(pod, container) {
			return nil, fmt.Errorf("container %q not found in pod %s", container, pod.Name)
		}
		return []string{container}, nil
	}

	if len(pod.Spec.Containers) == 0 {
		return nil, fmt.Errorf("pod %s has no containers", pod.Name)
	}
	if len(pod.Spec.Containers) > 1 {
		return nil, fmt.Errorf("pod %s has multiple containers; use --container or --all-containers", pod.Name)
	}
	return []string{pod.Spec.Containers[0].Name}, nil
}

func PodHasContainer(pod *corev1.Pod, name string) bool {
	for _, c := range pod.Spec.Containers {
		if c.Name == name {
			return true
		}
	}
	return false
}

func streamContainersInParallel(ctx context.Context, client kubernetes.Interface, pod *corev1.Pod, containers []string, opts PodLogsOptions) error {
	var wg sync.WaitGroup
	var mu sync.Mutex

	errCh := make(chan error, len(containers))

	for _, container := range containers {
		container := container
		wg.Add(1)

		go func() {
			defer wg.Done()
			prefix := fmt.Sprintf("[%s/%s] ", pod.Name, container)
			if err := streamContainerLogsWithMutex(ctx, client, pod.Namespace, pod.Name, container, opts, prefix, &mu); err != nil {
				errCh <- err
			}
		}()
	}

	wg.Wait()
	close(errCh)

	var allErrs []error
	for err := range errCh {
		allErrs = append(allErrs, err)
	}
	if len(allErrs) == 0 {
		return nil
	}

	return errors.Join(allErrs...)
}

func streamContainerLogs(ctx context.Context, client kubernetes.Interface, namespace, podName, container string, opts PodLogsOptions, prefix string) error {
	podLogOptions := &corev1.PodLogOptions{
		Container: container,
		Follow:    opts.Follow,
	}

	if opts.Since > 0 {
		sinceSeconds := int64(opts.Since.Seconds())
		if sinceSeconds > 0 {
			podLogOptions.SinceSeconds = &sinceSeconds
		}
	}

	if opts.Tail > 0 {
		tail := opts.Tail
		podLogOptions.TailLines = &tail
	}

	req := client.CoreV1().Pods(namespace).GetLogs(podName, podLogOptions)
	stream, err := req.Stream(ctx)
	if err != nil {
		return fmt.Errorf("stream logs for %s/%s container %s: %w", namespace, podName, container, err)
	}
	defer stream.Close()

	return copyLogStream(stream, opts.Out, prefix)
}

func streamContainerLogsWithMutex(ctx context.Context, client kubernetes.Interface, namespace, podName, container string, opts PodLogsOptions, prefix string, mu *sync.Mutex) error {
	podLogOptions := &corev1.PodLogOptions{
		Container: container,
		Follow:    opts.Follow,
	}

	if opts.Since > 0 {
		sinceSeconds := int64(opts.Since.Seconds())
		if sinceSeconds > 0 {
			podLogOptions.SinceSeconds = &sinceSeconds
		}
	}
	if opts.Tail > 0 {
		tail := opts.Tail
		podLogOptions.TailLines = &tail
	}

	req := client.CoreV1().Pods(namespace).GetLogs(podName, podLogOptions)
	stream, err := req.Stream(ctx)
	if err != nil {
		return fmt.Errorf("stream logs for %s/%s container %s: %w", namespace, podName, container, err)
	}
	defer stream.Close()

	reader := bufio.NewReader(stream)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			mu.Lock()
			if _, writeErr := fmt.Fprintf(opts.Out, "%s%s", prefix, line); writeErr != nil {
				mu.Unlock()
				return fmt.Errorf("write log output: %w", writeErr)
			}
			mu.Unlock()
		}

		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read log stream: %w", err)
		}
	}

	return nil
}

func copyLogStream(src io.Reader, dst io.Writer, prefix string) error {
	if prefix == "" {
		if _, err := io.Copy(dst, src); err != nil {
			return fmt.Errorf("copy logs: %w", err)
		}
		return nil
	}

	reader := bufio.NewReader(src)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			if _, writeErr := fmt.Fprintf(dst, "%s%s", prefix, line); writeErr != nil {
				return fmt.Errorf("write log output: %w", writeErr)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read log stream: %w", err)
		}
	}

	return nil
}
