// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package release

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This additional diagnostic keeps the frozen candidate holder unchanged. It
// exercises real canonical foreign Git blobs and receiving Go archives through
// the existing compiled entrypoint boundary. Authority events are fixture-only;
// no generated DT implementation or live approval is asserted by these bytes.
func TestReleaseSupplyForeignCanonicalProof(t *testing.T) {
	f := rsPrepareCandidate(t)
	h := f.h
	producer := moduleRoot(t)
	commit := f.proofSource["revision"].(string)
	const canonical = "proto/corelib/subscription/subscription.proto"
	const projected = "api/corelib/subscription/subscription.pb.go"
	foreignFiles := rsTrackedInventory(h, producer, commit, func(path string) bool { return path == canonical })
	if len(foreignFiles) != 1 {
		t.Fatal("HARNESS_NOT_EXECUTED: actual canonical proto census")
	}
	foreign := map[string]any{"repository": "PRO-Robotech/kacho", "revision": commit, "files": foreignFiles}
	proto, stderr, rc := h.run(producer, nil, "git", "show", commit+":"+canonical)
	if rc != 0 || len(proto) == 0 || rsSHA(proto) != foreignFiles[0].(map[string]any)["sha256"] {
		t.Fatalf("HARNESS_NOT_EXECUTED: canonical proto bytes: %s", stderr)
	}
	h.save("canonical.proto", proto)
	h.save("canonical-source.json", rsCanonicalJSON(t, foreign))
	absent := strings.Repeat("1", 40)
	_, _, rc = h.run(producer, nil, "git", "cat-file", "-e", absent+"^{commit}")
	if rc == 0 {
		t.Fatal("HARNESS_NOT_EXECUTED: supposed absent foreign commit exists")
	}

	prepare := func(label, path, body string) (string, string, map[string]any, map[string]any) {
		root, revision, m := f.variant(label, func(root string) { h.put(root, path, body) })
		files := rsTrackedInventory(h, root, revision, func(p string) bool { return p == path })
		if len(files) != 1 {
			t.Fatal("HARNESS_NOT_EXECUTED: receiving payload census")
		}
		m["components"] = []string{"CI-DT-1"}
		m["ci_dt_addenda"] = []string{}
		payload := rsCloneJSON(files[0].(map[string]any))
		payload["component"] = "CI-DT-1"
		m["payload"] = []any{payload}
		receiving := map[string]any{"repository": "PRO-Robotech/corelib", "revision": revision, "files": files}
		m["proofs"] = []any{f.proof("DT-P01", foreign), f.proof("DT-P02", receiving), f.proof("DT-P03", receiving)}
		archive := h.archive(root, f.module, label+"-archive")
		present := false
		for _, name := range archive.files {
			if name == path {
				present = true
			}
		}
		if !present {
			t.Fatal("HARNESS_NOT_EXECUTED: receiving payload absent from actual Go ZIP")
		}
		return root, revision, m, receiving
	}
	cases := []rsCandidateCase{}
	add := func(name, axis, outcome, reason, root, revision string, m map[string]any) {
		cases = append(cases, rsCandidateCase{name, "CI-RS-21", axis, outcome, reason, root, revision, f.origin, m, append([]rsHTTPFixture(nil), f.http...)})
	}
	root, revision, lawful, _ := prepare("foreign-canonical", projected, "// CI-RS opaque generated payload fixture.\npackage subscription\nconst Fixture=1\n")
	add("canonical-proto-with-receiving-projection", "none", "GREEN", "OK", root, revision, rsCloneJSON(lawful))
	badSource := rsCloneJSON(foreign)
	badSource["files"].([]any)[0].(map[string]any)["sha256"] = strings.Repeat("0", 64)
	bad := rsCloneJSON(lawful)
	bad["proofs"].([]any)[0] = f.proof("DT-P01", badSource)
	add("canonical-proto-digest-mismatch", "only declared foreign source digest; bound review and authority refreshed", "RED", "PAYLOAD_MISMATCH", root, revision, bad)
	unavailableSource := rsCloneJSON(foreign)
	unavailableSource["revision"] = absent
	unavailable := rsCloneJSON(lawful)
	unavailable["proofs"].([]any)[0] = f.proof("DT-P01", unavailableSource)
	add("canonical-proto-revision-unavailable", "only foreign Git revision absent; bound review and authority refreshed", "NOT_EXECUTED", "SOURCE_UNAVAILABLE", root, revision, unavailable)
	sameRoot, sameRevision, same, receiving := prepare("foreign-same-path", canonical, string(proto))
	if string(rsCanonicalJSON(t, receiving["files"])) != string(rsCanonicalJSON(t, foreign["files"])) {
		t.Fatal("HARNESS_NOT_EXECUTED: same-path ownership twin must have identical path/mode/digest")
	}
	add("same-bytes-receiving-coverage", "none; actual receiving payload identical to actual foreign path/mode/bytes", "GREEN", "OK", sameRoot, sameRevision, rsCloneJSON(same))
	foreignOnly := rsCloneJSON(same)
	foreignOnly["proofs"] = []any{same["proofs"].([]any)[0], f.proof("DT-P02", foreign), f.proof("DT-P03", foreign)}
	add("same-bytes-foreign-only-no-receiving-coverage", "only DT-P02/P03 source ownership and bound authority change; candidate and payload identical", "RED", "PAYLOAD_MISMATCH", sameRoot, sameRevision, foreignOnly)
	ledger := []any{}
	for i, c := range cases {
		twin := cases[0]
		if i >= 3 {
			twin = cases[3]
		}
		h.save(c.Name+".manifest.json", rsCanonicalJSON(t, c.Manifest))
		ledger = append(ledger, map[string]any{"name": c.Name, "scenario": c.Scenario, "changed_fact": c.Axis, "lawful_twin": twin.Name, "expected_outcome": c.Outcome, "expected_reason": c.Reason, "manifest_sha256": rsSHA(rsCanonicalJSON(t, c.Manifest)), "computed_manifest_fields": rsChangedJSON(rsCloneJSON(twin.Manifest), rsCloneJSON(c.Manifest), "$"), "authority_scope": "synthetic fixture only"})
	}
	h.save("foreign-case-ledger.json", rsCanonicalJSON(t, ledger))
	h.must(h.root, "python3", "-c", "import jsonschema; print(jsonschema.Draft202012Validator.__name__)")
	if missing := rsSupplySymbols(h, []string{"RunSupplyPreflight", "SupplyDependencies", "SupplyCommand", "SupplyCommandResult"}); len(missing) > 0 {
		t.Fatalf("CAPABILITY_ABSENT: missing %v; sut_invocations=0", missing)
	}
	binary := rsBuildCandidateBridge(h)
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
			manifestPath := filepath.Join(h.root, c.Name+".manifest.json")
			ch.put(h.root, c.Name+".manifest.json", string(rsCanonicalJSON(t, c.Manifest)))
			outDir := filepath.Join(h.root, "result-"+c.Name)
			if e := os.MkdirAll(outDir, 0755); e != nil {
				t.Fatal(e)
			}
			request := map[string]any{"args": []string{"--mode", "candidate", "--manifest", manifestPath, "--revision", c.Revision}, "output": outDir, "remotes": map[string]string{"https://github.com/PRO-Robotech/corelib.git": "file://" + filepath.ToSlash(c.Origin)}, "http": c.HTTP}
			raw, _ := json.Marshal(request)
			requestPath := filepath.Join(h.root, c.Name+".request.json")
			ch.put(h.root, c.Name+".request.json", string(raw))
			_, stderr, rc := ch.run(h.root, []string{"CI_RS_BRIDGE_REQUEST=" + requestPath, "GOPROXY=off", "GOSUMDB=off"}, binary, "-test.run=^TestCIRSConsumerBridge$", "-test.v", "-test.timeout=115s")
			if rc != 0 {
				t.Fatalf("HARNESS_NOT_EXECUTED: candidate bridge rc%d: %s", rc, stderr)
			}
			var meta struct {
				Exit      int      `json:"exit_code"`
				Unhandled []string `json:"unhandled_boundaries"`
				Deadline  bool     `json:"deadline_exceeded"`
			}
			if e := json.Unmarshal(mustRSRead(t, filepath.Join(outDir, "bridge-result.json")), &meta); e != nil || len(meta.Unhandled) > 0 || meta.Deadline {
				t.Fatalf("HARNESS_NOT_EXECUTED: candidate bridge metadata %v %+v", e, meta)
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
						Program string
						Args    []string
					}
					if e := json.Unmarshal(b, &cmd); e != nil {
						t.Fatal(e)
					}
					if filepath.Base(cmd.Program) == "git" {
						for _, arg := range cmd.Args {
							if arg == "push" {
								t.Error("SEMANTIC_MISMATCH: candidate preflight attempted push")
							}
						}
					}
				}
				if strings.HasPrefix(entry.Name(), "http-") {
					var record map[string]any
					if e := json.Unmarshal(b, &record); e != nil {
						t.Fatal(e)
					}
					if record["method"] != "GET" {
						t.Error("SEMANTIC_MISMATCH: candidate preflight attempted HTTP mutation")
					}
				}
			}
			result := rsValidateResult(&ch, mustRSRead(t, filepath.Join(outDir, "sut.stdout")), c.Name)
			wantRC := map[string]int{"GREEN": 0, "RED": 1, "NOT_EXECUTED": 3}[c.Outcome]
			if meta.Exit != wantRC || result["exit_code"] != float64(wantRC) || result["outcome"] != c.Outcome || result["reason"] != c.Reason {
				t.Errorf("SEMANTIC_MISMATCH: want%s/%s/%d actual exit%d %+v", c.Outcome, c.Reason, wantRC, meta.Exit, result)
			}
			if result["phase"] != "candidate" || result["stage"] != "NONE" || len(result["effects"].([]any)) != 0 {
				t.Errorf("SEMANTIC_MISMATCH: candidate read-only scope %+v", result)
			}
			seen := map[string]bool{}
			for _, raw := range result["checks"].([]any) {
				check := raw.(map[string]any)
				predicate := check["predicate"].(string)
				if seen[predicate] {
					t.Errorf("SEMANTIC_MISMATCH: duplicate predicate%s", predicate)
				}
				seen[predicate] = true
				if c.Outcome == "GREEN" && check["outcome"] != "GREEN" {
					t.Errorf("SEMANTIC_MISMATCH: GREEN hides%s=%v", predicate, check["outcome"])
				}
			}
			for _, raw := range result["checks"].([]any) {
				check := raw.(map[string]any)
				if check["predicate"] != "payload" && check["outcome"] != "GREEN" {
					t.Errorf("SEMANTIC_MISMATCH: foreign proof case confounded by prerequisite %v", check)
				}
			}
			if len(seen) != 9 || !seen["payload"] {
				t.Errorf("SEMANTIC_MISMATCH: expected all 9 candidate predicates, got %v", seen)
			}
			if c.Outcome == "GREEN" {
				want := []string{"invocation", "identity", "input", "ownership", "baseline", "package-floor", "consumer-census", "consumer-archive", "payload"}
				if len(seen) != len(want) {
					t.Errorf("SEMANTIC_MISMATCH: candidate predicate set%v", seen)
				}
				for _, p := range want {
					if !seen[p] {
						t.Errorf("SEMANTIC_MISMATCH: missing candidate predicate%s", p)
					}
				}
				census := result["census"].(map[string]any)
				for _, key := range []string{"input_files", "previous_packages", "candidate_packages", "consumers", "consumer_imports"} {
					n, ok := census[key].(float64)
					if !ok || n <= 0 {
						t.Errorf("SEMANTIC_MISMATCH: lawful census %s=%v", key, census[key])
					}
				}
				if result["candidate_sha"] != c.Revision {
					t.Errorf("SEMANTIC_MISMATCH: wrong candidate source%v", result["candidate_sha"])
				}
			}
		})
	}
}
