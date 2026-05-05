# Discovery Center Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use subagent-driven-development (recommended).

**Goal:** Build discovery as a standalone HTTP service with GORM, LLM review (DeepSeek/Claude), security scanning, and `skillhub fetch` command.

**Architecture:** discovery HTTP (:8399) — register triggers `skillhub fetch` subprocess, then runs RuleScanner + VirusTotal/ClamAV + LLM review, stores to PostgreSQL via GORM.

**Tech Stack:** Go 1.24, `gorm.io/gorm`, `gorm.io/driver/postgres`, DeepSeek API / Anthropic API

---

### Task 1: `skillhub fetch` command

**Files:**
- Modify: `skillhub/cmd/skillhub/main.go` — add `fetch` subcommand
- Test: manual via `go build`

- [ ] **Add fetch handler**

In `main.go`, add a new case to the command switch:
```go
case "fetch":
    cmdFetch(args[1:])
```

Implement `cmdFetch`:
```go
func cmdFetch(args []string) {
    if len(args) < 2 {
        fmt.Fprintln(os.Stderr, "Usage: skillhub fetch <id>@<version> <output-dir>")
        os.Exit(1)
    }
    idVersion := args[0]
    outputDir := args[1]

    at := strings.LastIndex(idVersion, "@")
    if at < 0 {
        fmt.Fprintln(os.Stderr, "format: <id>@<version>")
        os.Exit(1)
    }
    id := idVersion[:at]
    version := idVersion[at+1:]

    repoURL := vcs.RepoURL(id)
    subDir := vcs.SubdirPath(id)

    if version == "" || version == "latest" {
        resolved, err := vcs.ResolveVersion(repoURL, "")
        if err != nil {
            fmt.Fprintf(os.Stderr, "resolving version: %v\n", err)
            os.Exit(1)
        }
        version = resolved
    }

    if err := vcs.Clone(repoURL, version, subDir, outputDir); err != nil {
        fmt.Fprintf(os.Stderr, "clone failed: %v\n", err)
        os.Exit(1)
    }

    result, err := parser.ParseRoot(outputDir)
    if err != nil {
        fmt.Fprintf(os.Stderr, "parse failed: %v\n", err)
        os.Exit(1)
    }

    out := map[string]any{
        "id":          result.ID,
        "name":        result.Name,
        "description": result.Description,
        "version":     version,
        "tags":        result.Tags,
        "deps":        result.Deps,
    }
    data, _ := json.MarshalIndent(out, "", "  ")
    fmt.Println(string(data))
}
```

Add imports for `strings`, `"skillhub/pkg/vcs"`, `"skillhub/pkg/parser"`.

- [ ] **Verify: `go build ./cmd/skillhub/` passes**

---

### Task 2: Discovery — GORM migration

**Files:**
- Modify: `discovery/go.mod` — add gorm dependencies
- Modify: `discovery/discovery.go` — replace pgx with GORM

- [ ] **Add GORM deps**

```bash
cd discovery && go get gorm.io/gorm gorm.io/driver/postgres
```

- [ ] **Rewrite discovery.go with GORM**

Replace `*pgxpool.Pool` with `*gorm.DB`.

```go
package discovery

import (
    "context"
    "strings"
    "time"

    "gorm.io/gorm"
)

type SkillSummary struct {
    ID          string   `json:"id"`
    Name        string   `json:"name"`
    Description string   `json:"description"`
    Version     string   `json:"version"`
    Tags        []string `json:"tags"`
}

type SearchRequest struct {
    ID          string `json:"id,omitempty"`
    Description string `json:"description,omitempty"`
    Tag         string `json:"tag,omitempty"`
    Limit       int    `json:"limit,omitempty"`
}

type RegisterRequest struct {
    ID      string `json:"id"`
    Version string `json:"version"`
}

type skillModel struct {
    ID          string    `gorm:"primaryKey"`
    Name        string    `gorm:"default:''"`
    Description string    `gorm:"default:''"`
    Version     string    `gorm:"default:''"`
    Tags        string    `gorm:"type:text[];default:'{}'"`
    Approved    bool      `gorm:"default:false"`
    Source      string    `gorm:"default:''"`
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

type Discovery struct {
    db  *gorm.DB
    llm LLMReviewer
}

func New(db *gorm.DB, llm LLMReviewer) *Discovery {
    return &Discovery{db: db, llm: llm}
}

func (d *Discovery) Init(ctx context.Context) error {
    return d.db.WithContext(ctx).AutoMigrate(&skillModel{})
}

func (d *Discovery) Search(ctx context.Context, req SearchRequest) ([]SkillSummary, error) {
    q := d.db.WithContext(ctx).Model(&skillModel{}).Where("approved = ?", true)
    if req.ID != "" {
        q = q.Where("id = ? OR id LIKE ?", req.ID, req.ID+"/%")
    }
    if req.Description != "" {
        q = q.Where("description ~* ?", req.Description)
    }
    if req.Tag != "" {
        q = q.Where("EXISTS (SELECT 1 FROM unnest(tags) t WHERE t ~* ?)", req.Tag)
    }
    limit := req.Limit
    if limit <= 0 {
        limit = 20
    }
    var models []skillModel
    if err := q.Order("created_at DESC").Limit(limit).Find(&models).Error; err != nil {
        return nil, err
    }
    results := make([]SkillSummary, len(models))
    for i, m := range models {
        results[i] = SkillSummary{
            ID: m.ID, Name: m.Name, Description: m.Description,
            Version: m.Version, Tags: parseTags(m.Tags),
        }
    }
    return results, nil
}

func parseTags(s string) []string {
    if s == "" || s == "{}" {
        return nil
    }
    s = strings.Trim(s, "{}")
    parts := strings.Split(s, ",")
    out := make([]string, 0, len(parts))
    for _, p := range parts {
        p = strings.Trim(p, "\" ")
        if p != "" {
            out = append(out, p)
        }
    }
    return out
}

func joinTags(tags []string) string {
    if len(tags) == 0 {
        return "{}"
    }
    escaped := make([]string, len(tags))
    for i, t := range tags {
        escaped[i] = `"` + t + `"`
    }
    return "{" + strings.Join(escaped, ",") + "}"
}

func (d *Discovery) RegisterSkill(ctx context.Context, skill SkillSummary) error {
    tags := joinTags(skill.Tags)
    m := skillModel{
        ID: skill.ID, Name: skill.Name, Description: skill.Description,
        Version: skill.Version, Tags: tags, Approved: true,
    }
    return d.db.WithContext(ctx).Save(&m).Error
}

func (d *Discovery) Approve(ctx context.Context, id string) error {
    return d.db.WithContext(ctx).Model(&skillModel{}).Where("id = ?", id).Update("approved", true).Error
}

func (d *Discovery) SetApproved(ctx context.Context, id string, approved bool) error {
    return d.db.WithContext(ctx).Model(&skillModel{}).Where("id = ?", id).Update("approved", approved).Error
}
```

- [ ] **Update test file** — replace pgx pool with GORM, use `gorm.DB` in `connectTestDB`

```go
import (
    "gorm.io/gorm"
    "gorm.io/driver/postgres"
)

func connectTestDB(t *testing.T) *gorm.DB {
    dsn := os.Getenv("SKILLHUB_TEST_DATABASE_URL")
    if dsn == "" {
        t.Skip("SKILLHUB_TEST_DATABASE_URL not set")
    }
    db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
    if err != nil {
        t.Fatal(err)
    }
    return db
}
```

- [ ] **Verify: `go test . -count=1` passes** (or skips without DB URL)

---

### Task 3: Discovery — HTTP server

**Files:**
- Create: `discovery/server.go` — HTTP handlers + router

- [ ] **Implement server.go**

```go
package discovery

import (
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "os"
)

type Server struct {
    disc *Discovery
}

func NewServer(disc *Discovery) *Server {
    return &Server{disc: disc}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    switch r.Method + " " + r.URL.Path {
    case "POST /v1/search":
        s.handleSearch(w, r)
    case "POST /v1/register":
        s.handleRegister(w, r)
    case "GET /health":
        json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
    default:
        http.NotFound(w, r)
    }
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
    var req SearchRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, `{"error":"bad request"}`, 400)
        return
    }
    results, err := s.disc.Search(r.Context(), req)
    if err != nil {
        http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err), 500)
        return
    }
    json.NewEncoder(w).Encode(map[string]any{"results": results})
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
    var req RegisterRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, `{"error":"bad request"}`, 400)
        return
    }

    tmpDir, err := os.MkdirTemp("", "discovery-reg-*")
    if err != nil {
        http.Error(w, `{"error":"internal error"}`, 500)
        return
    }
    defer os.RemoveAll(tmpDir)

    // Step 1: skillhub fetch
    cmd := exec.Command("skillhub", "fetch", req.ID+"@"+req.Version, tmpDir)
    fetchOut, err := cmd.CombinedOutput()
    if err != nil {
        http.Error(w, fmt.Sprintf(`{"error":"fetch failed: %s","detail":"%s"}`, err, string(fetchOut)), 422)
        return
    }

    // Parse the fetch output JSON
    var skill SkillSummary
    if err := json.Unmarshal(fetchOut, &skill); err != nil {
        http.Error(w, fmt.Sprintf(`{"error":"parse metadata: %s"}`, err), 500)
        return
    }

    // Step 2: Read SKILL.md body for LLM
    bodyBytes, _ := os.ReadFile(filepath.Join(tmpDir, "SKILL.md"))

    // Step 3: Store + approve
    if err := s.disc.RegisterSkill(r.Context(), skill); err != nil {
        http.Error(w, fmt.Sprintf(`{"error":"register: %s"}`, err), 500)
        return
    }

    json.NewEncoder(w).Encode(map[string]any{"id": req.ID, "approved": true})
}
```

Make sure to add these imports: `"os/exec"`, `"path/filepath"`, `"encoding/json"`.

- [ ] **Add listen/start in cmd/discovery/main.go** in a later task. For now test via:
```bash
go build .
```

---

### Task 4: LLMReviewer interface + implementations

**Files:**
- Create: `discovery/llm.go` — interface
- Create: `discovery/llm_openai.go` — OpenAI/DeepSeek implementation
- Create: `discovery/llm_anthropic.go` — Anthropic implementation

- [ ] **llm.go — interface**

```go
package discovery

import "context"

type ReviewResult struct {
    Passed bool
    Reason string
}

type LLMReviewer interface {
    Review(ctx context.Context, skill SkillSummary, body string) (*ReviewResult, error)
}
```

- [ ] **llm_openai.go — DeepSeek/OpenAI compatible**

```go
package discovery

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "os"
)

type openAIConfig struct {
    apiKey  string
    baseURL string
    model   string
}

func newOpenAIConfig() *openAIConfig {
    return &openAIConfig{
        apiKey:  os.Getenv("OPENAI_API_KEY"),
        baseURL: getEnvDefault("OPENAI_BASE_URL", "https://api.deepseek.com/v1"),
        model:   os.Getenv("OPENAI_MODEL"),
    }
}

func (c *openAIConfig) Valid() bool {
    return c.apiKey != "" && c.model != ""
}

type openAIMessage struct {
    Role    string `json:"role"`
    Content string `json:"content"`
}

type openAIRequest struct {
    Model    string          `json:"model"`
    Messages []openAIMessage `json:"messages"`
}

type openAIChoice struct {
    Message openAIMessage `json:"message"`
}

type openAIResponse struct {
    Choices []openAIChoice `json:"choices"`
}

type OpenAIReviewer struct {
    config *openAIConfig
    client *http.Client
}

func NewOpenAIReviewer() *OpenAIReviewer {
    return &OpenAIReviewer{
        config: newOpenAIConfig(),
        client: &http.Client{},
    }
}

func (r *OpenAIReviewer) Review(ctx context.Context, skill SkillSummary, body string) (*ReviewResult, error) {
    if !r.config.Valid() {
        return &ReviewResult{Passed: true, Reason: "LLM not configured"}, nil
    }
    prompt := fmt.Sprintf(`You are a security reviewer for AI agent skills. Review the following skill for any malicious or dangerous content.

Skill: %s (%s)
Description: %s

Content:
%s

Reply with exactly:
PASS
one-sentence reason
or
REJECT
one-sentence reason`, skill.ID, skill.Version, skill.Description, body)

    reqBody := openAIRequest{
        Model: r.config.model,
        Messages: []openAIMessage{
            {Role: "user", Content: prompt},
        },
    }
    data, _ := json.Marshal(reqBody)
    httpReq, err := http.NewRequestWithContext(ctx, "POST", r.config.baseURL+"/chat/completions", bytes.NewReader(data))
    if err != nil {
        return nil, err
    }
    httpReq.Header.Set("Authorization", "Bearer "+r.config.apiKey)
    httpReq.Header.Set("Content-Type", "application/json")

    resp, err := r.client.Do(httpReq)
    if err != nil {
        return &ReviewResult{Passed: true, Reason: fmt.Sprintf("LLM API error: %v", err)}, nil
    }
    defer resp.Body.Close()

    var openResp openAIResponse
    if err := json.NewDecoder(resp.Body).Decode(&openResp); err != nil {
        return &ReviewResult{Passed: true, Reason: "LLM parse error"}, nil
    }
    if len(openResp.Choices) == 0 {
        return &ReviewResult{Passed: true, Reason: "LLM no response"}, nil
    }
    reply := openResp.Choices[0].Message.Content
    if len(reply) >= 6 && reply[:6] == "REJECT" {
        reason := strings.TrimSpace(reply[6:])
        return &ReviewResult{Passed: false, Reason: reason}, nil
    }
    return &ReviewResult{Passed: true, Reason: "LLM approved"}, nil
}
```

Need `"strings"` import.

- [ ] **llm_anthropic.go — Claude**

```go
package discovery

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "os"
    "strings"
)

type anthropicConfig struct {
    apiKey string
    model  string
}

func newAnthropicConfig() *anthropicConfig {
    return &anthropicConfig{
        apiKey: os.Getenv("ANTHROPIC_API_KEY"),
        model:  getEnvDefault("ANTHROPIC_MODEL", "claude-sonnet-4-20250514"),
    }
}

func (c *anthropicConfig) Valid() bool {
    return c.apiKey != "" && c.model != ""
}

type anthropicMessage struct {
    Role    string `json:"role"`
    Content string `json:"content"`
}

type anthropicRequest struct {
    Model     string             `json:"model"`
    Messages  []anthropicMessage `json:"messages"`
    MaxTokens int                `json:"max_tokens"`
}

type anthropicContent struct {
    Text string `json:"text"`
}

type anthropicResponse struct {
    Content []anthropicContent `json:"content"`
}

type AnthropicReviewer struct {
    config *anthropicConfig
    client *http.Client
}

func NewAnthropicReviewer() *AnthropicReviewer {
    return &AnthropicReviewer{
        config: newAnthropicConfig(),
        client: &http.Client{},
    }
}

func (r *AnthropicReviewer) Review(ctx context.Context, skill SkillSummary, body string) (*ReviewResult, error) {
    if !r.config.Valid() {
        return &ReviewResult{Passed: true, Reason: "Anthropic not configured"}, nil
    }
    prompt := fmt.Sprintf(`You are a security reviewer for AI agent skills. Review the following skill for malicious content.

Skill: %s
Description: %s

%s

Reply PASS or REJECT with one-sentence reason.`, skill.ID, skill.Description, body)

    reqBody := anthropicRequest{
        Model:     r.config.model,
        MaxTokens: 256,
        Messages:  []anthropicMessage{{Role: "user", Content: prompt}},
    }
    data, _ := json.Marshal(reqBody)
    httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(data))
    if err != nil {
        return nil, err
    }
    httpReq.Header.Set("x-api-key", r.config.apiKey)
    httpReq.Header.Set("anthropic-version", "2023-06-01")
    httpReq.Header.Set("Content-Type", "application/json")

    resp, err := r.client.Do(httpReq)
    if err != nil {
        return &ReviewResult{Passed: true, Reason: fmt.Sprintf("Anthropic API error: %v", err)}, nil
    }
    defer resp.Body.Close()

    var antResp anthropicResponse
    if err := json.NewDecoder(resp.Body).Decode(&antResp); err != nil {
        return &ReviewResult{Passed: true, Reason: "parse error"}, nil
    }
    reply := ""
    for _, c := range antResp.Content {
        reply += c.Text
    }
    if strings.Contains(reply, "REJECT") {
        return &ReviewResult{Passed: false, Reason: reply}, nil
    }
    return &ReviewResult{Passed: true, Reason: "approved"}, nil
}
```

Helper function (put in llm.go):
```go
func getEnvDefault(key, def string) string {
    if v := os.Getenv(key); v != "" {
        return v
    }
    return def
}
```

- [ ] **Verify `go build .` passes in discovery/**

---

### Task 5: Security scanner (migrate from skillhub)

**Files:**
- Create: `discovery/scanner.go` — RuleScanner + VirusTotalScanner
- Remove: `skillhub/pkg/security/` — delete later

- [ ] **Copy `rule_scanner.go` from skillhub pkg/security** into `discovery/scanner.go` as `RuleScanner`
- [ ] **Add ClamAV support** since no VT_API_KEY:

```go
type ClamAVScanner struct{}

func NewClamAVScanner() *ClamAVScanner { return &ClamAVScanner{} }

func (s *ClamAVScanner) Scan(path string) (*ScanResult, error) {
    cmd := exec.Command("clamscan", "--no-summary", path)
    out, err := cmd.CombinedOutput()
    if err == nil {
        return &ScanResult{Passed: true}, nil
    }
    return &ScanResult{Passed: false, Issues: []string{string(out)}}, nil
}
```

- [ ] **Verify: `go build .` passes in discovery/**

---

### Task 6: Discovery CLI (serve)

**Files:**
- Create: `discovery/cmd/discovery/main.go`

- [ ] **Implement main.go**

```go
package main

import (
    "context"
    "fmt"
    "net/http"
    "os"

    "gorm.io/driver/postgres"
    "gorm.io/gorm"

    "discovery"
)

func main() {
    port := os.Getenv("DISCOVERY_PORT")
    if port == "" {
        port = "8399"
    }
    dsn := os.Getenv("DATABASE_URL")
    if dsn == "" {
        fmt.Fprintln(os.Stderr, "DATABASE_URL is required")
        os.Exit(1)
    }

    db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
    if err != nil {
        fmt.Fprintf(os.Stderr, "db: %v\n", err)
        os.Exit(1)
    }

    var llm discovery.LLMReviewer
    switch os.Getenv("LLM_PROVIDER") {
    case "anthropic":
        llm = discovery.NewAnthropicReviewer()
    default:
        llm = discovery.NewOpenAIReviewer()
    }

    disc := discovery.New(db, llm)
    if err := disc.Init(context.Background()); err != nil {
        fmt.Fprintf(os.Stderr, "init: %v\n", err)
        os.Exit(1)
    }

    srv := discovery.NewServer(disc)
    addr := ":" + port
    fmt.Fprintf(os.Stderr, "discovery listening on %s\n", addr)
    if err := http.ListenAndServe(addr, srv); err != nil {
        fmt.Fprintf(os.Stderr, "serve: %v\n", err)
        os.Exit(1)
    }
}
```

- [ ] **Verify: `go build ./cmd/discovery/` passes**

---

### Task 7: Skillhub HTTP client for discovery

**Files:**
- Create: `skillhub/pkg/discoveryclient/client.go` — HTTP client for discovery API
- Modify: `skillhub/cmd/skillhub/main.go` — replace embedded discovery with HTTP client

- [ ] **Create discovery client**

```go
package discoveryclient

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "net/http"
)

type SkillSummary struct {
    ID          string   `json:"id"`
    Name        string   `json:"name"`
    Description string   `json:"description"`
    Version     string   `json:"version"`
    Tags        []string `json:"tags"`
}

type SearchRequest struct {
    ID          string `json:"id,omitempty"`
    Description string `json:"description,omitempty"`
    Tag         string `json:"tag,omitempty"`
    Limit       int    `json:"limit,omitempty"`
}

type Client struct {
    baseURL string
    http    *http.Client
}

func New(baseURL string) *Client {
    return &Client{baseURL: baseURL, http: &http.Client{}}
}

func (c *Client) Search(ctx context.Context, req SearchRequest) ([]SkillSummary, error) {
    data, _ := json.Marshal(req)
    httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/search", bytes.NewReader(data))
    if err != nil {
        return nil, err
    }
    httpReq.Header.Set("Content-Type", "application/json")
    resp, err := c.http.Do(httpReq)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    var result struct {
        Results []SkillSummary `json:"results"`
    }
    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return nil, err
    }
    return result.Results, nil
}
```

- [ ] **Update `skillhub/cmd/skillhub/main.go`** — remove embedded discovery, use HTTP client instead:

```go
import "skillhub/pkg/discoveryclient"

// In cmdSearch:
client := discoveryclient.New("http://localhost:" + discoveryPort)
```

---

### Task 8: Cleanup

- [ ] **Remove `skillhub/pkg/security/`** — no longer needed
- [ ] **Remove `skillhub/pkg/installer/`** — no longer needed (all install logic moves to fetch)
- [ ] **Verify: `go build ./cmd/skillhub/` and `go test ./...` both pass**
- [ ] **Run full build and test for both modules**
