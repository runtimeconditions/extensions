package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	goemitter "github.com/runtimeconditions/extensions/tooling/extension-bindings/emitters/go"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	flags := flag.NewFlagSet(goemitter.EmitterName, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	modelPath := flags.String("model", "", "normalized runtimeconditions.binding-model.yaml")
	packageConfigPath := flags.String("package-config", "", "one Go package-target configuration")
	output := flags.String("output", "", "new or empty output directory")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %s", strings.Join(flags.Args(), " "))
	}
	if *modelPath == "" || *packageConfigPath == "" || *output == "" {
		return fmt.Errorf("--model, --package-config, and --output are required")
	}
	model, err := goemitter.LoadModel(*modelPath)
	if err != nil {
		return err
	}
	target, err := goemitter.LoadPackageTarget(*packageConfigPath)
	if err != nil {
		return err
	}
	return goemitter.Emit(model, target, *output)
}
