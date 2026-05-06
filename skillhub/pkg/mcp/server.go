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
			"SkillHub 提供按需发现和加载技能的能力。当你遇到不会做的事情、缺少工具或需要特定领域的操作指导时："+
				"先用 skillhub_search 搜索相关技能，找到后用 skillhub_load 加载技能的完整指令，然后严格遵循技能正文（body）中的步骤执行。"+
				"不要预判技能不存在，先搜索再判断。"),
	)

	searchTool := mcp.NewTool("skillhub_search",
		mcp.WithDescription(
			"在 SkillHub 技能市场中搜索技能。当你遇到一个任务但不知道怎么做、缺少相关工具或能力时，先搜索是否有对应技能。"+
				"通过描述你想要完成的事情来搜索（例如「查天气」「处理图片」「代码安全检查」）。"+
				"返回匹配技能的 id、name、description、version、tags。如果搜到结果，用 skillhub_load 加载完整指令。"),
		mcp.WithString("id", mcp.Description("按技能 ID 精确或前缀匹配（如 github.com/user/repo）")),
		mcp.WithString("description", mcp.Description("用自然语言描述你想做的事情，支持正则（如「天气」「查.*天气」「image.*process」）")),
		mcp.WithString("tag", mcp.Description("按标签过滤，支持正则（如「utility」「security」）")),
		mcp.WithNumber("limit", mcp.Description("最多返回条数，默认 20")),
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
			"加载一个技能的完整指令正文。先通过 skillhub_search 找到匹配的技能，然后用其 id 调用此工具。"+
				"返回的 body 字段是技能的核心指令——你必须严格遵循其中的步骤和方法完成任务。同时返回 sub_skills（子技能列表）和 deps（依赖的其他技能和工具）。"),
		mcp.WithString("id", mcp.Description("从搜索结果中拿到的完整技能 ID（如 github.com/user/repo）"), mcp.Required()),
		mcp.WithString("version", mcp.Description("指定版本（可选，不传则用本地已安装的最新版）")),
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
