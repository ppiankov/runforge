package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/ppiankov/runforge/internal/cli"
)

func main() {
	if err := cli.NewRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		var rlErr *cli.RateLimitError
		if errors.As(err, &rlErr) {
			os.Exit(4)
		}
		os.Exit(1)
	}
}
