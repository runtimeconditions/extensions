package orchestrator

import (
	"fmt"
	"sort"

	"github.com/runtimeconditions/extensions/tooling/extension-bindings/normalizer"
)

type Target struct {
	Key          string         `yaml:"key" json:"key"`
	PackageKey   string         `yaml:"packageKey" json:"packageKey"`
	Language     string         `yaml:"language" json:"language"`
	Set          PackageSet     `yaml:"-" json:"-"`
	Config       LanguageTarget `yaml:"configuration" json:"configuration"`
	Dependencies []string       `yaml:"dependencies,omitempty" json:"dependencies,omitempty"`
	Selected     bool           `yaml:"selected" json:"selected"`
}
type BuildPlan struct {
	Targets []Target `yaml:"targets" json:"targets"`
}

func (p *Project) SelectTargets(keys []string) (BuildPlan, error) {
	selected := map[string]bool{}
	if len(keys) == 0 {
		for key, set := range p.Catalog.Packages {
			for lang := range set.Languages {
				selected[key+":"+lang] = true
			}
		}
	} else {
		for _, key := range keys {
			if selected[key] {
				return BuildPlan{}, fmt.Errorf("duplicate target %s", key)
			}
			selected[key] = true
		}
	}
	all := map[string]Target{}
	for key, set := range p.Catalog.Packages {
		for language, config := range set.Languages {
			id := key + ":" + language
			all[id] = Target{Key: id, PackageKey: key, Language: language, Set: set, Config: config, Selected: selected[id]}
		}
	}
	for key := range selected {
		if _, ok := all[key]; !ok {
			return BuildPlan{}, fmt.Errorf("unknown target %s", key)
		}
	}
	result := BuildPlan{Targets: []Target{}}
	for _, key := range sortedKeys(all) {
		if selected[key] {
			result.Targets = append(result.Targets, all[key])
		}
	}
	return result, nil
}

// OrderTargets derives package edges exclusively from resolved extension edges.
// A selected target pulls in its native dependency providers for build inputs;
// only Selected targets are retained as command outputs.
func (p *Project) OrderTargets(plan BuildPlan, models map[string]normalizer.BindingModel) (BuildPlan, error) {
	all := map[string]Target{}
	providers := map[string][]string{}
	for key, set := range p.Catalog.Packages {
		for language, config := range set.Languages {
			id := key + ":" + language
			all[id] = Target{Key: id, PackageKey: key, Language: language, Set: set, Config: config}
			providers[language+"\x00"+set.RootExtension] = append(providers[language+"\x00"+set.RootExtension], id)
		}
	}
	for _, target := range plan.Targets {
		entry := all[target.Key]
		entry.Selected = true
		all[target.Key] = entry
	}
	state := map[string]int{}
	result := BuildPlan{Targets: []Target{}}
	var visit func(string) error
	visit = func(key string) error {
		if state[key] == 2 {
			return nil
		}
		if state[key] == 1 {
			return fmt.Errorf("package dependency cycle at %s", key)
		}
		state[key] = 1
		target := all[key]
		model, ok := models[target.Set.RootExtension]
		if !ok {
			return fmt.Errorf("%s: root model missing", key)
		}
		for _, edge := range model.DependencyEdges {
			if edge.From != target.Set.RootExtension {
				continue
			}
			matches := providers[target.Language+"\x00"+edge.To]
			if len(matches) != 1 {
				return fmt.Errorf("%s: direct dependency %s requires exactly one %s catalog provider (found %d)", key, edge.To, target.Language, len(matches))
			}
			target.Dependencies = append(target.Dependencies, matches[0])
		}
		sort.Strings(target.Dependencies)
		for _, dependency := range target.Dependencies {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		state[key] = 2
		result.Targets = append(result.Targets, target)
		return nil
	}
	for _, target := range plan.Targets {
		if err := visit(target.Key); err != nil {
			return BuildPlan{}, err
		}
	}
	return result, nil
}
