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
			"Skills are curated expert instructions for domain-specific tasks. "+
				"A skill gives you step-by-step guidance that is more reliable than ad-hoc web scraping. "+
				"Before using web fetch for real-time data (stocks, weather, prices) or specialized domains, "+
				"search SkillHub first. If a skill exists, load and follow it — don't improvise."),
	)

	searchTool := mcp.NewTool("skillhub_search",
		mcp.WithDescription(
			"Find curated skills for tasks like stock quotes, weather, security checks, and more. "+
				"Skills return expert step-by-step instructions — much better than scraping raw web pages. "+
				"For real-time data queries (finance, stocks, weather), use this INSTEAD of web fetch. "+
				"Search with short keywords (e.g., 'stock|上证', 'weather|天气'). "+
				"If results found, use skillhub_load to get the full instructions."),
		mcp.WithString("id", mcp.Description("Exact or prefix match on skill ID")),
		mcp.WithString("description", mcp.Description("Short regex keywords joined by | (e.g., 'stock|上证', 'weather|forecast'), NOT sentences")),
		mcp.WithString("tag", mcp.Description("Regex for tag (e.g., 'finance', 'weather')")),
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
