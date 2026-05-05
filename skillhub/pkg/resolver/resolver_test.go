package resolver

import (
	"errors"
	"testing"

	"skillhub/pkg/types"
)

var errFetch = errors.New("fetch error")

func TestResolveSimple(t *testing.T) {
	r := New(func(id string) ([]types.SkillSummary, error) {
		return nil, nil
	})
	result, err := r.Resolve([]types.SkillSummary{{ID: "A", Version: "v1.0.0"}})
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := result["A"]; !ok || v != "v1.0.0" {
		t.Errorf("expected A=v1.0.0, got A=%v", v)
	}
	if len(result) != 1 {
		t.Errorf("expected 1 entry, got %d", len(result))
	}
}

func TestResolve_MVSKeepsHighest(t *testing.T) {
	r := New(func(id string) ([]types.SkillSummary, error) {
		return nil, nil
	})
	result, err := r.Resolve([]types.SkillSummary{
		{ID: "A", Version: "v1.0.0"},
		{ID: "A", Version: "v1.5.0"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := result["A"]; !ok || v != "v1.5.0" {
		t.Errorf("expected A=v1.5.0, got A=%v", v)
	}
}

func TestResolveTransitive(t *testing.T) {
	fetch := make(map[string][]types.SkillSummary)
	fetch["A"] = []types.SkillSummary{{ID: "C", Version: "v1.3.0"}}
	fetch["B"] = []types.SkillSummary{{ID: "C", Version: "v1.4.0"}}
	fetch["C"] = nil

	r := New(func(id string) ([]types.SkillSummary, error) {
		return fetch[id], nil
	})
	result, err := r.Resolve([]types.SkillSummary{
		{ID: "A", Version: "v1.0.0"},
		{ID: "B", Version: "v1.0.0"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := result["A"]; !ok || v != "v1.0.0" {
		t.Errorf("expected A=v1.0.0, got A=%v", v)
	}
	if v, ok := result["B"]; !ok || v != "v1.0.0" {
		t.Errorf("expected B=v1.0.0, got B=%v", v)
	}
	if v, ok := result["C"]; !ok || v != "v1.4.0" {
		t.Errorf("expected C=v1.4.0, got C=%v", v)
	}
}

func TestResolve_Cycle(t *testing.T) {
	fetch := make(map[string][]types.SkillSummary)
	fetch["A"] = []types.SkillSummary{{ID: "B", Version: "v1.0.0"}}
	fetch["B"] = []types.SkillSummary{{ID: "A", Version: "v1.0.0"}}

	r := New(func(id string) ([]types.SkillSummary, error) {
		return fetch[id], nil
	})
	result, err := r.Resolve([]types.SkillSummary{{ID: "A", Version: "v1.0.0"}})
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := result["A"]; !ok || v != "v1.0.0" {
		t.Errorf("expected A=v1.0.0, got A=%v", v)
	}
	if v, ok := result["B"]; !ok || v != "v1.0.0" {
		t.Errorf("expected B=v1.0.0, got B=%v", v)
	}
}

func TestResolve_DeepChain(t *testing.T) {
	fetch := make(map[string][]types.SkillSummary)
	fetch["A"] = []types.SkillSummary{{ID: "B", Version: "v2.0.0"}}
	fetch["B"] = []types.SkillSummary{{ID: "C", Version: "v3.0.0"}}
	fetch["C"] = []types.SkillSummary{{ID: "D", Version: "v4.0.0"}}
	fetch["D"] = nil

	r := New(func(id string) ([]types.SkillSummary, error) {
		return fetch[id], nil
	})
	result, err := r.Resolve([]types.SkillSummary{{ID: "A", Version: "v1.0.0"}})
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := result["A"]; !ok || v != "v1.0.0" {
		t.Errorf("expected A=v1.0.0, got A=%v", v)
	}
	if v, ok := result["B"]; !ok || v != "v2.0.0" {
		t.Errorf("expected B=v2.0.0, got B=%v", v)
	}
	if v, ok := result["C"]; !ok || v != "v3.0.0" {
		t.Errorf("expected C=v3.0.0, got C=%v", v)
	}
	if v, ok := result["D"]; !ok || v != "v4.0.0" {
		t.Errorf("expected D=v4.0.0, got D=%v", v)
	}
}

func TestResolve_Empty(t *testing.T) {
	r := New(func(id string) ([]types.SkillSummary, error) {
		return nil, nil
	})
	result, err := r.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty map, got %v", result)
	}
}

func TestResolve_Error(t *testing.T) {
	r := New(func(id string) ([]types.SkillSummary, error) {
		return nil, errFetch
	})
	_, err := r.Resolve([]types.SkillSummary{{ID: "A", Version: "v1.0.0"}})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}


