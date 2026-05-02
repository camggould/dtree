// Package main is the dtree CLI entrypoint.
package main

import (
	"fmt"
	"os"

	"github.com/cgould/dtree/internal/cli"
)

func main() {
	if err := cli.NewRootCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
