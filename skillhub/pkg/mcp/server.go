package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"skillhub/pkg/types"
)

func NewServer(toolImpl types.SkillHubTools) *server.MCPServer {
	s := server.NewMCPServer("skillhub", "1.0.0",
		server.WithInstructions("Search and load skills from SkillHub"),
	)

	searchTool := mcp.NewTool("skillhub_search",
		mcp.WithDescription("Search for skills in the registry"),
		mcp.WithString("id", mcp.Description("Skill ID (exact or prefix match)")),
		mcp.WithString("description", mcp.Description("Regex pattern for description")),
		mcp.WithString("tag", mcp.Description("Regex pattern for tag")),
		mcp.WithNumber("limit", mcp.Description("Maximum results")),
	)

	s.AddTool(searchTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id := mcp.ParseString(req, "id", "")
		description := mcp.ParseString(req, "description", "")
		tag := mcp.ParseString(req, "tag", "")
		limit := mcp.ParseInt(req, "limit", 0)

		searchReq := types.SearchRequest{
			ID:          id,
			Description: description,
			Tag:         tag,
			Limit:       limit,
		}

		results, err := toolImpl.Search(searchReq)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("search failed: %v", err)), nil
		}

		data, _ := json.MarshalIndent(results, "", "  ")
		return mcp.NewToolResultText(string(data)), nil
	})

	loadTool := mcp.NewTool("skillhub_load",
		mcp.WithDescription("Load a skill by ID"),
		mcp.WithString("id", mcp.Description("Full skill ID to load"), mcp.Required()),
		mcp.WithString("version", mcp.Description("Version constraint (optional)")),
	)

	s.AddTool(loadTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id := mcp.ParseString(req, "id", "")
		version := mcp.ParseString(req, "version", "")

		loadReq := types.LoadRequest{
			ID:      id,
			Version: version,
		}

		skill, err := toolImpl.Load(loadReq)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("load failed: %v", err)), nil
		}

		data, _ := json.MarshalIndent(skill, "", "  ")
		return mcp.NewToolResultText(string(data)), nil
	})

	return s
}
