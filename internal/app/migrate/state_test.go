package migrate

import (
	"path/filepath"
	"testing"
)

func TestStateStoreReturnsDeepClonedDiscoverySnapshots(t *testing.T) {
	store, err := NewStateStore(filepath.Join(t.TempDir(), "discoveries.json"))
	if err != nil {
		t.Fatalf("NewStateStore() error = %v", err)
	}
	saved, err := store.Save("Production", "aws", []string{"eu-west-3"}, []ResourceInfo{{
		ID:           "lambda-1",
		Name:         "thumbnailer",
		Tags:         map[string]string{"team": "media"},
		Dependencies: []string{"role-1"},
		Config:       map[string]interface{}{"settings": map[string]interface{}{"memory": "128"}},
	}})
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	saved.Regions[0] = "mutated"
	saved.Resources[0].Tags["team"] = "mutated"
	saved.Resources[0].Dependencies[0] = "mutated"
	saved.Resources[0].Config["settings"].(map[string]interface{})["memory"] = "mutated"

	got, err := store.Get(saved.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Regions[0] != "eu-west-3" || got.Resources[0].Tags["team"] != "media" || got.Resources[0].Dependencies[0] != "role-1" || got.Resources[0].Config["settings"].(map[string]interface{})["memory"] != "128" {
		t.Errorf("Get() returned a snapshot mutated through Save() result: %#v", got)
	}

	got.Resources[0].Tags["team"] = "changed-again"
	again, err := store.Get(saved.ID)
	if err != nil {
		t.Fatalf("Get() second call error = %v", err)
	}
	if again.Resources[0].Tags["team"] != "media" {
		t.Errorf("Get() leaked internal state through prior result: %#v", again)
	}
}

func TestStateStoreDeepClonesCompositeConfigValues(t *testing.T) {
	store, err := NewStateStore(filepath.Join(t.TempDir(), "discoveries.json"))
	if err != nil {
		t.Fatalf("NewStateStore() error = %v", err)
	}
	saved, err := store.Save("Production", "aws", nil, []ResourceInfo{{
		ID: "lambda-1",
		Config: map[string]interface{}{
			"rules":  []map[string]interface{}{{"labels": []string{"initial"}}},
			"groups": map[string][]string{"ops": {"initial"}},
		},
	}})
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	config := saved.Resources[0].Config
	config["rules"].([]map[string]interface{})[0]["labels"].([]string)[0] = "mutated"
	config["groups"].(map[string][]string)["ops"][0] = "mutated"

	got, err := store.Get(saved.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if label := got.Resources[0].Config["rules"].([]map[string]interface{})[0]["labels"].([]string)[0]; label != "initial" {
		t.Errorf("rules label = %q, want initial", label)
	}
	if group := got.Resources[0].Config["groups"].(map[string][]string)["ops"][0]; group != "initial" {
		t.Errorf("group value = %q, want initial", group)
	}
}
