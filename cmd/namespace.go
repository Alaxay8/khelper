package cmd

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/alaxay8/khelper/internal/kube"
	"github.com/alaxay8/khelper/pkg/output"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type namespaceRow struct {
	Name    string `json:"name"`
	Current bool   `json:"current"`
}

func newNamespaceCmd() *cobra.Command {
	nsCmd := &cobra.Command{
		Use:   "ns",
		Short: "Manage namespaces",
	}

	nsCmd.AddCommand(newNamespaceListCmd(), newNamespaceUseCmd())
	return nsCmd
}

func newNamespaceListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List namespaces in the cluster",
		RunE: func(cmd *cobra.Command, args []string) error {
			bundle, err := newClientBundle()
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			nsList, err := bundle.Clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
			if err != nil {
				return WrapExitError(ExitCodeGeneral, err, "list namespaces")
			}

			rows := make([]namespaceRow, 0, len(nsList.Items))
			for _, ns := range nsList.Items {
				rows = append(rows, namespaceRow{Name: ns.Name, Current: ns.Name == bundle.Namespace})
			}

			sort.SliceStable(rows, func(i, j int) bool {
				return rows[i].Name < rows[j].Name
			})

			if handled, err := writeJSONIfRequested(cmd, rows); err != nil {
				return err
			} else if handled {
				return nil
			}

			t := output.NewTable("CURRENT", "NAME")
			for _, row := range rows {
				marker := ""
				if row.Current {
					marker = "*"
				}
				t.AddRow(marker, row.Name)
			}
			if err := t.Render(cmd.OutOrStdout()); err != nil {
				return WrapExitError(ExitCodeGeneral, err, "render table")
			}
			return nil
		},
	}
}

func newNamespaceUseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "use <namespace>",
		Short: "Set default namespace for the current context in kubeconfig",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			namespace := args[0]
			settings := Config()

			rawCfg, err := kube.LoadRawKubeconfig(settings.Kubeconfig)
			if err != nil {
				return WrapExitError(ExitCodeGeneral, err, "load kubeconfig")
			}

			ctxName := kube.CurrentContextName(rawCfg, settings.Context)
			if ctxName == "" {
				return NewExitError(ExitCodeGeneral, "current context is empty in kubeconfig")
			}

			ctxCfg, ok := rawCfg.Contexts[ctxName]
			if !ok {
				return NewExitError(ExitCodeNotFound, fmt.Sprintf("context %q not found in kubeconfig", ctxName))
			}

			ctxCfg.Namespace = namespace
			rawCfg.Contexts[ctxName] = ctxCfg
			if err := kube.WriteRawKubeconfigAtomic(settings.Kubeconfig, rawCfg); err != nil {
				return WrapExitError(ExitCodeGeneral, err, "update context namespace")
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Set default namespace for context %q to %q\n", ctxName, namespace)
			return nil
		},
	}
}
