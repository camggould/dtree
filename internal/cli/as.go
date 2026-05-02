package cli

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/spf13/cobra"
)

// newAsCommand returns the `dtree as <handle> <subcmd> [args...]` command.
// It re-invokes the current binary with DTREE_AS=<handle> set.
func newAsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:                "as <handle> <subcmd> [args...]",
		Short:              "Run a subcommand with an identity override",
		DisableFlagParsing: true,
		Args:               cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			handle := args[0]
			rest := args[1:]

			executable, finalArgs, env, err := ComputeAsExec(handle, rest)
			if err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "error:", err)
				return err
			}

			// Try syscall.Exec first (replaces current process on Linux/macOS).
			execErr := syscall.Exec(executable, finalArgs, env)
			if execErr != nil {
				// Fall back to spawning a child process.
				child := exec.Command(executable, finalArgs[1:]...) //nolint:gosec
				child.Env = env
				child.Stdin = os.Stdin
				child.Stdout = os.Stdout
				child.Stderr = os.Stderr
				if runErr := child.Run(); runErr != nil {
					if exitErr, ok := runErr.(*exec.ExitError); ok {
						os.Exit(exitErr.ExitCode())
					}
					return runErr
				}
			}
			return nil
		},
	}
	return cmd
}

// ComputeAsExec returns the arguments that would be passed to syscall.Exec
// or exec.Cmd to re-invoke the current binary with DTREE_AS=handle.
// Exported for testing without triggering an actual exec.
func ComputeAsExec(handle string, args []string) (executable string, finalArgs []string, env []string, err error) {
	if handle == "" {
		return "", nil, nil, fmt.Errorf("handle must not be empty")
	}

	executable, err = os.Executable()
	if err != nil {
		return "", nil, nil, fmt.Errorf("cannot determine executable path: %w", err)
	}

	// Build the final argument list: [argv0, subcmd-args...]
	finalArgs = make([]string, 0, len(args)+1)
	finalArgs = append(finalArgs, executable)
	finalArgs = append(finalArgs, args...)

	// Build env: current env with DTREE_AS overridden.
	current := os.Environ()
	dtreeAsKey := "DTREE_AS=" + handle
	env = make([]string, 0, len(current)+1)
	replaced := false
	for _, kv := range current {
		if len(kv) >= 9 && kv[:9] == "DTREE_AS=" {
			env = append(env, dtreeAsKey)
			replaced = true
		} else {
			env = append(env, kv)
		}
	}
	if !replaced {
		env = append(env, dtreeAsKey)
	}

	return executable, finalArgs, env, nil
}
