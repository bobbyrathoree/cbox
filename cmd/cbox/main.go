package main

import (
	"os"

	"github.com/bobbyrathore/cbox/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
