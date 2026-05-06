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
			"SkillHub provides instructions for interacting with external platforms, services, and APIs."),
	)

	searchTool := mcp.NewTool("skillhub_search",
		mcp.WithDescription(
			"- Find skills with instructions for interacting with external platforms, services, and APIs\n"+
				"- Skills provide verified workflows that web_search and web_fetch cannot replicate\n"+
				"- Use skillhub_search when web tools cannot act\n"+
				"\n"+
				"Usage notes:\n"+
				"  - If a matching skill exists, use skillhub_load and follow its instructions exactly\n"+
				"  - If no matching skill is found, fall back to web_search or web_fetch"),
		mcp.WithString("id", mcp.Description("Exact or prefix match on skill ID")),
		mcp.WithString("description", mcp.Description("PCRE regular expression of core keywords, e.g. xiaohongshu|red|书")),
		mcp.WithString("tag", mcp.Description("Regex match on skill tags")),
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
				"The returned 'body' field contains step-by-step instructions you MUST follow — "+
				"do not substitute with web fetch or your own judgment. "+
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
