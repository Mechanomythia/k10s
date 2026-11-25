package engine

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// isolate points HOME and KUBECONFIG at temp dirs so tests never see the real ones.
func isolate(t *testing.T) (home string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("KUBECONFIG", filepath.Join(home, "none"))
	return home
}

func writeKube(t *testing.T, path, current string, ctxs map[string]string, server string) {
	t.Helper()
	var b strings.Builder
	fmt.Fprintf(&b, "apiVersion: v1\nkind: Config\ncurrent-context: %q\n", current)
	b.WriteString("clusters:\n")
	for n := range ctxs {
		fmt.Fprintf(&b, "- name: c-%s\n  cluster: {server: %q}\n", n, server)
	}
	b.WriteString("users:\n")
	for n := range ctxs {
		fmt.Fprintf(&b, "- name: u-%s\n  user: {token: fake}\n", n)
	}
	b.WriteString("contexts:\n")
	for n, ns := range ctxs {
		fmt.Fprintf(&b, "- name: %s\n  context: {cluster: c-%s, user: u-%s, namespace: %q}\n", n, n, n, ns)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestParseEnv0(t *testing.T) {
	env := parseEnv0([]byte("PATH=/a:/b\x00MULTI=one\ntwo\x00EMPTY=\x00KUBECONFIG=/x=y\x00"))
	if env["PATH"] != "/a:/b" || env["MULTI"] != "one\ntwo" || env["KUBECONFIG"] != "/x=y" {
		t.Errorf("got %q", env)
	}
}

func TestLoadLoginEnv(t *testing.T) {
	t.Run("from shell", func(t *testing.T) {
		t.Setenv("PATH", "/orig")
		t.Setenv("KUBECONFIG", "")
		LoadLoginEnv(func(context.Context, string) ([]byte, error) {
			return []byte("PATH=/shell/bin\x00KUBECONFIG=/k1:/k2\x00"), nil
		}, time.Second)
		if os.Getenv("PATH") != "/shell/bin" || os.Getenv("KUBECONFIG") != "/k1:/k2" {
			t.Errorf("PATH=%q KUBECONFIG=%q", os.Getenv("PATH"), os.Getenv("KUBECONFIG"))
		}
	})
	t.Run("timeout falls back", func(t *testing.T) {
		t.Setenv("PATH", "/orig")
		t.Setenv("KUBECONFIG", "")
		start := time.Now()
		LoadLoginEnv(func(ctx context.Context, _ string) ([]byte, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}, 50*time.Millisecond)
		if got := os.Getenv("PATH"); got != "/opt/homebrew/bin:/usr/local/bin:/orig" {
			t.Errorf("PATH=%q", got)
		}
		if os.Getenv("KUBECONFIG") != "" || time.Since(start) > 2*time.Second {
			t.Errorf("KUBECONFIG=%q after %v", os.Getenv("KUBECONFIG"), time.Since(start))
		}
	})
	t.Run("shell error falls back", func(t *testing.T) {
		t.Setenv("PATH", "/orig")
		LoadLoginEnv(func(context.Context, string) ([]byte, error) { return nil, errors.New("boom") }, time.Second)
		if !strings.HasSuffix(os.Getenv("PATH"), ":/orig") {
			t.Errorf("PATH=%q", os.Getenv("PATH"))
		}
	})
}

func TestIrisPathsFiltered(t *testing.T) {
	home := isolate(t)
	iris := filepath.Join(home, ".local/share/iris/claude/kubeconfig")
	mine := filepath.Join(home, "mine")
	t.Setenv("KUBECONFIG", iris+string(os.PathListSeparator)+mine)
	if got := NewKubeconfig().paths; len(got) != 1 || got[0] != mine {
		t.Errorf("paths = %v", got)
	}
}

func TestContextsMerged(t *testing.T) {
	home := isolate(t)
	a, b := filepath.Join(home, "a"), filepath.Join(home, "b")
	writeKube(t, a, "dev", map[string]string{"dev": "apps"}, "https://a.invalid")
	writeKube(t, b, "prod", map[string]string{"prod": ""}, "https://b.invalid")
	t.Setenv("KUBECONFIG", a+string(os.PathListSeparator)+b)
	got, err := NewKubeconfig().Contexts()
	if err != nil {
		t.Fatal(err)
	}
	want := []ContextInfo{
		{Name: "dev", Cluster: "c-dev", User: "u-dev", Namespace: "apps", Current: true},
		{Name: "prod", Cluster: "c-prod", User: "u-prod"},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestKubeconfigWatch(t *testing.T) {
	home := isolate(t)
	f := filepath.Join(home, "cfg")
	writeKube(t, f, "dev", map[string]string{"dev": ""}, "https://a.invalid")
	t.Setenv("KUBECONFIG", f)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := NewKubeconfig().Watch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Rename-replace, as editors do.
	tmp := filepath.Join(home, "cfg.tmp")
	writeKube(t, tmp, "dev", map[string]string{"dev": "", "new": ""}, "https://a.invalid")
	if err := os.Rename(tmp, f); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("no change notification")
	}
}

func TestConnsState(t *testing.T) {
	home := isolate(t)
	unauth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer unauth.Close()
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/version" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"major":"1","minor":"31","gitVersion":"v1.31.0"}`)
	}))
	defer ok.Close()
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	closed := "http://" + l.Addr().String()
	l.Close()

	tests := []struct {
		name, server string
		want         ContextState
	}{
		{"unauthorized", unauth.URL, StateUnauthorized},
		{"unreachable", closed, StateUnreachable},
		{"live", ok.URL, StateLive},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := filepath.Join(home, tt.name)
			writeKube(t, f, tt.name, map[string]string{tt.name: ""}, tt.server)
			t.Setenv("KUBECONFIG", f)
			c := NewConns(NewKubeconfig())
			var states []ContextState
			c.OnState = func(s ContextStateChange) { states = append(states, s.State) }
			cn, err := c.Get(context.Background(), tt.name)
			if (err == nil) != (tt.want == StateLive) || (cn != nil) != (err == nil) {
				t.Fatalf("conn=%v err=%v", cn, err)
			}
			if len(states) != 2 || states[0] != StateConnecting || states[1] != tt.want {
				t.Errorf("states = %v, want connecting then %s", states, tt.want)
			}
			if tt.want == StateLive {
				// Cached: no new transitions; Reset drops it.
				if cn2, _ := c.Get(context.Background(), tt.name); cn2 != cn || len(states) != 2 {
					t.Error("live conn not cached")
				}
				c.Reset()
				if cn3, _ := c.Get(context.Background(), tt.name); cn3 == cn {
					t.Error("Reset kept conn")
				}
			}
		})
	}
}

func TestRedact(t *testing.T) {
	got := redact("Authorization: Bearer abc123 and " + strings.Repeat("x", 40))
	if strings.Contains(got, "abc123") || strings.Contains(got, "xxxx") {
		t.Errorf("not redacted: %q", got)
	}
}
