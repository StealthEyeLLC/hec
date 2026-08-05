package hec

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeTestSkill(t *testing.T, root, directory, name, description, body string) string {
	t.Helper()
	location := filepath.Join(root, directory)
	if err := os.MkdirAll(location, 0755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\n\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(location, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return location
}

func testSkillDispatcher(t *testing.T) (*Dispatcher, string, string, string) {
	t.Helper()
	root := t.TempDir()
	builtin := filepath.Join(root, "builtin")
	owner := filepath.Join(root, "owner")
	workspaces := filepath.Join(root, "workspaces")
	for _, path := range []string{builtin, owner, workspaces} {
		if err := os.MkdirAll(path, 0755); err != nil {
			t.Fatal(err)
		}
	}
	dispatcher := NewDispatcher()
	dispatcher.skillRoots = []skillRoot{{Path: builtin, Source: "builtin"}, {Path: owner, Source: "owner"}}
	dispatcher.workspaceRoot = workspaces
	return dispatcher, builtin, owner, workspaces
}

func TestParseSkillMetadataFrontmatter(t *testing.T) {
	root := t.TempDir()
	location := writeTestSkill(t, root, "valid", "valid-skill", "Does useful work", "instructions")
	metadata, err := parseSkillMetadata(filepath.Join(location, "SKILL.md"), location, "builtin")
	if err != nil {
		t.Fatal(err)
	}
	want := SkillMetadata{Name: "valid-skill", Description: "Does useful work", Location: location, Source: "builtin"}
	if !reflect.DeepEqual(metadata, want) {
		t.Fatalf("metadata = %#v, want %#v", metadata, want)
	}

	cases := map[string]string{
		"no frontmatter": "# no frontmatter\n",
		"unterminated":   "---\nname: broken\n",
		"bad yaml":       "---\nname: [\ndescription: x\n---\n",
		"uppercase":      "---\nname: Bad-Skill\ndescription: x\n---\n",
		"empty name":     "---\nname: \"\"\ndescription: x\n---\n",
		"empty desc":     "---\nname: valid\ndescription: \"\"\n---\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(root, strings.ReplaceAll(name, " ", "-")+".md")
			if err := os.WriteFile(path, []byte(content), 0644); err != nil {
				t.Fatal(err)
			}
			if _, err := parseSkillMetadata(path, filepath.Dir(path), "owner"); err == nil {
				t.Fatal("expected frontmatter validation error")
			}
		})
	}
}

func TestSkillDiscoveryRootsWarningsAndDuplicateNames(t *testing.T) {
	dispatcher, builtin, owner, workspaces := testSkillDispatcher(t)
	writeTestSkill(t, builtin, "alpha-dir", "alpha", "Alpha skill", "body")
	writeTestSkill(t, owner, "alpha-owner", "alpha", "Owner alpha", "body")
	workspaceSkills := filepath.Join(workspaces, "project-a", ".hec", "skills")
	writeTestSkill(t, workspaceSkills, "beta-dir", "beta", "Workspace beta", "body")
	malformed := filepath.Join(owner, "broken")
	if err := os.MkdirAll(malformed, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(malformed, "SKILL.md"), []byte("not frontmatter"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(owner, "missing"), 0755); err != nil {
		t.Fatal(err)
	}

	skills, warnings := dispatcher.discoverSkills()
	if len(skills) != 3 {
		t.Fatalf("skills = %#v", skills)
	}
	if skills[0].Name != "alpha" || skills[0].Source != "builtin" || skills[1].Name != "alpha" || skills[1].Source != "owner" || skills[2].Name != "beta" || skills[2].Source != "workspace" {
		t.Fatalf("sorted skills = %#v", skills)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "broken") {
		t.Fatalf("warnings = %#v", warnings)
	}
	roots := discoverWorkspaceSkillRoots(workspaces)
	if len(roots) != 1 || roots[0].Path != workspaceSkills || roots[0].Source != "workspace" {
		t.Fatalf("workspace roots = %#v", roots)
	}
}

func TestSkillListPaginationAndFind(t *testing.T) {
	dispatcher, builtin, _, _ := testSkillDispatcher(t)
	writeTestSkill(t, builtin, "one", "one", "Durable jobs and system tools", "body one")
	writeTestSkill(t, builtin, "two", "two", "Native tools and worktrees", "body two")
	writeTestSkill(t, builtin, "three", "three", "Unrelated guide", "body three")

	listed := dispatcher.skillList(context.Background(), map[string]any{"offset": 1, "limit": 1})
	if !listed.OK {
		t.Fatalf("list = %#v", listed)
	}
	if listed.Result["offset"] != 1 || listed.Result["next_offset"] != 2 || listed.Result["total_skills"] != 3 || listed.Result["eof"] != false {
		t.Fatalf("pagination = %#v", listed.Result)
	}
	page := listed.Result["skills"].([]SkillMetadata)
	if len(page) != 1 || page[0].Name != "three" {
		t.Fatalf("page = %#v", page)
	}

	found := dispatcher.skillFind(context.Background(), map[string]any{"query": "durable jobs", "limit": 10})
	if !found.OK || found.Result["count"] != 1 {
		t.Fatalf("find = %#v", found)
	}
	matches := found.Result["skills"].([]SkillMetadata)
	if matches[0].Name != "one" {
		t.Fatalf("matches = %#v", matches)
	}
	found = dispatcher.skillFind(context.Background(), map[string]any{"query": "native tools"})
	matches = found.Result["skills"].([]SkillMetadata)
	if len(matches) < 1 || matches[0].Name != "two" {
		t.Fatalf("native matches = %#v", matches)
	}
}

func TestSkillReadByNameAndLocation(t *testing.T) {
	dispatcher, builtin, _, _ := testSkillDispatcher(t)
	location := writeTestSkill(t, builtin, "operator", "operator", "Operate things", "Read [reference](references/details.md).")
	if err := os.MkdirAll(filepath.Join(location, "references"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(location, "references", "details.md"), []byte("secret reference body"), 0644); err != nil {
		t.Fatal(err)
	}

	byName := dispatcher.skillRead(context.Background(), map[string]any{"name": "operator"})
	if !byName.OK {
		t.Fatalf("read by name = %#v", byName)
	}
	content := byName.Result["content"].(string)
	if !strings.Contains(content, "Read [reference]") || strings.Contains(content, "secret reference body") {
		t.Fatalf("skill content disclosure = %q", content)
	}
	if byName.Result["content_bytes"] != len(content) || byName.Result["location"] != location {
		t.Fatalf("read metadata = %#v", byName.Result)
	}

	byLocation := dispatcher.skillRead(context.Background(), map[string]any{"location": location})
	if !byLocation.OK || byLocation.Result["content"] != byName.Result["content"] {
		t.Fatalf("read by location = %#v", byLocation)
	}
}

func TestSkillReadAmbiguityAndInvalidLocation(t *testing.T) {
	dispatcher, builtin, owner, _ := testSkillDispatcher(t)
	first := writeTestSkill(t, builtin, "same-one", "same", "First", "one")
	second := writeTestSkill(t, owner, "same-two", "same", "Second", "two")
	ambiguous := dispatcher.skillRead(context.Background(), map[string]any{"name": "same"})
	if ambiguous.OK || ambiguous.Error == nil || ambiguous.Error.Code != "skill_ambiguous" || !strings.Contains(ambiguous.Error.Message, first) || !strings.Contains(ambiguous.Error.Message, second) {
		t.Fatalf("ambiguous result = %#v", ambiguous)
	}

	outside := dispatcher.skillRead(context.Background(), map[string]any{"location": t.TempDir()})
	if outside.OK || outside.Error == nil || outside.Error.Code != "skill_not_found" {
		t.Fatalf("outside location = %#v", outside)
	}
	relative := dispatcher.skillRead(context.Background(), map[string]any{"location": "relative/path"})
	if relative.OK || relative.Error == nil || relative.Error.Code != "invalid_arguments" {
		t.Fatalf("relative location = %#v", relative)
	}
	slashed := dispatcher.skillRead(context.Background(), map[string]any{"name": "same/path"})
	if slashed.OK || slashed.Error == nil || slashed.Error.Code != "invalid_arguments" {
		t.Fatalf("slashed name = %#v", slashed)
	}
	both := dispatcher.skillRead(context.Background(), map[string]any{"name": "same", "location": first})
	if both.OK || both.Error == nil || both.Error.Code != "invalid_arguments" {
		t.Fatalf("both selectors = %#v", both)
	}
}

func TestSkillReadUTF8AndSizeBounds(t *testing.T) {
	dispatcher, builtin, _, _ := testSkillDispatcher(t)
	invalidLocation := filepath.Join(builtin, "invalid")
	if err := os.MkdirAll(invalidLocation, 0755); err != nil {
		t.Fatal(err)
	}
	invalid := append([]byte("---\nname: invalid\ndescription: Invalid body\n---\n"), 0xff)
	if err := os.WriteFile(filepath.Join(invalidLocation, "SKILL.md"), invalid, 0644); err != nil {
		t.Fatal(err)
	}
	invalidResult := dispatcher.skillRead(context.Background(), map[string]any{"name": "invalid"})
	if invalidResult.OK || invalidResult.Error == nil || invalidResult.Error.Code != "skill_invalid" {
		t.Fatalf("invalid UTF-8 result = %#v", invalidResult)
	}

	largeLocation := filepath.Join(builtin, "large")
	if err := os.MkdirAll(largeLocation, 0755); err != nil {
		t.Fatal(err)
	}
	prefix := []byte("---\nname: large\ndescription: Large skill\n---\n")
	large := append(prefix, make([]byte, MaxSkillContentBytes-int64(len(prefix))+1)...)
	if err := os.WriteFile(filepath.Join(largeLocation, "SKILL.md"), large, 0644); err != nil {
		t.Fatal(err)
	}
	largeResult := dispatcher.skillRead(context.Background(), map[string]any{"name": "large"})
	if largeResult.OK || largeResult.Error == nil || largeResult.Error.Code != "skill_too_large" || !strings.Contains(largeResult.Error.Message, "file.read") {
		t.Fatalf("large result = %#v", largeResult)
	}
}

func TestSkillArgumentBounds(t *testing.T) {
	dispatcher, _, _, _ := testSkillDispatcher(t)
	cases := []Result{
		dispatcher.skillList(context.Background(), map[string]any{"offset": -1}),
		dispatcher.skillList(context.Background(), map[string]any{"limit": 0}),
		dispatcher.skillList(context.Background(), map[string]any{"limit": 1001}),
		dispatcher.skillFind(context.Background(), map[string]any{}),
		dispatcher.skillFind(context.Background(), map[string]any{"query": ""}),
		dispatcher.skillFind(context.Background(), map[string]any{"query": "x", "limit": 101}),
		dispatcher.skillRead(context.Background(), map[string]any{}),
	}
	for index, result := range cases {
		if result.OK || result.Error == nil || result.Error.Code != "invalid_arguments" {
			t.Fatalf("case %d = %#v", index, result)
		}
	}
}
