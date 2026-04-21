package main

import (
	"fmt"
	"os"

	"github.com/cornish/wsi-label-tools/internal/cli"
)

// Build-time metadata, set via goreleaser ldflags. "dev" for local builds.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--version", "-V", "version":
			fmt.Printf("wsi-label %s (commit %s, built %s)\n", version, commit, date)
			return
		}
	}
	err := cli.Run(os.Args[1:], os.Stdout, os.Stderr)
	code := cli.ExitCode(err)
	if err != nil && code != 0 && code != 10 {
		fmt.Fprintln(os.Stderr, "wsi-label:", err)
	}
	os.Exit(code)
}
