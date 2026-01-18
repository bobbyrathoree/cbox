package main

import (
	"fmt"
	"os"

	"github.com/bobbyrathore/cbox/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		// Print error if not already displayed by CLI handlers
		// This catches errors that slip through due to SilenceErrors
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
