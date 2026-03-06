package cmd

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/alaxay8/khelper/internal/doctor"
	"github.com/alaxay8/khelper/internal/kube"
	"github.com/alaxay8/khelper/pkg/output"
	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	var kind string
	var pick int
	var sinceStr string
	var logsTail int64
	var container string
	var allNamespaces bool

	cmd := &cobra.Command{
		Use:     "doctor <target>",
		Aliases: []string{"debug"},
		Short:   "Diagnose failing deployment/statefulset/pod and print root-cause hints",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := args[0]

			since := time.Hour
			if strings.TrimSpace(sinceStr) != "" {
				parsed, err := time.ParseDuration(sinceStr)
				if err != nil {
					return NewExitError(ExitCodeUsage, fmt.Sprintf("invalid --since value %q: %v", sinceStr, err))
				}
				if parsed < 0 {
					return NewExitError(ExitCodeUsage, "--since must be a non-negative duration")
				}
				since = parsed
			}

			if logsTail < 0 {
				return NewExitError(ExitCodeUsage, "--logs-tail must be a non-negative integer")
			}

			bundle, err := kube.NewClientBundle(Config())
			if err != nil {
				return WrapExitError(ExitCodeGeneral, err, "initialize kubernetes client")
			}

			namespaceScope := bundle.Namespace
			if allNamespaces {
				namespaceScope = kube.NamespaceAll
			}

			snapshot, err := doctor.Collect(cmd.Context(), bundle, target, doctor.CollectOptions{
				Namespace: namespaceScope,
				Kind:      kind,
				Pick:      pick,
				Since:     since,
				LogsTail:  logsTail,
				Container: container,
			})
			if err != nil {
				var invalidContainer *doctor.InvalidContainerError
				if errors.As(err, &invalidContainer) {
					return NewExitError(ExitCodeUsage, invalidContainer.Error())
				}

				var notFound *kube.NotFoundError
				var ambiguous *kube.AmbiguousMatchError
				var invalidPick *kube.InvalidPickError
				if errors.As(err, &notFound) || errors.As(err, &ambiguous) || errors.As(err, &invalidPick) {
					return err
				}

				return WrapExitError(ExitCodeGeneral, err, "collect diagnostics")
			}

			findings := doctor.Evaluate(snapshot, doctor.DefaultRules())
			if Config().Output == "json" {
				if err := output.PrintJSON(cmd.OutOrStdout(), findings); err != nil {
					return WrapExitError(ExitCodeGeneral, err, "write JSON output")
				}
			} else {
				table := output.NewTable("SEVERITY", "CHECK", "OBJECT", "MESSAGE", "ACTION")
				if len(findings) == 0 {
					table.AddRow("INFO", "summary", snapshot.Workload.Kind+"/"+snapshot.Workload.Name, "No findings detected", "-")
				} else {
					for _, finding := range findings {
						table.AddRow(
							strings.ToUpper(string(finding.Severity)),
							finding.Check,
							doctorCell(finding.Object),
							doctorCell(finding.Message),
							doctorCell(finding.Action),
						)
					}
				}
				if err := table.Render(cmd.OutOrStdout()); err != nil {
					return WrapExitError(ExitCodeGeneral, err, "render table")
				}
			}

			if doctor.HasIssues(findings) {
				return &ExitError{Code: ExitCodeDoctorFindings}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&kind, "kind", "", "Target kind override: deployment|statefulset|pod")
	cmd.Flags().IntVar(&pick, "pick", 0, "Pick match number when multiple targets are found (1-based)")
	cmd.Flags().BoolVarP(&allNamespaces, "all-namespaces", "A", false, "Search target across all namespaces")
	cmd.Flags().StringVar(&sinceStr, "since", "1h", "Analyze warning events newer than this duration (e.g. 30m, 2h)")
	cmd.Flags().Int64Var(&logsTail, "logs-tail", 120, "Tail lines to include from selected pod container for evidence (0 disables)")
	cmd.Flags().StringVar(&container, "container", "", "Container name for --logs-tail evidence")

	return cmd
}

func doctorCell(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
