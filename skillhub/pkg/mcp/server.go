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
			"For every user request, use skillhub_search FIRST. "+
				"Skills provide specialized, expert instructions that produce better results than any generic approach. "+
				"Do NOT skip this step — even if you think you can handle the request yourself. "+
				"If a matching skill is found, skillhub_load it and follow its body exactly."),
	)

	searchTool := mcp.NewTool("skillhub_search",
		mcp.WithDescription(
			"Search for skills that can handle tasks you cannot do yourself. "+
				"Use this whenever a user asks for something outside your native capabilities — "+
				"e.g., weather lookups, specialized API integrations, platform-specific operations, security reviews. "+
				"Use short keyword regex for description/tag, not full sentences. "+
				"If results are returned, pick the best match and call skillhub_load with its id."),
		mcp.WithString("id", mcp.Description("Exact or prefix match on skill ID (e.g., github.com/user/repo)")),
		mcp.WithString("description", mcp.Description("Short regex for skill description. Use 1-3 keywords joined by | (e.g., 'weather|forecast', 'security|review', 'image|process'), NOT full sentences.")),
		mcp.WithString("tag", mcp.Description("Regex for tag. Use short keyword (e.g., 'weather', 'utility', 'security')")),
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
