package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/alaxay8/khelper/internal/kube"
	"github.com/alaxay8/khelper/pkg/output"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newSetImageCmd() *cobra.Command {
	var kind string
	var pick int
	var allNamespaces bool

	cmd := &cobra.Command{
		Use:     "set-image <target> <container=image> [container=image...] | <target:tag>",
		Aliases: []string{"si"},
		Short:   "Update container images for deployment/statefulset",
		Long: strings.TrimSpace(`
Update container images for a Deployment or StatefulSet.

Supported forms:
  1) Explicit assignments:
     khelper set-image <target> <container=image> [container=image...]
  2) Shorthand tag update:
     khelper set-image <target:tag>

Target can be plain (<name>) or kind-qualified (<kind/name>), for example:
  frontend
  deployment/frontend
  statefulset/db

Shorthand behavior:
  - Keeps current image registry/repository and changes only the tag.
  - Updates one container:
    * if workload has one container -> update it;
    * if multiple containers -> try container name equal to target name.
  - If still ambiguous, command asks for explicit container=image format.
  - Digest-pinned images (@sha256:...) require explicit container=image.

Kind resolution:
  - If --kind is set, it is used.
  - Without --kind, command tries deployment, then statefulset.
  - If both kinds match in TTY mode, interactive numeric selection is shown.
`),
		Example: strings.TrimSpace(`
  # Explicit container=image update
  khelper set-image frontend server=ghcr.io/alaxay8/frontend:v1.0.1 -n shop

  # Multiple containers
  khelper set-image payment app=ghcr.io/acme/payment:v2 sidecar=ghcr.io/acme/sidecar:v2 -n shop

  # Shorthand tag update (alias: si)
  khelper si frontend:v1.0.1 -n shop

  # Kind-qualified shorthand target
  khelper si deployment/frontend:v1.0.1 -n shop

  # Cross-namespace search
  khelper si frontend:v1.0.1 -A
	`),
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 && strings.EqualFold(strings.TrimSpace(args[0]), "help") {
				return cmd.Help()
			}

			input, err := parseSetImageInput(args)
			if err != nil {
				return NewExitError(ExitCodeUsage, err.Error())
			}

			effectiveKind := strings.TrimSpace(kind)
			if input.Kind != "" {
				if effectiveKind == "" {
					effectiveKind = input.Kind
				} else {
					flagKind := canonicalSetImageKind(effectiveKind)
					if flagKind == "" {
						flagKind = strings.ToLower(strings.TrimSpace(effectiveKind))
					}
					if flagKind != input.Kind {
						return NewExitError(
							ExitCodeUsage,
							fmt.Sprintf("conflicting kinds: target prefix %q does not match --kind=%q", input.Kind, kind),
						)
					}
					effectiveKind = flagKind
				}
			}

			if isPodKind(effectiveKind) {
				return NewExitError(ExitCodeUsage, "set-image supports only deployment or statefulset")
			}

			bundle, err := kube.NewClientBundle(Config())
			if err != nil {
				return WrapExitError(ExitCodeGeneral, err, "initialize kubernetes client")
			}

			namespaceScope := bundle.Namespace
			if allNamespaces {
				namespaceScope = kube.NamespaceAll
			}

			resolver := kube.NewResolver(bundle.Clientset)
			workload, err := resolveSetImageWorkload(cmd.Context(), resolver, namespaceScope, input.Target, effectiveKind, pick, cmd.InOrStdin(), cmd.OutOrStdout())
			if err != nil {
				return err
			}

			assignments := input.Assignments
			if input.ShorthandTag != "" {
				assignments, err = kube.ResolveTagImageAssignments(
					cmd.Context(),
					bundle.Clientset,
					workload.Namespace,
					workload.Kind,
					workload.Name,
					targetNameHint(input.Target),
					input.ShorthandTag,
				)
				if err != nil {
					return WrapExitError(ExitCodeGeneral, err, "build image update for %s/%s", workload.Kind, workload.Name)
				}
			}

			result, err := kube.SetWorkloadImages(cmd.Context(), bundle.Clientset, workload.Namespace, workload.Kind, workload.Name, assignments)
			if err != nil {
				return WrapExitError(ExitCodeGeneral, err, "set image for %s/%s", workload.Kind, workload.Name)
			}

			if Config().Output == "json" {
				if err := output.PrintJSON(cmd.OutOrStdout(), result); err != nil {
					return WrapExitError(ExitCodeGeneral, err, "write JSON output")
				}
				return nil
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Updated %s/%s in namespace %s: %s\n", result.Kind, result.Name, result.Namespace, formatUpdatedImages(result.Updated))
			return nil
		},
	}

	cmd.Flags().StringVar(&kind, "kind", "", "Target kind override: deployment|statefulset")
	cmd.Flags().IntVar(&pick, "pick", 0, "Pick match number when multiple targets are found (1-based)")
	cmd.Flags().BoolVarP(&allNamespaces, "all-namespaces", "A", false, "Search target across all namespaces")

	return cmd
}

type setImageInput struct {
	Target       string
	Assignments  map[string]string
	ShorthandTag string
	Kind         string
}

func parseSetImageInput(args []string) (setImageInput, error) {
	if len(args) == 0 {
		return setImageInput{}, fmt.Errorf("expected either <target> <container=image> [container=image...] or <target:tag>")
	}

	if len(args) > 1 {
		target, targetKind, err := parseKindQualifiedTarget(strings.TrimSpace(args[0]))
		if err != nil {
			return setImageInput{}, err
		}

		assignments, err := parseImageAssignments(args[1:])
		if err != nil {
			return setImageInput{}, err
		}
		return setImageInput{
			Target:      target,
			Assignments: assignments,
			Kind:        targetKind,
		}, nil
	}

	target, tag, ok, err := parseTargetTagShorthand(args[0])
	if err != nil {
		return setImageInput{}, err
	}
	if !ok {
		return setImageInput{}, fmt.Errorf("expected either <target> <container=image> [container=image...] or <target:tag>")
	}

	target, targetKind, err := parseKindQualifiedTarget(target)
	if err != nil {
		return setImageInput{}, err
	}

	return setImageInput{
		Target:       target,
		ShorthandTag: tag,
		Kind:         targetKind,
	}, nil
}

func parseTargetTagShorthand(raw string) (target, tag string, ok bool, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", false, fmt.Errorf("target is required")
	}
	if strings.Contains(raw, "=") {
		return "", "", false, nil
	}

	colon := strings.LastIndex(raw, ":")
	if colon < 0 {
		return "", "", false, nil
	}

	target = strings.TrimSpace(raw[:colon])
	tag = strings.TrimSpace(raw[colon+1:])
	if target == "" || tag == "" {
		return "", "", false, fmt.Errorf("invalid shorthand %q (expected target:tag)", raw)
	}

	return target, tag, true, nil
}

func parseKindQualifiedTarget(raw string) (target, kind string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", fmt.Errorf("target is required")
	}

	parts := strings.SplitN(raw, "/", 2)
	if len(parts) != 2 {
		return raw, "", nil
	}

	kind = canonicalSetImageKind(parts[0])
	if kind == "" {
		return raw, "", nil
	}

	target = strings.TrimSpace(parts[1])
	if target == "" {
		return "", "", fmt.Errorf("invalid target %q (expected kind/name)", raw)
	}
	return target, kind, nil
}

func canonicalSetImageKind(kind string) string {
	kind = strings.ToLower(strings.TrimSpace(kind))
	switch kind {
	case kube.KindDeployment, "deploy", "deployment.apps":
		return kube.KindDeployment
	case kube.KindStatefulSet, "sts", "statefulset.apps":
		return kube.KindStatefulSet
	case kube.KindPod, "po", "pods":
		return kube.KindPod
	default:
		return ""
	}
}

func resolveSetImageWorkload(
	ctx context.Context,
	resolver *kube.Resolver,
	namespace, target, kind string,
	pick int,
	in io.Reader,
	out io.Writer,
) (kube.WorkloadRef, error) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind != "" {
		return resolver.ResolveWorkload(ctx, namespace, target, kind, pick)
	}

	deployment, depErr := resolver.ResolveWorkload(ctx, namespace, target, kube.KindDeployment, pick)
	statefulSet, stsErr := resolver.ResolveWorkload(ctx, namespace, target, kube.KindStatefulSet, pick)

	if depErr == nil && stsErr == nil {
		if canPromptForKindSelection(in, out) {
			selected, err := promptKindSelection(in, out, target, []kube.WorkloadRef{deployment, statefulSet})
			if err != nil {
				return kube.WorkloadRef{}, NewExitError(ExitCodeUsage, err.Error())
			}
			return selected, nil
		}
		return kube.WorkloadRef{}, NewExitError(
			ExitCodeUsage,
			fmt.Sprintf("target %q matches both deployment and statefulset; re-run with --kind=deployment or --kind=statefulset", target),
		)
	}
	if depErr == nil {
		return deployment, nil
	}
	if stsErr == nil {
		return statefulSet, nil
	}

	var notFound *kube.NotFoundError
	if depErr != nil && !errors.As(depErr, &notFound) {
		return kube.WorkloadRef{}, depErr
	}
	if stsErr != nil && !errors.As(stsErr, &notFound) {
		return kube.WorkloadRef{}, stsErr
	}

	return kube.WorkloadRef{}, &kube.NotFoundError{Namespace: namespace, Target: target, Kind: "deployment/statefulset"}
}

func canPromptForKindSelection(in io.Reader, out io.Writer) bool {
	inFD, ok := fileDescriptorFromReader(in)
	if !ok || !term.IsTerminal(inFD) {
		return false
	}
	outFD, ok := fileDescriptorFromWriter(out)
	if !ok || !term.IsTerminal(outFD) {
		return false
	}
	return true
}

func fileDescriptorFromReader(in io.Reader) (int, bool) {
	file, ok := in.(*os.File)
	if !ok {
		return 0, false
	}
	return int(file.Fd()), true
}

func fileDescriptorFromWriter(out io.Writer) (int, bool) {
	file, ok := out.(*os.File)
	if !ok {
		return 0, false
	}
	return int(file.Fd()), true
}

func promptKindSelection(in io.Reader, out io.Writer, target string, options []kube.WorkloadRef) (kube.WorkloadRef, error) {
	if len(options) == 0 {
		return kube.WorkloadRef{}, fmt.Errorf("no workload kinds to select from")
	}

	_, _ = fmt.Fprintf(out, "Target %q matches multiple workload kinds:\n", target)
	for i := range options {
		_, _ = fmt.Fprintf(out, "%d) %s/%s (%s)\n", i+1, options[i].Kind, options[i].Name, options[i].Namespace)
	}

	scanner := bufio.NewScanner(in)
	for {
		_, _ = fmt.Fprintf(out, "Choose kind [1-%d]: ", len(options))
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return kube.WorkloadRef{}, fmt.Errorf("read kind selection: %w", err)
			}
			return kube.WorkloadRef{}, fmt.Errorf("no selection provided")
		}

		value := strings.TrimSpace(scanner.Text())
		choice, err := strconv.Atoi(value)
		if err == nil && choice >= 1 && choice <= len(options) {
			return options[choice-1], nil
		}
		_, _ = fmt.Fprintf(out, "Invalid selection %q. Enter a number from 1 to %d.\n", value, len(options))
	}
}

func targetNameHint(target string) string {
	target = strings.TrimSpace(target)
	if target == "" {
		return ""
	}
	if slash := strings.LastIndex(target, "/"); slash >= 0 {
		target = target[slash+1:]
	}
	return strings.TrimSpace(target)
}

func parseImageAssignments(values []string) (map[string]string, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("at least one container=image assignment is required")
	}

	result := make(map[string]string, len(values))
	for _, raw := range values {
		raw = strings.TrimSpace(raw)
		parts := strings.SplitN(raw, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid image assignment %q (expected container=image)", raw)
		}

		container := strings.TrimSpace(parts[0])
		image := strings.TrimSpace(parts[1])
		if container == "" || image == "" {
			return nil, fmt.Errorf("invalid image assignment %q (expected container=image)", raw)
		}
		if _, exists := result[container]; exists {
			return nil, fmt.Errorf("duplicate image assignment for container %q", container)
		}
		result[container] = image
	}

	return result, nil
}
