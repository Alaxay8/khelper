package cmd

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/alaxay8/khelper/internal/kube"
	"github.com/alaxay8/khelper/pkg/output"
	"github.com/spf13/cobra"
)

type topOutput struct {
	Namespace string                   `json:"namespace,omitempty"`
	Pods      []kube.PodMetricSummary  `json:"pods,omitempty"`
	Nodes     []kube.NodeMetricSummary `json:"nodes,omitempty"`
}

func newTopCmd() *cobra.Command {
	var pods bool
	var nodes bool

	cmd := &cobra.Command{
		Use:   "top",
		Short: "Display pod/node resource metrics",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !pods && !nodes {
				pods = true
			}

			bundle, err := newClientBundle()
			if err != nil {
				return err
			}

			metricsClient, err := kube.NewMetricsClient(bundle.RESTConfig)
			if err != nil {
				return WrapExitError(ExitCodeGeneral, err, "initialize metrics client")
			}

			var podMetrics []kube.PodMetricSummary
			var nodeMetrics []kube.NodeMetricSummary

			if pods {
				podMetrics, err = kube.ListPodMetrics(cmd.Context(), metricsClient, bundle.Namespace)
				if err != nil {
					return wrapTopError(err)
				}
			}

			if nodes {
				nodeMetrics, err = kube.ListNodeMetrics(cmd.Context(), metricsClient)
				if err != nil {
					return wrapTopError(err)
				}
			}

			payload := topOutput{
				Namespace: bundle.Namespace,
				Pods:      podMetrics,
				Nodes:     nodeMetrics,
			}
			if handled, err := writeJSONIfRequested(cmd, payload); err != nil {
				return err
			} else if handled {
				return nil
			}

			if pods {
				t := output.NewTable("NAME", "CPU(m)", "MEMORY(Mi)")
				for _, m := range podMetrics {
					t.AddRow(m.Name, strconv.FormatInt(m.CPUMilli, 10), strconv.FormatInt(m.MemoryMi, 10))
				}
				if err := t.Render(cmd.OutOrStdout()); err != nil {
					return WrapExitError(ExitCodeGeneral, err, "render pod metrics table")
				}
			}

			if nodes {
				if pods {
					fmt.Fprintln(cmd.OutOrStdout())
				}
				t := output.NewTable("NAME", "CPU(m)", "MEMORY(Mi)")
				for _, m := range nodeMetrics {
					t.AddRow(m.Name, strconv.FormatInt(m.CPUMilli, 10), strconv.FormatInt(m.MemoryMi, 10))
				}
				if err := t.Render(cmd.OutOrStdout()); err != nil {
					return WrapExitError(ExitCodeGeneral, err, "render node metrics table")
				}
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&pods, "pods", false, "Show pod metrics")
	cmd.Flags().BoolVar(&nodes, "nodes", false, "Show node metrics")

	return cmd
}

func wrapTopError(err error) error {
	if errors.Is(err, kube.ErrMetricsUnavailable) {
		return WrapExitError(
			ExitCodeUnavailable,
			err,
			"metrics API unavailable. Install metrics-server and ensure metrics.k8s.io is registered",
		)
	}
	return WrapExitError(ExitCodeGeneral, err, "query metrics")
}
