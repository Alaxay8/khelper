package kube

import (
	"context"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
)

const eventObjectBatchThreshold = 20

// EventObjectRef identifies an object we want events for.
type EventObjectRef struct {
	Kind string
	Name string
}

// ListEventsByObjects fetches events for a specific set of involved objects.
// It deduplicates events returned from multiple API requests.
func ListEventsByObjects(ctx context.Context, client kubernetes.Interface, namespace string, refs []EventObjectRef) ([]corev1.Event, error) {
	grouped := groupEventObjectRefs(refs)
	if len(grouped) == 0 {
		return nil, nil
	}

	dedup := make(map[string]corev1.Event)
	for kind, namesSet := range grouped {
		names := sortedSetKeys(namesSet)
		if len(names) == 0 {
			continue
		}

		if len(names) <= eventObjectBatchThreshold {
			for _, name := range names {
				items, err := listEventsByObject(ctx, client, namespace, kind, name)
				if err != nil {
					return nil, err
				}
				appendUniqueEvents(dedup, items)
			}
			continue
		}

		items, err := listEventsByKind(ctx, client, namespace, kind)
		if err != nil {
			return nil, err
		}
		filtered := filterEventsByNames(items, kind, namesSet)
		appendUniqueEvents(dedup, filtered)
	}

	return sortedEventValues(dedup), nil
}

func groupEventObjectRefs(refs []EventObjectRef) map[string]map[string]struct{} {
	grouped := make(map[string]map[string]struct{})
	for i := range refs {
		kind := normalizeEventObjectKind(refs[i].Kind)
		name := strings.TrimSpace(refs[i].Name)
		if kind == "" || name == "" {
			continue
		}
		if _, ok := grouped[kind]; !ok {
			grouped[kind] = make(map[string]struct{})
		}
		grouped[kind][name] = struct{}{}
	}
	return grouped
}

func listEventsByObject(ctx context.Context, client kubernetes.Interface, namespace, kind, name string) ([]corev1.Event, error) {
	kubeKind := involvedObjectFieldKind(kind)
	selector := fields.Set{
		"involvedObject.kind": kubeKind,
		"involvedObject.name": name,
	}.String()
	list, err := client.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{FieldSelector: selector})
	if err != nil {
		return nil, fmt.Errorf("list events for %s/%s: %w", kind, name, err)
	}
	return filterEventsByObject(list.Items, kind, name), nil
}

func listEventsByKind(ctx context.Context, client kubernetes.Interface, namespace, kind string) ([]corev1.Event, error) {
	kubeKind := involvedObjectFieldKind(kind)
	selector := fields.Set{
		"involvedObject.kind": kubeKind,
	}.String()
	list, err := client.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{FieldSelector: selector})
	if err != nil {
		return nil, fmt.Errorf("list events for kind %s: %w", kind, err)
	}
	return filterEventsByKind(list.Items, kind), nil
}

func filterEventsByObject(events []corev1.Event, kind, name string) []corev1.Event {
	filtered := make([]corev1.Event, 0, len(events))
	for i := range events {
		event := events[i]
		if normalizeEventObjectKind(event.InvolvedObject.Kind) != kind {
			continue
		}
		if strings.TrimSpace(event.InvolvedObject.Name) != name {
			continue
		}
		filtered = append(filtered, event)
	}
	return filtered
}

func filterEventsByKind(events []corev1.Event, kind string) []corev1.Event {
	filtered := make([]corev1.Event, 0, len(events))
	for i := range events {
		event := events[i]
		if normalizeEventObjectKind(event.InvolvedObject.Kind) != kind {
			continue
		}
		filtered = append(filtered, event)
	}
	return filtered
}

func filterEventsByNames(events []corev1.Event, kind string, names map[string]struct{}) []corev1.Event {
	filtered := make([]corev1.Event, 0, len(events))
	for i := range events {
		event := events[i]
		if normalizeEventObjectKind(event.InvolvedObject.Kind) != kind {
			continue
		}
		if _, ok := names[strings.TrimSpace(event.InvolvedObject.Name)]; !ok {
			continue
		}
		filtered = append(filtered, event)
	}
	return filtered
}

func normalizeEventObjectKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "deployment", "deployments", "deployment.apps":
		return "deployment"
	case "statefulset", "statefulsets", "statefulset.apps":
		return "statefulset"
	case "pod", "pods":
		return "pod"
	case "replicaset", "replicasets", "replicaset.apps":
		return "replicaset"
	default:
		return strings.ToLower(strings.TrimSpace(kind))
	}
}

func involvedObjectFieldKind(kind string) string {
	switch normalizeEventObjectKind(kind) {
	case "deployment":
		return "Deployment"
	case "statefulset":
		return "StatefulSet"
	case "pod":
		return "Pod"
	case "replicaset":
		return "ReplicaSet"
	default:
		return strings.TrimSpace(kind)
	}
}

func appendUniqueEvents(dedup map[string]corev1.Event, events []corev1.Event) {
	for i := range events {
		event := events[i]
		key := eventDedupKey(event)
		if _, exists := dedup[key]; exists {
			continue
		}
		dedup[key] = event
	}
}

func eventDedupKey(event corev1.Event) string {
	if event.UID != "" {
		return "uid:" + string(event.UID)
	}
	return fmt.Sprintf("ns:%s|name:%s", event.Namespace, event.Name)
}

func sortedEventValues(dedup map[string]corev1.Event) []corev1.Event {
	items := make([]corev1.Event, 0, len(dedup))
	for _, event := range dedup {
		items = append(items, event)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Namespace != items[j].Namespace {
			return items[i].Namespace < items[j].Namespace
		}
		return items[i].Name < items[j].Name
	})
	return items
}

func sortedSetKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
