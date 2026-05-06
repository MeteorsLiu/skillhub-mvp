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
		server.WithInstructions(
			"Skills are installable packages that extend your capabilities with expert instructions. "+
				"When the user asks 'how to do X', 'can you help with X', 'is there a skill for X', "+
				"or any request where you lack built-in tools or specialized knowledge — search SkillHub first. "+
				"If a matching skill exists, load it and follow its body exactly; it will tell you how to complete the task."),
	)

	searchTool := mcp.NewTool("skillhub_search",
		mcp.WithDescription(
			"Search for installable skills. Use this when the user asks: "+
				"'how to do X', 'can you do X', 'find a skill for X', "+
				"'I need a tool for X', 'help me with X', or any request outside your native capabilities. "+
				"Search with short keywords joined by | (e.g., 'stock|上证', 'weather|天气'). "+
				"If results found, call skillhub_load to get step-by-step instructions."),
		mcp.WithString("id", mcp.Description("Exact or prefix match on skill ID")),
		mcp.WithString("description", mcp.Description("Keywords joined by | (e.g., 'stock|上证', 'weather|forecast'), NOT full sentences")),
		mcp.WithString("tag", mcp.Description("Regex for tag (e.g., 'finance', 'social')")),
		mcp.WithNumber("limit", mcp.Description("Max results (default 20)")),
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
		mcp.WithDescription(
			"Load the full instructions for a skill found via skillhub_search. "+
				"The returned 'body' field contains the step-by-step instructions you must follow to complete the user's task. "+
				"Also returns 'sub_skills' (nested sub-skills) and 'deps' (required tools and dependent skills)."),
		mcp.WithString("id", mcp.Description("The skill id from search results"), mcp.Required()),
		mcp.WithString("version", mcp.Description("Specific version (optional; defaults to latest installed)")),
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
