// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package release

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type supplyEvidence struct {
	engine    *supplyEngine
	candidate supplyCandidate
	revision  string
	roots     map[string]string
	trees     map[string]map[string]supplyTrackedFile
}

// Reads retry within one deadline; no retry resets the total network budget.
func (e *supplyEngine) readHTTP(address, reason string) ([]byte, *supplyFailure) {
	ctx, cancel := context.WithTimeout(e.ctx, time.Duration(e.manifest.NetworkSeconds)*time.Second)
	defer cancel()
	transport := e.deps.HTTP
	if transport == nil {
		client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		transport = func(_ context.Context, r *http.Request) (*http.Response, error) { return client.Do(r) }
	}
	for attempt := 0; attempt < 3; attempt++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
		if err != nil {
			return nil, supplyUnavailable(reason)
		}
		request.Header.Set("Accept", "application/json, application/octet-stream")
		response, callErr := transport(ctx, request)
		var body []byte
		var readErr error
		status := 0
		if response != nil {
			status = response.StatusCode
			if response.Body != nil {
				body, readErr = io.ReadAll(response.Body)
				if closeErr := response.Body.Close(); readErr == nil {
					readErr = closeErr
				}
			} else {
				readErr = io.ErrUnexpectedEOF
			}
		}
		if ctx.Err() != nil {
			return nil, supplyUnavailable("BUDGET_EXHAUSTED")
		}
		if callErr == nil && readErr == nil && status == http.StatusOK {
			return body, nil
		}
		if callErr == nil && readErr == nil && status > 0 && status < 500 && status != http.StatusTooManyRequests {
			return nil, supplyUnavailable(reason)
		}
		if attempt < 2 {
			delay := time.Duration(attempt+1) * time.Second
			if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < delay {
				delay = time.Until(deadline)
			}
			if delay <= 0 {
				return nil, supplyUnavailable("BUDGET_EXHAUSTED")
			}
			if e.deps.Sleep != nil {
				if e.deps.Sleep(ctx, delay) != nil {
					return nil, supplyUnavailable("BUDGET_EXHAUSTED")
				}
			} else {
				timer := time.NewTimer(delay)
				select {
				case <-ctx.Done():
					timer.Stop()
					return nil, supplyUnavailable("BUDGET_EXHAUSTED")
				case <-timer.C:
				}
			}
		}
	}
	return nil, supplyUnavailable(reason)
}

func supplyReadRef(value any, bad string) ([]byte, *supplyFailure) {
	ref, ok := supplyObject(value, "path", "sha256")
	if !ok || !supplyAbsolute(supplyString(ref["path"])) || !supplySHA256.MatchString(supplyString(ref["sha256"])) {
		return nil, supplyRed(bad)
	}
	name := supplyString(ref["path"])
	info, err := os.Lstat(name)
	if err != nil {
		return nil, supplyUnavailable("SOURCE_UNAVAILABLE")
	}
	if !info.Mode().IsRegular() {
		return nil, supplyRed(bad)
	}
	raw, err := os.ReadFile(name)
	if err != nil {
		return nil, supplyUnavailable("SOURCE_UNAVAILABLE")
	}
	if supplyDigest(raw) != supplyString(ref["sha256"]) {
		return nil, supplyRed(bad)
	}
	return raw, nil
}

func supplyReadRecord(value any, bad string, keys ...string) (map[string]any, *supplyFailure) {
	raw, failure := supplyReadRef(value, bad)
	if failure != nil {
		return nil, failure
	}
	object, ok := supplyJSON(raw)
	if !ok {
		return nil, supplyRed(bad)
	}
	object, ok = supplyObject(object, keys...)
	if !ok {
		return nil, supplyRed(bad)
	}
	if _, ok := supplyInteger(object["schema_version"], 1, 1); !ok {
		return nil, supplyRed(bad)
	}
	return object, nil
}

func supplyRuntimes(value any) bool {
	list, ok := value.([]any)
	if !ok || len(list) == 0 {
		return false
	}
	seen := map[string]bool{}
	for _, raw := range list {
		item, ok := supplyObject(raw, "tool", "version")
		if !ok {
			return false
		}
		name, version := supplyString(item["tool"]), supplyString(item["version"])
		if strings.TrimSpace(name) == "" || strings.TrimSpace(version) == "" || seen[name] {
			return false
		}
		seen[name] = true
	}
	return true
}

func (v *supplyEvidence) coordinate(value any, captured []byte, bad string) *supplyFailure {
	coordinate, ok := supplyObject(value, "repository", "issue_number", "comment_id", "author_login", "body_sha256")
	if !ok {
		return supplyRed(bad)
	}
	repo, actor := supplyString(coordinate["repository"]), supplyString(coordinate["author_login"])
	issue, issueOK := supplyInteger(coordinate["issue_number"], 1, 1<<53-1)
	id, idOK := supplyInteger(coordinate["comment_id"], 1, 1<<53-1)
	digest := supplyString(coordinate["body_sha256"])
	if !supplyRepository.MatchString(repo) || !issueOK || !idOK || strings.TrimSpace(actor) == "" || !supplySHA256.MatchString(digest) {
		return supplyRed(bad)
	}
	address := fmt.Sprintf("https://api.github.com/repos/%s/issues/comments/%d", repo, id)
	valid := func(raw []byte) bool {
		body, ok := supplyJSON(raw)
		if !ok {
			return false
		}
		got, ok := supplyInteger(body["id"], 1, 1<<53-1)
		if !ok || got != id {
			return false
		}
		user, ok := body["user"].(map[string]any)
		if !ok || supplyString(user["login"]) != actor {
			return false
		}
		text, ok := body["body"].(string)
		if !ok || supplyDigest([]byte(text)) != digest {
			return false
		}
		if supplyString(body["url"]) != address {
			return false
		}
		if html, exists := body["html_url"]; exists && supplyString(html) != fmt.Sprintf("https://github.com/%s/issues/%d#issuecomment-%d", repo, issue, id) {
			return false
		}
		if issueURL, exists := body["issue_url"]; exists && supplyString(issueURL) != fmt.Sprintf("https://api.github.com/repos/%s/issues/%d", repo, issue) {
			return false
		}
		return true
	}
	if captured != nil && !valid(captured) {
		return supplyRed(bad)
	}
	live, failure := v.engine.readHTTP(address, "SOURCE_UNAVAILABLE")
	if failure != nil {
		return failure
	}
	if !valid(live) {
		return supplyRed(bad)
	}
	return nil
}

func (v *supplyEvidence) source(value any, bad string) ([]supplySourceFile, *supplyFailure) {
	source, ok := supplyObject(value, "repository", "revision", "files")
	if !ok {
		return nil, supplyRed(bad)
	}
	repo, revision := supplyString(source["repository"]), supplyString(source["revision"])
	files, ok := supplyFileEntries(source["files"], false)
	if !ok || len(files) == 0 || !supplyRepository.MatchString(repo) || !supplySHA.MatchString(revision) {
		return nil, supplyRed(bad)
	}
	key := repo + "@" + revision
	actual, loaded := v.trees[key]
	if !loaded {
		root := v.roots[repo]
		if root == "" {
			remote, failure := v.engine.pinRemote(repo, 100+len(v.roots))
			if failure != nil {
				return nil, failure
			}
			root = remote.Root
			v.roots[repo] = root
		}
		tracked, failure := v.engine.tracked(root, revision)
		if failure != nil {
			return nil, failure
		}
		actual = supplyTrackedMap(tracked)
		v.trees[key] = actual
	}
	for _, file := range files {
		if !supplyFileMatches(file, actual[file.Path]) {
			return nil, supplyRed(bad)
		}
	}
	return files, nil
}

func (v *supplyEvidence) authority(value any, kind, digest, bad string, coordinate any) *supplyFailure {
	record, failure := supplyReadRecord(value, bad, "schema_version", "kind", "subject_sha256", "coordinate", "readback")
	if failure != nil {
		return failure
	}
	if supplyString(record["kind"]) != kind || supplyString(record["subject_sha256"]) != digest {
		return supplyRed(bad)
	}
	if coordinate != nil && !bytes.Equal(supplyCanonical(coordinate), supplyCanonical(record["coordinate"])) {
		return supplyRed(bad)
	}
	readback, failure := supplyReadRef(record["readback"], bad)
	if failure != nil {
		return failure
	}
	return v.coordinate(record["coordinate"], readback, bad)
}

func (v *supplyEvidence) proof(value any, bad string) (string, []supplySourceFile, *supplyFailure) {
	proof, ok := supplyObject(value, "subject", "subject_sha256", "review", "authority")
	if !ok {
		return "", nil, supplyRed(bad)
	}
	subject, ok := supplyObject(proof["subject"], "predicate_id", "source", "test", "runs")
	if !ok {
		return "", nil, supplyRed(bad)
	}
	id := supplyString(subject["predicate_id"])
	digest := supplyDigest(supplyCanonical(subject))
	if strings.TrimSpace(id) == "" || supplyString(proof["subject_sha256"]) != digest {
		return id, nil, supplyRed(bad)
	}
	files, failure := v.source(subject["source"], bad)
	if failure != nil {
		return id, nil, failure
	}
	if _, failure = v.source(subject["test"], bad); failure != nil {
		return id, nil, failure
	}
	runs, ok := subject["runs"].([]any)
	if !ok || len(runs) == 0 {
		return id, nil, supplyRed(bad)
	}
	for _, raw := range runs {
		run, ok := supplyObject(raw, "command", "cwd", "runtimes", "exit_code", "declared", "executed", "outputs")
		if !ok {
			return id, nil, supplyRed(bad)
		}
		command, ok := run["command"].([]any)
		if !ok || len(command) == 0 {
			return id, nil, supplyRed(bad)
		}
		for _, arg := range command {
			if _, ok := arg.(string); !ok {
				return id, nil, supplyRed(bad)
			}
		}
		if supplyString(command[0]) == "" || !supplyAbsolute(supplyString(run["cwd"])) || !supplyRuntimes(run["runtimes"]) {
			return id, nil, supplyRed(bad)
		}
		declared, dok := supplyInteger(run["declared"], 1, 1<<53-1)
		executed, eok := supplyInteger(run["executed"], 1, 1<<53-1)
		if _, ok := supplyInteger(run["exit_code"], -1<<31, 1<<31-1); !ok || !dok || !eok || declared != executed {
			return id, nil, supplyRed(bad)
		}
		outputs, ok := run["outputs"].([]any)
		if !ok || len(outputs) == 0 {
			return id, nil, supplyRed(bad)
		}
		for _, output := range outputs {
			if _, failure := supplyReadRef(output, bad); failure != nil {
				return id, nil, failure
			}
		}
	}
	review, failure := supplyReadRecord(proof["review"], bad, "schema_version", "kind", "predicate_id", "subject_sha256", "outcome", "reviewer", "independence_record", "authority_coordinate")
	if failure != nil {
		return id, nil, failure
	}
	if supplyString(review["kind"]) != "scoped-review" || supplyString(review["predicate_id"]) != id || supplyString(review["subject_sha256"]) != digest || supplyString(review["outcome"]) != "GREEN" || strings.TrimSpace(supplyString(review["reviewer"])) == "" {
		return id, nil, supplyRed(bad)
	}
	if _, failure = supplyReadRef(review["independence_record"], bad); failure != nil {
		return id, nil, failure
	}
	failure = v.authority(proof["authority"], "component-review", digest, bad, review["authority_coordinate"])
	return id, files, failure
}

func (v *supplyEvidence) producerIdentity() *supplyFailure {
	p := v.candidate.Producer
	if supplyString(p["repository"]) != "PRO-Robotech/kacho" {
		return supplyRed("REPOSITORY_MISMATCH")
	}
	if _, ok := supplyObject(p, "repository", "root", "commit", "tree", "executable_closure", "tool_versions", "machine_proofs", "regression_proofs", "review", "authorization"); !ok {
		return supplyRed("INVALID_CONFIRMATION")
	}
	root, commit, tree := supplyString(p["root"]), supplyString(p["commit"]), supplyString(p["tree"])
	if !supplyAbsolute(root) || !supplySHA.MatchString(commit) || !supplySHA.MatchString(tree) || !supplyRuntimes(p["tool_versions"]) {
		return supplyRed("INVALID_CONFIRMATION")
	}
	if failure := v.engine.identify(root, "PRO-Robotech/kacho", commit); failure != nil {
		return failure
	}
	v.roots["PRO-Robotech/kacho"] = root
	closure, ok := supplyFileEntries(p["executable_closure"], false)
	if !ok || len(closure) == 0 {
		return supplyRed("INVALID_CONFIRMATION")
	}
	// Actual frozen bytes are an input property, checked before the recorded
	// subject; dirt cannot be misclassified as a stale approval of unchanged bytes.
	head, failure := v.engine.git(root, nil, "rev-parse", "HEAD", "HEAD^{tree}")
	if failure != nil {
		return failure
	}
	fields := strings.Fields(string(head))
	if len(fields) != 2 || fields[0] != commit || fields[1] != tree {
		return supplyRed("INPUT_CHANGED")
	}
	status, failure := v.engine.git(root, nil, "status", "--porcelain", "--untracked-files=normal")
	if failure != nil {
		return failure
	}
	if len(bytes.TrimSpace(status)) != 0 {
		return supplyRed("INPUT_CHANGED")
	}
	tracked, failure := v.engine.tracked(root, commit)
	if failure != nil {
		return failure
	}
	actual := supplyTrackedMap(tracked)
	v.trees["PRO-Robotech/kacho@"+commit] = actual
	for _, file := range closure {
		if !supplyFileMatches(file, actual[file.Path]) {
			return supplyRed("INPUT_CHANGED")
		}
		disk := filepath.Join(root, filepath.FromSlash(file.Path))
		info, err := os.Lstat(disk)
		if err != nil {
			return supplyUnavailable("SOURCE_UNAVAILABLE")
		}
		if !info.Mode().IsRegular() {
			return supplyRed("INPUT_CHANGED")
		}
		mode := "100644"
		if info.Mode()&0111 != 0 {
			mode = "100755"
		}
		raw, err := os.ReadFile(disk)
		if err != nil {
			return supplyUnavailable("SOURCE_UNAVAILABLE")
		}
		if mode != file.Mode || supplyDigest(raw) != file.SHA256 {
			return supplyRed("INPUT_CHANGED")
		}
	}
	if failure = v.producerClosure(root, closure, actual); failure != nil {
		return failure
	}
	for _, key := range []string{"machine_proofs", "regression_proofs"} {
		proofs, ok := p[key].([]any)
		if !ok || len(proofs) == 0 {
			return supplyRed("INVALID_CONFIRMATION")
		}
		seen := map[string]bool{}
		for _, proof := range proofs {
			id, _, failure := v.proof(proof, "INVALID_CONFIRMATION")
			if failure != nil {
				return failure
			}
			if seen[id] {
				return supplyRed("INVALID_CONFIRMATION")
			}
			seen[id] = true
		}
	}
	inner := map[string]any{}
	for _, key := range []string{"repository", "commit", "tree", "executable_closure", "tool_versions", "machine_proofs", "regression_proofs"} {
		inner[key] = p[key]
	}
	r := supplyDigest(supplyCanonical(inner))
	review, failure := supplyReadRecord(p["review"], "INVALID_CONFIRMATION", "schema_version", "kind", "subject_sha256", "outcome", "reviewer", "independence_record", "authority_coordinate")
	if failure != nil {
		return failure
	}
	if supplyString(review["kind"]) != "producer-review" || supplyString(review["subject_sha256"]) != r || supplyString(review["outcome"]) != "GREEN" || strings.TrimSpace(supplyString(review["reviewer"])) == "" {
		return supplyRed("INVALID_CONFIRMATION")
	}
	if _, failure = supplyReadRef(review["independence_record"], "INVALID_CONFIRMATION"); failure != nil {
		return failure
	}
	if failure = v.coordinate(review["authority_coordinate"], nil, "INVALID_CONFIRMATION"); failure != nil {
		return failure
	}
	inner["review"] = p["review"]
	return v.authority(p["authorization"], "producer-execution", supplyDigest(supplyCanonical(inner)), "INVALID_CONFIRMATION", nil)
}

// This is the executing release producer's local source closure, not a copy
// of its proof ledger: Go derives module-local dependencies; tracked release
// scripts and their data, the CLI, go.mod and go.sum are included as well.
func (v *supplyEvidence) producerClosure(root string, closure []supplySourceFile, actual map[string]supplyTrackedFile) *supplyFailure {
	result, failure := v.engine.command(SupplyCommand{Program: filepath.Join(runtime.GOROOT(), "bin", "go"), Args: []string{"list", "-deps", "-f", "{{.Dir}}", "./internal/release"}, Dir: root, Env: supplyEnvironment()}, 600*time.Second)
	if failure != nil {
		return failure
	}
	if result.ExitCode != 0 {
		return supplyUnavailable("SOURCE_UNAVAILABLE")
	}
	dirs := map[string]bool{}
	for _, dir := range strings.Fields(string(result.Stdout)) {
		rel, err := filepath.Rel(root, dir)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			dirs[filepath.ToSlash(rel)] = true
		}
	}
	if len(dirs) == 0 {
		return supplyUnavailable("SOURCE_UNAVAILABLE")
	}
	declared := map[string]bool{}
	for _, file := range closure {
		declared[file.Path] = true
	}
	for name := range actual {
		required := name == "go.mod" || name == "go.sum" || strings.HasPrefix(name, "scripts/release/") || strings.HasPrefix(name, "tools/releasepreflight/") || (dirs[filepath.ToSlash(filepath.Dir(name))] && strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go"))
		if required && !declared[name] {
			return supplyRed("INPUT_CHANGED")
		}
	}
	return nil
}
