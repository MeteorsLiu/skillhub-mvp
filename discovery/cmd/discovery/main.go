package main

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/hibiken/asynq"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"discovery"
)

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	port := getEnv("DISCOVERY_PORT", "8399")
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		fmt.Fprintln(os.Stderr, "DATABASE_URL is required")
		os.Exit(1)
	}

	redisAddr := getEnv("REDIS_ADDR", "localhost:6379")
	redisOpt := asynq.RedisClientOpt{Addr: redisAddr}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "database: %v\n", err)
		os.Exit(1)
	}

	var llm discovery.LLMReviewer
	switch os.Getenv("LLM_PROVIDER") {
	case "anthropic":
		llm = discovery.NewAnthropicReviewer()
	default:
		llm = discovery.NewOpenAIReviewer()
	}

	var embedder discovery.Embedder
	if embedURL := os.Getenv("SKILLHUB_EMBED_URL"); embedURL != "" {
		embedder = discovery.NewHTTPEmbedder(embedURL)
	}

	disc := discovery.NewWithEmbedder(db, llm, embedder)
	if err := disc.Init(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "init: %v\n", err)
		os.Exit(1)
	}

	queueClient := asynq.NewClient(redisOpt)
	defer queueClient.Close()

	srv := discovery.NewServer(disc, queueClient)

	// Asynq worker
	queueServer := asynq.NewServer(redisOpt, asynq.Config{Concurrency: 2})
	mux := asynq.NewServeMux()
	mux.HandleFunc(discovery.TypeRegisterSkill, func(ctx context.Context, t *asynq.Task) error {
		return discovery.HandleRegisterSkill(ctx, t, disc)
	})

	go func() {
		fmt.Fprintf(os.Stderr, "queue worker started\n")
		if err := queueServer.Run(mux); err != nil {
			fmt.Fprintf(os.Stderr, "queue server: %v\n", err)
			os.Exit(1)
		}
	}()

	addr := ":" + port
	fmt.Fprintf(os.Stderr, "discovery listening on %s\n", addr)
	if err := http.ListenAndServe(addr, srv); err != nil {
		fmt.Fprintf(os.Stderr, "server: %v\n", err)
		os.Exit(1)
	}
}
