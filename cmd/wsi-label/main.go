package main

import (
	"fmt"
	"os"

	"github.com/tcornish/wsi-label/internal/cli"
)

func main() {
	err := cli.Run(os.Args[1:], os.Stdout, os.Stderr)
	code := cli.ExitCode(err)
	if err != nil && code != 0 && code != 10 {
		fmt.Fprintln(os.Stderr, "wsi-label:", err)
	}
	os.Exit(code)
}
