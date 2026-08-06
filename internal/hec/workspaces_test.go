package hec

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func writeWorkspaceManifest(t *testing.T, workspace, content string) string {
	t.Helper()
	hecDir := filepath.Join(workspace, ".hec")
	if err := os.MkdirAll(hecDir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(hecDir, "workspace.toml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func runTestGit(t *testing.T, directory string, args ...string) string {
	t.Helper()
	commandArgs := append([]string{"-C", directory}, args...)
	command := exec.Command("git", commandArgs...)
	command.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(commandArgs, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func createTestWorktree(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, path, "init", "-q")
	runTestGit(t, path, "config", "user.name", "HEC Slice 8 Test")
	runTestGit(t, path, "config", "user.email", "slice8-test@example.invalid")
	if err := os.WriteFile(filepath.Join(path, "README.md"), []byte("test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, path, "add", "README.md")
	runTestGit(t, path, "commit", "-q", "-m", "initial")
}

func TestValidWorkspaceName(t *testing.T) {
	valid := []string{"a", "A1", "project.one", "project_two", "project-three", strings.Repeat("a", 128)}
	invalid := []string{"", ".hidden", "-leading", "_leading", "has space", "slash/name", strings.Repeat("a", 129)}
	for _, name := range valid {
		if !validWorkspaceName(name) {
			t.Errorf("validWorkspaceName(%q) = false", name)
		}
	}
	for _, name := range invalid {
		if validWorkspaceName(name) {
			t.Errorf("validWorkspaceName(%q) = true", name)
		}
	}
}

func TestWorkspaceDiscoveryEmptyDirectChildrenAndSymlinks(t *testing.T) {
	root := t.TempDir()
	cards, warnings, err := discoverWorkspaceCapabilities(root, "git", nil)
	if err != nil || len(cards) != 0 || len(warnings) != 0 {
		t.Fatalf("empty discovery = %#v, %#v, %v", cards, warnings, err)
	}

	alpha := filepath.Join(root, "alpha")
	if err := os.MkdirAll(filepath.Join(alpha, "nested-project"), 0755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "bad name"), 0755); err != nil {
		t.Fatal(err)
	}

	cards, warnings, err = discoverWorkspaceCapabilities(root, "git", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 1 || cards[0].ID != "workspace.alpha" {
		t.Fatalf("direct-child cards = %#v", cards)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], filepath.Join(root, "bad name")) || !strings.Contains(warnings[0], "directory name") {
		t.Fatalf("warnings = %#v", warnings)
	}
	if strings.Contains(strings.Join(warnings, " "), "linked") {
		t.Fatalf("symlink should be ignored without warning: %#v", warnings)
	}
}

func TestWorkspaceManifestAbsenceAndStrictValidation(t *testing.T) {
	workspace := t.TempDir()
	manifest, err := loadWorkspaceManifest(filepath.Join(workspace, ".hec", "workspace.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.DescriptionSet || manifest.RepositorySet || manifest.DefaultCWDSet || manifest.Tags == nil || manifest.Skills == nil || manifest.EnvironmentKeys == nil {
		t.Fatalf("absent manifest = %#v", manifest)
	}

	cases := map[string]string{
		"malformed":     `description = "broken`,
		"unknown":       "unknown = true\n",
		"empty desc":    "description = \"   \"\n",
		"desc limit":    "description = \"" + strings.Repeat("d", MaxWorkspaceDescription+1) + "\"\n",
		"notes limit":   "notes = \"" + strings.Repeat("n", MaxWorkspaceNotes+1) + "\"\n",
		"empty cwd":     "default_cwd = \" \"\n",
		"cwd limit":     "default_cwd = \"" + strings.Repeat("c", MaxWorkspacePath+1) + "\"\n",
		"empty repo":    "repository = \" \"\n",
		"repo limit":    "repository = \"" + strings.Repeat("r", MaxWorkspacePath+1) + "\"\n",
		"empty tag":     "tags = [\"\"]\n",
		"tag limit":     "tags = [\"" + strings.Repeat("t", MaxWorkspaceTagBytes+1) + "\"]\n",
		"bad skill":     "skills = [\"Bad-Skill\"]\n",
		"empty skill":   "skills = [\"\"]\n",
		"bad env name":  "[env]\n1BAD = \"x\"\n",
		"bad env value": "[env]\nGOOD = 123\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			path := writeWorkspaceManifest(t, root, content)
			if _, err := loadWorkspaceManifest(path); err == nil {
				t.Fatal("expected validation failure")
			}
		})
	}

	oversized := t.TempDir()
	path := writeWorkspaceManifest(t, oversized, "description = \"x\"\n"+strings.Repeat("#", int(MaxWorkspaceManifestBytes)))
	if _, err := loadWorkspaceManifest(path); err == nil || !strings.Contains(err.Error(), "65536") {
		t.Fatalf("oversized error = %v", err)
	}

	symlinked := t.TempDir()
	target := filepath.Join(t.TempDir(), "workspace.toml")
	if err := os.WriteFile(target, []byte("description = \"outside\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	hecDir := filepath.Join(symlinked, ".hec")
	if err := os.MkdirAll(hecDir, 0755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(hecDir, "workspace.toml")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := loadWorkspaceManifest(link); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("symlinked manifest error = %v", err)
	}
}

func TestWorkspaceManifestCollectionLimits(t *testing.T) {
	tooManyTags := make([]string, MaxWorkspaceTags+1)
	for index := range tooManyTags {
		tooManyTags[index] = fmt.Sprintf("tag-%d", index)
	}
	tooManySkills := make([]string, MaxWorkspaceSkills+1)
	for index := range tooManySkills {
		tooManySkills[index] = fmt.Sprintf("skill-%d", index)
	}
	tooManyEnv := make([]string, 0, MaxWorkspaceEnvironment+1)
	for index := 0; index <= MaxWorkspaceEnvironment; index++ {
		tooManyEnv = append(tooManyEnv, fmt.Sprintf("ENV_%d = \"x\"", index))
	}

	cases := map[string]string{
		"tags":   "tags = [\"" + strings.Join(tooManyTags, "\", \"") + "\"]\n",
		"skills": "skills = [\"" + strings.Join(tooManySkills, "\", \"") + "\"]\n",
		"env":    "[env]\n" + strings.Join(tooManyEnv, "\n") + "\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			workspace := t.TempDir()
			path := writeWorkspaceManifest(t, workspace, content)
			if _, err := loadWorkspaceManifest(path); err == nil {
				t.Fatal("expected collection limit failure")
			}
		})
	}
}

func TestWorkspaceManifestNormalizationAndPathResolution(t *testing.T) {
	workspace := t.TempDir()
	absoluteCWD := filepath.Join(t.TempDir(), "absolute-cwd")
	content := "description = \"  Example workspace  \"\n" +
		"notes = \"  Notes for matching  \"\n" +
		"default_cwd = \"" + absoluteCWD + "\"\n" +
		"repository = \"main\"\n" +
		"tags = [\"zeta\", \" alpha \", \"zeta\"]\n" +
		"skills = [\"beta-skill\", \"alpha-skill\", \"beta-skill\"]\n" +
		"[env]\nZ_KEY = \"secret-z\"\nA_KEY = \"secret-a\"\n"
	path := writeWorkspaceManifest(t, workspace, content)
	manifest, err := loadWorkspaceManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Description != "Example workspace" || manifest.Notes != "Notes for matching" {
		t.Fatalf("trimmed strings = %#v", manifest)
	}
	if !reflect.DeepEqual(manifest.Tags, []string{"alpha", "zeta"}) || !reflect.DeepEqual(manifest.Skills, []string{"alpha-skill", "beta-skill"}) {
		t.Fatalf("normalized lists = %#v", manifest)
	}
	if !reflect.DeepEqual(manifest.EnvironmentKeys, []string{"A_KEY", "Z_KEY"}) {
		t.Fatalf("environment keys = %#v", manifest.EnvironmentKeys)
	}
	if resolveWorkspacePath(workspace, absoluteCWD) != absoluteCWD {
		t.Fatal("absolute path was rewritten")
	}
	if resolveWorkspacePath(workspace, "main/sub") != filepath.Join(workspace, "main", "sub") {
		t.Fatal("relative path was not resolved from workspace")
	}

	metadata, err := inspectWorkspace(workspace, filepath.Base(workspace), "git", []string{"gamma-skill", "alpha-skill"})
	if err != nil {
		t.Fatal(err)
	}
	if metadata.DefaultCWD != absoluteCWD || metadata.Repository != filepath.Join(workspace, "main") || metadata.RepositoryKind != "missing" || metadata.RepositoryExists {
		t.Fatalf("resolved metadata = %#v", metadata)
	}
	if !reflect.DeepEqual(metadata.Skills, []string{"alpha-skill", "beta-skill", "gamma-skill"}) {
		t.Fatalf("merged skills = %#v", metadata.Skills)
	}
	card := workspaceCapabilityCard(metadata)
	encoded, err := json.Marshal(card)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	if strings.Contains(text, "secret-a") || strings.Contains(text, "secret-z") || !strings.Contains(text, `"environment_keys":["A_KEY","Z_KEY"]`) {
		t.Fatalf("environment disclosure = %s", text)
	}
}

func TestWorkspaceRepositoryInferenceAndKinds(t *testing.T) {
	root := t.TempDir()

	worktreeWorkspace := filepath.Join(root, "worktree")
	worktreeRepo := filepath.Join(worktreeWorkspace, "main")
	createTestWorktree(t, worktreeRepo)
	metadata, err := inspectWorkspace(worktreeWorkspace, "worktree", "git", nil)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Repository != worktreeRepo || metadata.RepositoryKind != "worktree" || !metadata.RepositoryExists || metadata.DefaultCWD != worktreeRepo {
		t.Fatalf("worktree metadata = %#v", metadata)
	}

	bareWorkspace := filepath.Join(root, "bare")
	bareRepo := filepath.Join(bareWorkspace, "repository")
	if err := os.MkdirAll(bareWorkspace, 0755); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("git", "init", "--bare", "-q", bareRepo).CombinedOutput(); err != nil {
		t.Fatalf("init bare: %v\n%s", err, output)
	}
	metadata, err = inspectWorkspace(bareWorkspace, "bare", "git", nil)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Repository != bareRepo || metadata.RepositoryKind != "bare" || !metadata.RepositoryExists || !containsValue(metadata.Tags, "git") {
		t.Fatalf("bare metadata = %#v", metadata)
	}

	directoryWorkspace := filepath.Join(root, "directory")
	directoryRepo := filepath.Join(directoryWorkspace, "main")
	if err := os.MkdirAll(directoryRepo, 0755); err != nil {
		t.Fatal(err)
	}
	metadata, err = inspectWorkspace(directoryWorkspace, "directory", "git", nil)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.RepositoryKind != "directory" || !metadata.RepositoryExists {
		t.Fatalf("directory metadata = %#v", metadata)
	}

	missingWorkspace := filepath.Join(root, "missing")
	if err := os.MkdirAll(missingWorkspace, 0755); err != nil {
		t.Fatal(err)
	}
	writeWorkspaceManifest(t, missingWorkspace, "repository = \"future/repo\"\n")
	metadata, err = inspectWorkspace(missingWorkspace, "missing", "git", nil)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Repository != filepath.Join(missingWorkspace, "future", "repo") || metadata.RepositoryKind != "missing" || metadata.RepositoryExists {
		t.Fatalf("missing metadata = %#v", metadata)
	}

	noneWorkspace := filepath.Join(root, "none")
	if err := os.MkdirAll(noneWorkspace, 0755); err != nil {
		t.Fatal(err)
	}
	metadata, err = inspectWorkspace(noneWorkspace, "none", "git", nil)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Repository != "" || metadata.RepositoryKind != "none" || metadata.DefaultCWD != noneWorkspace {
		t.Fatalf("none metadata = %#v", metadata)
	}
}

func TestWorkspaceDiscoverySkillMergeSortingAndWarnings(t *testing.T) {
	root := t.TempDir()
	valid := filepath.Join(root, "valid")
	if err := os.MkdirAll(filepath.Join(valid, ".hec", "skills"), 0755); err != nil {
		t.Fatal(err)
	}
	writeWorkspaceManifest(t, valid, "description = \"Dedicated workspace\"\nnotes = \"searchable note\"\ntags = [\"slice8-unique-tag\", \"a\"]\nskills = [\"hint-skill\"]\n[env]\nDISCOVERY_KEY = \"do-not-return\"\n")
	writeTestSkill(t, filepath.Join(valid, ".hec", "skills"), "local", "local-skill", "Local skill metadata", "body must remain progressive")

	broken := filepath.Join(root, "broken")
	if err := os.MkdirAll(broken, 0755); err != nil {
		t.Fatal(err)
	}
	writeWorkspaceManifest(t, broken, "unknown = true\n")

	dispatcher := NewDispatcher()
	dispatcher.workspaceRoot = root
	dispatcher.skillRoots = nil
	dispatcher.capabilityDir = filepath.Join(t.TempDir(), "no-capabilities")
	dispatcher.recipeDir = filepath.Join(t.TempDir(), "no-recipes")

	result := dispatcher.capabilities(context.Background(), map[string]any{"query": "workspace.valid", "limit": 10})
	if !result.OK {
		t.Fatalf("capabilities = %#v", result)
	}
	cards := result.Result["capabilities"].([]CapabilityCard)
	var card CapabilityCard
	foundWorkspace := false
	for _, candidate := range cards {
		if candidate.ID == "workspace.valid" {
			card = candidate
			foundWorkspace = true
			break
		}
	}
	if !foundWorkspace {
		t.Fatalf("cards = %#v", cards)
	}
	if card.ID != "workspace.valid" || card.Source != "workspace" || !card.Installed || card.Commands == nil || card.Skills == nil || card.Recipe != nil || card.EnvironmentKeys == nil {
		t.Fatalf("workspace card = %#v", card)
	}
	if !reflect.DeepEqual(card.Skills, []string{"hint-skill", "local-skill"}) || !reflect.DeepEqual(*card.EnvironmentKeys, []string{"DISCOVERY_KEY"}) {
		t.Fatalf("merged metadata = %#v", card)
	}
	warnings, ok := result.Result["warnings"].([]string)
	if !ok || len(warnings) != 1 || !strings.Contains(warnings[0], broken) || !strings.Contains(warnings[0], "unknown field") || strings.Contains(warnings[0], "unknown = true") {
		t.Fatalf("warnings = %#v", result.Result["warnings"])
	}

	queries := []string{"Dedicated workspace", "searchable note", "slice8-unique-tag", valid, "DISCOVERY_KEY", "local-skill"}
	for _, query := range queries {
		matched := dispatcher.capabilities(context.Background(), map[string]any{"query": query, "limit": 10})
		if !matched.OK {
			t.Fatalf("query %q failed: %#v", query, matched)
		}
		found := false
		for _, candidate := range matched.Result["capabilities"].([]CapabilityCard) {
			if candidate.ID == "workspace.valid" {
				found = true
			}
		}
		if !found {
			t.Fatalf("query %q did not find workspace: %#v", query, matched.Result)
		}
	}

	listed := dispatcher.skillList(context.Background(), map[string]any{})
	if !listed.OK {
		t.Fatalf("skill.list = %#v", listed)
	}
	skills := listed.Result["skills"].([]SkillMetadata)
	if len(skills) != 1 || skills[0].Name != "local-skill" || skills[0].Source != "workspace" {
		t.Fatalf("workspace skills = %#v", skills)
	}
}

func TestWorkspaceCardsDeterministicStableArraysAndDuplicateIDs(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"zeta", "alpha"} {
		if err := os.Mkdir(filepath.Join(root, name), 0755); err != nil {
			t.Fatal(err)
		}
	}
	cards, warnings, err := discoverWorkspaceCapabilities(root, "git", nil)
	if err != nil || len(warnings) != 0 {
		t.Fatalf("discovery = %#v, %#v, %v", cards, warnings, err)
	}
	ids := []string{cards[0].ID, cards[1].ID}
	if !sort.StringsAreSorted(ids) || !reflect.DeepEqual(ids, []string{"workspace.alpha", "workspace.zeta"}) {
		t.Fatalf("ids = %#v", ids)
	}
	for _, card := range cards {
		if card.Commands == nil || card.Skills == nil || card.Tags == nil || card.EnvironmentKeys == nil || *card.EnvironmentKeys == nil {
			t.Fatalf("nil workspace arrays = %#v", card)
		}
		encoded, err := json.Marshal(card)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), `"commands":null`) || strings.Contains(string(encoded), `"skills":null`) || strings.Contains(string(encoded), `"environment_keys":null`) || strings.Contains(string(encoded), `"tags":null`) {
			t.Fatalf("null arrays = %s", encoded)
		}
	}

	manifests := t.TempDir()
	if err := os.WriteFile(filepath.Join(manifests, "duplicate.toml"), []byte("id = \"workspace.alpha\"\ndescription = \"duplicate\"\ninstalled_by_default = true\n"), 0644); err != nil {
		t.Fatal(err)
	}
	dispatcher := NewDispatcher()
	dispatcher.workspaceRoot = root
	dispatcher.skillRoots = nil
	dispatcher.capabilityDir = manifests
	dispatcher.recipeDir = t.TempDir()
	result := dispatcher.capabilities(context.Background(), map[string]any{"query": "workspace.alpha"})
	if result.OK || result.Error == nil || !strings.Contains(result.Error.Message, `duplicate capability id "workspace.alpha"`) {
		t.Fatalf("duplicate result = %#v", result)
	}
}

func containsValue(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
