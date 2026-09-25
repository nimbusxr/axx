package lifecycle

import (
	"slices"
	"strings"

	"github.com/nimbusxr/axx/internal/config"
)

// validateGraph checks that dependsOn only names declared apps and that the
// dependencies form a DAG.
func validateGraph(apps config.Apps) error {
	byName := make(map[string]config.App, len(apps))
	names := make([]string, 0, len(apps))
	for _, a := range apps {
		byName[a.Name] = a
		names = append(names, a.Name)
	}
	for _, a := range apps {
		for _, d := range a.DependsOn {
			if _, ok := byName[d]; !ok {
				return configErr(CodeUnknownDependency, "app %s depends on %q, which is not declared under apps", a.Name, d).
					WithHint("fix apps.%s.dependsOn; declared apps: %s", a.Name, strings.Join(names, ", "))
			}
		}
	}

	const (
		unvisited = iota
		visiting
		done
	)
	state := make(map[string]int, len(apps))
	var path []string
	var visit func(name string) error
	visit = func(name string) error {
		state[name] = visiting
		path = append(path, name)
		for _, d := range byName[name].DependsOn {
			switch state[d] {
			case visiting:
				cycle := append(slices.Clone(path[slices.Index(path, d):]), d)
				return configErr(CodeDependencyCycle, "apps depend on each other in a cycle: %s", strings.Join(cycle, " -> ")).
					WithHint("remove one of these apps.<name>.dependsOn entries so the apps can start in some order")
			case unvisited:
				if err := visit(d); err != nil {
					return err
				}
			}
		}
		path = path[:len(path)-1]
		state[name] = done
		return nil
	}
	for _, a := range apps {
		if state[a.Name] == unvisited {
			if err := visit(a.Name); err != nil {
				return err
			}
		}
	}
	return nil
}

// withDependencies returns the enabled apps among names plus every enabled
// app they depend on, transitively, in declaration order. Dependencies on
// disabled apps are ignored (they are managed elsewhere); unknown names are
// ignored (validateGraph reports them).
func withDependencies(apps config.Apps, names []string) []string {
	want := make(map[string]bool, len(apps))
	var add func(name string)
	add = func(name string) {
		app, ok := apps.Get(name)
		if !ok || !app.IsEnabled() || want[name] {
			return
		}
		want[name] = true
		for _, d := range app.DependsOn {
			add(d)
		}
	}
	for _, n := range names {
		add(n)
	}
	out := make([]string, 0, len(want))
	for _, a := range apps {
		if want[a.Name] {
			out = append(out, a.Name)
		}
	}
	return out
}

// usesDependsOn reports whether any app declares dependencies. Without any,
// apps start one after another in declaration order.
func usesDependsOn(apps config.Apps) bool {
	for _, a := range apps {
		if len(a.DependsOn) > 0 {
			return true
		}
	}
	return false
}
