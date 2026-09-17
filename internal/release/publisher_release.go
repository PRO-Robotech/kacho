// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package release

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
	"golang.org/x/mod/sumdb/dirhash"
	modzip "golang.org/x/mod/zip"
)

func (p *supplyPublisher) ancestry(target, point string) *supplyFailure {
	if !supplySHA.MatchString(target) {
		return supplyRed("INPUT_INVALID")
	}
	// A confirmed PR merge may cease to be advertised after main changes.
	// Read that exact object before asking Git for a semantic ancestry verdict.
	if _, f := p.transport("fetch", "--quiet", "--no-tags", "--no-recurse-submodules", "--no-write-fetch-head", "origin", target); f != nil {
		return f
	}
	if _, f := p.e.git(p.root, nil, "cat-file", "commit", target); f != nil {
		return f
	}
	if _, f := p.transport("fetch", "--quiet", "--force", "--prune", "--no-recurse-submodules", "origin", "+refs/heads/*:refs/remotes/origin/*", "+refs/tags/*:refs/tags/*"); f != nil {
		return f
	}
	main, f := p.resolveLocal("refs/remotes/origin/main")
	if f != nil {
		return f
	}
	result, f := p.e.pinQuery(p.root, "merge-base", "--is-ancestor", target, main)
	if f != nil {
		return f
	}
	p.snapshot(point, main, result.ExitCode == 0)
	if result.ExitCode == 1 {
		return supplyRed("TARGET_NOT_ON_MAIN")
	}
	if result.ExitCode != 0 {
		return supplyUnavailable("SOURCE_UNAVAILABLE")
	}
	p.pass("main-ancestry", []string{target, main}, 1)
	return nil
}
func (p *supplyPublisher) acceptMerge(pr *supplyPR, release bool) *supplyFailure {
	if !pr.Merged || !supplySHA.MatchString(pr.MergeSHA) {
		return supplyUnavailable("SOURCE_UNAVAILABLE")
	}
	p.target = pr.MergeSHA
	if release && p.inv.Phase == "release" && p.inv.Landed != pr.MergeSHA {
		return supplyRed("TARGET_NOT_ON_MAIN")
	}
	p.report.document["tag_target_sha"] = p.target
	if f := p.ancestry(p.target, "pre-write"); f != nil {
		return f
	}
	tree, f := p.resolveLocal(p.target + "^{tree}")
	if f != nil {
		return f
	}
	if tree != p.tree {
		return supplyRed("CONTENT_MISMATCH")
	}
	if f = p.compatibility(p.target); f != nil {
		p.fail("compatibility", f)
		return f
	}
	if release {
		if f = p.checks(p.target); f != nil {
			p.fail("required-checks", f)
			return f
		}
	} else if len(p.policy.Required) > 0 {
		// Resume independently asks the original accepted head checks again.
		if f = p.checks(pr.Head.SHA); f != nil {
			p.fail("required-checks", f)
			return f
		}
	}
	p.pass("pr", []string{"merged", p.c.Repository, fmt.Sprint(pr.Number), pr.Head.SHA, pr.MergeSHA}, 1)
	p.stage("MERGED_VERIFIED")
	return nil
}
func (p *supplyPublisher) release(probe bool) *supplyFailure {
	ref := "refs/heads/" + p.branch()
	sha, f := p.readRef(ref)
	if f != nil {
		return f
	}
	if sha == "" {
		return supplyUnavailable("SOURCE_UNAVAILABLE")
	}
	if sha != p.revision {
		p.refEffect("branch", "CONFLICT", ref, p.revision, sha)
		return supplyRed("EFFECT_IDENTITY_CONFLICT")
	}
	p.refEffect("branch", "PRESENT", ref, p.revision, sha)
	p.stage("BRANCH_PRESENT")
	pr, f := p.findPR()
	if f != nil {
		if f.Outcome == "RED" {
			p.prEffect("CONFLICT", pr)
		}
		return f
	}
	if pr == nil {
		return supplyUnavailable("SOURCE_UNAVAILABLE")
	}
	p.pr = pr
	p.prEffect("PRESENT", pr)
	p.stage("PR_OPEN")
	if !pr.Merged {
		return supplyUnavailable("SOURCE_UNAVAILABLE")
	}
	p.mergeEffect("PRESENT", pr)
	p.stage("MERGE_PRESENT")
	if f = p.acceptMerge(pr, true); f != nil {
		return f
	}
	if probe {
		tag, f := p.readRef("refs/tags/" + p.c.Version)
		if f != nil {
			return f
		}
		if tag == "" {
			return supplyUnavailable("SOURCE_UNAVAILABLE")
		}
		if tag != p.target {
			return supplyRed("TAG_IDENTITY_CONFLICT")
		}
		p.stage("TAG_PRESENT")
		p.pass("tag", []string{p.c.Version, p.target}, 1)
		if f = p.probeArchive(); f != nil {
			return f
		}
		return nil
	}
	if f = p.createTag(); f != nil {
		return f
	}
	p.pass("tag", []string{p.c.Version, p.target}, 1)
	if f = p.ancestry(p.target, "post-write"); f != nil {
		return f
	}
	if f = p.note(); f != nil {
		return f
	}
	return nil
}

type supplyNote struct {
	ID     int    `json:"id"`
	Tag    string `json:"tag_name"`
	Target string `json:"target_commitish"`
}

func (p *supplyPublisher) readNote() (*supplyNote, *supplyFailure) {
	status, raw, f := p.http("GET", p.api("/releases/tags/"+url.PathEscape(p.c.Version)), nil)
	if f != nil {
		return nil, f
	}
	if status == 404 {
		return nil, nil
	}
	var n supplyNote
	if status != 200 || json.Unmarshal(raw, &n) != nil || n.ID <= 0 {
		return nil, supplyUnavailable("SOURCE_UNAVAILABLE")
	}
	if n.Tag != p.c.Version || n.Target != p.target {
		return &n, supplyRed("EFFECT_IDENTITY_CONFLICT")
	}
	return &n, nil
}
func (p *supplyPublisher) noteEffect(state string, n *supplyNote) {
	var id, sha any
	if n != nil {
		id = n.ID
		if supplySHA.MatchString(n.Target) {
			sha = n.Target
		}
	}
	p.effect("release-note", state, map[string]any{"tag_ref": "refs/tags/" + p.c.Version, "expected_sha": p.target, "sha": sha, "release_id": id})
}
func (p *supplyPublisher) note() *supplyFailure {
	note, f := p.readNote()
	if f != nil {
		if f.Outcome == "RED" {
			p.noteEffect("CONFLICT", note)
		}
		return f
	}
	if note != nil {
		p.noteEffect("PRESENT", note)
		return nil
	}
	if f = p.fresh(false); f != nil {
		return f
	}
	if f = p.ancestry(p.target, "pre-write"); f != nil {
		return f
	}
	tag, f := p.readRef("refs/tags/" + p.c.Version)
	if f != nil {
		return f
	}
	if tag != p.target {
		return supplyRed("TAG_IDENTITY_CONFLICT")
	}
	p.noteEffect("UNKNOWN", nil)
	status, _, writeFailure := p.http("POST", p.api("/releases"), map[string]any{"tag_name": p.c.Version, "target_commitish": p.target, "name": p.c.Version, "body": p.marker(), "draft": false, "prerelease": semver.Prerelease(p.c.Version) != ""})
	note, f = p.readNote()
	if f != nil {
		if f.Outcome == "RED" {
			p.noteEffect("CONFLICT", note)
			return f
		}
		return supplyUnavailable("WRITE_OUTCOME_UNKNOWN")
	}
	if note == nil {
		if supplyTerminalWrite(status, writeFailure) {
			p.noteEffect("ABSENT", nil)
			return supplyRed("REMOTE_WRITE_REJECTED")
		}
		return supplyUnavailable("WRITE_OUTCOME_UNKNOWN")
	}
	p.noteEffect("PRESENT", note)
	return nil
}

func (p *supplyPublisher) probeArchive() *supplyFailure {
	escaped, err := module.EscapePath(p.c.ModulePath)
	if err != nil {
		return supplyRed("INPUT_INVALID")
	}
	version, err := module.EscapeVersion(p.c.Version)
	if err != nil {
		return supplyRed("VERSION_INVALID")
	}
	prefix := "https://proxy.golang.org/" + escaped + "/@v/" + version
	read := func(suffix string) ([]byte, *supplyFailure) {
		status, b, f := p.http("GET", prefix+suffix, nil)
		if f != nil {
			if f.Reason == "BUDGET_EXHAUSTED" {
				return nil, f
			}
			return nil, supplyUnavailable("PROXY_UNAVAILABLE")
		}
		if status != 200 {
			return nil, supplyUnavailable("PROXY_UNAVAILABLE")
		}
		return b, nil
	}
	info, f := read(".info")
	if f != nil {
		return f
	}
	var meta struct {
		Version string
		Time    time.Time
		Origin  *struct{ Hash, URL string }
	}
	if json.Unmarshal(info, &meta) != nil || meta.Version != p.c.Version || meta.Time.IsZero() {
		return supplyRed("PUBLISHED_ARCHIVE_MISMATCH")
	}
	if meta.Origin != nil && (meta.Origin.Hash != p.target || (meta.Origin.URL != "" && meta.Origin.URL != "https://github.com/"+p.c.Repository && meta.Origin.URL != "https://github.com/"+p.c.Repository+".git")) {
		return supplyRed("PUBLISHED_ARCHIVE_MISMATCH")
	}
	mod, f := read(".mod")
	if f != nil {
		return f
	}
	raw, f := read(".zip")
	if f != nil {
		return f
	}
	work := filepath.Join(p.e.work, "published")
	if os.Mkdir(work, 0700) != nil {
		return supplyUnavailable("HARNESS_UNAVAILABLE")
	}
	filename := filepath.Join(work, "published.zip")
	if os.WriteFile(filename, raw, 0600) != nil {
		return supplyUnavailable("HARNESS_UNAVAILABLE")
	}
	if _, err = modzip.CheckZip(module.Version{Path: p.c.ModulePath, Version: p.c.Version}, filename); err != nil {
		return supplyRed("PUBLISHED_ARCHIVE_MISMATCH")
	}
	sum, err := dirhash.HashZip(filename, dirhash.Hash1)
	if err != nil {
		return supplyRed("PUBLISHED_ARCHIVE_MISMATCH")
	}
	// The expected Go content checksum is derived independently from the exact
	// accepted Git tree, using the published module/version namespace.
	names := make([]string, 0, len(p.archive.Files))
	memberPrefix := p.c.ModulePath + "@" + p.c.Version + "/"
	for name := range p.archive.Files {
		names = append(names, memberPrefix+name)
	}
	expectedSum, err := dirhash.Hash1(names, func(name string) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(p.archive.Files[strings.TrimPrefix(name, memberPrefix)])), nil
	})
	if err != nil || sum != expectedSum {
		return supplyRed("PUBLISHED_ARCHIVE_MISMATCH")
	}
	archive, f := supplyReadModuleArchive(raw, p.c.ModulePath, p.c.Version)
	if f != nil {
		return supplyRed("PUBLISHED_ARCHIVE_MISMATCH")
	}
	if !bytes.Equal(mod, archive.Files["go.mod"]) || len(archive.Files) != len(p.archive.Files) {
		return supplyRed("PUBLISHED_ARCHIVE_MISMATCH")
	}
	for path, expected := range p.archive.Files {
		observed, exists := archive.Files[path]
		if !exists || !bytes.Equal(observed, expected) {
			return supplyRed("PUBLISHED_ARCHIVE_MISMATCH")
		}
	}
	verified, f := p.publicChecksum(archive, mod, info, sum)
	if f != nil {
		return f
	}
	// Published bytes get their own proxy/version/cache; no candidate ZIP is
	// substituted for this second consumer proof.
	engine := *p.e
	engine.work = work
	engine.version = p.c.Version
	dir := filepath.Join(work, "proxy", escaped, "@v")
	if os.MkdirAll(dir, 0700) != nil {
		return supplyUnavailable("HARNESS_UNAVAILABLE")
	}
	for path, b := range map[string][]byte{version + ".zip": raw, version + ".mod": mod, version + ".info": info, "list": []byte(p.c.Version + "\n")} {
		if os.WriteFile(filepath.Join(dir, path), b, 0600) != nil {
			return supplyUnavailable("HARNESS_UNAVAILABLE")
		}
	}
	engine.proxy = (&url.URL{Scheme: "file", Path: filepath.ToSlash(filepath.Join(work, "proxy"))}).String()
	r := supplyNewReport()
	engine.checkConsumers(p.revision, archive, r)
	p.absorb(r, map[string]bool{"consumer-census": true, "consumer-archive": true})
	if r.document["outcome"] != "GREEN" {
		return &supplyFailure{r.document["outcome"].(string), r.document["reason"].(string)}
	}
	if f = p.ancestry(p.target, "post-write"); f != nil {
		return f
	}
	p.pass("proxy", append([]string{p.c.ModulePath, p.c.Version, p.target, archive.Digest, sum, p.baselineSHA, p.baselineDigest}, verified...), len(archive.Files))
	p.stage("ARCHIVE_VERIFIED")
	return nil
}

// publicChecksum asks the ordinary Go verifier in a new empty cache. The
// private candidate proxy and its GOSUMDB=off cannot establish this predicate.
func (p *supplyPublisher) publicChecksum(expected supplyArchive, mod, info []byte, expectedSum string) ([]string, *supplyFailure) {
	unavailable := func() ([]string, *supplyFailure) { return nil, supplyUnavailable("PROXY_UNAVAILABLE") }
	mismatch := func() ([]string, *supplyFailure) { return nil, supplyRed("PUBLISHED_ARCHIVE_MISMATCH") }
	root := filepath.Join(p.e.work, "public-checksum")
	cache, consumer := filepath.Join(root, "cache"), filepath.Join(root, "consumer")
	for _, dir := range []string{cache, consumer, filepath.Join(root, "gopath")} {
		if os.MkdirAll(dir, 0700) != nil {
			return unavailable()
		}
	}
	if p.e.goBinary == "" || os.WriteFile(filepath.Join(consumer, "go.mod"), []byte("module ci-rs-public-probe.invalid/consumer\n\ngo 1.21\n"), 0600) != nil {
		return unavailable()
	}
	result, failure := p.e.command(SupplyCommand{Program: p.e.goBinary,
		Args: []string{"mod", "download", "-json", p.c.ModulePath + "@" + p.c.Version}, Dir: consumer,
		Env: supplyEnvironment("GOWORK=off", "GOENV=off", "GOFLAGS=", "GOTOOLCHAIN=local",
			"GOPROXY=https://proxy.golang.org", "GOSUMDB=sum.golang.org", "GOPRIVATE=", "GONOPROXY=", "GONOSUMDB=",
			"GOMODCACHE="+cache, "GOPATH="+filepath.Join(root, "gopath"))}, 600*time.Second)
	if failure != nil {
		if failure.Reason == "BUDGET_EXHAUSTED" {
			return nil, failure
		}
		return unavailable()
	}
	if result.ExitCode != 0 || result.Err != nil {
		return unavailable()
	}
	download, ok := supplyJSON(result.Stdout)
	if !ok || download["Error"] != nil {
		return unavailable()
	}
	if supplyString(download["Path"]) != p.c.ModulePath || supplyString(download["Version"]) != p.c.Version {
		return mismatch()
	}
	validOrigin := func(value any) bool {
		if value == nil {
			return true
		}
		origin, ok := value.(map[string]any)
		if !ok || supplyString(origin["Hash"]) != p.target {
			return false
		}
		u := supplyString(origin["URL"])
		return u == "https://github.com/"+p.c.Repository || u == "https://github.com/"+p.c.Repository+".git"
	}
	if !validOrigin(download["Origin"]) {
		return mismatch()
	}
	validSum := func(value any) (string, bool) {
		s, ok := value.(string)
		if !ok || !strings.HasPrefix(s, "h1:") {
			return "", false
		}
		b, err := base64.StdEncoding.Strict().DecodeString(strings.TrimPrefix(s, "h1:"))
		return s, err == nil && len(b) == 32 && "h1:"+base64.StdEncoding.EncodeToString(b) == s
	}
	sum, sumOK := validSum(download["Sum"])
	modSum, modSumOK := validSum(download["GoModSum"])
	if !sumOK || !modSumOK {
		return unavailable()
	}
	files := map[string][]byte{}
	paths := map[string]string{}
	for _, key := range []string{"Info", "GoMod", "Zip"} {
		name, ok := download[key].(string)
		if !ok || !filepath.IsAbs(name) {
			return mismatch()
		}
		rel, err := filepath.Rel(cache, name)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return mismatch()
		}
		st, err := os.Lstat(name)
		if err != nil {
			return unavailable()
		}
		if !st.Mode().IsRegular() {
			return mismatch()
		}
		resolved, err := filepath.EvalSymlinks(name)
		if err != nil {
			return unavailable()
		}
		rel, err = filepath.Rel(cache, resolved)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return mismatch()
		}
		files[key], err = os.ReadFile(name)
		if err != nil {
			return unavailable()
		}
		paths[key] = name
	}
	metadata, ok := supplyJSON(files["Info"])
	prior, priorOK := supplyJSON(info)
	if !ok || !priorOK {
		return unavailable()
	}
	observedTime, err := time.Parse(time.RFC3339Nano, supplyString(metadata["Time"]))
	priorTime, priorErr := time.Parse(time.RFC3339Nano, supplyString(prior["Time"]))
	if err != nil || priorErr != nil || !observedTime.Equal(priorTime) || supplyString(metadata["Version"]) != p.c.Version || !validOrigin(metadata["Origin"]) {
		return mismatch()
	}
	if _, err := modzip.CheckZip(module.Version{Path: p.c.ModulePath, Version: p.c.Version}, paths["Zip"]); err != nil {
		return mismatch()
	}
	actualSum, err := dirhash.HashZip(paths["Zip"], dirhash.Hash1)
	if err != nil || actualSum != sum || sum != expectedSum {
		return mismatch()
	}
	actualModSum, err := dirhash.Hash1([]string{"go.mod"}, func(string) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(files["GoMod"])), nil
	})
	if err != nil || actualModSum != modSum || !bytes.Equal(files["GoMod"], mod) {
		return mismatch()
	}
	archive, failure := supplyReadModuleArchive(files["Zip"], p.c.ModulePath, p.c.Version)
	if failure != nil || len(archive.Files) != len(expected.Files) {
		return mismatch()
	}
	for name, wanted := range expected.Files {
		if actual, exists := archive.Files[name]; !exists || !bytes.Equal(actual, wanted) {
			return mismatch()
		}
	}
	return []string{"public-go-checksum", p.e.goBinary, sum, modSum,
		supplyDigest(files["Info"]), supplyDigest(files["GoMod"]), supplyDigest(files["Zip"])}, nil
}

func (p *supplyPublisher) compatibility(target string) *supplyFailure {
	if semver.Compare(p.c.Version, p.c.Baseline) <= 0 {
		return supplyRed("VERSION_INVALID")
	}
	baseline, f := p.e.tracked(p.root, p.baselineSHA)
	if f != nil {
		return f
	}
	current, f := p.e.tracked(p.root, target)
	if f != nil {
		return f
	}
	oldProto, newProto := 0, 0
	// Count explicitly; neither the directory name nor the archive determines
	// whether a tracked contract domain exists.
	for _, file := range baseline {
		if strings.HasSuffix(file.Path, ".proto") {
			oldProto++
		}
	}
	for _, file := range current {
		if strings.HasSuffix(file.Path, ".proto") {
			newProto++
		}
	}
	if p.c.Repository == "PRO-Robotech/corelib" {
		modules := 0
		for _, file := range current {
			if filepath.Base(file.Path) != "go.mod" {
				continue
			}
			m, err := modfile.Parse(file.Path, file.Data, nil)
			if err != nil || m.Module == nil {
				return supplyRed("COMPATIBILITY_FAILED")
			}
			modules++
			for _, require := range m.Require {
				if strings.HasPrefix(require.Mod.Path, "github.com/PRO-Robotech/") {
					return supplyRed("COMPATIBILITY_FAILED")
				}
			}
		}
		if modules == 0 {
			return supplyUnavailable("SOURCE_UNAVAILABLE")
		}
	}
	if oldProto == 0 && newProto == 0 {
		return nil
	}
	// Reuse the legacy Buf subject only for its supported proto directory.
	// Unsupported layouts stay non-executed rather than acquiring a new policy.
	expectedVersion := ""
	for _, raw := range p.c.Producer["tool_versions"].([]any) {
		v := raw.(map[string]any)
		if supplyString(v["tool"]) == "buf" {
			expectedVersion = supplyString(v["version"])
		}
	}
	if expectedVersion == "" {
		return supplyUnavailable("SOURCE_UNAVAILABLE")
	}
	r, f := p.e.command(SupplyCommand{Program: "buf", Args: []string{"--version"}, Dir: p.root, Env: supplyEnvironment()}, time.Duration(p.c.NetworkSeconds)*time.Second)
	if f != nil {
		return f
	}
	if r.ExitCode != 0 || strings.TrimSpace(string(r.Stdout)) != expectedVersion {
		return supplyUnavailable("SOURCE_UNAVAILABLE")
	}
	dirs := []string{}
	for index, files := range [][]supplyTrackedFile{baseline, current} {
		dir, err := os.MkdirTemp(p.e.work, fmt.Sprintf("compat-%d-", index))
		if err != nil {
			return supplyUnavailable("HARNESS_UNAVAILABLE")
		}
		dirs = append(dirs, dir)
		config := false
		for _, file := range files {
			if strings.HasSuffix(file.Path, ".proto") && !strings.HasPrefix(file.Path, "proto/") {
				return supplyUnavailable("SOURCE_UNAVAILABLE")
			}
			if !strings.HasPrefix(file.Path, "proto/") {
				continue
			}
			if file.Path == "proto/buf.yaml" {
				config = true
			}
			dest := filepath.Join(dir, filepath.FromSlash(file.Path))
			if os.MkdirAll(filepath.Dir(dest), 0700) != nil || os.WriteFile(dest, file.Data, 0600) != nil {
				return supplyUnavailable("HARNESS_UNAVAILABLE")
			}
		}
		if !config {
			return supplyUnavailable("SOURCE_UNAVAILABLE")
		}
	}
	r, f = p.e.command(SupplyCommand{Program: "buf", Args: []string{"breaking", "--against", filepath.Join(dirs[0], "proto"), "--error-format=text"}, Dir: filepath.Join(dirs[1], "proto"), Env: supplyEnvironment()}, 600*time.Second)
	if f != nil {
		return f
	}
	if r.ExitCode == 0 {
		return nil
	}
	if r.ExitCode != 100 {
		return supplyUnavailable("SOURCE_UNAVAILABLE")
	}
	if semver.Major(p.c.Version) == "v0" && semver.Compare(semver.MajorMinor(p.c.Version)+".0", semver.MajorMinor(p.c.Baseline)+".0") > 0 {
		return nil
	}
	return supplyRed("COMPATIBILITY_FAILED")
}
