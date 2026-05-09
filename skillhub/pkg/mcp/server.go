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
			"SkillHub provides skills that change how a request is performed."),
	)

	searchTool := mcp.NewTool("search",
		mcp.WithDescription(
			"SkillHub provides skills that change how a request is performed.\n"+
				"\n"+
				"For each non-trivial request, check whether the requested information, action, output, or behavior may depend on an existing skill.\n"+
				"\n"+
				"CRITICAL: If it may depend on a skill, MUST call skillhub__search FIRST before web_search, web_fetch, browsing, or other regular tools.\n"+
				"\n"+
				"Search in English for the user's actual intent, not just keywords.\n"+
				"\n"+
				"Usage notes:\n"+
				"  - If a matching skill is relevant, use skillhub__load and follow it\n"+
				"  - If no relevant skill exists, proceed normally\n"+
				"  - Skip only direct answers, pure conversation, ordinary explanations, generic research, generic coding, and requests that need clarification first"),
		mcp.WithString("id", mcp.Description("Exact or prefix match on skill ID")),
		mcp.WithString("description", mcp.Description("English search query for the user's actual intent")),
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

	loadTool := mcp.NewTool("load",
		mcp.WithDescription(
			"Load the full instructions for a skill found via skillhub__search. "+
				"The returned 'body' field contains step-by-step instructions you MUST follow — "+
				"do not substitute with regular tools or your own judgment when the skill is relevant. "+
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
