// Command k10s-engine is the k10s engine: it speaks ndjson over stdin/stdout
// and logs to stderr.
package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/mechanomythia/k10s/internal/engine"
)

func main() {
	log.SetFlags(log.LstdFlags)
	log.SetOutput(os.Stderr)
	engine.LoadLoginEnv(engine.LoginShell, 5*time.Second)
	kube := engine.NewKubeconfig()
	conns := engine.NewConns(kube)
	ctx := context.Background()
	if changed, err := kube.Watch(ctx); err != nil {
		log.Printf("k10s-engine: kubeconfig watch: %v", err)
	} else {
		go func() {
			for range changed {
				conns.Reset()
			}
		}()
	}
	s := engine.New()
	kube.Register(s)
	conns.Register(s)
	engine.NewTables(conns).Register(s)
	if err := s.Serve(ctx, os.Stdin, os.Stdout); err != nil {
		log.Fatalf("k10s-engine: %v", err)
	}
}
