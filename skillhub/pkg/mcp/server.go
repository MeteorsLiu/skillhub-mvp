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
			"SkillHub helps agents find skills that teach them how to do EVERYTHING."),
	)

	searchTool := mcp.NewTool("search",
		mcp.WithDescription(
			"Search SkillHub for skills that teach the agent how to do tasks it may not know how to do.\n"+
				"\n"+
				"SkillHub helps agents find skills that teach them how to do EVERYTHING: use tools, follow workflows, call integrations, handle files, work in a domain, or match a style.\n"+
				"\n"+
				"Workflow:\n"+
				"\n"+
				"1. Before using web_search, web_fetch, browser, shell, file tools, or code execution, first ask: Would a specialized skill make this task easier, safer, more complete, or more accurate?\n"+
				"2. Web search finds information. SkillHub teaches you how to do the task. Use SkillHub first when the task is more than simply retrieving one fact.\n"+
				"3. When a user request contains multiple goals, split it into sub-tasks, then search and load the most relevant skill for each sub-task.\n"+
				"4. Search in English: use tag for the broad area and description for the specific user intent.\n"+
				"5. If a relevant skill is found, load it and follow it.\n"+
				"6. If no relevant skill is found, continue normally.\n"+
				"\n"+
				"Usage notes:\n"+
				"  - Use skillhub__search before web_search, web_fetch, browser, shell, file tools, or code execution when the task is more than simply retrieving one fact\n"+
				"  - If a relevant skill is found, call skillhub__load and follow it\n"+
				"  - If no relevant skill is found, continue normally"),
		mcp.WithString("id", mcp.Description("Exact or prefix match on skill ID")),
		mcp.WithString("description", mcp.Description("English regex pattern for the specific user intent, matched against skill name and description")),
		mcp.WithString("tag", mcp.Description("English broad skill area hint. Not regex")),
		mcp.WithNumber("limit", mcp.Description("Max results (default 100, capped at 100)")),
		mcp.WithNumber("offset", mcp.Description("0-based pagination offset. For the next page, use the last returned offset plus 1")),
	)

	s.AddTool(searchTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id := mcp.ParseString(req, "id", "")
		description := mcp.ParseString(req, "description", "")
		tag := mcp.ParseString(req, "tag", "")
		limit := mcp.ParseInt(req, "limit", 0)
		offset := mcp.ParseInt(req, "offset", 0)

		searchReq := types.SearchRequest{
			ID:          id,
			Description: description,
			Tag:         tag,
			Limit:       limit,
			Offset:      offset,
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
