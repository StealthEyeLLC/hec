package hec

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPPublishesWorkspaceCapabilitiesWithoutNewOperations(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	workspaceRoot := filepath.Join(root, "workspaces")
	workspace := filepath.Join(workspaceRoot, "mcp-project")
	repository := filepath.Join(workspace, "main")
	createTestWorktree(t, repository)
	writeWorkspaceManifest(t, workspace, `description = "MCP workspace discovery test"
notes = "Metadata-only workspace card"
default_cwd = "main"
repository = "main"
tags = ["mcp-workspace-test"]
skills = ["hinted-workspace-skill"]

[env]
MCP_WORKSPACE_SECRET = "must-not-appear"
`)
	workspaceSkills := filepath.Join(workspace, ".hec", "skills")
	writeTestSkill(t, workspaceSkills, "local", "local-workspace-skill", "Workspace-local test Skill", "body is disclosed only by skill.read")

	dispatcher := NewDispatcher()
	dispatcher.workspaceRoot = workspaceRoot
	dispatcher.skillRoots = nil
	dispatcher.capabilityDir = filepath.Join(root, "capabilities")
	dispatcher.recipeDir = filepath.Join(root, "recipes")
	for _, path := range []string{dispatcher.capabilityDir, dispatcher.recipeDir} {
		if err := os.MkdirAll(path, 0755); err != nil {
			t.Fatal(err)
		}
	}

	server := NewMCPServer(dispatcher)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "hec-workspace-test", Version: "1.0.0"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer clientSession.Close()

	listed, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(listed.Tools) != 1 {
		t.Fatalf("tool count = %d, want 1", len(listed.Tools))
	}
	tool := listed.Tools[0]
	if tool.Name != "call_hec" || tool.Title != "HEC" || tool.Description != "Operate the HEC workstation." {
		t.Fatalf("tool metadata = %#v", tool)
	}
	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("schema type = %T", tool.InputSchema)
	}
	branches, ok := schema["oneOf"].([]any)
	if !ok || len(branches) != 38 {
		t.Fatalf("schema branches = %#v", schema["oneOf"])
	}
	foundCapabilities := false
	for _, raw := range branches {
		branch := raw.(map[string]any)
		properties := branch["properties"].(map[string]any)
		operation := properties["operation"].(map[string]any)["const"].(string)
		for _, forbidden := range []string{"workspace.", "repository.", "delivery.", "worktree."} {
			if strings.HasPrefix(operation, forbidden) {
				t.Fatalf("forbidden operation %q", operation)
			}
		}
		if operation != "capabilities" {
			continue
		}
		foundCapabilities = true
		args := properties["args"].(map[string]any)
		if args["additionalProperties"] != false {
			t.Fatalf("capabilities args allow unknown fields: %#v", args)
		}
		argsProperties := args["properties"].(map[string]any)
		if len(argsProperties) != 3 || argsProperties["query"] == nil || argsProperties["limit"] == nil || argsProperties["include_missing"] == nil {
			t.Fatalf("capabilities input changed: %#v", argsProperties)
		}
	}
	if !foundCapabilities {
		t.Fatal("capabilities branch missing")
	}

	called, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "call_hec",
		Arguments: map[string]any{
			"operation": "capabilities",
			"args": map[string]any{
				"query": "workspace.mcp-project",
				"limit": 10,
			},
		},
	})
	if err != nil {
		t.Fatalf("call capabilities: %v", err)
	}
	if called.IsError {
		t.Fatalf("capabilities error = %#v", called)
	}
	structured, ok := called.StructuredContent.(map[string]any)
	if !ok || structured["ok"] != true || structured["operation"] != "capabilities" {
		t.Fatalf("structured content = %#v", called.StructuredContent)
	}
	result := structured["result"].(map[string]any)
	cards := result["capabilities"].([]any)
	var card map[string]any
	for _, raw := range cards {
		candidate := raw.(map[string]any)
		if candidate["id"] == "workspace.mcp-project" {
			card = candidate
			break
		}
	}
	if card == nil {
		t.Fatalf("workspace card missing: %#v", cards)
	}
	if card["description"] != "MCP workspace discovery test" || card["installed"] != true || card["source"] != "workspace" || card["name"] != "mcp-project" || card["location"] != workspace || card["repository"] != repository || card["repository_exists"] != true || card["repository_kind"] != "worktree" || card["default_cwd"] != repository || card["recipe"] != nil {
		t.Fatalf("workspace card = %#v", card)
	}
	commands := card["commands"].([]any)
	skills := card["skills"].([]any)
	environmentKeys := card["environment_keys"].([]any)
	if len(commands) != 0 || len(skills) != 2 || len(environmentKeys) != 1 || environmentKeys[0] != "MCP_WORKSPACE_SECRET" {
		t.Fatalf("workspace arrays = commands:%#v skills:%#v env:%#v", commands, skills, environmentKeys)
	}
	if strings.Contains(toJSONString(t, called.StructuredContent), "must-not-appear") {
		t.Fatal("workspace environment value leaked through capabilities")
	}
}

func toJSONString(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
