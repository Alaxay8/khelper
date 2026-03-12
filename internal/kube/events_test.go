package kube

import (
	"context"
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestListEventsByObjectsReturnsOnlyRequested(t *testing.T) {
	t.Parallel()

	client := fake.NewSimpleClientset(
		testEvent("dep-event", "shop", "Deployment", "payment"),
		testEvent("pod-event", "shop", "Pod", "payment-abc"),
		testEvent("rs-event", "shop", "ReplicaSet", "payment-7f4d9b4b5"),
		testEvent("other-event", "shop", "Pod", "checkout-abc"),
	)

	events, err := ListEventsByObjects(context.Background(), client, "shop", []EventObjectRef{
		{Kind: KindDeployment, Name: "payment"},
		{Kind: KindPod, Name: "payment-abc"},
		{Kind: "replicaset", Name: "payment-7f4d9b4b5"},
	})
	if err != nil {
		t.Fatalf("ListEventsByObjects returned error: %v", err)
	}

	if len(events) != 3 {
		t.Fatalf("expected 3 matching events, got %d", len(events))
	}

	got := map[string]struct{}{}
	for i := range events {
		got[events[i].Name] = struct{}{}
	}

	for _, name := range []string{"dep-event", "pod-event", "rs-event"} {
		if _, ok := got[name]; !ok {
			t.Fatalf("expected event %q in results, got %+v", name, got)
		}
	}
	if _, ok := got["other-event"]; ok {
		t.Fatalf("did not expect unrelated event in results: %+v", got)
	}
}

func TestListEventsByObjectsUsesObjectScopedQueriesForLargeInput(t *testing.T) {
	t.Parallel()

	client := fake.NewSimpleClientset(
		testEvent("pod-event-0", "shop", "Pod", "payment-00"),
		testEvent("pod-event-1", "shop", "Pod", "payment-01"),
		testEvent("pod-event-foreign", "shop", "Pod", "checkout-00"),
	)

	var fieldSelectors []string
	client.PrependReactor("list", "events", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
		listAction, ok := action.(k8stesting.ListAction)
		if !ok {
			t.Fatalf("expected ListAction, got %T", action)
		}
		fieldSelectors = append(fieldSelectors, listAction.GetListRestrictions().Fields.String())
		return false, nil, nil
	})

	refs := make([]EventObjectRef, 0, 25)
	for i := 0; i < 25; i++ {
		refs = append(refs, EventObjectRef{
			Kind: KindPod,
			Name: fmt.Sprintf("payment-%02d", i),
		})
	}

	events, err := ListEventsByObjects(context.Background(), client, "shop", refs)
	if err != nil {
		t.Fatalf("ListEventsByObjects returned error: %v", err)
	}

	if len(events) != 2 {
		t.Fatalf("expected 2 matching pod events, got %d", len(events))
	}

	got := map[string]struct{}{}
	for i := range events {
		got[events[i].Name] = struct{}{}
	}
	if _, ok := got["pod-event-0"]; !ok {
		t.Fatalf("expected pod-event-0 in results, got %+v", got)
	}
	if _, ok := got["pod-event-1"]; !ok {
		t.Fatalf("expected pod-event-1 in results, got %+v", got)
	}
	if _, ok := got["pod-event-foreign"]; ok {
		t.Fatalf("did not expect foreign pod event in results: %+v", got)
	}

	if len(fieldSelectors) != len(refs) {
		t.Fatalf("expected %d object-scoped list calls, got %d (%v)", len(refs), len(fieldSelectors), fieldSelectors)
	}
	for i := range fieldSelectors {
		selector := fieldSelectors[i]
		if selector == "involvedObject.kind=Pod" {
			t.Fatalf("unexpected kind-wide selector %q; expected object-scoped selector", selector)
		}
	}
}

func TestListEventsByObjectsWithPodNamePrefixesIncludesPrefixMatches(t *testing.T) {
	t.Parallel()

	client := fake.NewSimpleClientset(
		testEvent("stale-pod-event", "shop", "Pod", "frontend-84578d7b58-6v59m"),
		testEvent("current-pod-event", "shop", "Pod", "frontend-84578d7b58-k8mtn"),
		testEvent("foreign-pod-event", "shop", "Pod", "checkout-7bf5f9d8cf-2nfhx"),
	)

	events, err := ListEventsByObjectsWithPodNamePrefixes(context.Background(), client, "shop", []EventObjectRef{
		{Kind: KindDeployment, Name: "frontend"},
		{Kind: "replicaset", Name: "frontend-84578d7b58"},
	}, []string{"frontend-84578d7b58-"})
	if err != nil {
		t.Fatalf("ListEventsByObjectsWithPodNamePrefixes returned error: %v", err)
	}

	if len(events) != 2 {
		t.Fatalf("expected 2 matching pod prefix events, got %d", len(events))
	}

	got := map[string]struct{}{}
	for i := range events {
		got[events[i].Name] = struct{}{}
	}
	for _, name := range []string{"stale-pod-event", "current-pod-event"} {
		if _, ok := got[name]; !ok {
			t.Fatalf("expected event %q in results, got %+v", name, got)
		}
	}
	if _, ok := got["foreign-pod-event"]; ok {
		t.Fatalf("did not expect foreign pod event in results: %+v", got)
	}
}

func testEvent(name, namespace, kind, objectName string) *corev1.Event {
	return &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		InvolvedObject: corev1.ObjectReference{
			Kind: kind,
			Name: objectName,
		},
	}
}
