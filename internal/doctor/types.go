package doctor

import (
	"sort"
	"strings"
	"time"

	"github.com/alaxay8/khelper/internal/kube"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

type Severity string

const (
	SeverityInfo    Severity = "info"
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

type Finding struct {
	Severity Severity       `json:"severity"`
	Check    string         `json:"check"`
	Object   string         `json:"object"`
	Message  string         `json:"message"`
	Action   string         `json:"action"`
	Evidence map[string]any `json:"evidence,omitempty"`
}

type Rule struct {
	Name string
	Run  func(snapshot *Snapshot) []Finding
}

type CollectOptions struct {
	Kind      string
	Pick      int
	Since     time.Duration
	LogsTail  int64
	Container string
	Now       time.Time
}

type Snapshot struct {
	Namespace          string
	Target             string
	Workload           kube.WorkloadRef
	Deployment         *appsv1.Deployment
	StatefulSet        *appsv1.StatefulSet
	Pods               []corev1.Pod
	SelectedPod        *corev1.Pod
	SelectedPodWarning string
	Events             []corev1.Event
	Since              time.Duration
	LogSnippet         *LogSnippet
}

type LogSnippet struct {
	Pod       string `json:"pod"`
	Container string `json:"container"`
	Tail      int64  `json:"tail"`
	Text      string `json:"text,omitempty"`
	Error     string `json:"error,omitempty"`
}

type InvalidContainerError struct {
	Pod       string
	Container string
}

func (e *InvalidContainerError) Error() string {
	return "container \"" + e.Container + "\" not found in pod " + e.Pod
}

func Evaluate(snapshot *Snapshot, rules []Rule) []Finding {
	findings := make([]Finding, 0)
	for _, rule := range rules {
		if rule.Run == nil {
			continue
		}
		findings = append(findings, rule.Run(snapshot)...)
	}

	sort.SliceStable(findings, func(i, j int) bool {
		left := findings[i]
		right := findings[j]

		if severityRank(left.Severity) != severityRank(right.Severity) {
			return severityRank(left.Severity) < severityRank(right.Severity)
		}
		if left.Check != right.Check {
			return left.Check < right.Check
		}
		if left.Object != right.Object {
			return left.Object < right.Object
		}
		return left.Message < right.Message
	})

	return findings
}

func HasIssues(findings []Finding) bool {
	for _, finding := range findings {
		if finding.Severity == SeverityWarning || finding.Severity == SeverityError {
			return true
		}
	}
	return false
}

func DefaultRules() []Rule {
	return []Rule{
		{Name: "container-state", Run: checkContainerWaitingState},
		{Name: "oom-and-restarts", Run: checkOOMAndRestarts},
		{Name: "pending-unschedulable", Run: checkPendingUnschedulable},
		{Name: "probe-failures", Run: checkProbeFailures},
		{Name: "workload-replicas", Run: checkWorkloadReplicas},
		{Name: "warning-events", Run: checkWarningEvents},
	}
}

func severityRank(severity Severity) int {
	switch severity {
	case SeverityError:
		return 0
	case SeverityWarning:
		return 1
	case SeverityInfo:
		return 2
	default:
		return 3
	}
}

func normalizeSpace(input string) string {
	return strings.Join(strings.Fields(input), " ")
}
