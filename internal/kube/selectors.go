package kube

import (
	"fmt"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

const (
	LabelApp     = "app"
	LabelAppName = "app.kubernetes.io/name"
)

// TargetSelectors returns the default selectors used to resolve a target.
// The order defines precedence and must remain deterministic.
func TargetSelectors(target string) []string {
	target = strings.TrimSpace(target)
	if target == "" {
		return nil
	}
	return []string{
		fmt.Sprintf("%s=%s", LabelApp, target),
		fmt.Sprintf("%s=%s", LabelAppName, target),
	}
}

func SelectorFromLabels(lbls map[string]string) string {
	if len(lbls) == 0 {
		return ""
	}

	keys := make([]string, 0, len(lbls))
	for k := range lbls {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, lbls[k]))
	}
	return strings.Join(parts, ",")
}

func SelectorFromLabelSelector(sel *metav1.LabelSelector) (string, error) {
	if sel == nil {
		return "", nil
	}
	selector, err := metav1.LabelSelectorAsSelector(sel)
	if err != nil {
		return "", fmt.Errorf("convert label selector: %w", err)
	}
	return selector.String(), nil
}

func ParseSelector(selector string) (labels.Selector, error) {
	sel, err := labels.Parse(selector)
	if err != nil {
		return nil, fmt.Errorf("parse selector %q: %w", selector, err)
	}
	return sel, nil
}
