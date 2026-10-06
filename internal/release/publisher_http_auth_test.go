// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package release

// Эти UNIT-контроли вызывают настоящий default HTTP adapter. Исполняемый gh
// синтетический, как и RoundTripper: это явно не реальные credentials, не
// сетевое подтверждение GitHub и не product Go/compiler prerequisite.

import (
	"context"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

const rsHTTPAuthUnitToken = "synthetic-unit-credential-718901"
const rsHTTPAuthUnitSecretMarker = "SYNTHETIC_AUTH_PRIVATE_DIAGNOSTIC_2672"

type rsHTTPAuthUnitTransport struct {
	mu            sync.Mutex
	calls         int
	authorization []string
	hosts         []string
	redirect      bool
}

func (r *rsHTTPAuthUnitTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	r.calls++
	r.authorization = append(r.authorization, req.Header.Get("Authorization"))
	r.hosts = append(r.hosts, req.URL.Host)
	r.mu.Unlock()
	status := 200
	headers := make(http.Header)
	if r.redirect {
		status = 302
		headers.Set("Location", "https://credential-sink.invalid/never-send")
	}
	return &http.Response{StatusCode: status, Header: headers, Body: io.NopCloser(strings.NewReader("{}")), Request: req}, nil
}

type rsHTTPAuthUnitFixture struct{ root, gh, argv, env, pid string }

func rsHTTPAuthFixture(t *testing.T) rsHTTPAuthUnitFixture {
	t.Helper()
	dir := t.TempDir()
	f := rsHTTPAuthUnitFixture{dir, filepath.Join(dir, "gh"), filepath.Join(dir, "argv"), filepath.Join(dir, "env"), filepath.Join(dir, "pid")}
	// Только shell builtins и exec sleep по абсолютному пути. Никаких login,
	// чтений реального gh config/keyring, сети либо унаследованных credentials.
	script := `#!/bin/sh
[ "$#" = 4 ] && [ "$1" = auth ] && [ "$2" = token ] && [ "$3" = --hostname ] && [ "$4" = github.com ] || exit 93
printf '%s\n' "$@" > "$CI_RS_AUTH_UNIT_ARGV"
printf '%s' "$GH_PROMPT_DISABLED" > "$CI_RS_AUTH_UNIT_ENV"
printf '%s' "$$" > "$CI_RS_AUTH_UNIT_PID"
if [ "$CI_RS_AUTH_UNIT_HANG" = 1 ]; then exec /bin/sleep 30; fi
printf '%s' "$CI_RS_AUTH_UNIT_STDOUT"
printf '%s' "$CI_RS_AUTH_UNIT_STDERR" >&2
exit "$CI_RS_AUTH_UNIT_EXIT"
`
	if err := os.WriteFile(f.gh, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_HOST", "unrelated-unit-host.invalid")
	t.Setenv("GH_PROMPT_DISABLED", "")
	t.Setenv("CI_RS_AUTH_UNIT_ARGV", f.argv)
	t.Setenv("CI_RS_AUTH_UNIT_ENV", f.env)
	t.Setenv("CI_RS_AUTH_UNIT_PID", f.pid)
	t.Setenv("CI_RS_AUTH_UNIT_STDOUT", rsHTTPAuthUnitToken+"\n")
	t.Setenv("CI_RS_AUTH_UNIT_STDERR", rsHTTPAuthUnitSecretMarker)
	t.Setenv("CI_RS_AUTH_UNIT_EXIT", "0")
	t.Setenv("CI_RS_AUTH_UNIT_HANG", "")
	t.Cleanup(func() {
		b, err := os.ReadFile(f.pid)
		if err != nil {
			return
		}
		pid, err := strconv.Atoi(string(b))
		if err != nil {
			return
		}
		// Cleanup разрешает сигнал только собственному PID этой фикстуры.
		if syscall.Kill(pid, 0) == nil {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})
	return f
}

func rsHTTPAuthNoChild(t *testing.T, f rsHTTPAuthUnitFixture) {
	t.Helper()
	if _, err := os.Stat(f.argv); !os.IsNotExist(err) {
		t.Errorf("UNIT credential child unexpectedly invoked: %v", err)
	}
}

func rsHTTPAuthChildAbsent(t *testing.T, f rsHTTPAuthUnitFixture) {
	t.Helper()
	b, err := os.ReadFile(f.pid)
	if err != nil {
		t.Errorf("UNIT child PID proof absent: %v", err)
		return
	}
	pid, err := strconv.Atoi(string(b))
	if err != nil {
		t.Fatal(err)
	}
	if err = syscall.Kill(pid, 0); err != syscall.ESRCH {
		t.Errorf("UNIT own child not reaped: pid=%d err=%v", pid, err)
	}
}

func rsHTTPAuthExactChild(t *testing.T, f rsHTTPAuthUnitFixture) {
	t.Helper()
	b, err := os.ReadFile(f.argv)
	if err != nil {
		t.Errorf("UNIT fixed gh invocation absent: %v", err)
		return
	}
	if string(b) != "auth\ntoken\n--hostname\ngithub.com\n" {
		t.Errorf("UNIT wrong fixed argv: %q", b)
	}
	b, err = os.ReadFile(f.env)
	if err != nil || string(b) != "1" {
		t.Errorf("UNIT GH_PROMPT_DISABLED not fixed: %q err=%v", b, err)
	}
	rsHTTPAuthChildAbsent(t, f)
}

func rsHTTPAuthCapture(t *testing.T, call func() (*http.Response, error)) (*http.Response, error, string) {
	t.Helper()
	out, err := os.CreateTemp(t.TempDir(), "unit-parent-output-")
	if err != nil {
		t.Fatal(err)
	}
	originalOut, originalErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = out, out
	defer func() { os.Stdout, os.Stderr = originalOut, originalErr; out.Close() }()
	response, callErr := call()
	if _, err = out.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(out)
	if err != nil {
		t.Fatal(err)
	}
	return response, callErr, string(data)
}

func rsHTTPAuthCall(t *testing.T, ctx context.Context, address string, rt *rsHTTPAuthUnitTransport) (*http.Response, error, string) {
	t.Helper()
	old := http.DefaultTransport
	http.DefaultTransport = rt
	t.Cleanup(func() { http.DefaultTransport = old })
	req, err := http.NewRequestWithContext(ctx, "GET", address, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Unit-Original", "unchanged")
	response, callErr, output := rsHTTPAuthCapture(t, func() (*http.Response, error) { return supplyRuntimeHTTP(ctx, req) })
	if req.Header.Get("Authorization") != "" || req.Header.Get("X-Unit-Original") != "unchanged" {
		t.Error("UNIT adapter mutated caller request")
	}
	if response != nil && response.Body != nil {
		response.Body.Close()
	}
	if output != "" {
		t.Errorf("UNIT private command output escaped parent stdout/stderr (%d bytes)", len(output))
	}
	if callErr != nil && (strings.Contains(callErr.Error(), rsHTTPAuthUnitSecretMarker) || strings.Contains(callErr.Error(), rsHTTPAuthUnitToken)) {
		t.Error("UNIT private marker leaked through returned error")
	}
	return response, callErr, output
}

func TestPublisherHTTPAuthUnitFixture(t *testing.T) {
	f := rsHTTPAuthFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, f.gh, "auth", "token", "--hostname", "github.com")
	cmd.Env = append(os.Environ(), "GH_PROMPT_DISABLED=1")
	out, err := cmd.Output()
	if err != nil || string(out) != rsHTTPAuthUnitToken+"\n" {
		t.Fatalf("UNIT fixture prerequisite failed: rc-error=%v bytes=%d", err, len(out))
	}
	rsHTTPAuthExactChild(t, f)
	if _, err = os.Stat("/bin/sleep"); err != nil {
		t.Fatal(err)
	}
	t.Log("UNIT prerequisite: synthetic child, exact argv/stdout, no real credentials or HTTP")
}

func TestPublisherHTTPAuthBoundary(t *testing.T) {
	refusalMessages := map[string]string{}
	for _, c := range []struct{ name, gh, github, want string }{
		{"nonempty-gh-wins", "unit-gh-first", "unit-github-second", "unit-gh-first"},
		{"empty-gh-uses-github", "", "unit-github-second", "unit-github-second"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := rsHTTPAuthFixture(t)
			t.Setenv("GH_TOKEN", c.gh)
			t.Setenv("GITHUB_TOKEN", c.github)
			rt := &rsHTTPAuthUnitTransport{}
			r, err, _ := rsHTTPAuthCall(t, context.Background(), "https://api.github.com/user", rt)
			if err != nil || r == nil || rt.calls != 1 || len(rt.authorization) != 1 || rt.authorization[0] != "Bearer "+c.want {
				t.Errorf("UNIT env precedence failed: err=%v calls=%d", err, rt.calls)
			}
			rsHTTPAuthNoChild(t, f)
		})
	}
	for _, c := range []struct {
		name, output string
		unset        bool
	}{
		{"both-empty-fixed-gh-one-lf", rsHTTPAuthUnitToken + "\n", false},
		{"both-absent-fixed-gh-one-lf", rsHTTPAuthUnitToken + "\n", true},
		{"gh-no-final-lf", rsHTTPAuthUnitToken, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := rsHTTPAuthFixture(t)
			if c.unset {
				os.Unsetenv("GH_TOKEN")
				os.Unsetenv("GITHUB_TOKEN")
			}
			t.Setenv("CI_RS_AUTH_UNIT_STDOUT", c.output)
			rt := &rsHTTPAuthUnitTransport{}
			r, err, _ := rsHTTPAuthCall(t, context.Background(), "https://api.github.com/user", rt)
			if err != nil || r == nil || rt.calls != 1 || len(rt.authorization) != 1 || rt.authorization[0] != "Bearer "+rsHTTPAuthUnitToken {
				t.Errorf("UNIT fixed gh fallback failed: err=%v calls=%d", err, rt.calls)
			}
			rsHTTPAuthExactChild(t, f)
		})
	}
	for _, c := range []struct{ name, gh, github string }{
		{"malformed-nonempty-gh-no-fallback", rsHTTPAuthUnitToken + "\ninvalid", "unit-github-valid"},
		{"malformed-nonempty-github-no-fallback", "", rsHTTPAuthUnitToken + " invalid"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := rsHTTPAuthFixture(t)
			t.Setenv("GH_TOKEN", c.gh)
			t.Setenv("GITHUB_TOKEN", c.github)
			rt := &rsHTTPAuthUnitTransport{}
			r, err, _ := rsHTTPAuthCall(t, context.Background(), "https://api.github.com/user", rt)
			if err == nil || r != nil || rt.calls != 0 {
				t.Errorf("UNIT malformed explicit token admitted: err=%v calls=%d", err, rt.calls)
			}
			if err != nil {
				refusalMessages[c.name] = err.Error()
			}
			rsHTTPAuthNoChild(t, f)
		})
	}
	for _, c := range []struct{ name, output, exit string }{
		{"gh-embedded-lf", rsHTTPAuthUnitToken + "\nextra\n", "0"},
		{"gh-embedded-cr", rsHTTPAuthUnitToken + "\rextra\n", "0"},
		{"gh-space", rsHTTPAuthUnitToken + " extra\n", "0"},
		{"gh-leading-tab", "\t" + rsHTTPAuthUnitToken + "\n", "0"},
		{"gh-double-final-lf", rsHTTPAuthUnitToken + "\n\n", "0"},
		{"gh-empty", "", "0"},
		{"gh-nonzero-private-stderr", rsHTTPAuthUnitToken + "\n", "17"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := rsHTTPAuthFixture(t)
			t.Setenv("CI_RS_AUTH_UNIT_STDOUT", c.output)
			t.Setenv("CI_RS_AUTH_UNIT_EXIT", c.exit)
			rt := &rsHTTPAuthUnitTransport{}
			r, err, _ := rsHTTPAuthCall(t, context.Background(), "https://api.github.com/user", rt)
			if err == nil || r != nil || rt.calls != 0 {
				t.Errorf("UNIT bad credential output admitted: err=%v calls=%d", err, rt.calls)
			}
			if err != nil {
				refusalMessages[c.name] = err.Error()
			}
			rsHTTPAuthExactChild(t, f)
		})
	}
	t.Run("gh-unavailable", func(t *testing.T) {
		f := rsHTTPAuthFixture(t)
		if err := os.Remove(f.gh); err != nil {
			t.Fatal(err)
		}
		rt := &rsHTTPAuthUnitTransport{}
		r, err, _ := rsHTTPAuthCall(t, context.Background(), "https://api.github.com/user", rt)
		if err == nil || r != nil || rt.calls != 0 {
			t.Errorf("UNIT missing gh did not refuse: err=%v calls=%d", err, rt.calls)
		}
		if err != nil {
			refusalMessages["gh-unavailable"] = err.Error()
		}
		rsHTTPAuthNoChild(t, f)
	})
	for _, address := range []string{"http://api.github.com/user", "https://api.github.com.evil.invalid/user", "https://api.github.com:443/user", "https://user@api.github.com/user"} {
		t.Run("unsupported-"+address, func(t *testing.T) {
			f := rsHTTPAuthFixture(t)
			rt := &rsHTTPAuthUnitTransport{}
			r, err, _ := rsHTTPAuthCall(t, context.Background(), address, rt)
			if err == nil || r != nil || rt.calls != 0 {
				t.Errorf("UNIT unsupported endpoint admitted: err=%v calls=%d", err, rt.calls)
			}
			rsHTTPAuthNoChild(t, f)
		})
	}
	t.Run("proxy-never-gets-token", func(t *testing.T) {
		f := rsHTTPAuthFixture(t)
		t.Setenv("GH_TOKEN", rsHTTPAuthUnitToken)
		rt := &rsHTTPAuthUnitTransport{}
		r, err, _ := rsHTTPAuthCall(t, context.Background(), "https://proxy.golang.org/example/@v/v1.0.0.info", rt)
		if err != nil || r == nil || rt.calls != 1 || len(rt.authorization) != 1 || rt.authorization[0] != "" {
			t.Errorf("UNIT proxy credential isolation failed: err=%v calls=%d", err, rt.calls)
		}
		rsHTTPAuthNoChild(t, f)
	})
	t.Run("proxy-empty-env-no-fallback", func(t *testing.T) {
		f := rsHTTPAuthFixture(t)
		rt := &rsHTTPAuthUnitTransport{}
		r, err, _ := rsHTTPAuthCall(t, context.Background(), "https://proxy.golang.org/example/@v/v1.0.0.info", rt)
		if err != nil || r == nil || rt.calls != 1 || len(rt.authorization) != 1 || rt.authorization[0] != "" {
			t.Errorf("UNIT proxy used fallback auth: err=%v calls=%d", err, rt.calls)
		}
		rsHTTPAuthNoChild(t, f)
	})
	t.Run("fallback-redirect-is-not-followed", func(t *testing.T) {
		f := rsHTTPAuthFixture(t)
		rt := &rsHTTPAuthUnitTransport{redirect: true}
		r, err, _ := rsHTTPAuthCall(t, context.Background(), "https://api.github.com/user", rt)
		if err != nil || r == nil || r.StatusCode != 302 || rt.calls != 1 || len(rt.hosts) != 1 || rt.hosts[0] != "api.github.com" || len(rt.authorization) != 1 || rt.authorization[0] != "Bearer "+rsHTTPAuthUnitToken {
			t.Errorf("UNIT fallback redirect contract failed: err=%v calls=%d", err, rt.calls)
		}
		rsHTTPAuthExactChild(t, f)
	})
	t.Run("redirect-is-not-followed", func(t *testing.T) {
		f := rsHTTPAuthFixture(t)
		t.Setenv("GH_TOKEN", rsHTTPAuthUnitToken)
		rt := &rsHTTPAuthUnitTransport{redirect: true}
		r, err, _ := rsHTTPAuthCall(t, context.Background(), "https://api.github.com/user", rt)
		if err != nil || r == nil || r.StatusCode != 302 || rt.calls != 1 || len(rt.hosts) != 1 || rt.hosts[0] != "api.github.com" {
			t.Errorf("UNIT redirect forwarded: err=%v calls=%d", err, rt.calls)
		}
		rsHTTPAuthNoChild(t, f)
	})
	t.Run("canceled-context-no-child-no-send", func(t *testing.T) {
		f := rsHTTPAuthFixture(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		rt := &rsHTTPAuthUnitTransport{}
		r, err, _ := rsHTTPAuthCall(t, ctx, "https://api.github.com/user", rt)
		if err == nil || r != nil || rt.calls != 0 {
			t.Errorf("UNIT canceled request admitted: err=%v calls=%d", err, rt.calls)
		}
		rsHTTPAuthNoChild(t, f)
	})
	t.Run("running-context-cancels-own-child", func(t *testing.T) {
		f := rsHTTPAuthFixture(t)
		t.Setenv("CI_RS_AUTH_UNIT_HANG", "1")
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		rt := &rsHTTPAuthUnitTransport{}
		old := http.DefaultTransport
		http.DefaultTransport = rt
		defer func() { http.DefaultTransport = old }()
		req, err := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/user", nil)
		if err != nil {
			t.Fatal(err)
		}
		type result struct {
			response *http.Response
			err      error
		}
		done := make(chan result, 1)
		finished := make(chan struct{})
		go func() { defer close(finished); r, e := supplyRuntimeHTTP(ctx, req); done <- result{r, e} }()
		defer func() {
			cancel()
			select {
			case <-finished:
				return
			default:
			}
			if b, e := os.ReadFile(f.pid); e == nil {
				if pid, e := strconv.Atoi(string(b)); e == nil {
					_ = syscall.Kill(pid, syscall.SIGKILL)
				}
			}
			select {
			case <-finished:
			case <-time.After(2 * time.Second):
				t.Error("UNIT owned adapter goroutine did not finish after cleanup")
			}
		}()
		deadline := time.Now().Add(2 * time.Second)
		started := false
		for time.Now().Before(deadline) {
			if _, e := os.Stat(f.pid); e == nil {
				started = true
				break
			}
			select {
			case got := <-done:
				if got.response != nil && got.response.Body != nil {
					got.response.Body.Close()
				}
				t.Fatalf("UNIT adapter returned before credential child started: err=%v", got.err)
			default:
			}
			time.Sleep(5 * time.Millisecond)
		}
		cancel()
		if !started {
			t.Error("UNIT actual synthetic child did not start")
		}
		select {
		case got := <-done:
			if got.response != nil && got.response.Body != nil {
				got.response.Body.Close()
			}
			if got.err == nil || got.response != nil {
				t.Errorf("UNIT child cancellation not refused: %v", got.err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("UNIT credential child cancellation exceeded bounded wait")
		}
		rt.mu.Lock()
		calls := rt.calls
		rt.mu.Unlock()
		if calls != 0 {
			t.Errorf("UNIT canceled auth sent HTTP: %d", calls)
		}
		if started {
			rsHTTPAuthExactChild(t, f)
		}
	})
	if len(refusalMessages) != 10 {
		t.Errorf("UNIT fixed refusal census: got=%d want=10", len(refusalMessages))
	}
	var fixed string
	for _, message := range refusalMessages {
		if fixed == "" {
			fixed = message
		}
		if message == "" || message != fixed {
			t.Error("UNIT auth failures do not share one fixed sanitized error")
		}
	}
	t.Log("UNIT boundary complete; no live credentials, GitHub network, login or configuration writes")
}
