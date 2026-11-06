package orchestrator

import (
	"fmt"
	"runtime/debug"

	"github.com/runtimeconditions/extensions/tooling/extension-bindings/normalizer"
)

const modulePath = "github.com/runtimeconditions/extensions/tooling/extension-bindings/orchestrator"

// ModuleIdentity identifies a module linked into the running executable. The digest covers the
// module path, version and checksum, so it is the same on every platform.
func ModuleIdentity(path, name string) (Tool, error) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return Tool{}, fmt.Errorf("build information is unavailable")
	}
	module := &info.Main
	if info.Main.Path != path {
		module = nil
		for _, dep := range info.Deps {
			if dep.Path == path {
				module = dep
				break
			}
		}
	}
	if module == nil {
		return Tool{}, fmt.Errorf("module %s is not linked into this executable", path)
	}
	for module.Replace != nil {
		module = module.Replace
	}
	version := module.Version
	if version == "" {
		version = "(devel)"
	}
	return Tool{Name: name, Version: version, SHA256: normalizer.SHA256Hex([]byte(path + "@" + version + " " + module.Sum))}, nil
}
