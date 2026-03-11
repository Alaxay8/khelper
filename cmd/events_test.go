package cmd

import (
	"context"
	"testing"
	"time"

	"github.com/alaxay8/khelper/internal/kube"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

func TestFilterRelatedEventsAppliesScopeSinceAndWarnings(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 6, 12, 0, 0, 0, time.UTC)
	scope := eventScope{
		workloadKind: "deployment",
		workloadName: "payment",
		podNames: map[string]struct{}{
			"payment-abc": {},
		},
		podUIDs: map[string]struct{}{
			"pod-uid-1": {},
		},
		replicaSetNames: map[string]struct{}{
			"payment-7f4d9b4b5": {},
		},
	}

	events := []corev1.Event{
		{
			ObjectMeta:     metav1.ObjectMeta{Name: "deployment-info"},
			InvolvedObject: corev1.ObjectReference{Kind: "Deployment", Name: "payment"},
			Type:           corev1.EventTypeNormal,
			Reason:         "ScalingReplicaSet",
			LastTimestamp:  metav1.NewTime(now.Add(-5 * time.Minute)),
		},
		{
			ObjectMeta:     metav1.ObjectMeta{Name: "pod-warning"},
			InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "payment-abc", UID: types.UID("pod-uid-1")},
			Type:           corev1.EventTypeWarning,
			Reason:         "BackOff",
			LastTimestamp:  metav1.NewTime(now.Add(-2 * time.Minute)),
		},
		{
			ObjectMeta:     metav1.ObjectMeta{Name: "rs-old-warning"},
			InvolvedObject: corev1.ObjectReference{Kind: "ReplicaSet", Name: "payment-7f4d9b4b5"},
			Type:           corev1.EventTypeWarning,
			Reason:         "FailedCreate",
			LastTimestamp:  metav1.NewTime(now.Add(-2 * time.Hour)),
		},
		{
			ObjectMeta:     metav1.ObjectMeta{Name: "other-pod-warning"},
			InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "checkout-xyz"},
			Type:           corev1.EventTypeWarning,
			Reason:         "BackOff",
			LastTimestamp:  metav1.NewTime(now.Add(-1 * time.Minute)),
		},
	}

	filtered := filterRelatedEvents(events, scope, time.Hour, now, true)
	if len(filtered) != 1 {
		t.Fatalf("expected 1 warning event in scope and time window, got %d", len(filtered))
	}
	if filtered[0].Name != "pod-warning" {
		t.Fatalf("expected pod-warning, got %s", filtered[0].Name)
	}
}

func TestEventTimestampPrefersMostSpecificField(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 3, 6, 9, 0, 0, 0, time.UTC)
	wantEventTime := base.Add(-1 * time.Minute)
	wantSeriesTime := base.Add(-2 * time.Minute)
	wantLast := base.Add(-3 * time.Minute)
	wantFirst := base.Add(-4 * time.Minute)
	wantCreated := base.Add(-5 * time.Minute)

	cases := []struct {
		name  string
		event corev1.Event
		want  time.Time
	}{
		{
			name: "event time",
			event: corev1.Event{
				EventTime: metav1.NewMicroTime(wantEventTime),
				Series: &corev1.EventSeries{
					LastObservedTime: metav1.NewMicroTime(wantSeriesTime),
				},
				LastTimestamp:  metav1.NewTime(wantLast),
				FirstTimestamp: metav1.NewTime(wantFirst),
				ObjectMeta:     metav1.ObjectMeta{CreationTimestamp: metav1.NewTime(wantCreated)},
			},
			want: wantEventTime,
		},
		{
			name: "series time",
			event: corev1.Event{
				Series: &corev1.EventSeries{
					LastObservedTime: metav1.NewMicroTime(wantSeriesTime),
				},
				LastTimestamp:  metav1.NewTime(wantLast),
				FirstTimestamp: metav1.NewTime(wantFirst),
				ObjectMeta:     metav1.ObjectMeta{CreationTimestamp: metav1.NewTime(wantCreated)},
			},
			want: wantSeriesTime,
		},
		{
			name: "last timestamp",
			event: corev1.Event{
				LastTimestamp:  metav1.NewTime(wantLast),
				FirstTimestamp: metav1.NewTime(wantFirst),
				ObjectMeta:     metav1.ObjectMeta{CreationTimestamp: metav1.NewTime(wantCreated)},
			},
			want: wantLast,
		},
		{
			name: "first timestamp",
			event: corev1.Event{
				FirstTimestamp: metav1.NewTime(wantFirst),
				ObjectMeta:     metav1.ObjectMeta{CreationTimestamp: metav1.NewTime(wantCreated)},
			},
			want: wantFirst,
		},
		{
			name: "creation timestamp",
			event: corev1.Event{
				ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.NewTime(wantCreated)},
			},
			want: wantCreated,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := eventTimestamp(tc.event)
			if !got.Equal(tc.want) {
				t.Fatalf("expected %s, got %s", tc.want.Format(time.RFC3339), got.Format(time.RFC3339))
			}
		})
	}
}

func TestIsEventInScopeMatchesPodByUID(t *testing.T) {
	t.Parallel()

	scope := eventScope{
		workloadKind: "deployment",
		workloadName: "payment",
		podNames:     map[string]struct{}{},
		podUIDs: map[string]struct{}{
			"pod-uid-42": {},
		},
		replicaSetNames: map[string]struct{}{},
	}

	event := corev1.Event{
		InvolvedObject: corev1.ObjectReference{
			Kind: "Pod",
			Name: "payment-old-name",
			UID:  types.UID("pod-uid-42"),
		},
	}

	if !isEventInScope(event, scope) {
		t.Fatal("expected event to match scope by pod UID")
	}
}

func TestFilterRelatedEventsMatchesDeletedDeploymentPodEvents(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 6, 12, 0, 0, 0, time.UTC)
	workload := kube.WorkloadRef{
		Kind: kube.KindDeployment,
		Name: "frontend",
	}

	scope := buildEventScope(workload, nil, map[string]struct{}{
		"frontend-84578d7b58": {},
	})

	events := []corev1.Event{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "stale-pod-warning"},
			InvolvedObject: corev1.ObjectReference{
				Kind: "Pod",
				Name: "frontend-84578d7b58-6v59m",
			},
			Type:          corev1.EventTypeWarning,
			Reason:        "Unhealthy",
			Message:       "Readiness probe failed",
			LastTimestamp: metav1.NewTime(now.Add(-90 * time.Second)),
		},
	}

	filtered := filterRelatedEvents(events, scope, 15*time.Minute, now, true)
	if len(filtered) != 1 {
		t.Fatalf("expected stale deployment pod warning to match, got %d events", len(filtered))
	}
	if filtered[0].Name != "stale-pod-warning" {
		t.Fatalf("expected stale-pod-warning, got %s", filtered[0].Name)
	}
}

func TestRelatedReplicaSetNamesUsesWorkloadNamespace(t *testing.T) {
	t.Parallel()

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "payment",
			Namespace: "shop",
			UID:       types.UID("dep-uid"),
		},
	}
	replicaSet := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "payment-7f4d9b4b5",
			Namespace: "shop",
			Labels:    map[string]string{"app": "payment"},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       "payment",
				},
			},
		},
	}

	bundle := &kube.ClientBundle{
		Clientset: fake.NewSimpleClientset(deployment, replicaSet),
		Namespace: "default",
	}
	workload := kube.WorkloadRef{
		Kind:      kube.KindDeployment,
		Name:      "payment",
		Namespace: "shop",
		Selector:  "app=payment",
	}

	names, err := relatedReplicaSetNames(context.Background(), bundle, workload)
	if err != nil {
		t.Fatalf("relatedReplicaSetNames returned error: %v", err)
	}

	if len(names) != 1 {
		t.Fatalf("expected 1 related ReplicaSet name, got %d", len(names))
	}
	if _, ok := names["payment-7f4d9b4b5"]; !ok {
		t.Fatalf("expected ReplicaSet payment-7f4d9b4b5, got %+v", names)
	}
}

func TestRelatedReplicaSetNamesIgnoresPrefixMatchWithoutOwner(t *testing.T) {
	t.Parallel()

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "payment",
			Namespace: "shop",
			UID:       types.UID("dep-uid"),
		},
	}
	replicaSet := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "payment-7f4d9b4b5",
			Namespace: "shop",
			Labels:    map[string]string{"app": "payment"},
		},
	}

	bundle := &kube.ClientBundle{
		Clientset: fake.NewSimpleClientset(deployment, replicaSet),
		Namespace: "default",
	}
	workload := kube.WorkloadRef{
		Kind:      kube.KindDeployment,
		Name:      "payment",
		Namespace: "shop",
		Selector:  "app=payment",
	}

	names, err := relatedReplicaSetNames(context.Background(), bundle, workload)
	if err != nil {
		t.Fatalf("relatedReplicaSetNames returned error: %v", err)
	}

	if len(names) != 0 {
		t.Fatalf("expected no ReplicaSet names for prefix-only match, got %+v", names)
	}
}

func TestRelatedReplicaSetNamesIgnoresOwnerUIDMismatch(t *testing.T) {
	t.Parallel()

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "payment",
			Namespace: "shop",
			UID:       types.UID("dep-current"),
		},
	}
	replicaSet := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "payment-7f4d9b4b5",
			Namespace: "shop",
			Labels:    map[string]string{"app": "payment"},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       "payment",
					UID:        types.UID("dep-old"),
				},
			},
		},
	}

	bundle := &kube.ClientBundle{
		Clientset: fake.NewSimpleClientset(deployment, replicaSet),
		Namespace: "default",
	}
	workload := kube.WorkloadRef{
		Kind:      kube.KindDeployment,
		Name:      "payment",
		Namespace: "shop",
		Selector:  "app=payment",
	}

	names, err := relatedReplicaSetNames(context.Background(), bundle, workload)
	if err != nil {
		t.Fatalf("relatedReplicaSetNames returned error: %v", err)
	}

	if len(names) != 0 {
		t.Fatalf("expected no ReplicaSet names for owner UID mismatch, got %+v", names)
	}
}
