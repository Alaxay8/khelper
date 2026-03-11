package kube

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestGetRolloutStatusDeploymentComplete(t *testing.T) {
	t.Parallel()

	replicas := int32(2)
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "payment",
			Namespace: "shop",
			Annotations: map[string]string{
				"deployment.kubernetes.io/revision": "5",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "nginx:1.25"}}},
			},
		},
		Status: appsv1.DeploymentStatus{
			ObservedGeneration:  7,
			UpdatedReplicas:     2,
			ReadyReplicas:       2,
			AvailableReplicas:   2,
			UnavailableReplicas: 0,
		},
	}
	dep.Generation = 7

	client := fake.NewSimpleClientset(dep)

	status, err := GetRolloutStatus(context.Background(), client, "shop", KindDeployment, "payment")
	if err != nil {
		t.Fatalf("GetRolloutStatus returned error: %v", err)
	}

	if !status.Complete {
		t.Fatal("expected deployment rollout to be complete")
	}
	if status.CurrentRevision != "5" {
		t.Fatalf("expected revision 5, got %q", status.CurrentRevision)
	}
	if status.DesiredReplicas != 2 {
		t.Fatalf("expected desired replicas 2, got %d", status.DesiredReplicas)
	}
}

func TestGetRolloutStatusStatefulSetProgressing(t *testing.T) {
	t.Parallel()

	replicas := int32(3)
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "shop"},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "db", Image: "postgres:16"}}},
			},
		},
		Status: appsv1.StatefulSetStatus{
			ObservedGeneration: 5,
			ReadyReplicas:      2,
			UpdatedReplicas:    2,
			CurrentRevision:    "db-5f8477f97d",
			UpdateRevision:     "db-75c6d87c57",
		},
	}
	sts.Generation = 6

	client := fake.NewSimpleClientset(sts)

	status, err := GetRolloutStatus(context.Background(), client, "shop", KindStatefulSet, "db")
	if err != nil {
		t.Fatalf("GetRolloutStatus returned error: %v", err)
	}

	if status.Complete {
		t.Fatal("expected statefulset rollout to be progressing")
	}
	if status.CurrentRevision != "db-5f8477f97d" {
		t.Fatalf("unexpected current revision: %q", status.CurrentRevision)
	}
	if status.UpdateRevision != "db-75c6d87c57" {
		t.Fatalf("unexpected update revision: %q", status.UpdateRevision)
	}
}

func TestGetRolloutStatusStatefulSetPartitionedRollingUpdateCanBeComplete(t *testing.T) {
	t.Parallel()

	replicas := int32(3)
	partition := int32(1)
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "shop"},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: appsv1.RollingUpdateStatefulSetStrategyType,
				RollingUpdate: &appsv1.RollingUpdateStatefulSetStrategy{
					Partition: &partition,
				},
			},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "db", Image: "postgres:16"}}},
			},
		},
		Status: appsv1.StatefulSetStatus{
			ObservedGeneration: 6,
			ReadyReplicas:      3,
			UpdatedReplicas:    2,
			CurrentRevision:    "db-rev-old",
			UpdateRevision:     "db-rev-new",
		},
	}
	sts.Generation = 6

	client := fake.NewSimpleClientset(sts)

	status, err := GetRolloutStatus(context.Background(), client, "shop", KindStatefulSet, "db")
	if err != nil {
		t.Fatalf("GetRolloutStatus returned error: %v", err)
	}

	if !status.Complete {
		t.Fatal("expected partitioned statefulset rollout to be complete")
	}
}

func TestGetRolloutStatusStatefulSetOnDeleteCanBeCompleteWithoutAutoUpdate(t *testing.T) {
	t.Parallel()

	replicas := int32(3)
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "shop"},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: appsv1.OnDeleteStatefulSetStrategyType,
			},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "db", Image: "postgres:16"}}},
			},
		},
		Status: appsv1.StatefulSetStatus{
			ObservedGeneration: 4,
			ReadyReplicas:      3,
			UpdatedReplicas:    0,
			CurrentRevision:    "db-rev-old",
			UpdateRevision:     "db-rev-new",
		},
	}
	sts.Generation = 4

	client := fake.NewSimpleClientset(sts)

	status, err := GetRolloutStatus(context.Background(), client, "shop", KindStatefulSet, "db")
	if err != nil {
		t.Fatalf("GetRolloutStatus returned error: %v", err)
	}

	if !status.Complete {
		t.Fatal("expected onDelete statefulset rollout to be complete without forced update count")
	}
}

func TestWaitStatefulSetRolloutPartitionedRollingUpdateCanComplete(t *testing.T) {
	t.Parallel()

	replicas := int32(3)
	partition := int32(1)
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "shop"},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: appsv1.RollingUpdateStatefulSetStrategyType,
				RollingUpdate: &appsv1.RollingUpdateStatefulSetStrategy{
					Partition: &partition,
				},
			},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "db", Image: "postgres:16"}}},
			},
		},
		Status: appsv1.StatefulSetStatus{
			ObservedGeneration: 7,
			ReadyReplicas:      3,
			UpdatedReplicas:    2,
			CurrentRevision:    "db-rev-old",
			UpdateRevision:     "db-rev-new",
		},
	}
	sts.Generation = 7

	client := fake.NewSimpleClientset(sts)
	if err := waitStatefulSetRollout(context.Background(), client, "shop", "db", 100*time.Millisecond, nil); err != nil {
		t.Fatalf("waitStatefulSetRollout returned error: %v", err)
	}
}

func TestWaitStatefulSetRolloutOnDeleteCanCompleteWithoutAutoUpdate(t *testing.T) {
	t.Parallel()

	replicas := int32(3)
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "shop"},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: appsv1.OnDeleteStatefulSetStrategyType,
			},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "db", Image: "postgres:16"}}},
			},
		},
		Status: appsv1.StatefulSetStatus{
			ObservedGeneration: 5,
			ReadyReplicas:      3,
			UpdatedReplicas:    0,
			CurrentRevision:    "db-rev-old",
			UpdateRevision:     "db-rev-new",
		},
	}
	sts.Generation = 5

	client := fake.NewSimpleClientset(sts)
	if err := waitStatefulSetRollout(context.Background(), client, "shop", "db", 100*time.Millisecond, nil); err != nil {
		t.Fatalf("waitStatefulSetRollout returned error: %v", err)
	}
}

func TestListRolloutHistoryDeploymentSortedAndMarksCurrent(t *testing.T) {
	t.Parallel()

	replicas := int32(1)
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "payment",
			Namespace: "shop",
			UID:       "dep-uid",
			Annotations: map[string]string{
				"deployment.kubernetes.io/revision": "3",
			},
		},
		Spec: appsv1.DeploymentSpec{Replicas: &replicas},
	}

	rs3 := newReplicaSet("shop", "payment-7f9f", "dep-uid", 3, "nginx:1.27")
	rs1 := newReplicaSet("shop", "payment-5a6b", "dep-uid", 1, "nginx:1.25")
	rs2 := newReplicaSet("shop", "payment-66cd", "dep-uid", 2, "nginx:1.26")

	client := fake.NewSimpleClientset(dep, rs3, rs1, rs2)

	history, err := ListRolloutHistory(context.Background(), client, "shop", KindDeployment, "payment")
	if err != nil {
		t.Fatalf("ListRolloutHistory returned error: %v", err)
	}

	if len(history) != 3 {
		t.Fatalf("expected 3 revisions, got %d", len(history))
	}
	if history[0].Revision != 1 || history[1].Revision != 2 || history[2].Revision != 3 {
		t.Fatalf("expected revisions 1,2,3 in order, got %d,%d,%d", history[0].Revision, history[1].Revision, history[2].Revision)
	}
	if history[2].Current != true {
		t.Fatal("expected revision 3 to be marked as current")
	}
	if len(history[2].Images) != 1 || history[2].Images[0] != "app=nginx:1.27" {
		t.Fatalf("unexpected images for revision 3: %+v", history[2].Images)
	}
}

func TestUndoRolloutDeploymentToPreviousRevision(t *testing.T) {
	t.Parallel()

	replicas := int32(1)
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "payment",
			Namespace: "shop",
			UID:       "dep-uid",
			Annotations: map[string]string{
				"deployment.kubernetes.io/revision": "3",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "payment"}},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "nginx:1.27"}}},
			},
		},
		Status: appsv1.DeploymentStatus{
			ObservedGeneration: 1,
			UpdatedReplicas:    1,
			ReadyReplicas:      1,
			AvailableReplicas:  1,
		},
	}
	dep.Generation = 1

	rs1 := newReplicaSet("shop", "payment-5a6b", "dep-uid", 1, "nginx:1.25")
	rs2 := newReplicaSet("shop", "payment-66cd", "dep-uid", 2, "nginx:1.26")
	rs3 := newReplicaSet("shop", "payment-7f9f", "dep-uid", 3, "nginx:1.27")

	client := fake.NewSimpleClientset(dep, rs1, rs2, rs3)

	result, err := UndoRollout(context.Background(), client, "shop", KindDeployment, "payment", 0, 0, nil)
	if err != nil {
		t.Fatalf("UndoRollout returned error: %v", err)
	}
	if result.ToRevision != 2 {
		t.Fatalf("expected rollback to revision 2, got %d", result.ToRevision)
	}

	updated, err := client.AppsV1().Deployments("shop").Get(context.Background(), "payment", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get updated deployment: %v", err)
	}
	if len(updated.Spec.Template.Spec.Containers) != 1 {
		t.Fatalf("expected one container, got %d", len(updated.Spec.Template.Spec.Containers))
	}
	if updated.Spec.Template.Spec.Containers[0].Image != "nginx:1.26" {
		t.Fatalf("expected image nginx:1.26 after undo, got %q", updated.Spec.Template.Spec.Containers[0].Image)
	}
}

func TestSetWorkloadImagesUpdatesDeploymentContainers(t *testing.T) {
	t.Parallel()

	replicas := int32(1)
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "payment", Namespace: "shop"},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "app", Image: "nginx:1.25"},
						{Name: "sidecar", Image: "busybox:1.36"},
					},
				},
			},
		},
	}

	client := fake.NewSimpleClientset(dep)

	result, err := SetWorkloadImages(context.Background(), client, "shop", KindDeployment, "payment", map[string]string{
		"app": "nginx:1.27",
	})
	if err != nil {
		t.Fatalf("SetWorkloadImages returned error: %v", err)
	}

	if result.Updated["app"] != "nginx:1.27" {
		t.Fatalf("expected updated image map to contain app=nginx:1.27, got %+v", result.Updated)
	}

	updated, err := client.AppsV1().Deployments("shop").Get(context.Background(), "payment", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get updated deployment: %v", err)
	}

	if updated.Spec.Template.Spec.Containers[0].Image != "nginx:1.27" {
		t.Fatalf("expected app image nginx:1.27, got %q", updated.Spec.Template.Spec.Containers[0].Image)
	}
	if updated.Spec.Template.Spec.Containers[1].Image != "busybox:1.36" {
		t.Fatalf("expected sidecar image unchanged, got %q", updated.Spec.Template.Spec.Containers[1].Image)
	}
}

func TestSetWorkloadImagesErrorsWhenContainerMissing(t *testing.T) {
	t.Parallel()

	replicas := int32(1)
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "payment", Namespace: "shop"},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "nginx:1.25"}}},
			},
		},
	}

	client := fake.NewSimpleClientset(dep)

	if _, err := SetWorkloadImages(context.Background(), client, "shop", KindDeployment, "payment", map[string]string{
		"api": "nginx:1.27",
	}); err == nil {
		t.Fatal("expected error when requested container does not exist")
	}
}

func TestSetWorkloadImagesRetriesOnConflict(t *testing.T) {
	t.Parallel()

	replicas := int32(1)
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "payment", Namespace: "shop"},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "nginx:1.25"}}},
			},
		},
	}

	client := fake.NewSimpleClientset(dep)
	updateAttempts := 0
	client.PrependReactor("update", "deployments", func(action k8stesting.Action) (bool, runtime.Object, error) {
		updateAttempts++
		if updateAttempts == 1 {
			return true, nil, apierrors.NewConflict(
				schema.GroupResource{Group: "apps", Resource: "deployments"},
				"payment",
				errors.New("simulated conflict"),
			)
		}
		return false, nil, nil
	})

	result, err := SetWorkloadImages(context.Background(), client, "shop", KindDeployment, "payment", map[string]string{
		"app": "nginx:1.27",
	})
	if err != nil {
		t.Fatalf("SetWorkloadImages returned error: %v", err)
	}

	if result.Updated["app"] != "nginx:1.27" {
		t.Fatalf("expected updated image map to contain app=nginx:1.27, got %+v", result.Updated)
	}
	if updateAttempts < 2 {
		t.Fatalf("expected conflict retry to trigger at least 2 update attempts, got %d", updateAttempts)
	}
}

func TestUndoRolloutDeploymentRetriesOnConflict(t *testing.T) {
	t.Parallel()

	replicas := int32(1)
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "payment",
			Namespace: "shop",
			UID:       "dep-uid",
			Annotations: map[string]string{
				"deployment.kubernetes.io/revision": "3",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "payment"}},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "nginx:1.27"}}},
			},
		},
	}

	rs1 := newReplicaSet("shop", "payment-5a6b", "dep-uid", 1, "nginx:1.25")
	rs2 := newReplicaSet("shop", "payment-66cd", "dep-uid", 2, "nginx:1.26")
	rs3 := newReplicaSet("shop", "payment-7f9f", "dep-uid", 3, "nginx:1.27")

	client := fake.NewSimpleClientset(dep, rs1, rs2, rs3)
	updateAttempts := 0
	client.PrependReactor("update", "deployments", func(action k8stesting.Action) (bool, runtime.Object, error) {
		updateAttempts++
		if updateAttempts == 1 {
			return true, nil, apierrors.NewConflict(
				schema.GroupResource{Group: "apps", Resource: "deployments"},
				"payment",
				errors.New("simulated conflict"),
			)
		}
		return false, nil, nil
	})

	result, err := UndoRollout(context.Background(), client, "shop", KindDeployment, "payment", 0, 0, nil)
	if err != nil {
		t.Fatalf("UndoRollout returned error: %v", err)
	}

	if result.ToRevision != 2 {
		t.Fatalf("expected rollback to revision 2, got %d", result.ToRevision)
	}
	if updateAttempts < 2 {
		t.Fatalf("expected conflict retry to trigger at least 2 update attempts, got %d", updateAttempts)
	}
}

func newReplicaSet(namespace, name, deploymentUID string, revision int64, image string) *appsv1.ReplicaSet {
	return &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Annotations: map[string]string{
				"deployment.kubernetes.io/revision": strconv.FormatInt(revision, 10),
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       "payment",
					UID:        types.UID(deploymentUID),
				},
			},
		},
		Spec: appsv1.ReplicaSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: image}}},
			},
		},
	}
}
