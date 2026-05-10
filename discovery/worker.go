package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hibiken/asynq"
)

const TypeRegisterSkill = "register_skill"

func NewRegisterSkillTask(id, version string) (*asynq.Task, error) {
	payload, err := json.Marshal(map[string]string{"id": id, "version": version})
	if err != nil {
		return nil, err
	}
	return asynq.NewTask(TypeRegisterSkill, payload), nil
}

func FetchSkillMetadata(id, version string) (SkillSummary, string, error) {
	tmpDir, err := os.MkdirTemp("", "discovery-worker-*")
	if err != nil {
		return SkillSummary{}, "", err
	}

	cleanup := true
	defer func() {
		if cleanup {
			os.RemoveAll(tmpDir)
		}
	}()

	if version == "" {
		version = "latest"
	}

	cmd := exec.Command("skillhub", "fetch", id+"@"+version, tmpDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return SkillSummary{}, "", fmt.Errorf("fetch %s failed: %s", id, string(out))
	}

	var skill SkillSummary
	if err := json.Unmarshal(out, &skill); err != nil {
		return SkillSummary{}, "", err
	}

	cleanup = false
	return skill, tmpDir, nil
}

func HandleRegisterSkill(ctx context.Context, t *asynq.Task, disc *Discovery) error {
	var payload struct {
		ID      string `json:"id"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return err
	}

	log.Printf("worker: processing %s@%s", payload.ID, payload.Version)

	skill, tmpDir, err := FetchSkillMetadata(payload.ID, payload.Version)
	defer os.RemoveAll(tmpDir)
	if err != nil {
		log.Printf("worker: fetch %s failed: %v", payload.ID, err)
		_ = disc.Reject(ctx, payload.ID)
		return nil
	}

	if err := disc.RegisterSkill(ctx, skill); err != nil {
		log.Printf("worker: update metadata %s: %v", payload.ID, err)
	}

	chain := NewChainScanner()
	chain.Add(NewRuleScanner())
	if _, err := exec.LookPath("clamscan"); err == nil {
		chain.Add(NewClamAVScanner())
	}
	vt := NewVirusTotalScanner()
	if vt.Enabled() {
		chain.Add(vt)
	}
	scanResult, scanErr := chain.ScanDir(tmpDir)
	if scanErr != nil {
		log.Printf("worker: scan %s: %v", payload.ID, scanErr)
		_ = disc.Reject(ctx, payload.ID)
		return nil
	}
	if !scanResult.Passed {
		log.Printf("worker: security reject %s: %s", payload.ID, strings.Join(scanResult.Issues, "; "))
		_ = disc.Reject(ctx, payload.ID)
		return nil
	}

	if disc.llm != nil {
		bodyBytes, _ := os.ReadFile(filepath.Join(tmpDir, "SKILL.md"))
		result, err := disc.llm.Review(ctx, skill, string(bodyBytes))
		if err != nil {
			log.Printf("worker: LLM error %s: %v", payload.ID, err)
			_ = disc.Reject(ctx, payload.ID)
			return nil
		}
		if !result.Passed {
			log.Printf("worker: LLM reject %s: %s", payload.ID, result.Reason)
			_ = disc.Reject(ctx, payload.ID)
			return nil
		}
		log.Printf("worker: LLM pass %s: %s", payload.ID, result.Reason)
		log.Printf("worker: LLM response %s: %s", payload.ID, result.Detail)
	}

	if err := disc.Approve(ctx, payload.ID); err != nil {
		log.Printf("worker: approve %s: %v", payload.ID, err)
		return nil
	}
	log.Printf("worker: approved %s", payload.ID)
	return nil
}
