package resolver

import (
	"golang.org/x/mod/semver"
	"skillhub/pkg/types"
)

type DepFetcher func(id string) ([]types.SkillSummary, error)

type Resolver struct {
	fetch DepFetcher
}

func New(fetch DepFetcher) *Resolver {
	return &Resolver{fetch: fetch}
}

func (r *Resolver) Resolve(deps []types.SkillSummary) (map[string]string, error) {
	resolved := make(map[string]string)

	var queue []string
	queued := make(map[string]bool)

	for _, dep := range deps {
		if existing, ok := resolved[dep.ID]; ok {
			if semver.Compare(dep.Version, existing) > 0 {
				resolved[dep.ID] = dep.Version
			}
		} else {
			resolved[dep.ID] = dep.Version
			queue = append(queue, dep.ID)
			queued[dep.ID] = true
		}
	}

	visited := make(map[string]bool)
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]

		if visited[id] {
			continue
		}
		visited[id] = true

		transitive, err := r.fetch(id)
		if err != nil {
			return nil, err
		}

		for _, dep := range transitive {
			if existing, ok := resolved[dep.ID]; ok {
				if semver.Compare(dep.Version, existing) > 0 {
					resolved[dep.ID] = dep.Version
				}
			} else {
				resolved[dep.ID] = dep.Version
			}

			if !visited[dep.ID] && !queued[dep.ID] {
				queue = append(queue, dep.ID)
				queued[dep.ID] = true
			}
		}
	}

	return resolved, nil
}
