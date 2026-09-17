// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package release

import (
	"encoding/json"
	"golang.org/x/mod/module"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// rsRemotePinPrerequisites establishes actual origin reachability independently
// of any proposed pin checker. No local ref or locally present object is used
// as a substitute for a fresh remote clone. It is prerequisite evidence only.
func rsRemotePinPrerequisites(t *testing.T) {
	t.Helper()
	h := newRSHarness(t)
	repo := h.initRepo("pin-owner", map[string]string{
		"go.mod":     "module example.com/ci-rs-owner\n\ngo 1.21\n",
		"lib/lib.go": "package lib\n\nconst Value = 1\n",
	})
	base := h.must(repo, "git", "rev-parse", "HEAD")
	remote := filepath.Join(h.root, "origin.git")
	h.must(h.root, "git", "clone", "--bare", "--no-local", repo, remote)
	h.must(repo, "git", "remote", "add", "origin", "file://"+filepath.ToSlash(remote))
	h.must(repo, "git", "switch", "--quiet", "-c", "feature")
	h.put(repo, "lib/lib.go", "package lib\n\nconst Value = 2\n")
	h.must(repo, "git", "add", "lib/lib.go")
	h.must(repo, "git", "commit", "--quiet", "-m", "pin revision")
	pin := h.must(repo, "git", "rev-parse", "HEAD")
	if pin == base {
		t.Fatal("HARNESS_NOT_EXECUTED: no pin delta")
	}
	h.must(repo, "git", "tag", "local-only-pin", pin)

	type observation struct {
		Label            string `json:"label"`
		RemoteRefs       string `json:"remote_refs"`
		ObjectExit       int    `json:"object_exit"`
		MainAncestorExit int    `json:"main_ancestor_exit"`
	}
	observations := []observation{}
	observe := func(label string, wantObject, wantAncestor int) {
		clone := filepath.Join(h.root, "fresh-"+label)
		h.must(h.root, "git", "clone", "--mirror", "file://"+filepath.ToSlash(remote), clone)
		refs := h.must(clone, "git", "show-ref")
		_, _, objectRC := h.run(clone, nil, "git", "cat-file", "-e", pin+"^{commit}")
		ancestorRC := -1
		if objectRC == 0 {
			_, _, ancestorRC = h.run(clone, nil, "git", "merge-base", "--is-ancestor", pin, "refs/heads/main")
		}
		if (objectRC == 0) != (wantObject == 0) || ancestorRC != wantAncestor {
			t.Fatalf("HARNESS_NOT_EXECUTED: remote fixture %s has object=%d ancestor=%d; want object=%d ancestor=%d", label, objectRC, ancestorRC, wantObject, wantAncestor)
		}
		observations = append(observations, observation{label, refs, objectRC, ancestorRC})
	}
	observe("local-only", 1, -1)
	beforeBranch := h.must(repo, "git", "ls-remote", "--refs", "origin")
	h.must(repo, "git", "push", "origin", pin+":refs/heads/feature")
	afterBranch := h.must(repo, "git", "ls-remote", "--refs", "origin")
	if afterBranch != pin+"\trefs/heads/feature\n"+beforeBranch {
		t.Fatalf("HARNESS_NOT_EXECUTED: branch-only remote delta:\nbefore=%s\nafter=%s", beforeBranch, afterBranch)
	}
	observe("branch-only", 0, 1)
	h.must(repo, "git", "push", "origin", pin+":refs/heads/main")
	afterMain := h.must(repo, "git", "ls-remote", "--refs", "origin")
	want := strings.Replace(afterBranch, base+"\trefs/heads/main", pin+"\trefs/heads/main", 1)
	if afterMain != want {
		t.Fatalf("HARNESS_NOT_EXECUTED: main delta: got=%s want=%s", afterMain, want)
	}
	observe("main-ancestor", 0, 0)
	raw, err := json.MarshalIndent(map[string]any{"base": base, "pin": pin, "observations": observations, "source": "actual fresh local-bare clones", "classification": "HARNESS_PREREQUISITE_ONLY", "pin_gate_verdict": "NOT_EXECUTED"}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	h.save("pin-prerequisites.json", append(raw, '\n'))
	t.Logf("HARNESS_PREREQUISITES: fresh origin census 3; local-only pin absent; branch-only reachable but not main ancestor; single main update makes ancestor; no product pin verdict")
}

type rsPinFixture struct {
	h                                            *rsHarness
	product, revision, pseudo, pin, nestedPseudo string
	states                                       map[string]string
	manifest                                     map[string]any
}

func rsPreparePins(t *testing.T) rsPinFixture {
	h := newRSHarness(t)
	modPath := "github.com/PRO-Robotech/corelib"
	owner := h.initRepo("pins-owner", map[string]string{"go.mod": "module " + modPath + "\n\ngo 1.21\n", "lib/lib.go": "package lib\nconst Value=1\n"})
	h.must(owner, "git", "tag", "v1.0.0")
	remote := filepath.Join(h.root, "owner-origin.git")
	h.must(h.root, "git", "clone", "--bare", "--no-local", owner, remote)
	h.must(owner, "git", "remote", "add", "origin", "file://"+filepath.ToSlash(remote))
	h.must(owner, "git", "switch", "--quiet", "-c", "feature")
	h.put(owner, "lib/lib.go", "package lib\nconst Value=2\n")
	h.must(owner, "git", "add", "lib/lib.go")
	h.must(owner, "git", "commit", "--quiet", "-m", "pin feature")
	pin := h.must(owner, "git", "rev-parse", "HEAD")
	seconds, e := strconv.ParseInt(h.must(owner, "git", "show", "-s", "--format=%ct", pin), 10, 64)
	if e != nil {
		t.Fatal(e)
	}
	pseudo := module.PseudoVersion("v1", "v1.0.0", time.Unix(seconds, 0).UTC(), pin[:12])
	states := map[string]string{}
	snapshot := func(label string) {
		dest := filepath.Join(h.root, "origin-"+label+".git")
		h.must(h.root, "git", "clone", "--mirror", "file://"+filepath.ToSlash(remote), dest)
		states[label] = dest
		h.save("origin-"+label+".refs", []byte(h.must(dest, "git", "show-ref")+"\n"))
	}
	snapshot("local-only")
	h.must(owner, "git", "push", "origin", pin+":refs/heads/feature")
	snapshot("branch-only")
	h.must(owner, "git", "push", "origin", pin+":refs/heads/main")
	snapshot("main")
	// A second tracked product module requires a later pin. A successful
	// first requirement must not hide the unavailable second requirement.
	h.put(owner, "lib/lib.go", "package lib\nconst Value=3\n")
	h.must(owner, "git", "add", "lib/lib.go")
	h.must(owner, "git", "commit", "--quiet", "-m", "second product module pin")
	nestedPin := h.must(owner, "git", "rev-parse", "HEAD")
	nestedSeconds, err := strconv.ParseInt(h.must(owner, "git", "show", "-s", "--format=%ct", nestedPin), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	nestedPseudo := module.PseudoVersion("v1", "v1.0.0", time.Unix(nestedSeconds, 0).UTC(), nestedPin[:12])
	h.must(owner, "git", "push", "origin", nestedPin+":refs/heads/feature")
	snapshot("two-reachable")
	for label, expected := range map[string]bool{"main": false, "two-reachable": true} {
		_, _, code := h.run(states[label], nil, "git", "cat-file", "-e", nestedPin+"^{commit}")
		if (code == 0) != expected {
			t.Fatalf("HARNESS_NOT_EXECUTED: second pin reachability %s rc%d", label, code)
		}
	}
	_, _, rc := h.run(states["local-only"], nil, "git", "cat-file", "-e", pin+"^{commit}")
	if rc == 0 {
		t.Fatal("HARNESS_NOT_EXECUTED: absent pin unexpectedly in first remote")
	}
	for label, want := range map[string]int{"branch-only": 1, "main": 0} {
		_, _, got := h.run(states[label], nil, "git", "merge-base", "--is-ancestor", pin, "refs/heads/main")
		if got != want {
			t.Fatalf("HARNESS_NOT_EXECUTED: %s ancestry=%d want%d", label, got, want)
		}
	}
	product := h.initRepo("pins-product", map[string]string{
		"go.mod":           "module github.com/PRO-Robotech/kacho\n\ngo 1.21\n\nrequire " + modPath + " " + pseudo + "\n",
		"main.go":          "package main\nimport _ \"" + modPath + "/lib\"\nfunc main(){}\n",
		"nested/go.mod":    "module github.com/PRO-Robotech/kacho/nested\n\ngo 1.21\n",
		"nested/nested.go": "package nested\nconst Value=1\n",
	})
	h.must(product, "git", "remote", "add", "origin", "https://github.com/PRO-Robotech/kacho.git")
	revision := h.must(product, "git", "rev-parse", "HEAD")
	// A same-machine/cross-repository object must not stand in for owner origin.
	h.must(product, "git", "fetch", "--quiet", "--no-tags", owner, pin)
	h.must(product, "git", "cat-file", "-e", pin+"^{commit}")
	h.must(product, h.goBin, "mod", "edit", "-json")
	manifest := map[string]any{"schema_version": 1, "product_trees": []any{map[string]any{"repository": "PRO-Robotech/kacho", "root": product, "revision": revision, "module_roots": []string{".", "nested"}}}, "internal_modules": []any{map[string]any{"module_path": modPath, "repository": "PRO-Robotech/corelib"}}, "budgets": map[string]any{"network_seconds": 2, "checks_seconds": 5}}
	return rsPinFixture{h, product, revision, pseudo, pin, nestedPseudo, states, manifest}
}

type rsPinCase struct {
	Name, Scenario, Axis, Outcome, Reason, Remote string
	Final                                         bool
	Modules, Requirements, Pseudo                 int
	Manifest                                      map[string]any
}

func rsPinCases(f rsPinFixture) []rsPinCase {
	h := f.h
	cases := []rsPinCase{
		{"origin-lawful-branch", "CI-RS-14", "none", "GREEN", "OK", f.states["branch-only"], false, 2, 1, 1, rsCloneJSON(f.manifest)},
		{"origin-unreachable", "CI-RS-14", "only owner remote feature ref is absent", "RED", "PIN_UNREACHABLE", f.states["local-only"], false, 2, 1, 1, rsCloneJSON(f.manifest)},
		{"final-main-lawful", "CI-RS-15", "none", "GREEN", "OK", f.states["main"], true, 2, 1, 1, rsCloneJSON(f.manifest)},
		{"final-main-branch-only", "CI-RS-15", "only owner main ref remains before pin", "RED", "PIN_NOT_ON_MAIN", f.states["branch-only"], true, 2, 1, 1, rsCloneJSON(f.manifest)},
	}
	changedRevision := func(label string, change func(string)) (string, string) {
		copyRoot := filepath.Join(h.root, "product-"+label)
		h.must(h.root, "git", "clone", "--quiet", "--no-local", f.product, copyRoot)
		h.must(copyRoot, "git", "checkout", "--quiet", f.revision)
		h.must(copyRoot, "git", "config", "user.name", "CI-RS fixture")
		h.must(copyRoot, "git", "config", "user.email", "ci-rs@invalid")
		h.must(copyRoot, "git", "remote", "set-url", "origin", "https://github.com/PRO-Robotech/kacho.git")
		change(copyRoot)
		h.must(copyRoot, "git", "add", "--all")
		h.must(copyRoot, "git", "commit", "--quiet", "-m", label)
		sha := h.must(copyRoot, "git", "rev-parse", "HEAD")
		delta := h.must(copyRoot, "git", "diff", "--name-only", f.revision, sha)
		if delta != "go.mod" {
			t := h.t
			t.Fatalf("HARNESS_NOT_EXECUTED: %s changes non-single go.mod path: %q", label, delta)
		}
		h.save(label+".source.diff", []byte(h.must(copyRoot, "git", "diff", f.revision, sha)+"\n"))
		return copyRoot, sha
	}
	productBinding := func(m map[string]any, root, revision string) {
		d := m["product_trees"].([]any)[0].(map[string]any)
		d["root"] = root
		d["revision"] = revision
	}
	root, revision := changedRevision("ordinary-semver", func(root string) {
		raw := string(mustRSRead(h.t, filepath.Join(root, "go.mod")))
		if strings.Count(raw, f.pseudo) != 1 {
			h.t.Fatal("HARNESS_NOT_EXECUTED: pseudo replacement not exact")
		}
		h.put(root, "go.mod", strings.Replace(raw, f.pseudo, "v1.0.0", 1))
	})
	m := rsCloneJSON(f.manifest)
	productBinding(m, root, revision)
	cases = append(cases, rsPinCase{"ordinary-semver-zero-pseudo", "CI-RS-16", "only required version pseudo -> ordinary semver", "GREEN", "OK", f.states["branch-only"], true, 2, 1, 0, m})
	root, revision = changedRevision("zero-internal", func(root string) {
		raw := string(mustRSRead(h.t, filepath.Join(root, "go.mod")))
		line := "require github.com/PRO-Robotech/corelib " + f.pseudo + "\n"
		if strings.Count(raw, line) != 1 {
			h.t.Fatal("HARNESS_NOT_EXECUTED: required line missing")
		}
		h.put(root, "go.mod", strings.Replace(raw, line, "", 1))
	})
	m = rsCloneJSON(f.manifest)
	productBinding(m, root, revision)
	cases = append(cases, rsPinCase{"zero-internal", "CI-RS-17", "remove only internal require line", "RED", "INTERNAL_CENSUS_EMPTY", f.states["branch-only"], false, 2, 0, 0, m})
	m = rsCloneJSON(f.manifest)
	m["product_trees"] = []any{}
	cases = append(cases, rsPinCase{"zero-product-trees", "CI-RS-17", "product_trees -> []", "RED", "MODULE_CENSUS_EMPTY", f.states["branch-only"], false, 0, 0, 0, m})
	noModule := h.initRepo("product-without-modules", map[string]string{"README.md": "tracked product scope without Go modules\n"})
	h.must(noModule, "git", "remote", "add", "origin", "https://github.com/PRO-Robotech/kacho.git")
	noModuleSHA := h.must(noModule, "git", "rev-parse", "HEAD")
	m = rsCloneJSON(f.manifest)
	m["product_trees"] = []any{map[string]any{"repository": "PRO-Robotech/kacho", "root": noModule, "revision": noModuleSHA, "module_roots": []string{}}}
	cases = append(cases, rsPinCase{"nonempty-tree-zero-tracked-modules", "CI-RS-17", "nonempty tracked source scope has no go.mod", "RED", "MODULE_CENSUS_EMPTY", f.states["branch-only"], false, 0, 0, 0, m})
	m = rsCloneJSON(f.manifest)
	m["internal_modules"] = []any{}
	cases = append(cases, rsPinCase{"missing-owner-mapping", "CI-RS-18", "internal_modules -> []", "RED", "MODULE_MAPPING_INVALID", f.states["branch-only"], false, -1, -1, -1, m})
	cases = append(cases, rsPinCase{"owner-history-unavailable", "CI-RS-18", "only mapped owner transport unavailable", "NOT_EXECUTED", "SOURCE_UNAVAILABLE", filepath.Join(h.root, "absent-owner.git"), false, -1, -1, -1, rsCloneJSON(f.manifest)})
	m = rsCloneJSON(f.manifest)
	m["product_trees"].([]any)[0].(map[string]any)["module_roots"] = []string{"."}
	cases = append(cases, rsPinCase{"omitted-tracked-nested-module", "CI-RS-17", "omit only tracked nested go.mod root", "RED", "INPUT_INVALID", f.states["branch-only"], false, -1, -1, -1, m})
	root, revision = changedRevision("malformed-gomod", func(root string) {
		h.put(root, "go.mod", string(mustRSRead(h.t, filepath.Join(root, "go.mod")))+"require (\n")
	})
	m = rsCloneJSON(f.manifest)
	productBinding(m, root, revision)
	cases = append(cases, rsPinCase{"malformed-tracked-gomod", "CI-RS-17", "append one unterminated require group", "RED", "INPUT_INVALID", f.states["branch-only"], false, -1, -1, -1, m})
	bothRoot := filepath.Join(h.root, "two-required-product-modules")
	h.must(h.root, "git", "clone", "--quiet", "--no-local", f.product, bothRoot)
	h.must(bothRoot, "git", "remote", "set-url", "origin", "https://github.com/PRO-Robotech/kacho.git")
	h.must(bothRoot, "git", "config", "user.name", "CI-RS fixture")
	h.must(bothRoot, "git", "config", "user.email", "ci-rs@invalid")
	nestedMod := string(mustRSRead(h.t, filepath.Join(bothRoot, "nested/go.mod")))
	h.put(bothRoot, "nested/go.mod", nestedMod+"\nrequire github.com/PRO-Robotech/corelib "+f.nestedPseudo+"\n")
	h.must(bothRoot, "git", "add", "nested/go.mod")
	h.must(bothRoot, "git", "commit", "--quiet", "-m", "second tracked module has distinct internal pin")
	bothSHA := h.must(bothRoot, "git", "rev-parse", "HEAD")
	if delta := h.must(bothRoot, "git", "diff", "--name-only", f.revision, bothSHA); delta != "nested/go.mod" {
		h.t.Fatalf("HARNESS_NOT_EXECUTED: second module fixture changes%q", delta)
	}
	h.save("two-required-modules.source.diff", []byte(h.must(bothRoot, "git", "diff", f.revision, bothSHA)+"\n"))
	m = rsCloneJSON(f.manifest)
	productBinding(m, bothRoot, bothSHA)
	cases = append(cases, rsPinCase{"two-modules-two-reachable-pins", "CI-RS-14/17", "none: both tracked modules and both pin requirements reachable", "GREEN", "OK", f.states["two-reachable"], false, 2, 2, 2, m})
	cases = append(cases, rsPinCase{"second-module-pin-unreachable", "CI-RS-14/17", "only owner feature ref does not yet reach the second module pin", "RED", "PIN_UNREACHABLE", f.states["main"], false, 2, 2, 2, rsCloneJSON(m)})
	return cases
}

// rsInvokePreflightBridge uses the frozen thin bridge, never a policy stand-in.
// The entrypoint is the same for consumers and pins; the mode is an argument.
func rsInvokePreflightBridge(h *rsHarness, binary, label string, args []string, remotes map[string]string) (map[string]any, int) {
	h.t.Helper()
	outDir := filepath.Join(h.root, "bridge-"+label)
	if err := os.MkdirAll(outDir, 0755); err != nil {
		h.t.Fatal(err)
	}
	request := map[string]any{"args": args, "output": outDir, "remotes": remotes}
	b, _ := json.Marshal(request)
	requestPath := filepath.Join(h.root, label+".request.json")
	h.put(h.root, label+".request.json", string(b))
	_, stderr, rc := h.run(h.root, []string{"CI_RS_BRIDGE_REQUEST=" + requestPath, "GOPROXY=off", "GOSUMDB=off"}, binary, "-test.run=^TestCIRSConsumerBridge$", "-test.v", "-test.timeout=115s")
	if rc != 0 {
		h.t.Fatalf("HARNESS_NOT_EXECUTED: bridge rc%d: %s", rc, stderr)
	}
	metaBytes := mustRSRead(h.t, filepath.Join(outDir, "bridge-result.json"))
	var meta struct {
		Exit      int      `json:"exit_code"`
		Unhandled []string `json:"unhandled_boundaries"`
		Deadline  bool     `json:"deadline_exceeded"`
	}
	if err := json.Unmarshal(metaBytes, &meta); err != nil || len(meta.Unhandled) > 0 || meta.Deadline {
		h.t.Fatalf("HARNESS_NOT_EXECUTED: bridge result: %v %+v", err, meta)
	}
	entries, err := os.ReadDir(outDir)
	if err != nil {
		h.t.Fatal(err)
	}
	for _, entry := range entries {
		b := mustRSRead(h.t, filepath.Join(outDir, entry.Name()))
		h.save(entry.Name(), b)
		if strings.HasPrefix(entry.Name(), "command-") && strings.HasSuffix(entry.Name(), ".json") {
			var cmd struct {
				Program string   `json:"program"`
				Args    []string `json:"args"`
			}
			if err := json.Unmarshal(b, &cmd); err != nil {
				h.t.Fatal(err)
			}
			if filepath.Base(cmd.Program) == "git" {
				for _, arg := range cmd.Args {
					if arg == "push" {
						h.t.Errorf("SEMANTIC_MISMATCH: pins/preflight attempted remote write")
					}
				}
			}
		}
	}
	return rsValidateResult(h, mustRSRead(h.t, filepath.Join(outDir, "sut.stdout")), label), meta.Exit
}

func rsRunPinCases(t *testing.T) {
	f := rsPreparePins(t)
	h := f.h
	cases := rsPinCases(f)
	ledger := []map[string]any{}
	for _, c := range cases {
		b, _ := json.MarshalIndent(c.Manifest, "", "  ")
		h.save(c.Name+".manifest.json", append(b, '\n'))
		diff := rsChangedJSON(rsCloneJSON(f.manifest), rsCloneJSON(c.Manifest), "$")
		ledger = append(ledger, map[string]any{"name": c.Name, "scenario": c.Scenario, "changed_fact": c.Axis, "computed_manifest_fields": diff, "remote_fixture": c.Remote, "final_main": c.Final, "expected_outcome": c.Outcome, "expected_reason": c.Reason, "manifest_sha256": rsSHA(append(b, '\n')), "sut_invocations": 0})
	}
	b, _ := json.MarshalIndent(ledger, "", "  ")
	h.save("pin-case-ledger.json", append(b, '\n'))
	h.must(h.root, "python3", "-c", "import jsonschema; print(jsonschema.Draft202012Validator.__name__)")
	missing := rsSupplySymbols(h, []string{"RunSupplyPreflight", "SupplyDependencies", "SupplyCommand", "SupplyCommandResult"})
	if len(missing) > 0 {
		b, _ := json.MarshalIndent(map[string]any{"holder_outcome": "CAPABILITY_ABSENT", "boundary": "supply-entrypoints", "missing_symbols": missing, "prepared_cases": len(cases), "sut_invocations": 0, "sut_semantic_decisions": 0}, "", "  ")
		h.save("capability.json", append(b, '\n'))
		t.Fatalf("CAPABILITY_ABSENT: %d real prepared pin cases, missing %v; sut_invocations=0", len(cases), missing)
	}
	binary := rsBuildConsumerBridge(h)
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			ch := *h
			ch.t = t
			ch.serial = 0
			if h.capture != "" {
				ch.capture = filepath.Join(h.capture, c.Name)
				if err := os.MkdirAll(ch.capture, 0755); err != nil {
					t.Fatal(err)
				}
			}
			manifestPath := filepath.Join(h.root, c.Name+".manifest.json")
			raw, _ := json.Marshal(c.Manifest)
			ch.put(h.root, c.Name+".manifest.json", string(raw))
			args := []string{"--mode", "pins", "--manifest", manifestPath}
			if c.Final {
				args = append(args, "--final-main")
			}
			result, rc := rsInvokePreflightBridge(&ch, binary, c.Name, args, map[string]string{"https://github.com/PRO-Robotech/corelib.git": "file://" + filepath.ToSlash(c.Remote)})
			wantRC := map[string]int{"GREEN": 0, "RED": 1, "NOT_EXECUTED": 3}[c.Outcome]
			if rc != wantRC || result["exit_code"] != float64(wantRC) || result["outcome"] != c.Outcome || result["reason"] != c.Reason {
				t.Errorf("SEMANTIC_MISMATCH: want %s/%s/%d, actual exit%d %+v", c.Outcome, c.Reason, wantRC, rc, result)
			}
			if result["phase"] != "pins" || result["stage"] != "NONE" || len(result["effects"].([]any)) != 0 {
				t.Errorf("SEMANTIC_MISMATCH: pins phase/stage/effects %+v", result)
			}
			census := result["census"].(map[string]any)
			for key, want := range map[string]int{"modules": c.Modules, "internal_requirements": c.Requirements, "pseudo_versions": c.Pseudo} {
				if want >= 0 && census[key] != float64(want) {
					t.Errorf("SEMANTIC_MISMATCH: %s=%v want%d", key, census[key], want)
				}
			}
			seen := map[string]bool{}
			for _, raw := range result["checks"].([]any) {
				check := raw.(map[string]any)
				p := check["predicate"].(string)
				if seen[p] {
					t.Errorf("SEMANTIC_MISMATCH: duplicate predicate %s", p)
				}
				seen[p] = true
				if c.Outcome == "GREEN" && check["outcome"] != "GREEN" {
					t.Errorf("SEMANTIC_MISMATCH: GREEN hides non-GREEN check")
				}
			}
			if c.Outcome == "GREEN" {
				wanted := []string{"invocation", "identity", "input", "pins-origin"}
				if c.Final {
					wanted = append(wanted, "pins-main")
				}
				if len(seen) != len(wanted) {
					t.Errorf("SEMANTIC_MISMATCH: predicate set %v", seen)
				}
				for _, p := range wanted {
					if !seen[p] {
						t.Errorf("SEMANTIC_MISMATCH: missing %s", p)
					}
				}
			}
		})
	}
}

func TestInternalPinsReachability(t *testing.T) {
	t.Run("real_origin_prerequisites", func(t *testing.T) { rsRemotePinPrerequisites(t) })
	t.Run("internal_pin_contract", func(t *testing.T) { rsRunPinCases(t) })
}
