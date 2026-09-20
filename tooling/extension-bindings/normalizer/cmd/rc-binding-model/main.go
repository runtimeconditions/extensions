package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/runtimeconditions/extensions/tooling/extension-bindings/normalizer"
)

type repeatedFlag []string

func (values *repeatedFlag) String() string {
	return strings.Join(*values, ",")
}

func (values *repeatedFlag) Set(value string) error {
	*values = append(*values, value)
	return nil
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	flags := flag.NewFlagSet("rc-binding-model", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	var extensionRoots repeatedFlag
	var packageRoots repeatedFlag
	var overrides repeatedFlag
	flags.Var(&extensionRoots, "extension-root", "repository-local extension catalog root; repeatable")
	flags.Var(&packageRoots, "package-root", "package-local vendored extension root; repeatable")
	flags.Var(&overrides, "extension-override", "exact extension identifier and file as <id>=<path>; repeatable")
	rootID := flags.String("root", "", "exact root extension identifier")
	cacheDirectory := flags.String("cache", "", "content-addressed local extension cache")
	network := flags.Bool("network", false, "permit locked HTTPS and OCI resolution")
	dependencyLockPath := flags.String("dependency-lock", "", "existing dependency-lock YAML to verify")
	dependencyLockOutput := flags.String("dependency-lock-output", "", "new path for the resolved dependency-lock YAML")
	semanticSchema := flags.String("semantic-schema", "../model/runtimeconditions.extension-semantic.schema.yaml", "extension semantic schema path")
	modelSchema := flags.String("model-schema", "../model/runtimeconditions.binding-model.schema.yaml", "binding-model schema path")
	coreID := flags.String("core-profile-id", "", "core profile schema identifier")
	coreVersion := flags.String("core-profile-version", "", "core profile schema version")
	coreDigest := flags.String("core-profile-semantic-sha256", "", "core profile schema semantic SHA-256")
	normalizerDigest := flags.String("normalizer-sha256", "", "locked normalizer release SHA-256")
	output := flags.String("output", "-", "new model output path, or - for standard output")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %s", strings.Join(flags.Args(), " "))
	}
	if *rootID == "" || *coreID == "" || *coreVersion == "" || *coreDigest == "" || *normalizerDigest == "" {
		return fmt.Errorf("--root, --core-profile-id, --core-profile-version, --core-profile-semantic-sha256, and --normalizer-sha256 are required")
	}
	if *dependencyLockOutput != "" && (*dependencyLockOutput == *output || (*dependencyLockOutput == "-" && *output == "-")) {
		return fmt.Errorf("model and dependency lock outputs must be different")
	}
	overrideMap := map[string]string{}
	for _, value := range overrides {
		separator := strings.IndexByte(value, '=')
		if separator <= 0 || separator == len(value)-1 {
			return fmt.Errorf("invalid --extension-override %q; expected <id>=<path>", value)
		}
		id, path := value[:separator], value[separator+1:]
		if _, duplicate := overrideMap[id]; duplicate {
			return fmt.Errorf("duplicate --extension-override for %q", id)
		}
		overrideMap[id] = path
	}

	schemas, err := normalizer.LoadSchemas(*semanticSchema, *modelSchema)
	if err != nil {
		return err
	}
	var suppliedLock normalizer.DependencyLock
	resolverLocks := map[string]normalizer.LockEntry{}
	if *dependencyLockPath != "" {
		data, err := os.ReadFile(*dependencyLockPath)
		if err != nil {
			return err
		}
		suppliedLock, err = normalizer.DecodeDependencyLock(data)
		if err != nil {
			return err
		}
		for _, entry := range suppliedLock.Extensions {
			resolverLocks[entry.ID] = normalizer.LockEntry{
				SourceSHA256:   entry.SourceSHA256,
				SemanticSHA256: entry.SemanticSHA256,
				Locator:        entry.SourceLocator,
			}
		}
	}
	resolver, err := normalizer.NewResolver(normalizer.ResolverConfig{
		Schemas: schemas, Overrides: overrideMap,
		CatalogRoots: extensionRoots, PackageRoots: packageRoots,
		CacheDir: *cacheDirectory, Network: *network, Locks: resolverLocks,
	})
	if err != nil {
		return err
	}
	closure, err := resolver.Resolve(context.Background(), *rootID)
	if err != nil {
		return err
	}
	if *dependencyLockPath != "" {
		if err := normalizer.ValidateDependencyLock(closure, suppliedLock); err != nil {
			return err
		}
	}
	lock := normalizer.BuildDependencyLock(closure)
	model, err := normalizer.Normalize(closure, lock, schemas, normalizer.NormalizeConfig{
		CoreProfileSchema: normalizer.CoreProfileIdentity{
			ID: *coreID, Version: *coreVersion, SemanticSHA256: *coreDigest,
		},
		Normalizer: normalizer.ToolIdentity{
			Name: normalizer.NormalizerName, Version: normalizer.NormalizerVersion, SHA256: *normalizerDigest,
		},
	})
	if err != nil {
		return err
	}
	modelBytes, err := normalizer.CanonicalModelYAML(model)
	if err != nil {
		return err
	}
	if err := writeNew(*output, modelBytes); err != nil {
		return err
	}
	if *dependencyLockOutput != "" {
		lockBytes, err := normalizer.CanonicalDependencyLockYAML(lock)
		if err != nil {
			return err
		}
		if err := writeNew(*dependencyLockOutput, lockBytes); err != nil {
			return err
		}
	}
	return nil
}

func writeNew(path string, data []byte) error {
	if path == "-" {
		_, err := os.Stdout.Write(data)
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
