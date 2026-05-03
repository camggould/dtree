package cli_test

import (
	"testing"

	"github.com/cgould/dtree/internal/cli"
)

func TestMCPCommandRegistered(t *testing.T) {
	root := cli.NewRootCommand()
	cmd, _, err := root.Find([]string{"mcp"})
	if err != nil {
		t.Fatalf("find mcp command: %v", err)
	}
	if cmd == nil || cmd.Name() != "mcp" {
		t.Fatalf("mcp command not registered; got: %v", cmd)
	}
	for _, f := range []string{"as", "tree", "read-only", "http"} {
		if cmd.Flag(f) == nil {
			t.Errorf("missing flag --%s", f)
		}
	}
}
