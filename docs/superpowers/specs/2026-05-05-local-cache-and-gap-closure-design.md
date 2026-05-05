# Design: 本地缓存 + 补齐设计文档差距

**日期**: 2026-05-05

## 1. 设计目标

两项并行的改动：

1. **新增本地 SQLite 缓存层**（`pkg/cache`）：`skillhub_search` 优先查本地，未命中才走远程 discovery center。
2. **补齐设计文档中标记但代码未实现的功能**（P0/P1/P2）。

---

## 2. 模块变更

```
新增:
  skillhub/pkg/cache/          # SQLite 本地 skill 元数据索引

修改:
  skillhub/cmd/skillhub/main.go    # Load 内联安装逻辑，Search 优先查本地缓存
  skillhub/pkg/types/types.go      # JSON tag 对齐设计文档，删除 RootID
  skillhub/pkg/parser/parser.go    # id fallback(Git remote 推导), deps 元数据
  discovery/discovery.go           # Search 只返回 root skill
```

删除 `types.RootID()` —— 不再需要，改为 Load 内部的**最长前缀匹配**判断 sub-skill。

---

## 3. `pkg/cache` — SQLite 本地索引

### 3.1 表结构

复用 discovery center 的 schema：

```sql
CREATE TABLE IF NOT EXISTS skills (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    version     TEXT NOT NULL DEFAULT '',
    tags        TEXT NOT NULL DEFAULT '{}',
    status      TEXT NOT NULL DEFAULT '',
    source      TEXT NOT NULL DEFAULT '',    -- "local" | "remote"
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

- **只存 root skill**，不存 sub-skill。sub-skill 通过 root skill 的 `sub_skills` 字段发现。
- tags 存为 JSON 字符串（`'["social","xiaohongshu"]'`），用 `LIKE '%keyword%'` 匹配。

### 3.2 接口

```go
type Cache struct {
    db *sql.DB
}

// 首次启动：建表 + 从 $SKILLHUB_HOME/skills/ 扫描已有 skill 入库
func Open(path, skillsRoot string) (*Cache, error)

// regex 匹配 name/description，LIKE 匹配 tags
func (c *Cache) Search(description, tag string, limit int) ([]types.SkillSummary, error)

// 写入或更新一条。load 安装后和 search 远程返回后调用
func (c *Cache) Upsert(summary types.SkillSummary, source string) error
```

### 3.3 Search SQL

```sql
SELECT id, name, description, version, tags
FROM skills
WHERE (name REGEXP ? OR description REGEXP ? OR tags LIKE '%' || ? || '%')
LIMIT ?
```

SQLite 的 REGEXP 需要通过 Go driver 注册。tags 用 `LIKE` 做子串匹配。

### 3.4 数据来源

| 写入时机 | source |
|----------|--------|
| `skillhub_load` 安装完成后 | `"local"` |
| `skillhub_search` 远程返回后 | `"remote"` |

### 3.5 存储路径

```
$SKILLHUB_HOME/skillhub.db
```

---

## 4. Load 内联安装逻辑

### 4.1 流程

```go
func (c *mcpCore) Load(req types.LoadRequest) (*types.Skill, error) {
    rootID, subPath := splitSubSkill(req.ID)  // 最长前缀匹配

    home := skillHubHome()
    version := req.Version
    installPath := filepath.Join(home, "skills", rootID)

    if version == "" {
        version = selectLatestVersion(installPath)
        if version == "" {
            version = resolveRemote(rootID)
            cloneToLocal(rootID, version, installPath)
        }
    } else {
        verDir := filepath.Join(installPath, version)
        if _, err := os.Stat(verDir); os.IsNotExist(err) {
            cloneToLocal(rootID, version, installPath)
        }
    }

    fullPath := filepath.Join(installPath, version)
    var skill *types.Skill
    var err error
    if subPath != "" {
        skill, err = loader.LoadSub(fullPath, subPath, rootID, version)
    } else {
        skill, err = loader.LoadRoot(fullPath, version)
    }
    if err != nil {
        return nil, err
    }

    c.cache.Upsert(skillSummary(skill), "local")
    return skill, nil
}
```

### 4.2 子函数

```go
// splitSubSkill 最长前缀匹配缓存中的 root skill 判断是否为 sub-skill
func splitSubSkill(id string) (rootID, subPath string)

// selectLatestVersion 扫描 installPath/ 下所有 v* 目录，semver 比较选最大
func selectLatestVersion(installPath string) string

// resolveRemote 调用 vcs 获取 root skill 的最新 tag
func resolveRemote(rootID string) (string, error)

// cloneToLocal git clone root skill 到 installPath/version/ 目录
func cloneToLocal(rootID, version, installPath string) error
```

---

## 5. Search 改为本地缓存优先

### 5.1 流程

```go
func (c *mcpCore) Search(req types.SearchRequest) ([]types.SkillSummary, error) {
    results, err := c.cache.Search(req.Description, req.Tag, req.Limit)
    if err == nil && len(results) > 0 {
        return results, nil
    }
    // 本地未命中 → 远程 discovery center
    discReq := discoveryclient.SearchRequest{...}
    remoteResults, err := c.client.Search(ctx, discReq)
    if err != nil {
        return nil, err
    }
    // 后台写入本地缓存（不阻塞返回）
    go func() {
        for _, r := range remoteResults {
            c.cache.Upsert(r, "remote")
        }
    }()
    return remoteResults, nil
}
```

---

## 6. P0 修复

### 6.1 删除 `RootID()`，改为最长前缀匹配

`types.go:75-81` 删除 `RootID()`。在 `main.go` 的 `Load()`、`mcpCore` 内新增 `splitSubSkill(id string) (rootID, subPath string)`：

```go
func (c *mcpCore) splitSubSkill(id string) (string, string) {
    rootIDs, _ := c.cache.AllRootIDs() // 返回缓存中所有 root skill id
    for _, rootID := range rootIDs {
        if id == rootID {
            return rootID, ""
        }
        prefix := rootID + "/"
        if strings.HasPrefix(id, prefix) {
            return rootID, id[len(prefix):]
        }
    }
    // 缓存中没有 → 当 root 处理
    return id, ""
}
```

### 6.2 JSON tag 对齐设计文档

| 文件 | 字段 | 改前 | 改后 |
|------|------|------|------|
| `types.go:52` | `SubSkills` | `json:"subSkills"` | `json:"sub_skills,omitempty"` |
| `types.go:43` | `Deps.Skills` | `json:"skills"` | `json:"skills,omitempty"` |

### 6.3 Discovery center Search 只返回 root skill

discovery center 的 `skillModel` 增加 `is_sub BOOL NOT NULL DEFAULT false` 字段。Search 过滤：

```sql
WHERE status = 'approved' AND is_sub = false
  AND (id = ? OR id LIKE ?)
```

---

## 7. P1 修复

### 7.1 `id` fallback — 从 Git remote 推导

单仓库 skill 未声明 `id` 时：

```go
func deriveID(skillDir string) (string, error) {
    remotes, err := git.Remotes(skillDir)
    if err != nil {
        return "", fmt.Errorf("id is required and not set: add id to SKILL.md or ensure git remote is configured")
    }
    // 选 origin remote → "github.com/owner/repo"
    return remotes["origin"], nil
}
```

### 7.2 依赖 skill 补全 Name/Description

`Load()` 返回的 `Deps.Skills` 中的 `SkillSummary` 需要包含 `Name` 和 `Description`。从缓存的 SQLite 中补全已安装依赖的元数据；未安装的远程依赖通过 VCS 解析其远程 SKILL.md。

---

## 8. P2 修复

### 8.1 本地版本选择用 semver 比较

`selectLatestVersion()` 从文件名字典序改为 semver 语义版本比较。

### 8.2 tags 自动发现使用声明顺序

`parser.go:DiscoverSubSkills()` 当 `skills:` 声明存在时使用声明顺序；未声明时使用目录顺序。不改。

---

## 9. 不变的部分

以下模块本次不修改：

- `pkg/loader` — 加载逻辑不变
- `pkg/parser` — YAML 解析逻辑不变（仅增加 id fallback）
- `pkg/vcs` — git 操作不变
- `pkg/resolver` — MVS 不变
- `pkg/mcp` — MCP server 不变
- `pkg/discoveryclient` — HTTP client 不变
- `discovery/scanner.go` — 安全扫描不变
- `discovery/llm_*.go` — AI reviewer 不变
- `discovery/vt.go` — VirusTotal 不变
