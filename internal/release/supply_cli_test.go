// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package release

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Root routine readback: issue2588/comment5719922066. The closed parser data
// below is the exact33-row independent plan, not observations of source output.
type rsCLICase struct {
	ID      string   `json:"id"`
	Entry   string   `json:"entry"`
	Args    []string `json:"argv"`
	Route   string   `json:"route"`
	Outcome string   `json:"outcome"`
	Reason  string   `json:"reason"`
	Exit    int      `json:"exit_code"`
	Note    string   `json:"note"`
}

func rsCLIChild(h *rsHarness, t *testing.T, label string) *rsHarness {
	ch := *h
	ch.t = t
	ch.serial = 0
	if h.capture != "" {
		ch.capture = filepath.Join(h.capture, label)
		if e := os.MkdirAll(ch.capture, 0755); e != nil {
			t.Fatal(e)
		}
	}
	return &ch
}

func rsCLIPrerequisites(h *rsHarness) {
	h.t.Helper()
	h.must(h.root, "python3", "-c", "import jsonschema;print(jsonschema.Draft202012Validator.__name__)")
	// A real native process status control. This program contains no SUT policy,
	// schema or result; its only purpose is proving launcher status transport.
	dir := filepath.Join(h.root, "exit-control")
	h.put(dir, "main.go", "package main\nimport(\"os\";\"strconv\")\nfunc main(){n,e:=strconv.Atoi(os.Args[1]);if e!=nil{panic(e)};os.Exit(n)}\n")
	binary := filepath.Join(dir, "exit-control")
	h.must(dir, h.goBin, "build", "-mod=readonly", "-o", binary, "main.go")
	for _, want := range []int{0, 1, 2, 3} {
		out, errout, rc := h.run(dir, nil, binary, fmt.Sprint(want))
		if rc != want || len(out) != 0 || len(errout) != 0 {
			h.t.Fatalf("HARNESS_NOT_EXECUTED: actual binary rc%d want%d", rc, want)
		}
	}
	for _, n := range []int{2, 3} {
		_, _, rc := h.run(dir, nil, h.goBin, "run", "main.go", fmt.Sprint(n))
		if rc != 1 {
			h.t.Fatalf("HARNESS_NOT_EXECUTED: go run launch control rc%d", rc)
		}
	}
	h.save("exit-transport.json", rsCanonicalJSON(h.t, map[string]any{"scope": "prerequisite only, not release SUT", "actual_binary_sha256": rsSHA(mustRSRead(h.t, binary)), "native_codes": []int{0, 1, 2, 3}, "go_run_codes_for_2_3": []int{1, 1}}))
}

func rsCLIGitAdapter(h *rsHarness, root string) string {
	h.t.Helper()
	dir := filepath.Join(h.root, "caller-git-bin")
	capture := filepath.Join(h.root, "caller-git-captures")
	script := filepath.Join(moduleRoot(h.t), "scripts/release/release-callers-inject.sh")
	h.must(h.root, "bash", script, "--git-adapter", dir, root, capture)
	h.t.Cleanup(func() { rsCopyCaptures(h, capture, "caller-git") })
	return dir
}

func rsCLIGitControls(h *rsHarness, adapter string) {
	h.t.Helper()
	repo := h.initRepo("caller-git-control", map[string]string{"go.mod": "module github.com/PRO-Robotech/kacho\n\ngo 1.21\n", "value.go": "package fixture\nconst Value=1\n"})
	origin := filepath.Join(h.root, "caller-control-origin.git")
	h.must(h.root, "git", "clone", "--bare", "--no-local", repo, origin)
	canonical := "https://github.com/PRO-Robotech/kacho.git"
	h.must(repo, "git", "remote", "add", "origin", canonical)
	h.must(repo, "git", "config", "ciRsFixture.origin", origin)
	binary := filepath.Join(adapter, "git")
	for _, args := range [][]string{{"remote", "get-url", "origin"}, {"-c", "fixture.value=fetch", "remote", "get-url", "origin"}, {"-C", repo, "remote", "get-url", "origin"}} {
		out, errout, rc := h.run(repo, nil, binary, args...)
		if rc != 0 || strings.TrimSpace(string(out)) != canonical {
			h.t.Fatalf("HARNESS_NOT_EXECUTED: canonical identity changed: %s %s", out, errout)
		}
	}
	expected := h.must(repo, "git", "rev-parse", "HEAD") + "\trefs/heads/main"
	for _, args := range [][]string{{"ls-remote", "--refs", "origin"}, {"-C", repo, "ls-remote", "--refs", "origin"}, {"-c", "fixture.value=push", "ls-remote", "--refs", "origin"}} {
		out, errout, rc := h.run(repo, nil, binary, args...)
		if rc != 0 || strings.TrimSpace(string(out)) != expected {
			h.t.Fatalf("HARNESS_NOT_EXECUTED: actual mapped ls-remote: %s %s", out, errout)
		}
	}
	_, _, rc := h.run(repo, nil, binary, "ls-remote", "https://github.com/unowned/forbidden.git")
	if rc == 0 {
		h.t.Fatal("HARNESS_NOT_EXECUTED: undeclared network transport allowed")
	}
	h.save("git-boundary-controls.json", rsCanonicalJSON(h.t, map[string]any{"canonical_identity_controls": 3, "actual_transport_controls": 3, "foreign_refusal_controls": 1, "remote_sha": strings.Split(expected, "\t")[0]}))
}

func rsCLIRequire(h *rsHarness, cases int, paths ...string) {
	h.t.Helper()
	missing := rsSupplySymbols(h, []string{"RunSupplyCLI", "RunSupplyPreflight", "RunSupplyPublisher", "SupplyDependencies"})
	for _, p := range paths {
		if _, e := os.Stat(filepath.Join(moduleRoot(h.t), p)); os.IsNotExist(e) {
			missing = append(missing, "path:"+p)
		} else if e != nil {
			h.t.Fatal(e)
		}
	}
	if len(missing) > 0 {
		h.save("caller-capability.json", rsCanonicalJSON(h.t, map[string]any{"holder_outcome": "CAPABILITY_ABSENT", "missing": missing, "prepared_cases": cases, "sut_invocations": 0, "sut_semantic_decisions": 0, "scope": "actual prerequisites complete; no verdict about unexecuted cases"}))
		h.t.Fatalf("CAPABILITY_ABSENT: caller missing%v; prepared=%d; sut_invocations=0", missing, cases)
	}
}

func rsCLIBuild(h *rsHarness) string {
	h.t.Helper()
	root := moduleRoot(h.t)
	// Build the actual production main without a substituted dependency or main.
	bin := filepath.Join(h.root, "releasepreflight.actual")
	out, errout, rc := h.run(root, []string{"GOWORK=off", "GOTOOLCHAIN=local", "GOFLAGS="}, h.goBin, "build", "-mod=readonly", "-o", bin, "./tools/releasepreflight")
	if rc != 0 {
		h.t.Fatalf("HARNESS_NOT_EXECUTED: actual CLI compile/link rc%d %s %s", rc, out, errout)
	}
	raw := mustRSRead(h.t, bin)
	if len(raw) == 0 {
		h.t.Fatal("HARNESS_NOT_EXECUTED: empty CLI executable")
	}
	h.save("actual-cli-build.json", rsCanonicalJSON(h.t, map[string]any{"binary_sha256": rsSHA(raw), "bytes": len(raw), "command": []string{h.goBin, "build", "-mod=readonly", "-o", bin, "./tools/releasepreflight"}, "cwd": root, "production_main_unchanged": true}))
	return bin
}

func rsCLICheck(h *rsHarness, out []byte, rc int, outcome, reason, phase string) map[string]any {
	h.t.Helper()
	r := rsValidateResult(h, out, "actual-cli")
	want := map[string]int{"GREEN": 0, "RED": 1, "USAGE_ERROR": 2, "NOT_EXECUTED": 3}[outcome]
	if rc != want || r["exit_code"] != float64(want) || r["outcome"] != outcome || r["reason"] != reason {
		h.t.Errorf("SEMANTIC_MISMATCH: actual CLI rc%d result%+v want%s/%s/%d", rc, r, outcome, reason, want)
	}
	if phase != "" && r["phase"] != phase {
		h.t.Errorf("SEMANTIC_MISMATCH: phase=%v want%s", r["phase"], phase)
	}
	if r["stage"] != "NONE" || len(r["effects"].([]any)) != 0 {
		h.t.Errorf("SEMANTIC_MISMATCH: read-only caller remote effects %+v", r)
	}
	return r
}

func TestReleaseSupplyCLI(t *testing.T) {
	f := rsPrepareConsumers(t)
	h := f.h
	rsCLIPrerequisites(h)
	adapter := rsCLIGitAdapter(h, h.root)
	rsCLIGitControls(h, adapter)
	var cases []rsCLICase
	if e := json.Unmarshal([]byte(rsCLIParserCases), &cases); e != nil || len(cases) != 33 {
		t.Fatalf("HARNESS_NOT_EXECUTED: frozen33 parser ledger %v", e)
	}
	h.save("parser-cases.json", []byte(rsCLIParserCases+"\n"))
	h.save("caller-plan-binding.json", rsCanonicalJSON(t, map[string]any{"event": 5719922066, "parser_subject_sha256": "50ada39aae1e104088cbd1192ac32a9620ae9ebce4a6f96536a10611f9ffcce8", "actual_binary_required": true, "SUT_calls_before_capability": 0}))
	rsCLIRequire(h, len(cases), "tools/releasepreflight/main.go", "scripts/release/publish-module-tree.sh", "scripts/release/assert-internal-pins-reachable.sh")
	binary := rsCLIBuild(h)
	declaration := map[string]any{"schema_version": 1, "consumers": f.manifest("candidate-program")["consumers"]}
	decl := filepath.Join(h.root, "declared-consumers.json")
	h.put(h.root, filepath.Base(decl), string(rsCanonicalJSON(t, declaration)))
	manifest := filepath.Join(h.root, "consumers-manifest.json")
	h.put(h.root, filepath.Base(manifest), string(rsCanonicalJSON(t, f.manifest("candidate-program"))))
	replace := map[string]string{"<S>": f.revision, "<D>": decl, "<M>": manifest, "<ready>": f.target, "<missing-manifest>": filepath.Join(h.root, "absent-manifest.json")}
	for _, c := range cases {
		t.Run(c.ID, func(t *testing.T) {
			ch := rsCLIChild(h, t, c.ID)
			args := append([]string{}, c.Args...)
			for i, a := range args {
				if value, ok := replace[a]; ok {
					args[i] = value
				}
			}
			command := binary
			if c.Entry == "actual publish-module-tree.sh" {
				command = "bash"
				args = append([]string{filepath.Join(moduleRoot(t), "scripts/release/publish-module-tree.sh")}, args...)
			}
			if c.Entry == "actual assert-internal-pins-reachable.sh" {
				command = "bash"
				args = append([]string{filepath.Join(moduleRoot(t), "scripts/release/assert-internal-pins-reachable.sh")}, args...)
			}
			before := ch.must(f.target, "git", "show-ref")
			out, _, rc := ch.run(f.target, []string{"GOPROXY=off", "GOSUMDB=off"}, command, args...)
			phase := c.Route
			if phase == "rejected" {
				phase = ""
			}
			r := rsCLICheck(ch, out, rc, c.Outcome, c.Reason, phase)
			if c.Outcome == "GREEN" {
				rsP9CheckCensus(ch, r, f.revision, 5)
			}
			if after := ch.must(f.target, "git", "show-ref"); after != before {
				t.Error("SEMANTIC_MISMATCH: CLI changed refs")
			}
		})
	}
	rsCLIBootstrap(t, h)
}

// These failures execute actual wrappers and actual tool/filesystem failures.
// No stub command fabricates an exit status or a schema result.
func rsCLIBootstrap(t *testing.T, h *rsHarness) {
	for _, kind := range []string{"publisher", "pins"} {
		for _, fault := range []string{"missing-go", "bad-output-dir", "compiler-error"} {
			t.Run(kind+"-bootstrap-"+fault, func(t *testing.T) {
				ch := rsCLIChild(h, t, kind+"-bootstrap-"+fault)
				root := moduleRoot(t)
				file := "scripts/release/publish-module-tree.sh"
				phase := "plan"
				if kind == "pins" {
					file = "scripts/release/assert-internal-pins-reachable.sh"
					phase = "pins"
				}
				args := []string{"--manifest", filepath.Join(h.root, "absent.json")}
				if kind == "publisher" {
					args = []string{"--tree", h.root, "--repo", "PRO-Robotech/corelib", "--version", "v1.1.0", "--manifest", filepath.Join(h.root, "absent.json")}
				}
				env := []string{"GOPROXY=off", "GOSUMDB=off"}
				switch fault {
				case "missing-go":
					path := filepath.Join(h.root, kind+"-without-go")
					if e := os.MkdirAll(path, 0755); e != nil {
						t.Fatal(e)
					}
					for _, name := range []string{"bash", "dirname", "mktemp", "rm", "timeout", "env", "cat", "python3", "git", "date", "sleep", "readlink", "realpath", "sha256sum", "sed", "awk", "head", "tail", "cut", "mkdir", "chmod", "pwd"} {
						p, e := exec.LookPath(name)
						if e == nil {
							if e = os.Symlink(p, filepath.Join(path, name)); e != nil {
								t.Fatal(e)
							}
						}
					}
					env = append(env, "PATH="+path)
				case "bad-output-dir":
					bad := filepath.Join(h.root, kind+"-tmp-regular-file")
					ch.put(h.root, filepath.Base(bad), "not a directory\n")
					env = append(env, "TMPDIR="+bad)
				case "compiler-error":
					clone := filepath.Join(h.root, kind+"-broken-compiler-input")
					ch.must(h.root, "git", "clone", "--quiet", "--depth=1", "file://"+root, clone)
					ch.put(clone, "tools/releasepreflight/main.go", "package main\nfunc main(\n")
					root = clone
				}
				out, _, rc := ch.run(h.root, env, "/bin/bash", append([]string{filepath.Join(root, file)}, args...)...)
				r := rsCLICheck(ch, out, rc, "NOT_EXECUTED", "HARNESS_UNAVAILABLE", phase)
				if r["dry_run"] != true || len(r["snapshots"].([]any)) != 0 || len(r["checks"].([]any)) != 1 || r["checks"].([]any)[0].(map[string]any)["predicate"] != "input" {
					t.Errorf("SEMANTIC_MISMATCH: bootstrap shape %+v", r)
				}
				for _, key := range []string{"repository", "version", "plan_sha256", "input_sha256", "candidate_sha", "tag_target_sha"} {
					if r[key] != nil {
						t.Errorf("SEMANTIC_MISMATCH: bootstrap fabricated %s", key)
					}
				}
				for key, value := range r["census"].(map[string]any) {
					if value != nil {
						t.Errorf("SEMANTIC_MISMATCH: bootstrap census %s=%v", key, value)
					}
				}
			})
		}
	}
}

const rsCLIParserCases = `[
  {
    "id": "CLI-candidate-missing",
    "entry": "actual compiled CLI",
    "argv": [
      "--mode",
      "candidate",
      "--manifest",
      "<missing-manifest>",
      "--revision",
      "<S>"
    ],
    "route": "candidate",
    "outcome": "NOT_EXECUTED",
    "reason": "SOURCE_UNAVAILABLE",
    "exit_code": 3,
    "note": "S is an actual full fixture commit; no launcher rc collapse"
  },
  {
    "id": "CLI-consumers-missing",
    "entry": "actual compiled CLI",
    "argv": [
      "--mode",
      "consumers",
      "--manifest",
      "<missing-manifest>",
      "--revision",
      "<S>"
    ],
    "route": "consumers",
    "outcome": "NOT_EXECUTED",
    "reason": "SOURCE_UNAVAILABLE",
    "exit_code": 3,
    "note": "S is an actual full fixture commit; no launcher rc collapse"
  },
  {
    "id": "CLI-pins-missing",
    "entry": "actual compiled CLI",
    "argv": [
      "--mode",
      "pins",
      "--manifest",
      "<missing-manifest>"
    ],
    "route": "pins",
    "outcome": "NOT_EXECUTED",
    "reason": "SOURCE_UNAVAILABLE",
    "exit_code": 3,
    "note": "S is an actual full fixture commit; no launcher rc collapse"
  },
  {
    "id": "CLI-publisher-default-missing",
    "entry": "actual compiled CLI",
    "argv": [
      "--tree",
      "<ready>",
      "--repo",
      "PRO-Robotech/corelib",
      "--version",
      "v1.1.0",
      "--manifest",
      "<missing-manifest>"
    ],
    "route": "plan",
    "outcome": "NOT_EXECUTED",
    "reason": "SOURCE_UNAVAILABLE",
    "exit_code": 3,
    "note": "Valid local ready locator; no --phase or --commit; effects remain empty"
  },
  {
    "id": "CLI-empty",
    "entry": "actual compiled CLI",
    "argv": [],
    "route": "rejected",
    "outcome": "USAGE_ERROR",
    "reason": "INVALID_INVOCATION",
    "exit_code": 2,
    "note": ""
  },
  {
    "id": "CLI-unknown-mode",
    "entry": "actual compiled CLI",
    "argv": [
      "--mode",
      "unknown",
      "--manifest",
      "<M>"
    ],
    "route": "rejected",
    "outcome": "USAGE_ERROR",
    "reason": "INVALID_INVOCATION",
    "exit_code": 2,
    "note": ""
  },
  {
    "id": "CLI-missing-mode-value",
    "entry": "actual compiled CLI",
    "argv": [
      "--mode"
    ],
    "route": "rejected",
    "outcome": "USAGE_ERROR",
    "reason": "INVALID_INVOCATION",
    "exit_code": 2,
    "note": ""
  },
  {
    "id": "CLI-duplicate-mode",
    "entry": "actual compiled CLI",
    "argv": [
      "--mode",
      "consumers",
      "--mode",
      "consumers",
      "--manifest",
      "<M>",
      "--revision",
      "<S>"
    ],
    "route": "rejected",
    "outcome": "USAGE_ERROR",
    "reason": "INVALID_INVOCATION",
    "exit_code": 2,
    "note": ""
  },
  {
    "id": "CLI-missing-revision",
    "entry": "actual compiled CLI",
    "argv": [
      "--mode",
      "consumers",
      "--manifest",
      "<M>"
    ],
    "route": "rejected",
    "outcome": "USAGE_ERROR",
    "reason": "INVALID_INVOCATION",
    "exit_code": 2,
    "note": ""
  },
  {
    "id": "CLI-malformed-revision",
    "entry": "actual compiled CLI",
    "argv": [
      "--mode",
      "candidate",
      "--manifest",
      "<M>",
      "--revision",
      "abc"
    ],
    "route": "rejected",
    "outcome": "USAGE_ERROR",
    "reason": "INVALID_INVOCATION",
    "exit_code": 2,
    "note": ""
  },
  {
    "id": "CLI-pins-revision",
    "entry": "actual compiled CLI",
    "argv": [
      "--mode",
      "pins",
      "--manifest",
      "<M>",
      "--revision",
      "<S>"
    ],
    "route": "rejected",
    "outcome": "USAGE_ERROR",
    "reason": "INVALID_INVOCATION",
    "exit_code": 2,
    "note": ""
  },
  {
    "id": "CLI-pins-final-duplicate",
    "entry": "actual compiled CLI",
    "argv": [
      "--mode",
      "pins",
      "--manifest",
      "<M>",
      "--final-main",
      "--final-main"
    ],
    "route": "rejected",
    "outcome": "USAGE_ERROR",
    "reason": "INVALID_INVOCATION",
    "exit_code": 2,
    "note": ""
  },
  {
    "id": "CLI-consumer-final-main",
    "entry": "actual compiled CLI",
    "argv": [
      "--mode",
      "consumers",
      "--manifest",
      "<M>",
      "--revision",
      "<S>",
      "--final-main"
    ],
    "route": "rejected",
    "outcome": "USAGE_ERROR",
    "reason": "INVALID_INVOCATION",
    "exit_code": 2,
    "note": ""
  },
  {
    "id": "CLI-mixed-mode-phase",
    "entry": "actual compiled CLI",
    "argv": [
      "--mode",
      "pins",
      "--manifest",
      "<M>",
      "--phase",
      "plan"
    ],
    "route": "rejected",
    "outcome": "USAGE_ERROR",
    "reason": "INVALID_INVOCATION",
    "exit_code": 2,
    "note": ""
  },
  {
    "id": "CLI-unknown-flag",
    "entry": "actual compiled CLI",
    "argv": [
      "--unexpected"
    ],
    "route": "rejected",
    "outcome": "USAGE_ERROR",
    "reason": "INVALID_INVOCATION",
    "exit_code": 2,
    "note": ""
  },
  {
    "id": "CLI-publisher-unexpected-positional",
    "entry": "actual compiled CLI",
    "argv": [
      "extra"
    ],
    "route": "rejected",
    "outcome": "USAGE_ERROR",
    "reason": "INVALID_INVOCATION",
    "exit_code": 2,
    "note": ""
  },
  {
    "id": "CLI-bare-commit",
    "entry": "actual compiled CLI",
    "argv": [
      "--commit"
    ],
    "route": "rejected",
    "outcome": "USAGE_ERROR",
    "reason": "INVALID_INVOCATION",
    "exit_code": 2,
    "note": ""
  },
  {
    "id": "P9-route-exact",
    "entry": "actual compiled CLI",
    "argv": [
      "v1.1.0",
      "--consumers",
      "<D>",
      "--revision",
      "<S>"
    ],
    "route": "consumers",
    "outcome": "GREEN",
    "reason": "OK",
    "exit_code": 0,
    "note": "Lawful hermetic candidate and tracked canonical program; no external write"
  },
  {
    "id": "P9-route-named-order",
    "entry": "actual compiled CLI",
    "argv": [
      "v1.1.0",
      "--revision",
      "<S>",
      "--consumers",
      "<D>"
    ],
    "route": "consumers",
    "outcome": "GREEN",
    "reason": "OK",
    "exit_code": 0,
    "note": "Lawful hermetic candidate and tracked canonical program; no external write"
  },
  {
    "id": "P9-route-no-revision",
    "entry": "actual compiled CLI",
    "argv": [
      "v1.1.0",
      "--consumers",
      "<D>"
    ],
    "route": "rejected",
    "outcome": "USAGE_ERROR",
    "reason": "INVALID_INVOCATION",
    "exit_code": 2,
    "note": ""
  },
  {
    "id": "P9-route-no-consumers",
    "entry": "actual compiled CLI",
    "argv": [
      "v1.1.0",
      "--revision",
      "<S>"
    ],
    "route": "rejected",
    "outcome": "USAGE_ERROR",
    "reason": "INVALID_INVOCATION",
    "exit_code": 2,
    "note": ""
  },
  {
    "id": "P9-route-duplicate-consumers",
    "entry": "actual compiled CLI",
    "argv": [
      "v1.1.0",
      "--consumers",
      "<D>",
      "--consumers",
      "<D>",
      "--revision",
      "<S>"
    ],
    "route": "rejected",
    "outcome": "USAGE_ERROR",
    "reason": "INVALID_INVOCATION",
    "exit_code": 2,
    "note": ""
  },
  {
    "id": "P9-route-duplicate-revision",
    "entry": "actual compiled CLI",
    "argv": [
      "v1.1.0",
      "--consumers",
      "<D>",
      "--revision",
      "<S>",
      "--revision",
      "<S>"
    ],
    "route": "rejected",
    "outcome": "USAGE_ERROR",
    "reason": "INVALID_INVOCATION",
    "exit_code": 2,
    "note": ""
  },
  {
    "id": "P9-route-second-version",
    "entry": "actual compiled CLI",
    "argv": [
      "v1.1.0",
      "v1.1.1",
      "--consumers",
      "<D>",
      "--revision",
      "<S>"
    ],
    "route": "rejected",
    "outcome": "USAGE_ERROR",
    "reason": "INVALID_INVOCATION",
    "exit_code": 2,
    "note": ""
  },
  {
    "id": "P9-route-missing-value",
    "entry": "actual compiled CLI",
    "argv": [
      "v1.1.0",
      "--consumers",
      "--revision",
      "<S>"
    ],
    "route": "rejected",
    "outcome": "USAGE_ERROR",
    "reason": "INVALID_INVOCATION",
    "exit_code": 2,
    "note": ""
  },
  {
    "id": "P9-route-bad-revision",
    "entry": "actual compiled CLI",
    "argv": [
      "v1.1.0",
      "--consumers",
      "<D>",
      "--revision",
      "HEAD"
    ],
    "route": "rejected",
    "outcome": "USAGE_ERROR",
    "reason": "INVALID_INVOCATION",
    "exit_code": 2,
    "note": ""
  },
  {
    "id": "P9-route-mixed-phase",
    "entry": "actual compiled CLI",
    "argv": [
      "v1.1.0",
      "--consumers",
      "<D>",
      "--revision",
      "<S>",
      "--phase",
      "plan"
    ],
    "route": "rejected",
    "outcome": "USAGE_ERROR",
    "reason": "INVALID_INVOCATION",
    "exit_code": 2,
    "note": ""
  },
  {
    "id": "P9-route-mixed-mode",
    "entry": "actual compiled CLI",
    "argv": [
      "v1.1.0",
      "--consumers",
      "<D>",
      "--revision",
      "<S>",
      "--mode",
      "consumers"
    ],
    "route": "rejected",
    "outcome": "USAGE_ERROR",
    "reason": "INVALID_INVOCATION",
    "exit_code": 2,
    "note": ""
  },
  {
    "id": "P9-route-skip-pack-private",
    "entry": "actual compiled CLI",
    "argv": [
      "v1.1.0",
      "--consumers",
      "<D>",
      "--revision",
      "<S>",
      "--skip-pack"
    ],
    "route": "rejected",
    "outcome": "USAGE_ERROR",
    "reason": "INVALID_INVOCATION",
    "exit_code": 2,
    "note": ""
  },
  {
    "id": "publisher-wrapper-foreign-mode",
    "entry": "actual publish-module-tree.sh",
    "argv": [
      "--mode",
      "consumers",
      "--manifest",
      "<M>",
      "--revision",
      "<S>"
    ],
    "route": "rejected",
    "outcome": "USAGE_ERROR",
    "reason": "INVALID_INVOCATION",
    "exit_code": 2,
    "note": "Wrapper must not turn its own forbidden flags/positionals into another successful CLI route"
  },
  {
    "id": "publisher-wrapper-private-p9",
    "entry": "actual publish-module-tree.sh",
    "argv": [
      "v1.1.0",
      "--consumers",
      "<D>",
      "--revision",
      "<S>"
    ],
    "route": "rejected",
    "outcome": "USAGE_ERROR",
    "reason": "INVALID_INVOCATION",
    "exit_code": 2,
    "note": "Wrapper must not turn its own forbidden flags/positionals into another successful CLI route"
  },
  {
    "id": "pins-wrapper-caller-mode",
    "entry": "actual assert-internal-pins-reachable.sh",
    "argv": [
      "--mode",
      "pins",
      "--manifest",
      "<M>"
    ],
    "route": "rejected",
    "outcome": "USAGE_ERROR",
    "reason": "INVALID_INVOCATION",
    "exit_code": 2,
    "note": "Pins wrapper supplies exactly one --mode pins itself; accepts only --manifest and optional --final-main"
  },
  {
    "id": "pins-wrapper-caller-revision",
    "entry": "actual assert-internal-pins-reachable.sh",
    "argv": [
      "--manifest",
      "<M>",
      "--revision",
      "<S>"
    ],
    "route": "rejected",
    "outcome": "USAGE_ERROR",
    "reason": "INVALID_INVOCATION",
    "exit_code": 2,
    "note": "Pins wrapper supplies exactly one --mode pins itself; accepts only --manifest and optional --final-main"
  }
]`
