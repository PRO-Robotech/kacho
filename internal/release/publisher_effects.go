// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package release

import (
	"fmt"
	"strings"
	"time"
)

func (p *supplyPublisher) effect(kind, state string, fields map[string]any) {
	if p.inv.Commit == "" {
		return
	}
	effect := map[string]any{"kind": kind, "state": state, "repository": p.c.Repository}
	for k, v := range fields {
		effect[k] = v
	}
	for i, raw := range p.effects {
		if raw.(map[string]any)["kind"] == kind {
			p.effects[i] = effect
			p.report.document["effects"] = p.effects
			return
		}
	}
	p.effects = append(p.effects, effect)
	p.report.document["effects"] = p.effects
}
func (p *supplyPublisher) stage(value string) {
	order := map[string]int{"NONE": 0, "BRANCH_PRESENT": 1, "PR_OPEN": 2, "MERGE_PRESENT": 3, "MERGED_VERIFIED": 4, "TAG_PRESENT": 5, "ARCHIVE_VERIFIED": 6}
	if order[value] > order[p.report.document["stage"].(string)] {
		p.report.document["stage"] = value
	}
}
func (p *supplyPublisher) refEffect(kind, state, ref, expected, sha string) {
	var observed any
	if sha != "" {
		observed = sha
	}
	p.effect(kind, state, map[string]any{"ref": ref, "expected_sha": expected, "sha": observed})
}
func (p *supplyPublisher) prEffect(state string, pr *supplyPR) {
	var number, sha any
	if pr != nil {
		number = pr.Number
		if pr.Head.SHA != "" {
			sha = pr.Head.SHA
		}
	}
	p.effect("pr", state, map[string]any{"head_ref": "refs/heads/" + p.branch(), "base_ref": "refs/heads/main", "expected_head_sha": p.revision, "pr_number": number, "sha": sha, "plan_marker": p.marker()})
}
func (p *supplyPublisher) mergeEffect(state string, pr *supplyPR) {
	var sha, merge, main any
	if pr.Head.SHA != "" {
		sha = pr.Head.SHA
	}
	if state == "PRESENT" && pr.MergeSHA != "" {
		merge = pr.MergeSHA
	}
	if supplySHA.MatchString(pr.Base.SHA) {
		main = pr.Base.SHA
	}
	p.effect("merge", state, map[string]any{"head_ref": "refs/heads/" + p.branch(), "base_ref": "refs/heads/main", "expected_head_sha": p.revision, "pr_number": pr.Number, "sha": sha, "merge_sha": merge, "main_sha": main})
}
func supplyTerminalWrite(status int, f *supplyFailure) bool {
	return f == nil && status >= 400 && status < 500 && status != 408 && status != 429
}

func (p *supplyPublisher) deliver() *supplyFailure {
	if f := p.fresh(false); f != nil {
		return f
	}
	ref := "refs/heads/" + p.branch()
	sha, f := p.readRef(ref)
	if f != nil {
		return f
	}
	if sha != "" && sha != p.revision {
		p.refEffect("branch", "CONFLICT", ref, p.revision, sha)
		return supplyRed("EFFECT_IDENTITY_CONFLICT")
	}
	if sha == "" {
		if f := p.fresh(true); f != nil {
			return f
		}
		if f = p.createBranch(ref); f != nil {
			return f
		}
	} else {
		p.refEffect("branch", "PRESENT", ref, p.revision, sha)
		p.stage("BRANCH_PRESENT")
	}
	pr, f := p.findPR()
	if f != nil {
		if f.Outcome == "RED" {
			p.prEffect("CONFLICT", pr)
		}
		return f
	}
	if pr == nil {
		if f := p.fresh(true); f != nil {
			return f
		}
		p.prEffect("UNKNOWN", nil)
		status, _, writeFailure := p.http("POST", p.api("/pulls"), map[string]any{"head": p.branch(), "base": "main", "title": "Release " + p.c.ModulePath + " " + p.c.Version, "body": p.marker()})
		pr, f = p.findPR()
		if f != nil {
			if f.Outcome == "RED" {
				p.prEffect("CONFLICT", pr)
				return f
			}
			return supplyUnavailable("WRITE_OUTCOME_UNKNOWN")
		}
		if pr == nil {
			if supplyTerminalWrite(status, writeFailure) {
				p.prEffect("ABSENT", nil)
				return supplyRed("REMOTE_WRITE_REJECTED")
			}
			return supplyUnavailable("WRITE_OUTCOME_UNKNOWN")
		}
	}
	p.pr = pr
	p.prEffect("PRESENT", pr)
	p.stage("PR_OPEN")
	if pr.Merged {
		p.mergeEffect("PRESENT", pr)
		p.stage("MERGE_PRESENT")
		return p.acceptMerge(pr, false)
	}
	if f = p.fresh(true); f != nil {
		return f
	}
	if f = p.checks(p.revision); f != nil {
		p.fail("required-checks", f)
		return f
	}
	// Checks can race a changed head; reread it before permitting a merge call.
	reread, f := p.readPR(pr.Number)
	if f != nil {
		if f.Outcome == "RED" {
			p.prEffect("CONFLICT", reread)
		}
		return f
	}
	pr = reread
	p.pr = pr
	if pr.Mergeable == nil {
		return supplyUnavailable("SOURCE_UNAVAILABLE")
	}
	if !*pr.Mergeable {
		return supplyRed("REQUIRED_CHECKS_FAILED")
	}
	if f = p.fresh(true); f != nil {
		return f
	}
	p.mergeEffect("UNKNOWN", pr)
	status, _, writeFailure := p.http("PUT", p.api(fmt.Sprintf("/pulls/%d/merge", pr.Number)), map[string]any{"sha": p.revision, "merge_method": "squash"})
	observed, f := p.readPR(pr.Number)
	if f != nil {
		if f.Outcome == "RED" {
			if observed != nil {
				p.prEffect("CONFLICT", observed)
				p.mergeEffect("CONFLICT", observed)
			}
			return f
		}
		return supplyUnavailable("WRITE_OUTCOME_UNKNOWN")
	}
	if !observed.Merged {
		if supplyTerminalWrite(status, writeFailure) {
			p.mergeEffect("ABSENT", observed)
			return supplyRed("REMOTE_WRITE_REJECTED")
		}
		return supplyUnavailable("WRITE_OUTCOME_UNKNOWN")
	}
	p.pr = observed
	p.prEffect("PRESENT", observed)
	p.mergeEffect("PRESENT", observed)
	p.stage("MERGE_PRESENT")
	return p.acceptMerge(observed, false)
}

// The sole branch write is an explicit expected-empty compare-and-swap.
// A vacancy read alone cannot prevent a concurrent ancestor fast-forward.
func (p *supplyPublisher) createBranch(ref string) *supplyFailure {
	if ref != "refs/heads/"+p.branch() || !supplySHA256.MatchString(p.plan) || !supplySHA.MatchString(p.revision) {
		return supplyRed("INPUT_INVALID")
	}
	if f := p.transportURL(); f != nil {
		return f
	}
	p.refEffect("branch", "UNKNOWN", ref, p.revision, "")
	r, _ := p.e.command(SupplyCommand{Program: "git", Args: []string{"-c", "core.hooksPath=/dev/null", "push", "--porcelain", "--no-follow-tags", "--force-with-lease=" + ref + ":", "https://github.com/" + p.c.Repository + ".git", p.revision + ":" + ref}, Dir: p.root, Env: supplyPublisherEnvironment()}, time.Duration(p.c.NetworkSeconds)*time.Second)
	terminal := r.ExitCode != 0 && (strings.Contains(string(r.Stdout), "[remote rejected]") || strings.Contains(string(r.Stdout), "[rejected]"))
	sha, f := p.readRef(ref)
	if f != nil {
		return supplyUnavailable("WRITE_OUTCOME_UNKNOWN")
	}
	if sha == "" {
		if terminal {
			p.refEffect("branch", "ABSENT", ref, p.revision, "")
			return supplyRed("REMOTE_WRITE_REJECTED")
		}
		return supplyUnavailable("WRITE_OUTCOME_UNKNOWN")
	}
	if sha != p.revision {
		p.refEffect("branch", "CONFLICT", ref, p.revision, sha)
		return supplyRed("EFFECT_IDENTITY_CONFLICT")
	}
	p.refEffect("branch", "PRESENT", ref, p.revision, sha)
	p.stage("BRANCH_PRESENT")
	return nil
}

// pushRef never retries a write. The ref's creation guard is chosen by the
// kind-specific caller; the returned failure is classified only after readback.
func (p *supplyPublisher) pushRef(ref, sha string) (bool, *supplyFailure) {
	if f := p.transportURL(); f != nil {
		return false, f
	}
	r, f := p.e.command(SupplyCommand{Program: "git", Args: []string{"-c", "core.hooksPath=/dev/null", "push", "--porcelain", "--no-follow-tags", "https://github.com/" + p.c.Repository + ".git", sha + ":" + ref}, Dir: p.root, Env: supplyPublisherEnvironment()}, time.Duration(p.c.NetworkSeconds)*time.Second)
	terminal := r.ExitCode != 0 && (strings.Contains(string(r.Stdout), "[remote rejected]") || strings.Contains(string(r.Stdout), "[rejected]"))
	return terminal, f
}
func (p *supplyPublisher) createTag() *supplyFailure {
	ref := "refs/tags/" + p.c.Version
	sha, f := p.readRef(ref)
	if f != nil {
		return f
	}
	if sha != "" {
		if sha != p.target {
			p.refEffect("tag", "CONFLICT", ref, p.target, sha)
			return supplyRed("TAG_IDENTITY_CONFLICT")
		}
		p.refEffect("tag", "PRESENT", ref, p.target, sha)
		p.stage("TAG_PRESENT")
		return nil
	}
	if f = p.fresh(false); f != nil {
		return f
	}
	if f = p.ancestry(p.target, "pre-write"); f != nil {
		return f
	}
	p.refEffect("tag", "UNKNOWN", ref, p.target, "")
	terminal, _ := p.pushRef(ref, p.target)
	sha, f = p.readRef(ref)
	if f != nil {
		return supplyUnavailable("WRITE_OUTCOME_UNKNOWN")
	}
	if sha == "" {
		if terminal {
			p.refEffect("tag", "ABSENT", ref, p.target, "")
			return supplyRed("REMOTE_WRITE_REJECTED")
		}
		return supplyUnavailable("WRITE_OUTCOME_UNKNOWN")
	}
	if sha != p.target {
		p.refEffect("tag", "CONFLICT", ref, p.target, sha)
		return supplyRed("TAG_IDENTITY_CONFLICT")
	}
	p.refEffect("tag", "PRESENT", ref, p.target, sha)
	p.stage("TAG_PRESENT")
	return nil
}
