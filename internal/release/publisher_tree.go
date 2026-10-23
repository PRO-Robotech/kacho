// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package release

import (
	"bytes"
	"crypto/sha1"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

func (p *supplyPublisher) prepare() *supplyFailure {
	raw, err := os.ReadFile(p.inv.Manifest)
	if err != nil {
		return supplyUnavailable("SOURCE_UNAVAILABLE")
	}
	p.raw = raw
	p.manifestDigest = supplyDigest(raw)
	doc, ok := supplyJSON(raw)
	if !ok {
		return supplyRed("INPUT_INVALID")
	}
	if supplyString(doc["repository"]) != p.inv.Repository {
		p.fail("identity", supplyRed("REPOSITORY_MISMATCH"))
		return supplyRed("REPOSITORY_MISMATCH")
	}
	if supplyString(doc["version"]) != p.inv.Version {
		return supplyRed("VERSION_INVALID")
	}
	if _, exists := doc["candidate_root"]; exists {
		return supplyRed("INPUT_INVALID")
	}
	p.root = filepath.Join(p.e.work, "receiving")
	doc["candidate_root"] = p.root
	c, f := supplyParseCandidate(supplyCanonical(doc))
	if f != nil {
		return f
	}
	p.c = c
	if p.inv.Network != 0 {
		p.c.NetworkSeconds = p.inv.Network
	}
	if p.inv.Checks != 0 {
		p.c.ChecksSeconds = p.inv.Checks
	}
	p.c.Raw["budgets"] = map[string]any{"network_seconds": p.c.NetworkSeconds, "checks_seconds": p.c.ChecksSeconds}
	p.e.manifest = p.c.supplyManifest
	// This owned empty receiving repository has exactly the origin a clone
	// would create. URL readback happens before any default-config transport.
	if _, f = p.e.git(p.e.work, nil, "init", "--quiet", p.root); f != nil {
		return f
	}
	if _, f = p.e.git(p.root, nil, "remote", "add", "origin", "https://github.com/"+c.Repository+".git"); f != nil {
		return f
	}
	if _, f = p.transport("fetch", "--quiet", "--no-recurse-submodules", "origin", "+refs/heads/*:refs/remotes/origin/*", "+refs/tags/*:refs/tags/*"); f != nil {
		return f
	}
	// Git's checkout must not infer an input tree from the current remote branch.
	base, f := p.e.tracked(p.root, c.Base)
	if f != nil {
		return f
	}
	baseMap := supplyTrackedMap(base)
	input := map[string]supplyTrackedFile{}
	if p.inv.Phase == "probe" {
		target, f := p.resolveLocal("refs/tags/" + c.Version + "^{commit}")
		if f != nil {
			return f
		}
		files, f := p.e.tracked(p.root, target)
		if f != nil {
			return f
		}
		all := supplyTrackedMap(files)
		for _, decl := range c.Input {
			v, exists := all[decl.Path]
			if !exists || !supplyFileMatches(decl, v) {
				return supplyRed("PUBLISHED_ARCHIVE_MISMATCH")
			}
			input[decl.Path] = v
		}
	} else {
		input, f = supplyReadReady(p.inv.Tree, c.Input)
		if f != nil {
			return f
		}
	}
	if supplyDigest(supplyCanonical(c.Raw["input_files"])) != c.InputDigest {
		return supplyRed("INPUT_CHANGED")
	}
	combined := map[string]supplyTrackedFile{}
	for name, v := range input {
		combined[name] = v
	}
	for _, name := range c.Preserve {
		v, ok := baseMap[name]
		if !ok {
			return supplyRed("UNDECLARED_PATH_LOSS")
		}
		if own, exists := combined[name]; exists && (own.Mode != v.Mode || !bytes.Equal(own.Data, v.Data)) {
			return supplyRed("RECEIVING_OWNERSHIP_CONFLICT")
		}
		combined[name] = v
	}
	if f = supplyOwnership(c, combined, baseMap); f != nil {
		return f
	}
	if _, f = p.e.git(p.root, nil, "read-tree", "--empty"); f != nil {
		return f
	}
	names := []string{}
	for name := range combined {
		names = append(names, name)
	}
	sort.Strings(names)
	var index strings.Builder
	for _, name := range names {
		v := combined[name]
		hash, f := p.e.git(p.root, v.Data, "hash-object", "-w", "--stdin")
		if f != nil {
			return f
		}
		index.WriteString(v.Mode + " " + strings.TrimSpace(string(hash)) + "\t" + name + "\x00")
	}
	if _, f = p.e.git(p.root, []byte(index.String()), "update-index", "-z", "--index-info"); f != nil {
		return f
	}
	tree, f := p.e.git(p.root, nil, "write-tree")
	if f != nil {
		return f
	}
	p.tree = strings.TrimSpace(string(tree))
	if f = p.makeCommit(); f != nil {
		return f
	}
	if _, f = p.e.git(p.root, nil, "checkout", "--quiet", "--detach", p.revision); f != nil {
		return f
	}
	// Resolve baseline content before sealing the plan; this read-only copy has
	// its own scratch space, separate from the later candidate evaluator.
	baselineEngine := *p.e
	baselineEngine.work = filepath.Join(p.e.work, "plan-baseline")
	if os.Mkdir(baselineEngine.work, 0700) != nil {
		return supplyUnavailable("HARNESS_UNAVAILABLE")
	}
	excluded := ""
	if p.inv.Phase == "release" || p.inv.Phase == "probe" {
		excluded = c.Version
	}
	previous, sha, f := baselineEngine.previousArchiveFor(c, excluded)
	if f != nil {
		return f
	}
	p.baselineSHA, p.baselineDigest = sha, previous.Digest
	identity := map[string]any{"manifest": supplyPlanProjection(p.c.Raw), "candidate_tree": p.tree, "candidate_sha": p.revision, "baseline_sha": sha, "baseline_archive_sha256": previous.Digest}
	p.plan = supplyDigest(supplyCanonical(identity))
	p.report.document["plan_sha256"], p.report.document["candidate_sha"], p.report.document["input_sha256"] = p.plan, p.revision, c.InputDigest
	evaluation := filepath.Join(p.e.work, "candidate")
	if os.Mkdir(evaluation, 0700) != nil {
		return supplyUnavailable("HARNESS_UNAVAILABLE")
	}
	p.e.work = evaluation
	return nil
}

func supplyReadReady(root string, declared []supplySourceFile) (map[string]supplyTrackedFile, *supplyFailure) {
	result := map[string]supplyTrackedFile{}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, supplyRed("INPUT_INVALID")
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return nil, supplyUnavailable("SOURCE_UNAVAILABLE")
	}
	if !info.IsDir() {
		return nil, supplyRed("INPUT_INVALID")
	}
	err = filepath.WalkDir(absolute, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(absolute, name)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if !supplyRelative(rel, false) {
			return fs.ErrInvalid
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fs.ErrInvalid
		}
		b, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		mode := "100644"
		if info.Mode().Perm()&0111 != 0 {
			mode = "100755"
		}
		result[rel] = supplyTrackedFile{Path: rel, Mode: mode, Data: b}
		return nil
	})
	if err != nil {
		if err == fs.ErrInvalid {
			return nil, supplyRed("INPUT_INVALID")
		}
		return nil, supplyUnavailable("SOURCE_UNAVAILABLE")
	}
	if len(result) != len(declared) {
		return nil, supplyRed("INPUT_CHANGED")
	}
	for _, v := range declared {
		if !supplyFileMatches(v, result[v.Path]) {
			return nil, supplyRed("INPUT_CHANGED")
		}
	}
	return result, nil
}

// Only local execution locators are projected out. Typed digest/source roles
// and relative tracked paths remain. This also avoids circular temporary roots.
func supplyPlanProjection(v any) any {
	switch x := v.(type) {
	case map[string]any:
		r := map[string]any{}
		for k, v := range x {
			if k == "root" || k == "candidate_root" || k == "cwd" {
				continue
			}
			if k == "path" {
				if s, ok := v.(string); ok && filepath.IsAbs(s) {
					continue
				}
			}
			r[k] = supplyPlanProjection(v)
		}
		return r
	case []any:
		r := make([]any, len(x))
		for i, v := range x {
			r[i] = supplyPlanProjection(v)
		}
		return r
	default:
		return v
	}
}
func (p *supplyPublisher) resolveLocal(ref string) (string, *supplyFailure) {
	b, f := p.e.git(p.root, nil, "rev-parse", "--verify", ref)
	if f != nil {
		return "", f
	}
	sha := strings.TrimSpace(string(b))
	if !supplySHA.MatchString(sha) {
		return "", supplyUnavailable("SOURCE_UNAVAILABLE")
	}
	return sha, nil
}
func (p *supplyPublisher) readRef(ref string) (string, *supplyFailure) {
	b, f := p.transport("ls-remote", "https://github.com/"+p.c.Repository+".git", ref, ref+"^{}")
	if f != nil {
		return "", f
	}
	value := ""
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 || !supplySHA.MatchString(fields[0]) || (fields[1] != ref && fields[1] != ref+"^{}") {
			return "", supplyUnavailable("SOURCE_UNAVAILABLE")
		}
		if fields[1] == ref+"^{}" || value == "" {
			value = fields[0]
		}
	}
	return value, nil
}
func (p *supplyPublisher) fresh(requireBase bool) *supplyFailure {
	raw, err := os.ReadFile(p.inv.Manifest)
	if err != nil {
		return supplyUnavailable("SOURCE_UNAVAILABLE")
	}
	if supplyDigest(raw) != p.manifestDigest {
		return supplyRed("INPUT_CHANGED")
	}
	if p.inv.Phase != "probe" {
		if _, f := supplyReadReady(p.inv.Tree, p.c.Input); f != nil {
			if f.Outcome == "RED" {
				return supplyRed("INPUT_CHANGED")
			}
			return f
		}
	}
	// Re-read the full compatible tag census. An unchanged selected ref does
	// not make a plan fresh after an unrelated newer release appeared.
	tags, f := p.transport("ls-remote", "--tags", "https://github.com/"+p.c.Repository+".git")
	if f != nil {
		return f
	}
	excluded := ""
	if p.inv.Phase == "release" || p.inv.Phase == "probe" {
		excluded = p.c.Version
	}
	refs := []string{}
	if text := strings.TrimSpace(string(tags)); text != "" {
		refs = strings.Split(text, "\n")
	}
	if _, f := supplyLatestBaseline(p.c, refs, excluded); f != nil {
		if f.Outcome == "RED" {
			return supplyRed("INPUT_CHANGED")
		}
		return f
	}
	baseline, f := p.readRef("refs/tags/" + p.c.Baseline)
	if f != nil {
		return f
	}
	if baseline != p.baselineSHA {
		return supplyRed("INPUT_CHANGED")
	}
	if requireBase {
		main, f := p.readRef("refs/heads/main")
		if f != nil {
			return f
		}
		if main != p.c.Base {
			return supplyRed("INPUT_CHANGED")
		}
	}
	if p.pr != nil {
		pr, f := p.readPR(p.pr.Number)
		if f != nil {
			if f.Outcome == "RED" {
				p.prEffect("CONFLICT", pr)
			}
			return f
		}
		if p.pr.Merged && (!pr.Merged || pr.MergeSHA != p.pr.MergeSHA) {
			return supplyRed("INPUT_CHANGED")
		}
	}
	identity, f := p.configuredIdentity()
	if f != nil {
		return f
	}
	if identity != p.personalIdentity {
		return supplyRed("INPUT_CHANGED")
	}
	// Bound records and the exact executed producer remain immutable too.
	ev := supplyEvidence{engine: p.e, candidate: p.c, revision: p.revision, roots: map[string]string{p.c.Repository: p.root}, trees: map[string]map[string]supplyTrackedFile{}}
	if f := ev.producerIdentity(); f != nil {
		return f
	}
	for _, proof := range p.c.Proofs {
		if _, _, f := ev.proof(proof, "PAYLOAD_MISMATCH"); f != nil {
			return f
		}
	}
	return nil
}

// defaultGit never changes configuration. Callers are the finite native
// identity reads and exact publisher transport, guarded by transportURL.
func (p *supplyPublisher) defaultGit(root string, args ...string) ([]byte, *supplyFailure) {
	r, f := p.e.command(SupplyCommand{Program: "git", Args: append([]string{"-c", "core.hooksPath=/dev/null"}, args...), Dir: root, Env: supplyPublisherEnvironment()}, time.Duration(p.c.NetworkSeconds)*time.Second)
	if f != nil {
		return nil, f
	}
	if r.ExitCode != 0 {
		return nil, supplyUnavailable("SOURCE_UNAVAILABLE")
	}
	return r.Stdout, nil
}

func (p *supplyPublisher) transportURL() *supplyFailure {
	for _, args := range [][]string{{"remote", "get-url", "origin"}, {"remote", "get-url", "--push", "origin"}} {
		b, f := p.defaultGit(p.root, args...)
		if f != nil {
			return f
		}
		if strings.TrimSpace(string(b)) != "https://github.com/"+p.c.Repository+".git" {
			return supplyRed("REPOSITORY_MISMATCH")
		}
	}
	return nil
}

func (p *supplyPublisher) transport(args ...string) ([]byte, *supplyFailure) {
	if f := p.transportURL(); f != nil {
		return nil, f
	}
	return p.defaultGit(p.root, args...)
}

func (p *supplyPublisher) configuredIdentity() (string, *supplyFailure) {
	root := supplyString(p.c.Producer["root"])
	if !supplyAbsolute(root) {
		return "", supplyRed("INVALID_CONFIRMATION")
	}
	nameBytes, f := p.defaultGit(root, "config", "--get", "user.name")
	if f != nil {
		return "", f
	}
	emailBytes, f := p.defaultGit(root, "config", "--get", "user.email")
	if f != nil {
		return "", f
	}
	name, email := strings.TrimSpace(string(nameBytes)), strings.TrimSpace(string(emailBytes))
	if name == "" || strings.ContainsAny(name, "<>\r\n\x00") || email != "pointpu@prorobotech.ru" {
		return "", supplyUnavailable("SOURCE_UNAVAILABLE")
	}
	identity := name + " <" + email + ">"
	for _, field := range []string{"GIT_AUTHOR_IDENT", "GIT_COMMITTER_IDENT"} {
		b, f := p.defaultGit(root, "var", field)
		if f != nil {
			return "", f
		}
		native := strings.TrimSpace(string(b))
		if !strings.HasPrefix(native, identity+" ") {
			return "", supplyUnavailable("SOURCE_UNAVAILABLE")
		}
		tail := strings.Fields(strings.TrimPrefix(native, identity+" "))
		if len(tail) != 2 {
			return "", supplyUnavailable("SOURCE_UNAVAILABLE")
		}
		if _, err := strconv.ParseInt(tail[0], 10, 64); err != nil {
			return "", supplyUnavailable("SOURCE_UNAVAILABLE")
		}
		if len(tail[1]) != 5 || (tail[1][0] != '+' && tail[1][0] != '-') {
			return "", supplyUnavailable("SOURCE_UNAVAILABLE")
		}
	}
	return identity, nil
}

func (p *supplyPublisher) makeCommit() *supplyFailure {
	identity, f := p.configuredIdentity()
	if f != nil {
		return f
	}
	p.personalIdentity = identity
	stamp := int64(0)
	for _, source := range [][2]string{{supplyString(p.c.Producer["root"]), supplyString(p.c.Producer["commit"])}, {p.root, p.c.Base}} {
		if !supplyAbsolute(source[0]) || !supplySHA.MatchString(source[1]) {
			return supplyRed("INVALID_CONFIRMATION")
		}
		b, f := p.e.git(source[0], nil, "show", "-s", "--format=%ct", source[1])
		if f != nil {
			return f
		}
		n, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
		if err != nil || n < 0 || n == math.MaxInt64 {
			return supplyUnavailable("SOURCE_UNAVAILABLE")
		}
		if n > stamp {
			stamp = n
		}
	}
	stamp++
	if !supplySHA.MatchString(p.tree) {
		return supplyUnavailable("SOURCE_UNAVAILABLE")
	}
	metadata := identity + " " + strconv.FormatInt(stamp, 10) + " +0000"
	object := []byte("tree " + p.tree + "\nparent " + p.c.Base + "\nauthor " + metadata + "\ncommitter " + metadata + "\n\nchore(release): assemble module " + p.c.Version + "\n\nMetadata time derives from exact producer/base snapshots.\n")
	b, f := p.e.git(p.root, object, "hash-object", "-t", "commit", "-w", "--stdin")
	if f != nil {
		return f
	}
	p.revision = strings.TrimSpace(string(b))
	hash := sha1.New()
	fmt.Fprintf(hash, "commit %d%c", len(object), 0)
	hash.Write(object)
	if !supplySHA.MatchString(p.revision) || p.revision != fmt.Sprintf("%x", hash.Sum(nil)) {
		return supplyUnavailable("SOURCE_UNAVAILABLE")
	}
	readback, f := p.e.git(p.root, nil, "cat-file", "commit", p.revision)
	if f != nil {
		return f
	}
	if !bytes.Equal(object, readback) {
		return supplyUnavailable("SOURCE_UNAVAILABLE")
	}
	fields, f := p.e.git(p.root, nil, "show", "-s", "--format=%T%n%P%n%an <%ae>%n%cn <%ce>%n%at%n%ct", p.revision)
	if f != nil {
		return f
	}
	expected := p.tree + "\n" + p.c.Base + "\n" + identity + "\n" + identity + "\n" + strconv.FormatInt(stamp, 10) + "\n" + strconv.FormatInt(stamp, 10) + "\n"
	if string(fields) != expected {
		return supplyUnavailable("SOURCE_UNAVAILABLE")
	}
	return nil
}
