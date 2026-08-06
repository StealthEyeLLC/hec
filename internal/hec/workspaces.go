package hec

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/BurntSushi/toml"
)

const (
	MaxWorkspaceManifestBytes = int64(65536)
	MaxWorkspaceDescription   = 4096
	MaxWorkspaceNotes         = 16384
	MaxWorkspacePath          = 4096
	MaxWorkspaceTags          = 100
	MaxWorkspaceTagBytes      = 128
	MaxWorkspaceSkills        = 100
	MaxWorkspaceEnvironment   = 256
	MaxWorkspaceCardNotes     = 1024
)

var (
	workspaceNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	workspaceEnvPattern  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

type workspaceManifest struct {
	Description string            `toml:"description"`
	Notes       string            `toml:"notes"`
	DefaultCWD  string            `toml:"default_cwd"`
	Repository  string            `toml:"repository"`
	Tags        []string          `toml:"tags"`
	Skills      []string          `toml:"skills"`
	Env         map[string]string `toml:"env"`
}

type normalizedWorkspaceManifest struct {
	Description     string
	DescriptionSet  bool
	Notes           string
	NotesSet        bool
	DefaultCWD      string
	DefaultCWDSet   bool
	Repository      string
	RepositorySet   bool
	Tags            []string
	Skills          []string
	EnvironmentKeys []string
}

type workspaceMetadata struct {
	Name             string
	Location         string
	Description      string
	Notes            string
	DefaultCWD       string
	Repository       string
	RepositoryExists bool
	RepositoryKind   string
	Tags             []string
	Skills           []string
	EnvironmentKeys  []string
	AdditionalSearch []string
}

func validWorkspaceName(name string) bool {
	return workspaceNamePattern.MatchString(name)
}

func discoverWorkspaceCapabilities(workspaceRoot, gitPath string, skills []SkillMetadata) ([]CapabilityCard, []string, error) {
	if workspaceRoot == "" {
		return nil, nil, nil
	}
	entries, err := os.ReadDir(workspaceRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("read workspace root %s: %w", workspaceRoot, err)
	}

	workspaceSkills := workspaceSkillNames(skills)
	cards := make([]CapabilityCard, 0, len(entries))
	warnings := make([]string, 0)
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() {
			continue
		}
		name := entry.Name()
		location := filepath.Join(workspaceRoot, name)
		if !validWorkspaceName(name) {
			warnings = append(warnings, fmt.Sprintf("skip workspace %s: directory name does not match %s", location, workspaceNamePattern.String()))
			continue
		}
		metadata, err := inspectWorkspace(location, name, gitPath, workspaceSkills[filepath.Clean(location)])
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("skip workspace %s: %v", location, err))
			continue
		}
		cards = append(cards, workspaceCapabilityCard(metadata))
	}
	sort.Slice(cards, func(i, j int) bool { return capabilityLexicalLess(cards[i], cards[j]) })
	sort.Strings(warnings)
	return cards, warnings, nil
}

func workspaceSkillNames(skills []SkillMetadata) map[string][]string {
	byWorkspace := make(map[string][]string)
	for _, skill := range skills {
		if skill.Source != "workspace" {
			continue
		}
		skillRoot := filepath.Dir(filepath.Clean(skill.Location))
		if filepath.Base(skillRoot) != "skills" {
			continue
		}
		hecDir := filepath.Dir(skillRoot)
		if filepath.Base(hecDir) != ".hec" {
			continue
		}
		workspace := filepath.Dir(hecDir)
		byWorkspace[workspace] = append(byWorkspace[workspace], skill.Name)
	}
	for workspace, names := range byWorkspace {
		byWorkspace[workspace] = sortedUniqueStrings(names)
	}
	return byWorkspace
}

func inspectWorkspace(location, name, gitPath string, discoveredSkills []string) (workspaceMetadata, error) {
	manifest, err := loadWorkspaceManifest(filepath.Join(location, ".hec", "workspace.toml"))
	if err != nil {
		return workspaceMetadata{}, err
	}

	repository := ""
	repositoryDeclared := manifest.RepositorySet
	if repositoryDeclared {
		repository = resolveWorkspacePath(location, manifest.Repository)
	} else {
		for _, candidate := range []string{filepath.Join(location, "main"), filepath.Join(location, "repository")} {
			if info, statErr := os.Stat(candidate); statErr == nil && info.IsDir() {
				repository = candidate
				break
			} else if statErr != nil && !os.IsNotExist(statErr) {
				return workspaceMetadata{}, fmt.Errorf("inspect repository candidate %s: %w", candidate, statErr)
			}
		}
	}

	repositoryExists := false
	repositoryKind := "none"
	if repository != "" {
		inspectionPath := filepath.Clean(repository)
		if _, statErr := os.Stat(inspectionPath); statErr == nil {
			repositoryExists = true
			repositoryKind = inspectWorkspaceRepositoryKind(gitPath, inspectionPath)
		} else if os.IsNotExist(statErr) {
			if repositoryDeclared {
				repositoryKind = "missing"
			} else {
				repository = ""
			}
		} else {
			return workspaceMetadata{}, fmt.Errorf("inspect repository %s: %w", repository, statErr)
		}
	}

	defaultCWD := location
	if manifest.DefaultCWDSet {
		defaultCWD = resolveWorkspacePath(location, manifest.DefaultCWD)
	} else if repository != "" {
		defaultCWD = repository
	}

	description := manifest.Description
	if !manifest.DescriptionSet {
		description = fmt.Sprintf("Workspace %s at %s.", name, location)
	}
	tags := append([]string{"workspace", "project"}, manifest.Tags...)
	if repositoryKind == "worktree" || repositoryKind == "bare" {
		tags = append(tags, "git")
	}
	tags = sortedUniqueStrings(tags)
	workspaceSkills := sortedUniqueStrings(append(append([]string(nil), manifest.Skills...), discoveredSkills...))
	environmentKeys := nonNilStrings(manifest.EnvironmentKeys)

	search := []string{name, location, repository, defaultCWD, manifest.Notes}
	search = append(search, tags...)
	search = append(search, workspaceSkills...)
	search = append(search, environmentKeys...)

	return workspaceMetadata{
		Name:             name,
		Location:         location,
		Description:      description,
		Notes:            conciseWorkspaceNotes(manifest.Notes),
		DefaultCWD:       defaultCWD,
		Repository:       repository,
		RepositoryExists: repositoryExists,
		RepositoryKind:   repositoryKind,
		Tags:             tags,
		Skills:           workspaceSkills,
		EnvironmentKeys:  environmentKeys,
		AdditionalSearch: search,
	}, nil
}

func loadWorkspaceManifest(path string) (normalizedWorkspaceManifest, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return normalizedWorkspaceManifest{
				Tags:            []string{},
				Skills:          []string{},
				EnvironmentKeys: []string{},
			}, nil
		}
		return normalizedWorkspaceManifest{}, fmt.Errorf("stat workspace manifest: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return normalizedWorkspaceManifest{}, fmt.Errorf("workspace manifest is not a regular file")
	}
	if info.Size() > MaxWorkspaceManifestBytes {
		return normalizedWorkspaceManifest{}, fmt.Errorf("workspace manifest exceeds 65536 bytes")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return normalizedWorkspaceManifest{}, fmt.Errorf("read workspace manifest: %w", err)
	}
	if int64(len(data)) > MaxWorkspaceManifestBytes {
		return normalizedWorkspaceManifest{}, fmt.Errorf("workspace manifest exceeds 65536 bytes")
	}

	var raw workspaceManifest
	metadata, err := toml.DecodeReader(bytes.NewReader(data), &raw)
	if err != nil {
		return normalizedWorkspaceManifest{}, fmt.Errorf("decode workspace manifest: %w", err)
	}
	if undecoded := metadata.Undecoded(); len(undecoded) > 0 {
		return normalizedWorkspaceManifest{}, fmt.Errorf("workspace manifest contains unknown field %q", undecoded[0].String())
	}

	normalized := normalizedWorkspaceManifest{
		DescriptionSet: metadata.IsDefined("description"),
		NotesSet:       metadata.IsDefined("notes"),
		DefaultCWDSet:  metadata.IsDefined("default_cwd"),
		RepositorySet:  metadata.IsDefined("repository"),
	}
	if normalized.DescriptionSet {
		normalized.Description = strings.TrimSpace(raw.Description)
		if normalized.Description == "" {
			return normalizedWorkspaceManifest{}, fmt.Errorf("description must be nonempty when supplied")
		}
		if len(normalized.Description) > MaxWorkspaceDescription {
			return normalizedWorkspaceManifest{}, fmt.Errorf("description exceeds 4096 bytes")
		}
	}
	if normalized.NotesSet {
		normalized.Notes = strings.TrimSpace(raw.Notes)
		if len(normalized.Notes) > MaxWorkspaceNotes {
			return normalizedWorkspaceManifest{}, fmt.Errorf("notes exceeds 16384 bytes")
		}
	}
	if normalized.DefaultCWDSet {
		normalized.DefaultCWD = strings.TrimSpace(raw.DefaultCWD)
		if normalized.DefaultCWD == "" {
			return normalizedWorkspaceManifest{}, fmt.Errorf("default_cwd must be nonempty when supplied")
		}
		if len(normalized.DefaultCWD) > MaxWorkspacePath {
			return normalizedWorkspaceManifest{}, fmt.Errorf("default_cwd exceeds 4096 bytes")
		}
	}
	if normalized.RepositorySet {
		normalized.Repository = strings.TrimSpace(raw.Repository)
		if normalized.Repository == "" {
			return normalizedWorkspaceManifest{}, fmt.Errorf("repository must be nonempty when supplied")
		}
		if len(normalized.Repository) > MaxWorkspacePath {
			return normalizedWorkspaceManifest{}, fmt.Errorf("repository exceeds 4096 bytes")
		}
	}

	normalized.Tags, err = normalizeWorkspaceValues("tag", raw.Tags, MaxWorkspaceTags, MaxWorkspaceTagBytes, nil)
	if err != nil {
		return normalizedWorkspaceManifest{}, err
	}
	normalized.Skills, err = normalizeWorkspaceValues("skill hint", raw.Skills, MaxWorkspaceSkills, 0, validSkillName)
	if err != nil {
		return normalizedWorkspaceManifest{}, err
	}
	if len(raw.Env) > MaxWorkspaceEnvironment {
		return normalizedWorkspaceManifest{}, fmt.Errorf("env exceeds 256 entries")
	}
	normalized.EnvironmentKeys = make([]string, 0, len(raw.Env))
	for name := range raw.Env {
		if !workspaceEnvPattern.MatchString(name) {
			return normalizedWorkspaceManifest{}, fmt.Errorf("environment name %q is invalid", name)
		}
		normalized.EnvironmentKeys = append(normalized.EnvironmentKeys, name)
	}
	sort.Strings(normalized.EnvironmentKeys)
	if normalized.Tags == nil {
		normalized.Tags = []string{}
	}
	if normalized.Skills == nil {
		normalized.Skills = []string{}
	}
	if normalized.EnvironmentKeys == nil {
		normalized.EnvironmentKeys = []string{}
	}
	return normalized, nil
}

func normalizeWorkspaceValues(kind string, values []string, maxEntries, maxBytes int, validator func(string) bool) ([]string, error) {
	if len(values) > maxEntries {
		return nil, fmt.Errorf("%s entries exceed %d", kind, maxEntries)
	}
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, fmt.Errorf("%s entries must be nonempty", kind)
		}
		if maxBytes > 0 && len(value) > maxBytes {
			return nil, fmt.Errorf("%s %q exceeds %d bytes", kind, value, maxBytes)
		}
		if validator != nil && !validator(value) {
			return nil, fmt.Errorf("%s %q is invalid", kind, value)
		}
		cleaned = append(cleaned, value)
	}
	return sortedUniqueStrings(cleaned), nil
}

func resolveWorkspacePath(workspace, value string) string {
	if filepath.IsAbs(value) {
		return value
	}
	return filepath.Clean(filepath.Join(workspace, value))
}

func inspectWorkspaceRepositoryKind(gitPath, path string) string {
	if gitPath == "" {
		gitPath = "git"
	}
	if gitBoolean(gitPath, path, "--is-inside-work-tree") {
		return "worktree"
	}
	if gitBoolean(gitPath, path, "--is-bare-repository") {
		return "bare"
	}
	return "directory"
}

func gitBoolean(gitPath, path, argument string) bool {
	command := exec.Command(gitPath, "-C", path, "rev-parse", argument)
	command.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C")
	output, err := command.Output()
	return err == nil && strings.TrimSpace(string(output)) == "true"
}

func workspaceCapabilityCard(metadata workspaceMetadata) CapabilityCard {
	repositoryExists := metadata.RepositoryExists
	environmentKeys := nonNilStrings(metadata.EnvironmentKeys)
	return CapabilityCard{
		ID:                 "workspace." + metadata.Name,
		Description:        metadata.Description,
		Installed:          true,
		Commands:           []string{},
		Skills:             nonNilStrings(metadata.Skills),
		Recipe:             nil,
		Tags:               nonNilStrings(metadata.Tags),
		Source:             "workspace",
		Name:               metadata.Name,
		Location:           metadata.Location,
		Repository:         metadata.Repository,
		RepositoryExists:   &repositoryExists,
		RepositoryKind:     metadata.RepositoryKind,
		DefaultCWD:         metadata.DefaultCWD,
		EnvironmentKeys:    &environmentKeys,
		Notes:              metadata.Notes,
		AdditionalMetadata: nonNilStrings(metadata.AdditionalSearch),
	}
}

func sortedUniqueStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		cleaned = append(cleaned, value)
	}
	sort.Strings(cleaned)
	if cleaned == nil {
		return []string{}
	}
	return cleaned
}

func conciseWorkspaceNotes(notes string) string {
	if len(notes) <= MaxWorkspaceCardNotes {
		return notes
	}
	limit := MaxWorkspaceCardNotes - len("...")
	for limit > 0 && !utf8.RuneStart(notes[limit]) {
		limit--
	}
	return notes[:limit] + "..."
}
