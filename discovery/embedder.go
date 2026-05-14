package discovery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type HTTPEmbedder struct {
	url  string
	http *http.Client
}

func NewHTTPEmbedder(url string) *HTTPEmbedder {
	return &HTTPEmbedder{
		url:  url,
		http: &http.Client{Timeout: 10 * time.Second},
	}
}

func (e *HTTPEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	body, err := json.Marshal(map[string]any{"input": texts})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embedding API returned %d", resp.StatusCode)
	}

	var out struct {
		Embeddings [][]float32 `json:"embeddings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if len(out.Embeddings) != len(texts) {
		return nil, fmt.Errorf("embedding API returned %d vectors for %d texts", len(out.Embeddings), len(texts))
	}
	for i, embedding := range out.Embeddings {
		if len(embedding) != EmbeddingDimensions {
			return nil, fmt.Errorf("embedding %d has %d dimensions, expected %d", i, len(embedding), EmbeddingDimensions)
		}
	}
	return out.Embeddings, nil
}
