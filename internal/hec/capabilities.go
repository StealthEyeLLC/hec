package hec

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/BurntSushi/toml"
	hecschemas "github.com/StealthEyeLLC/hec/schemas"
)

const (
	DefaultCapabilityDir = "/opt/hec/current/capabilities"
	DefaultRecipeDir     = "/opt/hec/current/forge/recipes"
)

type capabilitiesArgs struct {
	Query          *string `json:"query"`
	Limit          *int    `json:"limit"`
	IncludeMissing *bool   `json:"include_missing"`
}

type CapabilityCard struct {
	ID                   string   `json:"id"`
	Description          string   `json:"description"`
	Installed            bool     `json:"installed"`
	Commands             []string `json:"commands"`
	Skills               []string `json:"skills"`
	Recipe               *string  `json:"recipe"`
	Operation            string   `json:"operation,omitempty"`
	Tags                 []string `json:"tags,omitempty"`
	Source               string   `json:"source,omitempty"`
	ApproximateDiskClass string   `json:"approximate_disk_class,omitempty"`
}

type capabilityManifest struct {
	ID                   string   `toml:"id"`
	Description          string   `toml:"description"`
	Tags                 []string `toml:"tags"`
	Commands             []string `toml:"commands"`
	Skills               []string `toml:"skills"`
	Recipe               string   `toml:"recipe"`
	InstalledByDefault   bool     `toml:"installed_by_default"`
	ApproximateDiskClass string   `toml:"approximate_disk_class"`
}

type operationSchema struct {
	OneOf []struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Properties  struct {
			Operation struct {
				Const       string `json:"const"`
				Description string `json:"description"`
			} `json:"operation"`
		} `json:"properties"`
	} `json:"oneOf"`
}

type metadataRank struct {
	Exact      bool
	Prefix     bool
	AllTokens  bool
	MatchCount int
}

func (rank metadataRank) matched() bool {
	return rank.Exact || rank.Prefix || rank.MatchCount > 0
}

func (rank metadataRank) betterThan(other metadataRank) bool {
	if rank.Exact != other.Exact {
		return rank.Exact
	}
	if rank.Prefix != other.Prefix {
		return rank.Prefix
	}
	if rank.AllTokens != other.AllTokens {
		return rank.AllTokens
	}
	return rank.MatchCount > other.MatchCount
}

func (d *Dispatcher) capabilities(_ context.Context, raw map[string]any) Result {
	var args capabilitiesArgs
	if err := decodeOperationArgs(raw, &args); err != nil {
		return failedResult("capabilities", "invalid_arguments", err.Error())
	}

	var query string
	if args.Query != nil {
		query = strings.TrimSpace(*args.Query)
		if query == "" {
			return failedResult("capabilities", "invalid_arguments", "query must be a nonempty string when supplied")
		}
		if len(query) > 512 {
			return failedResult("capabilities", "invalid_arguments", "query must not exceed 512 bytes")
		}
	}
	limit := 10
	if args.Limit != nil {
		limit = *args.Limit
	}
	if limit < 1 || limit > 100 {
		return failedResult("capabilities", "invalid_arguments", "limit must be between 1 and 100")
	}
	includeMissing := false
	if args.IncludeMissing != nil {
		includeMissing = *args.IncludeMissing
	}

	cards, err := d.collectCapabilities()
	if err != nil {
		return failedResult("capabilities", "capability_discovery_failed", err.Error())
	}
	if query != "" && validCommandToken(query) {
		if _, err := lookPathWithPATH(query, d.commandPath); err == nil && !cardsRepresentCommand(cards, query) {
			cards = append(cards, CapabilityCard{
				ID:          "command." + strings.ToLower(query),
				Description: fmt.Sprintf("Command %s is available on the HEC service PATH.", query),
				Installed:   true,
				Commands:    []string{query},
				Skills:      []string{},
				Recipe:      nil,
				Source:      "path",
			})
		}
	}

	filtered := make([]CapabilityCard, 0, len(cards))
	for _, card := range cards {
		if includeMissing || card.Installed {
			filtered = append(filtered, card)
		}
	}
	if query == "" {
		sort.Slice(filtered, func(i, j int) bool { return capabilityLexicalLess(filtered[i], filtered[j]) })
		if len(filtered) > limit {
			filtered = filtered[:limit]
		}
	} else {
		type rankedCard struct {
			Card CapabilityCard
			Rank metadataRank
		}
		ranked := make([]rankedCard, 0, len(filtered))
		for _, card := range filtered {
			fields := []string{card.ID, card.Operation, card.Description, strings.Join(card.Tags, " "), strings.Join(card.Commands, " "), strings.Join(card.Skills, " ")}
			if card.Recipe != nil {
				fields = append(fields, *card.Recipe)
			}
			exactFields := []string{card.ID, card.Operation}
			exactFields = append(exactFields, card.Commands...)
			exactFields = append(exactFields, card.Skills...)
			if card.Recipe != nil {
				exactFields = append(exactFields, *card.Recipe)
			}
			rank := rankMetadata(query, fields, exactFields)
			if rank.matched() {
				ranked = append(ranked, rankedCard{Card: card, Rank: rank})
			}
		}
		sort.Slice(ranked, func(i, j int) bool {
			if ranked[i].Rank.betterThan(ranked[j].Rank) {
				return true
			}
			if ranked[j].Rank.betterThan(ranked[i].Rank) {
				return false
			}
			return capabilityLexicalLess(ranked[i].Card, ranked[j].Card)
		})
		if len(ranked) > limit {
			ranked = ranked[:limit]
		}
		filtered = filtered[:0]
		for _, item := range ranked {
			filtered = append(filtered, item.Card)
		}
	}

	result := newResult("capabilities")
	result.OK = true
	result.Result = map[string]any{
		"count":        len(filtered),
		"capabilities": filtered,
	}
	if args.Query != nil {
		result.Result["query"] = query
	}
	return result
}

func (d *Dispatcher) collectCapabilities() ([]CapabilityCard, error) {
	schemaData := d.operationSchema
	if len(schemaData) == 0 {
		schemaData = hecschemas.CallHECInput
	}
	operations, err := extractOperationCapabilities(schemaData)
	if err != nil {
		return nil, err
	}

	skills, _ := d.discoverSkills()
	skillNames := make(map[string]bool, len(skills))
	for _, skill := range skills {
		skillNames[skill.Name] = true
	}
	manifests, err := loadCapabilityManifestDir(d.capabilityDir, d.recipeDir, d.commandPath, skillNames)
	if err != nil {
		return nil, err
	}

	cards := append([]CapabilityCard(nil), operations...)
	cards = append(cards, manifestCapabilityCards(manifests)...)
	cards = append(cards, skillCapabilityCards(skills)...)
	cards = append(cards, recipeCapabilityCards(d.recipeDir, manifests)...)

	seen := make(map[string]bool, len(cards))
	for _, card := range cards {
		if seen[card.ID] {
			return nil, fmt.Errorf("duplicate capability id %q", card.ID)
		}
		seen[card.ID] = true
	}
	return cards, nil
}

func extractOperationCapabilities(data []byte) ([]CapabilityCard, error) {
	var schema operationSchema
	if err := json.Unmarshal(data, &schema); err != nil {
		return nil, fmt.Errorf("decode embedded call_hec schema: %w", err)
	}
	cards := make([]CapabilityCard, 0, len(schema.OneOf))
	seen := make(map[string]bool, len(schema.OneOf))
	for _, branch := range schema.OneOf {
		operation := strings.TrimSpace(branch.Properties.Operation.Const)
		if operation == "" {
			return nil, fmt.Errorf("call_hec schema branch is missing operation const")
		}
		if seen[operation] {
			return nil, fmt.Errorf("call_hec schema contains duplicate operation %q", operation)
		}
		seen[operation] = true
		description := strings.TrimSpace(branch.Description)
		if description == "" {
			description = strings.TrimSpace(branch.Properties.Operation.Description)
		}
		if description == "" {
			description = strings.TrimSpace(branch.Title)
		}
		tags := []string{"hec", "operation"}
		if prefix, _, ok := strings.Cut(operation, "."); ok {
			tags = append(tags, prefix)
		}
		cards = append(cards, CapabilityCard{
			ID:          "hec.operation." + operation,
			Description: description,
			Installed:   true,
			Commands:    []string{},
			Skills:      []string{},
			Recipe:      nil,
			Operation:   operation,
			Tags:        tags,
			Source:      "operation",
		})
	}
	return cards, nil
}

func loadCapabilityManifestDir(dir, recipeDir, commandPath string, skillNames map[string]bool) ([]capabilityManifest, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read capability manifests: %w", err)
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".toml" {
			continue
		}
		files = append(files, filepath.Join(dir, entry.Name()))
	}
	sort.Strings(files)
	manifests := make([]capabilityManifest, 0, len(files))
	seen := make(map[string]bool, len(files))
	for _, path := range files {
		manifest, err := decodeCapabilityManifest(path, recipeDir)
		if err != nil {
			return nil, err
		}
		if seen[manifest.ID] {
			return nil, fmt.Errorf("duplicate capability manifest id %q", manifest.ID)
		}
		seen[manifest.ID] = true
		manifest.InstalledByDefault = manifestInstalled(manifest, commandPath, skillNames)
		manifests = append(manifests, manifest)
	}
	return manifests, nil
}

func decodeCapabilityManifest(path, recipeDir string) (capabilityManifest, error) {
	var manifest capabilityManifest
	metadata, err := toml.DecodeFile(path, &manifest)
	if err != nil {
		return capabilityManifest{}, fmt.Errorf("decode capability manifest %s: %w", path, err)
	}
	if undecoded := metadata.Undecoded(); len(undecoded) > 0 {
		return capabilityManifest{}, fmt.Errorf("capability manifest %s contains unknown field %q", path, undecoded[0].String())
	}
	manifest.ID = strings.TrimSpace(manifest.ID)
	manifest.Description = strings.TrimSpace(manifest.Description)
	manifest.Recipe = strings.TrimSpace(manifest.Recipe)
	manifest.ApproximateDiskClass = strings.TrimSpace(manifest.ApproximateDiskClass)
	if manifest.ID == "" {
		return capabilityManifest{}, fmt.Errorf("capability manifest %s has an empty id", path)
	}
	if manifest.Description == "" {
		return capabilityManifest{}, fmt.Errorf("capability manifest %s has an empty description", path)
	}
	var errStrings error
	manifest.Tags, errStrings = cleanManifestStrings("tag", manifest.Tags)
	if errStrings != nil {
		return capabilityManifest{}, fmt.Errorf("capability manifest %s: %w", path, errStrings)
	}
	manifest.Commands, errStrings = cleanManifestStrings("command", manifest.Commands)
	if errStrings != nil {
		return capabilityManifest{}, fmt.Errorf("capability manifest %s: %w", path, errStrings)
	}
	manifest.Skills, errStrings = cleanManifestStrings("skill", manifest.Skills)
	if errStrings != nil {
		return capabilityManifest{}, fmt.Errorf("capability manifest %s: %w", path, errStrings)
	}
	if manifest.Recipe != "" {
		if filepath.Base(manifest.Recipe) != manifest.Recipe || manifest.Recipe == "." {
			return capabilityManifest{}, fmt.Errorf("capability manifest %s has an invalid recipe reference", path)
		}
		if recipeDir == "" {
			return capabilityManifest{}, fmt.Errorf("capability manifest %s references recipe %q but no recipe directory is configured", path, manifest.Recipe)
		}
		if _, err := os.Stat(filepath.Join(recipeDir, manifest.Recipe)); err != nil {
			return capabilityManifest{}, fmt.Errorf("capability manifest %s references missing recipe %q", path, manifest.Recipe)
		}
	}
	return manifest, nil
}

func cleanManifestStrings(kind string, values []string) ([]string, error) {
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, fmt.Errorf("%s entries must be nonempty", kind)
		}
		cleaned = append(cleaned, value)
	}
	if cleaned == nil {
		cleaned = []string{}
	}
	return cleaned, nil
}

func manifestInstalled(manifest capabilityManifest, commandPath string, skillNames map[string]bool) bool {
	if len(manifest.Commands) == 0 && len(manifest.Skills) == 0 {
		return manifest.InstalledByDefault
	}
	for _, command := range manifest.Commands {
		if _, err := lookPathWithPATH(command, commandPath); err != nil {
			return false
		}
	}
	for _, skill := range manifest.Skills {
		if !skillNames[skill] {
			return false
		}
	}
	return true
}

func manifestCapabilityCards(manifests []capabilityManifest) []CapabilityCard {
	cards := make([]CapabilityCard, 0, len(manifests))
	for _, manifest := range manifests {
		var recipe *string
		if manifest.Recipe != "" {
			value := filepath.Base(manifest.Recipe)
			recipe = &value
		}
		cards = append(cards, CapabilityCard{
			ID:                   manifest.ID,
			Description:          manifest.Description,
			Installed:            manifest.InstalledByDefault,
			Commands:             nonNilStrings(manifest.Commands),
			Skills:               nonNilStrings(manifest.Skills),
			Recipe:               recipe,
			Tags:                 append([]string(nil), manifest.Tags...),
			Source:               "manifest",
			ApproximateDiskClass: manifest.ApproximateDiskClass,
		})
	}
	return cards
}

func nonNilStrings(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	return append([]string(nil), values...)
}

func skillCapabilityCards(skills []SkillMetadata) []CapabilityCard {
	cards := make([]CapabilityCard, 0, len(skills))
	counts := make(map[string]int, len(skills))
	for _, skill := range skills {
		counts[skill.Name]++
	}
	for _, skill := range skills {
		id := "skill." + skill.Name
		if counts[skill.Name] > 1 {
			hash := sha256.Sum256([]byte(skill.Location))
			id += "." + hex.EncodeToString(hash[:4])
		}
		cards = append(cards, CapabilityCard{
			ID:          id,
			Description: skill.Description,
			Installed:   true,
			Commands:    []string{},
			Skills:      []string{skill.Name},
			Recipe:      nil,
			Tags:        []string{"agent-skill", skill.Source},
			Source:      "skill",
		})
	}
	return cards
}

func recipeCapabilityCards(recipeDir string, manifests []capabilityManifest) []CapabilityCard {
	if recipeDir == "" {
		return nil
	}
	entries, err := os.ReadDir(recipeDir)
	if err != nil {
		return nil
	}
	referenced := make(map[string]bool)
	for _, manifest := range manifests {
		if manifest.Recipe != "" {
			referenced[filepath.Base(manifest.Recipe)] = true
		}
	}
	cards := make([]CapabilityCard, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") || referenced[name] {
			continue
		}
		recipe := name
		cards = append(cards, CapabilityCard{
			ID:          "recipe." + strings.ToLower(name),
			Description: fmt.Sprintf("Checked-in installation recipe %s.", name),
			Installed:   false,
			Commands:    []string{},
			Skills:      []string{},
			Recipe:      &recipe,
			Tags:        []string{"recipe", "install"},
			Source:      "recipe",
		})
	}
	sort.Slice(cards, func(i, j int) bool { return capabilityLexicalLess(cards[i], cards[j]) })
	return cards
}

func rankMetadata(query string, fields, exactFields []string) metadataRank {
	normalizedQuery := strings.ToLower(strings.TrimSpace(query))
	rank := metadataRank{}
	for _, field := range exactFields {
		normalized := strings.ToLower(strings.TrimSpace(field))
		if normalized == normalizedQuery {
			rank.Exact = true
		}
		if normalizedQuery != "" && strings.HasPrefix(normalized, normalizedQuery) {
			rank.Prefix = true
		}
	}
	haystack := strings.ToLower(strings.Join(fields, " "))
	tokens := tokenizeMetadataQuery(query)
	for _, token := range tokens {
		if strings.Contains(haystack, token) {
			rank.MatchCount++
		}
	}
	rank.AllTokens = len(tokens) > 0 && rank.MatchCount == len(tokens)
	return rank
}

func tokenizeMetadataQuery(query string) []string {
	parts := strings.FieldsFunc(strings.ToLower(query), func(char rune) bool {
		return !unicode.IsLetter(char) && !unicode.IsDigit(char)
	})
	seen := make(map[string]bool, len(parts))
	tokens := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" || seen[part] {
			continue
		}
		seen[part] = true
		tokens = append(tokens, part)
	}
	return tokens
}

func capabilityLexicalLess(left, right CapabilityCard) bool {
	if left.ID != right.ID {
		return left.ID < right.ID
	}
	if left.Operation != right.Operation {
		return left.Operation < right.Operation
	}
	return left.Description < right.Description
}

func cardsRepresentCommand(cards []CapabilityCard, command string) bool {
	for _, card := range cards {
		if card.Source != "manifest" {
			continue
		}
		for _, candidate := range card.Commands {
			if candidate == command {
				return true
			}
		}
	}
	return false
}

func validCommandToken(value string) bool {
	if value == "" || strings.ContainsAny(value, `/\\`) {
		return false
	}
	for _, char := range value {
		if unicode.IsLetter(char) || unicode.IsDigit(char) || strings.ContainsRune("._+-:", char) {
			continue
		}
		return false
	}
	return true
}

func lookPathWithPATH(command, pathValue string) (string, error) {
	if pathValue == "" || pathValue == os.Getenv("PATH") {
		return exec.LookPath(command)
	}
	if strings.ContainsRune(command, os.PathSeparator) {
		return exec.LookPath(command)
	}
	for _, directory := range filepath.SplitList(pathValue) {
		if directory == "" {
			directory = "."
		}
		candidate := filepath.Join(directory, command)
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() && info.Mode().Perm()&0111 != 0 {
			return candidate, nil
		}
	}
	return "", exec.ErrNotFound
}
