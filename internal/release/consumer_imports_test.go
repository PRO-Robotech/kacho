// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package release

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"golang.org/x/mod/module"
	modzip "golang.org/x/mod/zip"
)

// These helpers construct real Git revisions and Go module archives. They do
// not implement a substitute release policy or manufacture a SUT result.
// CI_RS_TEST_CAPTURE_DIR is a test-output destination, never a product seam.
type rsHarness struct {
	t       *testing.T
	root    string
	capture string
	serial  int
	goBin   string
}

type rsCommandCapture struct {
	Command   []string `json:"command"`
	Dir       string   `json:"cwd"`
	Exit      int      `json:"exit_code"`
	TimedOut  bool     `json:"timed_out"`
	StdoutSHA string   `json:"stdout_sha256"`
	StderrSHA string   `json:"stderr_sha256"`
	Duration  float64  `json:"duration_seconds"`
}

func newRSHarness(t *testing.T) *rsHarness {
	t.Helper()
	h := &rsHarness{t: t, root: t.TempDir(), goBin: filepath.Join(runtime.GOROOT(), "bin", "go")}
	if dst := os.Getenv("CI_RS_TEST_CAPTURE_DIR"); dst != "" {
		h.capture = filepath.Join(dst, strings.ReplaceAll(t.Name(), "/", "__"))
		if err := os.MkdirAll(h.capture, 0755); err != nil {
			t.Fatalf("HARNESS_NOT_EXECUTED: capture directory: %v", err)
		}
	}
	// modzip.CreateFromVCS starts Git itself. Refuse inherited redirection
	// rather than let it touch another worktree's index.
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "GIT_") {
			t.Fatalf("HARNESS_NOT_EXECUTED: inherited Git variable %s", strings.SplitN(entry, "=", 2)[0])
		}
	}
	h.must(h.root, "git", "--version")
	h.must(h.root, h.goBin, "version")
	binaryBytes, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatalf("HARNESS_NOT_EXECUTED: test binary identity: %v", err)
	}
	goBytes, err := os.ReadFile(h.goBin)
	if err != nil {
		t.Fatalf("HARNESS_NOT_EXECUTED: Go binary identity: %v", err)
	}
	runtimeRecord, _ := json.MarshalIndent(map[string]any{"go_binary": h.goBin, "go_binary_sha256": rsSHA(goBytes), "test_binary_sha256": rsSHA(binaryBytes), "runtime_go_version": runtime.Version(), "runtime_goos": runtime.GOOS, "runtime_goarch": runtime.GOARCH}, "", "  ")
	h.save("runtime.json", append(runtimeRecord, '\n'))
	return h
}

func rsEnvironment(extra ...string) []string {
	values := map[string]string{}
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if ok && !strings.HasPrefix(key, "GIT_") {
			values[key] = value
		}
	}
	for _, entry := range append([]string{"GOWORK=off", "GOFLAGS=", "GOTOOLCHAIN=local", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0"}, extra...) {
		key, value, _ := strings.Cut(entry, "=")
		values[key] = value
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	env := make([]string, 0, len(keys))
	for _, key := range keys {
		env = append(env, key+"="+values[key])
	}
	return env
}

func rsSHA(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }

func (h *rsHarness) save(name string, raw []byte) {
	h.t.Helper()
	if h.capture == "" {
		return
	}
	if err := os.WriteFile(filepath.Join(h.capture, name), raw, 0600); err != nil {
		h.t.Fatalf("HARNESS_NOT_EXECUTED: capture %s: %v", name, err)
	}
}

func (h *rsHarness) run(dir string, extra []string, name string, args ...string) ([]byte, []byte, int) {
	h.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = rsEnvironment(extra...)
	var out, errout bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errout
	start := time.Now()
	err := cmd.Run()
	rc := 0
	if err != nil {
		rc = -1
		if e, ok := err.(*exec.ExitError); ok {
			rc = e.ExitCode()
		}
	}
	h.serial++
	prefix := fmt.Sprintf("%03d", h.serial)
	h.save(prefix+".stdout", out.Bytes())
	h.save(prefix+".stderr", errout.Bytes())
	record := rsCommandCapture{append([]string{name}, args...), dir, rc, ctx.Err() != nil, rsSHA(out.Bytes()), rsSHA(errout.Bytes()), time.Since(start).Seconds()}
	data, e := json.MarshalIndent(record, "", "  ")
	if e != nil {
		h.t.Fatal(e)
	}
	h.save(prefix+".json", append(data, '\n'))
	h.t.Logf("capture %s: %s %q rc=%d stdout=%s stderr=%s", prefix, name, args, rc, record.StdoutSHA, record.StderrSHA)
	if ctx.Err() != nil || rc < 0 {
		h.t.Fatalf("HARNESS_NOT_EXECUTED: command unavailable or timed out: %s %q: %v", name, args, err)
	}
	return out.Bytes(), errout.Bytes(), rc
}

func (h *rsHarness) must(dir, name string, args ...string) string {
	h.t.Helper()
	out, errout, rc := h.run(dir, nil, name, args...)
	if rc != 0 {
		h.t.Fatalf("HARNESS_NOT_EXECUTED: prerequisite %s %q rc=%d\n%s\n%s", name, args, rc, out, errout)
	}
	return strings.TrimSpace(string(out))
}

func (h *rsHarness) put(root, path, body string) {
	h.t.Helper()
	full := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0644); err != nil {
		h.t.Fatal(err)
	}
}

func (h *rsHarness) initRepo(name string, files map[string]string) string {
	h.t.Helper()
	dir := filepath.Join(h.root, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		h.t.Fatal(err)
	}
	h.must(dir, "git", "init", "--quiet", "-b", "main")
	h.must(dir, "git", "config", "user.name", "CI-RS fixture")
	h.must(dir, "git", "config", "user.email", "ci-rs@invalid")
	for path, body := range files {
		h.put(dir, path, body)
	}
	h.must(dir, "git", "add", "--all")
	h.must(dir, "git", "commit", "--quiet", "-m", "lawful fixture")
	return dir
}

type rsArchive struct {
	revision, version, proxy, path string
	files                          []string
}

func (h *rsHarness) archive(repo, modPath, label string) rsArchive {
	h.t.Helper()
	revision := h.must(repo, "git", "rev-parse", "HEAD")
	version := "v0.0.0-20000101000000-" + revision[:12]
	escaped, err := module.EscapePath(modPath)
	if err != nil {
		h.t.Fatal(err)
	}
	proxy := filepath.Join(h.root, "proxy-"+label)
	verdir := filepath.Join(proxy, escaped, "@v")
	if err := os.MkdirAll(verdir, 0755); err != nil {
		h.t.Fatal(err)
	}
	path := filepath.Join(verdir, version+".zip")
	zf, err := os.Create(path)
	if err != nil {
		h.t.Fatal(err)
	}
	packErr := modzip.CreateFromVCS(zf, module.Version{Path: modPath, Version: version}, repo, revision, "")
	closeErr := zf.Close()
	if packErr != nil || closeErr != nil {
		h.t.Fatalf("HARNESS_NOT_EXECUTED: Go archive creation: %v / %v", packErr, closeErr)
	}
	z, err := zip.OpenReader(path)
	if err != nil {
		h.t.Fatal(err)
	}
	defer z.Close()
	files := []string{}
	prefix := modPath + "@" + version + "/"
	for _, f := range z.File {
		if !strings.HasPrefix(f.Name, prefix) {
			h.t.Fatal("HARNESS_NOT_EXECUTED: unexpected zip prefix")
		}
		files = append(files, strings.TrimPrefix(f.Name, prefix))
	}
	sort.Strings(files)
	if len(files) == 0 {
		h.t.Fatal("HARNESS_NOT_EXECUTED: empty archive")
	}
	gomod := h.must(repo, "git", "show", revision+":go.mod") + "\n"
	h.put(verdir, version+".mod", gomod)
	h.put(verdir, version+".info", fmt.Sprintf(`{"Version":%q,"Time":"2000-01-01T00:00:00Z"}`, version))
	h.put(verdir, "list", version+"\n")
	raw, err := os.ReadFile(path)
	if err != nil {
		h.t.Fatal(err)
	}
	h.save(label+".zip", raw)
	meta, _ := json.MarshalIndent(map[string]any{"revision": revision, "version": version, "files": files, "zip_sha256": rsSHA(raw), "zip_bytes": len(raw)}, "", "  ")
	h.save(label+".archive.json", append(meta, '\n'))
	return rsArchive{revision, version, proxy, path, files}
}

func (h *rsHarness) buildConsumer(a rsArchive, modPath, program, label string) (string, int) {
	h.t.Helper()
	dir := filepath.Join(h.root, "consumer-"+label)
	cache := filepath.Join(h.root, "cache-"+label)
	h.put(dir, "go.mod", "module consumer.example/"+label+"\n\ngo 1.21\n\nrequire "+modPath+" "+a.version+"\n")
	h.put(dir, "main.go", program)
	extra := []string{"GOPROXY=file://" + filepath.ToSlash(a.proxy), "GOSUMDB=off", "GONOSUMDB=", "GOPRIVATE=", "GONOPROXY=", "GOMODCACHE=" + cache, "GOFLAGS=-mod=mod", "CGO_ENABLED=0"}
	// Cleanup the dedicated read-only module cache without touching shared caches.
	h.t.Cleanup(func() {
		cmd := exec.Command(h.goBin, "clean", "-modcache")
		cmd.Env = rsEnvironment(extra...)
		_ = cmd.Run()
	})
	out, errout, rc := h.run(dir, extra, h.goBin, "build", "./...")
	return string(out) + string(errout), rc
}

// rsTwoImportPrerequisites demonstrates the current P8's precise scope by
// running the existing test function, unmodified, in a real fixture repository.
// Its PASS is not a P9 result. A full consumer builds both imports from the
// same Go ZIP in a fresh module cache; only the second tracked file is changed.
func rsTwoImportPrerequisites(t *testing.T) (*rsHarness, string, string) {
	t.Helper()
	h := newRSHarness(t)
	modPath := "example.com/ci-rs-fixture"
	first := "pkg/api/kacho/cloud/reference/reference.go"
	second := "second/second.go"
	repo := h.initRepo("target", map[string]string{
		"go.mod": "module " + modPath + "\n\ngo 1.21\n",
		first:    "package reference\n\ntype Referrer struct{}\n",
		second:   "package second\n\nfunc Value() int { return 7 }\n",
	})
	program := "package main\nimport ref \"" + modPath + "/pkg/api/kacho/cloud/reference\"\nimport \"" + modPath + "/second\"\nfunc main(){ var r *ref.Referrer; _ = r; _ = second.Value() }\n"
	lawful := h.archive(repo, modPath, "lawful")
	if output, rc := h.buildConsumer(lawful, modPath, program, "lawful"); rc != 0 {
		t.Fatalf("HARNESS_NOT_EXECUTED: lawful two-import consumer failed: %s", output)
	}
	oldSHA := lawful.revision
	h.must(repo, "git", "rm", "--cached", second)
	h.must(repo, "git", "commit", "--quiet", "-m", "single fact: second package is not tracked")
	delta := h.must(repo, "git", "diff", "--name-status", oldSHA, "HEAD")
	if delta != "D\t"+second {
		t.Fatalf("HARNESS_NOT_EXECUTED: not a single-fact twin: %q", delta)
	}
	if _, err := os.Stat(filepath.Join(repo, second)); err != nil {
		t.Fatalf("HARNESS_NOT_EXECUTED: untracked twin disappeared: %v", err)
	}
	bad := h.archive(repo, modPath, "missing-second")
	if len(lawful.files) != len(bad.files)+1 {
		t.Fatal("HARNESS_NOT_EXECUTED: archive delta cardinality")
	}
	for _, path := range bad.files {
		if path == second {
			t.Fatal("HARNESS_NOT_EXECUTED: untracked file reached archive")
		}
	}
	output, rc := h.buildConsumer(bad, modPath, program, "missing-second")
	if rc == 0 || !strings.Contains(output, modPath+"/second") {
		t.Fatalf("HARNESS_NOT_EXECUTED: missing-package inversion was not established: rc=%d\n%s", rc, output)
	}
	// Current P8 is an actual existing test, not a stand-in shell or Go stub.
	p8out, p8err, p8rc := h.run(repo, nil, os.Args[0], "-test.run=^TestExternalConsumerCanBuildTheModule$", "-test.v", "-test.timeout=110s")
	if p8rc != 0 || !bytes.Contains(p8out, []byte("--- PASS: TestExternalConsumerCanBuildTheModule")) {
		t.Fatalf("HARNESS_NOT_EXECUTED: existing P8 observation failed: rc=%d\n%s\n%s", p8rc, p8out, p8err)
	}
	h.save("single-fact.diff", []byte(h.must(repo, "git", "diff", oldSHA, "HEAD")+"\n"))
	h.save("declared-consumer.go", []byte(program))
	h.save("legacy-gap.json", []byte(fmt.Sprintf("{\"scenario\":\"CI-RS-07/09\",\"declared_imports\":2,\"lawful_archive_files\":%d,\"negative_archive_files\":%d,\"changed_path\":%q,\"legacy_p8_exit\":%d,\"full_consumer_exit\":%d,\"classification\":\"OBSERVED_LEGACY_COVERAGE_GAP\",\"future_p9_verdict\":\"NOT_EXECUTED\"}\n", len(lawful.files), len(bad.files), second, p8rc, rc)))
	t.Logf("OBSERVED_LEGACY_COVERAGE_GAP: declared imports=2; current P8 PASS; full consumer rc=%d names %s/second; archive delta=%s", rc, modPath, delta)
	return h, repo, modPath
}

const rsConsumerBridgeSource = `package release_test

import (
 "bytes"
 "context"
 "encoding/json"
 "fmt"
 "net/http"
 "os"
 "os/exec"
 "path/filepath"
 "strings"
 "sync"
 "testing"
 "time"
 release "github.com/PRO-Robotech/kacho/internal/release"
)

type bridgeRequest struct {
 Args []string ` + "`" + `json:"args"` + "`" + `
 Output string ` + "`" + `json:"output"` + "`" + `
 Remotes map[string]string ` + "`" + `json:"remotes"` + "`" + `
}
func TestCIRSConsumerBridge(t *testing.T) {
 raw,e:=os.ReadFile(os.Getenv("CI_RS_BRIDGE_REQUEST"));if e!=nil {t.Fatal(e)}
 var request bridgeRequest
 dec:=json.NewDecoder(bytes.NewReader(raw));dec.DisallowUnknownFields();if e=dec.Decode(&request);e!=nil {t.Fatal(e)}
 if request.Output=="" || len(request.Args)==0 {t.Fatal("invalid bridge input")}
 if e=os.MkdirAll(request.Output,0755);e!=nil {t.Fatal(e)}
 var mu sync.Mutex;ordinal:=0;unhandled:=[]string{}
 deps:=release.SupplyDependencies{
  Command:func(ctx context.Context,c release.SupplyCommand) release.SupplyCommandResult {
   mu.Lock();ordinal++;id:=ordinal;mu.Unlock()
   name:=filepath.Base(c.Program)
   if name!="git" && name!="go" {mu.Lock();unhandled=append(unhandled,"command:"+c.Program);mu.Unlock();return release.SupplyCommandResult{ExitCode:-1,Err:fmt.Errorf("unhandled test boundary %s",c.Program)}}
   values:=map[string]string{}
   for _,v:=range os.Environ(){k,val,ok:=strings.Cut(v,"=");if ok&&!strings.HasPrefix(k,"GIT_"){values[k]=val}}
   for _,v:=range c.Env{k,val,ok:=strings.Cut(v,"=");if ok{values[k]=val}}
   args:=append([]string(nil),c.Args...)
   if name=="git" {
    prefix:=[]string{"-c","protocol.allow=never","-c","protocol.file.allow=always"}
    for canonical,local:=range request.Remotes {prefix=append(prefix,"-c","url."+local+".insteadOf="+canonical)}
    args=append(prefix,args...)
   }
   env:=[]string{};for k,v:=range values{env=append(env,k+"="+v)}
   if name=="go" {proxy:=values["GOPROXY"];allowed:=proxy!="";for _,part:=range strings.FieldsFunc(proxy,func(r rune)bool{return r==','||r=='|'}){if part!="off"&&!strings.HasPrefix(part,"file://"){allowed=false}};if !allowed{mu.Lock();unhandled=append(unhandled,"non-file Go proxy");mu.Unlock();return release.SupplyCommandResult{ExitCode:-1,Err:fmt.Errorf("network Go proxy forbidden in fixture")}}}
   cmd:=exec.CommandContext(ctx,c.Program,args...);cmd.Dir=c.Dir;cmd.Env=env;cmd.Stdin=bytes.NewReader(c.Stdin)
   var stdout,stderr bytes.Buffer;cmd.Stdout=&stdout;cmd.Stderr=&stderr
   err:=cmd.Run();rc:=0;if err!=nil {rc=-1;if ee,ok:=err.(*exec.ExitError);ok {rc=ee.ExitCode()}}
   observedEnv:=map[string]string{}
   for _,key:=range []string{"GOWORK","GOMODCACHE","GOPROXY","GOOS","GOARCH","CGO_ENABLED","GOSUMDB","GONOSUMDB","GONOPROXY","GOPRIVATE","GOTOOLCHAIN"}{if value,ok:=values[key];ok {if key=="GOPROXY" && value!="off" && !strings.HasPrefix(value,"file://"){value="<non-file-proxy>"};observedEnv[key]=value}}
   record:=map[string]any{"program":c.Program,"args":c.Args,"actual_args":args,"cwd":c.Dir,"go_environment":observedEnv,"stdin":string(c.Stdin),"exit_code":rc}
   encoded,_:=json.MarshalIndent(record,"","  ")
   prefix:=filepath.Join(request.Output,fmt.Sprintf("command-%04d",id))
   if e:=os.WriteFile(prefix+".json",encoded,0600);e!=nil {panic(e)}
   if e:=os.WriteFile(prefix+".stdout",stdout.Bytes(),0600);e!=nil {panic(e)}
   if e:=os.WriteFile(prefix+".stderr",stderr.Bytes(),0600);e!=nil {panic(e)}
   return release.SupplyCommandResult{Stdout:stdout.Bytes(),Stderr:stderr.Bytes(),ExitCode:rc,Err:err}
  },
  HTTP:func(ctx context.Context,r *http.Request)(*http.Response,error) {
   // Consumers-mode fixtures have no external prerequisites. Their source
   // revisions and every dependency archive are local and exact. An HTTP
   // request therefore cannot silently reach the live network.
   mu.Lock();unhandled=append(unhandled,"http:"+r.Method+" "+r.URL.Redacted());mu.Unlock()
   return nil,fmt.Errorf("unhandled HTTP boundary: %s %s",r.Method,r.URL)
  },
  Now:time.Now,
  Sleep:func(ctx context.Context,d time.Duration) error {select {case <-ctx.Done():return ctx.Err();case <-time.After(d):return nil}},
 }
 var stdout,stderr bytes.Buffer
 ctx,cancel:=context.WithTimeout(context.Background(),110*time.Second);defer cancel()
 rc:=release.RunSupplyPreflight(ctx,request.Args,deps,&stdout,&stderr)
 for suffix,data:=range map[string][]byte{"sut.stdout":stdout.Bytes(),"sut.stderr":stderr.Bytes()} {if e:=os.WriteFile(filepath.Join(request.Output,suffix),data,0600);e!=nil{t.Fatal(e)}}
 meta:=map[string]any{"exit_code":rc,"commands":ordinal,"unhandled_boundaries":unhandled,"deadline_exceeded":ctx.Err()!=nil}
 b,e:=json.MarshalIndent(meta,"","  ");if e!=nil {t.Fatal(e)}
 if e:=os.WriteFile(filepath.Join(request.Output,"bridge-result.json"),b,0600);e!=nil{t.Fatal(e)}
}
`

const rsResultSchemaGzip = "H4sIAAAAAAAC/+1dW3ebuBZ+z6/I8pq3U+okc1lr+kZsEjOxwQM405xOhiXbsk2DEQM4TU6X//sRGDASEsip3Uw89CVGt33R1ta3tQX9enJ62oqefdj6cNpC489wErXexWVgOnUiB3nAHQbIh0HkwBC3mQE3hEmDAP69cgI4xYWf8DMuCScLuAT2IwxC3DEZBpf6CxDC7AGtogla5o8BBOG2JXxyInuCpnn1NHi2g1VeH0ZgXujqo9CJUPCcldB0XeDZ4QJc/PxLVuR4/iqiyibAmzpTEMG4PCvEhOwIBHMYFUtDD/jhAkVh3hd64Wr7tICTh/wJzmZYl2ELP90n+vKLavzK1FhWHg+GvDDCBedJwZrQZaEZ9FbLfAJysVMeNjqEroPHLxYF0IXbOdn0CtCYKMjVQhRiplZLzCzR1fESKeN/9wS32WRX8nttKIpWHM9QusXHkSlfK7ZiGLpRLNZ0y1Y+Kp2RhZszqae2VUlcvymOqWq3cl/t2viv3pEtVddYtR1du1KNQaneUIa6qVq6cWcPVBPXd3pk9+HIstNByhWdnqxdU5JrXaXTl7E+7KFs9ey+bpokwY6i3qrata3/oSmG2VOHCXN9tWMV213KptJXNazEwdC6Y9aYltxXmDUjTb6V1b58SdYP5c5NPC9XfV03SpxhLszRQDFsdTDUDSvWh4n5ZDbpKPiHWeYtb7BRQqJulvpMfWR0+HwatqH8PlIpo7Jk41qx7NiI8KgDWdUo0paiWcxpzEbDE6Z0bkz7ChMlx74cdeOxlY89eWRaZN3Q0D/ecXkdXfZVs4fHlo1OT71VmAzkerkcqf0ug/5Q1TAFQ5GxSdEUcBVH5oHeHfUV7myoWCOGJve5DdL+A3k4jC2SMVGWjIu7WK+qdce00z8M1VJsfWR19EE8oTcatmvS6O76utxlqgVPdE+Ru6x1dIvXBsd4MCW8tNRLtR8zVdZlTzY0xTR5M2YoAx2zvGHcUH5TOtR8K1dXuIwhNtNjbfe/Kqd1th3/fPvzYvvzR+bg2V5aGDrb9scI4Q3BaxHtN5ttpffUdI10GoasdXr20FBMLC81O/qQdPPYhLH/YLRNKro2njX1SlVKJsTokS2XvAtnP8gBQ0Eq4D3rM0Ksr/mvgorCKHC8eYFovPGBKIJBrNHWX59k6b9A+t+Z9Kv9Xrr/T5t6/qGVd1y/q6bkrVy30JopCgMvfIMcS8frQ28eLXK4sS8+ixDsADrH2gXS7P7rLz+t96xgAii+Kc5JOHtA1n862zfrFOh+U7xvQwOGfwVBAJ5ztlpOBJfFduwALBO9JgzLIT0VjGXiI8eLqOUOHK8Q1aSlaBzC4BFObUA1T2fECW3gTWCI/edWF/dkBEGHNwQPZCFrP0nLx2Rokg8PpS+BE7GqUBildUTVfeFpzVZBiakKUxIzJppUUa87UpuhYJn0asXrWYqcJeTTYUwTlxyx35NWvf21Jt3KJtJlGDdlskIGyzbX1OvOHBeSAWYAUx2yqh4dtAptH0weMF4JmSEsu5YVy2aFtrP0URARdUs0XVHksV1jcwCuncqzhB7Zxw/haoqy+D4kg/AQubFQSfzMWMo2cZ6Q2t+zi0CmhxNqEfKWIKFXam2WPSvtBonJjgWeE4cJWwzhLJP1fEZUrd+JDUz6WNIiiyt5XWEXxyUaZddHJBxjXR6TdLlfOUKhcr94RLJlfv2IRGLvS8fkH6l99YhEI3HBUQlG4ppjMkcCl711wdgBAD1p/4joFpc5E0AFhVSSMR+jkGpMy8JVwlFIlsIngHWNqe0a5+bcCMe6jveIcA+HYmxTN8Ve24meGTUxni8Xoy8e9ocLxy9XxTG1i2ViBM8bCCbNXIQYRpdt/BKR6mQ1AMFk4TwyKSSLg9V1iQNqZ+y4TCl9BjuZIUil2KgQ2UubCDhgjBmBOYsQemLRx/5XQoEzdzxOZUxM9MihnAutMQ06L8rMjlbmSCsypXXclnKnNcwSeVSRbKpYTlUos1qTX63JsorlWnfLuNblXeuyr2I5WJFMrHA+VjAru1tuViRDW5mnFcnW1udsRTO39flbgSzuLrlcwYxubV63NrsrlOMVzPSK5nsFs76Cud/6DHBtHrg2GyyWExbKDAvmhwWzxHXeOocx3BNfEqNxkZrIsTQ3YUgjyjUbVJV4ZOPjMpAVwMgVKLmEkysJlLEyKRs1GzXYObsK9iLwjDxYUg9nlkv4egeMXYWzk7oHx5uyABl1C69olhFgpWsCOCsXwicfs46DQzoxtRkJF5LapxMzPFC+Zf0Da76zW3atcYBB46JVZzLsSwTiS+fFtwZ4/Gx0zGSFg9VSL0lfoihUypf8Op5nzjYyht+iVwtPsbN9aBQPE7YXEEzD9qe//vwzFFIiYXz7mFZeapAzhzyqPM/Idl5ijO7GLJNhAfIs/1n2oUzLqDrbaAHXFd0rHLY91TmL2nVFe45sKTEbroV0wNBwK1pA75sE4BjVLnayq6UIy/uCnfl7TSffhzXz+Ybms2LzE9jJyI2Q0+D+bVoIzzW/eLLFgpQ3DVljQGEzcWt8sGpXI9qkMxPW+oGNzXTMOuxnt4+vUy5B8ACDA8NhP2ig8KtB4dzaXgsP50ZdbSSFscsn4ZU4O18S3x9sb9fcviE3/1CCOpg4f32Q3cQgu6mn6Hn3YLMdVTJM6fw0Hlba3DL/wLkT3kRFR4CieUug0hft5F5qHUwD7/7x8G4JgzlkD5VfWD8s7Es4aJBfg/z+hchvp9zSeQOovg1QbX1doyS+knivKTXZgQYHNzj4pTiYF6BXuqTmnL05Z3+BnTSn7UdyQSS+OdwERsdwOwTPZHM5pIF/DfxrQM6u09lcDmlA69sErQ1cPRhcjT9b9ELIyiCffJnTdqYHhrMpIclDUXPg/3q4NrOdBts22LbKGfyb76o0mP8Ij3xrbHvvZ777Ri8ndGn6xtdJKnLrh82nvWPuF1Hkhx/a7c8h8qRN8XsUzNvTAMyi9sXZxZl0ftFO2286J2rJO86daLEav5+gZXto6JKBxnjXnizaD2CyQNIXFDyEPpjA9hRNwvZkAbw5xH8dKdvkw5Xvu8/tAIYrN3qfcvA5+xpCK3IiN1Fzfi0JmxMK4fR00+N00+NUOu0a8pWVfh6eWHj5e23UIqv4WgL3hfh8IW3egK9+zy5/c660PCpIs76zSxE/2/nV+Jxt/WaHD+sdRHEG+aL/d1Tb+StLXvwQwuto4OKVNcD72sN3VMGPB1VB+TvWFPkoWMFDCF56iTfbe8CTmr62e/Y6k8/7KsfBvhWyJ5XyXCkOSllhLcfDUm+dv8KaO9xGxbM57qv7NUiwEsYKQNhqhCQ0D/H/h3KyPvk/4Xb1D3hmAAA="

// The accepted JSON schema is an immutable test oracle, not a second release
// implementation. All result objects are validated before semantic assertions.
func mustRSRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func rsSchema(t *testing.T) []byte {
	t.Helper()
	compressed, e := base64.StdEncoding.DecodeString(rsResultSchemaGzip)
	if e != nil {
		t.Fatal(e)
	}
	z, e := gzip.NewReader(bytes.NewReader(compressed))
	if e != nil {
		t.Fatal(e)
	}
	raw, e := io.ReadAll(z)
	z.Close()
	if e != nil {
		t.Fatal(e)
	}
	if rsSHA(raw) != "d4cd888ef8edea93f2b771fe2d5b65b4dbeb01cd01603dd2843fc5bfca13da32" {
		t.Fatal("HARNESS_NOT_EXECUTED: accepted result schema drift")
	}
	return raw
}

func rsSupplySymbols(h *rsHarness, names []string) []string {
	h.t.Helper()
	root := moduleRoot(h.t)
	raw := h.must(root, h.goBin, "list", "-json", "./internal/release")
	var pkg struct {
		Dir            string
		GoFiles        []string
		InvalidGoFiles []string
		Error          any
	}
	if e := json.Unmarshal([]byte(raw), &pkg); e != nil || pkg.Error != nil || len(pkg.InvalidGoFiles) > 0 {
		h.t.Fatalf("HARNESS_NOT_EXECUTED: package discovery: %v, %+v", e, pkg)
	}
	found := map[string]bool{}
	for _, name := range pkg.GoFiles {
		parsed, e := parser.ParseFile(token.NewFileSet(), filepath.Join(pkg.Dir, name), nil, parser.AllErrors)
		if e != nil {
			h.t.Fatalf("HARNESS_NOT_EXECUTED: source parse: %v", e)
		}
		for _, d := range parsed.Decls {
			switch x := d.(type) {
			case *ast.FuncDecl:
				if x.Recv == nil {
					found[x.Name.Name] = true
				}
			case *ast.GenDecl:
				for _, spec := range x.Specs {
					if ty, ok := spec.(*ast.TypeSpec); ok {
						found[ty.Name.Name] = true
					}
				}
			}
		}
	}
	missing := []string{}
	for _, name := range names {
		if !found[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	return missing
}

type rsConsumerFixture struct {
	h                                                              *rsHarness
	target, consumer, foreign, revision, consumerRevision, modPath string
	programPaths                                                   []string
	archive                                                        rsArchive
}

func rsPrepareConsumers(t *testing.T) rsConsumerFixture {
	t.Helper()
	h := newRSHarness(t)
	modPath := "github.com/PRO-Robotech/corelib"
	files := map[string]string{"go.mod": "module " + modPath + "\n\ngo 1.21\n"}
	for _, name := range []string{"first", "second", "testonly", "linuxonly", "windowsonly"} {
		files[name+"/value.go"] = "package " + name + "\n\nconst Value = 1\n"
	}
	programs := map[string]string{
		"main.go":         "package main\nimport _ \"" + modPath + "/first\"\nimport _ \"" + modPath + "/second\"\nfunc main() {}\n",
		"main_test.go":    "package main\nimport _ \"" + modPath + "/testonly\"\n",
		"only_linux.go":   "//go:build linux\n\npackage main\nimport _ \"" + modPath + "/linuxonly\"\n",
		"only_windows.go": "//go:build windows\n\npackage main\nimport _ \"" + modPath + "/windowsonly\"\n",
	}
	paths := []string{}
	for name, body := range programs {
		files["testdata/consumer/"+name] = body
		paths = append(paths, "testdata/consumer/"+name)
	}
	sort.Strings(paths)
	files["testdata/invalid/main.go"] = "package main\nfunc broken(\n"
	files["testdata/zero/main.go"] = "package main\nfunc main() {}\n"
	files["testdata/prefix/main.go"] = "package main\nimport _ \"" + modPath + "extra/second\"\nfunc main() {}\n"
	target := h.initRepo("declared-target", files)
	h.must(target, "git", "remote", "add", "origin", "https://github.com/PRO-Robotech/corelib.git")
	archive := h.archive(target, modPath, "declared-lawful")
	consumerFiles := map[string]string{"go.mod": "module github.com/PRO-Robotech/kacho\n\ngo 1.21\n\nrequire " + modPath + " " + archive.version + "\n"}
	for name, body := range programs {
		consumerFiles[name] = body
	}
	consumer := h.initRepo("declared-consumer", consumerFiles)
	h.must(consumer, "git", "remote", "add", "origin", "https://github.com/PRO-Robotech/kacho.git")
	revision := h.must(consumer, "git", "rev-parse", "HEAD")
	foreign := filepath.Join(h.root, "consumer-origin.git")
	h.must(h.root, "git", "clone", "--bare", "--no-local", consumer, foreign)
	// Before SUT: every actual consumer import, including tests and the two
	// build-constrained files, is compiled by the real Go tool from this ZIP.
	for _, goos := range []string{"linux", "windows"} {
		cache := filepath.Join(h.root, "control-cache-"+goos)
		extra := []string{"GOPROXY=file://" + filepath.ToSlash(archive.proxy), "GOMODCACHE=" + cache, "GOSUMDB=off", "GOPRIVATE=", "GONOPROXY=", "GONOSUMDB=", "GOOS=" + goos, "GOARCH=amd64", "CGO_ENABLED=0", "GOFLAGS=-mod=mod"}
		_, errout, rc := h.run(consumer, extra, h.goBin, "test", "-c", "-o", filepath.Join(h.root, "consumer-"+goos+".test"), ".")
		if rc != 0 {
			t.Fatalf("HARNESS_NOT_EXECUTED: lawful real %s test-source build: %s", goos, errout)
		}
		built, err := os.ReadFile(filepath.Join(h.root, "consumer-"+goos+".test"))
		if err != nil || len(built) == 0 {
			t.Fatalf("HARNESS_NOT_EXECUTED: %s compile produced no test binary: %v", goos, err)
		}
		record, _ := json.MarshalIndent(map[string]any{"goos": goos, "goarch": "amd64", "compiled_bytes": len(built), "compiled_sha256": rsSHA(built), "declared_target_imports": 5, "archive_sha256": rsSHA(mustRSRead(t, archive.path))}, "", "  ")
		h.save("lawful-"+goos+"-compile.json", append(record, '\n'))
		h.t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			c := exec.CommandContext(ctx, h.goBin, "clean", "-modcache")
			c.Env = rsEnvironment(extra...)
			_ = c.Run()
		})
	}
	// Artifact evidence must not confuse a successful control with a SUT result.
	h.save("lawful-programs.txt", []byte(h.must(consumer, "git", "ls-files")+"\n"))
	return rsConsumerFixture{h, target, consumer, foreign, archive.revision, revision, modPath, paths, archive}
}

func (f rsConsumerFixture) manifest(kind string) map[string]any {
	contexts := []any{map[string]any{"goos": "linux", "goarch": "amd64", "cgo_enabled": false, "tags": []string{}}, map[string]any{"goos": "windows", "goarch": "amd64", "cgo_enabled": false, "tags": []string{}}}
	var declaration map[string]any
	switch kind {
	case "repository":
		declaration = map[string]any{"type": "repository", "repository": "PRO-Robotech/kacho", "root": f.consumer, "revision": f.consumerRevision, "module_roots": []string{"."}, "contexts": contexts}
	case "candidate-program":
		declaration = map[string]any{"type": "external-program", "source": map[string]any{"kind": "candidate"}, "program_paths": f.programPaths, "contexts": contexts}
	case "repository-program":
		paths := []string{}
		for _, p := range f.programPaths {
			paths = append(paths, strings.TrimPrefix(p, "testdata/consumer/"))
		}
		declaration = map[string]any{"type": "external-program", "source": map[string]any{"kind": "repository", "repository": "PRO-Robotech/kacho", "root": f.consumer, "revision": f.consumerRevision}, "program_paths": paths, "contexts": contexts}
	default:
		f.h.t.Fatalf("HARNESS_NOT_EXECUTED: unknown declaration kind %q", kind)
	}
	return map[string]any{"schema_version": 1, "repository": "PRO-Robotech/corelib", "module_path": f.modPath, "version": "v1.1.0", "candidate_root": f.target, "consumers": []any{declaration}, "budgets": map[string]any{"network_seconds": 2, "checks_seconds": 5}}
}

type rsConsumerCase struct {
	Name, Scenario, Kind, Axis, Outcome, Reason string
	Imports                                     int
	Manifest                                    map[string]any
	Revision                                    string
	ExtraEnv                                    []string
}

func rsCloneJSON(m map[string]any) map[string]any {
	b, e := json.Marshal(m)
	if e != nil {
		panic(e)
	}
	var out map[string]any
	if e = json.Unmarshal(b, &out); e != nil {
		panic(e)
	}
	return out
}
func rsDeclaration(m map[string]any) map[string]any {
	return m["consumers"].([]any)[0].(map[string]any)
}

func rsConsumerCases(f rsConsumerFixture) []rsConsumerCase {
	h := f.h
	cases := []rsConsumerCase{}
	add := func(name, scenario, kind, axis, outcome, reason string, count int, m map[string]any, rev string) {
		cases = append(cases, rsConsumerCase{name, scenario, kind, axis, outcome, reason, count, m, rev, nil})
	}
	for _, kind := range []string{"repository", "candidate-program", "repository-program"} {
		good := f.manifest(kind)
		add(kind+"-lawful", "CI-RS-07", kind, "none", "GREEN", "OK", 5, good, f.revision)
		m := rsCloneJSON(good)
		m["consumers"] = []any{}
		add(kind+"-empty", "CI-RS-08", kind, "consumers: nonempty -> []", "RED", "CONSUMER_CENSUS_EMPTY", -1, m, f.revision)
		m = rsCloneJSON(good)
		d := rsDeclaration(m)
		if kind == "repository" {
			d["module_roots"] = []string{"missing-module"}
		} else {
			d["program_paths"] = []string{"missing-program.go"}
		}
		add(kind+"-missing-declaration", "CI-RS-08", kind, "one declared source coordinate -> missing", "RED", "CONSUMER_DECLARATION_INVALID", -1, m, f.revision)
		if kind != "candidate-program" {
			m = rsCloneJSON(good)
			d = rsDeclaration(m)
			if kind == "repository" {
				d["revision"] = strings.Repeat("f", 40)
			} else {
				d["source"].(map[string]any)["revision"] = strings.Repeat("f", 40)
			}
			add(kind+"-unavailable-revision", "CI-RS-08", kind, "only pinned revision -> absent remote commit", "NOT_EXECUTED", "SOURCE_UNAVAILABLE", -1, m, f.revision)
		}
	}
	for _, path := range []string{"testdata/zero/main.go", "testdata/prefix/main.go"} {
		m := rsCloneJSON(f.manifest("candidate-program"))
		rsDeclaration(m)["program_paths"] = []string{path}
		add("candidate-program-zero-"+strings.Split(path, "/")[1], "CI-RS-08", "candidate-program", "program_paths -> one tracked zero-target-import source", "RED", "CONSUMER_CENSUS_EMPTY", 0, m, f.revision)
	}
	// Each tracked Go source is an independent single-fact archive omission.
	// The consumer declarations and contexts retain their exact original bytes.
	for _, name := range []string{"second", "testonly", "linuxonly", "windowsonly"} {
		h.must(f.target, "git", "checkout", "--force", "--quiet", f.revision)
		path := name + "/value.go"
		h.must(f.target, "git", "rm", "--cached", path)
		h.must(f.target, "git", "commit", "--quiet", "-m", "single omitted package: "+name)
		bad := h.archive(f.target, f.modPath, "missing-"+name)
		delta := h.must(f.target, "git", "diff", "--name-status", f.revision, bad.revision)
		if delta != "D\t"+path {
			h.t.Fatalf("HARNESS_NOT_EXECUTED: unexpected single-fact delta %q", delta)
		}
		// Independent real clean consumer build proves that the changed package
		// is absent from this exact archive, including test/constrained packages.
		program := "package main\nimport _ \"" + f.modPath + "/" + name + "\"\nfunc main(){}\n"
		output, rc := h.buildConsumer(bad, f.modPath, program, "missing-control-"+name)
		if rc == 0 || !strings.Contains(output, f.modPath+"/"+name) {
			h.t.Fatalf("HARNESS_NOT_EXECUTED: real omission inversion %s: rc=%d %s", name, rc, output)
		}
		snapshot := filepath.Join(h.root, "candidate-missing-"+name)
		h.must(h.root, "git", "clone", "--no-local", f.target, snapshot)
		h.must(snapshot, "git", "checkout", "--quiet", bad.revision)
		// Keep the omitted bytes visible in the working tree. Go ZIP may not use them.
		raw, e := os.ReadFile(filepath.Join(f.target, path))
		if e != nil {
			h.t.Fatal(e)
		}
		h.put(snapshot, path, string(raw))
		h.must(snapshot, "git", "remote", "set-url", "origin", "https://github.com/PRO-Robotech/corelib.git")
		for _, kind := range []string{"repository", "candidate-program", "repository-program"} {
			m := rsCloneJSON(f.manifest(kind))
			m["candidate_root"] = snapshot
			add(kind+"-missing-"+name, "CI-RS-07/09", kind, "archive omits only "+path+"; working file remains", "RED", "CONSUMER_IMPORT_MISSING", 5, m, bad.revision)
		}
	}
	m := rsCloneJSON(f.manifest("candidate-program"))
	rsDeclaration(m)["program_paths"] = []string{"testdata/invalid/main.go"}
	add("candidate-program-malformed", "CI-RS-08", "candidate-program", "program_paths -> one tracked malformed Go source", "RED", "CONSUMER_DECLARATION_INVALID", -1, m, f.revision)
	m = rsCloneJSON(f.manifest("candidate-program"))
	rsDeclaration(m)["contexts"] = []any{map[string]any{"goos": "linux", "goarch": "amd64", "cgo_enabled": false, "tags": []string{}}}
	add("candidate-program-uncovered-windows-import", "CI-RS-08", "candidate-program", "contexts: remove only windows", "RED", "CONSUMER_DECLARATION_INVALID", -1, m, f.revision)
	h.must(f.target, "git", "checkout", "--force", "--quiet", f.revision)
	// Existing package bytes are deliberately made reachable via a workspace;
	// a correct SUT still resolves the candidate archive with GOWORK=off.
	workspace := filepath.Join(h.root, "poison.go.work")
	h.put(h.root, "poison.go.work", "go 1.21\nuse (\n"+filepath.ToSlash(f.target)+"\n"+filepath.ToSlash(f.consumer)+"\n)\n")
	for i := range cases {
		if strings.HasSuffix(cases[i].Name, "-missing-second") {
			cases[i].ExtraEnv = []string{"GOWORK=" + workspace}
		}
	}
	return cases
}

func rsBuildConsumerBridge(h *rsHarness) string {
	h.t.Helper()
	root := moduleRoot(h.t)
	clone := filepath.Join(h.root, "sut-copy")
	h.must(h.root, "git", "clone", "--quiet", "--depth=1", "file://"+filepath.ToSlash(root), clone)
	// Compilation uses the exact current tracked production source. Uncommitted
	// source would otherwise be silently replaced by HEAD during clone.
	listing := h.must(root, h.goBin, "list", "-json", "./internal/release")
	var pkg struct{ GoFiles []string }
	if e := json.Unmarshal([]byte(listing), &pkg); e != nil {
		h.t.Fatal(e)
	}
	for _, name := range pkg.GoFiles {
		a, e := os.ReadFile(filepath.Join(root, "internal/release", name))
		if e != nil {
			h.t.Fatal(e)
		}
		b, e := os.ReadFile(filepath.Join(clone, "internal/release", name))
		if e != nil || !bytes.Equal(a, b) {
			h.t.Fatalf("HARNESS_NOT_EXECUTED: actual/committed source mismatch %s", name)
		}
	}
	h.put(clone, "internal/release/ci_rs_bridge_test.go", rsConsumerBridgeSource)
	binary := filepath.Join(h.root, "consumer-bridge.test")
	h.must(clone, h.goBin, "test", "-c", "-o", binary, "./internal/release")
	return binary
}

func rsValidateResult(h *rsHarness, raw []byte, label string) map[string]any {
	h.t.Helper()
	schemaPath := filepath.Join(h.root, "result.schema.json")
	h.put(h.root, "result.schema.json", string(rsSchema(h.t)))
	resultPath := filepath.Join(h.root, label+".result.json")
	h.put(h.root, label+".result.json", string(raw))
	script := "import json,sys,jsonschema\ndef pairs(items):\n d={}\n for k,v in items:\n  if k in d: raise ValueError('duplicate key:'+k)\n  d[k]=v\n return d\ns=json.load(open(sys.argv[1])); r=json.load(open(sys.argv[2]),object_pairs_hook=pairs); jsonschema.Draft202012Validator(s).validate(r)\n"
	_, errout, rc := h.run(h.root, nil, "python3", "-c", script, schemaPath, resultPath)
	if rc != 0 {
		h.t.Fatalf("HARNESS_NOT_EXECUTED: malformed SUT result, not a producer outcome: %s", errout)
	}
	var result map[string]any
	if e := json.Unmarshal(raw, &result); e != nil {
		h.t.Fatal(e)
	}
	return result
}

func rsChangedJSON(a, b any, path string) []string {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	if bytes.Equal(left, right) {
		return nil
	}
	am, aok := a.(map[string]any)
	bm, bok := b.(map[string]any)
	if aok && bok {
		keys := map[string]bool{}
		for k := range am {
			keys[k] = true
		}
		for k := range bm {
			keys[k] = true
		}
		out := []string{}
		for k := range keys {
			out = append(out, rsChangedJSON(am[k], bm[k], path+"."+k)...)
		}
		sort.Strings(out)
		return out
	}
	aa, aok := a.([]any)
	ba, bok := b.([]any)
	if aok && bok && len(aa) == len(ba) {
		out := []string{}
		for i := range aa {
			out = append(out, rsChangedJSON(aa[i], ba[i], fmt.Sprintf("%s[%d]", path, i))...)
		}
		return out
	}
	return []string{path}
}

func rsRunConsumerCases(t *testing.T) {
	t.Helper()
	f := rsPrepareConsumers(t)
	h := f.h
	cases := rsConsumerCases(f)
	h.must(h.root, "python3", "-c", "import sys,jsonschema; from importlib.metadata import version; print(sys.version); print(version('jsonschema')); print(jsonschema.Draft202012Validator.__name__)")
	ledger := []map[string]any{}
	for _, c := range cases {
		original := rsCloneJSON(f.manifest(c.Kind))
		current := rsCloneJSON(c.Manifest)
		changed := rsChangedJSON(original, current, "$")
		expectedChanges := 1
		if c.Outcome == "GREEN" {
			expectedChanges = 0
		}
		if len(changed) != expectedChanges {
			t.Fatalf("HARNESS_NOT_EXECUTED: %s computed manifest changes=%v; want %d", c.Name, changed, expectedChanges)
		}
		m, _ := json.MarshalIndent(c.Manifest, "", "  ")
		h.save(c.Name+".manifest.json", append(m, '\n'))
		ledger = append(ledger, map[string]any{"name": c.Name, "scenario": c.Scenario, "declaration_kind": c.Kind, "changed_fact": c.Axis, "computed_manifest_fields": changed, "expected_outcome": c.Outcome, "expected_reason": c.Reason, "expected_imports": c.Imports, "revision": c.Revision, "manifest_sha256": rsSHA(append(m, '\n')), "sut_invocations": 0})
	}
	raw, _ := json.MarshalIndent(ledger, "", "  ")
	h.save("consumer-case-ledger.json", append(raw, '\n'))
	if _, err := parser.ParseFile(token.NewFileSet(), "ci_rs_bridge_test.go", rsConsumerBridgeSource, parser.AllErrors); err != nil {
		t.Fatalf("HARNESS_NOT_EXECUTED: bridge syntax: %v", err)
	}
	missing := rsSupplySymbols(h, []string{"RunSupplyPreflight", "SupplyDependencies", "SupplyCommand", "SupplyCommandResult"})
	if len(missing) > 0 {
		b, _ := json.MarshalIndent(map[string]any{"holder_outcome": "CAPABILITY_ABSENT", "boundary": "supply-entrypoints", "missing_symbols": missing, "prepared_cases": len(cases), "sut_invocations": 0, "sut_semantic_decisions": 0}, "", "  ")
		h.save("capability.json", append(b, '\n'))
		t.Fatalf("CAPABILITY_ABSENT: validated real prerequisites; prepared cases=%d; missing %v; sut_invocations=0, sut_semantic_decisions=0", len(cases), missing)
	}
	binary := rsBuildConsumerBridge(h)
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			caseHarness := *h
			caseHarness.t = t
			caseHarness.serial = 0
			if h.capture != "" {
				caseHarness.capture = filepath.Join(h.capture, c.Name)
				if e := os.MkdirAll(caseHarness.capture, 0755); e != nil {
					t.Fatal(e)
				}
			}
			outDir := filepath.Join(h.root, "result-"+c.Name)
			if e := os.MkdirAll(outDir, 0755); e != nil {
				t.Fatal(e)
			}
			manifestPath := filepath.Join(h.root, c.Name+".manifest.json")
			b, _ := json.Marshal(c.Manifest)
			h.put(h.root, c.Name+".manifest.json", string(b))
			args := []string{"--mode", "consumers", "--manifest", manifestPath, "--revision", c.Revision}
			request := map[string]any{"args": args, "output": outDir, "remotes": map[string]string{"https://github.com/PRO-Robotech/kacho.git": "file://" + filepath.ToSlash(f.foreign)}}
			encoded, _ := json.Marshal(request)
			requestPath := filepath.Join(h.root, c.Name+".request.json")
			h.put(h.root, c.Name+".request.json", string(encoded))
			extra := append([]string{"CI_RS_BRIDGE_REQUEST=" + requestPath, "GOPROXY=off", "GOSUMDB=off"}, c.ExtraEnv...)
			_, stderr, bridgeRC := caseHarness.run(h.root, extra, binary, "-test.run=^TestCIRSConsumerBridge$", "-test.v", "-test.timeout=115s")
			if bridgeRC != 0 {
				t.Fatalf("HARNESS_NOT_EXECUTED: bridge rc=%d %s", bridgeRC, stderr)
			}
			metadata, e := os.ReadFile(filepath.Join(outDir, "bridge-result.json"))
			if e != nil {
				t.Fatal(e)
			}
			var meta struct {
				Exit      int      `json:"exit_code"`
				Unhandled []string `json:"unhandled_boundaries"`
				Deadline  bool     `json:"deadline_exceeded"`
			}
			if e = json.Unmarshal(metadata, &meta); e != nil || len(meta.Unhandled) > 0 || meta.Deadline {
				t.Fatalf("HARNESS_NOT_EXECUTED: bridge metadata: %v %+v", e, meta)
			}
			entries, e := os.ReadDir(outDir)
			if e != nil {
				t.Fatal(e)
			}
			for _, entry := range entries {
				b, e := os.ReadFile(filepath.Join(outDir, entry.Name()))
				if e != nil {
					t.Fatal(e)
				}
				caseHarness.save(entry.Name(), b)
				if strings.HasPrefix(entry.Name(), "command-") && strings.HasSuffix(entry.Name(), ".json") {
					var record struct {
						Program       string            `json:"program"`
						Args          []string          `json:"args"`
						GoEnvironment map[string]string `json:"go_environment"`
					}
					if err := json.Unmarshal(b, &record); err != nil {
						t.Fatalf("HARNESS_NOT_EXECUTED: command journal: %v", err)
					}
					if filepath.Base(record.Program) == "git" {
						for _, arg := range record.Args {
							if arg == "push" {
								t.Errorf("SEMANTIC_MISMATCH: read-only consumers mode attempted git push")
							}
						}
					}
					if filepath.Base(record.Program) == "go" && len(record.Args) > 0 && (record.Args[0] == "build" || record.Args[0] == "test" || record.Args[0] == "list") {
						if record.GoEnvironment["GOWORK"] != "off" || record.GoEnvironment["GOMODCACHE"] == "" || !strings.HasPrefix(record.GoEnvironment["GOPROXY"], "file://") {
							t.Errorf("SEMANTIC_MISMATCH: consumer build is not isolated: %v", record.GoEnvironment)
						}
					}
				}
			}
			stdout, e := os.ReadFile(filepath.Join(outDir, "sut.stdout"))
			if e != nil {
				t.Fatal(e)
			}
			result := rsValidateResult(&caseHarness, stdout, c.Name)
			wantRC := map[string]int{"GREEN": 0, "RED": 1, "NOT_EXECUTED": 3}[c.Outcome]
			if meta.Exit != wantRC || result["exit_code"] != float64(wantRC) || result["outcome"] != c.Outcome || result["reason"] != c.Reason {
				t.Errorf("SEMANTIC_MISMATCH: want %s/%s/exit%d, bridge exit=%d, result=%s", c.Outcome, c.Reason, wantRC, meta.Exit, stdout)
			}
			if result["phase"] != "consumers" || result["stage"] != "NONE" || len(result["effects"].([]any)) != 0 {
				t.Errorf("SEMANTIC_MISMATCH: consumers mode leaked phase/stage/effects: %s", stdout)
			}
			checks := result["checks"].([]any)
			seen := map[string]bool{}
			for _, raw := range checks {
				check := raw.(map[string]any)
				predicate := check["predicate"].(string)
				if seen[predicate] {
					t.Errorf("SEMANTIC_MISMATCH: duplicate predicate %s", predicate)
				}
				seen[predicate] = true
			}
			if c.Outcome == "GREEN" {
				want := []string{"invocation", "identity", "input", "consumer-census", "consumer-archive"}
				if len(seen) != len(want) {
					t.Errorf("SEMANTIC_MISMATCH: required predicate set %v", seen)
				}
				for _, p := range want {
					if !seen[p] {
						t.Errorf("SEMANTIC_MISMATCH: missing predicate %s", p)
					}
				}
				for _, raw := range checks {
					if raw.(map[string]any)["outcome"] != "GREEN" {
						t.Errorf("SEMANTIC_MISMATCH: GREEN hides non-GREEN check")
					}
				}
			}
			if c.Imports >= 0 {
				census := result["census"].(map[string]any)
				if census["consumer_imports"] != float64(c.Imports) {
					t.Errorf("SEMANTIC_MISMATCH: consumer import census got=%v want=%d", census["consumer_imports"], c.Imports)
				}
			}
			if c.Outcome == "GREEN" && result["candidate_sha"] != c.Revision {
				t.Errorf("SEMANTIC_MISMATCH: candidate binding got=%v want=%s", result["candidate_sha"], c.Revision)
			}
		})
	}
}

func TestReleaseDeclaredConsumerImports(t *testing.T) {
	t.Run("real_archive_prerequisites", func(t *testing.T) { rsTwoImportPrerequisites(t) })
	t.Run("declared_consumers_contract", func(t *testing.T) { rsRunConsumerCases(t) })
}
