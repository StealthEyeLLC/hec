package hec

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	hecschemas "github.com/StealthEyeLLC/hec/schemas"
)

func TestDecodeCapabilityManifestStrictValidation(t *testing.T) {
	root := t.TempDir()
	recipeDir := filepath.Join(root, "recipes")
	if err := os.MkdirAll(recipeDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(recipeDir, "install-tool"), []byte("recipe"), 0644); err != nil {
		t.Fatal(err)
	}
	validPath := filepath.Join(root, "valid.toml")
	valid := `id = "tool.example"
description = "Example tool"
tags = ["example"]
commands = ["example"]
skills = ["example-skill"]
recipe = "install-tool"
installed_by_default = true
approximate_disk_class = "small"
`
	if err := os.WriteFile(validPath, []byte(valid), 0644); err != nil {
		t.Fatal(err)
	}
	manifest, err := decodeCapabilityManifest(validPath, recipeDir)
	if err != nil {
		t.Fatalf("decode valid manifest: %v", err)
	}
	if manifest.ID != "tool.example" || manifest.Description != "Example tool" || manifest.Recipe != "install-tool" {
		t.Fatalf("manifest = %#v", manifest)
	}
	if !reflect.DeepEqual(manifest.Tags, []string{"example"}) || !reflect.DeepEqual(manifest.Commands, []string{"example"}) || !reflect.DeepEqual(manifest.Skills, []string{"example-skill"}) {
		t.Fatalf("manifest arrays = %#v", manifest)
	}

	cases := map[string]string{
		"malformed":         `id = "broken`,
		"unknown field":     "id = \"x\"\ndescription = \"x\"\nunknown = true\n",
		"empty id":          "id = \"\"\ndescription = \"x\"\n",
		"empty description": "id = \"x\"\ndescription = \"\"\n",
		"empty command":     "id = \"x\"\ndescription = \"x\"\ncommands = [\"\"]\n",
		"empty skill":       "id = \"x\"\ndescription = \"x\"\nskills = [\"\"]\n",
		"empty tag":         "id = \"x\"\ndescription = \"x\"\ntags = [\"\"]\n",
		"invalid recipe":    "id = \"x\"\ndescription = \"x\"\nrecipe = \"../x\"\n",
		"missing recipe":    "id = \"x\"\ndescription = \"x\"\nrecipe = \"missing\"\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(root, strings.ReplaceAll(name, " ", "-")+".toml")
			if err := os.WriteFile(path, []byte(content), 0644); err != nil {
				t.Fatal(err)
			}
			if _, err := decodeCapabilityManifest(path, recipeDir); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestLoadCapabilityManifestDirRejectsDuplicateIDs(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.toml", "b.toml"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("id = \"duplicate\"\ndescription = \"same\"\ninstalled_by_default = true\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := loadCapabilityManifestDir(dir, t.TempDir(), "", nil); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate error = %v", err)
	}
}

func TestManifestInstalledUsesControlledPathAndSkills(t *testing.T) {
	bin := t.TempDir()
	command := filepath.Join(bin, "present")
	if err := os.WriteFile(command, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	skills := map[string]bool{"guide": true}
	cases := []struct {
		name     string
		manifest capabilityManifest
		want     bool
	}{
		{name: "default installed", manifest: capabilityManifest{InstalledByDefault: true}, want: true},
		{name: "default missing", manifest: capabilityManifest{}, want: false},
		{name: "command and skill", manifest: capabilityManifest{Commands: []string{"present"}, Skills: []string{"guide"}}, want: true},
		{name: "missing command", manifest: capabilityManifest{Commands: []string{"absent"}, Skills: []string{"guide"}}, want: false},
		{name: "missing skill", manifest: capabilityManifest{Commands: []string{"present"}, Skills: []string{"absent"}}, want: false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := manifestInstalled(test.manifest, bin, skills); got != test.want {
				t.Fatalf("installed = %v, want %v", got, test.want)
			}
		})
	}
}

func TestExtractOperationCapabilitiesUsesSchemaMetadata(t *testing.T) {
	cards, err := extractOperationCapabilities(hecschemas.CallHECInput)
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 38 {
		t.Fatalf("operation cards = %d, want 38", len(cards))
	}
	found := false
	for _, card := range cards {
		if card.ID == "hec.operation.terminal.open" {
			found = true
			if card.Operation != "terminal.open" || !card.Installed || card.Description == "" {
				t.Fatalf("terminal card = %#v", card)
			}
		}
	}
	if !found {
		t.Fatal("terminal.open operation card missing")
	}

	duplicate := []byte(`{"oneOf":[{"description":"a","properties":{"operation":{"const":"x"}}},{"description":"b","properties":{"operation":{"const":"x"}}}]}`)
	if _, err := extractOperationCapabilities(duplicate); err == nil {
		t.Fatal("expected duplicate operation error")
	}
}

func TestCapabilitiesDirectCommandIncludeMissingAndLimit(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	manifests := filepath.Join(root, "capabilities")
	skillsRoot := filepath.Join(root, "skills")
	recipes := filepath.Join(root, "recipes")
	for _, path := range []string{bin, manifests, skillsRoot, recipes} {
		if err := os.MkdirAll(path, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(bin, "presentcmd"), []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(manifests, "installed.toml"), []byte("id = \"installed.card\"\ndescription = \"Installed command card\"\ncommands = [\"presentcmd\"]\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(manifests, "missing.toml"), []byte("id = \"missing.card\"\ndescription = \"Missing command card\"\ncommands = [\"missingcmd\"]\n"), 0644); err != nil {
		t.Fatal(err)
	}
	writeTestSkill(t, skillsRoot, "operator", "operator", "Guidance for native tools", "body")

	dispatcher := NewDispatcher()
	dispatcher.capabilityDir = manifests
	dispatcher.skillRoots = []skillRoot{{Path: skillsRoot, Source: "builtin"}}
	dispatcher.workspaceRoot = filepath.Join(root, "no-workspaces")
	dispatcher.recipeDir = recipes
	dispatcher.commandPath = bin

	result := dispatcher.capabilities(context.Background(), map[string]any{"query": "presentcmd"})
	if !result.OK {
		t.Fatalf("present command result = %#v", result)
	}
	cards := result.Result["capabilities"].([]CapabilityCard)
	if len(cards) != 1 || cards[0].ID != "installed.card" {
		t.Fatalf("present command cards = %#v", cards)
	}

	if err := os.WriteFile(filepath.Join(bin, "directonly"), []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	result = dispatcher.capabilities(context.Background(), map[string]any{"query": "directonly"})
	cards = result.Result["capabilities"].([]CapabilityCard)
	if len(cards) != 1 || cards[0].ID != "command.directonly" || cards[0].Source != "path" {
		t.Fatalf("synthetic direct command cards = %#v", cards)
	}

	result = dispatcher.capabilities(context.Background(), map[string]any{"query": "missingcmd"})
	if result.Result["count"] != 0 {
		t.Fatalf("missing excluded result = %#v", result.Result)
	}
	result = dispatcher.capabilities(context.Background(), map[string]any{"query": "missingcmd", "include_missing": true})
	cards = result.Result["capabilities"].([]CapabilityCard)
	if len(cards) != 1 || cards[0].ID != "missing.card" || cards[0].Installed {
		t.Fatalf("missing included cards = %#v", cards)
	}

	result = dispatcher.capabilities(context.Background(), map[string]any{"limit": 2})
	if result.Result["count"] != 2 || len(result.Result["capabilities"].([]CapabilityCard)) != 2 {
		t.Fatalf("limited result = %#v", result.Result)
	}

	result = dispatcher.capabilities(context.Background(), map[string]any{"query": "two words"})
	for _, card := range result.Result["capabilities"].([]CapabilityCard) {
		if card.Source == "path" {
			t.Fatalf("multiword query created direct command card: %#v", card)
		}
	}
}

func TestMetadataTokenizationAndRanking(t *testing.T) {
	got := tokenizeMetadataQuery("Git-worktree, durable/jobs git")
	want := []string{"git", "worktree", "durable", "jobs"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tokens = %#v, want %#v", got, want)
	}
	exact := rankMetadata("git", []string{"git tools"}, []string{"git"})
	prefix := rankMetadata("git", []string{"github tools"}, []string{"github"})
	all := rankMetadata("durable jobs", []string{"systemd durable transient jobs"}, nil)
	partial := rankMetadata("durable jobs", []string{"durable work"}, nil)
	if !exact.betterThan(prefix) || !prefix.betterThan(all) || !all.betterThan(partial) {
		t.Fatalf("unexpected rank order: exact=%#v prefix=%#v all=%#v partial=%#v", exact, prefix, all, partial)
	}
}

func TestLookPathWithPATHDoesNotExecuteCommand(t *testing.T) {
	bin := t.TempDir()
	marker := filepath.Join(bin, "executed")
	command := filepath.Join(bin, "probe")
	content := "#!/bin/sh\ntouch " + marker + "\n"
	if err := os.WriteFile(command, []byte(content), 0755); err != nil {
		t.Fatal(err)
	}
	resolved, err := lookPathWithPATH("probe", bin)
	if err != nil || resolved != command {
		t.Fatalf("lookup = %q, %v", resolved, err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("command was executed during lookup: %v", err)
	}
}
