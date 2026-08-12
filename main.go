package main

import (
	"errors"
	"fmt"
	"os"

	"ai-resume-tailor/internal/cli"
)

func main() {
	if err := cli.Run(os.Args[1:]); err != nil {
		// A UsageError means the user invoked a command wrong -> exit code 2.
		// Anything else is a real failure -> exit code 1.
		var ue *cli.UsageError
		if errors.As(err, &ue) {
			fmt.Fprintln(os.Stderr, ue.Error())
			os.Exit(2)
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
