package doctor

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/alaxay8/khelper/internal/kube"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const maxLogEvidenceBytes = 4096

func Collect(ctx context.Context, bundle *kube.ClientBundle, target string, options CollectOptions) (*Snapshot, error) {
	if bundle == nil {
		return nil, fmt.Errorf("client bundle is required")
	}
	target = strings.TrimSpace(target)
	if target == "" {
		return nil, fmt.Errorf("target is required")
	}

	resolver := kube.NewResolver(bundle.Clientset)
	namespaceScope := strings.TrimSpace(options.Namespace)
	if namespaceScope == "" {
		namespaceScope = bundle.Namespace
	}

	workload, err := resolver.ResolveWorkload(ctx, namespaceScope, target, options.Kind, options.Pick)
	if err != nil {
		return nil, err
	}

	snapshot := &Snapshot{
		Namespace: workload.Namespace,
		Target:    target,
		Workload:  workload,
		Since:     options.Since,
	}

	if err := hydrateWorkloadStatus(ctx, bundle, snapshot); err != nil {
		return nil, err
	}

	pods, err := listWorkloadPods(ctx, bundle, workload)
	if err != nil {
		return nil, err
	}
	snapshot.Pods = pods

	resolvedPod, err := resolver.ResolvePod(ctx, workload.Namespace, workload.Name, workload.Kind, 0)
	if err == nil {
		snapshot.SelectedPod = resolvedPod.Pod
		snapshot.SelectedPodWarning = resolvedPod.Warning
	} else {
		var notFound *kube.NotFoundError
		if !errors.As(err, &notFound) {
			return nil, err
		}
	}

	events, err := listRelatedEvents(ctx, bundle, snapshot, options)
	if err != nil {
		return nil, err
	}
	snapshot.Events = events

	if options.LogsTail > 0 && snapshot.SelectedPod != nil {
		logSnippet, err := captureLogSnippet(ctx, bundle, snapshot.SelectedPod, options)
		if err != nil {
			return nil, err
		}
		snapshot.LogSnippet = logSnippet
	}

	return snapshot, nil
}

func hydrateWorkloadStatus(ctx context.Context, bundle *kube.ClientBundle, snapshot *Snapshot) error {
	namespace := snapshot.Namespace
	if namespace == "" {
		namespace = bundle.Namespace
	}

	switch snapshot.Workload.Kind {
	case kube.KindDeployment:
		dep, err := bundle.Clientset.AppsV1().Deployments(namespace).Get(ctx, snapshot.Workload.Name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return &kube.NotFoundError{Namespace: namespace, Target: snapshot.Workload.Name, Kind: kube.KindDeployment}
			}
			return fmt.Errorf("get deployment %s: %w", snapshot.Workload.Name, err)
		}
		snapshot.Deployment = dep
	case kube.KindStatefulSet:
		sts, err := bundle.Clientset.AppsV1().StatefulSets(namespace).Get(ctx, snapshot.Workload.Name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return &kube.NotFoundError{Namespace: namespace, Target: snapshot.Workload.Name, Kind: kube.KindStatefulSet}
			}
			return fmt.Errorf("get statefulset %s: %w", snapshot.Workload.Name, err)
		}
		snapshot.StatefulSet = sts
	}

	return nil
}

func listWorkloadPods(ctx context.Context, bundle *kube.ClientBundle, workload kube.WorkloadRef) ([]corev1.Pod, error) {
	namespace := workload.Namespace
	if namespace == "" {
		namespace = bundle.Namespace
	}

	switch workload.Kind {
	case kube.KindPod:
		pod, err := bundle.Clientset.CoreV1().Pods(namespace).Get(ctx, workload.Name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return nil, &kube.NotFoundError{Namespace: namespace, Target: workload.Name, Kind: kube.KindPod}
			}
			return nil, fmt.Errorf("get pod %s: %w", workload.Name, err)
		}
		return []corev1.Pod{*pod}, nil
	default:
		if workload.Selector == "" {
			return nil, fmt.Errorf("workload %s/%s has no pod selector", workload.Kind, workload.Name)
		}
		list, err := bundle.Clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: workload.Selector})
		if err != nil {
			return nil, fmt.Errorf("list pods with selector %q: %w", workload.Selector, err)
		}

		pods := append([]corev1.Pod(nil), list.Items...)
		sort.SliceStable(pods, func(i, j int) bool {
			return pods[i].Name < pods[j].Name
		})
		return pods, nil
	}
}

func listRelatedEvents(ctx context.Context, bundle *kube.ClientBundle, snapshot *Snapshot, options CollectOptions) ([]corev1.Event, error) {
	namespace := snapshot.Namespace
	if namespace == "" {
		namespace = bundle.Namespace
	}

	relatedReplicaSets, err := relatedReplicaSetNames(ctx, bundle, snapshot.Workload)
	if err != nil {
		return nil, err
	}

	refs := relatedEventObjectRefs(snapshot, relatedReplicaSets)
	list, err := kube.ListEventsByObjectsWithPodNamePrefixes(ctx, bundle.Clientset, namespace, refs, podNamePrefixes(snapshot, relatedReplicaSets))
	if err != nil {
		return nil, fmt.Errorf("list related events: %w", err)
	}

	now := options.Now
	if now.IsZero() {
		now = time.Now()
	}
	var cutoff time.Time
	if options.Since > 0 {
		cutoff = now.Add(-options.Since)
	}

	related := make([]corev1.Event, 0)
	for _, event := range list {
		if !isRelatedEvent(event, snapshot, relatedReplicaSets) {
			continue
		}
		ts := eventTimestamp(event)
		if !cutoff.IsZero() && !ts.IsZero() && ts.Before(cutoff) {
			continue
		}
		related = append(related, event)
	}

	sort.SliceStable(related, func(i, j int) bool {
		left := eventTimestamp(related[i])
		right := eventTimestamp(related[j])
		if left.Equal(right) {
			return related[i].Name < related[j].Name
		}
		if left.IsZero() {
			return false
		}
		if right.IsZero() {
			return true
		}
		return left.After(right)
	})

	return related, nil
}

func relatedEventObjectRefs(snapshot *Snapshot, relatedReplicaSets map[string]struct{}) []kube.EventObjectRef {
	if snapshot == nil {
		return nil
	}

	refs := make([]kube.EventObjectRef, 0, len(snapshot.Pods)+len(relatedReplicaSets)+1)
	if strings.TrimSpace(snapshot.Workload.Kind) != "" && strings.TrimSpace(snapshot.Workload.Name) != "" {
		refs = append(refs, kube.EventObjectRef{
			Kind: snapshot.Workload.Kind,
			Name: snapshot.Workload.Name,
		})
	}

	podNames := make([]string, 0, len(snapshot.Pods))
	for i := range snapshot.Pods {
		name := strings.TrimSpace(snapshot.Pods[i].Name)
		if name == "" {
			continue
		}
		podNames = append(podNames, name)
	}
	sort.Strings(podNames)
	for i := range podNames {
		refs = append(refs, kube.EventObjectRef{
			Kind: kube.KindPod,
			Name: podNames[i],
		})
	}

	replicaSetNames := make([]string, 0, len(relatedReplicaSets))
	for name := range relatedReplicaSets {
		replicaSetNames = append(replicaSetNames, name)
	}
	sort.Strings(replicaSetNames)
	for i := range replicaSetNames {
		refs = append(refs, kube.EventObjectRef{
			Kind: "replicaset",
			Name: replicaSetNames[i],
		})
	}

	return refs
}

func podNamePrefixes(snapshot *Snapshot, relatedReplicaSets map[string]struct{}) []string {
	if snapshot == nil {
		return nil
	}

	prefixes := make([]string, 0, len(relatedReplicaSets)+1)
	switch snapshot.Workload.Kind {
	case kube.KindDeployment:
		for name := range relatedReplicaSets {
			if strings.TrimSpace(name) == "" {
				continue
			}
			prefixes = append(prefixes, name+"-")
		}
	case kube.KindStatefulSet:
		name := strings.TrimSpace(snapshot.Workload.Name)
		if name != "" {
			prefixes = append(prefixes, name+"-")
		}
	}

	sort.Strings(prefixes)
	return prefixes
}

func relatedReplicaSetNames(ctx context.Context, bundle *kube.ClientBundle, workload kube.WorkloadRef) (map[string]struct{}, error) {
	names := make(map[string]struct{})
	if workload.Kind != kube.KindDeployment || workload.Selector == "" {
		return names, nil
	}

	replicaSets, err := bundle.Clientset.AppsV1().ReplicaSets(workload.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: workload.Selector,
	})
	if err != nil {
		return nil, fmt.Errorf("list related replica sets: %w", err)
	}

	for i := range replicaSets.Items {
		rs := replicaSets.Items[i]
		if replicaSetOwnedByDeployment(rs, workload.Name) {
			names[rs.Name] = struct{}{}
		}
	}

	return names, nil
}

func replicaSetOwnedByDeployment(rs appsv1.ReplicaSet, deploymentName string) bool {
	for _, owner := range rs.OwnerReferences {
		if strings.EqualFold(owner.Kind, "Deployment") && owner.Name == deploymentName {
			return true
		}
	}
	return strings.HasPrefix(rs.Name, deploymentName+"-")
}

func isRelatedEvent(event corev1.Event, snapshot *Snapshot, relatedReplicaSets map[string]struct{}) bool {
	kind := normalizeRelatedEventKind(event.InvolvedObject.Kind)
	name := event.InvolvedObject.Name

	if eventMatchesWorkload(kind, name, snapshot.Workload) {
		return true
	}

	for _, pod := range snapshot.Pods {
		if kind != "pod" {
			continue
		}
		if name == pod.Name {
			return true
		}
		if event.InvolvedObject.UID != "" && event.InvolvedObject.UID == pod.UID {
			return true
		}
	}

	if kind == "replicaset" {
		if _, ok := relatedReplicaSets[name]; ok {
			return true
		}
	}

	return false
}

func eventMatchesWorkload(kind, name string, workload kube.WorkloadRef) bool {
	kind = normalizeRelatedEventKind(kind)
	if name != workload.Name {
		return false
	}

	expectedKind := strings.ToLower(workload.Kind)
	if expectedKind == "" {
		return false
	}
	if kind == expectedKind {
		return true
	}

	switch expectedKind {
	case kube.KindDeployment:
		return kind == "deployment"
	case kube.KindStatefulSet:
		return kind == "statefulset"
	case kube.KindPod:
		return kind == "pod"
	default:
		return false
	}
}

func normalizeRelatedEventKind(kind string) string {
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

func eventTimestamp(event corev1.Event) time.Time {
	if !event.EventTime.IsZero() {
		return event.EventTime.Time
	}
	if event.Series != nil && !event.Series.LastObservedTime.IsZero() {
		return event.Series.LastObservedTime.Time
	}
	if !event.LastTimestamp.IsZero() {
		return event.LastTimestamp.Time
	}
	if !event.FirstTimestamp.IsZero() {
		return event.FirstTimestamp.Time
	}
	return event.CreationTimestamp.Time
}

func captureLogSnippet(ctx context.Context, bundle *kube.ClientBundle, pod *corev1.Pod, options CollectOptions) (*LogSnippet, error) {
	container, err := chooseContainerForLogs(pod, options.Container)
	if err != nil {
		return nil, err
	}
	if container == "" {
		return nil, nil
	}

	tail := options.LogsTail
	if tail <= 0 {
		return nil, nil
	}

	opts := &corev1.PodLogOptions{Container: container, TailLines: &tail}
	req := bundle.Clientset.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, opts)
	logSnippet := &LogSnippet{Pod: pod.Name, Container: container, Tail: tail}

	raw, err := req.DoRaw(ctx)
	if err != nil {
		logSnippet.Error = err.Error()
		return logSnippet, nil
	}

	text := strings.TrimSpace(string(raw))
	if len(text) > maxLogEvidenceBytes {
		text = text[len(text)-maxLogEvidenceBytes:]
	}
	logSnippet.Text = text

	return logSnippet, nil
}

func chooseContainerForLogs(pod *corev1.Pod, requested string) (string, error) {
	if pod == nil {
		return "", nil
	}

	requested = strings.TrimSpace(requested)
	if requested != "" {
		if !kube.PodHasContainer(pod, requested) {
			return "", &InvalidContainerError{Pod: pod.Name, Container: requested}
		}
		return requested, nil
	}

	if len(pod.Spec.Containers) == 0 {
		return "", nil
	}

	for _, status := range pod.Status.ContainerStatuses {
		if status.State.Waiting != nil || status.State.Terminated != nil || status.RestartCount > 0 {
			return status.Name, nil
		}
	}

	return pod.Spec.Containers[0].Name, nil
}

func replicasOrDefault(value *int32, fallback int32) int32 {
	if value == nil {
		return fallback
	}
	return *value
}

func deploymentObject(dep *appsv1.Deployment) string {
	if dep == nil {
		return "deployment/unknown"
	}
	return fmt.Sprintf("deployment/%s", dep.Name)
}

func statefulSetObject(sts *appsv1.StatefulSet) string {
	if sts == nil {
		return "statefulset/unknown"
	}
	return fmt.Sprintf("statefulset/%s", sts.Name)
}
