package kube

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

func TestTargetSelectorsPrecedence(t *testing.T) {
	t.Parallel()

	selectors := TargetSelectors("payment")
	if len(selectors) != 2 {
		t.Fatalf("expected 2 selectors, got %d", len(selectors))
	}
	if selectors[0] != "app=payment" {
		t.Fatalf("expected first selector app=payment, got %q", selectors[0])
	}
	if selectors[1] != "app.kubernetes.io/name=payment" {
		t.Fatalf("expected second selector app.kubernetes.io/name=payment, got %q", selectors[1])
	}
}

func TestSelectorFromLabelsSorted(t *testing.T) {
	t.Parallel()

	sel := SelectorFromLabels(map[string]string{
		"tier": "backend",
		"app":  "payment",
	})

	want := "app=payment,tier=backend"
	if sel != want {
		t.Fatalf("expected %q, got %q", want, sel)
	}
}

func TestSelectorFromLabelSelectorFormatting(t *testing.T) {
	t.Parallel()

	sel, err := SelectorFromLabelSelector(&metav1.LabelSelector{
		MatchLabels: map[string]string{"app": "payment"},
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{
				Key:      "tier",
				Operator: metav1.LabelSelectorOpIn,
				Values:   []string{"backend", "api"},
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if sel == "" {
		t.Fatal("selector should not be empty")
	}

	parsed, err := ParseSelector(sel)
	if err != nil {
		t.Fatalf("selector should be parseable: %v", err)
	}
	if !parsed.Matches(labels.Set(map[string]string{"app": "payment", "tier": "backend"})) {
		t.Fatalf("parsed selector %q should match app=payment,tier=backend", sel)
	}
}
