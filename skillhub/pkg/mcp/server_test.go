package mcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	skillhubmcp "skillhub/pkg/mcp"
	"skillhub/pkg/types"
)

type mockTools struct {
	searchFunc func(types.SearchRequest) ([]types.SkillSummary, error)
	loadFunc   func(types.LoadRequest) (*types.Skill, error)
}

func (m *mockTools) Search(req types.SearchRequest) ([]types.SkillSummary, error) {
	if m.searchFunc == nil {
		return nil, errors.New("unexpected search call")
	}
	return m.searchFunc(req)
}

func (m *mockTools) Load(req types.LoadRequest) (*types.Skill, error) {
	if m.loadFunc == nil {
		return nil, errors.New("unexpected load call")
	}
	return m.loadFunc(req)
}

func callTool(t *testing.T, srv *server.MCPServer, reqJSON string) *mcp.CallToolResult {
	t.Helper()
	raw := json.RawMessage(reqJSON)
	resp := srv.HandleMessage(context.Background(), raw)
	rpcResp, ok := resp.(mcp.JSONRPCResponse)
	if !ok {
		var rpcErr mcp.JSONRPCError
		if e, ok := resp.(mcp.JSONRPCError); ok {
			rpcErr = e
		}
		t.Fatalf("expected JSONRPCResponse, got error: %+v", rpcErr)
	}
	data, _ := json.Marshal(rpcResp.Result)
	var result mcp.CallToolResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal CallToolResult: %v", err)
	}
	return &result
}

func listTools(t *testing.T, srv *server.MCPServer) []mcp.Tool {
	t.Helper()
	raw := json.RawMessage(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	resp := srv.HandleMessage(context.Background(), raw)
	rpcResp, ok := resp.(mcp.JSONRPCResponse)
	if !ok {
		t.Fatalf("expected JSONRPCResponse for tools/list")
	}
	data, _ := json.Marshal(rpcResp.Result)
	var list struct {
		Tools []mcp.Tool `json:"tools"`
	}
	if err := json.Unmarshal(data, &list); err != nil {
		t.Fatalf("unmarshal tools list: %v", err)
	}
	return list.Tools
}

func TestToolsList(t *testing.T) {
	srv := skillhubmcp.NewServer(&mockTools{})
	tools := listTools(t, srv)

	foundSearch := false
	foundLoad := false
	for _, tool := range tools {
		if tool.Name == "search" {
			foundSearch = true
		}
		if tool.Name == "load" {
			foundLoad = true
		}
	}
	if !foundSearch {
		t.Error("missing search")
	}
	if !foundLoad {
		t.Error("missing load")
	}
}

func TestSearchToolDescribesTagAndDescriptionSemantics(t *testing.T) {
	srv := skillhubmcp.NewServer(&mockTools{})
	tools := listTools(t, srv)

	var search *mcp.Tool
	for i := range tools {
		if tools[i].Name == "search" {
			search = &tools[i]
			break
		}
	}
	if search == nil {
		t.Fatal("missing search")
	}

	data, err := json.Marshal(search)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		"Search skillhub__search in English: tag is the broad skill area; description is the specific user intent",
		"English broad skill area hint. Not regex",
		"English regex pattern for the specific user intent",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("tool metadata missing %q in %s", want, text)
		}
	}
}

func TestSearch(t *testing.T) {
	srv := skillhubmcp.NewServer(&mockTools{
		searchFunc: func(req types.SearchRequest) ([]types.SkillSummary, error) {
			if req.ID != "test" {
				t.Errorf("expected id 'test', got %q", req.ID)
			}
			if req.Limit != 5 {
				t.Errorf("expected limit 5, got %d", req.Limit)
			}
			if req.Offset != 10 {
				t.Errorf("expected offset 10, got %d", req.Offset)
			}
			offset := 10
			return []types.SkillSummary{{ID: "test", Name: "Test", Version: "v1.0.0", Offset: &offset}}, nil
		},
	})
	result := callTool(t, srv, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"search","arguments":{"id":"test","limit":5,"offset":10}}}`)
	if result.IsError {
		t.Fatal("tool returned error")
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content, got %d", len(result.Content))
	}
	text, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	var skills []types.SkillSummary
	if err := json.Unmarshal([]byte(text.Text), &skills); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(skills) != 1 || skills[0].ID != "test" {
		t.Errorf("unexpected skills: %+v", skills)
	}
	if skills[0].Offset == nil || *skills[0].Offset != 10 {
		t.Fatalf("expected offset 10, got %+v", skills[0].Offset)
	}
}

func TestLoad(t *testing.T) {
	srv := skillhubmcp.NewServer(&mockTools{
		loadFunc: func(req types.LoadRequest) (*types.Skill, error) {
			if req.ID != "test" {
				t.Errorf("expected id 'test', got %q", req.ID)
			}
			return &types.Skill{ID: "test", Name: "Test", Version: "v1.0.0", Body: "content"}, nil
		},
	})
	result := callTool(t, srv, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"load","arguments":{"id":"test"}}}`)
	if result.IsError {
		t.Fatal("tool returned error")
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content, got %d", len(result.Content))
	}
	text, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	var skill types.Skill
	if err := json.Unmarshal([]byte(text.Text), &skill); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if skill.ID != "test" || skill.Name != "Test" {
		t.Errorf("unexpected skill: %+v", skill)
	}
}

func TestSearchError(t *testing.T) {
	srv := skillhubmcp.NewServer(&mockTools{
		searchFunc: func(req types.SearchRequest) ([]types.SkillSummary, error) {
			return nil, errors.New("search failed")
		},
	})
	result := callTool(t, srv, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"search","arguments":{"id":"x"}}}`)
	if !result.IsError {
		t.Fatal("expected error result")
	}
}

func TestLoadError(t *testing.T) {
	srv := skillhubmcp.NewServer(&mockTools{
		loadFunc: func(req types.LoadRequest) (*types.Skill, error) {
			return nil, errors.New("load failed")
		},
	})
	result := callTool(t, srv, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"load","arguments":{"id":"x"}}}`)
	if !result.IsError {
		t.Fatal("expected error result")
	}
}
