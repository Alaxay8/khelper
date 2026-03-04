package kube

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	KindDeployment  = "deployment"
	KindStatefulSet = "statefulset"
	KindPod         = "pod"
)

var (
	ErrInvalidKind = errors.New("invalid kind")
)

type WorkloadRef struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Selector  string `json:"selector,omitempty"`
	MatchRule string `json:"matchRule"`
}

type PodResolution struct {
	Workload WorkloadRef `json:"workload"`
	Pod      *corev1.Pod `json:"pod"`
	Warning  string      `json:"warning,omitempty"`
}

type NotFoundError struct {
	Namespace string
	Target    string
	Kind      string
}

func (e *NotFoundError) Error() string {
	if e.Kind == "" {
		return fmt.Sprintf("target %q not found in namespace %q", e.Target, e.Namespace)
	}
	return fmt.Sprintf("%s target %q not found in namespace %q", e.Kind, e.Target, e.Namespace)
}

type AmbiguousMatchError struct {
	Namespace string
	Target    string
	Kind      string
	Matches   []WorkloadRef
}

func (e *AmbiguousMatchError) Error() string {
	items := make([]string, 0, len(e.Matches))
	for i, m := range e.Matches {
		items = append(items, fmt.Sprintf("%d:%s/%s", i+1, m.Kind, m.Name))
	}
	return fmt.Sprintf(
		"multiple matches for %q in namespace %q (%s). Re-run with --pick=N. Matches: %s",
		e.Target,
		e.Namespace,
		e.Kind,
		strings.Join(items, ", "),
	)
}

type InvalidPickError struct {
	Pick int
	Max  int
}

func (e *InvalidPickError) Error() string {
	return fmt.Sprintf("invalid --pick value %d (valid range: 1-%d)", e.Pick, e.Max)
}

type Resolver struct {
	client kubernetes.Interface
}

func NewResolver(client kubernetes.Interface) *Resolver {
	return &Resolver{client: client}
}

func (r *Resolver) ResolveWorkload(ctx context.Context, namespace, target, kind string, pick int) (WorkloadRef, error) {
	namespace = strings.TrimSpace(namespace)
	target = strings.TrimSpace(target)
	if namespace == "" {
		namespace = "default"
	}
	if target == "" {
		return WorkloadRef{}, fmt.Errorf("target is required")
	}

	kinds, err := normalizeKinds(kind)
	if err != nil {
		return WorkloadRef{}, err
	}

	var lastNotFound error
	for _, k := range kinds {
		ref, err := r.resolveSingleKind(ctx, namespace, target, k, pick)
		if err == nil {
			return ref, nil
		}

		var nf *NotFoundError
		if errors.As(err, &nf) {
			lastNotFound = err
			continue
		}
		return WorkloadRef{}, err
	}

	if lastNotFound != nil {
		return WorkloadRef{}, &NotFoundError{Namespace: namespace, Target: target, Kind: strings.Join(kinds, "/")}
	}

	return WorkloadRef{}, &NotFoundError{Namespace: namespace, Target: target, Kind: strings.Join(kinds, "/")}
}

func (r *Resolver) ResolvePod(ctx context.Context, namespace, target, kind string, pick int) (*PodResolution, error) {
	workload, err := r.ResolveWorkload(ctx, namespace, target, kind, pick)
	if err != nil {
		return nil, err
	}

	if workload.Kind == KindPod {
		pod, err := r.client.CoreV1().Pods(namespace).Get(ctx, workload.Name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return nil, &NotFoundError{Namespace: namespace, Target: workload.Name, Kind: KindPod}
			}
			return nil, fmt.Errorf("get pod %s: %w", workload.Name, err)
		}
		return &PodResolution{Workload: workload, Pod: pod}, nil
	}

	if workload.Selector == "" {
		return nil, fmt.Errorf("workload %s/%s does not expose a pod selector", workload.Kind, workload.Name)
	}

	pods, err := r.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: workload.Selector})
	if err != nil {
		return nil, fmt.Errorf("list pods for selector %q: %w", workload.Selector, err)
	}
	if len(pods.Items) == 0 {
		return nil, &NotFoundError{Namespace: namespace, Target: workload.Name, Kind: KindPod}
	}

	selected, warning := selectBestPod(pods.Items)
	return &PodResolution{Workload: workload, Pod: &selected, Warning: warning}, nil
}

func normalizeKinds(kind string) ([]string, error) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "" {
		return []string{KindDeployment, KindStatefulSet, KindPod}, nil
	}

	switch kind {
	case KindDeployment, "deploy", "deployment.apps":
		return []string{KindDeployment}, nil
	case KindStatefulSet, "sts", "statefulset.apps":
		return []string{KindStatefulSet}, nil
	case KindPod, "po", "pods":
		return []string{KindPod}, nil
	default:
		return nil, fmt.Errorf("%w %q (allowed: deployment, statefulset, pod)", ErrInvalidKind, kind)
	}
}

func (r *Resolver) resolveSingleKind(ctx context.Context, namespace, target, kind string, pick int) (WorkloadRef, error) {
	switch kind {
	case KindDeployment:
		return r.resolveDeployment(ctx, namespace, target, pick)
	case KindStatefulSet:
		return r.resolveStatefulSet(ctx, namespace, target, pick)
	case KindPod:
		return r.resolvePodByTarget(ctx, namespace, target, pick)
	default:
		return WorkloadRef{}, fmt.Errorf("%w %q", ErrInvalidKind, kind)
	}
}

func (r *Resolver) resolveDeployment(ctx context.Context, namespace, target string, pick int) (WorkloadRef, error) {
	dep, err := r.client.AppsV1().Deployments(namespace).Get(ctx, target, metav1.GetOptions{})
	if err == nil {
		return deploymentRef(dep, "name")
	}
	if err != nil && !apierrors.IsNotFound(err) {
		return WorkloadRef{}, fmt.Errorf("get deployment %s: %w", target, err)
	}

	for _, selector := range TargetSelectors(target) {
		lst, err := r.client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			return WorkloadRef{}, fmt.Errorf("list deployments by selector %q: %w", selector, err)
		}
		if len(lst.Items) == 0 {
			continue
		}

		refs := make([]WorkloadRef, 0, len(lst.Items))
		for i := range lst.Items {
			ref, err := deploymentRef(&lst.Items[i], selector)
			if err != nil {
				return WorkloadRef{}, err
			}
			refs = append(refs, ref)
		}
		return pickSingleMatch(namespace, target, KindDeployment, refs, pick)
	}

	return WorkloadRef{}, &NotFoundError{Namespace: namespace, Target: target, Kind: KindDeployment}
}

func (r *Resolver) resolveStatefulSet(ctx context.Context, namespace, target string, pick int) (WorkloadRef, error) {
	sts, err := r.client.AppsV1().StatefulSets(namespace).Get(ctx, target, metav1.GetOptions{})
	if err == nil {
		return statefulSetRef(sts, "name")
	}
	if err != nil && !apierrors.IsNotFound(err) {
		return WorkloadRef{}, fmt.Errorf("get statefulset %s: %w", target, err)
	}

	for _, selector := range TargetSelectors(target) {
		lst, err := r.client.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			return WorkloadRef{}, fmt.Errorf("list statefulsets by selector %q: %w", selector, err)
		}
		if len(lst.Items) == 0 {
			continue
		}

		refs := make([]WorkloadRef, 0, len(lst.Items))
		for i := range lst.Items {
			ref, err := statefulSetRef(&lst.Items[i], selector)
			if err != nil {
				return WorkloadRef{}, err
			}
			refs = append(refs, ref)
		}
		return pickSingleMatch(namespace, target, KindStatefulSet, refs, pick)
	}

	return WorkloadRef{}, &NotFoundError{Namespace: namespace, Target: target, Kind: KindStatefulSet}
}

func (r *Resolver) resolvePodByTarget(ctx context.Context, namespace, target string, pick int) (WorkloadRef, error) {
	pod, err := r.client.CoreV1().Pods(namespace).Get(ctx, target, metav1.GetOptions{})
	if err == nil {
		return podRef(pod, "name"), nil
	}
	if err != nil && !apierrors.IsNotFound(err) {
		return WorkloadRef{}, fmt.Errorf("get pod %s: %w", target, err)
	}

	for _, selector := range TargetSelectors(target) {
		lst, err := r.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			return WorkloadRef{}, fmt.Errorf("list pods by selector %q: %w", selector, err)
		}
		if len(lst.Items) == 0 {
			continue
		}

		refs := make([]WorkloadRef, 0, len(lst.Items))
		for i := range lst.Items {
			refs = append(refs, podRef(&lst.Items[i], selector))
		}
		return pickSingleMatch(namespace, target, KindPod, refs, pick)
	}

	return WorkloadRef{}, &NotFoundError{Namespace: namespace, Target: target, Kind: KindPod}
}

func pickSingleMatch(namespace, target, kind string, refs []WorkloadRef, pick int) (WorkloadRef, error) {
	sort.SliceStable(refs, func(i, j int) bool {
		return refs[i].Name < refs[j].Name
	})

	if len(refs) == 1 {
		return refs[0], nil
	}

	if len(refs) == 0 {
		return WorkloadRef{}, &NotFoundError{Namespace: namespace, Target: target, Kind: kind}
	}

	if pick == 0 {
		return WorkloadRef{}, &AmbiguousMatchError{
			Namespace: namespace,
			Target:    target,
			Kind:      kind,
			Matches:   refs,
		}
	}

	if pick < 1 || pick > len(refs) {
		return WorkloadRef{}, &InvalidPickError{Pick: pick, Max: len(refs)}
	}

	return refs[pick-1], nil
}

func deploymentRef(dep *appsv1.Deployment, matchRule string) (WorkloadRef, error) {
	selector, err := SelectorFromLabelSelector(dep.Spec.Selector)
	if err != nil {
		return WorkloadRef{}, fmt.Errorf("deployment %s selector: %w", dep.Name, err)
	}
	if selector == "" {
		selector = SelectorFromLabels(dep.Spec.Template.Labels)
	}

	return WorkloadRef{
		Kind:      KindDeployment,
		Name:      dep.Name,
		Namespace: dep.Namespace,
		Selector:  selector,
		MatchRule: matchRule,
	}, nil
}

func statefulSetRef(sts *appsv1.StatefulSet, matchRule string) (WorkloadRef, error) {
	selector, err := SelectorFromLabelSelector(sts.Spec.Selector)
	if err != nil {
		return WorkloadRef{}, fmt.Errorf("statefulset %s selector: %w", sts.Name, err)
	}
	if selector == "" {
		selector = SelectorFromLabels(sts.Spec.Template.Labels)
	}

	return WorkloadRef{
		Kind:      KindStatefulSet,
		Name:      sts.Name,
		Namespace: sts.Namespace,
		Selector:  selector,
		MatchRule: matchRule,
	}, nil
}

func podRef(pod *corev1.Pod, matchRule string) WorkloadRef {
	selector := SelectorFromLabels(pod.Labels)
	return WorkloadRef{
		Kind:      KindPod,
		Name:      pod.Name,
		Namespace: pod.Namespace,
		Selector:  selector,
		MatchRule: matchRule,
	}
}

func selectBestPod(pods []corev1.Pod) (corev1.Pod, string) {
	running := make([]corev1.Pod, 0, len(pods))
	for _, p := range pods {
		if p.Status.Phase == corev1.PodRunning {
			running = append(running, p)
		}
	}

	if len(running) > 0 {
		return newestPod(running), ""
	}

	chosen := newestPod(pods)
	warning := fmt.Sprintf("no Running pods found; using newest pod %s (%s)", chosen.Name, chosen.Status.Phase)
	return chosen, warning
}

func newestPod(pods []corev1.Pod) corev1.Pod {
	best := pods[0]
	bestTS := podTimestamp(best)

	for i := 1; i < len(pods); i++ {
		ts := podTimestamp(pods[i])
		if ts.After(bestTS) || (ts.Equal(bestTS) && pods[i].Name > best.Name) {
			best = pods[i]
			bestTS = ts
		}
	}

	return best
}

func podTimestamp(p corev1.Pod) time.Time {
	if p.Status.StartTime != nil {
		return p.Status.StartTime.Time
	}
	return p.CreationTimestamp.Time
}
