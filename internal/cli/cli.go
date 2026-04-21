package cli

import (
	"fmt"
	"io"
)

// Run dispatches subcommands. Returns a typed error; callers map to exit codes.
func Run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		printUsage(stderr)
		return fmt.Errorf("%w: no subcommand", ErrUsage)
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "inspect":
		return RunInspect(rest, stdout, stderr)
	case "replace":
		return RunReplace(rest, stdout, stderr)
	case "strip":
		return RunStrip(rest, stdout, stderr)
	case "-h", "--help", "help":
		printUsage(stdout)
		return nil
	default:
		printUsage(stderr)
		return fmt.Errorf("%w: unknown subcommand %q", ErrUsage, sub)
	}
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, `wsi-label — replace or strip the label image in WSI files

Usage:
  wsi-label replace <wsi-in> <label-image> [<wsi-out>] [flags]
  wsi-label strip   <wsi-in>               [<wsi-out>] [flags]
  wsi-label inspect <wsi-in>                           [flags]

Run 'wsi-label <subcommand> -h' for subcommand flags.`)
}
