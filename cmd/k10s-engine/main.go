// Command k10s-engine is the k10s engine: it speaks ndjson over stdin/stdout
// and logs to stderr.
package main

import (
	"context"
	"log"
	"os"

	"github.com/mechanomythia/k10s/internal/engine"
)

func main() {
	log.SetFlags(log.LstdFlags)
	log.SetOutput(os.Stderr)
	if err := engine.New().Serve(context.Background(), os.Stdin, os.Stdout); err != nil {
		log.Fatalf("k10s-engine: %v", err)
	}
}
