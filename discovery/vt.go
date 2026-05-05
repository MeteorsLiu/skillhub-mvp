package discovery

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type VirusTotalScanner struct {
	apiKey string
	client *http.Client
}

func NewVirusTotalScanner() *VirusTotalScanner {
	return &VirusTotalScanner{
		apiKey: os.Getenv("VT_API_KEY"),
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (s *VirusTotalScanner) Enabled() bool {
	return s.apiKey != ""
}

func (s *VirusTotalScanner) Scan(path string) (*ScanResult, error) {
	return s.scanFile(path)
}

func (s *VirusTotalScanner) ScanDir(dir string) (*ScanResult, error) {
	if !s.Enabled() {
		return &ScanResult{Passed: true}, nil
	}

	combined := &ScanResult{Passed: true}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		result, scanErr := s.scanFile(path)
		if scanErr != nil {
			return scanErr
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

func (s *VirusTotalScanner) scanFile(path string) (*ScanResult, error) {
	if !s.Enabled() {
		return &ScanResult{Passed: true}, nil
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("vt open: %w", err)
	}

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		f.Close()
		return nil, fmt.Errorf("vt hash: %w", err)
	}
	f.Close()

	hash := hex.EncodeToString(h.Sum(nil))
	log.Printf("vt: checking %s (sha256=%s...)", filepath.Base(path), hash[:16])

	malicious, engines, err := s.lookupHash(hash)
	if err != nil {
		log.Printf("vt: lookup %s skipped: %v", filepath.Base(path), err)
		return &ScanResult{Passed: true}, nil
	}

	if malicious > 0 {
		log.Printf("vt: %d/%d flagged %s", malicious, engines, filepath.Base(path))
		return &ScanResult{
			Passed: false,
			Issues: []string{fmt.Sprintf("vt: %d/%d flagged %s", malicious, engines, filepath.Base(path))},
		}, nil
	}
	log.Printf("vt: %s clean (0/%d)", filepath.Base(path), engines)
	return &ScanResult{Passed: true}, nil
}

type vtHashResp struct {
	Data struct {
		Attributes struct {
			LastAnalysisStats map[string]int `json:"last_analysis_stats"`
		} `json:"attributes"`
	} `json:"data"`
}

func (s *VirusTotalScanner) lookupHash(hash string) (malicious, engines int, err error) {
	url := "https://www.virustotal.com/api/v3/files/" + hash
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, 0, err
	}
	req.Header.Set("x-apikey", s.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		time.Sleep(16 * time.Second)
		return s.lookupHash(hash)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, 0, fmt.Errorf("vt: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var result vtHashResp
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, 0, err
	}
	stats := result.Data.Attributes.LastAnalysisStats
	engines = stats["harmless"] + stats["malicious"] + stats["suspicious"] + stats["undetected"]
	return stats["malicious"], engines, nil
}

func (s *VirusTotalScanner) uploadFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("file", filepath.Base(path))
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(fw, f); err != nil {
		return "", err
	}
	w.Close()

	req, err := http.NewRequest("POST", "https://www.virustotal.com/api/v3/files", &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("x-apikey", s.apiKey)
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		time.Sleep(16 * time.Second)
		return s.uploadFile(path)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("upload status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var result struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.Data.ID, nil
}

func (s *VirusTotalScanner) pollAnalysis(id string) (malicious, engines int, err error) {
	for i := 0; i < 20; i++ {
		time.Sleep(3 * time.Second)
		url := "https://www.virustotal.com/api/v3/analyses/" + id
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			continue
		}
		req.Header.Set("x-apikey", s.apiKey)
		req.Header.Set("Accept", "application/json")

		resp, err := s.client.Do(req)
		if err != nil {
			continue
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			time.Sleep(16 * time.Second)
			continue
		}

		var body struct {
			Data struct {
				Attributes struct {
					Status string         `json:"status"`
					Stats  map[string]int `json:"stats"`
				} `json:"attributes"`
			} `json:"data"`
		}
		decodeErr := json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		if decodeErr != nil {
			continue
		}

		if body.Data.Attributes.Status == "completed" {
			stats := body.Data.Attributes.Stats
			engines = stats["harmless"] + stats["malicious"] + stats["suspicious"] + stats["undetected"]
			return stats["malicious"], engines, nil
		}
	}
	return 0, 0, fmt.Errorf("analysis timed out after 60s")
}
