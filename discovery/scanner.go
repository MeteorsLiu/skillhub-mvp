package discovery

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

type ScanResult struct {
	Passed bool
	Issues []string
}

type Scanner interface {
	Scan(path string) (*ScanResult, error)
}

// ─── RuleScanner ────────────────────────────────────────────────────────────

type rule struct {
	name    string
	pattern *regexp.Regexp
}

type RuleScanner struct {
	rules []rule
}

func NewRuleScanner() *RuleScanner {
	return &RuleScanner{
		rules: []rule{
			{name: "base64 decode to shell", pattern: regexp.MustCompile(`base64\s+-d\s*\|.*\b(sh|bash)\b`)},
			{name: "curl to IP address", pattern: regexp.MustCompile(`curl\s+(https?://\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3})`)},
			{name: "wget to IP address", pattern: regexp.MustCompile(`wget\s+(https?://\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3})`)},
			{name: "hardcoded secret", pattern: regexp.MustCompile(`(?i)(?:sk-|api[_-]?key|secret|token)\s*[:=]\s*['\"]?[a-zA-Z0-9_\-]{16,}`)},
			{name: "reverse shell", pattern: regexp.MustCompile(`>&?\s*/dev/tcp/`)},
			{name: "destructive rm -rf /", pattern: regexp.MustCompile(`rm\s+-rf\s+/\s*$`)},
			{name: "insecure chmod 777", pattern: regexp.MustCompile(`chmod\s+777`)},
		},
	}
}

func (s *RuleScanner) Scan(path string) (*ScanResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return s.ScanContent(path, string(data)), nil
}

func (s *RuleScanner) ScanContent(name, content string) *ScanResult {
	var issues []string
	for _, r := range s.rules {
		if r.pattern.MatchString(content) {
			issues = append(issues, fmt.Sprintf("%s: matched %s", name, r.name))
		}
	}
	if len(issues) > 0 {
		return &ScanResult{Passed: false, Issues: issues}
	}
	return &ScanResult{Passed: true}
}

func (s *RuleScanner) ScanDir(dir string) (*ScanResult, error) {
	combined := &ScanResult{Passed: true}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		result, err := s.Scan(path)
		if err != nil {
			return err
		}
		if !result.Passed {
			combined.Passed = false
			combined.Issues = append(combined.Issues, result.Issues...)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return combined, nil
}

// ─── ClamAVScanner ──────────────────────────────────────────────────────────

type ClamAVScanner struct{}

func NewClamAVScanner() *ClamAVScanner {
	return &ClamAVScanner{}
}

func (s *ClamAVScanner) Scan(path string) (*ScanResult, error) {
	cmd := exec.Command("clamscan", "--no-summary", path)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return &ScanResult{Passed: true}, nil
	}
	return &ScanResult{Passed: false, Issues: []string{strings.TrimSpace(string(out))}}, nil
}

func (s *ClamAVScanner) ScanDir(dir string) (*ScanResult, error) {
	cmd := exec.Command("clamscan", "--no-summary", "-r", dir)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return &ScanResult{Passed: true}, nil
	}
	return &ScanResult{Passed: false, Issues: []string{strings.TrimSpace(string(out))}}, nil
}

// ─── ChainScanner ───────────────────────────────────────────────────────────

type ChainScanner struct {
	scanners []Scanner
}

func NewChainScanner() *ChainScanner {
	return &ChainScanner{}
}

func (c *ChainScanner) Add(s Scanner) {
	c.scanners = append(c.scanners, s)
}

func (c *ChainScanner) Scan(path string) (*ScanResult, error) {
	result := &ScanResult{Passed: true}
	for _, s := range c.scanners {
		r, err := s.Scan(path)
		if err != nil {
			continue
		}
		if !r.Passed {
			result.Passed = false
			result.Issues = append(result.Issues, r.Issues...)
		}
	}
	return result, nil
}

func (c *ChainScanner) ScanDir(dir string) (*ScanResult, error) {
	result := &ScanResult{Passed: true}
	for _, s := range c.scanners {
		var r *ScanResult
		var err error
		if rs, ok := s.(interface{ ScanDir(string) (*ScanResult, error) }); ok {
			r, err = rs.ScanDir(dir)
		} else {
			r, err = s.Scan(dir)
		}
		if err != nil {
			continue
		}
		if !r.Passed {
			result.Passed = false
			result.Issues = append(result.Issues, r.Issues...)
		}
	}
	return result, nil
}

func (c *ChainScanner) ScanContent(name, content string) *ScanResult {
	result := &ScanResult{Passed: true}
	for _, s := range c.scanners {
		if rs, ok := s.(interface{ ScanContent(string, string) *ScanResult }); ok {
			r := rs.ScanContent(name, content)
			if !r.Passed {
				result.Passed = false
				result.Issues = append(result.Issues, r.Issues...)
			}
		}
	}
	return result
}
