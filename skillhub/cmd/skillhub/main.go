package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
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

var httpAddr = flag.String("http", "", "Serve over HTTP on this address (e.g. :8398). Empty means stdio.")

const skillResourceRoot = "/tmp/.llar"

func main() {
	flag.Parse()
	args := flag.Args()

	if len(args) == 0 {
		if *httpAddr != "" {
			cmdServeHTTP()
		} else {
			cmdServe()
		}
		return
	}

	switch args[0] {
	case "serve":
		if *httpAddr != "" {
			cmdServeHTTP()
		} else {
			cmdServe()
		}
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

func resourceCacheDir(rootID, version string) (string, error) {
	escaped, err := filepath.Localize(rootID)
	if err != nil {
		return "", err
	}
	return filepath.Join(skillResourceRoot, escaped+"@"+version), nil
}

func prepareResourceDirectory(srcDir, rootID, version string) (string, error) {
	dstDir, err := resourceCacheDir(rootID, version)
	if err != nil {
		return "", err
	}
	if err := os.RemoveAll(dstDir); err != nil {
		return "", err
	}
	if err := copyResourcesWithoutSkillMD(srcDir, dstDir); err != nil {
		return "", err
	}
	return dstDir, nil
}

func copyResourcesWithoutSkillMD(srcDir, dstDir string) error {
	return filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(dstDir, 0755)
		}
		if d.Name() == ".git" && d.IsDir() {
			return filepath.SkipDir
		}
		if d.Name() == "SKILL.md" {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}

		target := filepath.Join(dstDir, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		return copyFile(path, target, info.Mode().Perm())
	})
}

func copyFile(src, dst string, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
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

func cmdServeHTTP() {
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
	httpServer := server.NewStreamableHTTPServer(mcpSrv)
	fmt.Fprintf(os.Stderr, "skillhub HTTP MCP listening on %s\n", *httpAddr)
	if err := httpServer.Start(*httpAddr); err != nil {
		fmt.Fprintf(os.Stderr, "MCP HTTP error: %v\n", err)
		os.Exit(1)
	}
}

type mcpCore struct {
	client discoverySearcher
	cache  *cachepkg.Cache
}

type discoverySearcher interface {
	Search(ctx context.Context, req discoveryclient.SearchRequest) ([]discoveryclient.SkillSummary, error)
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
		cached, err := c.cache.GetPromotedSearch(req)
		if err == nil && cached != nil {
			return cached, nil
		}
	}

	discReq := discoveryclient.SearchRequest{
		ID:          req.ID,
		Description: req.Description,
		Tag:         req.Tag,
		Limit:       req.Limit,
		Offset:      req.Offset,
	}
	remoteResults, err := c.client.Search(context.Background(), discReq)
	if err != nil {
		return nil, err
	}

	out := discoveryResultsToTypes(remoteResults)
	if c.cache != nil {
		if ok, err := c.cache.ShouldPromoteSearch(req); err == nil && ok {
			_ = c.cache.PutPromotedSearch(req, out)
		}
		_ = c.cache.RecordSearchObservation(req, out)
		for _, r := range out {
			_ = c.cache.Upsert(r, "remote")
		}
	}

	return out, nil
}

func discoveryResultsToTypes(remoteResults []discoveryclient.SkillSummary) []types.SkillSummary {
	out := make([]types.SkillSummary, len(remoteResults))
	for i, r := range remoteResults {
		out[i] = types.SkillSummary{
			ID:          r.ID,
			Name:        r.Name,
			Description: r.Description,
			Version:     r.Version,
			Tags:        r.Tags,
			Offset:      r.Offset,
		}
	}
	return out
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
	resourceDir, err := prepareResourceDirectory(fullPath, rootID, version)
	if err != nil {
		return nil, fmt.Errorf("prepare resource directory: %w", err)
	}
	skill.ResourceDirectory = resourceDir

	if c.cache != nil {
		c.cache.Upsert(types.SkillSummary{
			ID:          skill.ID,
			Name:        skill.Name,
			Description: extractFirstLine(skill.Body),
			Version:     skill.Version,
		}, "local")

		for i, dep := range skill.Deps.Skills {
			depResults, _ := c.cache.Search(dep.ID, "", 1, 0)
			for _, r := range depResults {
				if r.ID == dep.ID {
					skill.Deps.Skills[i].Name = r.Name
					skill.Deps.Skills[i].Description = r.Description
				}
			}
		}
	}

	return skill, nil
}

func cmdSearch(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: skillhub search <query>")
		os.Exit(1)
	}

	client := discoveryclient.New(discoveryBaseURL())
	req := searchRequestFromQuery(args[0])

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

func searchRequestFromQuery(query string) discoveryclient.SearchRequest {
	return discoveryclient.SearchRequest{
		Description: query,
		Tag:         query,
		Limit:       20,
	}
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
