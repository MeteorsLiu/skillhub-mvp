package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mark3labs/mcp-go/server"
	"golang.org/x/mod/semver"

	cachepkg "skillhub/pkg/cache"
	"skillhub/pkg/discoveryclient"
	"skillhub/pkg/loader"
	"skillhub/pkg/mcp"
	"skillhub/pkg/parser"
	"skillhub/pkg/types"
	"skillhub/pkg/vcs"
)

func main() {
	flag.Parse()
	args := flag.Args()

	if len(args) == 0 {
		cmdServe()
		return
	}

	switch args[0] {
	case "serve":
		cmdServe()
	case "search":
		cmdSearch(args[1:])
	case "load":
		cmdLoad(args[1:])
	case "fetch":
		cmdFetch(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", args[0])
		os.Exit(1)
	}
}

func discoveryBaseURL() string {
	host := os.Getenv("SKILLHUB_DISCOVERY_HOST")
	if host == "" {
		host = "http://localhost:8399"
	}
	return host
}

func skillHubHome() string {
	home := os.Getenv("SKILLHUB_HOME")
	if home == "" {
		h, _ := os.UserHomeDir()
		home = filepath.Join(h, ".skillhub")
	}
	return home
}

func selectInstalledVersion(installPath string) string {
	entries, err := os.ReadDir(installPath)
	if err != nil {
		return ""
	}
	var latest string
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "v") {
			continue
		}
		if latest == "" || semver.Compare(e.Name(), latest) > 0 {
			latest = e.Name()
		}
	}
	return latest
}

func extractFirstLine(body string) string {
	body = strings.TrimSpace(body)
	if idx := strings.IndexByte(body, '\n'); idx >= 0 {
		return body[:idx]
	}
	return body
}

func cmdServe() {
	client := discoveryclient.New(discoveryBaseURL())
	home := skillHubHome()
	dbPath := filepath.Join(home, "skillhub.db")
	skillsRoot := filepath.Join(home, "skills")
	c, err := cachepkg.Open(dbPath, skillsRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cache init warning: %v\n", err)
		c = nil
	}
	core := &mcpCore{client: client, cache: c}
	mcpSrv := mcp.NewServer(core)
	if err := server.ServeStdio(mcpSrv); err != nil {
		fmt.Fprintf(os.Stderr, "MCP error: %v\n", err)
		os.Exit(1)
	}
}

type mcpCore struct {
	client *discoveryclient.Client
	cache  *cachepkg.Cache
}

func (c *mcpCore) splitSubSkill(id string) (rootID, subPath string) {
	if c.cache == nil {
		return id, ""
	}
	ids, err := c.cache.AllRootIDs()
	if err != nil || len(ids) == 0 {
		return id, ""
	}
	for _, root := range ids {
		if id == root {
			return root, ""
		}
		prefix := root + "/"
		if strings.HasPrefix(id, prefix) {
			return root, id[len(prefix):]
		}
	}
	return id, ""
}

func (c *mcpCore) Search(req types.SearchRequest) ([]types.SkillSummary, error) {
	if c.cache != nil {
		results, err := c.cache.Search(req.Description, req.Tag, req.Limit)
		if err == nil && len(results) > 0 {
			return results, nil
		}
	}

	discReq := discoveryclient.SearchRequest{
		ID:          req.ID,
		Description: req.Description,
		Tag:         req.Tag,
		Limit:       req.Limit,
	}
	remoteResults, err := c.client.Search(context.Background(), discReq)
	if err != nil {
		return nil, err
	}

	if c.cache != nil {
		go func() {
			for _, r := range remoteResults {
				c.cache.Upsert(types.SkillSummary{
					ID:          r.ID,
					Name:        r.Name,
					Description: r.Description,
					Version:     r.Version,
					Tags:        r.Tags,
				}, "remote")
			}
		}()
	}

	out := make([]types.SkillSummary, len(remoteResults))
	for i, r := range remoteResults {
		out[i] = types.SkillSummary{
			ID:          r.ID,
			Name:        r.Name,
			Description: r.Description,
			Version:     r.Version,
			Tags:        r.Tags,
		}
	}
	return out, nil
}

func (c *mcpCore) Load(req types.LoadRequest) (*types.Skill, error) {
	rootID, subPath := c.splitSubSkill(req.ID)

	home := skillHubHome()
	version := req.Version
	installPath := filepath.Join(home, "skills", rootID)

	if version == "" {
		version = selectInstalledVersion(installPath)
		if version == "" {
			repoURL := vcs.RepoURL(rootID)
			if repoURL == "" {
				return nil, fmt.Errorf("invalid root id: %q", rootID)
			}
			var err error
			version, err = vcs.ResolveVersion(repoURL, "")
			if err != nil {
				return nil, fmt.Errorf("resolve version for %q: %w", rootID, err)
			}
			subDir := vcs.SubdirPath(rootID)
			targetDir := filepath.Join(installPath, version)
			if err := vcs.Clone(repoURL, version, subDir, targetDir); err != nil {
				return nil, fmt.Errorf("clone %q: %w", rootID, err)
			}
		}
	} else {
		targetDir := filepath.Join(installPath, version)
		if _, err := os.Stat(targetDir); os.IsNotExist(err) {
			repoURL := vcs.RepoURL(rootID)
			if repoURL == "" {
				return nil, fmt.Errorf("invalid root id: %q", rootID)
			}
			subDir := vcs.SubdirPath(rootID)
			if err := vcs.Clone(repoURL, version, subDir, targetDir); err != nil {
				return nil, fmt.Errorf("clone %q@%s: %w", rootID, version, err)
			}
		}
	}

	fullPath := filepath.Join(installPath, version)
	var skill *types.Skill
	var loadErr error
	if subPath != "" {
		skill, loadErr = loader.LoadSub(fullPath, subPath, rootID, version)
	} else {
		skill, loadErr = loader.LoadRoot(fullPath, version)
	}
	if loadErr != nil {
		return nil, loadErr
	}

	if c.cache != nil {
		c.cache.Upsert(types.SkillSummary{
			ID:          skill.ID,
			Name:        skill.Name,
			Description: extractFirstLine(skill.Body),
			Version:     skill.Version,
		}, "local")
	}

	return skill, nil
}

func cmdSearch(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: skillhub search <query>")
		os.Exit(1)
	}

	client := discoveryclient.New(discoveryBaseURL())
	req := discoveryclient.SearchRequest{
		ID:          args[0],
		Description: args[0],
		Tag:         args[0],
		Limit:       20,
	}

	results, err := client.Search(context.Background(), req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "search failed: %v\n", err)
		os.Exit(1)
	}

	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "json error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(data))
}

func cmdLoad(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: skillhub load <id> [version]")
		os.Exit(1)
	}

	req := types.LoadRequest{ID: args[0]}
	if len(args) > 1 {
		req.Version = args[1]
	}

	core := &mcpCore{}
	skill, err := core.Load(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load failed: %v\n", err)
		os.Exit(1)
	}

	data, err := json.MarshalIndent(skill, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "json error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(data))
}

func cmdFetch(args []string) {
	if len(args) != 2 {
		fmt.Fprintf(os.Stderr, "Usage: skillhub fetch <id>@<version> <output-dir>\n")
		os.Exit(1)
	}

	idVersion := args[0]
	outputDir := args[1]

	atIdx := strings.LastIndex(idVersion, "@")
	if atIdx < 0 {
		fmt.Fprintf(os.Stderr, "Error: invalid id@version format (missing @)\n")
		os.Exit(1)
	}
	id := idVersion[:atIdx]
	version := idVersion[atIdx+1:]
	if id == "" || version == "" {
		fmt.Fprintf(os.Stderr, "Error: invalid id@version format\n")
		os.Exit(1)
	}

	repoURL := vcs.RepoURL(id)
	if repoURL == "" {
		fmt.Fprintf(os.Stderr, "Error: invalid id %q\n", id)
		os.Exit(1)
	}
	subDir := vcs.SubdirPath(id)

	if version == "" || version == "latest" {
		var err error
		version, err = vcs.ResolveVersion(repoURL, "")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error resolving version: %v\n", err)
			os.Exit(1)
		}
	}

	if err := vcs.Clone(repoURL, version, subDir, outputDir); err != nil {
		fmt.Fprintf(os.Stderr, "Error cloning: %v\n", err)
		os.Exit(1)
	}

	result, err := parser.ParseRootWithID(outputDir, id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing SKILL.md: %v\n", err)
		os.Exit(1)
	}

	output := map[string]interface{}{
		"id":          result.ID,
		"name":        result.Name,
		"description": result.Description,
		"version":     version,
		"tags":        result.Tags,
		"deps":        result.Deps,
	}

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error encoding JSON: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(data))
}
