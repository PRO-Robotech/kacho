// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package release

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"golang.org/x/mod/module"
	modzip "golang.org/x/mod/zip"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// rsCandidatePrerequisites creates a nonempty previous-version package floor,
// preserved receiving-owned bytes/mode, and real non-Go archive payload. This
// independently establishes the inputs; it never decides the SUT's policy.
func rsCandidatePrerequisites(t *testing.T) {
	t.Helper()
	h := newRSHarness(t)
	modPath := "example.com/ci-rs-floor"
	workflow := "name: fixture\non: [push]\njobs:\n  check:\n    runs-on: ubuntu-latest\n    steps:\n      - run: go test ./...\n"
	payload := "export const fixture = 'non-go-payload';\n"
	repo := h.initRepo("receiving", map[string]string{
		"go.mod":                         "module " + modPath + "\n\ngo 1.21\n",
		"first/first.go":                 "package first\n\nconst Value = 1\n",
		"second/second.go":               "package second\n\nconst Value = 2\n",
		".github/workflows/required.yml": workflow,
		"scanner/main.mjs":               payload,
		"empty/.keep":                    "",
	})
	h.must(repo, "git", "update-index", "--chmod=+x", "scanner/main.mjs")
	h.must(repo, "git", "commit", "--quiet", "-m", "tracked executable payload mode")
	h.must(repo, "git", "tag", "v1.0.0")
	baseline := h.archive(repo, modPath, "floor-baseline")
	workflowBefore := h.must(repo, "git", "ls-tree", "HEAD", ".github/workflows/required.yml")
	payloadBefore := h.must(repo, "git", "ls-tree", "HEAD", "scanner/main.mjs")
	if !strings.HasPrefix(workflowBefore, "100644 ") || !strings.HasPrefix(payloadBefore, "100755 ") {
		t.Fatal("HARNESS_NOT_EXECUTED: Git mode prerequisite")
	}
	packageSources := []string{}
	for _, p := range baseline.files {
		if strings.HasSuffix(p, ".go") {
			packageSources = append(packageSources, p)
		}
	}
	if len(packageSources) != 2 {
		t.Fatalf("HARNESS_NOT_EXECUTED: baseline package sources=%v", packageSources)
	}
	if _, err := os.Stat(filepath.Join(repo, "empty")); err != nil {
		t.Fatal(err)
	}
	// Remove exactly one package source while keeping a tracked directory.
	h.must(repo, "git", "rm", "second/second.go")
	h.put(repo, "second/.keep", "")
	h.must(repo, "git", "add", "second/.keep")
	h.must(repo, "git", "commit", "--quiet", "-m", "package source replaced by an empty marker")
	candidate := h.archive(repo, modPath, "floor-missing-package")
	workflowAfter := h.must(repo, "git", "ls-tree", "HEAD", ".github/workflows/required.yml")
	payloadAfter := h.must(repo, "git", "ls-tree", "HEAD", "scanner/main.mjs")
	if workflowAfter != workflowBefore || payloadAfter != payloadBefore {
		t.Fatal("HARNESS_NOT_EXECUTED: receiving-owned or payload drift in floor fixture")
	}
	zr, err := zip.OpenReader(candidate.path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	observedPayload := false
	remainingSources := 0
	marker := false
	for _, f := range zr.File {
		relative := strings.TrimPrefix(f.Name, modPath+"@"+candidate.version+"/")
		if strings.HasSuffix(relative, ".go") {
			remainingSources++
		}
		if relative == "second/.keep" {
			marker = true
		}
		if relative == "scanner/main.mjs" {
			r, e := f.Open()
			if e != nil {
				t.Fatal(e)
			}
			b, e := io.ReadAll(r)
			r.Close()
			if e != nil {
				t.Fatal(e)
			}
			if string(b) != payload {
				t.Fatal("HARNESS_NOT_EXECUTED: non-Go ZIP payload differs")
			}
			observedPayload = true
		}
	}
	if remainingSources != 1 || !marker || !observedPayload {
		t.Fatalf("HARNESS_NOT_EXECUTED: archive floor fixture sources=%d marker=%t payload=%t", remainingSources, marker, observedPayload)
	}
	raw, err := json.MarshalIndent(map[string]any{"baseline_version": "v1.0.0", "baseline_revision": baseline.revision, "candidate_revision": candidate.revision, "baseline_package_source_count": len(packageSources), "candidate_package_source_count": remainingSources, "candidate_empty_directory_marker": marker, "receiving_owned_git_entry": workflowBefore, "payload_git_entry": payloadBefore, "payload_zip_sha256": rsSHA([]byte(payload)), "classification": "HARNESS_PREREQUISITE_ONLY", "candidate_gate_verdict": "NOT_EXECUTED"}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	h.save("candidate-prerequisites.json", append(raw, '\n'))
	t.Log("HARNESS_PREREQUISITES: actual Go ZIP baseline has 2 packages; candidate has 1 despite directory marker; receiving-owned Git entry and non-Go ZIP payload preserved; no candidate verdict")
}

func TestReleaseSupplyCandidatePreflight(t *testing.T) {
	t.Run("real_archive_floor_prerequisites", func(t *testing.T) { rsCandidatePrerequisites(t) })
	t.Run("candidate_contract", func(t *testing.T) { rsRunCandidateCases(t) })
}

// Canonical fixture identity follows the separately recorded root routine:
// sorted object keys, UTF-8 strings, compact JSON and no final newline. Go's
// optional HTML/JavaScript-safe escaping must not alter those agreed bytes.
func rsCanonicalJSON(t *testing.T, value any) []byte {
	t.Helper()
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	if err := e.Encode(value); err != nil {
		t.Fatal(err)
	}
	raw := bytes.TrimSuffix(b.Bytes(), []byte{'\n'})
	var out bytes.Buffer
	for i := 0; i < len(raw); i++ {
		if raw[i] == '\\' && i+1 < len(raw) {
			if bytes.HasPrefix(raw[i:], []byte(`\u2028`)) {
				out.WriteRune('\u2028')
				i += 5
				continue
			}
			if bytes.HasPrefix(raw[i:], []byte(`\u2029`)) {
				out.WriteRune('\u2029')
				i += 5
				continue
			}
			out.WriteByte(raw[i])
			i++
			out.WriteByte(raw[i])
			continue
		}
		out.WriteByte(raw[i])
	}
	return out.Bytes()
}

func rsVersionArchive(h *rsHarness, repo, modPath, version, label string) rsArchive {
	h.t.Helper()
	revision := h.must(repo, "git", "rev-parse", "HEAD")
	escaped, err := module.EscapePath(modPath)
	if err != nil {
		h.t.Fatal(err)
	}
	proxy := filepath.Join(h.root, "proxy-"+label)
	dir := filepath.Join(proxy, escaped, "@v")
	if err := os.MkdirAll(dir, 0755); err != nil {
		h.t.Fatal(err)
	}
	name := filepath.Join(dir, version+".zip")
	out, err := os.Create(name)
	if err != nil {
		h.t.Fatal(err)
	}
	packErr := modzip.CreateFromVCS(out, module.Version{Path: modPath, Version: version}, repo, revision, "")
	closeErr := out.Close()
	if packErr != nil || closeErr != nil {
		h.t.Fatalf("HARNESS_NOT_EXECUTED: versioned archive %v %v", packErr, closeErr)
	}
	h.put(dir, version+".mod", h.must(repo, "git", "show", revision+":go.mod")+"\n")
	h.put(dir, version+".info", fmt.Sprintf(`{"Version":%q,"Time":"2000-01-01T00:00:00Z"}`, version))
	h.put(dir, "list", version+"\n")
	z, err := zip.OpenReader(name)
	if err != nil {
		h.t.Fatal(err)
	}
	defer z.Close()
	files := []string{}
	for _, f := range z.File {
		files = append(files, strings.TrimPrefix(f.Name, modPath+"@"+version+"/"))
	}
	sort.Strings(files)
	h.save(label+".zip", mustRSRead(h.t, name))
	meta, _ := json.MarshalIndent(map[string]any{"revision": revision, "version": version, "files": files, "zip_sha256": rsSHA(mustRSRead(h.t, name))}, "", "  ")
	h.save(label+".archive.json", append(meta, '\n'))
	return rsArchive{revision, version, proxy, name, files}
}

func rsTrackedInventory(h *rsHarness, repo, revision string, include func(string) bool) []any {
	listing := h.must(repo, "git", "ls-tree", "-r", "--full-tree", revision)
	rows := []any{}
	for _, line := range strings.Split(listing, "\n") {
		meta, path, ok := strings.Cut(line, "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 3 || fields[1] != "blob" || (fields[0] != "100644" && fields[0] != "100755") {
			h.t.Fatalf("HARNESS_NOT_EXECUTED: unsupported Git fixture tree %q", line)
		}
		if include != nil && !include(path) {
			continue
		}
		raw, stderr, rc := h.run(repo, nil, "git", "show", revision+":"+path)
		if rc != 0 {
			h.t.Fatalf("HARNESS_NOT_EXECUTED: tracked file %s %s", path, stderr)
		}
		rows = append(rows, map[string]any{"path": path, "mode": fields[0], "sha256": rsSHA(raw)})
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].(map[string]any)["path"].(string) < rows[j].(map[string]any)["path"].(string)
	})
	return rows
}

type rsHTTPFixture struct {
	Method   string `json:"method"`
	URL      string `json:"url"`
	Status   int    `json:"status"`
	BodyPath string `json:"body_path"`
	BodySHA  string `json:"body_sha256"`
}

// The additional seam only substitutes exact HTTP response bytes. Commands
// still use the independently accepted Git adapter and actual Go executables.
func rsCandidateBridgeSource(t *testing.T) string {
	t.Helper()
	s := rsConsumerBridgeSource
	patches := [][2]string{
		{" \"context\"\n", " \"context\"\n \"crypto/sha256\"\n \"encoding/hex\"\n \"io\"\n"},
		{"type bridgeRequest struct {", "type bridgeHTTP struct {Method string `json:\"method\"`;URL string `json:\"url\"`;Status int `json:\"status\"`;BodyPath string `json:\"body_path\"`;BodySHA string `json:\"body_sha256\"`}\ntype bridgeRequest struct {\n HTTP []bridgeHTTP `json:\"http\"`"},
	}
	for _, p := range patches {
		if strings.Count(s, p[0]) != 1 {
			t.Fatalf("HARNESS_NOT_EXECUTED: bridge adaptation anchor %q", p[0])
		}
		s = strings.Replace(s, p[0], p[1], 1)
	}
	start := strings.Index(s, "  HTTP:func(ctx context.Context,r *http.Request)(*http.Response,error) {")
	if start < 0 {
		t.Fatal("HARNESS_NOT_EXECUTED: HTTP seam anchor")
	}
	end := strings.Index(s[start:], "  Now:time.Now,")
	if end < 0 {
		t.Fatal("HARNESS_NOT_EXECUTED: HTTP seam anchor")
	}
	httpCode := `  HTTP:func(ctx context.Context,r *http.Request)(*http.Response,error) {
   mu.Lock();ordinal++;id:=ordinal;mu.Unlock()
   record:=map[string]any{"method":r.Method,"url":r.URL.String()}
   save:=func(){b,_:=json.MarshalIndent(record,"","  ");if err:=os.WriteFile(filepath.Join(request.Output,fmt.Sprintf("http-%04d.json",id)),b,0600);err!=nil{panic(err)}}
   if r.Method!="GET" {record["mutation_attempt"]=true;save();mu.Lock();unhandled=append(unhandled,"candidate HTTP mutation");mu.Unlock();return nil,fmt.Errorf("candidate requested HTTP mutation")}
   for _,fixture:=range request.HTTP {if fixture.Method==r.Method&&fixture.URL==r.URL.String(){
    body,err:=os.ReadFile(fixture.BodyPath);if err!=nil{save();mu.Lock();unhandled=append(unhandled,"unread fixture HTTP body");mu.Unlock();return nil,err}
    sum:=sha256.Sum256(body);digest:=hex.EncodeToString(sum[:]);if digest!=fixture.BodySHA{save();mu.Lock();unhandled=append(unhandled,"fixture HTTP body drift");mu.Unlock();return nil,fmt.Errorf("fixture body drift")}
    record["status"]=fixture.Status;record["body_sha256"]=digest;save()
    return &http.Response{StatusCode:fixture.Status,Header:make(http.Header),Body:io.NopCloser(bytes.NewReader(body)),Request:r},nil
   }}
   save();mu.Lock();unhandled=append(unhandled,"http:"+r.Method+" "+r.URL.String());mu.Unlock();return nil,fmt.Errorf("unhandled exact fixture HTTP request")
  },
`
	return s[:start] + httpCode + s[start+end:]
}

func rsBuildCandidateBridge(h *rsHarness) string {
	// First builds and validates the unchanged production-source clone using
	// the accepted bridge; then adds only the exact HTTP fixture response seam.
	_ = rsBuildConsumerBridge(h)
	clone := filepath.Join(h.root, "sut-copy")
	h.put(clone, "internal/release/ci_rs_bridge_test.go", rsCandidateBridgeSource(h.t))
	binary := filepath.Join(h.root, "candidate-bridge.test")
	h.must(clone, h.goBin, "test", "-c", "-o", binary, "./internal/release")
	return binary
}

type rsCandidateFixture struct {
	h                                    *rsHarness
	repo, base, revision, origin, module string
	baseline                             rsArchive
	manifest                             map[string]any
	http                                 []rsHTTPFixture
	proofSource, proofTest               map[string]any
	proofRun                             map[string]any
	ordinal                              int
}

type rsCandidateCase struct {
	Name, Scenario, Axis, Outcome, Reason, Root, Revision, Origin string
	Manifest                                                      map[string]any
	HTTP                                                          []rsHTTPFixture
}

func (f *rsCandidateFixture) variant(label string, change func(string)) (string, string, map[string]any) {
	h := f.h
	root := filepath.Join(h.root, "candidate-"+label)
	h.must(h.root, "git", "clone", "--quiet", "--no-local", f.repo, root)
	h.must(root, "git", "config", "user.name", "CI-RS fixture")
	h.must(root, "git", "config", "user.email", "ci-rs@invalid")
	h.must(root, "git", "remote", "set-url", "origin", "https://github.com/PRO-Robotech/corelib.git")
	change(root)
	h.must(root, "git", "add", "--all")
	h.must(root, "git", "commit", "--quiet", "-m", label)
	revision := h.must(root, "git", "rev-parse", "HEAD")
	h.save(label+".source.diff", []byte(h.must(root, "git", "diff", f.base, revision)+"\n"))
	m := rsCloneJSON(f.manifest)
	m["candidate_root"] = root
	input := rsTrackedInventory(h, root, revision, func(path string) bool { return !strings.HasPrefix(path, ".github/") })
	m["input_files"] = input
	m["input_tree_digest"] = rsSHA(rsCanonicalJSON(h.t, input))
	replace := []string{}
	found := map[string]bool{}
	for _, raw := range input {
		p := raw.(map[string]any)["path"].(string)
		replace = append(replace, p)
		found[p] = true
	}
	m["replace_paths"] = replace
	removed := []string{}
	for _, raw := range f.manifest["input_files"].([]any) {
		p := raw.(map[string]any)["path"].(string)
		if !found[p] {
			removed = append(removed, p)
		}
	}
	m["remove_paths"] = removed
	return root, revision, m
}

func rsCandidateCases(f *rsCandidateFixture) []rsCandidateCase {
	h := f.h
	cases := []rsCandidateCase{}
	add := func(name, scenario, axis, outcome, reason, root, revision, origin string, m map[string]any, http []rsHTTPFixture) {
		cases = append(cases, rsCandidateCase{name, scenario, axis, outcome, reason, root, revision, origin, m, append([]rsHTTPFixture(nil), http...)})
	}
	lawful := func(name, scenario string, m map[string]any, root, revision string) {
		add(name, scenario, "none", "GREEN", "OK", root, revision, f.origin, m, f.http)
	}
	lawful("ready-tree-explicit-preserve", "CI-RS-03/04/05/06/20", rsCloneJSON(f.manifest), f.repo, f.revision)
	identical := rsCloneJSON(f.manifest)
	allInput := rsTrackedInventory(h, f.repo, f.base, nil)
	identical["input_files"] = allInput
	identical["input_tree_digest"] = rsSHA(rsCanonicalJSON(h.t, allInput))
	identical["preserve_paths"] = []string{}
	allReplace := []string{}
	for _, raw := range allInput {
		allReplace = append(allReplace, raw.(map[string]any)["path"].(string))
	}
	identical["replace_paths"] = allReplace
	lawful("input-includes-identical-owned-file", "CI-RS-04", identical, f.repo, f.revision)
	m := rsCloneJSON(f.manifest)
	m["preserve_paths"] = []string{}
	add("omitted-github-preserve", "CI-RS-03", "remove only .github explicit preservation", "RED", "UNDECLARED_PATH_LOSS", f.repo, f.revision, f.origin, m, f.http)
	for _, kind := range []string{"bytes", "mode"} {
		root, revision, m := f.variant("receiving-"+kind, func(root string) {
			if kind == "bytes" {
				h.put(root, ".github/workflows/required.yml", string(mustRSRead(h.t, filepath.Join(root, ".github/workflows/required.yml")))+"# altered by source\n")
			} else {
				if e := os.Chmod(filepath.Join(root, ".github/workflows/required.yml"), 0755); e != nil {
					h.t.Fatal(e)
				}
			}
		})
		input := rsTrackedInventory(h, root, revision, nil)
		m["input_files"] = input
		m["input_tree_digest"] = rsSHA(rsCanonicalJSON(h.t, input))
		m["preserve_paths"] = []string{}
		paths := []string{}
		for _, raw := range input {
			paths = append(paths, raw.(map[string]any)["path"].(string))
		}
		m["replace_paths"] = paths
		add("receiving-owned-"+kind, "CI-RS-04", "only receiving-owned "+kind+" changes", "RED", "RECEIVING_OWNERSHIP_CONFLICT", root, revision, f.origin, m, f.http)
	}
	root, revision, m := f.variant("floor-empty-directory", func(root string) {
		if e := os.Remove(filepath.Join(root, "second/value.go")); e != nil {
			h.t.Fatal(e)
		}
		h.put(root, "second/.keep", "")
	})
	a := h.archive(root, f.module, "candidate-floor-negative")
	for _, name := range a.files {
		if name == "second/value.go" {
			h.t.Fatal("HARNESS_NOT_EXECUTED: floor source unexpectedly in archive")
		}
	}
	add("directory-is-not-package", "CI-RS-05", "one Go package source removed; tracked directory marker remains", "RED", "PACKAGE_FLOOR_LOSS", root, revision, f.origin, m, f.http)
	unavailable := append([]rsHTTPFixture(nil), f.http...)
	for i := range unavailable {
		if strings.HasSuffix(unavailable[i].URL, "v1.0.0.zip") {
			unavailable[i].Status = 404
		}
	}
	add("previous-archive-unavailable", "CI-RS-06", "only previous-version zip HTTP status becomes404", "NOT_EXECUTED", "BASELINE_UNAVAILABLE", f.repo, f.revision, f.origin, rsCloneJSON(f.manifest), unavailable)
	staleRemote := filepath.Join(h.root, "newer-tag-origin.git")
	h.must(h.root, "git", "clone", "--mirror", "file://"+filepath.ToSlash(f.origin), staleRemote)
	h.must(staleRemote, "git", "tag", "v1.0.1", f.base)
	add("baseline-stale-newer-tag", "CI-RS-06", "only origin has newer semver tag, not yet on proxy", "RED", "BASELINE_STALE", f.repo, f.revision, staleRemote, rsCloneJSON(f.manifest), f.http)
	// A valid Go ZIP with no .go source is a measured empty baseline, not an
	// absent fixture or missing tool. The current candidate remains nonempty.
	emptyRepo := h.initRepo("empty-previous", map[string]string{"go.mod": "module " + f.module + "\n\ngo 1.21\n", "README.md": "no Go packages in this released fixture\n"})
	emptyArchive := rsVersionArchive(h, emptyRepo, f.module, "v1.0.0", "empty-previous")
	emptyRemote := filepath.Join(h.root, "empty-previous-origin.git")
	h.must(h.root, "git", "clone", "--mirror", "file://"+filepath.ToSlash(f.origin), emptyRemote)
	h.must(emptyRemote, "git", "fetch", "--quiet", emptyRepo, emptyArchive.revision)
	h.must(emptyRemote, "git", "tag", "-d", "v1.0.0")
	h.must(emptyRemote, "git", "tag", "v1.0.0", emptyArchive.revision)
	emptyHTTP := append([]rsHTTPFixture(nil), f.http...)
	for i := range emptyHTTP {
		if strings.Contains(emptyHTTP[i].URL, "/@v/v1.0.0.") {
			suffix := emptyHTTP[i].URL[strings.LastIndex(emptyHTTP[i].URL, "/")+1:]
			path := filepath.Join(filepath.Dir(emptyArchive.path), suffix)
			emptyHTTP[i].BodyPath = path
			emptyHTTP[i].BodySHA = rsSHA(mustRSRead(h.t, path))
		}
	}
	add("previous-package-census-empty", "CI-RS-06", "previous release is valid ZIP with zero Go packages", "RED", "BASELINE_EMPTY", f.repo, f.revision, emptyRemote, rsCloneJSON(f.manifest), emptyHTTP)
	// Components are opaque payload to RS. These fixture bytes are not a
	// claimed implementation of NP/DT; scoped authority responses below are
	// synthetic and the RS checks must bind every byte and required proof ID.
	compressed, err := base64.StdEncoding.DecodeString(rsNotice2590Gzip)
	if err != nil {
		h.t.Fatal(err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		h.t.Fatal(err)
	}
	notice, err := io.ReadAll(zr)
	zr.Close()
	if err != nil || rsSHA(notice) != "c85a8ed9b06a0720fa4050e3537ee90892cb1aa81e84103aba63da7fe6125f96" {
		h.t.Fatal("HARNESS_NOT_EXECUTED: exact approved notice fixture identity")
	}
	componentFiles := map[string]map[string]string{
		"CI-NP-1": {"ci/newman/publication.py": "# synthetic RS payload fixture, not an NP implementation\nVALUE = 1\n", "ci/newman/publication.mjs": "// synthetic RS payload fixture\nexport const value = 1;\n", "ci/newman/package-lock.json": "{\"name\":\"ci-rs-fixture\",\"lockfileVersion\":3,\"packages\":{\"node_modules/@actions/artifact\":{\"version\":\"6.2.1\"}}}\n"},
		"CI-DT-1": {"api/corelib/subscription/subscription.pb.go": "// Synthetic RS binding fixture, not generated production output.\npackage subscription\nconst Fixture = 1\n", "migratorcli/notice_test.go": string(notice)},
	}
	root, revision, m = f.variant("component-payload", func(root string) {
		for _, files := range componentFiles {
			for path, body := range files {
				h.put(root, path, body)
			}
		}
		if e := os.Chmod(filepath.Join(root, "ci/newman/publication.py"), 0755); e != nil {
			h.t.Fatal(e)
		}
	})
	m["components"] = []string{"CI-DT-1", "CI-NP-1"}
	m["ci_dt_addenda"] = []string{"2590"}
	payload := []any{}
	sourceFiles := map[string][]any{"CI-DT-1": {}, "CI-NP-1": {}}
	for _, raw := range m["input_files"].([]any) {
		entry := raw.(map[string]any)
		path := entry["path"].(string)
		for component, files := range componentFiles {
			if _, ok := files[path]; ok {
				row := rsCloneJSON(entry)
				row["component"] = component
				payload = append(payload, row)
				sourceFiles[component] = append(sourceFiles[component], entry)
			}
		}
	}
	m["payload"] = payload
	proofs := []any{}
	for _, id := range []string{"DT-A2590-P01", "DT-A2590-P02", "DT-P01", "DT-P02", "DT-P03", "NP-P01", "NP-P02", "NP-P03", "NP-P04", "NP-P05", "NP-P06"} {
		component := "CI-DT-1"
		if strings.HasPrefix(id, "NP-") {
			component = "CI-NP-1"
		}
		source := map[string]any{"repository": "PRO-Robotech/corelib", "revision": revision, "files": sourceFiles[component]}
		proofs = append(proofs, f.proof(id, source))
	}
	m["proofs"] = proofs
	componentArchive := h.archive(root, f.module, "actual-component-payload")
	z, e := zip.OpenReader(componentArchive.path)
	if e != nil {
		h.t.Fatal(e)
	}
	observed := map[string]string{}
	for _, file := range z.File {
		r, e := file.Open()
		if e != nil {
			h.t.Fatal(e)
		}
		b, e := io.ReadAll(r)
		r.Close()
		if e != nil {
			h.t.Fatal(e)
		}
		observed[strings.TrimPrefix(file.Name, f.module+"@"+componentArchive.version+"/")] = rsSHA(b)
	}
	z.Close()
	for _, raw := range payload {
		entry := raw.(map[string]any)
		if observed[entry["path"].(string)] != entry["sha256"] {
			h.t.Fatalf("HARNESS_NOT_EXECUTED: actual non-Go/DT payload bytes absent%v", entry)
		}
	}
	lawful("full-prerelease-proofs-postrelease-pending", "CI-RS-21", rsCloneJSON(m), root, revision)
	for i, raw := range proofs {
		id := raw.(map[string]any)["subject"].(map[string]any)["predicate_id"].(string)
		for _, axis := range []string{"missing", "altered", "unreadable"} {
			variant := rsCloneJSON(m)
			p := variant["proofs"].([]any)
			reason, outcome := "PAYLOAD_MISMATCH", "RED"
			switch axis {
			case "missing":
				variant["proofs"] = append(p[:i:i], p[i+1:]...)
			case "altered":
				p[i].(map[string]any)["subject_sha256"] = strings.Repeat("0", 64)
			case "unreadable":
				p[i].(map[string]any)["review"].(map[string]any)["path"] = filepath.Join(h.root, "absent-"+id+"-review.json")
				reason, outcome = "SOURCE_UNAVAILABLE", "NOT_EXECUTED"
			}
			add(id+"-"+axis, "CI-RS-21", "only "+id+" proof "+axis, outcome, reason, root, revision, f.origin, variant, f.http)
		}
	}
	// Each closed component set has its own lawful archive. This prevents a
	// missing second component from explaining the projection-only refusal.
	for _, component := range []string{"CI-NP-1", "CI-DT-1"} {
		componentRoot, componentRevision, separate := f.variant("only-"+component, func(root string) {
			for path, body := range componentFiles[component] {
				if path == "migratorcli/notice_test.go" {
					continue
				}
				h.put(root, path, body)
				if path == "ci/newman/publication.py" {
					if err := os.Chmod(filepath.Join(root, path), 0755); err != nil {
						h.t.Fatal(err)
					}
				}
			}
		})
		separate["components"] = []string{component}
		separate["ci_dt_addenda"] = []string{}
		files := []any{}
		separatePayload := []any{}
		for _, raw := range separate["input_files"].([]any) {
			entry := raw.(map[string]any)
			if _, ok := componentFiles[component][entry["path"].(string)]; ok {
				files = append(files, entry)
				row := rsCloneJSON(entry)
				row["component"] = component
				separatePayload = append(separatePayload, row)
			}
		}
		separate["payload"] = separatePayload
		ids := []string{"DT-P01", "DT-P02", "DT-P03"}
		if component == "CI-NP-1" {
			ids = []string{"NP-P01", "NP-P02", "NP-P03", "NP-P04", "NP-P05", "NP-P06"}
		}
		separateProofs := []any{}
		for _, id := range ids {
			separateProofs = append(separateProofs, f.proof(id, map[string]any{"repository": "PRO-Robotech/corelib", "revision": componentRevision, "files": files}))
		}
		separate["proofs"] = separateProofs
		lawful("complete-"+component+"-only", "CI-RS-21", separate, componentRoot, componentRevision)
		if component == "CI-NP-1" {
			onlyProjection := rsCloneJSON(separate)
			onlyProjection["proofs"] = []any{separateProofs[0]}
			add("NP-only-projection-insufficient", "CI-RS-21", "NP-P02..06 absent; no other component applies", "RED", "PAYLOAD_MISMATCH", componentRoot, componentRevision, f.origin, onlyProjection, f.http)
		}
	}
	projection := rsCloneJSON(m)
	projection["proofs"] = []any{rsCloneJSON(proofs[5].(map[string]any))}
	add("projection-only-is-insufficient", "CI-RS-21", "proof set narrowed to NP-P01 only", "RED", "PAYLOAD_MISMATCH", root, revision, f.origin, projection, f.http)
	wrongPayload := rsCloneJSON(m)
	wrongPayload["payload"].([]any)[0].(map[string]any)["sha256"] = strings.Repeat("0", 64)
	add("payload-digest-mismatch", "CI-RS-21", "only one declared payload digest differs", "RED", "PAYLOAD_MISMATCH", root, revision, f.origin, wrongPayload, f.http)
	// The producer review and execution authority are distinct bound events.
	// All mutations below retain the unaffected subject, files and HTTP bodies.
	producerCase := func(name, axis, outcome, reason string, mutate func(map[string]any), responses []rsHTTPFixture) {
		v := rsCloneJSON(f.manifest)
		priorResponses := len(f.http)
		mutate(v["producer_execution"].(map[string]any))
		boundResponses := append(append([]rsHTTPFixture(nil), responses...), f.http[priorResponses:]...)
		add(name, "CI-RS-20/22", axis, outcome, reason, f.repo, f.revision, f.origin, v, boundResponses)
	}
	baseHTTP := cases[0].HTTP
	producerCase("producer-authorization-missing", "remove only separate execution authorization", "RED", "INVALID_CONFIRMATION", func(p map[string]any) { delete(p, "authorization") }, baseHTTP)
	producerCase("producer-repository-wrong", "producer repository changes only", "RED", "REPOSITORY_MISMATCH", func(p map[string]any) { p["repository"] = "PRO-Robotech/corelib" }, baseHTTP)
	for _, field := range []string{"review", "authorization"} {
		producerCase("producer-"+field+"-unreadable", "only "+field+" file becomes unavailable", "NOT_EXECUTED", "SOURCE_UNAVAILABLE", func(p map[string]any) {
			p[field].(map[string]any)["path"] = filepath.Join(h.root, "absent-producer-"+field+".json")
		}, baseHTTP)
	}
	producerCase("producer-wrong-authority-kind", "only execution authority kind becomes component-review", "RED", "INVALID_CONFIRMATION", func(p map[string]any) {
		ref := p["authorization"].(map[string]any)
		var record map[string]any
		if e := json.Unmarshal(mustRSRead(h.t, ref["path"].(string)), &record); e != nil {
			h.t.Fatal(e)
		}
		record["kind"] = "component-review"
		p["authorization"] = f.fileRef("wrong-execution-kind.json", rsCanonicalJSON(h.t, record))
	}, baseHTTP)
	producerCase("producer-review-stale-R", "only prior review subject digest is stale", "RED", "INVALID_CONFIRMATION", func(p map[string]any) {
		ref := p["review"].(map[string]any)
		var record map[string]any
		if e := json.Unmarshal(mustRSRead(h.t, ref["path"].(string)), &record); e != nil {
			h.t.Fatal(e)
		}
		record["subject_sha256"] = strings.Repeat("0", 64)
		p["review"] = f.fileRef("review-stale-R.json", rsCanonicalJSON(h.t, record))
		// Rebind the derived outer E authority. The only unlawful fact is R,
		// so checking E alone cannot accidentally satisfy this negative.
		f.rebindExecutionAuthority(p, "stale-R-with-valid-E")
	}, baseHTTP)
	for _, axis := range []string{"unknown-key", "duplicate-key"} {
		producerCase("producer-review-"+axis, "only prior review JSON "+axis+"; outer E is correctly rebound", "RED", "INVALID_CONFIRMATION", func(p map[string]any) {
			ref := p["review"].(map[string]any)
			raw := mustRSRead(h.t, ref["path"].(string))
			var record map[string]any
			if err := json.Unmarshal(raw, &record); err != nil {
				h.t.Fatal(err)
			}
			if axis == "unknown-key" {
				record["readback"] = ref
				raw = rsCanonicalJSON(h.t, record)
			} else {
				raw = append(bytes.TrimSuffix(raw, []byte("}")), []byte(`,"kind":"producer-review"}`)...)
			}
			p["review"] = f.fileRef("prior-review-"+axis+".json", raw)
			f.rebindExecutionAuthority(p, axis+"-valid-E")
		}, baseHTTP)
	}
	producerCase("producer-authorization-stale-E", "only execution subject digest is stale", "RED", "INVALID_CONFIRMATION", func(p map[string]any) {
		ref := p["authorization"].(map[string]any)
		var record map[string]any
		if e := json.Unmarshal(mustRSRead(h.t, ref["path"].(string)), &record); e != nil {
			h.t.Fatal(e)
		}
		record["subject_sha256"] = strings.Repeat("0", 64)
		p["authorization"] = f.fileRef("authorization-stale-E.json", rsCanonicalJSON(h.t, record))
	}, baseHTTP)
	for _, axis := range []string{"actor", "body"} {
		responses := append([]rsHTTPFixture(nil), baseHTTP...)
		for i := range responses {
			if strings.HasSuffix(responses[i].URL, "/989999") {
				var body map[string]any
				if e := json.Unmarshal(mustRSRead(h.t, responses[i].BodyPath), &body); e != nil {
					h.t.Fatal(e)
				}
				if axis == "actor" {
					body["user"].(map[string]any)["login"] = "other-fixture-actor"
				} else {
					body["body"] = body["body"].(string) + "changed"
				}
				ref := f.fileRef("prior-review-"+axis+".json", rsCanonicalJSON(h.t, body))
				responses[i].BodyPath = ref["path"].(string)
				responses[i].BodySHA = ref["sha256"].(string)
			}
		}
		producerCase("producer-review-event-"+axis+"-mismatch", "only prior HTTP event "+axis+" differs", "RED", "INVALID_CONFIRMATION", func(map[string]any) {}, responses)
	}
	producerRoot := f.manifest["producer_execution"].(map[string]any)["root"].(string)
	ownedProducer := filepath.Join(h.root, "same-producer-copy")
	h.must(h.root, "git", "clone", "--quiet", "--no-local", producerRoot, ownedProducer)
	h.must(ownedProducer, "git", "remote", "set-url", "origin", "https://github.com/PRO-Robotech/kacho.git")
	producerCase("producer-relocated-same-frozen-content", "only actual identical producer locator changes", "GREEN", "OK", func(p map[string]any) { p["root"] = ownedProducer }, baseHTTP)
	dirtyProducer := filepath.Join(h.root, "changed-producer-copy")
	h.must(h.root, "git", "clone", "--quiet", "--no-local", producerRoot, dirtyProducer)
	h.must(dirtyProducer, "git", "remote", "set-url", "origin", "https://github.com/PRO-Robotech/kacho.git")
	closurePath := "scripts/release/publish-version.sh"
	original := mustRSRead(h.t, filepath.Join(dirtyProducer, closurePath))
	h.put(dirtyProducer, closurePath, string(original)+"\n# single fixture source drift\n")
	if changed := h.must(dirtyProducer, "git", "diff", "--name-only"); changed != closurePath {
		h.t.Fatalf("HARNESS_NOT_EXECUTED: producer drift fixture%q", changed)
	}
	producerCase("producer-frozen-closure-drift", "only one actual tracked producer source file drifts", "RED", "INPUT_CHANGED", func(p map[string]any) { p["root"] = dirtyProducer }, baseHTTP)
	return cases
}

func (f *rsCandidateFixture) fileRef(label string, raw []byte) map[string]any {
	f.ordinal++
	name := fmt.Sprintf("record-%04d-%s", f.ordinal, label)
	f.h.put(f.h.root, name, string(raw))
	f.h.save(name, raw)
	return map[string]any{"path": filepath.Join(f.h.root, name), "sha256": rsSHA(raw)}
}

func (f *rsCandidateFixture) rebindExecutionAuthority(producer map[string]any, label string) {
	projection := rsCloneJSON(producer)
	delete(projection, "root")
	delete(projection, "authorization")
	digest := rsSHA(rsCanonicalJSON(f.h.t, projection))
	number := 980000 + f.ordinal
	body := "SYNTHETIC FIXTURE EXECUTION SUBJECT ONLY\nSubject-SHA256: " + digest + "\nNo live execution permission.\n"
	coordinate := map[string]any{"repository": "PRO-Robotech/kacho", "issue_number": 2588, "comment_id": number, "author_login": "ci-rs-fixture-root", "body_sha256": rsSHA([]byte(body))}
	api := fmt.Sprintf("https://api.github.com/repos/PRO-Robotech/kacho/issues/comments/%d", number)
	readback := f.fileRef(label+"-event.json", rsCanonicalJSON(f.h.t, map[string]any{"id": number, "url": api, "user": map[string]any{"login": "ci-rs-fixture-root"}, "body": body}))
	f.http = append(f.http, rsHTTPFixture{"GET", api, 200, readback["path"].(string), readback["sha256"].(string)})
	producer["authorization"] = f.fileRef(label+"-authority.json", rsCanonicalJSON(f.h.t, map[string]any{"schema_version": 1, "kind": "producer-execution", "subject_sha256": digest, "coordinate": coordinate, "readback": readback}))
}

// These records model previously reviewed fixture evidence, not actual NP/DT
// or T5 approval. Actual captures/revisions/digests are bound, while authority
// readback is substituted only at the explicitly declared test HTTP boundary.
func (f *rsCandidateFixture) proof(id string, source map[string]any) map[string]any {
	h := f.h
	subject := map[string]any{"predicate_id": id, "source": source, "test": f.proofTest, "runs": []any{f.proofRun}}
	digest := rsSHA(rsCanonicalJSON(h.t, subject))
	number := 900000 + f.ordinal
	body := "SYNTHETIC CI-RS FIXTURE ONLY\nDecision: APPROVE\nPredicate: " + id + "\nSubject-SHA256: " + digest + "\nThis is not live NP, DT, T5, or execution authorization.\n"
	coordinate := map[string]any{"repository": "PRO-Robotech/kacho", "issue_number": 2588, "comment_id": number, "author_login": "ci-rs-fixture-reviewer", "body_sha256": rsSHA([]byte(body))}
	apiURL := fmt.Sprintf("https://api.github.com/repos/PRO-Robotech/kacho/issues/comments/%d", number)
	apiBody := rsCanonicalJSON(h.t, map[string]any{"id": number, "url": apiURL, "html_url": fmt.Sprintf("https://github.com/PRO-Robotech/kacho/issues/2588#issuecomment-%d", number), "user": map[string]any{"login": "ci-rs-fixture-reviewer"}, "body": body})
	readback := f.fileRef(id+"-readback.json", apiBody)
	f.http = append(f.http, rsHTTPFixture{"GET", apiURL, 200, readback["path"].(string), readback["sha256"].(string)})
	independence := f.fileRef(id+"-independence.json", rsCanonicalJSON(h.t, map[string]any{"classification": "SYNTHETIC_FIXTURE_ONLY", "reviewer": "ci-rs-fixture-reviewer", "source_author": "ci-rs-fixture-author", "subject_sha256": digest}))
	review := f.fileRef(id+"-review.json", rsCanonicalJSON(h.t, map[string]any{"schema_version": 1, "kind": "scoped-review", "predicate_id": id, "subject_sha256": digest, "outcome": "GREEN", "reviewer": "ci-rs-fixture-reviewer", "independence_record": independence, "authority_coordinate": coordinate}))
	authority := f.fileRef(id+"-authority.json", rsCanonicalJSON(h.t, map[string]any{"schema_version": 1, "kind": "component-review", "subject_sha256": digest, "coordinate": coordinate, "readback": readback}))
	return map[string]any{"subject": subject, "subject_sha256": digest, "review": review, "authority": authority}
}

func rsPrepareCandidate(t *testing.T) *rsCandidateFixture {
	h := newRSHarness(t)
	f := &rsCandidateFixture{h: h, module: "github.com/PRO-Robotech/corelib"}
	workflow := "name: receiving-owned\non: [push]\njobs:\n  required:\n    runs-on: ubuntu-latest\n    steps:\n      - run: go test ./...\n"
	f.repo = h.initRepo("candidate-base", map[string]string{"go.mod": "module " + f.module + "\n\ngo 1.21\n", "first/value.go": "package first\nconst Value=1\n", "second/value.go": "package second\nconst Value=2\n", "second/.keep": "", ".github/workflows/required.yml": workflow, "testdata/consumer/main.go": "package main\nimport \"" + f.module + "/first\"\nfunc main(){_ = first.Value}\n"})
	h.must(f.repo, "git", "remote", "add", "origin", "https://github.com/PRO-Robotech/corelib.git")
	f.base = h.must(f.repo, "git", "rev-parse", "HEAD")
	f.revision = f.base
	h.must(f.repo, "git", "tag", "v1.0.0")
	f.origin = filepath.Join(h.root, "candidate-origin.git")
	h.must(h.root, "git", "clone", "--bare", "--no-local", f.repo, f.origin)
	f.baseline = rsVersionArchive(h, f.repo, f.module, "v1.0.0", "previous-version")
	program := "package main\nimport \"" + f.module + "/first\"\nimport \"" + f.module + "/second\"\nfunc main(){_ = first.Value+second.Value}\n"
	if output, rc := h.buildConsumer(f.baseline, f.module, program, "previous-floor"); rc != 0 {
		t.Fatalf("HARNESS_NOT_EXECUTED: actual previous nonempty Go archive: %s", output)
	}
	escaped, err := module.EscapePath(f.module)
	if err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"list", "v1.0.0.info", "v1.0.0.mod", "v1.0.0.zip"} {
		path := filepath.Join(f.baseline.proxy, escaped, "@v", suffix)
		endpoint := "https://proxy.golang.org/" + escaped + "/@v/" + suffix
		parsed, e := url.Parse(endpoint)
		if e != nil {
			t.Fatal(e)
		}
		seen := map[string]bool{}
		for _, name := range []string{endpoint, parsed.String()} {
			if !seen[name] {
				f.http = append(f.http, rsHTTPFixture{"GET", name, 200, path, rsSHA(mustRSRead(t, path))})
				seen[name] = true
			}
		}
	}
	input := rsTrackedInventory(h, f.repo, f.base, func(path string) bool { return !strings.HasPrefix(path, ".github/") })
	receiving := rsTrackedInventory(h, f.repo, f.base, func(path string) bool { return strings.HasPrefix(path, ".github/") })
	owned := []any{}
	preserve := []string{}
	replace := []string{}
	for _, raw := range receiving {
		entry := raw.(map[string]any)
		entry["reason"] = "receiving required workflow"
		owned = append(owned, entry)
		preserve = append(preserve, entry["path"].(string))
	}
	for _, raw := range input {
		replace = append(replace, raw.(map[string]any)["path"].(string))
	}
	f.manifest = map[string]any{"schema_version": 1, "repository": "PRO-Robotech/corelib", "module_path": f.module, "version": "v1.0.1", "base_sha": f.base, "baseline_version": "v1.0.0", "input_tree_digest": rsSHA(rsCanonicalJSON(t, input)), "input_files": input, "receiving_owned": owned, "preserve_paths": preserve, "replace_paths": replace, "remove_paths": []string{}, "candidate_root": f.repo, "consumers": []any{map[string]any{"type": "external-program", "source": map[string]any{"kind": "candidate"}, "program_paths": []string{"testdata/consumer/main.go"}, "contexts": []any{map[string]any{"goos": "linux", "goarch": "amd64", "cgo_enabled": false, "tags": []string{}}}}}, "product_trees": []any{}, "internal_modules": []any{}, "components": []string{}, "ci_dt_addenda": []string{}, "payload": []any{}, "proofs": []any{}, "budgets": map[string]any{"network_seconds": 3, "checks_seconds": 5}}
	// Source identity is the actual committed producer, never a tiny substitute
	// program. Capture a real existing independent regression and its source.
	producer := moduleRoot(t)
	producerSHA := h.must(producer, "git", "rev-parse", "HEAD")
	producerTree := h.must(producer, "git", "rev-parse", "HEAD^{tree}")
	listing := h.must(producer, h.goBin, "list", "-deps", "-f", "{{.Dir}}", "./internal/release")
	dirs := map[string]bool{}
	for _, dir := range strings.Fields(listing) {
		if strings.HasPrefix(dir, producer+string(filepath.Separator)) {
			rel, e := filepath.Rel(producer, dir)
			if e != nil {
				t.Fatal(e)
			}
			dirs[filepath.ToSlash(rel)] = true
		}
	}
	closure := rsTrackedInventory(h, producer, producerSHA, func(path string) bool {
		return path == "go.mod" || path == "go.sum" || (dirs[filepath.ToSlash(filepath.Dir(path))] && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go")) || strings.HasPrefix(path, "scripts/release/") || strings.HasPrefix(path, "tools/releasepreflight/")
	})
	testFiles := rsTrackedInventory(h, producer, producerSHA, func(path string) bool {
		return path == "internal/release/consumable_test.go" || path == "internal/release/consumable_injection_test.go" || path == "internal/release/pack_test.go"
	})
	if len(closure) == 0 || len(testFiles) == 0 {
		t.Fatal("HARNESS_NOT_EXECUTED: empty actual producer closure/test identity")
	}
	out, errout, rc := h.run(producer, nil, os.Args[0], "-test.run=^TestConsumabilityGate", "-test.v", "-test.timeout=110s")
	if rc != 0 || bytes.Count(out, []byte("--- PASS: TestConsumabilityGate")) != 4 {
		t.Fatalf("HARNESS_NOT_EXECUTED: real existing regression rc%d: %s %s", rc, out, errout)
	}
	outRef := f.fileRef("actual-regression.stdout", out)
	errRef := f.fileRef("actual-regression.stderr", errout)
	f.proofRun = map[string]any{"command": []string{os.Args[0], "-test.run=^TestConsumabilityGate", "-test.v", "-test.timeout=110s"}, "cwd": producer, "runtimes": []any{map[string]any{"tool": "go", "version": runtime.Version()}}, "exit_code": rc, "declared": 4, "executed": 4, "outputs": []any{outRef, errRef}}
	f.proofTest = map[string]any{"repository": "PRO-Robotech/kacho", "revision": producerSHA, "files": testFiles}
	f.proofSource = map[string]any{"repository": "PRO-Robotech/kacho", "revision": producerSHA, "files": closure}
	machine := f.proof("TestConsumabilityGateStaysSilentOnALegitimateTwin", f.proofSource)
	regression := f.proof("TestConsumabilityGateRedOnAnUntrackedSource", f.proofSource)
	producerRecord := map[string]any{"repository": "PRO-Robotech/kacho", "root": producer, "commit": producerSHA, "tree": producerTree, "executable_closure": closure, "tool_versions": []any{map[string]any{"tool": "go", "version": runtime.Version()}, map[string]any{"tool": "git", "version": h.must(h.root, "git", "--version")}}, "machine_proofs": []any{machine}, "regression_proofs": []any{regression}}
	reviewSubject := rsCloneJSON(producerRecord)
	delete(reviewSubject, "root")
	reviewDigest := rsSHA(rsCanonicalJSON(t, reviewSubject))
	reviewBody := "SYNTHETIC FIXTURE PRIOR PRODUCER REVIEW ONLY\nSubject-SHA256: " + reviewDigest + "\nNo live T5 verdict or execution permission.\n"
	reviewCoordinate := map[string]any{"repository": "PRO-Robotech/kacho", "issue_number": 2588, "comment_id": 989999, "author_login": "ci-rs-fixture-reviewer", "body_sha256": rsSHA([]byte(reviewBody))}
	reviewAPI := "https://api.github.com/repos/PRO-Robotech/kacho/issues/comments/989999"
	reviewReadback := f.fileRef("producer-review-readback.json", rsCanonicalJSON(t, map[string]any{"id": 989999, "url": reviewAPI, "user": map[string]any{"login": "ci-rs-fixture-reviewer"}, "body": reviewBody}))
	f.http = append(f.http, rsHTTPFixture{"GET", reviewAPI, 200, reviewReadback["path"].(string), reviewReadback["sha256"].(string)})
	producerRecord["review"] = f.fileRef("producer-scoped-review.json", rsCanonicalJSON(t, map[string]any{"schema_version": 1, "kind": "producer-review", "subject_sha256": reviewDigest, "outcome": "GREEN", "reviewer": "ci-rs-fixture-reviewer", "independence_record": f.fileRef("producer-review-independence.json", []byte("SYNTHETIC FIXTURE ONLY: ci-rs-fixture-reviewer differs from ci-rs-fixture-author\n")), "authority_coordinate": reviewCoordinate}))
	projection := rsCloneJSON(producerRecord)
	delete(projection, "root")
	producerDigest := rsSHA(rsCanonicalJSON(t, projection))
	body := "SYNTHETIC FIXTURE EXECUTION BINDING ONLY\nSubject-SHA256: " + producerDigest + "\nNo live release is authorized.\n"
	number := 990000
	coord := map[string]any{"repository": "PRO-Robotech/kacho", "issue_number": 2588, "comment_id": number, "author_login": "ci-rs-fixture-root", "body_sha256": rsSHA([]byte(body))}
	api := fmt.Sprintf("https://api.github.com/repos/PRO-Robotech/kacho/issues/comments/%d", number)
	readback := f.fileRef("producer-execution-readback.json", rsCanonicalJSON(t, map[string]any{"id": number, "url": api, "user": map[string]any{"login": "ci-rs-fixture-root"}, "body": body}))
	f.http = append(f.http, rsHTTPFixture{"GET", api, 200, readback["path"].(string), readback["sha256"].(string)})
	producerRecord["authorization"] = f.fileRef("producer-execution-authority.json", rsCanonicalJSON(t, map[string]any{"schema_version": 1, "kind": "producer-execution", "subject_sha256": producerDigest, "coordinate": coord, "readback": readback}))
	f.manifest["producer_execution"] = producerRecord
	return f
}

func rsRunCandidateCases(t *testing.T) {
	f := rsPrepareCandidate(t)
	h := f.h
	// Independent Python serializer verifies the exact routine bytes, including
	// characters which encoding/json may optionally escape for JavaScript safety.
	for _, path := range []string{"first/value.go", "данные/é<&>.go", "unicode/line\u2028break.go"} {
		value := []any{map[string]any{"path": path, "mode": "100644", "sha256": strings.Repeat("a", 64)}}
		arg, _ := json.Marshal(value)
		out, stderr, rc := h.run(h.root, nil, "python3", "-c", "import json,sys;sys.stdout.buffer.write(json.dumps(json.loads(sys.argv[1]),sort_keys=True,separators=(',',':'),ensure_ascii=False).encode())", string(arg))
		if rc != 0 || !bytes.Equal(out, rsCanonicalJSON(t, value)) {
			t.Fatalf("HARNESS_NOT_EXECUTED: canonical fixture serializer mismatch %q: %s", path, stderr)
		}
	}
	cases := rsCandidateCases(f)
	ledger := []map[string]any{}
	var componentTwin, ownedTwin rsCandidateCase
	for _, c := range cases {
		if c.Name == "full-prerelease-proofs-postrelease-pending" {
			componentTwin = c
		}
		if c.Name == "input-includes-identical-owned-file" {
			ownedTwin = c
		}
	}
	for _, c := range cases {
		twin := cases[0]
		if strings.Contains(c.Scenario, "CI-RS-21") {
			twin = componentTwin
		}
		if c.Name == "NP-only-projection-insufficient" {
			for _, known := range cases {
				if known.Name == "complete-CI-NP-1-only" {
					twin = known
				}
			}
		}
		if strings.HasPrefix(c.Name, "receiving-owned-") {
			twin = ownedTwin
		}
		if c.Outcome == "GREEN" {
			twin = c
		}
		manifestChanges := rsChangedJSON(rsCloneJSON(twin.Manifest), rsCloneJSON(c.Manifest), "$")
		httpChanges := rsChangedJSON(rsCloneJSON(map[string]any{"http": twin.HTTP}), rsCloneJSON(map[string]any{"http": c.HTTP}), "$")
		raw := rsCanonicalJSON(t, c.Manifest)
		h.save(c.Name+".manifest.json", raw)
		ledger = append(ledger, map[string]any{"name": c.Name, "scenario": c.Scenario, "changed_fact": c.Axis, "lawful_twin": twin.Name, "computed_manifest_fields": manifestChanges, "computed_http_fields": httpChanges, "changed_origin_fixture": twin.Origin != c.Origin, "expected_outcome": c.Outcome, "expected_reason": c.Reason, "expected_exit": map[string]int{"GREEN": 0, "RED": 1, "NOT_EXECUTED": 3}[c.Outcome], "candidate_revision": c.Revision, "manifest_sha256": rsSHA(raw), "forbidden_effects": []string{"branch", "pr", "merge", "tag", "release-note"}, "sut_invocations": 0})
	}
	raw, _ := json.MarshalIndent(ledger, "", "  ")
	h.save("candidate-case-ledger.json", append(raw, '\n'))
	h.must(h.root, "python3", "-c", "import jsonschema; print(jsonschema.Draft202012Validator.__name__)")
	if _, err := parser.ParseFile(token.NewFileSet(), "ci_rs_bridge_test.go", rsCandidateBridgeSource(t), parser.AllErrors); err != nil {
		t.Fatalf("HARNESS_NOT_EXECUTED: candidate bridge syntax: %v", err)
	}
	missing := rsSupplySymbols(h, []string{"RunSupplyPreflight", "SupplyDependencies", "SupplyCommand", "SupplyCommandResult"})
	if len(missing) > 0 {
		raw, _ := json.MarshalIndent(map[string]any{"holder_outcome": "CAPABILITY_ABSENT", "prepared_cases": len(cases), "missing_symbols": missing, "sut_invocations": 0, "sut_semantic_decisions": 0, "synthetic_authority_scope": "fixture-only; no live NP/DT/T5 permission"}, "", "  ")
		h.save("capability.json", append(raw, '\n'))
		t.Fatalf("CAPABILITY_ABSENT: %d real prepared candidate cases; missing%v; sut_invocations=0", len(cases), missing)
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

const rsNotice2590Gzip = "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"
