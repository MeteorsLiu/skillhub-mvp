package cache

import (
	_ "embed"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/go-ego/gse"
)

//go:embed jieba.dict.txt
var jiebaDict string

var asciiTokenRE = regexp.MustCompile(`[A-Za-z0-9_+#.-]+`)

type Tokenizer struct {
	seg gse.Segmenter
}

func NewTokenizer() (*Tokenizer, error) {
	var tok Tokenizer
	if err := tok.seg.LoadDictEmbed(jiebaDict); err != nil {
		return nil, err
	}
	return &tok, nil
}

func (t *Tokenizer) Tokens(text string) []string {
	seen := map[string]bool{}
	tokens := make([]string, 0, 16)
	add := func(token string) {
		token = strings.TrimSpace(strings.ToLower(token))
		if len([]rune(token)) < 2 || seen[token] {
			return
		}
		seen[token] = true
		tokens = append(tokens, token)
	}
	for _, token := range t.seg.CutSearch(text, true) {
		add(token)
	}
	for _, token := range asciiTokenRE.FindAllString(text, -1) {
		add(token)
	}
	return tokens
}

func ftsMatchExpr(tokens []string) string {
	if len(tokens) > 24 {
		tokens = tokens[:24]
	}
	parts := make([]string, 0, len(tokens))
	for _, token := range tokens {
		encoded, _ := json.Marshal(token)
		parts = append(parts, string(encoded))
	}
	return strings.Join(parts, " OR ")
}
