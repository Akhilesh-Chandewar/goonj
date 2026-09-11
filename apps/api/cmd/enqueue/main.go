// Command enqueue is a small ops utility: it enqueues one task payload onto
// the Asynq queue so idempotent pipeline steps can be re-run without
// re-uploading audio — e.g. audio:embed after changing the embedder, or
// audio:transcribe after configuring a transcriber.
//
// Usage:
//
//	enqueue <task-type> '<json-payload>'
//
// Examples:
//
//	REDIS_URL=redis://localhost:16380/0 go run ./cmd/enqueue \
//	  audio:embed '{"audio_id":"0197..."}'
//
//	REDIS_URL=redis://localhost:16380/0 go run ./cmd/enqueue \
//	  audio:transcribe '{"audio_id":"0197...","storage_key":"audio/processed/0197.../medium.mp3"}'
package main

import (
	"fmt"
	"os"

	"github.com/hibiken/asynq"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: enqueue <task-type> '<json-payload>'")
		os.Exit(2)
	}
	typ, payload := os.Args[1], os.Args[2]

	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "redis://localhost:6379/0"
	}
	opt, err := asynq.ParseRedisURI(redisURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bad REDIS_URL: %v\n", err)
		os.Exit(1)
	}
	client := asynq.NewClient(opt)
	defer client.Close()

	task := asynq.NewTask(typ, []byte(payload))
	info, err := client.Enqueue(task)
	if err != nil {
		fmt.Fprintf(os.Stderr, "enqueue failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("enqueued %s id=%s queue=%s max_retry=%d\n",
		info.Type, info.ID, info.Queue, info.MaxRetry)
}
