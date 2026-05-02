package cli_test

import (
	"strings"
	"testing"

	"github.com/cgould/dtree/internal/cli"
)

// TestAsHandleEmpty verifies that an empty handle returns an error.
func TestAsHandleEmpty(t *testing.T) {
	_, _, _, err := cli.ComputeAsExec("", []string{"new", "foo"})
	if err == nil {
		t.Fatal("expected error for empty handle, got nil")
	}
}

// TestAsForwardsArgs verifies the finalArgs include the executable and subcommand args.
func TestAsForwardsArgs(t *testing.T) {
	handle := "cam-claude"
	args := []string{"new", "foo"}

	executable, finalArgs, env, err := cli.ComputeAsExec(handle, args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if executable == "" {
		t.Error("executable should not be empty")
	}
	// finalArgs[0] should be the executable path (argv0).
	if len(finalArgs) < 1 {
		t.Fatal("finalArgs should have at least argv0")
	}
	if finalArgs[0] != executable {
		t.Errorf("finalArgs[0]: got %q, want %q", finalArgs[0], executable)
	}
	// Remaining args should be the subcommand args.
	if len(finalArgs) != 3 {
		t.Fatalf("expected 3 finalArgs (executable + 2 subcmd args), got %d: %v", len(finalArgs), finalArgs)
	}
	if finalArgs[1] != "new" {
		t.Errorf("finalArgs[1]: got %q, want %q", finalArgs[1], "new")
	}
	if finalArgs[2] != "foo" {
		t.Errorf("finalArgs[2]: got %q, want %q", finalArgs[2], "foo")
	}

	// DTREE_AS=handle must be in env.
	want := "DTREE_AS=" + handle
	found := false
	for _, kv := range env {
		if kv == want {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("env does not contain %q; env: %v", want, env)
	}
}

// TestAsPreservesOtherEnv verifies that existing env vars (e.g. PATH) are preserved.
func TestAsPreservesOtherEnv(t *testing.T) {
	// PATH will be in the env on any real system.
	_, _, env, err := cli.ComputeAsExec("myhandle", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	hasPath := false
	for _, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			hasPath = true
			break
		}
	}
	if !hasPath {
		t.Error("env does not contain PATH; existing env should be preserved")
	}
}

// TestAsNoSubcmdArgs verifies that passing just a handle (no subcommand) works.
func TestAsNoSubcmdArgs(t *testing.T) {
	executable, finalArgs, _, err := cli.ComputeAsExec("cam", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(finalArgs) != 1 || finalArgs[0] != executable {
		t.Errorf("expected finalArgs=[%q], got %v", executable, finalArgs)
	}
}
