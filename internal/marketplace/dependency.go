package marketplace

import (
	"fmt"
	"sort"
)

// ResolveOrder returns a topological install order for the given
// manifests. Manifests with no dependencies come first; cycles
// surface as ErrDependencyCycle. Within a level, slugs sort
// alphabetically to keep installs deterministic.
//
// The function only validates dependency-graph shape; semver
// satisfaction is the caller's job (ConstraintSatisfied is exposed in
// manifest.go). Keeping the two concerns separate keeps both
// functions small (cyclomatic <10).
func ResolveOrder(manifests []Manifest) ([]Manifest, error) {
	if len(manifests) == 0 {
		return nil, nil
	}
	bySlug, err := indexBySlug(manifests)
	if err != nil {
		return nil, err
	}
	indegree := buildIndegree(manifests, bySlug)
	queue := readyQueue(manifests, indegree)
	return drainQueue(queue, indegree, bySlug)
}

// indexBySlug builds a slug -> manifest lookup, rejecting duplicates
// up front so callers see a single ErrSlugAlreadyExists rather than a
// confusing topological failure.
func indexBySlug(manifests []Manifest) (map[string]Manifest, error) {
	out := make(map[string]Manifest, len(manifests))
	for _, m := range manifests {
		if _, dup := out[m.Slug]; dup {
			return nil, fmt.Errorf("%w: %s", ErrSlugAlreadyExists, m.Slug)
		}
		out[m.Slug] = m
	}
	return out, nil
}

// buildIndegree counts inbound dependency edges for each manifest. We
// reject manifests pointing at unknown slugs because the install
// pipeline cannot satisfy them.
func buildIndegree(manifests []Manifest, bySlug map[string]Manifest) map[string]int {
	indegree := make(map[string]int, len(manifests))
	for _, m := range manifests {
		indegree[m.Slug] = 0
	}
	for _, m := range manifests {
		for _, dep := range m.Dependencies {
			if _, ok := bySlug[dep.Slug]; !ok {
				continue
			}
			indegree[m.Slug]++
		}
	}
	return indegree
}

// readyQueue returns the manifests with zero unresolved dependencies,
// sorted alphabetically by slug for determinism.
func readyQueue(manifests []Manifest, indegree map[string]int) []Manifest {
	queue := make([]Manifest, 0, len(manifests))
	for _, m := range manifests {
		if indegree[m.Slug] == 0 {
			queue = append(queue, m)
		}
	}
	sort.Slice(queue, func(i, j int) bool { return queue[i].Slug < queue[j].Slug })
	return queue
}

// drainQueue walks the queue Kahn-style, decrementing indegree on
// dependents and growing the queue as new ready nodes appear. Any
// nodes left with non-zero indegree are part of a cycle.
func drainQueue(queue []Manifest, indegree map[string]int, bySlug map[string]Manifest) ([]Manifest, error) {
	dependents := buildDependents(bySlug)
	out := make([]Manifest, 0, len(bySlug))
	for len(queue) > 0 {
		head := queue[0]
		queue = queue[1:]
		out = append(out, head)
		queue = appendNewReady(queue, indegree, dependents[head.Slug], bySlug)
	}
	if len(out) != len(bySlug) {
		return nil, ErrDependencyCycle
	}
	return out, nil
}

// buildDependents inverts the dependency edges so we can find every
// manifest whose indegree to decrement when we drain a node.
func buildDependents(bySlug map[string]Manifest) map[string][]string {
	out := make(map[string][]string, len(bySlug))
	for slug, m := range bySlug {
		_ = slug
		for _, dep := range m.Dependencies {
			if _, ok := bySlug[dep.Slug]; !ok {
				continue
			}
			out[dep.Slug] = append(out[dep.Slug], m.Slug)
		}
	}
	for k := range out {
		sort.Strings(out[k])
	}
	return out
}

// appendNewReady walks the dependents of a freshly-drained slug and
// adds any that hit zero indegree to the queue.
func appendNewReady(queue []Manifest, indegree map[string]int, deps []string, bySlug map[string]Manifest) []Manifest {
	for _, slug := range deps {
		indegree[slug]--
		if indegree[slug] == 0 {
			queue = append(queue, bySlug[slug])
		}
	}
	return queue
}

// VerifyDependencySemver checks every dep of newManifest against the
// installed catalogue. Returns ErrSemverConflict on the first
// mismatch. Used by Registry.Install before persisting the new row.
func VerifyDependencySemver(newManifest Manifest, installed []Manifest) error {
	bySlug := make(map[string]Manifest, len(installed))
	for _, m := range installed {
		bySlug[m.Slug] = m
	}
	for _, dep := range newManifest.Dependencies {
		got, ok := bySlug[dep.Slug]
		if !ok {
			return fmt.Errorf("%w: missing dep %q", ErrSemverConflict, dep.Slug)
		}
		ok, err := ConstraintSatisfied(dep.Constraint, got.Version)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%w: %s @ %s does not satisfy %q",
				ErrSemverConflict, dep.Slug, got.Version, dep.Constraint)
		}
	}
	return nil
}
