// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package release

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type supplyPublisherInvocation struct {
	Phase, Tree, Repository, Version, Manifest, Commit, Plan, Landed string
	Via                                                              bool
	Network, Checks                                                  int
}

type supplyPublisher struct {
	e                                                       *supplyEngine
	inv                                                     supplyPublisherInvocation
	c                                                       supplyCandidate
	raw                                                     []byte
	report                                                  *supplyReport
	root, revision, tree, plan, baselineSHA, baselineDigest string
	manifestDigest, personalIdentity                        string
	archive                                                 supplyArchive
	effects                                                 []any
	policy                                                  supplyPolicy
	pr                                                      *supplyPR
	target                                                  string
}

func supplyParsePublisher(args []string) (supplyPublisherInvocation, *supplyFailure) {
	v := supplyPublisherInvocation{Phase: "plan"}
	seen := map[string]bool{}
	bad := func() (supplyPublisherInvocation, *supplyFailure) {
		return v, &supplyFailure{"USAGE_ERROR", "INVALID_INVOCATION"}
	}
	for i := 0; i < len(args); i++ {
		key := args[i]
		if seen[key] {
			return bad()
		}
		seen[key] = true
		if key == "--via-pull-request" {
			v.Via = true
			continue
		}
		if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
			return bad()
		}
		i++
		value := args[i]
		switch key {
		case "--phase":
			if value != "plan" && value != "deliver" && value != "release" && value != "probe" {
				return bad()
			}
			v.Phase = value
		case "--tree":
			v.Tree = value
		case "--repo":
			v.Repository = value
		case "--version":
			v.Version = value
		case "--manifest":
			v.Manifest = value
		case "--commit":
			v.Commit = value
		case "--plan-sha256":
			v.Plan = value
		case "--landed-sha":
			v.Landed = value
		case "--network-budget", "--checks-budget":
			n, err := strconv.Atoi(value)
			limit := 300
			if key == "--checks-budget" {
				limit = 7200
			}
			if err != nil || n < 1 || n > limit || strconv.Itoa(n) != value {
				return bad()
			}
			if key == "--network-budget" {
				v.Network = n
			} else {
				v.Checks = n
			}
		default:
			return bad()
		}
	}
	if !supplyRepository.MatchString(v.Repository) || v.Version == "" || v.Manifest == "" {
		return bad()
	}
	if v.Phase == "probe" {
		if v.Commit != "" || v.Plan != "" || v.Tree != "" || v.Landed != "" {
			return bad()
		}
	} else if v.Tree == "" {
		return bad()
	}
	if v.Phase == "release" && !supplySHA.MatchString(v.Landed) {
		return bad()
	}
	if v.Phase != "release" && v.Landed != "" {
		return bad()
	}
	if v.Commit != "" && (!supplyRepository.MatchString(v.Commit) || !supplySHA256.MatchString(v.Plan)) {
		return v, &supplyFailure{"USAGE_ERROR", "INVALID_CONFIRMATION"}
	}
	if v.Commit == "" && v.Plan != "" {
		return v, &supplyFailure{"USAGE_ERROR", "INVALID_CONFIRMATION"}
	}
	return v, nil
}

// RunSupplyPublisher implements the ready-tree producer. The injected boundaries
// are Go API arguments only; the product invocation accepts no fixture controls.
func RunSupplyPublisher(ctx context.Context, args []string, deps SupplyDependencies, stdout, stderr io.Writer) int {
	report := supplyNewReport()
	inv, failure := supplyParsePublisher(args)
	report.document["phase"] = inv.Phase
	report.document["dry_run"] = (inv.Commit == "" || inv.Phase == "plan") && inv.Phase != "probe"
	if supplyRepository.MatchString(inv.Repository) {
		report.document["repository"] = inv.Repository
	}
	if inv.Version != "" {
		report.document["version"] = inv.Version
	}
	report.check("invocation", failure, []string{inv.Phase}, 1)
	if failure != nil {
		return report.finish(stdout)
	}
	work, err := os.MkdirTemp("", "release-publisher-")
	if err != nil {
		report.check("input", supplyUnavailable("HARNESS_UNAVAILABLE"), nil, nil)
		return report.finish(stdout)
	}
	defer supplyRemoveOwned(work)
	stage, cancel := context.WithTimeout(ctx, 3600*time.Second)
	defer cancel()
	if deps.Command == nil {
		deps.Command = supplyOSCommand
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.Sleep == nil {
		deps.Sleep = supplySleep
	}
	if deps.HTTP == nil {
		deps.HTTP = supplyRuntimeHTTP
	}
	e := &supplyEngine{ctx: stage, deps: deps, work: work, manifest: supplyManifest{NetworkSeconds: 30, ChecksSeconds: 1800}}
	if inv.Network != 0 {
		e.manifest.NetworkSeconds = inv.Network
	}
	if inv.Checks != 0 {
		e.manifest.ChecksSeconds = inv.Checks
	}
	p := &supplyPublisher{e: e, inv: inv, report: report, effects: []any{}}
	if failure = p.prepare(); failure != nil {
		p.fail("input", failure)
		return report.finish(stdout)
	}
	if inv.Commit != "" && (inv.Commit != inv.Repository || inv.Plan != p.plan) {
		p.fail("invocation", &supplyFailure{"USAGE_ERROR", "INVALID_CONFIRMATION"})
		return report.finish(stdout)
	}
	if inv.Phase == "deliver" && inv.Commit != "" && !inv.Via {
		p.fail("pr", supplyRed("PR_REQUIRED"))
		return report.finish(stdout)
	}
	// The same candidate engine owns all nine candidate predicates. Its temporary
	// root is derived here and is never another publisher input.
	candidateReport := supplyNewReport()
	candidateReport.check("invocation", nil, []string{inv.Phase}, 1)
	publishedVersion := ""
	if inv.Phase == "release" || inv.Phase == "probe" {
		publishedVersion = p.c.Version
	}
	p.archive = e.checkCandidate(p.c, p.revision, candidateReport, publishedVersion)
	p.absorb(candidateReport, nil)
	if p.failed() {
		return report.finish(stdout)
	}
	if failure = p.compatibility(p.revision); failure != nil {
		p.fail("compatibility", failure)
		return report.finish(stdout)
	}
	p.pass("compatibility", []string{p.c.Repository, p.baselineSHA, p.revision, "tracked-protobuf-domain"}, 1)
	pinRaw := map[string]any{}
	for _, k := range []string{"schema_version", "product_trees", "internal_modules", "budgets"} {
		pinRaw[k] = p.c.Raw[k]
	}
	pins, failure := supplyParsePins(supplyCanonical(pinRaw))
	if failure != nil {
		p.fail("pins-origin", failure)
		return report.finish(stdout)
	}
	pe := *e
	pe.work = filepath.Join(work, "pins")
	if os.Mkdir(pe.work, 0700) != nil {
		p.fail("pins-origin", supplyUnavailable("HARNESS_UNAVAILABLE"))
		return report.finish(stdout)
	}
	pinReport := supplyNewReport()
	pe.checkPins(pins, false, pinReport)
	p.absorb(pinReport, map[string]bool{"pins-origin": true})
	if pinReport.document["outcome"] != "GREEN" && !p.failed() {
		p.fail("pins-origin", &supplyFailure{pinReport.document["outcome"].(string), pinReport.document["reason"].(string)})
	}
	if p.failed() {
		return report.finish(stdout)
	}
	policy, failure := p.readPolicy()
	if failure != nil {
		p.fail("pr", failure)
		return report.finish(stdout)
	}
	p.policy = policy
	p.pass("pr", []string{"policy", inv.Repository, policy.Main}, len(policy.Required))
	if (inv.Commit == "" || inv.Phase == "plan") && inv.Phase != "probe" {
		return report.finish(stdout)
	}
	if inv.Phase == "deliver" {
		failure = p.deliver()
	} else {
		failure = p.release(inv.Phase == "probe")
	}
	if failure != nil && !p.failed() {
		p.fail(p.failurePredicate(failure), failure)
	}
	return report.finish(stdout)
}

func supplySleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
func (p *supplyPublisher) failed() bool { return p.report.document["outcome"] != "GREEN" }
func (p *supplyPublisher) fail(predicate string, f *supplyFailure) {
	p.report.replaceCheck(predicate, f, nil, nil)
}
func (p *supplyPublisher) pass(predicate string, subjects []string, count any) {
	p.report.replaceCheck(predicate, nil, subjects, count)
}
func (p *supplyPublisher) absorb(r *supplyReport, only map[string]bool) {
	for _, c := range r.checks {
		pred := c["predicate"].(string)
		if only != nil && !only[pred] {
			continue
		}
		var f *supplyFailure
		if c["outcome"] != "GREEN" {
			f = &supplyFailure{c["outcome"].(string), c["reason"].(string)}
		}
		p.report.replaceCheck(pred, f, c["subjects"].([]string), c["examined"])
	}
	for k, v := range r.census {
		if v != nil && (only == nil || k == "modules" || k == "internal_requirements" || k == "pseudo_versions" || k == "resolved_pins") {
			p.report.census[k] = v
		}
	}
}
func (p *supplyPublisher) failurePredicate(f *supplyFailure) string {
	switch f.Reason {
	case "INPUT_CHANGED":
		return "input"
	case "TARGET_NOT_ON_MAIN", "CONTENT_MISMATCH":
		return "main-ancestry"
	case "REQUIRED_CHECKS_FAILED":
		return "required-checks"
	case "TAG_IDENTITY_CONFLICT":
		return "tag"
	case "PROXY_UNAVAILABLE", "PUBLISHED_ARCHIVE_MISMATCH":
		return "proxy"
	default:
		if p.target != "" {
			return "tag"
		}
		return "pr"
	}
}
