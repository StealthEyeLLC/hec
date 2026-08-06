package hec

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

const (
	DefaultBuiltinSkillRoot = "/opt/hec/current/skills"
	DefaultOwnerSkillRoot   = "/etc/hec/skills"
	DefaultWorkspaceRoot    = "/srv/hec/workspaces"
	MaxSkillContentBytes    = int64(1 << 20)
	maxSkillFrontmatterSize = 64 << 10
)

type skillRoot struct {
	Path   string
	Source string
}

type SkillMetadata struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Location    string `json:"location"`
	Source      string `json:"source"`
}

type skillListArgs struct {
	Offset *int `json:"offset"`
	Limit  *int `json:"limit"`
}

type skillFindArgs struct {
	Query string `json:"query"`
	Limit *int   `json:"limit"`
}

type skillReadArgs struct {
	Name     *string `json:"name"`
	Location *string `json:"location"`
}

type skillFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

func (d *Dispatcher) skillList(_ context.Context, raw map[string]any) Result {
	var args skillListArgs
	if err := decodeOperationArgs(raw, &args); err != nil {
		return failedResult("skill.list", "invalid_arguments", err.Error())
	}
	offset := 0
	if args.Offset != nil {
		offset = *args.Offset
	}
	limit := 100
	if args.Limit != nil {
		limit = *args.Limit
	}
	if offset < 0 {
		return failedResult("skill.list", "invalid_arguments", "offset must be greater than or equal to zero")
	}
	if limit < 1 || limit > 1000 {
		return failedResult("skill.list", "invalid_arguments", "limit must be between 1 and 1000")
	}

	skills, warnings := d.discoverSkills()
	start := offset
	if start > len(skills) {
		start = len(skills)
	}
	end := start + limit
	if end > len(skills) {
		end = len(skills)
	}
	page := append([]SkillMetadata(nil), skills[start:end]...)

	result := newResult("skill.list")
	result.OK = true
	result.Result = map[string]any{
		"offset":       offset,
		"next_offset":  offset + len(page),
		"total_skills": len(skills),
		"eof":          end >= len(skills),
		"skills":       page,
	}
	if len(warnings) > 0 {
		result.Result["warnings"] = warnings
	}
	return result
}

func (d *Dispatcher) skillFind(_ context.Context, raw map[string]any) Result {
	var args skillFindArgs
	if err := decodeOperationArgs(raw, &args); err != nil {
		return failedResult("skill.find", "invalid_arguments", err.Error())
	}
	query := strings.TrimSpace(args.Query)
	if query == "" {
		return failedResult("skill.find", "invalid_arguments", "query must be a nonempty string")
	}
	if len(query) > 512 {
		return failedResult("skill.find", "invalid_arguments", "query must not exceed 512 bytes")
	}
	limit := 10
	if args.Limit != nil {
		limit = *args.Limit
	}
	if limit < 1 || limit > 100 {
		return failedResult("skill.find", "invalid_arguments", "limit must be between 1 and 100")
	}

	skills, warnings := d.discoverSkills()
	type rankedSkill struct {
		Skill SkillMetadata
		Rank  metadataRank
	}
	ranked := make([]rankedSkill, 0, len(skills))
	for _, skill := range skills {
		rank := rankMetadata(query, []string{skill.Name, skill.Description, skill.Location}, []string{skill.Name})
		if !rank.matched() {
			continue
		}
		ranked = append(ranked, rankedSkill{Skill: skill, Rank: rank})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].Rank.betterThan(ranked[j].Rank) {
			return true
		}
		if ranked[j].Rank.betterThan(ranked[i].Rank) {
			return false
		}
		left := ranked[i].Skill
		right := ranked[j].Skill
		if left.Name != right.Name {
			return left.Name < right.Name
		}
		if left.Source != right.Source {
			return left.Source < right.Source
		}
		return left.Location < right.Location
	})
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	matches := make([]SkillMetadata, 0, len(ranked))
	for _, item := range ranked {
		matches = append(matches, item.Skill)
	}

	result := newResult("skill.find")
	result.OK = true
	result.Result = map[string]any{
		"query":  query,
		"count":  len(matches),
		"skills": matches,
	}
	if len(warnings) > 0 {
		result.Result["warnings"] = warnings
	}
	return result
}

func (d *Dispatcher) skillRead(_ context.Context, raw map[string]any) Result {
	var args skillReadArgs
	if err := decodeOperationArgs(raw, &args); err != nil {
		return failedResult("skill.read", "invalid_arguments", err.Error())
	}
	if (args.Name == nil) == (args.Location == nil) {
		return failedResult("skill.read", "invalid_arguments", "skill.read requires exactly one of name or location")
	}

	skills, _ := d.discoverSkills()
	var selected SkillMetadata
	if args.Name != nil {
		name := strings.TrimSpace(*args.Name)
		if name == "" {
			return failedResult("skill.read", "invalid_arguments", "name must be a nonempty string")
		}
		if strings.ContainsAny(name, `/\\`) {
			return failedResult("skill.read", "invalid_arguments", "name must not contain path separators")
		}
		matches := make([]SkillMetadata, 0, 1)
		for _, skill := range skills {
			if skill.Name == name {
				matches = append(matches, skill)
			}
		}
		switch len(matches) {
		case 0:
			return failedResult("skill.read", "skill_not_found", fmt.Sprintf("skill %q was not found", name))
		case 1:
			selected = matches[0]
		default:
			locations := make([]string, 0, len(matches))
			for _, match := range matches {
				locations = append(locations, match.Location)
			}
			sort.Strings(locations)
			return failedResult("skill.read", "skill_ambiguous", fmt.Sprintf("skill %q is ambiguous; use one of these locations: %s", name, strings.Join(locations, ", ")))
		}
	} else {
		location := filepath.Clean(strings.TrimSpace(*args.Location))
		if location == "." || !filepath.IsAbs(location) {
			return failedResult("skill.read", "invalid_arguments", "location must be an absolute discovered skill directory")
		}
		found := false
		for _, skill := range skills {
			if skill.Location == location {
				selected = skill
				found = true
				break
			}
		}
		if !found {
			return failedResult("skill.read", "skill_not_found", fmt.Sprintf("no discovered skill exists at %q", location))
		}
	}

	path := filepath.Join(selected.Location, "SKILL.md")
	info, err := os.Stat(path)
	if err != nil {
		return failedResult("skill.read", "skill_read_failed", fmt.Sprintf("read %s: %v", path, err))
	}
	if info.Size() > MaxSkillContentBytes {
		return failedResult("skill.read", "skill_too_large", fmt.Sprintf("SKILL.md is larger than 1 MiB; use file.read for %s", path))
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return failedResult("skill.read", "skill_read_failed", fmt.Sprintf("read %s: %v", path, err))
	}
	if !utf8.Valid(content) {
		return failedResult("skill.read", "skill_invalid", "SKILL.md is not valid UTF-8")
	}

	result := newResult("skill.read")
	result.OK = true
	result.Result = map[string]any{
		"name":          selected.Name,
		"description":   selected.Description,
		"location":      selected.Location,
		"source":        selected.Source,
		"content":       string(content),
		"content_bytes": len(content),
	}
	return result
}

func (d *Dispatcher) discoverSkills() ([]SkillMetadata, []string) {
	roots := append([]skillRoot(nil), d.skillRoots...)
	roots = append(roots, discoverWorkspaceSkillRoots(d.workspaceRoot)...)

	var skills []SkillMetadata
	var warnings []string
	for _, root := range roots {
		entries, err := os.ReadDir(root.Path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			warnings = append(warnings, fmt.Sprintf("skip skill root %s: %v", root.Path, err))
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			location := filepath.Join(root.Path, entry.Name())
			metadata, err := parseSkillMetadata(filepath.Join(location, "SKILL.md"), location, root.Source)
			if err != nil {
				if !os.IsNotExist(err) {
					warnings = append(warnings, fmt.Sprintf("skip skill %s: %v", location, err))
				}
				continue
			}
			skills = append(skills, metadata)
		}
	}
	sort.Slice(skills, func(i, j int) bool {
		if skills[i].Name != skills[j].Name {
			return skills[i].Name < skills[j].Name
		}
		if skills[i].Source != skills[j].Source {
			return skills[i].Source < skills[j].Source
		}
		return skills[i].Location < skills[j].Location
	})
	sort.Strings(warnings)
	return skills, warnings
}

func discoverWorkspaceSkillRoots(workspaceRoot string) []skillRoot {
	if workspaceRoot == "" {
		return nil
	}
	entries, err := os.ReadDir(workspaceRoot)
	if err != nil {
		return nil
	}
	roots := make([]skillRoot, 0, len(entries))
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() || !validWorkspaceName(entry.Name()) {
			continue
		}
		hecPath := filepath.Join(workspaceRoot, entry.Name(), ".hec")
		hecInfo, err := os.Lstat(hecPath)
		if err != nil || hecInfo.Mode()&os.ModeSymlink != 0 || !hecInfo.IsDir() {
			continue
		}
		path := filepath.Join(hecPath, "skills")
		info, err := os.Lstat(path)
		if err == nil && info.Mode()&os.ModeSymlink == 0 && info.IsDir() {
			roots = append(roots, skillRoot{Path: path, Source: "workspace"})
		}
	}
	sort.Slice(roots, func(i, j int) bool { return roots[i].Path < roots[j].Path })
	return roots
}

func parseSkillMetadata(path, location, source string) (SkillMetadata, error) {
	file, err := os.Open(path)
	if err != nil {
		return SkillMetadata{}, err
	}
	defer file.Close()

	frontmatter, err := readSkillFrontmatter(file)
	if err != nil {
		return SkillMetadata{}, err
	}
	var parsed skillFrontmatter
	if err := yaml.Unmarshal(frontmatter, &parsed); err != nil {
		return SkillMetadata{}, fmt.Errorf("invalid YAML frontmatter: %w", err)
	}
	parsed.Name = strings.TrimSpace(parsed.Name)
	parsed.Description = strings.TrimSpace(parsed.Description)
	if !validSkillName(parsed.Name) {
		return SkillMetadata{}, fmt.Errorf("name must be nonempty and lowercase")
	}
	if parsed.Description == "" {
		return SkillMetadata{}, fmt.Errorf("description must be nonempty")
	}
	return SkillMetadata{
		Name:        parsed.Name,
		Description: parsed.Description,
		Location:    filepath.Clean(location),
		Source:      source,
	}, nil
}

func readSkillFrontmatter(reader io.Reader) ([]byte, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), maxSkillFrontmatterSize)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("missing YAML frontmatter")
	}
	if strings.TrimSpace(scanner.Text()) != "---" {
		return nil, fmt.Errorf("SKILL.md must begin with YAML frontmatter")
	}
	var buffer bytes.Buffer
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "---" {
			return buffer.Bytes(), nil
		}
		buffer.WriteString(line)
		buffer.WriteByte('\n')
		if buffer.Len() > maxSkillFrontmatterSize {
			return nil, fmt.Errorf("YAML frontmatter exceeds 64 KiB")
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("unterminated YAML frontmatter")
}

func validSkillName(name string) bool {
	if name == "" {
		return false
	}
	for index, char := range name {
		switch {
		case char >= 'a' && char <= 'z':
		case char >= '0' && char <= '9':
		case char == '-' && index > 0 && index < len(name)-1:
		default:
			return false
		}
	}
	return true
}
