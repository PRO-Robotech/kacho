// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package release

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// This adds the accepted longest-module-prefix case without altering the
// frozen 27 consumer cases. All dependency ZIPs and caches are produced by
// real Git/Go commands; the bridge receives no manufactured command output.
func TestReleaseConsumerSeparatelyRequiredNestedModule(t *testing.T) {
	h := newRSHarness(t)
	parent := "github.com/PRO-Robotech/corelib"
	nested := parent + "/nested"
	target := h.initRepo("nested-parent", map[string]string{
		"go.mod":         "module " + parent + "\n\ngo 1.21\n",
		"first/value.go": "package first\nconst Value=1\n",
	})
	h.must(target, "git", "remote", "add", "origin", "https://github.com/PRO-Robotech/corelib.git")
	lawful := h.archive(target, parent, "nested-parent-lawful")
	dependency := h.initRepo("separate-nested", map[string]string{
		"go.mod":        "module " + nested + "\n\ngo 1.21\n",
		"leaf/value.go": "package leaf\nconst Value=2\n",
	})
	dep := h.archive(dependency, nested, "separate-nested")
	program := "package main\nimport \"" + parent + "/first\"\nimport \"" + nested + "/leaf\"\nfunc main(){ _ = first.Value + leaf.Value }\n"
	gomod := func(a rsArchive) string {
		return "module github.com/PRO-Robotech/kacho\n\ngo 1.21\n\nrequire (\n" + parent + " " + a.version + "\n" + nested + " " + dep.version + "\n)\n"
	}
	consumer := h.initRepo("nested-consumer", map[string]string{"go.mod": gomod(lawful), "main.go": program})
	h.must(consumer, "git", "remote", "add", "origin", "https://github.com/PRO-Robotech/kacho.git")
	consumerSHA := h.must(consumer, "git", "rev-parse", "HEAD")
	origin := filepath.Join(h.root, "nested-consumer.git")
	h.must(h.root, "git", "clone", "--bare", "--no-local", consumer, origin)
	cache := filepath.Join(h.root, "actual-download-cache")
	env := func(a rsArchive) []string {
		return []string{"GOPROXY=file://" + filepath.ToSlash(a.proxy) + ",file://" + filepath.ToSlash(dep.proxy), "GOMODCACHE=" + cache, "GOSUMDB=off", "GONOSUMDB=", "GOPRIVATE=", "GONOPROXY=", "CGO_ENABLED=0", "GOFLAGS=-mod=mod"}
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, h.goBin, "clean", "-modcache")
		cmd.Env = rsEnvironment(env(lawful)...)
		_ = cmd.Run()
	})
	control := filepath.Join(h.root, "actual-control")
	h.put(control, "go.mod", gomod(lawful))
	h.put(control, "main.go", program)
	for _, args := range [][]string{{"mod", "download", "all"}, {"build", "-o", filepath.Join(h.root, "actual-two-module-consumer"), "."}} {
		out, errout, rc := h.run(control, env(lawful), h.goBin, args...)
		if rc != 0 {
			t.Fatalf("HARNESS_NOT_EXECUTED: real two-module prerequisite %v rc%d: %s %s", args, rc, out, errout)
		}
	}
	listed, errout, rc := h.run(control, env(lawful), h.goBin, "list", "-deps", "-json", ".")
	if rc != 0 {
		t.Fatalf("HARNESS_NOT_EXECUTED: actual module ownership: %s", errout)
	}
	owners := map[string]string{}
	decoder := json.NewDecoder(bytes.NewReader(listed))
	for {
		var pkg struct {
			ImportPath string
			Module     *struct{ Path, Version string }
		}
		err := decoder.Decode(&pkg)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if pkg.Module != nil {
			owners[pkg.ImportPath] = pkg.Module.Path
		}
	}
	if owners[parent+"/first"] != parent || owners[nested+"/leaf"] != nested {
		t.Fatalf("HARNESS_NOT_EXECUTED: actual distinct module ownership: %v", owners)
	}
	compiled := mustRSRead(t, filepath.Join(h.root, "actual-two-module-consumer"))
	if len(compiled) == 0 {
		t.Fatal("HARNESS_NOT_EXECUTED: empty executable")
	}
	// Keep the exact target package's lawful ZIP in the actual cache. The
	// missing-parent twin must still reject its own candidate archive.
	h.must(target, "git", "rm", "--cached", "first/value.go")
	h.must(target, "git", "commit", "--quiet", "-m", "single fact: parent package absent from archive")
	if delta := h.must(target, "git", "diff", "--name-status", lawful.revision, "HEAD"); delta != "D\tfirst/value.go" {
		t.Fatalf("HARNESS_NOT_EXECUTED: parent twin delta %q", delta)
	}
	bad := h.archive(target, parent, "nested-parent-missing")
	if _, err := os.Stat(filepath.Join(target, "first/value.go")); err != nil {
		t.Fatal(err)
	}
	badRoot := filepath.Join(h.root, "missing-parent-candidate")
	h.must(h.root, "git", "clone", "--quiet", "--no-local", target, badRoot)
	h.must(badRoot, "git", "remote", "set-url", "origin", "https://github.com/PRO-Robotech/corelib.git")
	h.put(badRoot, "first/value.go", string(mustRSRead(t, filepath.Join(target, "first/value.go"))))
	h.put(control, "go.mod", gomod(bad))
	_, badErr, badRC := h.run(control, env(bad), h.goBin, "build", "-o", filepath.Join(h.root, "missing-parent-binary"), ".")
	if badRC == 0 || !bytes.Contains(badErr, []byte(parent+"/first")) {
		t.Fatalf("HARNESS_NOT_EXECUTED: actual missing-parent inversion rc%d: %s", badRC, badErr)
	}
	h.must(target, "git", "checkout", "--force", "--quiet", lawful.revision)
	prereq, _ := json.MarshalIndent(map[string]any{"classification": "HARNESS_PREREQUISITE_ONLY", "parent_module": parent, "nested_module": nested, "owners": owners, "compiled_sha256": rsSHA(compiled), "compiled_bytes": len(compiled), "lawful_parent_zip_sha256": rsSHA(mustRSRead(t, lawful.path)), "nested_zip_sha256": rsSHA(mustRSRead(t, dep.path)), "negative_parent_zip_sha256": rsSHA(mustRSRead(t, bad.path)), "missing_parent_build_exit": badRC, "expected_parent_census": 1, "real_dependency_download_cache": cache}, "", "  ")
	h.save("nested-module-prerequisites.json", append(prereq, '\n'))
	manifest := map[string]any{"schema_version": 1, "repository": "PRO-Robotech/corelib", "module_path": parent, "version": "v1.0.1", "candidate_root": target, "consumers": []any{map[string]any{"type": "repository", "repository": "PRO-Robotech/kacho", "root": consumer, "revision": consumerSHA, "module_roots": []string{"."}, "contexts": []any{map[string]any{"goos": "linux", "goarch": "amd64", "cgo_enabled": false, "tags": []string{}}}}}, "budgets": map[string]any{"network_seconds": 2, "checks_seconds": 5}}
	cases := []struct{ Name, Root, Revision, Outcome, Reason string }{{"lawful-separate-required-nested", target, lawful.revision, "GREEN", "OK"}, {"missing-parent-package-with-lawful-cache", badRoot, bad.revision, "RED", "CONSUMER_IMPORT_MISSING"}}
	ledger, _ := json.MarshalIndent(map[string]any{"scenario": "CI-RS-07/09", "accepted_rule": "longest matching separately required module owns import", "prepared_cases": 2, "cases": cases, "expected_parent_imports": 1, "sut_invocations": 0}, "", "  ")
	h.save("nested-case-ledger.json", append(ledger, '\n'))
	if missing := rsSupplySymbols(h, []string{"RunSupplyPreflight", "SupplyDependencies", "SupplyCommand", "SupplyCommandResult"}); len(missing) > 0 {
		b, _ := json.MarshalIndent(map[string]any{"holder_outcome": "CAPABILITY_ABSENT", "prepared_cases": 2, "missing_symbols": missing, "sut_invocations": 0, "sut_semantic_decisions": 0}, "", "  ")
		h.save("capability.json", append(b, '\n'))
		t.Fatalf("CAPABILITY_ABSENT: real two-module prerequisite and missing-parent inversion established; missing%v; sut_invocations=0", missing)
	}
	binary := rsBuildConsumerBridge(h)
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
			m := rsCloneJSON(manifest)
			m["candidate_root"] = c.Root
			raw, _ := json.Marshal(m)
			manifestPath := filepath.Join(h.root, c.Name+".manifest.json")
			ch.put(h.root, c.Name+".manifest.json", string(raw))
			ch.save("manifest.json", raw)
			outDir := filepath.Join(h.root, "result-"+c.Name)
			if e := os.MkdirAll(outDir, 0755); e != nil {
				t.Fatal(e)
			}
			request := map[string]any{"args": []string{"--mode", "consumers", "--manifest", manifestPath, "--revision", c.Revision}, "output": outDir, "remotes": map[string]string{"https://github.com/PRO-Robotech/kacho.git": "file://" + filepath.ToSlash(origin)}}
			encoded, _ := json.Marshal(request)
			requestPath := filepath.Join(h.root, c.Name+".request.json")
			ch.put(h.root, c.Name+".request.json", string(encoded))
			_, stderr, bridgeRC := ch.run(h.root, []string{"CI_RS_BRIDGE_REQUEST=" + requestPath, "GOPROXY=file://" + filepath.ToSlash(dep.proxy), "GOMODCACHE=" + cache, "GOSUMDB=off"}, binary, "-test.run=^TestCIRSConsumerBridge$", "-test.v", "-test.timeout=115s")
			if bridgeRC != 0 {
				t.Fatalf("HARNESS_NOT_EXECUTED: bridge rc%d: %s", bridgeRC, stderr)
			}
			var meta struct {
				Exit      int      `json:"exit_code"`
				Unhandled []string `json:"unhandled_boundaries"`
				Deadline  bool     `json:"deadline_exceeded"`
			}
			if e := json.Unmarshal(mustRSRead(t, filepath.Join(outDir, "bridge-result.json")), &meta); e != nil || len(meta.Unhandled) > 0 || meta.Deadline {
				t.Fatalf("HARNESS_NOT_EXECUTED: bridge metadata %v %+v", e, meta)
			}
			entries, e := os.ReadDir(outDir)
			if e != nil {
				t.Fatal(e)
			}
			for _, entry := range entries {
				b := mustRSRead(t, filepath.Join(outDir, entry.Name()))
				ch.save(entry.Name(), b)
				if strings.HasPrefix(entry.Name(), "command-") && strings.HasSuffix(entry.Name(), ".json") {
					var cmd struct {
						Program     string
						Args        []string
						Environment map[string]string `json:"go_environment"`
					}
					if e := json.Unmarshal(b, &cmd); e != nil {
						t.Fatal(e)
					}
					if filepath.Base(cmd.Program) == "git" {
						for _, arg := range cmd.Args {
							if arg == "push" {
								t.Error("SEMANTIC_MISMATCH: read-only mode attempted push")
							}
						}
					}
					if filepath.Base(cmd.Program) == "go" && len(cmd.Args) > 0 && (cmd.Args[0] == "list" || cmd.Args[0] == "build" || cmd.Args[0] == "test") {
						if cmd.Environment["GOWORK"] != "off" || cmd.Environment["GOMODCACHE"] == "" || cmd.Environment["GOMODCACHE"] == cache || !strings.HasPrefix(cmd.Environment["GOPROXY"], "file://") {
							t.Errorf("SEMANTIC_MISMATCH: SUT build does not use its own isolated candidate cache: %v", cmd.Environment)
						}
					}
				}
			}
			result := rsValidateResult(&ch, mustRSRead(t, filepath.Join(outDir, "sut.stdout")), c.Name)
			wantRC := map[string]int{"GREEN": 0, "RED": 1}[c.Outcome]
			if meta.Exit != wantRC || result["exit_code"] != float64(wantRC) || result["outcome"] != c.Outcome || result["reason"] != c.Reason {
				t.Errorf("SEMANTIC_MISMATCH: want%s/%s/%d actual exit%d %+v", c.Outcome, c.Reason, wantRC, meta.Exit, result)
			}
			if result["phase"] != "consumers" || result["stage"] != "NONE" || len(result["effects"].([]any)) != 0 {
				t.Errorf("SEMANTIC_MISMATCH: read-only phase/effects %+v", result)
			}
			if census := result["census"].(map[string]any)["consumer_imports"]; census != float64(1) {
				t.Errorf("SEMANTIC_MISMATCH: parent census=%v; separately required nested import must be excluded", census)
			}
			seen := map[string]bool{}
			for _, raw := range result["checks"].([]any) {
				check := raw.(map[string]any)
				p := check["predicate"].(string)
				if seen[p] {
					t.Errorf("SEMANTIC_MISMATCH: duplicate predicate%s", p)
				}
				seen[p] = true
				if c.Outcome == "GREEN" && check["outcome"] != "GREEN" {
					t.Errorf("SEMANTIC_MISMATCH: lawful predicate%s is%v", p, check["outcome"])
				}
			}
			if c.Outcome == "GREEN" {
				if len(seen) != 5 {
					t.Errorf("SEMANTIC_MISMATCH: predicate count%v", seen)
				}
				for _, p := range []string{"invocation", "identity", "input", "consumer-census", "consumer-archive"} {
					if !seen[p] {
						t.Errorf("SEMANTIC_MISMATCH: missing%s", p)
					}
				}
			}
		})
	}
}
