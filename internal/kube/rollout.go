package kube

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
)

func RolloutRestart(ctx context.Context, client kubernetes.Interface, namespace, kind, name string, timeout time.Duration, out io.Writer) error {
	kind = strings.ToLower(strings.TrimSpace(kind))
	timestamp := time.Now().UTC().Format(time.RFC3339)

	patchBytes, err := json.Marshal(map[string]any{
		"spec": map[string]any{
			"template": map[string]any{
				"metadata": map[string]any{
					"annotations": map[string]string{
						"kubectl.kubernetes.io/restartedAt": timestamp,
					},
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("build restart patch: %w", err)
	}

	if out != nil {
		_, _ = fmt.Fprintf(out, "Restarting %s/%s in namespace %s...\n", kind, name, namespace)
	}

	switch kind {
	case KindDeployment:
		if _, err := client.AppsV1().Deployments(namespace).Patch(ctx, name, types.StrategicMergePatchType, patchBytes, metav1.PatchOptions{}); err != nil {
			return fmt.Errorf("patch deployment %s: %w", name, err)
		}
		if out != nil {
			_, _ = fmt.Fprintln(out, "Waiting for deployment rollout to complete...")
		}
		return waitDeploymentRollout(ctx, client, namespace, name, timeout, out)
	case KindStatefulSet:
		if _, err := client.AppsV1().StatefulSets(namespace).Patch(ctx, name, types.StrategicMergePatchType, patchBytes, metav1.PatchOptions{}); err != nil {
			return fmt.Errorf("patch statefulset %s: %w", name, err)
		}
		if out != nil {
			_, _ = fmt.Fprintln(out, "Waiting for statefulset rollout to complete...")
		}
		return waitStatefulSetRollout(ctx, client, namespace, name, timeout, out)
	default:
		return fmt.Errorf("restart supports only deployment/statefulset, got %q", kind)
	}
}

func waitDeploymentRollout(ctx context.Context, client kubernetes.Interface, namespace, name string, timeout time.Duration, out io.Writer) error {
	poll := 2 * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}

	err := wait.PollUntilContextTimeout(ctx, poll, timeout, true, func(ctx context.Context) (bool, error) {
		dep, err := client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return false, fmt.Errorf("get deployment %s: %w", name, err)
		}

		desired := int32(1)
		if dep.Spec.Replicas != nil {
			desired = *dep.Spec.Replicas
		}

		if out != nil {
			_, _ = fmt.Fprintf(out, "  observed=%d generation=%d updated=%d ready=%d available=%d desired=%d\n",
				dep.Status.ObservedGeneration,
				dep.Generation,
				dep.Status.UpdatedReplicas,
				dep.Status.ReadyReplicas,
				dep.Status.AvailableReplicas,
				desired,
			)
		}

		done := dep.Status.ObservedGeneration >= dep.Generation &&
			dep.Status.UpdatedReplicas == desired &&
			dep.Status.ReadyReplicas == desired &&
			dep.Status.AvailableReplicas == desired &&
			dep.Status.UnavailableReplicas == 0
		return done, nil
	})
	if err != nil {
		return fmt.Errorf("wait for deployment rollout: %w", err)
	}

	if out != nil {
		_, _ = fmt.Fprintln(out, "Deployment rollout complete.")
	}
	return nil
}

func waitStatefulSetRollout(ctx context.Context, client kubernetes.Interface, namespace, name string, timeout time.Duration, out io.Writer) error {
	poll := 2 * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}

	err := wait.PollUntilContextTimeout(ctx, poll, timeout, true, func(ctx context.Context) (bool, error) {
		sts, err := client.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return false, fmt.Errorf("get statefulset %s: %w", name, err)
		}

		desired := int32(1)
		if sts.Spec.Replicas != nil {
			desired = *sts.Spec.Replicas
		}

		if out != nil {
			_, _ = fmt.Fprintf(out, "  observed=%d generation=%d updated=%d ready=%d desired=%d revision=%s/%s\n",
				sts.Status.ObservedGeneration,
				sts.Generation,
				sts.Status.UpdatedReplicas,
				sts.Status.ReadyReplicas,
				desired,
				sts.Status.CurrentRevision,
				sts.Status.UpdateRevision,
			)
		}

		done := sts.Status.ObservedGeneration >= sts.Generation &&
			sts.Status.UpdatedReplicas == desired &&
			sts.Status.ReadyReplicas == desired
		if desired > 0 {
			done = done && sts.Status.CurrentRevision == sts.Status.UpdateRevision
		}

		return done, nil
	})
	if err != nil {
		return fmt.Errorf("wait for statefulset rollout: %w", err)
	}

	if out != nil {
		_, _ = fmt.Fprintln(out, "StatefulSet rollout complete.")
	}
	return nil
}
