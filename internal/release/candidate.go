// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package release

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
	modzip "golang.org/x/mod/zip"
)

func runSupplyCandidate(ctx context.Context, invocation supplyInvocation, deps SupplyDependencies, report *supplyReport, stdout io.Writer) int {
	raw, err := os.ReadFile(invocation.Manifest)
	if err != nil {
		report.check("input", supplyUnavailable("SOURCE_UNAVAILABLE"), nil, nil)
		return report.finish(stdout)
	}
	candidate, failure := supplyParseCandidate(raw)
	if failure != nil {
		report.check("input", failure, nil, nil)
		return report.finish(stdout)
	}
	report.document["repository"], report.document["version"] = candidate.Repository, candidate.Version
	report.document["input_sha256"] = candidate.InputDigest
	stage, cancel := context.WithTimeout(ctx, 3600*time.Second)
	defer cancel()
	work, err := os.MkdirTemp("", "release-candidate-")
	if err != nil {
		report.check("input", supplyUnavailable("HARNESS_UNAVAILABLE"), nil, nil)
		return report.finish(stdout)
	}
	defer supplyRemoveOwned(work)
	if deps.Command == nil {
		deps.Command = supplyOSCommand
	}
	engine := supplyEngine{ctx: stage, deps: deps, manifest: candidate.supplyManifest, work: work}
	evidence := supplyEvidence{engine: &engine, candidate: candidate, revision: invocation.Revision, roots: map[string]string{candidate.Repository: candidate.CandidateRoot}, trees: map[string]map[string]supplyTrackedFile{}}
	failure = engine.identify(candidate.CandidateRoot, candidate.Repository, invocation.Revision)
	if failure == nil {
		failure = evidence.producerIdentity()
	}
	// Input drift remains an input failure, while missing/stale authority is an
	// identity failure; neither can become candidate permission.
	if failure != nil && failure.Reason == "INPUT_CHANGED" {
		report.check("identity", nil, []string{candidate.Repository, invocation.Revision}, 1)
		report.check("input", failure, nil, nil)
		return report.finish(stdout)
	}
	report.check("identity", failure, []string{candidate.Repository, invocation.Revision, "PRO-Robotech/kacho@" + supplyString(candidate.Producer["commit"])}, 2)
	if failure != nil {
		return report.finish(stdout)
	}
	current, failure := engine.tracked(candidate.CandidateRoot, invocation.Revision)
	if failure == nil && supplyDigest(supplyCanonical(candidate.Raw["input_files"])) != candidate.InputDigest {
		failure = supplyRed("INPUT_CHANGED")
	}
	actual := supplyTrackedMap(current)
	if failure == nil {
		for _, file := range candidate.Input {
			if !supplyFileMatches(file, actual[file.Path]) {
				failure = supplyRed("INPUT_CHANGED")
				break
			}
		}
	}
	if failure == nil {
		report.census["input_files"] = len(candidate.Input)
		report.document["candidate_sha"] = invocation.Revision
	}
	report.check("input", failure, []string{invocation.Revision, candidate.InputDigest}, report.census["input_files"])
	if failure != nil {
		return report.finish(stdout)
	}
	evidence.trees[candidate.Repository+"@"+invocation.Revision] = actual
	base, failure := engine.tracked(candidate.CandidateRoot, candidate.Base)
	if failure == nil {
		failure = supplyOwnership(candidate, actual, supplyTrackedMap(base))
	}
	if failure == nil {
		report.census["preserved_files"] = len(candidate.Preserve)
	}
	report.check("ownership", failure, []string{candidate.Base, invocation.Revision}, report.census["preserved_files"])
	if failure != nil {
		return report.finish(stdout)
	}
	baseline, baselineSHA, failure := engine.previousArchive(candidate)
	if failure == nil {
		report.census["previous_packages"] = len(baseline.Packages)
		if len(baseline.Packages) == 0 {
			failure = supplyRed("BASELINE_EMPTY")
		}
	}
	baselineSubjects := []string{candidate.Baseline}
	if baselineSHA != "" {
		baselineSubjects = append(baselineSubjects, baselineSHA)
	}
	if baseline.Digest != "" {
		baselineSubjects = append(baselineSubjects, baseline.Digest)
	}
	report.check("baseline", failure, baselineSubjects, report.census["previous_packages"])
	if failure != nil {
		return report.finish(stdout)
	}
	archive, failure := engine.archive(invocation.Revision)
	if failure == nil {
		report.census["candidate_packages"] = len(archive.Packages)
		if len(archive.Packages) == 0 {
			failure = supplyRed("PACKAGE_FLOOR_LOSS")
		}
		for name := range baseline.Packages {
			if !archive.Packages[name] {
				failure = supplyRed("PACKAGE_FLOOR_LOSS")
			}
		}
	}
	floorSubjects := []string{baseline.Digest}
	if archive.Digest != "" {
		floorSubjects = append(floorSubjects, archive.Digest)
	}
	report.check("package-floor", failure, floorSubjects, report.census["candidate_packages"])
	if failure != nil {
		return report.finish(stdout)
	}
	engine.checkConsumers(invocation.Revision, archive, report)
	failure = evidence.payload(archive, actual, supplyTrackedMap(base))
	if failure == nil {
		report.census["payload_files"] = len(candidate.Payload)
	}
	report.check("payload", failure, []string{archive.Digest}, report.census["payload_files"])
	return report.finish(stdout)
}

func supplyOwnership(c supplyCandidate, current, base map[string]supplyTrackedFile) *supplyFailure {
	input := map[string]supplySourceFile{}
	for _, f := range c.Input {
		input[f.Path] = f
	}
	owned := map[string]bool{}
	for _, f := range c.Owned {
		if !supplyFileMatches(f, base[f.Path]) {
			return supplyRed("RECEIVING_OWNERSHIP_CONFLICT")
		}
		owned[f.Path] = true
	}
	for name := range base {
		if strings.HasPrefix(name, ".github/") {
			if !owned[name] {
				return supplyRed("RECEIVING_OWNERSHIP_CONFLICT")
			}
			owned[name] = true
		}
	}
	for name := range owned {
		old, now := base[name], current[name]
		if old.Mode != now.Mode || !bytes.Equal(old.Data, now.Data) || now.Path != name {
			return supplyRed("RECEIVING_OWNERSHIP_CONFLICT")
		}
	}
	disposition := map[string]string{}
	for _, set := range []struct {
		name  string
		paths []string
	}{{"preserve", c.Preserve}, {"replace", c.Replace}, {"remove", c.Remove}} {
		for _, name := range set.paths {
			if disposition[name] != "" {
				return supplyRed("UNDECLARED_PATH_LOSS")
			}
			disposition[name] = set.name
			old, inBase := base[name]
			now, inCandidate := current[name]
			incoming, inInput := input[name]
			switch set.name {
			case "preserve":
				if !inBase || !inCandidate || now.Mode != old.Mode || !bytes.Equal(now.Data, old.Data) {
					return supplyRed("UNDECLARED_PATH_LOSS")
				}
			case "replace":
				if !inInput || !inCandidate || !supplyFileMatches(incoming, now) {
					return supplyRed("UNDECLARED_PATH_LOSS")
				}
			case "remove":
				if !inBase || inCandidate || inInput {
					return supplyRed("UNDECLARED_PATH_LOSS")
				}
			}
		}
	}
	for name := range base {
		if disposition[name] == "" {
			return supplyRed("UNDECLARED_PATH_LOSS")
		}
	}
	for name := range input {
		if disposition[name] == "" {
			return supplyRed("UNDECLARED_PATH_LOSS")
		}
	}
	for name := range current {
		if disposition[name] == "" || disposition[name] == "remove" {
			return supplyRed("UNDECLARED_PATH_LOSS")
		}
	}
	return nil
}

func (e *supplyEngine) previousArchive(c supplyCandidate) (supplyArchive, string, *supplyFailure) {
	var archive supplyArchive
	remote, failure := e.pinRemote(c.Repository, 0)
	if failure != nil {
		return archive, "", failure
	}
	latest := ""
	for _, ref := range remote.Refs {
		fields := strings.Fields(ref)
		tag := strings.TrimPrefix(fields[1], "refs/tags/")
		if tag == fields[1] || tag == "" || semver.Canonical(tag) != tag || semver.Major(tag) != semver.Major(c.Version) || module.Check(c.ModulePath, tag) != nil {
			continue
		}
		if latest == "" || semver.Compare(tag, latest) > 0 {
			latest = tag
		}
	}
	if latest == "" {
		return archive, "", supplyRed("BASELINE_EMPTY")
	}
	if latest != c.Baseline {
		return archive, "", supplyRed("BASELINE_STALE")
	}
	target, failure := e.git(remote.Root, nil, "rev-parse", "--verify", "refs/tags/"+latest+"^{commit}")
	if failure != nil {
		return archive, "", failure
	}
	revision := strings.TrimSpace(string(target))
	if !supplySHA.MatchString(revision) {
		return archive, "", supplyUnavailable("BASELINE_UNAVAILABLE")
	}
	escaped, err := module.EscapePath(c.ModulePath)
	if err != nil {
		return archive, revision, supplyRed("INPUT_INVALID")
	}
	version, err := module.EscapeVersion(latest)
	if err != nil {
		return archive, revision, supplyRed("VERSION_INVALID")
	}
	prefix := "https://proxy.golang.org/" + escaped + "/@v/" + version
	info, failure := e.readHTTP(prefix+".info", "BASELINE_UNAVAILABLE")
	if failure != nil {
		return archive, revision, failure
	}
	var metadata struct {
		Version string
		Time    time.Time
	}
	if json.Unmarshal(info, &metadata) != nil || metadata.Version != latest || metadata.Time.IsZero() {
		return archive, revision, supplyRed("INPUT_INVALID")
	}
	mod, failure := e.readHTTP(prefix+".mod", "BASELINE_UNAVAILABLE")
	if failure != nil {
		return archive, revision, failure
	}
	raw, failure := e.readHTTP(prefix+".zip", "BASELINE_UNAVAILABLE")
	if failure != nil {
		return archive, revision, failure
	}
	filename := filepath.Join(e.work, "previous.zip")
	if os.WriteFile(filename, raw, 0600) != nil {
		return archive, revision, supplyUnavailable("HARNESS_UNAVAILABLE")
	}
	if _, err := modzip.CheckZip(module.Version{Path: c.ModulePath, Version: latest}, filename); err != nil {
		return archive, revision, supplyRed("INPUT_INVALID")
	}
	archive, failure = supplyReadModuleArchive(raw, c.ModulePath, latest)
	if failure != nil {
		return archive, revision, failure
	}
	if !bytes.Equal(mod, archive.Files["go.mod"]) {
		return archive, revision, supplyRed("INPUT_INVALID")
	}
	// The tag must name the archive's actual source, not merely an equal-looking
	// version in the proxy. Compare member bytes, ignoring ZIP writer metadata.
	proof := *e
	proof.manifest.CandidateRoot = remote.Root
	proof.work = filepath.Join(e.work, "baseline-source")
	if os.Mkdir(proof.work, 0700) != nil {
		return archive, revision, supplyUnavailable("HARNESS_UNAVAILABLE")
	}
	expected, failure := proof.archive(revision)
	if failure != nil {
		return archive, revision, failure
	}
	if len(expected.Files) != len(archive.Files) {
		return archive, revision, supplyRed("CONTENT_MISMATCH")
	}
	for name, data := range expected.Files {
		actual, exists := archive.Files[name]
		if !exists || !bytes.Equal(data, actual) {
			return archive, revision, supplyRed("CONTENT_MISMATCH")
		}
	}
	return archive, revision, nil
}

func (v *supplyEvidence) payload(archive supplyArchive, current, base map[string]supplyTrackedFile) *supplyFailure {
	c := v.candidate
	required := map[string]string{}
	for _, component := range c.Components {
		ids := []string{"DT-P01", "DT-P02", "DT-P03"}
		if component == "CI-NP-1" {
			ids = []string{"NP-P01", "NP-P02", "NP-P03", "NP-P04", "NP-P05", "NP-P06"}
		}
		for _, id := range ids {
			required[id] = component
		}
	}
	if supplyContains(c.Addenda, "2590") {
		required["DT-A2590-P01"] = "CI-DT-1"
		required["DT-A2590-P02"] = "CI-DT-1"
	}
	if len(c.Proofs) != len(required) {
		return supplyRed("PAYLOAD_MISMATCH")
	}
	// Completeness is established before reading records, so a missing proof
	// cannot be hidden by an unrelated inaccessible record.
	seen := map[string]bool{}
	for _, raw := range c.Proofs {
		p, ok := raw.(map[string]any)
		if !ok {
			return supplyRed("PAYLOAD_MISMATCH")
		}
		subject, ok := p["subject"].(map[string]any)
		if !ok {
			return supplyRed("PAYLOAD_MISMATCH")
		}
		id := supplyString(subject["predicate_id"])
		if required[id] == "" || seen[id] {
			return supplyRed("PAYLOAD_MISMATCH")
		}
		seen[id] = true
	}
	reviewed := map[string]map[string]supplySourceFile{}
	var combined *supplyFailure
	for _, raw := range c.Proofs {
		id, files, failure := v.proof(raw, "PAYLOAD_MISMATCH")
		combined = supplyPinFailure(combined, failure)
		if failure != nil {
			continue
		}
		component := required[id]
		if reviewed[component] == nil {
			reviewed[component] = map[string]supplySourceFile{}
		}
		p := raw.(map[string]any)
		source := p["subject"].(map[string]any)["source"].(map[string]any)
		if supplyString(source["repository"]) != c.Repository {
			combined = supplyRed("PAYLOAD_MISMATCH")
			continue
		}
		for _, file := range files {
			if prior, exists := reviewed[component][file.Path]; exists && prior != file {
				combined = supplyRed("PAYLOAD_MISMATCH")
			}
			reviewed[component][file.Path] = file
		}
	}
	if combined != nil {
		return combined
	}
	declared := map[string]supplyPayloadFile{}
	counts := map[string]int{}
	npPython, npJS, npLock := false, false, false
	for _, file := range c.Payload {
		declared[file.Path] = file
		counts[file.Component]++
		data, exists := archive.Files[file.Path]
		if !exists || !supplyFileMatches(file.supplySourceFile, current[file.Path]) || supplyDigest(data) != file.SHA256 || reviewed[file.Component][file.Path] != file.supplySourceFile {
			return supplyRed("PAYLOAD_MISMATCH")
		}
		if file.Component == "CI-NP-1" {
			npPython = npPython || strings.HasSuffix(file.Path, ".py")
			npJS = npJS || strings.HasSuffix(file.Path, ".js") || strings.HasSuffix(file.Path, ".mjs")
			npLock = npLock || filepath.Base(file.Path) == "package-lock.json"
		}
		if file.Path == "migratorcli/notice_test.go" {
			if file.Component != "CI-DT-1" || !supplyContains(c.Addenda, "2590") || file.SHA256 != "c85a8ed9b06a0720fa4050e3537ee90892cb1aa81e84103aba63da7fe6125f96" {
				return supplyRed("PAYLOAD_MISMATCH")
			}
		}
	}
	for _, component := range c.Components {
		if counts[component] == 0 {
			return supplyRed("PAYLOAD_MISMATCH")
		}
	}
	if supplyContains(c.Components, "CI-NP-1") && (!npPython || !npJS || !npLock) {
		return supplyRed("PAYLOAD_MISMATCH")
	}
	if supplyContains(c.Addenda, "2590") && declared["migratorcli/notice_test.go"].Path == "" {
		return supplyRed("PAYLOAD_MISMATCH")
	}
	// Known canonical component homes cannot disappear from the manifest when
	// their bytes change. Additional approved payload paths are checked above.
	paths := map[string]bool{}
	for name := range base {
		paths[name] = true
	}
	for name := range current {
		paths[name] = true
	}
	for name := range paths {
		old, now := base[name], current[name]
		if old.Path == now.Path && old.Mode == now.Mode && bytes.Equal(old.Data, now.Data) {
			continue
		}
		component := ""
		if strings.HasPrefix(name, "ci/newman_publication/") {
			component = "CI-NP-1"
		}
		if name == "api/corelib/subscription/subscription.pb.go" || name == "migratorcli/notice_test.go" {
			component = "CI-DT-1"
		}
		if component != "" && declared[name].Component != component {
			return supplyRed("PAYLOAD_MISMATCH")
		}
	}
	return nil
}
