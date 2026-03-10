package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestIsCompletionCommandRecognizesCompletionBranch(t *testing.T) {
	t.Parallel()

	root := &cobra.Command{Use: "khelper"}
	completionCmd := newCompletionCmd()
	completionInstallCmd := newCompletionInstallCmd()
	root.AddCommand(completionCmd, completionInstallCmd)

	bashCmd, _, err := completionCmd.Find([]string{"bash"})
	if err != nil {
		t.Fatalf("failed to resolve completion bash command: %v", err)
	}

	if !isCompletionCommand(completionCmd) {
		t.Fatal("expected completion command to be detected")
	}
	if !isCompletionCommand(bashCmd) {
		t.Fatal("expected completion subcommand to be detected")
	}
	if !isCompletionCommand(completionInstallCmd) {
		t.Fatal("expected completion-install command to be detected")
	}
}

func TestIsCompletionCommandRejectsNonCompletionBranch(t *testing.T) {
	t.Parallel()

	root := &cobra.Command{Use: "khelper"}
	nonCompletion := &cobra.Command{Use: "pods"}
	root.AddCommand(nonCompletion)

	if isCompletionCommand(nonCompletion) {
		t.Fatal("did not expect non-completion command to be detected")
	}
}

func TestIsCompletionCommandRecognizesInternalCobraCompletionCommands(t *testing.T) {
	t.Parallel()

	complete := &cobra.Command{Use: "__complete"}
	if !isCompletionCommand(complete) {
		t.Fatal("expected __complete to be detected")
	}

	completeNoDesc := &cobra.Command{Use: "__completeNoDesc"}
	if !isCompletionCommand(completeNoDesc) {
		t.Fatal("expected __completeNoDesc to be detected")
	}
}
