// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package release

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	"golang.org/x/mod/sumdb"
	"golang.org/x/mod/sumdb/dirhash"
	"golang.org/x/mod/sumdb/note"
)

// The fixture's only command transformation maps the two fixed public service
// coordinates to owned servers and their ephemeral public verification key.
// This exact adapter is also extracted into the isolated product bridge.
type rsChecksumMapping struct {
	Proxy   string `json:"proxy"`
	SumDB   string `json:"sumdb"`
	Module  string `json:"module"`
	Version string `json:"version"`
	Go      string `json:"go"`
	Root    string `json:"root"`
}

func rsChecksumSHA(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func rsChecksumWithin(root, path string) bool {
	r, e := filepath.Rel(root, path)
	return e == nil && r != "." && !filepath.IsAbs(r) && r != ".." && !strings.HasPrefix(r, ".."+string(os.PathSeparator))
}
func rsChecksumExecute(ctx context.Context, program string, args []string, dir string, env, declared []string, stdin []byte, fixture rsChecksumMapping, output, label string) ([]byte, []byte, int, error) {
	values := map[string]string{}
	explicit := map[string]string{}
	for _, v := range env {
		k, x, ok := strings.Cut(v, "=")
		if ok {
			values[k] = x
		}
	}
	for _, v := range declared {
		k, x, ok := strings.Cut(v, "=")
		if ok {
			explicit[k] = x
		}
	}
	fixed := map[string]string{"GOWORK": "off", "GOENV": "off", "GOFLAGS": "", "GOTOOLCHAIN": "local", "GOPROXY": "https://proxy.golang.org", "GOSUMDB": "sum.golang.org", "GOPRIVATE": "", "GONOPROXY": "", "GONOSUMDB": ""}
	original := map[string]string{}
	actual := map[string]string{}
	declaredSafe := map[string]string{}
	for _, key := range []string{"GOWORK", "GOENV", "GOFLAGS", "GOTOOLCHAIN", "GOPROXY", "GOSUMDB", "GOPRIVATE", "GONOPROXY", "GONOSUMDB", "GOMODCACHE", "GOPATH", "HOME", "TMPDIR"} {
		if v, ok := values[key]; ok {
			original[key] = v
			actual[key] = v
		}
		if v, ok := explicit[key]; ok {
			declaredSafe[key] = v
		}
	}
	violations := []string{}
	if program != fixture.Go || !reflect.DeepEqual(args, []string{"mod", "download", "-json", fixture.Module + "@" + fixture.Version}) || len(stdin) != 0 {
		violations = append(violations, "exact resolved Go argv/stdin")
	}
	for k, want := range fixed {
		got, ok := explicit[k]
		if !ok || got != want || values[k] != want {
			violations = append(violations, "explicit environment "+k)
		}
	}
	cache := values["GOMODCACHE"]
	cacheEntries, cacheErr := os.ReadDir(cache)
	if explicit["GOMODCACHE"] != cache || !rsChecksumWithin(fixture.Root, cache) || cacheErr != nil || len(cacheEntries) != 0 {
		violations = append(violations, "new empty owned module cache")
	}
	entries, dirErr := os.ReadDir(dir)
	if !rsChecksumWithin(fixture.Root, dir) || dirErr != nil || len(entries) != 1 || entries[0].Name() != "go.mod" {
		violations = append(violations, "new minimal owned probe directory")
	}
	modBytes, modErr := os.ReadFile(filepath.Join(dir, "go.mod"))
	mf, parseErr := modfile.Parse("go.mod", modBytes, nil)
	if modErr != nil || parseErr != nil || mf == nil || mf.Module == nil || mf.Module.Mod.Path != "ci-rs-public-probe.invalid/consumer" || mf.Go == nil || len(mf.Require) != 0 || len(mf.Replace) != 0 || len(mf.Exclude) != 0 || mf.Toolchain != nil {
		violations = append(violations, "minimal go.mod without dependency bypass")
	}
	prefix := filepath.Join(output, label)
	record := map[string]any{"program": program, "original_args": args, "actual_args": args, "cwd": dir, "original_env": original, "declared_env": declaredSafe, "mapped_keys": []string{"GOPROXY", "GOSUMDB"}, "fresh_cache_entries_before": len(cacheEntries), "violations": violations, "go_mod_sha256": rsChecksumSHA(modBytes), "started": false}
	if deadline, ok := ctx.Deadline(); ok {
		record["context_remaining_nanoseconds"] = time.Until(deadline).Nanoseconds()
	}
	write := func(suffix string, b []byte) {
		if e := os.WriteFile(prefix+suffix, b, 0600); e != nil {
			panic(e)
		}
	}
	finish := func(out, errout []byte, rc int, err error) ([]byte, []byte, int, error) {
		record["exit_code"] = rc
		record["stdout_sha256"] = rsChecksumSHA(out)
		record["stderr_sha256"] = rsChecksumSHA(errout)
		record["context_deadline_exceeded"] = errors.Is(ctx.Err(), context.DeadlineExceeded)
		record["error"] = fmt.Sprint(err)
		write(".stdout", out)
		write(".stderr", errout)
		b, e := json.MarshalIndent(record, "", "  ")
		if e != nil {
			panic(e)
		}
		write(".json", b)
		return out, errout, rc, err
	}
	if len(violations) > 0 {
		return finish(nil, nil, -1, fmt.Errorf("fixture refused invalid public command contract: %v", violations))
	}
	if !strings.HasPrefix(fixture.Proxy, "http://127.0.0.1:") || !strings.Contains(fixture.SumDB, " http://127.0.0.1:") {
		return finish(nil, nil, -1, fmt.Errorf("unowned checksum fixture endpoint"))
	}
	values["GOPROXY"] = fixture.Proxy
	values["GOSUMDB"] = fixture.SumDB
	actual["GOPROXY"] = fixture.Proxy
	actual["GOSUMDB"] = fixture.SumDB
	record["actual_env"] = actual
	keys := []string{}
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	actualEnv := []string{}
	for _, k := range keys {
		actualEnv = append(actualEnv, k+"="+values[k])
	}
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Dir = dir
	cmd.Env = actualEnv
	cmd.Stdin = bytes.NewReader(stdin)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	start := time.Now()
	err := cmd.Start()
	rc := -1
	if err == nil {
		record["started"] = true
		record["pid"] = cmd.Process.Pid
		err = cmd.Wait()
		rc = cmd.ProcessState.ExitCode()
		_, statErr := os.Stat(fmt.Sprintf("/proc/%d", cmd.Process.Pid))
		record["pid_reaped"] = os.IsNotExist(statErr)
		record["process_group_absent"] = errors.Is(syscall.Kill(-cmd.Process.Pid, 0), syscall.ESRCH)
	}
	record["elapsed_nanoseconds"] = time.Since(start).Nanoseconds()
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	var downloaded map[string]any
	dec := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	de := dec.Decode(&downloaded)
	var extra any
	if de == nil {
		de = dec.Decode(&extra)
		if errors.Is(de, io.EOF) {
			de = nil
		} else if de == nil {
			de = fmt.Errorf("extra JSON value")
		}
	}
	record["one_complete_json"] = de == nil
	record["download_json"] = downloaded
	files := []any{}
	if rc == 0 && de == nil {
		for _, key := range []string{"Info", "GoMod", "Zip"} {
			path, ok := downloaded[key].(string)
			if !ok || !rsChecksumWithin(cache, path) {
				continue
			}
			st, e := os.Lstat(path)
			if e != nil || !st.Mode().IsRegular() {
				continue
			}
			b, e := os.ReadFile(path)
			if e != nil {
				continue
			}
			write(".verified-"+key, b)
			files = append(files, map[string]any{"field": key, "path": path, "sha256": rsChecksumSHA(b), "bytes": len(b)})
		}
	}
	record["verified_files"] = files
	return finish(stdout.Bytes(), stderr.Bytes(), rc, err)
}

type rsChecksumBadSignature struct{ sumdb.ServerOps }

func (s rsChecksumBadSignature) Signed(ctx context.Context) ([]byte, error) {
	b, e := s.ServerOps.Signed(ctx)
	if e != nil {
		return nil, e
	}
	lines := strings.Split(string(b), "\n")
	for i, line := range lines {
		if !strings.HasPrefix(line, "— ") {
			continue
		}
		parts := strings.Fields(line)
		sig, e := base64.StdEncoding.DecodeString(parts[2])
		if e != nil {
			return nil, e
		}
		sig[len(sig)-1] ^= 1
		parts[2] = base64.StdEncoding.EncodeToString(sig)
		lines[i] = strings.Join(parts, " ")
	}
	return []byte(strings.Join(lines, "\n")), nil
}

type rsChecksumService struct {
	h                                       *rsHarness
	mapping                                 rsChecksumMapping
	dir, mode, originalH1, alteredH1, modH1 string
	original, altered, mod, record          []byte
	proxy, database                         *httptest.Server
	mu                                      sync.Mutex
	rows                                    []map[string]any
	serial                                  int
	active                                  atomic.Int64
	closed                                  bool
}

func rsChecksumRecordHandler(s *rsChecksumService, kind string, inner http.Handler, hang bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.active.Add(1)
		defer s.active.Add(-1)
		s.mu.Lock()
		s.serial++
		id := s.serial
		s.mu.Unlock()
		body, e := io.ReadAll(r.Body)
		if e != nil {
			panic(e)
		}
		prefix := filepath.Join(s.dir, fmt.Sprintf("http-%03d", id))
		if e = os.WriteFile(prefix+".request", body, 0600); e != nil {
			panic(e)
		}
		row := map[string]any{"id": id, "service": kind, "method": r.Method, "path": r.URL.Path, "request_sha256": rsSHA(body)}
		if hang {
			<-r.Context().Done()
			row["cancelled"] = true
			row["response_sent"] = false
		} else {
			rr := httptest.NewRecorder()
			inner.ServeHTTP(rr, r)
			b := rr.Body.Bytes()
			if e = os.WriteFile(prefix+".response", b, 0600); e != nil {
				panic(e)
			}
			row["status"] = rr.Code
			row["response_sent"] = true
			row["response_sha256"] = rsSHA(b)
			for k, v := range rr.Header() {
				w.Header()[k] = v
			}
			w.WriteHeader(rr.Code)
			_, _ = w.Write(b)
		}
		b, e := json.MarshalIndent(row, "", "  ")
		if e != nil {
			panic(e)
		}
		if e = os.WriteFile(prefix+".json", b, 0600); e != nil {
			panic(e)
		}
		s.mu.Lock()
		s.rows = append(s.rows, row)
		s.mu.Unlock()
	})
}
func rsChecksumAlterZIP(t *testing.T, raw []byte, mod, version string) []byte {
	t.Helper()
	zr, e := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if e != nil {
		t.Fatal(e)
	}
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	changed := 0
	for _, f := range zr.File {
		r, e := f.Open()
		if e != nil {
			t.Fatal(e)
		}
		data, e := io.ReadAll(r)
		r.Close()
		if e != nil {
			t.Fatal(e)
		}
		if f.Name == mod+"@"+version+"/first/value.go" {
			if bytes.Count(data, []byte("Value=2")) != 1 {
				t.Fatal("HARNESS_NOT_EXECUTED: checksum ZIP delta anchor")
			}
			data = bytes.Replace(data, []byte("Value=2"), []byte("Value=3"), 1)
			changed++
		}
		header := f.FileHeader
		w, e := zw.CreateHeader(&header)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = w.Write(data); e != nil {
			t.Fatal(e)
		}
	}
	if e = zw.Close(); e != nil {
		t.Fatal(e)
	}
	if changed != 1 {
		t.Fatal("HARNESS_NOT_EXECUTED: checksum single-member inversion")
	}
	return b.Bytes()
}
func rsNewChecksumService(h *rsHarness, p *rsPublisherFixture, label, mode, signer, verifier string) *rsChecksumService {
	dir := filepath.Join(h.root, "checksum-service-"+label)
	if e := os.MkdirAll(dir, 0700); e != nil {
		h.t.Fatal(e)
	}
	escaped, e := module.EscapePath(p.f.module)
	if e != nil {
		h.t.Fatal(e)
	}
	s := &rsChecksumService{h: h, dir: dir, mode: mode, original: mustRSRead(h.t, p.published.path), mod: mustRSRead(h.t, filepath.Join(p.published.proxy, escaped, "@v", "v1.0.1.mod"))}
	s.altered = rsChecksumAlterZIP(h.t, s.original, p.f.module, "v1.0.1")
	for name, b := range map[string][]byte{"original.zip": s.original, "altered.zip": s.altered, "module.mod": s.mod} {
		if e = os.WriteFile(filepath.Join(dir, name), b, 0600); e != nil {
			h.t.Fatal(e)
		}
	}
	s.originalH1, e = dirhash.HashZip(filepath.Join(dir, "original.zip"), dirhash.Hash1)
	if e != nil {
		h.t.Fatal(e)
	}
	s.alteredH1, e = dirhash.HashZip(filepath.Join(dir, "altered.zip"), dirhash.Hash1)
	if e != nil {
		h.t.Fatal(e)
	}
	s.modH1, e = dirhash.Hash1([]string{"go.mod"}, func(string) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(s.mod)), nil })
	if e != nil {
		h.t.Fatal(e)
	}
	signedSum := s.originalH1
	if mode == "signed-altered-content" {
		signedSum = s.alteredH1
	}
	s.record = []byte(fmt.Sprintf("%s v1.0.1 %s\n%s v1.0.1/go.mod %s\n", p.f.module, signedSum, p.f.module, s.modH1))
	if e = os.WriteFile(filepath.Join(dir, "signed-record.txt"), s.record, 0600); e != nil {
		h.t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(dir, "public-verifier.txt"), []byte(verifier), 0600); e != nil {
		h.t.Fatal(e)
	}
	var ops sumdb.ServerOps = sumdb.NewTestServer(signer, func(path, version string) ([]byte, error) {
		if path != p.f.module || version != "v1.0.1" {
			return nil, fmt.Errorf("unexpected fixture checksum identity")
		}
		return s.record, nil
	})
	if mode == "invalid-signature" {
		ops = rsChecksumBadSignature{ops}
	}
	var db http.Handler = sumdb.NewServer(ops)
	if mode == "unavailable" {
		db = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "owned checksum fixture unavailable", 503) })
	}
	s.database = httptest.NewServer(rsChecksumRecordHandler(s, "sumdb", db, mode == "deadline"))
	zipBytes := s.original
	if mode == "altered-zip" || mode == "signed-altered-content" {
		zipBytes = s.altered
	}
	s.proxy = httptest.NewServer(rsChecksumRecordHandler(s, "proxy", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/" + escaped + "/@v/v1.0.1.info":
			_, _ = w.Write([]byte(`{"Version":"v1.0.1","Time":"2026-01-01T00:00:00Z"}`))
		case "/" + escaped + "/@v/v1.0.1.mod":
			_, _ = w.Write(s.mod)
		case "/" + escaped + "/@v/v1.0.1.zip":
			_, _ = w.Write(zipBytes)
		case "/" + escaped + "/@v/list":
			_, _ = w.Write([]byte("v1.0.1\n"))
		default:
			http.NotFound(w, r)
		}
	}), false))
	s.mapping = rsChecksumMapping{s.proxy.URL, verifier + " " + s.database.URL, p.f.module, "v1.0.1", h.goBin, h.root}
	h.t.Cleanup(func() { s.close() })
	return s
}
func (s *rsChecksumService) close() {
	if s.closed {
		return
	}
	s.proxy.Close()
	s.database.Close()
	s.closed = true
	s.mu.Lock()
	defer s.mu.Unlock()
	sort.Slice(s.rows, func(i, j int) bool { return s.rows[i]["id"].(int) < s.rows[j]["id"].(int) })
	b := rsCanonicalJSON(s.h.t, map[string]any{"mode": s.mode, "original_h1": s.originalH1, "altered_h1": s.alteredH1, "go_mod_h1": s.modH1, "original_zip_sha256": rsSHA(s.original), "altered_zip_sha256": rsSHA(s.altered), "record_sha256": rsSHA(s.record), "mapping": s.mapping, "requests": s.rows, "active_handlers_after_close": s.active.Load(), "servers_closed": true, "private_key_persisted": false})
	if e := os.WriteFile(filepath.Join(s.dir, "service-summary.json"), b, 0600); e != nil {
		s.h.t.Fatal(e)
	}
	rsCopyCaptures(s.h, s.dir, filepath.Base(s.dir))
	if s.active.Load() != 0 {
		s.h.t.Error("HARNESS_NOT_EXECUTED: checksum server handler leaked")
	}
}

func rsChecksumPrerequisite(h *rsHarness, p *rsPublisherFixture, mode, signer, verifier string) {
	s := rsNewChecksumService(h, p, "control-"+mode, mode, signer, verifier)
	defer s.close()
	root := filepath.Join(h.root, "checksum-command-"+mode)
	for _, d := range []string{"consumer", "cache", "gopath", "home", "tmp", "capture"} {
		if e := os.MkdirAll(filepath.Join(root, d), 0700); e != nil {
			h.t.Fatal(e)
		}
	}
	h.put(filepath.Join(root, "consumer"), "go.mod", "module ci-rs-public-probe.invalid/consumer\n\ngo 1.21\n")
	extra := []string{"GOWORK=off", "GOENV=off", "GOFLAGS=", "GOTOOLCHAIN=local", "GOPROXY=https://proxy.golang.org", "GOSUMDB=sum.golang.org", "GOPRIVATE=", "GONOPROXY=", "GONOSUMDB=", "GOMODCACHE=" + filepath.Join(root, "cache"), "GOPATH=" + filepath.Join(root, "gopath"), "HOME=" + filepath.Join(root, "home"), "TMPDIR=" + filepath.Join(root, "tmp")}
	h.t.Cleanup(func() {
		_, stderr, rc := h.run(h.root, []string{"GOMODCACHE=" + filepath.Join(root, "cache")}, h.goBin, "clean", "-modcache")
		if rc != 0 {
			h.t.Errorf("HARNESS_NOT_EXECUTED: checksum own cache cleanup %s", stderr)
		}
	})
	timeout := 15 * time.Second
	if mode == "deadline" {
		timeout = time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	stdout, _, rc, err := rsChecksumExecute(ctx, h.goBin, []string{"mod", "download", "-json", p.f.module + "@v1.0.1"}, filepath.Join(root, "consumer"), rsEnvironment(extra...), extra, nil, s.mapping, filepath.Join(root, "capture"), "public-go")
	var commandRecord map[string]any
	if e := json.Unmarshal(mustRSRead(h.t, filepath.Join(root, "capture/public-go.json")), &commandRecord); e != nil {
		h.t.Fatal(e)
	}
	if commandRecord["started"] != true || commandRecord["pid_reaped"] != true || commandRecord["process_group_absent"] != true || len(commandRecord["violations"].([]any)) != 0 {
		h.t.Fatalf("HARNESS_NOT_EXECUTED: checksum control was not an actual completed Go command %+v", commandRecord)
	}
	wantSuccess := mode == "lawful" || mode == "restored" || mode == "signed-altered-content"
	if (rc == 0) != wantSuccess || (mode == "deadline" && !errors.Is(err, context.DeadlineExceeded)) || (!wantSuccess && mode != "deadline" && rc != 1) {
		h.t.Fatalf("HARNESS_NOT_EXECUTED: actual checksum prerequisite %s rc%d %v", mode, rc, err)
	}
	var result map[string]any
	if wantSuccess {
		if e := json.Unmarshal(stdout, &result); e != nil {
			h.t.Fatal(e)
		}
		sum := s.originalH1
		if mode == "signed-altered-content" {
			sum = s.alteredH1
		}
		if result["Path"] != p.f.module || result["Version"] != "v1.0.1" || result["Sum"] != sum || result["GoModSum"] != s.modH1 || result["Error"] != nil {
			h.t.Fatalf("HARNESS_NOT_EXECUTED: verified real checksum result %+v", result)
		}
	}
	rsCopyCaptures(h, filepath.Join(root, "capture"), "checksum-control-"+mode)
	h.save("checksum-control-"+mode+".json", rsCanonicalJSON(h.t, map[string]any{"classification": "ACTUAL_GO_PREREQUISITE_NO_SUT", "mode": mode, "exit_code": rc, "deadline_exceeded": errors.Is(err, context.DeadlineExceeded), "expected_success": wantSuccess, "exact_adapter_used": true}))
}

// Extract only the generic actual-command adapter declarations. The baseline
// compiles and executes these same declarations before any API discovery.
func rsChecksumAdapterDeclarations(t *testing.T) string {
	path := filepath.Join(moduleRoot(t), "internal/release/supply_public_checksum_test.go")
	raw := mustRSRead(t, path)
	fs := token.NewFileSet()
	file, e := parser.ParseFile(fs, path, raw, parser.AllErrors)
	if e != nil {
		t.Fatal(e)
	}
	names := map[string]bool{"rsChecksumMapping": true, "rsChecksumSHA": true, "rsChecksumWithin": true, "rsChecksumExecute": true}
	var out strings.Builder
	for _, d := range file.Decls {
		name := ""
		switch x := d.(type) {
		case *ast.FuncDecl:
			name = x.Name.Name
		case *ast.GenDecl:
			if len(x.Specs) == 1 {
				if z, ok := x.Specs[0].(*ast.TypeSpec); ok {
					name = z.Name.Name
				}
			}
		}
		if !names[name] {
			continue
		}
		f := fs.File(d.Pos())
		out.Write(raw[f.Offset(d.Pos()):f.Offset(d.End())])
		out.WriteString("\n")
		delete(names, name)
	}
	if len(names) != 0 {
		t.Fatal("HARNESS_NOT_EXECUTED: missing checksum adapter declarations")
	}
	return out.String()
}
func rsChecksumBridgeSource(t *testing.T) string {
	s := rsPublisherBridgeSource(t)
	replace := func(old, new string) {
		if strings.Count(s, old) != 1 {
			t.Fatalf("HARNESS_NOT_EXECUTED: checksum bridge anchor %q", old)
		}
		s = strings.Replace(s, old, new, 1)
	}
	replace(" \"net/http\"\n", " \"net/http\"\n \"crypto/sha256\"\n \"encoding/hex\"\n \"errors\"\n \"reflect\"\n \"sort\"\n \"syscall\"\n \"golang.org/x/mod/modfile\"\n")
	replace("type bridgeRequest struct {", "type bridgeRequest struct {\n Checksum rsChecksumMapping `json:\"checksum\"`\n")
	replace("   args:=append([]string(nil),c.Args...)", `   if name=="go"&&len(c.Args)>1&&c.Args[0]=="mod"&&c.Args[1]=="download"&&values["GOPROXY"]=="https://proxy.golang.org"{
    original:=[]string{};for k,v:=range values{original=append(original,k+"="+v)}
    stdout,stderr,rc,err:=rsChecksumExecute(ctx,c.Program,c.Args,c.Dir,original,c.Env,c.Stdin,request.Checksum,request.Output,fmt.Sprintf("public-go-%04d",id))
    return release.SupplyCommandResult{Stdout:stdout,Stderr:stderr,ExitCode:rc,Err:err}
   }
   args:=append([]string(nil),c.Args...)`)
	// The seven-second limit is this test's explicit parent context, not the
	// product's HTTP network budget or its ordinary600s Go command policy.
	replace("rc:=release.RunSupplyPublisher(ctx,request.Args,deps,&stdout,&stderr)", `sutCtx:=ctx;sutCancel:=func(){};probeContext:=len(request.Args)>1&&request.Args[0]=="--phase"&&request.Args[1]=="probe"
 sutBudget:=time.Duration(0);if probeContext{sutBudget=7*time.Second;sutCtx,sutCancel=context.WithTimeout(ctx,sutBudget)};defer sutCancel()
 rc:=release.RunSupplyPublisher(sutCtx,request.Args,deps,&stdout,&stderr)`)
	replace(`"deadline_exceeded":ctx.Err()!=nil,`, `"deadline_exceeded":ctx.Err()!=nil,"harness_deadline_exceeded":errors.Is(ctx.Err(),context.DeadlineExceeded),"sut_deadline_exceeded":errors.Is(sutCtx.Err(),context.DeadlineExceeded),"sut_probe_context_applied":probeContext,"sut_parent_budget_nanoseconds":int64(sutBudget),`)
	return s + "\n" + rsChecksumAdapterDeclarations(t)
}
func rsBuildChecksumBridge(h *rsHarness) string {
	_ = rsBuildConsumerBridge(h)
	clone := filepath.Join(h.root, "sut-copy")
	h.put(clone, "internal/release/ci_rs_bridge_test.go", rsChecksumBridgeSource(h.t))
	binary := filepath.Join(h.root, "checksum-bridge.test")
	h.must(clone, h.goBin, "test", "-c", "-o", binary, "./internal/release")
	return binary
}
func rsChecksumInvoke(h *rsHarness, p *rsPublisherFixture, f *rsForgeProcess, s *rsChecksumService, binary, label string, args []string) rsPublisherObservation {
	dir := filepath.Join(h.root, "checksum-result-"+label)
	if e := os.MkdirAll(dir, 0700); e != nil {
		h.t.Fatal(e)
	}
	remotes := map[string]string{}
	for k, v := range p.remotes {
		remotes[k] = v
	}
	remotes["https://github.com/PRO-Robotech/corelib.git"] = "file://" + f.repository
	req := map[string]any{"args": args, "output": dir, "remotes": remotes, "endpoint": f.endpoint, "receive_pack": f.wrapper, "checksum": s.mapping}
	path := filepath.Join(h.root, "checksum-request-"+label+".json")
	h.put(h.root, filepath.Base(path), string(rsCanonicalJSON(h.t, req)))
	ownedTmp := filepath.Join(h.root, "checksum-sut-tmp-"+label)
	if e := os.MkdirAll(ownedTmp, 0700); e != nil {
		h.t.Fatal(e)
	}
	// Isolate SumDB checkpoints without changing the existing producer dependency
	// cache. The public command still must explicitly override it with a fresh one.
	producerCache := h.must(h.root, h.goBin, "env", "GOMODCACHE")
	_, stderr, rc := h.run(h.root, []string{"CI_RS_BRIDGE_REQUEST=" + path, "GOPROXY=off", "GOSUMDB=off", "GOPATH=" + filepath.Join(h.root, "checksum-sut-gopath-"+label), "GOMODCACHE=" + producerCache, "TMPDIR=" + ownedTmp}, binary, "-test.run=^TestCIRSConsumerBridge$", "-test.v", "-test.timeout=115s")
	if rc != 0 {
		h.t.Fatalf("HARNESS_NOT_EXECUTED: checksum bridge rc%d %s", rc, stderr)
	}
	var meta map[string]any
	if e := json.Unmarshal(mustRSRead(h.t, filepath.Join(dir, "bridge-result.json")), &meta); e != nil {
		h.t.Fatal(e)
	}
	rsCopyCaptures(h, dir, label)
	if meta["deadline_exceeded"] != false || meta["harness_deadline_exceeded"] != false || len(meta["unhandled_boundaries"].([]any)) != 0 {
		h.t.Fatalf("HARNESS_NOT_EXECUTED: checksum outer bridge %+v", meta)
	}
	probe := len(args) > 1 && args[0] == "--phase" && args[1] == "probe"
	wantBudget := float64(0)
	if probe {
		wantBudget = float64(7 * time.Second)
	}
	if meta["sut_probe_context_applied"] != probe || meta["sut_parent_budget_nanoseconds"] != wantBudget || (!probe && meta["sut_deadline_exceeded"] != false) {
		h.t.Fatalf("HARNESS_NOT_EXECUTED: checksum explicit parent context metadata %+v", meta)
	}
	publicCommands, globErr := filepath.Glob(filepath.Join(dir, "public-go-*.json"))
	if globErr != nil {
		h.t.Fatal(globErr)
	}
	for _, path := range publicCommands {
		var record map[string]any
		if e := json.Unmarshal(mustRSRead(h.t, path), &record); e != nil {
			h.t.Fatal(e)
		}
		original, ok := record["original_env"].(map[string]any)
		if !ok {
			continue
		}
		cache, ok := original["GOMODCACHE"].(string)
		if !ok || !rsChecksumWithin(h.root, cache) {
			continue
		}
		h.t.Cleanup(func() {
			if _, e := os.Stat(cache); os.IsNotExist(e) {
				return
			}
			_, stderr, rc := h.run(h.root, []string{"GOMODCACHE=" + cache}, h.goBin, "clean", "-modcache")
			if rc != 0 {
				h.t.Errorf("HARNESS_NOT_EXECUTED: owned public probe cache cleanup: %s", stderr)
			}
		})
	}
	result := rsValidateResult(h, mustRSRead(h.t, filepath.Join(dir, "sut.stdout")), label)
	commands := []map[string]any{}
	entries, e := os.ReadDir(dir)
	if e != nil {
		h.t.Fatal(e)
	}
	for _, x := range entries {
		if strings.HasPrefix(x.Name(), "command-") && strings.HasSuffix(x.Name(), ".json") {
			var c map[string]any
			if e = json.Unmarshal(mustRSRead(h.t, filepath.Join(dir, x.Name())), &c); e != nil {
				h.t.Fatal(e)
			}
			commands = append(commands, c)
		}
	}
	var state map[string]any
	if e = json.Unmarshal(mustRSRead(h.t, f.state), &state); e != nil {
		h.t.Fatal(e)
	}
	if len(state["unknown"].([]any)) != 0 {
		h.t.Fatalf("HARNESS_NOT_EXECUTED: checksum undeclared forge routes %+v", state["unknown"])
	}
	return rsPublisherObservation{result, meta, dir, commands}
}

func TestReleaseSupplyPublicChecksum(t *testing.T) {
	p := rsPreparePublisher(t)
	h := p.f.h
	h.save("checksum-sumdb-library.json", []byte(h.must(moduleRoot(t), h.goBin, "list", "-m", "-json", "golang.org/x/mod")))
	signer, verifier, e := note.GenerateKey(rand.Reader, "ci-rs-checksum.invalid")
	if e != nil {
		t.Fatal(e)
	}
	modes := []string{"lawful", "altered-zip", "invalid-signature", "unavailable", "deadline", "restored", "signed-altered-content"}
	cases := []rsPublisherCase{}
	ledger := []any{}
	for _, mode := range modes {
		outcome, reason, stage := "NOT_EXECUTED", "PROXY_UNAVAILABLE", "TAG_PRESENT"
		if mode == "lawful" || mode == "restored" {
			outcome, reason, stage = "GREEN", "OK", "ARCHIVE_VERIFIED"
		}
		if mode == "deadline" {
			reason = "BUDGET_EXHAUSTED"
		}
		if mode == "signed-altered-content" {
			outcome, reason = "RED", "PUBLISHED_ARCHIVE_MISMATCH"
		}
		cases = append(cases, rsPublisherCase{Name: mode, Scenario: "CI-RS-13/22", Phase: "probe", Outcome: outcome, Reason: reason, Stage: stage, Effects: map[string]string{}})
		ledger = append(ledger, map[string]any{"mode": mode, "outcome": outcome, "reason": reason, "stage": stage, "required_actual_public_Go_calls": 1, "sut_calls": 0})
		rsChecksumPrerequisite(h, p, mode, signer, verifier)
	}
	h.save("checksum-case-ledger.json", rsCanonicalJSON(t, ledger))
	bridgeSource := rsChecksumBridgeSource(t)
	h.save("checksum-bridge-source.go.txt", []byte(bridgeSource))
	if _, e = parser.ParseFile(token.NewFileSet(), "ci_rs_bridge_test.go", bridgeSource, parser.AllErrors); e != nil {
		t.Fatal(e)
	}
	missing := rsSupplySymbols(h, []string{"RunSupplyPublisher", "SupplyDependencies", "SupplyCommand", "SupplyCommandResult"})
	if len(missing) > 0 {
		h.save("checksum-capability.json", rsCanonicalJSON(t, map[string]any{"holder_outcome": "CAPABILITY_ABSENT", "missing_symbols": missing, "prepared_cases": len(cases), "sut_invocations": 0, "actual_Go_prerequisites": len(modes)}))
		t.Fatalf("CAPABILITY_ABSENT: public-checksum publisher %v; prepared=%d; sut_invocations=0", missing, len(cases))
	}
	binary := rsBuildChecksumBridge(h)
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			ch := *h
			ch.t = t
			ch.serial = 0
			if h.capture != "" {
				ch.capture = filepath.Join(h.capture, c.Name)
				if e := os.MkdirAll(ch.capture, 0700); e != nil {
					t.Fatal(e)
				}
			}
			s := rsNewChecksumService(&ch, p, "sut-"+c.Name, c.Name, signer, verifier)
			defer s.close()
			f := rsStartForge(&ch, p, "checksum-"+c.Name)
			manifestPath := filepath.Join(h.root, "checksum-manifest-"+c.Name+".json")
			ch.put(h.root, filepath.Base(manifestPath), string(rsCanonicalJSON(t, p.manifest)))
			args := func(phase string) []string {
				return []string{"--phase", phase, "--tree", p.tree, "--repo", "PRO-Robotech/corelib", "--version", "v1.0.1", "--manifest", manifestPath, "--via-pull-request", "--network-budget", "7", "--checks-budget", "7"}
			}
			planning := rsChecksumInvoke(&ch, p, f, s, binary, c.Name+"-plan", args("plan"))
			if planning.result["outcome"] != "GREEN" {
				t.Fatalf("SEMANTIC_MISMATCH: checksum lawful plan prerequisite %+v", planning.result)
			}
			plan, ok := planning.result["plan_sha256"].(string)
			if !ok || len(plan) != 64 {
				t.Fatal("SEMANTIC_MISMATCH: checksum plan key")
			}
			candidate, ok := planning.result["candidate_sha"].(string)
			if !ok || len(candidate) != 40 {
				t.Fatal("SEMANTIC_MISMATCH: checksum candidate key")
			}
			delivered := rsChecksumInvoke(&ch, p, f, s, binary, c.Name+"-deliver", append(args("deliver"), "--commit", "PRO-Robotech/corelib", "--plan-sha256", plan))
			rsCheckPublisher(&ch, rsPublisherCase{Name: "checksum-delivery-prerequisite", Phase: "deliver", Outcome: "GREEN", Reason: "OK", Stage: "MERGED_VERIFIED", Effects: map[string]string{"branch": "PRESENT", "pr": "PRESENT", "merge": "PRESENT"}}, delivered, plan, candidate)
			if t.Failed() {
				return
			}
			landed := ch.must(h.root, "git", "--git-dir", f.repository, "rev-parse", "refs/heads/main")
			ch.must(h.root, "git", "--git-dir", f.repository, "merge-base", "--is-ancestor", candidate, landed)
			readOnly := rsChecksumInvoke(&ch, p, f, s, binary, c.Name+"-release-plan", append(args("release"), "--landed-sha", landed))
			if readOnly.result["outcome"] != "GREEN" || readOnly.result["dry_run"] != true {
				t.Fatalf("SEMANTIC_MISMATCH: checksum release prerequisite %+v", readOnly.result)
			}
			ch.must(h.root, "git", "--git-dir", f.repository, "update-ref", "refs/tags/v1.0.1", landed, strings.Repeat("0", 40))
			status, _, err := rsForgeCall(&ch, f, "POST", "/releases", map[string]any{"tag_name": "v1.0.1", "target_commitish": landed, "body": "preexisting checksum fixture note"})
			if err != nil || status != 201 {
				t.Fatal("HARNESS_NOT_EXECUTED: checksum note prerequisite")
			}
			before := mustRSRead(t, f.state)
			mainBefore := ch.must(h.root, "git", "--git-dir", f.repository, "rev-parse", "refs/heads/main")
			probe := []string{"--phase", "probe", "--repo", "PRO-Robotech/corelib", "--version", "v1.0.1", "--manifest", manifestPath, "--network-budget", "7", "--checks-budget", "7"}
			observed := rsChecksumInvoke(&ch, p, f, s, binary, c.Name+"-subject", probe)
			rsCheckPublisher(&ch, c, observed, plan, candidate)
			s.close()
			rsCheckChecksumCommand(&ch, s, c, observed)
			if got := ch.must(h.root, "git", "--git-dir", f.repository, "rev-parse", "refs/tags/v1.0.1^{}"); got != landed {
				t.Error("SEMANTIC_MISMATCH: checksum outcome lost tag")
			}
			if got := ch.must(h.root, "git", "--git-dir", f.repository, "rev-parse", "refs/heads/main"); got != mainBefore {
				t.Error("SEMANTIC_MISMATCH: read-only checksum probe changed main")
			}
			var oldState, newState map[string]any
			if e := json.Unmarshal(before, &oldState); e != nil {
				t.Fatal(e)
			}
			if e := json.Unmarshal(mustRSRead(t, f.state), &newState); e != nil {
				t.Fatal(e)
			}
			if !reflect.DeepEqual(oldState["writes"], newState["writes"]) {
				t.Error("SEMANTIC_MISMATCH: read-only checksum probe wrote forge state")
			}
		})
	}
}
func rsCheckChecksumCommand(h *rsHarness, s *rsChecksumService, c rsPublisherCase, o rsPublisherObservation) {
	h.t.Helper()
	paths, e := filepath.Glob(filepath.Join(o.directory, "public-go-*.json"))
	if e != nil || len(paths) != 1 {
		h.t.Errorf("SEMANTIC_MISMATCH: mandatory public Go calls=%d want1", len(paths))
		return
	}
	var r map[string]any
	if e = json.Unmarshal(mustRSRead(h.t, paths[0]), &r); e != nil {
		h.t.Fatal(e)
	}
	if len(r["violations"].([]any)) != 0 || r["started"] != true || r["pid_reaped"] != true || r["process_group_absent"] != true {
		h.t.Errorf("SEMANTIC_MISMATCH: actual checksum command contract %+v", r)
	}
	lookup := false
	for _, request := range s.rows {
		if request["service"] == "sumdb" && strings.HasPrefix(request["path"].(string), "/lookup/") {
			lookup = true
			if c.Name == "unavailable" && request["status"] != 503 {
				h.t.Error("HARNESS_NOT_EXECUTED: service unavailable did not produce real503")
			}
			if c.Name == "deadline" && request["cancelled"] != true {
				h.t.Error("HARNESS_NOT_EXECUTED: hanging request was not cancelled")
			}
		}
	}
	if !lookup {
		h.t.Error("SEMANTIC_MISMATCH: public Go did not query actual checksum service")
	}
	good := c.Name == "lawful" || c.Name == "restored" || c.Name == "signed-altered-content"
	if (r["exit_code"] == float64(0)) != good {
		h.t.Errorf("SEMANTIC_MISMATCH: actual checksum command exit %v", r["exit_code"])
	}
	remaining, finite := r["context_remaining_nanoseconds"].(float64)
	if !finite || remaining <= 0 || remaining > float64(7*time.Second) {
		h.t.Errorf("SEMANTIC_MISMATCH: public command not clipped by explicit test parent context7s: %v", r["context_remaining_nanoseconds"])
	}
	if o.meta["sut_deadline_exceeded"] != (c.Name == "deadline") {
		h.t.Errorf("SEMANTIC_MISMATCH: inner SUT deadline observation %+v", o.meta)
	}
	if c.Name == "deadline" && r["context_deadline_exceeded"] != true {
		h.t.Error("SEMANTIC_MISMATCH: checksum budget case did not reach actual deadline")
	}
	if good {
		d, ok := r["download_json"].(map[string]any)
		if !ok || r["one_complete_json"] != true {
			h.t.Fatal("SEMANTIC_MISMATCH: checksum success lacks complete actual Go JSON")
		}
		want := s.originalH1
		if c.Name == "signed-altered-content" {
			want = s.alteredH1
		}
		if d["Path"] != s.mapping.Module || d["Version"] != s.mapping.Version || d["Sum"] != want || d["GoModSum"] != s.modH1 || d["Error"] != nil || len(r["verified_files"].([]any)) != 3 {
			h.t.Errorf("SEMANTIC_MISMATCH: actual Go identity/cache/h1 %+v", r)
		}
		raw := mustRSRead(h.t, strings.TrimSuffix(paths[0], ".json")+".verified-Zip")
		expected := s.original
		if c.Name == "signed-altered-content" {
			expected = s.altered
		}
		if !bytes.Equal(raw, expected) {
			h.t.Error("SEMANTIC_MISMATCH: exact verified archive bytes")
		}
	}
	if c.Outcome != "GREEN" && o.result["stage"] == "ARCHIVE_VERIFIED" {
		h.t.Error("SEMANTIC_MISMATCH: failed mandatory checksum gate advanced archive")
	}
}
