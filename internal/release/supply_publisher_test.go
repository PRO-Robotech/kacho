// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package release

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"golang.org/x/mod/module"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Publisher fixtures use the same real Git/Go boundaries as the frozen consumer
// holder. Only the receiving forge, proxy, clock and the owned receive-pack
// server endpoint are test adapters; no command result is manufactured.
func rsPublisherBridgeSource(t *testing.T) string {
	t.Helper()
	s := rsConsumerBridgeSource
	replace := func(old, next string) {
		if strings.Count(s, old) != 1 {
			t.Fatalf("HARNESS_NOT_EXECUTED: publisher bridge anchor %q", old)
		}
		s = strings.Replace(s, old, next, 1)
	}
	replace(" \"net/http\"\n", " \"net/http\"\n \"net/url\"\n \"io\"\n")
	replace("type bridgeRequest struct {", `type bridgeRequest struct {
 Endpoint string `+"`json:\"endpoint\"`"+`
 ReceivePack string `+"`json:\"receive_pack\"`"+`
`)
	replace("var mu sync.Mutex;ordinal:=0;unhandled:=[]string{}", "var mu sync.Mutex;ordinal:=0;unhandled:=[]string{};virtualNow:=time.Now();sleeps:=[]int64{}")
	// Derive the command position from the independently fixed global-option
	// parser, never by searching for an arbitrary argument equal to push.
	start := strings.Index(s, "func rsGitCommandVerb(")
	end := strings.Index(s[start:], "\nfunc rsGitTransportArgs(")
	if start < 0 || end < 0 {
		t.Fatal("HARNESS_NOT_EXECUTED: Git parser fixture source")
	}
	indexParser := s[start : start+end]
	indexParser = strings.Replace(indexParser, "rsGitCommandVerb(args []string) string", "rsPublisherGitCommandIndex(args []string) int", 1)
	indexParser = strings.ReplaceAll(indexParser, `return ""`, `return -1`)
	indexParser = strings.ReplaceAll(indexParser, "return arg", "return i")
	s = s[:start] + indexParser + "\n" + s[start:]
	replace("args=rsGitTransportArgs(c.Args,request.Remotes)", `args=rsGitTransportArgs(c.Args,request.Remotes)
    if rsGitCommandVerb(c.Args)=="push" {
     if request.ReceivePack=="" {mu.Lock();unhandled=append(unhandled,"publisher fixture receive-pack missing");mu.Unlock();return release.SupplyCommandResult{ExitCode:-1,Err:fmt.Errorf("fixture receive-pack missing")}}
     i:=rsPublisherGitCommandIndex(args);if i<0||args[i]!="push"{panic("fixture Git verb parser disagreement")}
     next:=append([]string{},args[:i+1]...);next=append(next,"--receive-pack="+request.ReceivePack);args=append(next,args[i+1:]...)
    }`)
	start = strings.Index(s, "  HTTP:func(ctx context.Context,r *http.Request)(*http.Response,error) {")
	end = strings.Index(s[start:], "  Now:time.Now,")
	if start < 0 || end < 0 {
		t.Fatal("HARNESS_NOT_EXECUTED: publisher HTTP seam anchor")
	}
	httpCode := `  HTTP:func(ctx context.Context,r *http.Request)(*http.Response,error) {
   mu.Lock();ordinal++;id:=ordinal;mu.Unlock()
   if r.URL.Scheme!="https"||(r.URL.Host!="api.github.com"&&r.URL.Host!="proxy.golang.org") {mu.Lock();unhandled=append(unhandled,"foreign HTTP endpoint");mu.Unlock();return nil,fmt.Errorf("foreign fixture HTTP endpoint")}
   endpoint,err:=url.Parse(request.Endpoint);if err!=nil||endpoint.Scheme!="http"||endpoint.Hostname()!="127.0.0.1"{panic("invalid owned forge fixture")}
   cloned:=r.Clone(ctx);cloned.URL=&url.URL{Scheme:endpoint.Scheme,Host:endpoint.Host,Path:"/fixture"};cloned.Host=endpoint.Host
   cloned.Header=make(http.Header);cloned.Header.Set("X-CI-RS-Original-URL",r.URL.String());cloned.Header.Set("Content-Type","application/json")
   client:=&http.Client{Transport:&http.Transport{Proxy:nil},CheckRedirect:func(*http.Request,[]*http.Request)error{return fmt.Errorf("fixture redirect forbidden")}}
   response,err:=client.Do(cloned);record:=map[string]any{"method":r.Method,"url":r.URL.String(),"error":fmt.Sprint(err)}
   if response!=nil{record["status"]=response.StatusCode;if response.StatusCode==599 {mu.Lock();unhandled=append(unhandled,"fixture unknown HTTP route");mu.Unlock()};raw,readErr:=io.ReadAll(response.Body);response.Body.Close();if readErr!=nil{return nil,readErr};response.Body=io.NopCloser(bytes.NewReader(raw))}
   encoded,_:=json.MarshalIndent(record,"","  ");if e:=os.WriteFile(filepath.Join(request.Output,fmt.Sprintf("http-%04d.json",id)),encoded,0600);e!=nil{panic(e)}
   client.CloseIdleConnections();return response,err
  },
`
	s = s[:start] + httpCode + s[start+end:]
	replace("Now:time.Now,", "Now:func()time.Time{mu.Lock();defer mu.Unlock();return virtualNow},")
	replace("Sleep:func(ctx context.Context,d time.Duration) error {select {case <-ctx.Done():return ctx.Err();case <-time.After(d):return nil}},", `Sleep:func(ctx context.Context,d time.Duration) error {if d<0{return fmt.Errorf("negative sleep")};select{case <-ctx.Done():return ctx.Err();default:};mu.Lock();sleeps=append(sleeps,int64(d));virtualNow=virtualNow.Add(d);mu.Unlock();return nil},`)
	replace("rc:=release.RunSupplyPreflight(ctx,request.Args,deps,&stdout,&stderr)", "rc:=release.RunSupplyPublisher(ctx,request.Args,deps,&stdout,&stderr)")
	replace(`"deadline_exceeded":ctx.Err()!=nil}`, `"deadline_exceeded":ctx.Err()!=nil,"sleep_nanoseconds":sleeps}`)
	return s
}

type rsPublisherFixture struct {
	f                          *rsCandidateFixture
	tree                       string
	manifest                   map[string]any
	remotes                    map[string]string
	published                  rsArchive
	wrongPackage, wrongPayload rsArchive
}

func rsPreparePublisher(t *testing.T) *rsPublisherFixture {
	f := rsPrepareCandidate(t)
	h := f.h
	root, revision, m := f.variant("publisher-ready", func(root string) {
		h.put(root, "first/value.go", "package first\nconst Value=2\n")
		h.put(root, "assets/payload.txt", "actual non-Go ready input\n")
	})
	published := rsVersionArchive(h, root, f.module, "v1.0.1", "publisher-expected")
	if out, rc := h.buildConsumer(published, f.module, "package main\nimport \""+f.module+"/first\"\nimport \""+f.module+"/second\"\nfunc main(){_ = first.Value+second.Value}\n", "publisher-full"); rc != 0 {
		t.Fatalf("HARNESS_NOT_EXECUTED: publisher ready ZIP does not build both imports: %s", out)
	}
	if published.revision != revision {
		t.Fatal("HARNESS_NOT_EXECUTED: publisher archive identity")
	}
	m["candidate_root"] = root
	delete(m, "candidate_root")
	tree := filepath.Join(h.root, "publisher-input")
	for _, raw := range m["input_files"].([]any) {
		entry := raw.(map[string]any)
		path := entry["path"].(string)
		b := mustRSRead(t, filepath.Join(root, path))
		if rsSHA(b) != entry["sha256"] {
			t.Fatal("HARNESS_NOT_EXECUTED: ready input drift")
		}
		h.put(tree, path, string(b))
		mode := os.FileMode(0644)
		if entry["mode"] == "100755" {
			mode = 0755
		}
		if err := os.Chmod(filepath.Join(tree, path), mode); err != nil {
			t.Fatal(err)
		}
	}
	consumers := []any{}
	products := []any{}
	remotes := map[string]string{}
	for _, product := range []string{"kacho", "kaname"} {
		repository := "PRO-Robotech/" + product
		repo := h.initRepo("publisher-"+product, map[string]string{"go.mod": "module github.com/" + repository + "\n\ngo 1.21\n\nrequire " + f.module + " v1.0.0\n", "main.go": "package main\nimport \"" + f.module + "/first\"\nimport \"" + f.module + "/second\"\nfunc main(){_ = first.Value+second.Value}\n"})
		h.must(repo, "git", "remote", "add", "origin", "https://github.com/"+repository+".git")
		sha := h.must(repo, "git", "rev-parse", "HEAD")
		origin := filepath.Join(h.root, "publisher-"+product+".git")
		h.must(h.root, "git", "clone", "--bare", "--no-local", repo, origin)
		remotes["https://github.com/"+repository+".git"] = "file://" + filepath.ToSlash(origin)
		contexts := []any{map[string]any{"goos": "linux", "goarch": "amd64", "cgo_enabled": false, "tags": []string{}}}
		consumers = append(consumers, map[string]any{"type": "repository", "repository": repository, "root": repo, "revision": sha, "module_roots": []string{"."}, "contexts": contexts})
		products = append(products, map[string]any{"repository": repository, "root": repo, "revision": sha, "module_roots": []string{"."}})
		_, stderr, rc := h.run(repo, []string{"GOPROXY=file://" + filepath.ToSlash(f.baseline.proxy), "GOMODCACHE=" + filepath.Join(h.root, "publisher-cache-"+product), "GOSUMDB=off", "GOPRIVATE=", "GONOPROXY=", "GONOSUMDB=", "GOFLAGS=-mod=mod", "CGO_ENABLED=0"}, h.goBin, "build", "-o", filepath.Join(h.root, product+"-control"), ".")
		if rc != 0 {
			t.Fatalf("HARNESS_NOT_EXECUTED: actual %s consumer control: %s", product, stderr)
		}
	}
	m["consumers"] = consumers
	m["product_trees"] = products
	m["internal_modules"] = []any{map[string]any{"module_path": f.module, "repository": "PRO-Robotech/corelib"}}
	m["budgets"] = map[string]any{"network_seconds": 7, "checks_seconds": 7}
	wrongArchives := map[string]rsArchive{}
	for _, axis := range []string{"package", "payload"} {
		clone := filepath.Join(h.root, "published-missing-"+axis)
		h.must(h.root, "git", "clone", "--quiet", "--no-local", root, clone)
		h.must(clone, "git", "config", "user.name", "CI-RS fixture")
		h.must(clone, "git", "config", "user.email", "ci-rs@invalid")
		path := "second/value.go"
		if axis == "payload" {
			path = "assets/payload.txt"
		}
		h.must(clone, "git", "rm", path)
		h.must(clone, "git", "commit", "--quiet", "-m", "single published omission "+axis)
		if delta := h.must(clone, "git", "diff", "--name-status", revision, "HEAD"); delta != "D\t"+path {
			t.Fatalf("HARNESS_NOT_EXECUTED: archive single-fact delta %s", delta)
		}
		bad := rsVersionArchive(h, clone, f.module, "v1.0.1", "published-missing-"+axis)
		wrongArchives[axis] = bad
		if axis == "package" {
			out, rc := h.buildConsumer(bad, f.module, "package main\nimport _ \""+f.module+"/second\"\nfunc main(){}\n", "published-missing-package")
			if rc == 0 || !strings.Contains(out, f.module+"/second") {
				t.Fatalf("HARNESS_NOT_EXECUTED: actual published package omission %s", out)
			}
		}
	}
	return &rsPublisherFixture{f: f, tree: tree, manifest: m, remotes: remotes, published: published, wrongPackage: wrongArchives["package"], wrongPayload: wrongArchives["payload"]}
}

// Transport birth controls are executed before capability discovery. The same
// script installs server hooks for later SUT cases, with actual push/readback.
func rsPublisherTransportControls(p *rsPublisherFixture) {
	h := p.f.h
	script := filepath.Join(moduleRoot(h.t), "scripts/release/publish-module-tree-inject.sh")
	client := h.initRepo("transport-client", map[string]string{"value.txt": "base\n"})
	base := h.must(client, "git", "rev-parse", "HEAD")
	origins := map[string]string{}
	for _, mode := range []string{"lawful", "reject", "lost-present", "lost-unavailable"} {
		origin := filepath.Join(h.root, "transport-"+mode+".git")
		h.must(h.root, "git", "clone", "--bare", "--no-local", client, origin)
		origins[mode] = origin
	}
	h.put(client, "value.txt", "candidate\n")
	h.must(client, "git", "add", "value.txt")
	h.must(client, "git", "commit", "--quiet", "-m", "actual candidate")
	candidate := h.must(client, "git", "rev-parse", "HEAD")
	ref := "refs/heads/release/module-" + rsSHA([]byte("publisher transport control"))
	ledger := []any{}
	for _, mode := range []string{"lawful", "reject", "lost-present", "lost-unavailable"} {
		origin := origins[mode]
		capture := filepath.Join(h.root, "transport-captures-"+mode)
		config := map[string]any{"root": h.root, "captures": capture, "repository": origin, "transport_mode": mode, "expected_ref": ref, "expected_sha": candidate}
		path := filepath.Join(h.root, "transport-"+mode+".json")
		h.put(h.root, filepath.Base(path), string(rsCanonicalJSON(h.t, config)))
		raw := h.must(h.root, "bash", script, "--fixture-transport", path)
		var result map[string]string
		if err := json.Unmarshal([]byte(raw), &result); err != nil {
			h.t.Fatal(err)
		}
		_, stderr, pushRC := h.run(client, nil, "git", "-c", "protocol.allow=never", "-c", "protocol.file.allow=always", "-c", "core.hooksPath=/dev/null", "push", "--porcelain", "--receive-pack="+result["wrapper"], "file://"+origin, candidate+":"+ref)
		actualOrigin := origin
		if mode == "lost-unavailable" {
			actualOrigin += ".hidden"
		}
		refs := h.must(h.root, "git", "--git-dir", actualOrigin, "show-ref")
		present := strings.Contains(refs, candidate+" "+ref)
		if (pushRC == 0) != (mode == "lawful") || present != (mode != "reject") {
			h.t.Fatalf("HARNESS_NOT_EXECUTED: actual transport %s rc%d refs%s stderr%s", mode, pushRC, refs, stderr)
		}
		read, _, readRC := h.run(h.root, nil, "git", "-c", "protocol.allow=never", "-c", "protocol.file.allow=always", "ls-remote", "--refs", "file://"+origin, ref)
		if mode == "lost-unavailable" {
			if readRC == 0 || len(read) != 0 {
				h.t.Fatal("HARNESS_NOT_EXECUTED: unavailable readback")
			}
		} else if readRC != 0 || strings.Contains(string(read), candidate) != present {
			h.t.Fatal("HARNESS_NOT_EXECUTED: exact transport readback")
		}
		entries, err := os.ReadDir(capture)
		if err != nil {
			h.t.Fatal(err)
		}
		for _, entry := range entries {
			b := mustRSRead(h.t, filepath.Join(capture, entry.Name()))
			h.save(mode+"-"+entry.Name(), b)
			if strings.HasSuffix(entry.Name(), "-hook.json") {
				var hook map[string]any
				if err := json.Unmarshal(b, &hook); err != nil {
					h.t.Fatal(err)
				}
				if hook["verified_owned_receive_pack"] != true {
					h.t.Fatal("HARNESS_NOT_EXECUTED: hook ownership not proven")
				}
				for _, key := range []string{"hook_pid", "receive_pack_pid"} {
					pid := int(hook[key].(float64))
					for i := 0; i < 100; i++ {
						if _, err := os.Stat(fmt.Sprintf("/proc/%d", pid)); os.IsNotExist(err) {
							break
						}
						time.Sleep(10 * time.Millisecond)
					}
					if _, err := os.Stat(fmt.Sprintf("/proc/%d", pid)); !os.IsNotExist(err) {
						h.t.Fatalf("HARNESS_NOT_EXECUTED: fixture child remains %d", pid)
					}
				}
			}
		}
		ledger = append(ledger, map[string]any{"case": mode, "push_exit": pushRC, "actual_ref_present": present, "readback_exit": readRC, "readback": string(read), "base": base, "candidate": candidate, "ref": ref, "classification": "HARNESS_PREREQUISITE_ONLY_NO_SUT"})
	}
	h.save("publisher-transport-controls.json", rsCanonicalJSON(h.t, ledger))
}

type rsPublisherCase struct {
	Name, Scenario, Phase, Fault, Outcome, Reason, Stage string
	Commit, DryRun                                       bool
	Effects                                              map[string]string
}

func rsPublisherCases() []rsPublisherCase {
	cases := []rsPublisherCase{}
	add := func(name, scenario, phase, fault, outcome, reason, stage string, commit, dry bool, effects map[string]string) {
		cases = append(cases, rsPublisherCase{name, scenario, phase, fault, outcome, reason, stage, commit, dry, effects})
	}
	for _, phase := range []string{"plan", "deliver", "release"} {
		add(phase+"-read-only", "CI-RS-01", phase, "lawful", "GREEN", "OK", "NONE", false, true, map[string]string{})
	}
	for _, fault := range []string{"commit-bare", "commit-wrong-repository", "commit-wrong-plan"} {
		reason := "INVALID_CONFIRMATION"
		if fault == "commit-bare" {
			reason = "INVALID_INVOCATION"
		}
		add(fault, "CI-RS-02", "deliver", fault, "USAGE_ERROR", reason, "NONE", true, false, map[string]string{})
	}
	add("receiving-repository-mismatch", "CI-RS-02", "deliver", "repo-mismatch", "RED", "REPOSITORY_MISMATCH", "NONE", true, false, map[string]string{})
	add("deliver-no-pr-route", "CI-RS-10", "deliver", "no-pr", "RED", "PR_REQUIRED", "NONE", true, false, map[string]string{})
	lawful := map[string]string{"branch": "PRESENT", "pr": "PRESENT", "merge": "PRESENT"}
	add("lawful-protected-delivery", "CI-RS-10/11/19", "deliver", "lawful", "GREEN", "OK", "MERGED_VERIFIED", true, false, lawful)
	add("required-check-failed", "CI-RS-11", "deliver", "checks-failed", "RED", "REQUIRED_CHECKS_FAILED", "PR_OPEN", true, false, map[string]string{"branch": "PRESENT", "pr": "PRESENT"})
	add("required-check-deadline", "CI-RS-12", "deliver", "checks-pending", "NOT_EXECUTED", "BUDGET_EXHAUSTED", "PR_OPEN", true, false, map[string]string{"branch": "PRESENT", "pr": "PRESENT"})
	add("required-check-recovers", "CI-RS-12", "deliver", "checks-last-sleep", "GREEN", "OK", "MERGED_VERIFIED", true, false, lawful)
	for _, fault := range []string{"read-unavailable", "read-recovers"} {
		outcome, reason := "NOT_EXECUTED", "SOURCE_UNAVAILABLE"
		if fault == "read-recovers" {
			outcome, reason = "GREEN", "OK"
		}
		add(fault, "CI-RS-12", "plan", fault, outcome, reason, "NONE", false, true, map[string]string{})
	}
	add("branch-existing-conflict", "CI-RS-19", "deliver", "branch-conflict", "RED", "EFFECT_IDENTITY_CONFLICT", "NONE", true, false, map[string]string{"branch": "CONFLICT"})
	add("branch-terminal-rejection", "CI-RS-19", "deliver", "branch-rejected", "RED", "REMOTE_WRITE_REJECTED", "NONE", true, false, map[string]string{"branch": "ABSENT"})
	add("branch-lost-readable", "CI-RS-12/19", "deliver", "branch-lost-present", "GREEN", "OK", "MERGED_VERIFIED", true, false, lawful)
	add("branch-lost-unavailable", "CI-RS-12/19", "deliver", "branch-lost-unavailable", "NOT_EXECUTED", "WRITE_OUTCOME_UNKNOWN", "NONE", true, false, map[string]string{"branch": "UNKNOWN"})
	add("branch-present-pr-rejected", "CI-RS-19", "deliver", "pr-rejected", "RED", "REMOTE_WRITE_REJECTED", "BRANCH_PRESENT", true, false, map[string]string{"branch": "PRESENT", "pr": "ABSENT"})
	for _, fault := range []string{"pr-lost-present", "pr-lost-unavailable", "pr-lost-empty", "pr-duplicate"} {
		outcome, reason, stage, effects := "GREEN", "OK", "MERGED_VERIFIED", lawful
		if fault != "pr-lost-present" {
			outcome, reason, stage = "NOT_EXECUTED", "WRITE_OUTCOME_UNKNOWN", "BRANCH_PRESENT"
			effects = map[string]string{"branch": "PRESENT", "pr": "UNKNOWN"}
		}
		if fault == "pr-duplicate" {
			outcome, reason = "RED", "EFFECT_IDENTITY_CONFLICT"
			effects = map[string]string{"branch": "PRESENT", "pr": "CONFLICT"}
		}
		add(fault, "CI-RS-12/19", "deliver", fault, outcome, reason, stage, true, false, effects)
	}
	add("merge-head-conflict", "CI-RS-11/19/22", "deliver", "merge-head-conflict", "RED", "PR_HEAD_CHANGED", "PR_OPEN", true, false, map[string]string{"branch": "PRESENT", "pr": "CONFLICT", "merge": "CONFLICT"})
	add("merge-rejected", "CI-RS-19", "deliver", "merge-rejected", "RED", "REMOTE_WRITE_REJECTED", "PR_OPEN", true, false, map[string]string{"branch": "PRESENT", "pr": "PRESENT", "merge": "ABSENT"})
	add("merge-lost-readable", "CI-RS-12/19", "deliver", "merge-lost-present", "GREEN", "OK", "MERGED_VERIFIED", true, false, lawful)
	add("merge-lost-unavailable", "CI-RS-12/19", "deliver", "merge-lost-unavailable", "NOT_EXECUTED", "WRITE_OUTCOME_UNKNOWN", "PR_OPEN", true, false, map[string]string{"branch": "PRESENT", "pr": "PRESENT", "merge": "UNKNOWN"})
	add("consumer-subject-before-admission", "CI-RS-22", "deliver", "consumer-drift", "USAGE_ERROR", "INVALID_CONFIRMATION", "NONE", true, false, map[string]string{})
	for _, fault := range []string{"input-drift-after-pr", "base-drift-after-pr", "consumer-drift-after-pr", "baseline-drift-after-pr"} {
		add(fault, "CI-RS-22", "deliver", fault, "RED", "INPUT_CHANGED", "PR_OPEN", true, false, map[string]string{"branch": "PRESENT", "pr": "PRESENT"})
	}
	add("pr-head-changed", "CI-RS-22", "deliver", "pr-head-changed", "RED", "PR_HEAD_CHANGED", "PR_OPEN", true, false, map[string]string{"branch": "PRESENT", "pr": "CONFLICT"})
	for _, fault := range []string{"merge-content-mismatch", "merge-ancestry-lost"} {
		reason := "CONTENT_MISMATCH"
		if fault == "merge-ancestry-lost" {
			reason = "TARGET_NOT_ON_MAIN"
		}
		add(fault, "CI-RS-11/22", "deliver", fault, "RED", reason, "MERGE_PRESENT", true, false, lawful)
	}
	releaseEffects := map[string]string{"branch": "PRESENT", "pr": "PRESENT", "merge": "PRESENT", "tag": "PRESENT", "release-note": "PRESENT"}
	for _, fault := range []string{"lawful", "tag-lost-present", "tag-lost-unavailable", "tag-conflict", "note-lost-present", "note-lost-unavailable", "note-conflict", "target-branch-only", "target-ancestry-lost", "target-main-advanced", "post-tag-ancestry-lost"} {
		outcome, reason, stage := "GREEN", "OK", "TAG_PRESENT"
		effects := map[string]string{}
		for k, v := range releaseEffects {
			effects[k] = v
		}
		switch fault {
		case "tag-lost-unavailable":
			outcome, reason, stage = "NOT_EXECUTED", "WRITE_OUTCOME_UNKNOWN", "MERGED_VERIFIED"
			effects["tag"] = "UNKNOWN"
			delete(effects, "release-note")
		case "tag-conflict":
			outcome, reason, stage = "RED", "TAG_IDENTITY_CONFLICT", "MERGED_VERIFIED"
			effects["tag"] = "CONFLICT"
			delete(effects, "release-note")
		case "note-lost-unavailable":
			outcome, reason = "NOT_EXECUTED", "WRITE_OUTCOME_UNKNOWN"
			effects["release-note"] = "UNKNOWN"
		case "note-conflict":
			outcome, reason = "RED", "EFFECT_IDENTITY_CONFLICT"
			effects["release-note"] = "CONFLICT"
		case "target-branch-only", "target-ancestry-lost":
			outcome, reason, stage = "RED", "TARGET_NOT_ON_MAIN", "MERGE_PRESENT"
			delete(effects, "tag")
			delete(effects, "release-note")
		case "post-tag-ancestry-lost":
			outcome, reason = "RED", "TARGET_NOT_ON_MAIN"
			delete(effects, "release-note")
		}
		add("release-"+fault, "CI-RS-11/12/19/22", "release", fault, outcome, reason, stage, true, false, effects)
	}
	for _, fault := range []string{"lawful", "proxy-unavailable", "published-archive-mismatch", "published-payload-mismatch"} {
		outcome, reason, stage := "GREEN", "OK", "ARCHIVE_VERIFIED"
		if fault == "proxy-unavailable" {
			outcome, reason, stage = "NOT_EXECUTED", "PROXY_UNAVAILABLE", "TAG_PRESENT"
		}
		if fault == "published-archive-mismatch" || fault == "published-payload-mismatch" {
			outcome, reason, stage = "RED", "PUBLISHED_ARCHIVE_MISMATCH", "TAG_PRESENT"
		}
		add("probe-"+fault, "CI-RS-13/22", "probe", fault, outcome, reason, stage, false, false, map[string]string{})
	}
	return cases
}

type rsForgeProcess struct {
	cmd                                                    *exec.Cmd
	endpoint, control, state, capture, repository, wrapper string
	stderr                                                 bytes.Buffer
	stopped                                                bool
}

func rsStartForge(h *rsHarness, p *rsPublisherFixture, label string) *rsForgeProcess {
	h.t.Helper()
	dir := filepath.Join(h.root, "forge-"+label)
	if err := os.MkdirAll(dir, 0755); err != nil {
		h.t.Fatal(err)
	}
	origin := filepath.Join(dir, "receiving.git")
	h.must(h.root, "git", "clone", "--bare", "--no-local", p.f.origin, origin)
	control := filepath.Join(dir, "control.json")
	h.put(dir, "control.json", `{"fault":"lawful"}`)
	capture := filepath.Join(dir, "captures")
	state := filepath.Join(dir, "state.json")
	escaped, err := module.EscapePath(p.f.module)
	if err != nil {
		h.t.Fatal(err)
	}
	published := map[string]string{}
	for _, suffix := range []string{"v1.0.1.info", "v1.0.1.mod", "v1.0.1.zip"} {
		published[suffix] = filepath.Join(p.published.proxy, escaped, "@v", suffix)
	}
	config := map[string]any{"root": h.root, "captures": capture, "repository": origin, "control": control, "state": state, "http": p.f.http, "published": published, "wrong_zip": p.wrongPackage.path, "wrong_payload_zip": p.wrongPayload.path, "base": p.f.base}
	path := filepath.Join(dir, "config.json")
	h.put(dir, "config.json", string(rsCanonicalJSON(h.t, config)))
	script := filepath.Join(moduleRoot(h.t), "scripts/release/publish-module-tree-inject.sh")
	ctx, cancel := context.WithCancel(context.Background())
	c := exec.CommandContext(ctx, "bash", script, "--fixture-forge", path)
	c.Dir = h.root
	c.Env = rsEnvironment()
	proc := &rsForgeProcess{cmd: c, control: control, state: state, capture: capture, repository: origin}
	c.Stderr = &proc.stderr
	stdout, e := c.StdoutPipe()
	if e != nil {
		h.t.Fatal(e)
	}
	if e = c.Start(); e != nil {
		h.t.Fatal(e)
	}
	lines := make(chan []byte, 1)
	go func() { line, _ := bufio.NewReader(stdout).ReadBytes('\n'); lines <- line }()
	select {
	case line := <-lines:
		var handshake struct {
			Endpoint string `json:"endpoint"`
			PID      int    `json:"pid"`
		}
		if e = json.Unmarshal(line, &handshake); e != nil || handshake.PID != c.Process.Pid {
			cancel()
			_ = c.Wait()
			h.t.Fatalf("HARNESS_NOT_EXECUTED: forge handshake %s %v", line, e)
		}
		proc.endpoint = handshake.Endpoint
		h.save(label+"-forge-handshake.json", line)
	case <-time.After(10 * time.Second):
		cancel()
		_ = c.Wait()
		h.t.Fatal("HARNESS_NOT_EXECUTED: forge startup deadline")
	}
	h.t.Cleanup(func() {
		if !proc.stopped {
			cancel()
			waitErr := c.Wait()
			proc.stopped = true
			h.save(label+"-forge-process.json", rsCanonicalJSON(h.t, map[string]any{"pid": c.Process.Pid, "wait_completed": true, "exit_code": c.ProcessState.ExitCode(), "cleanup_cancellation": true, "wait_result": fmt.Sprint(waitErr)}))
		}
		h.save(label+"-forge.stderr", proc.stderr.Bytes())
		rsCopyCaptures(h, proc.capture, label+"-forge")
		transportCapture := filepath.Join(filepath.Dir(proc.repository), "transport-captures")
		rsVerifyHookChildren(h, transportCapture)
		rsCopyCaptures(h, transportCapture, label+"-transport")
		if b, e := os.ReadFile(proc.state); e == nil {
			h.save(label+"-forge-state.json", b)
		}
	})
	proc.wrapper = rsInstallPublisherTransport(h, origin, "lawful", "", "")
	return proc
}
func rsCopyCaptures(h *rsHarness, root, label string) {
	h.t.Helper()
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if h.capture != "" {
			if e := os.MkdirAll(filepath.Join(h.capture, label, filepath.Dir(relative)), 0755); e != nil {
				return e
			}
		}
		h.save(label+"/"+relative, mustRSRead(h.t, path))
		return nil
	})
	if err != nil {
		h.t.Fatalf("HARNESS_NOT_EXECUTED: fixture capture copy: %v", err)
	}
}
func rsInstallPublisherTransport(h *rsHarness, origin, mode, ref, sha string) string {
	h.t.Helper()
	cfg := map[string]any{"root": h.root, "captures": filepath.Join(filepath.Dir(origin), "transport-captures"), "repository": origin, "transport_mode": mode}
	if mode == "post-tag-reset" {
		cfg["base"] = h.must(h.root, "git", "--git-dir", origin, "rev-parse", "refs/tags/v1.0.0^{}")
	}
	if ref != "" {
		cfg["expected_ref"] = ref
	}
	if sha != "" {
		cfg["expected_sha"] = sha
	}
	path := filepath.Join(filepath.Dir(origin), "transport-config.json")
	h.put(filepath.Dir(origin), "transport-config.json", string(rsCanonicalJSON(h.t, cfg)))
	raw := h.must(h.root, "bash", filepath.Join(moduleRoot(h.t), "scripts/release/publish-module-tree-inject.sh"), "--fixture-transport", path)
	var parsed map[string]string
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		h.t.Fatal(err)
	}
	return parsed["wrapper"]
}
func (f *rsForgeProcess) fault(t *testing.T, fault string) {
	t.Helper()
	if e := os.WriteFile(f.control, rsCanonicalJSON(t, map[string]any{"fault": fault}), 0600); e != nil {
		t.Fatal(e)
	}
}
func rsForgeCall(h *rsHarness, f *rsForgeProcess, method, path string, body any) (int, []byte, error) {
	raw := []byte{}
	if body != nil {
		raw = rsCanonicalJSON(h.t, body)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, e := http.NewRequestWithContext(ctx, method, f.endpoint+"/fixture", bytes.NewReader(raw))
	if e != nil {
		h.t.Fatal(e)
	}
	req.Header.Set("X-CI-RS-Original-URL", "https://api.github.com/repos/PRO-Robotech/corelib"+path)
	client := &http.Client{Transport: &http.Transport{Proxy: nil}}
	defer client.CloseIdleConnections()
	r, e := client.Do(req)
	if e != nil {
		return 0, nil, e
	}
	defer r.Body.Close()
	b, e := io.ReadAll(r.Body)
	return r.StatusCode, b, e
}
func rsForgeBirth(h *rsHarness, p *rsPublisherFixture) {
	f := rsStartForge(h, p, "birth")
	plan := rsSHA([]byte("actual forge birth control"))
	branch := "release/module-" + plan
	// The client repository contains the exact Go-ZIP candidate object.
	client := filepath.Join(h.root, "candidate-publisher-ready")
	h.must(client, "git", "-c", "protocol.allow=never", "-c", "protocol.file.allow=always", "-c", "core.hooksPath=/dev/null", "push", "--receive-pack="+f.wrapper, "file://"+f.repository, p.published.revision+":refs/heads/"+branch)
	status, _, err := rsForgeCall(h, f, "POST", "/pulls", map[string]any{"head": branch, "base": "main", "body": "CI-RS-1 plan-sha256:" + plan, "title": "fixture"})
	if err != nil || status != 201 {
		h.t.Fatalf("HARNESS_NOT_EXECUTED: forge PR birth %d %v", status, err)
	}
	status, _, err = rsForgeCall(h, f, "PUT", "/pulls/101/merge", map[string]any{"sha": p.published.revision})
	if err != nil || status != 200 {
		h.t.Fatalf("HARNESS_NOT_EXECUTED: forge actual merge %d %v", status, err)
	}
	main := h.must(h.root, "git", "--git-dir", f.repository, "rev-parse", "refs/heads/main")
	if main == p.published.revision || main == p.f.base {
		h.t.Fatal("HARNESS_NOT_EXECUTED: forge did not create actual merge commit")
	}
	h.must(h.root, "git", "--git-dir", f.repository, "merge-base", "--is-ancestor", p.published.revision, main)
	if got := h.must(h.root, "git", "--git-dir", f.repository, "rev-parse", main+"^{tree}"); got != h.must(client, "git", "rev-parse", p.published.revision+"^{tree}") {
		h.t.Fatal("HARNESS_NOT_EXECUTED: forge merge changed candidate content")
	}
	h.must(h.root, "git", "--git-dir", f.repository, "update-ref", "refs/tags/v1.0.1", main, strings.Repeat("0", 40))
	status, _, err = rsForgeCall(h, f, "POST", "/releases", map[string]any{"tag_name": "v1.0.1", "target_commitish": main, "body": "fixture note"})
	if err != nil || status != 201 {
		h.t.Fatal("HARNESS_NOT_EXECUTED: forge note birth")
	}
	tag := h.must(h.root, "git", "--git-dir", f.repository, "rev-parse", "refs/tags/v1.0.1^{}")
	if tag != main {
		h.t.Fatal("HARNESS_NOT_EXECUTED: actual peeled tag control")
	}
	h.save("publisher-forge-birth.json", rsCanonicalJSON(h.t, map[string]any{"classification": "HARNESS_PREREQUISITE_ONLY_NO_SUT", "branch": branch, "candidate": p.published.revision, "base": p.f.base, "actual_merge": main, "peeled_tag": tag, "archive_sha256": rsSHA(mustRSRead(h.t, p.published.path))}))
}

func rsBuildPublisherBridge(h *rsHarness) string {
	_ = rsBuildConsumerBridge(h)
	clone := filepath.Join(h.root, "sut-copy")
	h.put(clone, "internal/release/ci_rs_bridge_test.go", rsPublisherBridgeSource(h.t))
	binary := filepath.Join(h.root, "publisher-bridge.test")
	h.must(clone, h.goBin, "test", "-c", "-o", binary, "./internal/release")
	return binary
}

type rsPublisherObservation struct {
	result    map[string]any
	meta      map[string]any
	directory string
	commands  []map[string]any
}

func rsInvokePublisher(h *rsHarness, p *rsPublisherFixture, f *rsForgeProcess, binary, label string, args []string) rsPublisherObservation {
	h.t.Helper()
	dir := filepath.Join(h.root, "publisher-result-"+label)
	if err := os.MkdirAll(dir, 0755); err != nil {
		h.t.Fatal(err)
	}
	remotes := map[string]string{}
	for k, v := range p.remotes {
		remotes[k] = v
	}
	remotes["https://github.com/PRO-Robotech/corelib.git"] = "file://" + f.repository
	req := map[string]any{"args": args, "output": dir, "remotes": remotes, "endpoint": f.endpoint, "receive_pack": f.wrapper}
	path := filepath.Join(h.root, "publisher-request-"+label+".json")
	h.put(h.root, filepath.Base(path), string(rsCanonicalJSON(h.t, req)))
	_, stderr, rc := h.run(h.root, []string{"CI_RS_BRIDGE_REQUEST=" + path, "GOPROXY=off", "GOSUMDB=off"}, binary, "-test.run=^TestCIRSConsumerBridge$", "-test.v", "-test.timeout=115s")
	if rc != 0 {
		h.t.Fatalf("HARNESS_NOT_EXECUTED: publisher bridge rc%d %s", rc, stderr)
	}
	var meta map[string]any
	if err := json.Unmarshal(mustRSRead(h.t, filepath.Join(dir, "bridge-result.json")), &meta); err != nil {
		h.t.Fatal(err)
	}
	rsCopyCaptures(h, dir, label)
	if meta["deadline_exceeded"] != false || len(meta["unhandled_boundaries"].([]any)) != 0 {
		h.t.Fatalf("HARNESS_NOT_EXECUTED: publisher bridge %+v", meta)
	}
	var state map[string]any
	if err := json.Unmarshal(mustRSRead(h.t, f.state), &state); err != nil {
		h.t.Fatal(err)
	}
	if len(state["unknown"].([]any)) != 0 {
		h.t.Fatalf("HARNESS_NOT_EXECUTED: undeclared fixture routes %+v", state["unknown"])
	}
	result := rsValidateResult(h, mustRSRead(h.t, filepath.Join(dir, "sut.stdout")), label)
	commands := []map[string]any{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		h.t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "command-") && strings.HasSuffix(entry.Name(), ".json") {
			var cmd map[string]any
			if err := json.Unmarshal(mustRSRead(h.t, filepath.Join(dir, entry.Name())), &cmd); err != nil {
				h.t.Fatal(err)
			}
			commands = append(commands, cmd)
		}
	}
	return rsPublisherObservation{result, meta, dir, commands}
}

func rsCheckPublisher(h *rsHarness, c rsPublisherCase, o rsPublisherObservation, plan, candidate string) {
	t := h.t
	t.Helper()
	r := o.result
	rc := map[string]int{"GREEN": 0, "RED": 1, "USAGE_ERROR": 2, "NOT_EXECUTED": 3}[c.Outcome]
	if r["outcome"] != c.Outcome || r["reason"] != c.Reason || r["exit_code"] != float64(rc) || o.meta["exit_code"] != float64(rc) {
		t.Errorf("SEMANTIC_MISMATCH: %s want %s/%s/%d actual %+v meta%+v", c.Name, c.Outcome, c.Reason, rc, r, o.meta)
	}
	if r["phase"] != c.Phase || (c.Outcome != "USAGE_ERROR" && r["dry_run"] != c.DryRun) || r["stage"] != c.Stage {
		t.Errorf("SEMANTIC_MISMATCH: phase/dry/stage want %s/%t/%s actual %v/%v/%v", c.Phase, c.DryRun, c.Stage, r["phase"], r["dry_run"], r["stage"])
	}
	effects := r["effects"].([]any)
	seen := map[string]bool{}
	for _, raw := range effects {
		e := raw.(map[string]any)
		kind := e["kind"].(string)
		if seen[kind] || c.Effects[kind] != e["state"] || e["repository"] != "PRO-Robotech/corelib" {
			t.Errorf("SEMANTIC_MISMATCH: typed effect %+v expected%v", e, c.Effects)
		}
		seen[kind] = true
		if kind == "branch" && (e["ref"] != "refs/heads/release/module-"+plan || e["expected_sha"] != candidate) {
			t.Errorf("SEMANTIC_MISMATCH: exact branch identity %+v", e)
		}
		if kind == "pr" && (e["head_ref"] != "refs/heads/release/module-"+plan || e["expected_head_sha"] != candidate || e["plan_marker"] != "CI-RS-1 plan-sha256:"+plan) {
			t.Errorf("SEMANTIC_MISMATCH: exact PR identity %+v", e)
		}
		if e["state"] == "PRESENT" && (kind == "branch" || kind == "tag" || kind == "release-note") && e["sha"] != e["expected_sha"] {
			t.Errorf("SEMANTIC_MISMATCH: PRESENT identity %+v", e)
		}
		if (e["state"] == "UNKNOWN" || e["state"] == "ABSENT") && kind != "merge" && e["sha"] != nil {
			t.Errorf("SEMANTIC_MISMATCH: uncertain effect manufactures observed SHA %+v", e)
		}
	}
	if len(seen) != len(c.Effects) {
		t.Errorf("SEMANTIC_MISMATCH: effect set %v want%v", seen, c.Effects)
	}
	predicates := map[string]bool{}
	for _, raw := range r["checks"].([]any) {
		check := raw.(map[string]any)
		name := check["predicate"].(string)
		if predicates[name] {
			t.Errorf("SEMANTIC_MISMATCH: duplicate predicate %s", name)
		}
		predicates[name] = true
		if c.Outcome == "GREEN" && check["outcome"] != "GREEN" {
			t.Errorf("SEMANTIC_MISMATCH: GREEN contains failed predicate %+v", check)
		}
	}
	if c.Outcome == "GREEN" {
		want := []string{"invocation", "identity", "input", "ownership", "baseline", "package-floor", "consumer-census", "consumer-archive", "payload", "compatibility", "pins-origin", "pr"}
		if !c.DryRun && c.Phase != "plan" {
			want = append(want, "required-checks", "main-ancestry")
		}
		if !c.DryRun && (c.Phase == "release" || c.Phase == "probe") {
			want = append(want, "tag")
		}
		if c.Phase == "probe" {
			want = append(want, "proxy")
		}
		if len(predicates) != len(want) {
			t.Errorf("SEMANTIC_MISMATCH: mandatory predicate cardinality got%v want%v", predicates, want)
		}
		for _, name := range want {
			if !predicates[name] {
				t.Errorf("SEMANTIC_MISMATCH: missing phase predicate %s", name)
			}
		}
		for _, key := range []string{"input_files", "previous_packages", "candidate_packages", "consumers", "consumer_imports", "modules", "internal_requirements"} {
			n, ok := r["census"].(map[string]any)[key].(float64)
			if !ok || n <= 0 {
				t.Errorf("SEMANTIC_MISMATCH: nonempty publisher census %s=%v", key, r["census"].(map[string]any)[key])
			}
		}
	}
	pushes := 0
	for _, cmd := range o.commands {
		if filepath.Base(cmd["program"].(string)) != "git" {
			continue
		}
		args := []string{}
		for _, a := range cmd["args"].([]any) {
			args = append(args, a.(string))
		}
		verb := rsPublisherVerb(t, args)
		if verb != "push" {
			continue
		}
		pushes++
		for _, arg := range args {
			if arg == "--force" || arg == "-f" || strings.HasPrefix(arg, "--force-with-lease") || strings.HasPrefix(arg, "+") || strings.Contains(arg, ":refs/heads/main") || arg == "--delete" {
				t.Errorf("SEMANTIC_MISMATCH: forbidden push flag/ref %q", args)
			}
		}
	}
	if (c.DryRun || c.Phase == "probe" || len(c.Effects) == 0) && pushes != 0 {
		t.Errorf("SEMANTIC_MISMATCH: forbidden pushes %d", pushes)
	}
	if pushes > 1 {
		t.Errorf("SEMANTIC_MISMATCH: write automatically retried %d pushes", pushes)
	}
}
func rsPublisherVerb(t *testing.T, args []string) string {
	// The assertions need only identify actual command position; preserve the
	// same option-value rule as the adapter, including -C push and -c push.
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			return a
		}
		switch a {
		case "-C", "-c", "--git-dir", "--work-tree", "--namespace", "--super-prefix", "--config-env", "--attr-source":
			i++
			if i >= len(args) {
				return ""
			}
		default:
			if strings.Contains(a, "=") || a == "--bare" || a == "--no-pager" || a == "-P" {
				continue
			}
			return ""
		}
	}
	return ""
}

func TestReleaseSupplyPublisherProtocol(t *testing.T) {
	p := rsPreparePublisher(t)
	h := p.f.h
	rsPublisherParserControls(h)
	rsPublisherTransportControls(p)
	rsForgeBirth(h, p)
	rsForgeFaultControls(h, p)
	rsForgeMutationControls(h, p)
	rsTagTransportControls(h, p)
	cases := rsPublisherCases()
	rsValidatePublisherCoverage(h, cases)
	ledger := []any{}
	for _, c := range cases {
		ledger = append(ledger, map[string]any{"name": c.Name, "scenario": c.Scenario, "phase": c.Phase, "changed_field": "fixture.fault", "lawful_twin": "lawful-protected-delivery", "fault": c.Fault, "expected_outcome": c.Outcome, "expected_reason": c.Reason, "expected_stage": c.Stage, "expected_effects": c.Effects, "sut_invocations": 0})
	}
	h.save("publisher-case-ledger.json", rsCanonicalJSON(t, ledger))
	h.save("publisher-base-manifest.json", rsCanonicalJSON(t, p.manifest))
	if _, err := parser.ParseFile(token.NewFileSet(), "ci_rs_bridge_test.go", rsPublisherBridgeSource(t), parser.AllErrors); err != nil {
		t.Fatalf("HARNESS_NOT_EXECUTED: publisher bridge syntax %v", err)
	}
	missing := rsSupplySymbols(h, []string{"RunSupplyPublisher", "SupplyDependencies", "SupplyCommand", "SupplyCommandResult"})
	if len(missing) > 0 {
		h.save("publisher-capability.json", rsCanonicalJSON(t, map[string]any{"holder_outcome": "CAPABILITY_ABSENT", "missing_symbols": missing, "prepared_cases": len(cases), "sut_invocations": 0, "sut_semantic_decisions": 0, "fixture_prerequisites": "actual Git/Go/archive/receiving hooks/forge merge completed; no product verdict"}))
		t.Fatalf("CAPABILITY_ABSENT: publisher symbols %v; prepared=%d; sut_invocations=0", missing, len(cases))
	}
	binary := rsBuildPublisherBridge(h)
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			ch := *h
			ch.t = t
			ch.serial = 0
			if h.capture != "" {
				ch.capture = filepath.Join(h.capture, c.Name)
				if e := os.MkdirAll(ch.capture, 0755); e != nil {
					t.Fatal(e)
				}
			}
			f := rsStartForge(&ch, p, c.Name)
			manifest := rsCloneJSON(p.manifest)
			manifestPath := filepath.Join(h.root, c.Name+"-publisher-manifest.json")
			ch.put(h.root, filepath.Base(manifestPath), string(rsCanonicalJSON(t, manifest)))
			tree := filepath.Join(h.root, "ready-"+c.Name)
			if err := filepath.WalkDir(p.tree, func(path string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				rel, e := filepath.Rel(p.tree, path)
				if e != nil {
					return e
				}
				if d.IsDir() {
					return os.MkdirAll(filepath.Join(tree, rel), 0755)
				}
				info, e := d.Info()
				if e != nil {
					return e
				}
				return os.WriteFile(filepath.Join(tree, rel), mustRSRead(t, path), info.Mode().Perm())
			}); err != nil {
				t.Fatal(err)
			}
			args := func(phase string) []string {
				return []string{"--phase", phase, "--tree", tree, "--repo", "PRO-Robotech/corelib", "--version", "v1.0.1", "--manifest", manifestPath, "--via-pull-request", "--network-budget", "7", "--checks-budget", "7"}
			}
			planArgs := args("plan")
			planning := rsInvokePublisher(&ch, p, f, binary, c.Name+"-plan", planArgs)
			if planning.result["outcome"] != "GREEN" {
				t.Fatalf("SEMANTIC_MISMATCH: same-fixture lawful plan failed before %s: %+v", c.Name, planning.result)
			}
			plan, ok := planning.result["plan_sha256"].(string)
			if !ok || len(plan) != 64 {
				t.Fatal("SEMANTIC_MISMATCH: plan lacks exact key")
			}
			candidate, ok := planning.result["candidate_sha"].(string)
			if !ok || len(candidate) != 40 {
				t.Fatal("SEMANTIC_MISMATCH: plan lacks candidate SHA")
			}
			rsCheckPublisher(&ch, rsPublisherCase{Name: "prerequisite-plan", Phase: "plan", Outcome: "GREEN", Reason: "OK", Stage: "NONE", DryRun: true, Effects: map[string]string{}}, planning, plan, candidate)
			if t.Failed() {
				return
			}
			if (c.Phase == "release" && c.Commit) || c.Phase == "probe" {
				rsRunReleaseCase(&ch, p, f, binary, c, args, plan, candidate)
				return
			}
			invocation := args(c.Phase)
			if c.Phase == "release" {
				invocation = append(invocation, "--landed-sha", p.f.base)
			}
			if c.Commit {
				invocation = append(invocation, "--commit", "PRO-Robotech/corelib", "--plan-sha256", plan)
			}
			switch c.Fault {
			case "commit-bare":
				invocation = append(args(c.Phase), "--commit")
			case "commit-wrong-repository":
				invocation = append(args(c.Phase), "--commit", "corelib", "--plan-sha256", plan)
			case "commit-wrong-plan":
				invocation = append(args(c.Phase), "--commit", "PRO-Robotech/corelib", "--plan-sha256", strings.Repeat("0", 64))
			case "repo-mismatch":
				for i, arg := range invocation {
					if arg == "--repo" {
						invocation[i+1] = "PRO-Robotech/kacho"
					}
				}
			case "no-pr":
				for i, arg := range invocation {
					if arg == "--via-pull-request" {
						invocation = append(invocation[:i:i], invocation[i+1:]...)
						break
					}
				}
			case "input-drift":
				ch.put(tree, "first/value.go", "package first\nconst Value=3\n")
			case "base-drift":
				old := ch.must(h.root, "git", "--git-dir", f.repository, "rev-parse", "refs/heads/main")
				treeSHA := ch.must(h.root, "git", "--git-dir", f.repository, "rev-parse", old+"^{tree}")
				next := rsFixtureCommit(&ch, f.repository, treeSHA, old, "base drift")
				ch.must(h.root, "git", "--git-dir", f.repository, "update-ref", "refs/heads/main", next, old)
			case "consumer-drift":
				consumers := manifest["consumers"].([]any)
				consumers[0].(map[string]any)["contexts"].([]any)[0].(map[string]any)["tags"] = []string{"changed"}
				ch.put(h.root, filepath.Base(manifestPath), string(rsCanonicalJSON(t, manifest)))
			case "baseline-drift":
				ch.must(h.root, "git", "--git-dir", f.repository, "update-ref", "refs/tags/v1.0.0", strings.Repeat("0", 40))
			case "branch-conflict":
				ch.must(h.root, "git", "--git-dir", f.repository, "update-ref", "refs/heads/release/module-"+plan, p.f.base, strings.Repeat("0", 40))
			case "branch-rejected":
				f.wrapper = rsInstallPublisherTransport(&ch, f.repository, "reject", "refs/heads/release/module-"+plan, candidate)
			case "branch-lost-present", "branch-lost-unavailable":
				f.wrapper = rsInstallPublisherTransport(&ch, f.repository, strings.TrimPrefix(c.Fault, "branch-"), "refs/heads/release/module-"+plan, candidate)
			}
			f.fault(t, c.Fault)
			if strings.HasSuffix(c.Fault, "-after-pr") {
				mutation := map[string]any{"trigger": "after-pr-create", "accepted_plan": plan}
				switch c.Fault {
				case "input-drift-after-pr":
					path := filepath.Join(tree, "first/value.go")
					mutation["kind"] = "file"
					mutation["path"] = path
					mutation["before_sha256"] = rsSHA(mustRSRead(t, path))
					mutation["after"] = "package first\nconst Value=3\n"
				case "consumer-drift-after-pr":
					mutation["kind"] = "file"
					mutation["path"] = manifestPath
					mutation["before_sha256"] = rsSHA(mustRSRead(t, manifestPath))
					changed := rsCloneJSON(manifest)
					changed["consumers"].([]any)[0].(map[string]any)["contexts"].([]any)[0].(map[string]any)["tags"] = []string{"changed"}
					mutation["after"] = string(rsCanonicalJSON(t, changed))
				case "base-drift-after-pr", "baseline-drift-after-pr":
					name := "refs/heads/main"
					if c.Fault == "baseline-drift-after-pr" {
						name = "refs/tags/v1.0.0"
					}
					mutation["kind"] = "ref"
					mutation["ref"] = name
					mutation["before_sha"] = p.f.base
					mutation["operation"] = "advance"
				}
				if err := os.WriteFile(f.control, rsCanonicalJSON(t, map[string]any{"fault": c.Fault, "mutation": mutation}), 0600); err != nil {
					t.Fatal(err)
				}
			}
			observed := rsInvokePublisher(&ch, p, f, binary, c.Name+"-subject", invocation)
			rsCheckPublisher(&ch, c, observed, plan, candidate)
			rsCheckActualEffects(&ch, f, c, observed)
			var state map[string]any
			if err := json.Unmarshal(mustRSRead(t, f.state), &state); err != nil {
				t.Fatal(err)
			}
			writeCounts := map[string]int{}
			for _, raw := range state["writes"].([]any) {
				kind := raw.(map[string]any)["kind"].(string)
				writeCounts[kind]++
				if _, ok := c.Effects[kind]; !ok {
					t.Errorf("SEMANTIC_MISMATCH: forbidden dependent %s write", kind)
				}
			}
			for kind, n := range writeCounts {
				if n > 1 {
					t.Errorf("SEMANTIC_MISMATCH: duplicated %s write %d", kind, n)
				}
			}
			if c.Fault == "read-recovers" || c.Fault == "read-unavailable" {
				sleeps := observed.meta["sleep_nanoseconds"].([]any)
				if len(sleeps) != 2 || sleeps[0] != float64(time.Second) || sleeps[1] != float64(2*time.Second) {
					t.Errorf("SEMANTIC_MISMATCH: bounded 1/2 second retry schedule %v", sleeps)
				}
			}
			if c.Fault == "checks-pending" {
				sleeps := observed.meta["sleep_nanoseconds"].([]any)
				if len(sleeps) != 2 || sleeps[0] != float64(5*time.Second) || sleeps[1] != float64(2*time.Second) {
					t.Errorf("SEMANTIC_MISMATCH: clipped checks 5/2 seconds %v", sleeps)
				}
			}
			rsPublisherRecovery(&ch, p, f, binary, c, invocation, plan, candidate)
			// Repeated exact delivery may read the objects but never write again.
			if c.Name == "lawful-protected-delivery" && !t.Failed() {
				before := len(state["writes"].([]any))
				resume := rsInvokePublisher(&ch, p, f, binary, c.Name+"-resume", invocation)
				rsCheckPublisher(&ch, c, resume, plan, candidate)
				if err := json.Unmarshal(mustRSRead(t, f.state), &state); err != nil {
					t.Fatal(err)
				}
				if len(state["writes"].([]any)) != before {
					t.Error("SEMANTIC_MISMATCH: resume repeated forge writes")
				}
				for _, cmd := range resume.commands {
					a := []string{}
					for _, v := range cmd["args"].([]any) {
						a = append(a, v.(string))
					}
					if filepath.Base(cmd["program"].(string)) == "git" && rsPublisherVerb(t, a) == "push" {
						t.Error("SEMANTIC_MISMATCH: resume repeated Git push")
					}
				}
			}
		})
	}
}
func rsFixtureCommit(h *rsHarness, repo, tree, parent, message string) string {
	out, stderr, rc := h.run(h.root, []string{"GIT_AUTHOR_NAME=CI-RS fixture", "GIT_AUTHOR_EMAIL=ci-rs@invalid", "GIT_COMMITTER_NAME=CI-RS fixture", "GIT_COMMITTER_EMAIL=ci-rs@invalid"}, "git", "--git-dir", repo, "commit-tree", tree, "-p", parent, "-m", message)
	if rc != 0 {
		h.t.Fatalf("HARNESS_NOT_EXECUTED: actual fixture commit %s", stderr)
	}
	return strings.TrimSpace(string(out))
}

func rsRunReleaseCase(h *rsHarness, p *rsPublisherFixture, f *rsForgeProcess, binary string, c rsPublisherCase, args func(string) []string, plan, candidate string) {
	t := h.t
	deliverArgs := append(args("deliver"), "--commit", "PRO-Robotech/corelib", "--plan-sha256", plan)
	delivered := rsInvokePublisher(h, p, f, binary, c.Name+"-delivered", deliverArgs)
	lawful := rsPublisherCase{Name: "actual-pr-delivery-prerequisite", Phase: "deliver", Outcome: "GREEN", Reason: "OK", Stage: "MERGED_VERIFIED", Effects: map[string]string{"branch": "PRESENT", "pr": "PRESENT", "merge": "PRESENT"}}
	rsCheckPublisher(h, lawful, delivered, plan, candidate)
	if t.Failed() {
		return
	}
	landed := h.must(h.root, "git", "--git-dir", f.repository, "rev-parse", "refs/heads/main")
	h.must(h.root, "git", "--git-dir", f.repository, "merge-base", "--is-ancestor", candidate, landed)
	releaseArgs := append(args("release"), "--landed-sha", landed)
	readOnly := rsInvokePublisher(h, p, f, binary, c.Name+"-release-read-only", releaseArgs)
	if readOnly.result["outcome"] != "GREEN" || readOnly.result["dry_run"] != true || len(readOnly.result["effects"].([]any)) != 0 {
		t.Fatalf("SEMANTIC_MISMATCH: same exact landed input read-only release refuses: %+v", readOnly.result)
	}
	releasePlan, ok := readOnly.result["plan_sha256"].(string)
	if !ok || len(releasePlan) != 64 {
		t.Fatal("SEMANTIC_MISMATCH: release dry-run lacks plan binding")
	}
	// The old-version floor remains v1.0.0 even after the new tag is present.
	// This actual tag is a fixture prerequisite only for read-only probe cases.
	if c.Phase == "probe" {
		h.must(h.root, "git", "--git-dir", f.repository, "update-ref", "refs/tags/v1.0.1", landed, strings.Repeat("0", 40))
		status, _, err := rsForgeCall(h, f, "POST", "/releases", map[string]any{"tag_name": "v1.0.1", "target_commitish": landed, "body": "preexisting exact fixture note"})
		if err != nil || status != 201 {
			t.Fatal("HARNESS_NOT_EXECUTED: preexisting note fixture")
		}
	}
	var before map[string]any
	if err := json.Unmarshal(mustRSRead(t, f.state), &before); err != nil {
		t.Fatal(err)
	}
	beforeWrites := len(before["writes"].([]any))
	invocation := append(releaseArgs, "--commit", "PRO-Robotech/corelib", "--plan-sha256", releasePlan)
	if c.Phase == "probe" {
		invocation = []string{"--phase", "probe", "--repo", "PRO-Robotech/corelib", "--version", "v1.0.1", "--manifest", args("plan")[9], "--network-budget", "7", "--checks-budget", "7"}
	}
	// Mutations below are real receiving objects; expected source/Go payload
	// remains byte-identical, so cache/working-tree substitution cannot help.
	switch c.Fault {
	case "tag-lost-present", "tag-lost-unavailable":
		f.wrapper = rsInstallPublisherTransport(h, f.repository, strings.TrimPrefix(c.Fault, "tag-"), "refs/tags/v1.0.1", landed)
	case "tag-conflict":
		h.must(h.root, "git", "--git-dir", f.repository, "update-ref", "refs/tags/v1.0.1", p.f.base, strings.Repeat("0", 40))
	case "note-conflict":
		h.must(h.root, "git", "--git-dir", f.repository, "update-ref", "refs/tags/v1.0.1", landed, strings.Repeat("0", 40))
		status, _, err := rsForgeCall(h, f, "POST", "/releases", map[string]any{"tag_name": "v1.0.1", "target_commitish": landed, "body": "preexisting fixture note"})
		if err != nil || status != 201 {
			t.Fatal("HARNESS_NOT_EXECUTED: conflicting note source")
		}
		beforeWrites++
	case "target-ancestry-lost":
		h.must(h.root, "git", "--git-dir", f.repository, "update-ref", "refs/heads/main", p.f.base, landed)
	case "target-main-advanced":
		tree := h.must(h.root, "git", "--git-dir", f.repository, "rev-parse", landed+"^{tree}")
		next := rsFixtureCommit(h, f.repository, tree, landed, "ordinary descendant main advancement")
		h.must(h.root, "git", "--git-dir", f.repository, "update-ref", "refs/heads/main", next, landed)
	case "target-branch-only":
		tree := h.must(h.root, "git", "--git-dir", f.repository, "rev-parse", landed+"^{tree}")
		foreign := rsFixtureCommit(h, f.repository, tree, p.f.base, "unaccepted branch-only target")
		h.must(h.root, "git", "--git-dir", f.repository, "update-ref", "refs/heads/unaccepted", foreign)
		for i, arg := range invocation {
			if arg == "--landed-sha" {
				invocation[i+1] = foreign
			}
		}
	case "post-tag-ancestry-lost":
		f.wrapper = rsInstallPublisherTransport(h, f.repository, "post-tag-reset", "refs/tags/v1.0.1", landed)
	}
	f.fault(t, c.Fault)
	observed := rsInvokePublisher(h, p, f, binary, c.Name+"-subject", invocation)
	rsCheckPublisher(h, c, observed, plan, candidate)
	rsCheckActualEffects(h, f, c, observed)
	var after map[string]any
	if err := json.Unmarshal(mustRSRead(t, f.state), &after); err != nil {
		t.Fatal(err)
	}
	writes := after["writes"].([]any)[beforeWrites:]
	counts := map[string]int{}
	for _, raw := range writes {
		kind := raw.(map[string]any)["kind"].(string)
		counts[kind]++
		if _, ok := c.Effects[kind]; !ok {
			t.Errorf("SEMANTIC_MISMATCH: forbidden dependent %s write", kind)
		}
	}
	for kind, n := range counts {
		if n > 1 {
			t.Errorf("SEMANTIC_MISMATCH: duplicate %s writes %d", kind, n)
		}
	}
	actualRepo := f.repository
	if _, err := os.Stat(actualRepo); os.IsNotExist(err) {
		actualRepo += ".hidden"
	}
	tagOut, _, tagRC := h.run(h.root, nil, "git", "--git-dir", actualRepo, "rev-parse", "--verify", "refs/tags/v1.0.1^{}")
	if c.Stage == "TAG_PRESENT" || c.Stage == "ARCHIVE_VERIFIED" {
		if tagRC != 0 || strings.TrimSpace(string(tagOut)) != landed {
			t.Errorf("SEMANTIC_MISMATCH: actual tag lost or replaced after outcome %s", tagOut)
		}
	}
	for _, raw := range observed.result["effects"].([]any) {
		effect := raw.(map[string]any)
		kind := effect["kind"].(string)
		if kind == "tag" || kind == "release-note" {
			if effect["expected_sha"] != landed {
				t.Errorf("SEMANTIC_MISMATCH: target identity replaced with branch/producer %v", effect)
			}
		}
	}
	// Source must check the accepted receiving target, never local Kacho F or
	// an unrelated current-main advancement. Captured HTTP requests expose it.
	entries, err := os.ReadDir(observed.directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "http-") {
			continue
		}
		var request map[string]any
		if err := json.Unmarshal(mustRSRead(t, filepath.Join(observed.directory, entry.Name())), &request); err != nil {
			t.Fatal(err)
		}
		url := request["url"].(string)
		if strings.Contains(url, "/check-runs") || strings.HasSuffix(url, "/status") {
			if !strings.Contains(url, "/commits/"+landed+"/") {
				t.Errorf("SEMANTIC_MISMATCH: checks evaluated other SHA %s", url)
			}
		}
	}
	rsPublisherRecovery(h, p, f, binary, c, invocation, plan, candidate)
	if c.Name == "release-lawful" && !t.Failed() {
		count := len(after["writes"].([]any))
		resume := rsInvokePublisher(h, p, f, binary, c.Name+"-resume", invocation)
		rsCheckPublisher(h, c, resume, plan, candidate)
		if err := json.Unmarshal(mustRSRead(t, f.state), &after); err != nil {
			t.Fatal(err)
		}
		if len(after["writes"].([]any)) != count {
			t.Error("SEMANTIC_MISMATCH: no-op release resume repeated HTTP write")
		}
		for _, cmd := range resume.commands {
			a := []string{}
			for _, raw := range cmd["args"].([]any) {
				a = append(a, raw.(string))
			}
			if filepath.Base(cmd["program"].(string)) == "git" && rsPublisherVerb(t, a) == "push" {
				t.Error("SEMANTIC_MISMATCH: no-op release resume repeated tag push")
			}
		}
	}
}

// These controls establish the forge's individual fault axes independently of
// the publisher. Lost responses are actual closed TCP responses after mutation;
// Git commit/ref readbacks prove the resulting receiving state.
func rsForgeFaultControls(h *rsHarness, p *rsPublisherFixture) {
	for _, fault := range []string{"pr-rejected", "pr-lost-present", "pr-lost-unavailable", "pr-lost-empty", "pr-duplicate", "merge-rejected", "merge-head-conflict", "merge-lost-present", "merge-lost-unavailable", "merge-content-mismatch", "merge-ancestry-lost", "note-lost-present", "note-lost-unavailable", "note-conflict"} {
		f := rsStartForge(h, p, "control-"+fault)
		plan := rsSHA([]byte("forge control " + fault))
		branch := "release/module-" + plan
		client := filepath.Join(h.root, "candidate-publisher-ready")
		h.must(client, "git", "-c", "protocol.allow=never", "-c", "protocol.file.allow=always", "-c", "core.hooksPath=/dev/null", "push", "--receive-pack="+f.wrapper, "file://"+f.repository, p.published.revision+":refs/heads/"+branch)
		if strings.HasPrefix(fault, "pr-") {
			f.fault(h.t, fault)
		}
		status, body, err := rsForgeCall(h, f, "POST", "/pulls", map[string]any{"head": branch, "base": "main", "body": "CI-RS-1 plan-sha256:" + plan, "title": "control"})
		if strings.HasPrefix(fault, "pr-lost") || fault == "pr-duplicate" {
			if err == nil {
				h.t.Fatalf("HARNESS_NOT_EXECUTED: %s must actually lose response", fault)
			}
		} else if fault == "pr-rejected" {
			if err != nil || status != 422 {
				h.t.Fatal("HARNESS_NOT_EXECUTED: terminal PR rejection control")
			}
		} else if err != nil || status != 201 {
			h.t.Fatalf("HARNESS_NOT_EXECUTED: lawful PR for %s %d %s %v", fault, status, body, err)
		}
		readStatus, readBody, readErr := rsForgeCall(h, f, "GET", "/pulls?head=PRO-Robotech:"+branch, nil)
		if readErr != nil {
			h.t.Fatal(readErr)
		}
		if fault == "pr-lost-unavailable" {
			if readStatus != 503 {
				h.t.Fatal("HARNESS_NOT_EXECUTED: lost PR readback availability")
			}
		} else {
			var prs []any
			if err := json.Unmarshal(readBody, &prs); err != nil {
				h.t.Fatal(err)
			}
			n := 1
			if fault == "pr-rejected" || fault == "pr-lost-empty" {
				n = 0
			}
			if fault == "pr-duplicate" {
				n = 2
			}
			if len(prs) != n {
				h.t.Fatalf("HARNESS_NOT_EXECUTED: %s lookup count=%d", fault, len(prs))
			}
		}
		if strings.HasPrefix(fault, "pr-") {
			h.save("control-"+fault+"-result.json", rsCanonicalJSON(h.t, map[string]any{"classification": "HARNESS_ONLY_NO_SUT", "fault": fault, "response_exit_error": err != nil, "readback_status": readStatus, "readback_sha256": rsSHA(readBody)}))
			continue
		}
		if strings.HasPrefix(fault, "merge-") {
			f.fault(h.t, fault)
		}
		status, _, err = rsForgeCall(h, f, "PUT", "/pulls/101/merge", map[string]any{"sha": p.published.revision})
		if strings.HasPrefix(fault, "merge-lost") {
			if err == nil {
				h.t.Fatal("HARNESS_NOT_EXECUTED: actual lost merge response")
			}
		} else if fault == "merge-rejected" || fault == "merge-head-conflict" {
			expectedStatus := 405
			if fault == "merge-head-conflict" {
				expectedStatus = 409
			}
			if err != nil || status != expectedStatus {
				h.t.Fatal("HARNESS_NOT_EXECUTED: merge rejection")
			}
		} else if err != nil || status != 200 {
			h.t.Fatalf("HARNESS_NOT_EXECUTED: merge response %d %v", status, err)
		}
		readStatus, readBody, readErr = rsForgeCall(h, f, "GET", "/pulls/101", nil)
		if readErr != nil {
			h.t.Fatal(readErr)
		}
		if fault == "merge-lost-unavailable" {
			if readStatus != 503 {
				h.t.Fatal("HARNESS_NOT_EXECUTED: lost merge readback")
			}
		} else {
			var pr map[string]any
			if err := json.Unmarshal(readBody, &pr); err != nil {
				h.t.Fatal(err)
			}
			if pr["merged"] != (fault != "merge-rejected" && fault != "merge-head-conflict") {
				h.t.Fatal("HARNESS_NOT_EXECUTED: merge fact control")
			}
		}
		var state map[string]any
		if err := json.Unmarshal(mustRSRead(h.t, f.state), &state); err != nil {
			h.t.Fatal(err)
		}
		pr := state["prs"].([]any)[0].(map[string]any)
		if fault != "merge-rejected" && fault != "merge-head-conflict" {
			merge := pr["merge_commit_sha"].(string)
			main := h.must(h.root, "git", "--git-dir", f.repository, "rev-parse", "refs/heads/main")
			_, _, ancestor := h.run(h.root, nil, "git", "--git-dir", f.repository, "merge-base", "--is-ancestor", merge, main)
			if (ancestor == 0) == (fault == "merge-ancestry-lost") {
				h.t.Fatal("HARNESS_NOT_EXECUTED: actual receiving ancestry inversion")
			}
			tree := h.must(h.root, "git", "--git-dir", f.repository, "rev-parse", merge+"^{tree}")
			expectedTree := h.must(client, "git", "rev-parse", p.published.revision+"^{tree}")
			if (tree == expectedTree) == (fault == "merge-content-mismatch") {
				h.t.Fatal("HARNESS_NOT_EXECUTED: actual receiving content inversion")
			}
			if strings.HasPrefix(fault, "note-") {
				h.must(h.root, "git", "--git-dir", f.repository, "update-ref", "refs/tags/v1.0.1", merge, strings.Repeat("0", 40))
				f.fault(h.t, fault)
				status, _, err = rsForgeCall(h, f, "POST", "/releases", map[string]any{"tag_name": "v1.0.1", "target_commitish": merge})
				if strings.HasPrefix(fault, "note-lost") && err == nil {
					h.t.Fatal("HARNESS_NOT_EXECUTED: note lost response")
				}
				readStatus, readBody, readErr = rsForgeCall(h, f, "GET", "/releases/tags/v1.0.1", nil)
				if readErr != nil {
					h.t.Fatal(readErr)
				}
				if fault == "note-lost-unavailable" {
					if readStatus != 503 {
						h.t.Fatal("HARNESS_NOT_EXECUTED: note unavailable")
					}
				} else {
					var note map[string]any
					if err := json.Unmarshal(readBody, &note); err != nil {
						h.t.Fatal(err)
					}
					if (note["target_commitish"] == merge) == (fault == "note-conflict") {
						h.t.Fatal("HARNESS_NOT_EXECUTED: note identity inversion")
					}
				}
			}
		}
		h.save("control-"+fault+"-result.json", rsCanonicalJSON(h.t, map[string]any{"classification": "HARNESS_ONLY_NO_SUT", "fault": fault, "readback_status": readStatus, "readback_sha256": rsSHA(readBody), "actual_pr": pr}))
	}
}

func rsForgeMutationControls(h *rsHarness, p *rsPublisherFixture) {
	for _, axis := range []string{"input", "consumer", "base", "baseline"} {
		f := rsStartForge(h, p, "mutation-"+axis)
		plan := rsSHA([]byte("after-admission mutation " + axis))
		branch := "release/module-" + plan
		h.must(filepath.Join(h.root, "candidate-publisher-ready"), "git", "-c", "protocol.allow=never", "-c", "protocol.file.allow=always", "push", "--receive-pack="+f.wrapper, "file://"+f.repository, p.published.revision+":refs/heads/"+branch)
		mutation := map[string]any{"trigger": "after-pr-create", "accepted_plan": plan}
		if axis == "input" || axis == "consumer" {
			path := filepath.Join(filepath.Dir(f.control), axis+"-source.json")
			before := []byte("frozen input\n")
			after := "changed input\n"
			if axis == "consumer" {
				before = rsCanonicalJSON(h.t, p.manifest)
				m := rsCloneJSON(p.manifest)
				m["consumers"].([]any)[0].(map[string]any)["contexts"].([]any)[0].(map[string]any)["tags"] = []string{"changed"}
				after = string(rsCanonicalJSON(h.t, m))
			}
			if err := os.WriteFile(path, before, 0600); err != nil {
				h.t.Fatal(err)
			}
			mutation["kind"] = "file"
			mutation["path"] = path
			mutation["before_sha256"] = rsSHA(before)
			mutation["after"] = after
		} else {
			name := "refs/heads/main"
			if axis == "baseline" {
				name = "refs/tags/v1.0.0"
			}
			mutation["kind"] = "ref"
			mutation["ref"] = name
			mutation["before_sha"] = p.f.base
			mutation["operation"] = "advance"
		}
		if err := os.WriteFile(f.control, rsCanonicalJSON(h.t, map[string]any{"fault": "lawful", "mutation": mutation}), 0600); err != nil {
			h.t.Fatal(err)
		}
		status, _, err := rsForgeCall(h, f, "POST", "/pulls", map[string]any{"head": branch, "base": "main", "body": "CI-RS-1 plan-sha256:" + plan, "title": "actual admitted-effect control"})
		if err != nil || status != 201 {
			h.t.Fatalf("HARNESS_NOT_EXECUTED: after-PR mutation %s %d %v", axis, status, err)
		}
		var state map[string]any
		if err := json.Unmarshal(mustRSRead(h.t, f.state), &state); err != nil {
			h.t.Fatal(err)
		}
		record, ok := state["controlled_mutation"].(map[string]any)
		if !ok || record["accepted_plan"] != plan || len(state["prs"].([]any)) != 1 {
			h.t.Fatal("HARNESS_NOT_EXECUTED: mutation timing/identity")
		}
		if axis == "input" || axis == "consumer" {
			if record["before_sha256"] == record["after_sha256"] || rsSHA(mustRSRead(h.t, mutation["path"].(string))) != record["after_sha256"] {
				h.t.Fatal("HARNESS_NOT_EXECUTED: actual file mutation")
			}
		} else {
			now := h.must(h.root, "git", "--git-dir", f.repository, "rev-parse", mutation["ref"].(string))
			if now == p.f.base || now != record["after_sha"] {
				h.t.Fatal("HARNESS_NOT_EXECUTED: actual ref mutation")
			}
			h.must(h.root, "git", "--git-dir", f.repository, "merge-base", "--is-ancestor", p.f.base, now)
		}
		h.save("mutation-control-"+axis+".json", rsCanonicalJSON(h.t, record))
	}
}
func rsTagTransportControls(h *rsHarness, p *rsPublisherFixture) {
	client := filepath.Join(h.root, "candidate-publisher-ready")
	for _, mode := range []string{"lawful", "lost-present", "lost-unavailable", "post-tag-reset"} {
		dir := filepath.Join(h.root, "tag-transport-"+mode)
		if err := os.MkdirAll(dir, 0755); err != nil {
			h.t.Fatal(err)
		}
		origin := filepath.Join(dir, "receiving.git")
		h.must(h.root, "git", "clone", "--bare", "--no-local", p.f.origin, origin)
		h.must(client, "git", "-c", "protocol.allow=never", "-c", "protocol.file.allow=always", "push", "file://"+origin, p.published.revision+":refs/heads/main")
		wrapper := rsInstallPublisherTransport(h, origin, mode, "refs/tags/v1.0.1", p.published.revision)
		_, stderr, rc := h.run(client, nil, "git", "-c", "protocol.allow=never", "-c", "protocol.file.allow=always", "-c", "core.hooksPath=/dev/null", "push", "--porcelain", "--receive-pack="+wrapper, "file://"+origin, p.published.revision+":refs/tags/v1.0.1")
		if (rc == 0) != (mode == "lawful" || mode == "post-tag-reset") {
			h.t.Fatalf("HARNESS_NOT_EXECUTED: tag transport %s rc%d %s", mode, rc, stderr)
		}
		actual := origin
		if mode == "lost-unavailable" {
			actual += ".hidden"
		}
		tag := h.must(h.root, "git", "--git-dir", actual, "rev-parse", "refs/tags/v1.0.1^{}")
		if tag != p.published.revision {
			h.t.Fatal("HARNESS_NOT_EXECUTED: tag was not actually created")
		}
		main := h.must(h.root, "git", "--git-dir", actual, "rev-parse", "refs/heads/main")
		_, _, ancestor := h.run(h.root, nil, "git", "--git-dir", actual, "merge-base", "--is-ancestor", tag, main)
		if (ancestor == 0) == (mode == "post-tag-reset") {
			h.t.Fatal("HARNESS_NOT_EXECUTED: actual post-tag ancestry inversion")
		}
		read, _, readRC := h.run(h.root, nil, "git", "-c", "protocol.allow=never", "-c", "protocol.file.allow=always", "ls-remote", "--refs", "file://"+origin, "refs/tags/v1.0.1")
		if mode == "lost-unavailable" {
			if readRC == 0 || len(read) != 0 {
				h.t.Fatal("HARNESS_NOT_EXECUTED: unavailable tag readback")
			}
		} else if readRC != 0 || !strings.Contains(string(read), tag) {
			h.t.Fatal("HARNESS_NOT_EXECUTED: exact tag readback")
		}
		rsVerifyHookChildren(h, filepath.Join(dir, "transport-captures"))
		rsCopyCaptures(h, filepath.Join(dir, "transport-captures"), "tag-control-"+mode)
		h.save("tag-transport-"+mode+".json", rsCanonicalJSON(h.t, map[string]any{"classification": "HARNESS_ONLY_NO_SUT", "mode": mode, "push_exit": rc, "actual_tag": tag, "actual_main": main, "ancestry_exit": ancestor, "readback_exit": readRC}))
	}
}

func rsVerifyHookChildren(h *rsHarness, directory string) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		h.t.Fatal(err)
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), "-hook.json") {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal(mustRSRead(h.t, filepath.Join(directory, entry.Name())), &record); err != nil {
			h.t.Fatal(err)
		}
		if record["verified_owned_receive_pack"] != true {
			h.t.Fatal("HARNESS_NOT_EXECUTED: hook did not verify owned receiving process")
		}
		for _, key := range []string{"hook_pid", "receive_pack_pid"} {
			pid := int(record[key].(float64))
			for i := 0; i < 100; i++ {
				if _, e := os.Stat(fmt.Sprintf("/proc/%d", pid)); os.IsNotExist(e) {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if _, e := os.Stat(fmt.Sprintf("/proc/%d", pid)); !os.IsNotExist(e) {
				h.t.Fatalf("HARNESS_NOT_EXECUTED: owned hook/receive-pack remains %d", pid)
			}
		}
	}
}
func rsValidatePublisherCoverage(h *rsHarness, cases []rsPublisherCase) {
	expected := map[string]bool{}
	for _, n := range []int{1, 2, 10, 11, 12, 13, 19, 22} {
		expected[fmt.Sprintf("CI-RS-%02d", n)] = true
	}
	seen := map[string]bool{}
	names := map[string]bool{}
	for _, c := range cases {
		if names[c.Name] {
			h.t.Fatalf("HARNESS_NOT_EXECUTED: duplicate publisher case %s", c.Name)
		}
		names[c.Name] = true
		for _, raw := range strings.Split(c.Scenario, "/") {
			id := raw
			if !strings.HasPrefix(id, "CI-RS-") {
				id = "CI-RS-" + id
			}
			if !expected[id] {
				h.t.Fatalf("HARNESS_NOT_EXECUTED: unknown publisher scenario %s", id)
			}
			seen[id] = true
		}
	}
	if len(cases) == 0 || len(seen) != len(expected) {
		h.t.Fatalf("HARNESS_NOT_EXECUTED: incomplete publisher scenario census %v", seen)
	}
	h.save("publisher-scenario-census.json", rsCanonicalJSON(h.t, map[string]any{"required_scenarios": expected, "declared_scenarios": seen, "declared_cases": len(cases), "invoked_cases": 0, "full_CI_RS_T1_execution_claimed": false}))
}

func rsCheckActualEffects(h *rsHarness, f *rsForgeProcess, c rsPublisherCase, o rsPublisherObservation) {
	t := h.t
	var state map[string]any
	if err := json.Unmarshal(mustRSRead(t, f.state), &state); err != nil {
		t.Fatal(err)
	}
	root := f.repository
	if _, err := os.Stat(root); os.IsNotExist(err) {
		root += ".hidden"
	}
	readRef := func(name string) string {
		out, _, rc := h.run(h.root, nil, "git", "--git-dir", root, "rev-parse", "--verify", name)
		if rc != 0 {
			return ""
		}
		return strings.TrimSpace(string(out))
	}
	for _, raw := range o.result["effects"].([]any) {
		effect := raw.(map[string]any)
		kind, status := effect["kind"].(string), effect["state"].(string)
		if kind == "branch" || kind == "tag" {
			observed := readRef(effect["ref"].(string) + "^{}")
			switch status {
			case "PRESENT", "CONFLICT":
				historical := false
				if kind == "branch" && status == "PRESENT" && (c.Fault == "pr-head-changed" || c.Fault == "merge-head-conflict") {
					if mutation, ok := state["head_mutation"].(map[string]any); ok {
						historical = mutation["ref"] == effect["ref"] && mutation["before_sha"] == effect["sha"] && mutation["after_sha"] == observed
					}
				}
				if observed == "" || (effect["sha"] != observed && !historical) {
					t.Errorf("SEMANTIC_MISMATCH: claimed %s %s differs from actual Git facts %s", kind, status, observed)
				}
			case "ABSENT":
				if observed != "" {
					t.Errorf("SEMANTIC_MISMATCH: ABSENT %s actually exists %s", kind, observed)
				}
			}
		}
		if kind == "pr" || kind == "merge" {
			prs := state["prs"].([]any)
			if status == "ABSENT" && kind == "pr" && len(prs) != 0 {
				t.Error("SEMANTIC_MISMATCH: ABSENT PR actually exists")
			}
			if status == "PRESENT" {
				found := false
				for _, prRaw := range prs {
					pr := prRaw.(map[string]any)
					if effect["pr_number"] != pr["number"] {
						continue
					}
					found = true
					actualHead := readRef("refs/heads/" + pr["head_ref"].(string))
					if effect["sha"] != actualHead || effect["head_ref"] != "refs/heads/"+pr["head_ref"].(string) {
						t.Errorf("SEMANTIC_MISMATCH: exact PR head differs %+v", effect)
					}
					if kind == "pr" && !strings.Contains(pr["body"].(string), effect["plan_marker"].(string)) {
						t.Error("SEMANTIC_MISMATCH: actual PR marker differs")
					}
					if kind == "merge" && (pr["merged"] != true || effect["merge_sha"] != pr["merge_commit_sha"]) {
						t.Error("SEMANTIC_MISMATCH: claimed merge is not actual receiving commit")
					}
				}
				if !found {
					t.Error("SEMANTIC_MISMATCH: claimed PRESENT PR/merge absent from actual fixture")
				}
			}
		}
		if kind == "release-note" && status == "PRESENT" {
			found := false
			for _, raw := range state["notes"].([]any) {
				note := raw.(map[string]any)
				if note["id"] == effect["release_id"] && "refs/tags/"+note["tag_name"].(string) == effect["tag_ref"] && note["target_commitish"] == effect["expected_sha"] {
					found = true
				}
			}
			if !found {
				t.Error("SEMANTIC_MISMATCH: claimed PRESENT note lacks exact actual fixture object")
			}
		}
	}
	if strings.HasSuffix(c.Fault, "-after-pr") {
		mutation, ok := state["controlled_mutation"].(map[string]any)
		if !ok || mutation["trigger"] != "after actual branch push and PR creation, before PR response" {
			t.Error("HARNESS_NOT_EXECUTED: intended in-invocation mutation was not reached")
		}
	}
	if c.Outcome == "GREEN" {
		builds, lists := 0, 0
		for _, cmd := range o.commands {
			if filepath.Base(cmd["program"].(string)) != "go" {
				continue
			}
			for _, arg := range cmd["args"].([]any) {
				if arg == "build" {
					builds++
				}
				if arg == "list" {
					lists++
				}
			}
		}
		if builds < 2 || lists < 2 {
			t.Errorf("SEMANTIC_MISMATCH: phase GREEN lacks actual two-consumer Go list/build executions: list=%d build=%d", lists, builds)
		}
	}
}

func rsPublisherParserControls(h *rsHarness) {
	source := rsPublisherBridgeSource(h.t)
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, "publisher-bridge.go", source, parser.AllErrors)
	if err != nil {
		h.t.Fatal(err)
	}
	functions := ""
	count := 0
	for _, decl := range parsed.Decls {
		f, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if f.Name.Name != "rsGitCommandVerb" && f.Name.Name != "rsPublisherGitCommandIndex" && f.Name.Name != "rsGitTransportArgs" {
			continue
		}
		file := fset.File(f.Pos())
		functions += source[file.Offset(f.Pos()):file.Offset(f.End())] + "\n"
		count++
	}
	if count != 3 {
		h.t.Fatal("HARNESS_NOT_EXECUTED: actual publisher parser extraction")
	}
	program := "package main\nimport(\"encoding/json\";\"fmt\";\"os\";\"strings\")\n" + functions + "\nfunc main(){args:=os.Args[1:];b,_:=json.Marshal(map[string]any{\"index\":rsPublisherGitCommandIndex(args),\"verb\":rsGitCommandVerb(args),\"args\":args});fmt.Println(string(b))}\n"
	h.put(h.root, "actual-publisher-parser.go", program)
	h.save("actual-publisher-parser.go.txt", []byte(program))
	binary := filepath.Join(h.root, "actual-publisher-parser")
	h.must(h.root, h.goBin, "build", "-o", binary, filepath.Join(h.root, "actual-publisher-parser.go"))
	controls := []struct {
		args  []string
		index int
		verb  string
	}{
		{[]string{"remote", "get-url", "origin"}, 0, "remote"},
		{[]string{"-C", "clone", "remote", "get-url", "origin"}, 2, "remote"},
		{[]string{"-Cclone", "remote", "get-url", "origin"}, -1, ""},
		{[]string{"-c", "fetch", "remote", "get-url", "origin"}, 2, "remote"},
		{[]string{"-c", "probe.mode=clone", "-C", "push", "remote", "get-url", "origin"}, 4, "remote"},
		{[]string{"-cprobe.mode=fetch", "ls-remote", "origin"}, -1, ""},
		{[]string{"--git-dir", "clone", "--work-tree", "fetch", "remote", "get-url", "origin"}, 4, "remote"},
		{[]string{"--namespace=clone", "--config-env", "probe.mode=ENV", "fetch", "origin"}, 3, "fetch"},
		{[]string{"--super-prefix", "ls-remote", "--attr-source=HEAD", "remote", "get-url", "origin"}, 3, "remote"},
		{[]string{"--bare", "--no-pager", "--no-replace-objects", "clone", "source", "dest"}, 3, "clone"},
		{[]string{"-C", "one", "-C", "two", "-c", "probe.mode=ls-remote", "push", "origin"}, 6, "push"},
		{[]string{"--exec-path=/fixture/bin", "ls-remote", "origin"}, 1, "ls-remote"},
		{[]string{"--unknown", "clone"}, -1, ""}, {[]string{"-C"}, -1, ""}, {[]string{"--version", "fetch"}, -1, ""}, {[]string{}, -1, ""},
	}
	for _, c := range controls {
		raw := h.must(h.root, binary, c.args...)
		var result struct {
			Index int
			Verb  string
			Args  []string
		}
		if err := json.Unmarshal([]byte(raw), &result); err != nil {
			h.t.Fatal(err)
		}
		if result.Index != c.index || result.Verb != c.verb || !reflect.DeepEqual(result.Args, c.args) {
			h.t.Fatalf("HARNESS_NOT_EXECUTED: actual parser %q => %+v expected%d/%s", c.args, result, c.index, c.verb)
		}
	}
	h.save("publisher-parser-controls.json", rsCanonicalJSON(h.t, map[string]any{"classification": "HARNESS_ONLY_NO_SUT", "actual_extracted_source_sha256": rsSHA([]byte(program)), "executed": len(controls), "passed": len(controls)}))
}

func rsPublisherRecovery(h *rsHarness, p *rsPublisherFixture, f *rsForgeProcess, binary string, c rsPublisherCase, invocation []string, plan, candidate string) {
	if h.t.Failed() {
		return
	}
	if c.Fault != "branch-lost-unavailable" && c.Fault != "pr-lost-unavailable" && c.Fault != "pr-lost-empty" && c.Fault != "merge-lost-unavailable" && c.Fault != "tag-lost-unavailable" && c.Fault != "note-lost-unavailable" {
		return
	}
	// Only availability changes between these explicit invocations. No ref,
	// PR, merge or note is fabricated/replaced and no new key is supplied.
	if _, err := os.Stat(f.repository); os.IsNotExist(err) {
		if err := os.Rename(f.repository+".hidden", f.repository); err != nil {
			h.t.Fatal(err)
		}
	}
	f.fault(h.t, "lawful")
	recovery := c
	recovery.Name = c.Name + "-readback-recovery"
	recovery.Fault = "lawful"
	recovery.Outcome = "GREEN"
	recovery.Reason = "OK"
	recovery.Stage = "MERGED_VERIFIED"
	recovery.Effects = map[string]string{"branch": "PRESENT", "pr": "PRESENT", "merge": "PRESENT"}
	if c.Phase == "release" {
		recovery.Stage = "TAG_PRESENT"
		recovery.Effects["tag"] = "PRESENT"
		recovery.Effects["release-note"] = "PRESENT"
	}
	observed := rsInvokePublisher(h, p, f, binary, recovery.Name, invocation)
	rsCheckPublisher(h, recovery, observed, plan, candidate)
	rsCheckActualEffects(h, f, recovery, observed)
	var state map[string]any
	if err := json.Unmarshal(mustRSRead(h.t, f.state), &state); err != nil {
		h.t.Fatal(err)
	}
	counts := map[string]int{}
	for _, raw := range state["writes"].([]any) {
		counts[raw.(map[string]any)["kind"].(string)]++
	}
	for kind, n := range counts {
		if n > 1 {
			h.t.Errorf("SEMANTIC_MISMATCH: recovery duplicated already-created %s (%d writes)", kind, n)
		}
	}
	for _, cmd := range observed.commands {
		if filepath.Base(cmd["program"].(string)) != "git" {
			continue
		}
		args := []string{}
		for _, arg := range cmd["args"].([]any) {
			args = append(args, arg.(string))
		}
		if rsPublisherVerb(h.t, args) == "push" {
			h.t.Error("SEMANTIC_MISMATCH: recovery rewrote already-created branch/tag")
		}
	}
}
