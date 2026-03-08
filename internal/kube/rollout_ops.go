package kube

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	deploymentRevisionAnnotation = "deployment.kubernetes.io/revision"
	changeCauseAnnotation        = "kubernetes.io/change-cause"
	podTemplateHashLabel         = "pod-template-hash"
)

// RolloutStatus describes current rollout state for a workload.
type RolloutStatus struct {
	Kind                string `json:"kind"`
	Name                string `json:"name"`
	Namespace           string `json:"namespace"`
	CurrentRevision     string `json:"currentRevision,omitempty"`
	UpdateRevision      string `json:"updateRevision,omitempty"`
	ObservedGeneration  int64  `json:"observedGeneration"`
	Generation          int64  `json:"generation"`
	DesiredReplicas     int32  `json:"desiredReplicas"`
	UpdatedReplicas     int32  `json:"updatedReplicas"`
	ReadyReplicas       int32  `json:"readyReplicas"`
	AvailableReplicas   int32  `json:"availableReplicas,omitempty"`
	UnavailableReplicas int32  `json:"unavailableReplicas,omitempty"`
	Complete            bool   `json:"complete"`
	Message             string `json:"message"`
}

// RolloutHistoryEntry describes one rollout revision.
type RolloutHistoryEntry struct {
	Revision    int64     `json:"revision"`
	Name        string    `json:"name,omitempty"`
	CreatedAt   time.Time `json:"createdAt,omitempty"`
	Images      []string  `json:"images,omitempty"`
	ChangeCause string    `json:"changeCause,omitempty"`
	Current     bool      `json:"current"`
}

// UndoRolloutResult contains details about a rollback operation.
type UndoRolloutResult struct {
	Kind         string `json:"kind"`
	Name         string `json:"name"`
	Namespace    string `json:"namespace"`
	FromRevision int64  `json:"fromRevision,omitempty"`
	ToRevision   int64  `json:"toRevision"`
}

// SetImageResult describes what container images were updated.
type SetImageResult struct {
	Kind      string            `json:"kind"`
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	Updated   map[string]string `json:"updated"`
}

func GetRolloutStatus(ctx context.Context, client kubernetes.Interface, namespace, kind, name string) (*RolloutStatus, error) {
	kind, err := normalizeRolloutKind(kind)
	if err != nil {
		return nil, err
	}

	switch kind {
	case KindDeployment:
		dep, err := client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return nil, &NotFoundError{Namespace: namespace, Target: name, Kind: KindDeployment}
			}
			return nil, fmt.Errorf("get deployment %s: %w", name, err)
		}
		desired := int32(1)
		if dep.Spec.Replicas != nil {
			desired = *dep.Spec.Replicas
		}
		complete := dep.Status.ObservedGeneration >= dep.Generation &&
			dep.Status.UpdatedReplicas == desired &&
			dep.Status.ReadyReplicas == desired &&
			dep.Status.AvailableReplicas == desired &&
			dep.Status.UnavailableReplicas == 0
		message := "rollout in progress"
		if complete {
			message = "rollout complete"
		}
		return &RolloutStatus{
			Kind:                KindDeployment,
			Name:                dep.Name,
			Namespace:           dep.Namespace,
			CurrentRevision:     strings.TrimSpace(dep.Annotations[deploymentRevisionAnnotation]),
			ObservedGeneration:  dep.Status.ObservedGeneration,
			Generation:          dep.Generation,
			DesiredReplicas:     desired,
			UpdatedReplicas:     dep.Status.UpdatedReplicas,
			ReadyReplicas:       dep.Status.ReadyReplicas,
			AvailableReplicas:   dep.Status.AvailableReplicas,
			UnavailableReplicas: dep.Status.UnavailableReplicas,
			Complete:            complete,
			Message:             message,
		}, nil
	case KindStatefulSet:
		sts, err := client.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return nil, &NotFoundError{Namespace: namespace, Target: name, Kind: KindStatefulSet}
			}
			return nil, fmt.Errorf("get statefulset %s: %w", name, err)
		}
		desired := int32(1)
		if sts.Spec.Replicas != nil {
			desired = *sts.Spec.Replicas
		}
		complete := sts.Status.ObservedGeneration >= sts.Generation &&
			sts.Status.UpdatedReplicas == desired &&
			sts.Status.ReadyReplicas == desired
		if desired > 0 {
			complete = complete && sts.Status.CurrentRevision == sts.Status.UpdateRevision
		}
		message := "rollout in progress"
		if complete {
			message = "rollout complete"
		}
		return &RolloutStatus{
			Kind:               KindStatefulSet,
			Name:               sts.Name,
			Namespace:          sts.Namespace,
			CurrentRevision:    sts.Status.CurrentRevision,
			UpdateRevision:     sts.Status.UpdateRevision,
			ObservedGeneration: sts.Status.ObservedGeneration,
			Generation:         sts.Generation,
			DesiredReplicas:    desired,
			UpdatedReplicas:    sts.Status.UpdatedReplicas,
			ReadyReplicas:      sts.Status.ReadyReplicas,
			Complete:           complete,
			Message:            message,
		}, nil
	default:
		return nil, fmt.Errorf("rollout status supports only deployment/statefulset, got %q", kind)
	}
}

func ListRolloutHistory(ctx context.Context, client kubernetes.Interface, namespace, kind, name string) ([]RolloutHistoryEntry, error) {
	kind, err := normalizeRolloutKind(kind)
	if err != nil {
		return nil, err
	}

	switch kind {
	case KindDeployment:
		return listDeploymentHistory(ctx, client, namespace, name)
	case KindStatefulSet:
		return listStatefulSetHistory(ctx, client, namespace, name)
	default:
		return nil, fmt.Errorf("rollout history supports only deployment/statefulset, got %q", kind)
	}
}

func UndoRollout(ctx context.Context, client kubernetes.Interface, namespace, kind, name string, toRevision int64, timeout time.Duration, out io.Writer) (*UndoRolloutResult, error) {
	kind, err := normalizeRolloutKind(kind)
	if err != nil {
		return nil, err
	}

	switch kind {
	case KindDeployment:
		return undoDeploymentRollout(ctx, client, namespace, name, toRevision, timeout, out)
	case KindStatefulSet:
		return undoStatefulSetRollout(ctx, client, namespace, name, toRevision, timeout, out)
	default:
		return nil, fmt.Errorf("rollout undo supports only deployment/statefulset, got %q", kind)
	}
}

func SetWorkloadImages(ctx context.Context, client kubernetes.Interface, namespace, kind, name string, updates map[string]string) (*SetImageResult, error) {
	kind, err := normalizeRolloutKind(kind)
	if err != nil {
		return nil, err
	}
	if len(updates) == 0 {
		return nil, fmt.Errorf("at least one container=image assignment is required")
	}

	switch kind {
	case KindDeployment:
		dep, err := client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return nil, &NotFoundError{Namespace: namespace, Target: name, Kind: KindDeployment}
			}
			return nil, fmt.Errorf("get deployment %s: %w", name, err)
		}
		applied, err := applyContainerImageUpdates(&dep.Spec.Template.Spec, updates)
		if err != nil {
			return nil, err
		}
		if _, err := client.AppsV1().Deployments(namespace).Update(ctx, dep, metav1.UpdateOptions{}); err != nil {
			return nil, fmt.Errorf("update deployment %s: %w", name, err)
		}
		return &SetImageResult{Kind: KindDeployment, Name: dep.Name, Namespace: dep.Namespace, Updated: applied}, nil
	case KindStatefulSet:
		sts, err := client.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return nil, &NotFoundError{Namespace: namespace, Target: name, Kind: KindStatefulSet}
			}
			return nil, fmt.Errorf("get statefulset %s: %w", name, err)
		}
		applied, err := applyContainerImageUpdates(&sts.Spec.Template.Spec, updates)
		if err != nil {
			return nil, err
		}
		if _, err := client.AppsV1().StatefulSets(namespace).Update(ctx, sts, metav1.UpdateOptions{}); err != nil {
			return nil, fmt.Errorf("update statefulset %s: %w", name, err)
		}
		return &SetImageResult{Kind: KindStatefulSet, Name: sts.Name, Namespace: sts.Namespace, Updated: applied}, nil
	default:
		return nil, fmt.Errorf("set-image supports only deployment/statefulset, got %q", kind)
	}
}

// ResolveTagImageAssignments builds container=image updates from an existing workload image and a new tag.
// It updates exactly one container chosen from the pod template based on containerHint and workload shape.
func ResolveTagImageAssignments(ctx context.Context, client kubernetes.Interface, namespace, kind, name, containerHint, tag string) (map[string]string, error) {
	kind, err := normalizeRolloutKind(kind)
	if err != nil {
		return nil, err
	}

	var podSpec *corev1.PodSpec
	switch kind {
	case KindDeployment:
		dep, err := client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return nil, &NotFoundError{Namespace: namespace, Target: name, Kind: KindDeployment}
			}
			return nil, fmt.Errorf("get deployment %s: %w", name, err)
		}
		podSpec = &dep.Spec.Template.Spec
	case KindStatefulSet:
		sts, err := client.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return nil, &NotFoundError{Namespace: namespace, Target: name, Kind: KindStatefulSet}
			}
			return nil, fmt.Errorf("get statefulset %s: %w", name, err)
		}
		podSpec = &sts.Spec.Template.Spec
	default:
		return nil, fmt.Errorf("set-image supports only deployment/statefulset, got %q", kind)
	}

	return buildTagImageAssignments(*podSpec, containerHint, tag)
}

func listDeploymentHistory(ctx context.Context, client kubernetes.Interface, namespace, name string) ([]RolloutHistoryEntry, error) {
	dep, err := client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, &NotFoundError{Namespace: namespace, Target: name, Kind: KindDeployment}
		}
		return nil, fmt.Errorf("get deployment %s: %w", name, err)
	}

	list, err := client.AppsV1().ReplicaSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list replica sets for deployment %s: %w", name, err)
	}

	currentRevision := parseDeploymentRevision(dep.Annotations)
	entries := make([]RolloutHistoryEntry, 0, len(list.Items))
	for i := range list.Items {
		rs := list.Items[i]
		if !replicaSetOwnedByDeployment(rs, dep) {
			continue
		}

		revision := parseDeploymentRevision(rs.Annotations)
		entry := RolloutHistoryEntry{
			Revision:    revision,
			Name:        rs.Name,
			CreatedAt:   rs.CreationTimestamp.Time,
			Images:      podTemplateImages(rs.Spec.Template),
			ChangeCause: strings.TrimSpace(rs.Annotations[changeCauseAnnotation]),
			Current:     revision > 0 && revision == currentRevision,
		}
		entries = append(entries, entry)
	}

	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Revision == entries[j].Revision {
			if entries[i].CreatedAt.Equal(entries[j].CreatedAt) {
				return entries[i].Name < entries[j].Name
			}
			return entries[i].CreatedAt.Before(entries[j].CreatedAt)
		}
		return entries[i].Revision < entries[j].Revision
	})

	return entries, nil
}

func listStatefulSetHistory(ctx context.Context, client kubernetes.Interface, namespace, name string) ([]RolloutHistoryEntry, error) {
	sts, err := client.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, &NotFoundError{Namespace: namespace, Target: name, Kind: KindStatefulSet}
		}
		return nil, fmt.Errorf("get statefulset %s: %w", name, err)
	}

	list, err := client.AppsV1().ControllerRevisions(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list controller revisions for statefulset %s: %w", name, err)
	}

	entries := make([]RolloutHistoryEntry, 0, len(list.Items))
	for i := range list.Items {
		rev := list.Items[i]
		if !controllerRevisionOwnedByStatefulSet(rev, sts) {
			continue
		}

		template, err := podTemplateFromControllerRevision(rev)
		if err != nil {
			return nil, err
		}

		entry := RolloutHistoryEntry{
			Revision:    rev.Revision,
			Name:        rev.Name,
			CreatedAt:   rev.CreationTimestamp.Time,
			Images:      podTemplateImages(*template),
			ChangeCause: strings.TrimSpace(rev.Annotations[changeCauseAnnotation]),
			Current:     rev.Name == sts.Status.CurrentRevision,
		}
		entries = append(entries, entry)
	}

	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Revision == entries[j].Revision {
			if entries[i].CreatedAt.Equal(entries[j].CreatedAt) {
				return entries[i].Name < entries[j].Name
			}
			return entries[i].CreatedAt.Before(entries[j].CreatedAt)
		}
		return entries[i].Revision < entries[j].Revision
	})

	return entries, nil
}

func undoDeploymentRollout(ctx context.Context, client kubernetes.Interface, namespace, name string, toRevision int64, timeout time.Duration, out io.Writer) (*UndoRolloutResult, error) {
	dep, err := client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, &NotFoundError{Namespace: namespace, Target: name, Kind: KindDeployment}
		}
		return nil, fmt.Errorf("get deployment %s: %w", name, err)
	}

	list, err := client.AppsV1().ReplicaSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list replica sets for deployment %s: %w", name, err)
	}

	templatesByRevision := make(map[int64]corev1.PodTemplateSpec)
	revisionOrder := make([]int64, 0)
	for i := range list.Items {
		rs := list.Items[i]
		if !replicaSetOwnedByDeployment(rs, dep) {
			continue
		}
		revision := parseDeploymentRevision(rs.Annotations)
		if revision <= 0 {
			continue
		}
		if _, exists := templatesByRevision[revision]; !exists {
			revisionOrder = append(revisionOrder, revision)
		}
		templatesByRevision[revision] = *rs.Spec.Template.DeepCopy()
	}

	if len(templatesByRevision) == 0 {
		return nil, fmt.Errorf("no rollout revisions found for deployment/%s", name)
	}

	sort.SliceStable(revisionOrder, func(i, j int) bool { return revisionOrder[i] < revisionOrder[j] })
	currentRevision := parseDeploymentRevision(dep.Annotations)
	targetRevision, err := pickTargetRevision(revisionOrder, currentRevision, toRevision)
	if err != nil {
		return nil, err
	}

	targetTemplate := templatesByRevision[targetRevision]
	if targetTemplate.Labels != nil {
		delete(targetTemplate.Labels, podTemplateHashLabel)
	}
	dep.Spec.Template = targetTemplate
	if _, err := client.AppsV1().Deployments(namespace).Update(ctx, dep, metav1.UpdateOptions{}); err != nil {
		return nil, fmt.Errorf("update deployment %s for rollback: %w", name, err)
	}

	if timeout > 0 {
		if out != nil {
			_, _ = fmt.Fprintf(out, "Waiting for deployment rollout to revision %d...\n", targetRevision)
		}
		if err := waitDeploymentRollout(ctx, client, namespace, name, timeout, out); err != nil {
			return nil, err
		}
	}

	return &UndoRolloutResult{
		Kind:         KindDeployment,
		Name:         dep.Name,
		Namespace:    dep.Namespace,
		FromRevision: currentRevision,
		ToRevision:   targetRevision,
	}, nil
}

func undoStatefulSetRollout(ctx context.Context, client kubernetes.Interface, namespace, name string, toRevision int64, timeout time.Duration, out io.Writer) (*UndoRolloutResult, error) {
	sts, err := client.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, &NotFoundError{Namespace: namespace, Target: name, Kind: KindStatefulSet}
		}
		return nil, fmt.Errorf("get statefulset %s: %w", name, err)
	}

	list, err := client.AppsV1().ControllerRevisions(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list controller revisions for statefulset %s: %w", name, err)
	}

	templatesByRevision := make(map[int64]corev1.PodTemplateSpec)
	revisionOrder := make([]int64, 0)
	currentRevision := int64(0)

	for i := range list.Items {
		rev := list.Items[i]
		if !controllerRevisionOwnedByStatefulSet(rev, sts) {
			continue
		}
		template, err := podTemplateFromControllerRevision(rev)
		if err != nil {
			return nil, err
		}
		if _, exists := templatesByRevision[rev.Revision]; !exists {
			revisionOrder = append(revisionOrder, rev.Revision)
		}
		templatesByRevision[rev.Revision] = *template
		if rev.Name == sts.Status.CurrentRevision {
			currentRevision = rev.Revision
		}
	}

	if len(templatesByRevision) == 0 {
		return nil, fmt.Errorf("no rollout revisions found for statefulset/%s", name)
	}

	sort.SliceStable(revisionOrder, func(i, j int) bool { return revisionOrder[i] < revisionOrder[j] })
	targetRevision, err := pickTargetRevision(revisionOrder, currentRevision, toRevision)
	if err != nil {
		return nil, err
	}

	sts.Spec.Template = templatesByRevision[targetRevision]
	if _, err := client.AppsV1().StatefulSets(namespace).Update(ctx, sts, metav1.UpdateOptions{}); err != nil {
		return nil, fmt.Errorf("update statefulset %s for rollback: %w", name, err)
	}

	if timeout > 0 {
		if out != nil {
			_, _ = fmt.Fprintf(out, "Waiting for statefulset rollout to revision %d...\n", targetRevision)
		}
		if err := waitStatefulSetRollout(ctx, client, namespace, name, timeout, out); err != nil {
			return nil, err
		}
	}

	return &UndoRolloutResult{
		Kind:         KindStatefulSet,
		Name:         sts.Name,
		Namespace:    sts.Namespace,
		FromRevision: currentRevision,
		ToRevision:   targetRevision,
	}, nil
}

func applyContainerImageUpdates(podSpec *corev1.PodSpec, updates map[string]string) (map[string]string, error) {
	found := make(map[string]string, len(updates))

	for i := range podSpec.Containers {
		container := &podSpec.Containers[i]
		if image, ok := updates[container.Name]; ok {
			container.Image = image
			found[container.Name] = image
		}
	}
	for i := range podSpec.InitContainers {
		container := &podSpec.InitContainers[i]
		if image, ok := updates[container.Name]; ok {
			container.Image = image
			found[container.Name] = image
		}
	}

	missing := make([]string, 0)
	for name := range updates {
		if _, ok := found[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, fmt.Errorf("container(s) not found in pod template: %s", strings.Join(missing, ", "))
	}

	return found, nil
}

func buildTagImageAssignments(podSpec corev1.PodSpec, containerHint, tag string) (map[string]string, error) {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return nil, fmt.Errorf("image tag is required")
	}

	containerHint = strings.TrimSpace(containerHint)
	containers := podSpec.Containers
	if len(containers) == 0 {
		return nil, fmt.Errorf("workload pod template has no containers")
	}

	var selected *corev1.Container
	if len(containers) == 1 {
		selected = &containers[0]
	} else if containerHint != "" {
		for i := range containers {
			if containers[i].Name == containerHint {
				selected = &containers[i]
				break
			}
		}
	}

	if selected == nil {
		available := make([]string, 0, len(containers))
		for i := range containers {
			available = append(available, containers[i].Name)
		}
		sort.Strings(available)
		return nil, fmt.Errorf("multiple containers found (%s); use explicit container=image assignment", strings.Join(available, ", "))
	}

	nextImage, err := imageWithTag(selected.Image, tag)
	if err != nil {
		return nil, fmt.Errorf("build image for container %q: %w", selected.Name, err)
	}

	return map[string]string{selected.Name: nextImage}, nil
}

func imageWithTag(image, tag string) (string, error) {
	image = strings.TrimSpace(image)
	tag = strings.TrimSpace(tag)
	if image == "" {
		return "", fmt.Errorf("current image is empty")
	}
	if tag == "" {
		return "", fmt.Errorf("image tag is required")
	}
	if strings.Contains(image, "@") {
		return "", fmt.Errorf("current image %q is digest-pinned; use explicit container=image", image)
	}

	lastSlash := strings.LastIndex(image, "/")
	lastColon := strings.LastIndex(image, ":")
	base := image
	if lastColon > lastSlash {
		base = image[:lastColon]
	}

	return base + ":" + tag, nil
}

func parseDeploymentRevision(annotations map[string]string) int64 {
	if len(annotations) == 0 {
		return 0
	}
	revision := strings.TrimSpace(annotations[deploymentRevisionAnnotation])
	if revision == "" {
		return 0
	}
	parsed, err := strconv.ParseInt(revision, 10, 64)
	if err != nil {
		return 0
	}
	return parsed
}

func pickTargetRevision(revisions []int64, currentRevision, requestedRevision int64) (int64, error) {
	if len(revisions) == 0 {
		return 0, fmt.Errorf("no rollout revisions found")
	}

	if requestedRevision > 0 {
		for _, revision := range revisions {
			if revision == requestedRevision {
				return revision, nil
			}
		}
		return 0, fmt.Errorf("requested revision %d not found", requestedRevision)
	}

	if currentRevision > 0 {
		for i := len(revisions) - 1; i >= 0; i-- {
			if revisions[i] < currentRevision {
				return revisions[i], nil
			}
		}
		return 0, fmt.Errorf("no previous revision found before current revision %d", currentRevision)
	}

	if len(revisions) < 2 {
		return 0, fmt.Errorf("at least two revisions are required for rollback")
	}
	return revisions[len(revisions)-2], nil
}

func normalizeRolloutKind(kind string) (string, error) {
	kinds, err := normalizeKinds(kind)
	if err != nil {
		return "", err
	}
	if len(kinds) != 1 {
		return "", fmt.Errorf("kind must resolve to a single workload kind")
	}
	kind = kinds[0]
	if kind == KindPod {
		return "", fmt.Errorf("rollout operations support only deployment/statefulset")
	}
	return kind, nil
}

func replicaSetOwnedByDeployment(rs appsv1.ReplicaSet, dep *appsv1.Deployment) bool {
	for _, owner := range rs.OwnerReferences {
		if !strings.EqualFold(owner.Kind, "Deployment") {
			continue
		}
		if owner.UID != "" && dep.UID != "" {
			if owner.UID == dep.UID {
				return true
			}
			continue
		}
		if owner.Name == dep.Name {
			return true
		}
	}
	return false
}

func controllerRevisionOwnedByStatefulSet(rev appsv1.ControllerRevision, sts *appsv1.StatefulSet) bool {
	for _, owner := range rev.OwnerReferences {
		if !strings.EqualFold(owner.Kind, "StatefulSet") {
			continue
		}
		if owner.UID != "" && sts.UID != "" {
			if owner.UID == sts.UID {
				return true
			}
			continue
		}
		if owner.Name == sts.Name {
			return true
		}
	}
	return false
}

func podTemplateImages(template corev1.PodTemplateSpec) []string {
	images := make([]string, 0, len(template.Spec.InitContainers)+len(template.Spec.Containers))
	for i := range template.Spec.InitContainers {
		container := template.Spec.InitContainers[i]
		images = append(images, fmt.Sprintf("%s=%s", container.Name, container.Image))
	}
	for i := range template.Spec.Containers {
		container := template.Spec.Containers[i]
		images = append(images, fmt.Sprintf("%s=%s", container.Name, container.Image))
	}
	sort.Strings(images)
	return images
}

func podTemplateFromControllerRevision(rev appsv1.ControllerRevision) (*corev1.PodTemplateSpec, error) {
	if len(rev.Data.Raw) == 0 {
		return nil, fmt.Errorf("controllerrevision/%s has empty data", rev.Name)
	}

	var raw map[string]any
	if err := json.Unmarshal(rev.Data.Raw, &raw); err != nil {
		return nil, fmt.Errorf("decode controllerrevision/%s data: %w", rev.Name, err)
	}

	specRaw, ok := raw["spec"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("controllerrevision/%s data does not contain spec", rev.Name)
	}
	templateRaw, ok := specRaw["template"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("controllerrevision/%s data does not contain spec.template", rev.Name)
	}

	templateRaw = stripPatchDirectives(templateRaw).(map[string]any)
	data, err := json.Marshal(templateRaw)
	if err != nil {
		return nil, fmt.Errorf("encode controllerrevision/%s template: %w", rev.Name, err)
	}

	var template corev1.PodTemplateSpec
	if err := json.Unmarshal(data, &template); err != nil {
		return nil, fmt.Errorf("decode controllerrevision/%s pod template: %w", rev.Name, err)
	}
	return &template, nil
}

func stripPatchDirectives(v any) any {
	switch value := v.(type) {
	case map[string]any:
		clean := make(map[string]any, len(value))
		for key, item := range value {
			if strings.HasPrefix(key, "$") {
				continue
			}
			clean[key] = stripPatchDirectives(item)
		}
		return clean
	case []any:
		clean := make([]any, 0, len(value))
		for _, item := range value {
			clean = append(clean, stripPatchDirectives(item))
		}
		return clean
	default:
		return v
	}
}
