package engine

import (
	"bytes"
	"context"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ShellFunc runs `env -0` in the user's login shell and returns its stdout.
type ShellFunc func(ctx context.Context, shell string) ([]byte, error)

// LoginShell is the ShellFunc that really runs `$SHELL -l -c 'env -0'`.
func LoginShell(ctx context.Context, shell string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, shell, "-l", "-c", "env -0")
	cmd.WaitDelay = time.Second // do not hang on a background child holding the pipe
	return cmd.Output()
}

const pathFallback = "/opt/homebrew/bin:/usr/local/bin"

// LoadLoginEnv sets this process's PATH and KUBECONFIG from the login shell,
// so exec plugins resolve and kubeconfig matches what the user's terminal
// sees. If the shell fails or times out, PATH gets pathFallback prepended and
// KUBECONFIG is left as is (unset means clientcmd's default).
func LoadLoginEnv(run ShellFunc, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var env map[string]string
	out, err := run(ctx, os.Getenv("SHELL"))
	if err == nil {
		env = parseEnv0(out)
	} else {
		log.Printf("engine: login shell env failed, using fallback: %v", err)
	}
	if p := env["PATH"]; p != "" {
		os.Setenv("PATH", p)
	} else {
		os.Setenv("PATH", pathFallback+":"+os.Getenv("PATH"))
	}
	if k := env["KUBECONFIG"]; k != "" {
		os.Setenv("KUBECONFIG", k)
	}
}

// parseEnv0 parses NUL-separated NAME=value entries; values may hold newlines.
func parseEnv0(b []byte) map[string]string {
	env := map[string]string{}
	for _, e := range bytes.Split(b, []byte{0}) {
		if k, v, ok := strings.Cut(string(e), "="); ok {
			env[k] = v
		}
	}
	return env
}
