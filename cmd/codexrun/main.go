package main

import (
	"fmt"
	"os"

	"github.com/ppiankov/codexrun/internal/cli"
)

func main() {
	if err := cli.NewRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
