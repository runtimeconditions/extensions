package main

import (
	"context"
	"fmt"
	"os"

	"github.com/runtimeconditions/extensions/tooling/extension-bindings/orchestrator"
)

func main() {
	if err := orchestrator.Run(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
