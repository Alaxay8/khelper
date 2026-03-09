package cmd

import (
	"fmt"
	"sort"

	"github.com/alaxay8/khelper/internal/kube"
	"github.com/alaxay8/khelper/pkg/output"
	"github.com/spf13/cobra"
)

type contextRow struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	Current   bool   `json:"current"`
}

func newContextCmd() *cobra.Command {
	ctxCmd := &cobra.Command{
		Use:   "ctx",
		Short: "Manage kubeconfig contexts",
	}

	ctxCmd.AddCommand(newContextListCmd(), newContextUseCmd())
	return ctxCmd
}

func newContextListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List kubeconfig contexts",
		RunE: func(cmd *cobra.Command, args []string) error {
			settings := Config()
			rawCfg, err := kube.LoadRawKubeconfig(settings.Kubeconfig)
			if err != nil {
				return WrapExitError(ExitCodeGeneral, err, "load kubeconfig")
			}

			rows := make([]contextRow, 0, len(rawCfg.Contexts))
			names := make([]string, 0, len(rawCfg.Contexts))
			for name := range rawCfg.Contexts {
				names = append(names, name)
			}
			sort.Strings(names)

			for _, name := range names {
				ctx := rawCfg.Contexts[name]
				rows = append(rows, contextRow{
					Name:      name,
					Namespace: ctx.Namespace,
					Current:   rawCfg.CurrentContext == name,
				})
			}

			if handled, err := writeJSONIfRequested(cmd, rows); err != nil {
				return err
			} else if handled {
				return nil
			}

			t := output.NewTable("CURRENT", "NAME", "NAMESPACE")
			for _, row := range rows {
				marker := ""
				if row.Current {
					marker = "*"
				}
				ns := row.Namespace
				if ns == "" {
					ns = "default"
				}
				t.AddRow(marker, row.Name, ns)
			}
			if err := t.Render(cmd.OutOrStdout()); err != nil {
				return WrapExitError(ExitCodeGeneral, err, "render table")
			}

			return nil
		},
	}
}

func newContextUseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "use <name>",
		Short: "Set current kubeconfig context",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			settings := Config()

			rawCfg, err := kube.LoadRawKubeconfig(settings.Kubeconfig)
			if err != nil {
				return WrapExitError(ExitCodeGeneral, err, "load kubeconfig")
			}

			if _, ok := rawCfg.Contexts[name]; !ok {
				return NewExitError(ExitCodeNotFound, fmt.Sprintf("context %q not found in kubeconfig", name))
			}

			rawCfg.CurrentContext = name
			if err := kube.WriteRawKubeconfigAtomic(settings.Kubeconfig, rawCfg); err != nil {
				return WrapExitError(ExitCodeGeneral, err, "update kubeconfig current-context")
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Switched current context to %q\n", name)
			return nil
		},
	}
}
