# Discovery Center Design

Date: 2026-04-30
Status: Brainstormed and approved

## Architecture

```
skillhub CLI                          discovery HTTP (:8399)
  │                                      │
  │  serve  → MCP stdio                  POST /v1/register ──→ exec skillhub fetch
  │  search → HTTP client ───────────→   POST /v1/search        → GORM
  │  load   → $SKILLHUB_HOME            GET  /health
  │  fetch  → git clone + parser
```

- discovery 是独立 HTTP 服务，不内嵌在 skillhub 中
- skillhub 通过 HTTP 调用 discovery 做 search 和 register
- security 和 LLM 评审全部在 discovery 侧
- skillhub 只保留 MCP server、fetch、本地安装加载

## `skillhub fetch` 命令

```
skillhub fetch <id>@<version> <output-dir>
```

拉取远程 skill 到本地目录，输出 JSON 元数据到 stdout。

stdout:
```json
{
  "id": "github.com/acme/clawhub/social/publish-post",
  "name": "发布小红书图文",
  "description": "发布小红书图文内容",
  "version": "v1.4.2",
  "tags": ["social", "xiaohongshu"],
  "deps": {"tools": ["ffmpeg"], "skills": [{"id": "...", "version": "..."}]}
}
```

output-dir:
```
output-dir/
├── SKILL.md
└── skills/
    ├── draft-post/SKILL.md
    └── publish-final/SKILL.md
```

## Discovery HTTP API

| Method | Path | Body | Response |
|--------|------|------|----------|
| POST | /v1/search | SearchRequest | {results: []SkillSummary} |
| POST | /v1/register | RegisterRequest | {id, approved} |
| GET | /health | — | {status: "ok"} |

Register 流程：
1. `exec.Command("skillhub", "fetch", id+"@"+version, tmpDir)`
2. RuleScanner.ScanDir(tmpDir) — 文件内容规则检查
3. VirusTotal (可选) — API key 配置时启用
4. LLMReviewer.Review(skill, body) — 语义安全审查
5. 全部通过 → GORM 入库 (approved=true) → 200
6. 任一失败 → 422 + 原因

## 安全扫描

### RuleScanner
从 skillhub pkg/security 迁移过来，规则不变：base64+exec、curl/wget 到 IP、硬编码密钥、reverse shell、rm -rf 等。

### VirusTotalScanner
可选，需要 VT_API_KEY。无 key 时跳过。

## LLM 评审

```go
type ReviewResult struct {
    Passed bool
    Reason string
}

type LLMReviewer interface {
    Review(ctx context.Context, skill SkillSummary, body string) (*ReviewResult, error)
}
```

两个实现：
- **OpenAIReviewer** — OpenAI 兼容格式（DeepSeek、通义千问、Gemini 等）
- **AnthropicReviewer** — Anthropic Claude 格式

通过环境变量选择：
```bash
LLM_PROVIDER=openai              # 默认
OPENAI_BASE_URL=https://api.deepseek.com/v1
OPENAI_MODEL=deepseek-v4-flash
# 或
LLM_PROVIDER=anthropic
ANTHROPIC_MODEL=claude-sonnet-4-20250514
```

## GORM

替换 pgx，使用 `gorm.io/gorm` + `gorm.io/driver/postgres`。

Model:
```go
type skillModel struct {
    ID          string    `gorm:"primaryKey"`
    Name        string
    Description string
    Version     string
    Tags        string    `gorm:"type:text[]"`
    Approved    bool
    Source      string
    CreatedAt   time.Time
    UpdatedAt   time.Time
}
```

## 目录结构

```
skills/
├── discovery/
│   ├── cmd/discovery/main.go    — serve
│   ├── discovery.go             — GORM + 核心逻辑
│   ├── server.go                — HTTP handler
│   ├── llm.go                   — LLMReviewer interface
│   ├── llm_openai.go            — DeepSeek/OpenAI
│   ├── llm_anthropic.go         — Claude
│   ├── scanner.go               — RuleScanner + VirusTotal
│   ├── go.mod
│   └── go.sum
├── skillhub/
│   ├── cmd/skillhub/main.go     — serve, search, load, fetch
│   ├── pkg/{types,vcs,parser,installer,loader,mcp}
│   ├── AGENTS.md
│   └── go.mod
├── doc/
└── docs/
```

## 环境变量

### discovery

| 变量 | 默认 | 说明 |
|------|------|------|
| DISCOVERY_PORT | 8399 | HTTP 端口 |
| DATABASE_URL | — | PostgreSQL 连接串 |
| LLM_PROVIDER | openai | openai 或 anthropic |
| OPENAI_API_KEY | — | OpenAI 兼容 API key |
| OPENAI_BASE_URL | https://api.deepseek.com/v1 | 可换其他兼容服务 |
| OPENAI_MODEL | deepseek-v4-flash | 模型名 |
| ANTHROPIC_API_KEY | — | Claude API key |
| ANTHROPIC_MODEL | claude-sonnet-4-20250514 | 模型名 |
| VT_API_KEY | — | VirusTotal API key |

## 实现顺序

1. `skillhub fetch` 命令（复用 vcs + parser）
2. discovery 接入 GORM
3. discovery HTTP server（search + register + health）
4. discovery LLM reviewer（OpenAI + Anthropic）
5. discovery security scanner（从 skillhub 迁移）
6. discovery CLI（serve）
7. skillhub search/register 改为 HTTP 调用 discovery
8. 清理 skillhub pkg/security
