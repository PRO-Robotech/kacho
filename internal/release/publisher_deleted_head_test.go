// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package release

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"golang.org/x/mod/module"
)

// Новый holder использует прежние настоящие Git/Go/HTTP границы. Его временный
// forge отличается только именованными преобразованиями ниже; старые52 и script
// не редактируются. Изменённые API поля — отрицательные fixture inputs, не
// утверждение о получении такого ответа от GitHub.
func rsDeletedHeadScript(h *rsHarness) string {
	original := mustRSRead(h.t, filepath.Join(moduleRoot(h.t), "scripts/release/publish-module-tree-inject.sh"))
	s := string(original)
	changes := []any{}
	replace := func(label, old, next string) {
		if strings.Count(s, old) != 1 {
			h.t.Fatalf("HARNESS_NOT_EXECUTED: deleted-head fixture anchor %s", label)
		}
		s = strings.Replace(s, old, next, 1)
		changes = append(changes, map[string]any{"label": label, "old": old, "new": next})
	}
	replace("native-single-parent-squash", "['commit-tree',tree,'-p',old,'-p',head]", "['commit-tree',tree,'-p',old]")
	replace("retained-actual-pr-head", "'sha':ref(REPO,'refs/heads/'+pr['head_ref'])", "'sha':pr['created_head_sha'] if pr['merged'] or ref(REPO,'refs/heads/'+pr['head_ref']) is None else ref(REPO,'refs/heads/'+pr['head_ref'])")
	replace("merged-detail-null-mergeable", "p['mergeable']=True;p['mergeable_state']='clean';return p", "p['mergeable']=None if pr['merged'] else True;p['mergeable_state']='clean';return p")
	replace("append-only-owned-fixture-restore", "ctrl=controls();fault=ctrl.get('fault','lawful');method=self.command", `ctrl=controls();fault=ctrl.get('fault','lawful');method=self.command
        restore=ctrl.get('deleted_head_restore')
        if restore and state.get('deleted_head_restore_token')!=restore['token']:
            state.setdefault('deleted_head_restores',[]).append({'token':restore['token'],'before_prs':state['prs'],'before_notes':state['notes'],'after_prs':restore['prs'],'after_notes':restore['notes']})
            state['prs']=restore['prs'];state['notes']=restore['notes'];state['deleted_head_restore_token']=restore['token'];persist()`)
	replace("real-list-shape", "prs=[pr_view(p) for p in state['prs']]", `prs=[pr_view(p) for p in state['prs']]
            axis=ctrl.get('deleted_head_fault','')
            if axis=='discovery-empty-after-merge':prs=[]
            if axis=='list-detail-head-disagreement' and prs:prs[0]['head']['sha']=C['base']
            if ctrl.get('deleted_head_shape')!='historical-enriched':
                for view in prs:view.pop('merged',None);view.pop('mergeable',None)`)
	replace("bounded-numbered-detail-inputs", "return self.answer(200,pr_view(pr),record)", `view=pr_view(pr);axis=ctrl.get('deleted_head_fault','')
                if axis=='detail-unavailable':return self.answer(503,{'message':'owned detail unavailable'},record)
                if axis=='detail-missing':return self.answer(404,{'message':'owned detail absent'},record)
                if axis=='wrong-pr-head':view['head']['sha']=C['base']
                if axis=='detail-number-mismatch':view['number']=102
                if axis=='detail-merged-absent':view.pop('merged',None)
                if axis=='detail-merged-wrong-type':view['merged']='true'
                if axis=='detail-head-repo-mismatch':view['head']['repo']['full_name']='PRO-Robotech/foreign'
                if axis=='detail-head-ref-mismatch':view['head']['ref']='release/foreign'
                if axis=='detail-base-ref-mismatch':view['base']['ref']='other-main'
                if axis=='detail-plan-marker-mismatch':view['body']='CI-RS-1 plan-sha256:'+'0'*64
                if axis=='detail-invalid-merge-sha':view['merge_commit_sha']='invalid'
                return self.answer(200,view,record)`)
	path := filepath.Join(h.root, "deleted-head-forge.sh")
	h.put(h.root, filepath.Base(path), s)
	h.save("deleted-head-forge-original.sh", original)
	h.save("deleted-head-forge-derived.sh", []byte(s))
	h.save("deleted-head-forge-transform.json", rsCanonicalJSON(h.t, map[string]any{"original_sha256": rsSHA(original), "derived_sha256": rsSHA([]byte(s)), "changes": changes}))
	h.must(h.root, "bash", "-n", path)
	return path
}

type rsDeletedSnapshot struct {
	refs       map[string]string
	prs, notes []any
	writeCount int
}

func rsDeletedState(h *rsHarness, f *rsForgeProcess) map[string]any {
	var state map[string]any
	if err := json.Unmarshal(mustRSRead(h.t, f.state), &state); err != nil {
		h.t.Fatal(err)
	}
	return state
}

func rsDeletedRefs(h *rsHarness, f *rsForgeProcess) map[string]string {
	text := h.must(h.root, "git", "--git-dir", f.repository, "for-each-ref", "--format=%(refname) %(objectname)")
	refs := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) != 2 {
			h.t.Fatal("HARNESS_NOT_EXECUTED: native ref snapshot")
		}
		refs[parts[0]] = parts[1]
	}
	return refs
}

func rsDeletedSnapshotOf(h *rsHarness, f *rsForgeProcess, label string) rsDeletedSnapshot {
	state := rsDeletedState(h, f)
	snap := rsDeletedSnapshot{rsDeletedRefs(h, f), state["prs"].([]any), state["notes"].([]any), len(state["writes"].([]any))}
	h.save(label+"-snapshot.json", rsCanonicalJSON(h.t, map[string]any{"refs": snap.refs, "prs": snap.prs, "notes": snap.notes, "writes": snap.writeCount}))
	return snap
}

func rsDeletedControl(h *rsHarness, f *rsForgeProcess, control map[string]any) {
	if control == nil {
		control = map[string]any{}
	}
	if _, ok := control["fault"]; !ok {
		control["fault"] = "lawful"
	}
	if err := os.WriteFile(f.control, rsCanonicalJSON(h.t, control), 0600); err != nil {
		h.t.Fatal(err)
	}
}

// Откат собственной fixture использует только ранее захваченные реальные refs
// и metadata. Журнал writes и snapshots не откатывается: запрещённый SUT write
// остаётся отдельным FAIL, а восстановление не приписывается producer.
func rsDeletedRestore(h *rsHarness, f *rsForgeProcess, snap rsDeletedSnapshot, label string) {
	current := rsDeletedRefs(h, f)
	names := []string{}
	for name := range current {
		names = append(names, name)
	}
	for name := range snap.refs {
		if _, ok := current[name]; !ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		old, want := current[name], snap.refs[name]
		if old == want {
			continue
		}
		if want == "" {
			h.must(h.root, "git", "--git-dir", f.repository, "update-ref", "-d", name, old)
		} else {
			if old == "" {
				old = strings.Repeat("0", 40)
			}
			h.must(h.root, "git", "--git-dir", f.repository, "update-ref", name, want, old)
		}
	}
	control := map[string]any{"deleted_head_restore": map[string]any{"token": label, "prs": snap.prs, "notes": snap.notes}}
	rsDeletedControl(h, f, control)
	status, _, err := rsForgeCall(h, f, "GET", "", nil)
	if err != nil || status != 200 {
		h.t.Fatal("HARNESS_NOT_EXECUTED: explicit fixture metadata restoration")
	}
	rsDeletedControl(h, f, nil)
	after := rsDeletedState(h, f)
	if !reflect.DeepEqual(rsDeletedRefs(h, f), snap.refs) || !reflect.DeepEqual(after["prs"], snap.prs) || !reflect.DeepEqual(after["notes"], snap.notes) || len(after["writes"].([]any)) < snap.writeCount {
		h.t.Fatal("HARNESS_NOT_EXECUTED: restore changed prior capture/journal")
	}
	h.save(label+"-restored.json", rsCanonicalJSON(h.t, map[string]any{"refs": rsDeletedRefs(h, f), "prs": after["prs"], "notes": after["notes"], "journal_entries_retained": len(after["writes"].([]any)), "fixture_only": true}))
}

func rsDeletedReadPR(h *rsHarness, f *rsForgeProcess) map[string]any {
	status, raw, err := rsForgeCall(h, f, "GET", "/pulls/101", nil)
	if err != nil || status != 200 {
		h.t.Fatal("HARNESS_NOT_EXECUTED: native fixture PR readback")
	}
	var pr map[string]any
	if json.Unmarshal(raw, &pr) != nil {
		h.t.Fatal("HARNESS_NOT_EXECUTED: fixture PR JSON")
	}
	return pr
}

func rsDeletedProve(h *rsHarness, f *rsForgeProcess, candidate, base, plan, label string, deleted bool) string {
	pr := rsDeletedReadPR(h, f)
	merge, _ := pr["merge_commit_sha"].(string)
	if pr["merged"] != true || pr["mergeable"] != nil || merge == candidate || merge == base || len(merge) != 40 {
		h.t.Fatal("HARNESS_NOT_EXECUTED: exact merged detail")
	}
	if pr["head"].(map[string]any)["sha"] != candidate {
		h.t.Fatal("HARNESS_NOT_EXECUTED: retained original actual head")
	}
	parents := strings.Fields(h.must(h.root, "git", "--git-dir", f.repository, "rev-list", "--parents", "-n", "1", merge))
	if len(parents) != 2 || parents[1] != base {
		h.t.Fatal("HARNESS_NOT_EXECUTED: expected one native squash parent")
	}
	tree := h.must(h.root, "git", "--git-dir", f.repository, "rev-parse", merge+"^{tree}")
	if tree != h.must(h.root, "git", "--git-dir", f.repository, "rev-parse", candidate+"^{tree}") {
		h.t.Fatal("HARNESS_NOT_EXECUTED: native squash content")
	}
	h.must(h.root, "git", "--git-dir", f.repository, "merge-base", "--is-ancestor", merge, "refs/heads/main")
	_, _, rc := h.run(h.root, nil, "git", "--git-dir", f.repository, "merge-base", "--is-ancestor", candidate, merge)
	if rc != 1 {
		h.t.Fatal("HARNESS_NOT_EXECUTED: squash must not imply candidate ancestry")
	}
	ref := "refs/heads/release/module-" + plan
	_, _, refRC := h.run(h.root, nil, "git", "--git-dir", f.repository, "show-ref", "--verify", "--quiet", ref)
	refStatus, _, err := rsForgeCall(h, f, "GET", "/git/ref/heads/release/module-"+plan, nil)
	if err != nil || (deleted && (refRC != 1 || refStatus != 404)) || (!deleted && (refRC != 0 || refStatus != 200)) {
		h.t.Fatal("HARNESS_NOT_EXECUTED: branch-presence three-boundary proof")
	}
	ls := h.must(h.root, "git", "ls-remote", f.repository, ref)
	if deleted && ls != "" {
		h.t.Fatal("HARNESS_NOT_EXECUTED: remote branch not deleted")
	}
	status, raw, err := rsForgeCall(h, f, "GET", "/pulls?state=all", nil)
	var list []map[string]any
	if err != nil || status != 200 || json.Unmarshal(raw, &list) != nil || len(list) != 1 {
		h.t.Fatal("HARNESS_NOT_EXECUTED: unique list")
	}
	if _, ok := list[0]["merged"]; ok {
		h.t.Fatal("HARNESS_NOT_EXECUTED: list fabricates detail merged")
	}
	if _, ok := list[0]["mergeable"]; ok {
		h.t.Fatal("HARNESS_NOT_EXECUTED: list fabricates detail mergeable")
	}
	h.save(label+"-native-proof.json", rsCanonicalJSON(h.t, map[string]any{"kind": "ACTUAL_GIT_AND_FORGE_PREREQUISITE", "candidate": candidate, "merge": merge, "parents": parents[1:], "tree": tree, "deleted": deleted, "ref": ref, "ref_exit": refRC, "ref_http_status": refStatus, "list": list, "detail": pr, "candidate_is_ancestor": false}))
	return merge
}

type rsDeletedCase struct {
	name   string
	phases []string
}

func rsDeletedCases() []rsDeletedCase {
	return []rsDeletedCase{
		{"present-head", []string{"deliver", "release", "probe"}},
		{"deleted-head", []string{"deliver", "release", "probe"}},
		{"ordinary-main-advance", []string{"release"}},
		{"wrong-pr-head", []string{"deliver", "release", "probe"}},
		{"duplicate-discovery", []string{"deliver", "release", "probe"}},
		{"open-unmerged-without-head", []string{"deliver", "release"}},
		{"discovery-empty-after-merge", []string{"deliver", "release", "probe"}},
		{"ancestry-lost", []string{"deliver", "release", "probe"}},
		{"live-branch-conflict", []string{"deliver", "release", "probe"}},
		{"detail-unavailable", []string{"release"}},
		{"detail-missing", []string{"release"}},
		{"detail-number-mismatch", []string{"deliver"}},
		{"detail-merged-absent", []string{"release"}},
		{"detail-merged-wrong-type", []string{"release"}},
		{"detail-head-repo-mismatch", []string{"release"}},
		{"detail-head-ref-mismatch", []string{"release"}},
		{"detail-base-ref-mismatch", []string{"release"}},
		{"detail-plan-marker-mismatch", []string{"release"}},
		{"list-detail-head-disagreement", []string{"deliver"}},
		{"detail-invalid-merge-sha", []string{"release"}},
		{"merged-content-mismatch", []string{"release"}},
		{"required-checks-failed", []string{"release"}},
	}
}

func rsDeletedExpectation(profile, phase string, present bool) rsPublisherCase {
	c := rsPublisherCase{Name: profile, Phase: phase, Outcome: "GREEN", Reason: "OK", Stage: "MERGED_VERIFIED", Effects: map[string]string{"pr": "PRESENT", "merge": "PRESENT"}}
	if present {
		c.Effects["branch"] = "PRESENT"
	}
	if phase == "release" {
		c.Stage = "TAG_PRESENT"
		c.Effects["tag"] = "PRESENT"
		c.Effects["release-note"] = "PRESENT"
	}
	if phase == "probe" {
		c.Stage = "ARCHIVE_VERIFIED"
		c.Effects = map[string]string{}
	}
	if profile == "present-head" || profile == "deleted-head" || profile == "ordinary-main-advance" || profile == "lawful" {
		return c
	}
	c.Outcome = "RED"
	c.Stage = "NONE"
	c.Effects = map[string]string{}
	switch profile {
	case "wrong-pr-head", "list-detail-head-disagreement":
		c.Reason = "PR_HEAD_CHANGED"
		c.Effects["pr"] = "CONFLICT"
	case "duplicate-discovery", "detail-number-mismatch", "detail-head-repo-mismatch", "detail-head-ref-mismatch", "detail-base-ref-mismatch", "detail-plan-marker-mismatch":
		c.Reason = "EFFECT_IDENTITY_CONFLICT"
		c.Effects["pr"] = "CONFLICT"
	case "live-branch-conflict":
		c.Reason = "EFFECT_IDENTITY_CONFLICT"
		c.Effects["branch"] = "CONFLICT"
	case "ancestry-lost", "merged-content-mismatch", "required-checks-failed":
		c.Reason = map[string]string{"ancestry-lost": "TARGET_NOT_ON_MAIN", "merged-content-mismatch": "CONTENT_MISMATCH", "required-checks-failed": "REQUIRED_CHECKS_FAILED"}[profile]
		c.Stage = "MERGE_PRESENT"
		c.Effects = map[string]string{"pr": "PRESENT", "merge": "PRESENT"}
	case "open-unmerged-without-head":
		c.Outcome = "NOT_EXECUTED"
		c.Reason = "SOURCE_UNAVAILABLE"
		c.Stage = "PR_OPEN"
		c.Effects["pr"] = "PRESENT"
	case "discovery-empty-after-merge":
		c.Outcome = "NOT_EXECUTED"
		c.Reason = "SOURCE_UNAVAILABLE"
		if phase == "deliver" {
			c.Outcome = "RED"
			c.Reason = "INPUT_CHANGED"
		}
	default:
		c.Outcome = "NOT_EXECUTED"
		c.Reason = "SOURCE_UNAVAILABLE"
	}
	if phase == "probe" {
		c.Effects = map[string]string{}
	}
	return c
}

// Эта проверка смотрит actual writes и refs, а не доверяет result.effects.
func rsDeletedCheckMutation(h *rsHarness, f *rsForgeProcess, before rsDeletedSnapshot, o rsPublisherObservation, phase string, allowRelease bool, target string) {
	after := rsDeletedState(h, f)
	writes := after["writes"].([]any)[before.writeCount:]
	counts := map[string]int{}
	for _, raw := range writes {
		counts[raw.(map[string]any)["kind"].(string)]++
	}
	pushes := 0
	for _, cmd := range o.commands {
		if filepath.Base(cmd["program"].(string)) != "git" {
			continue
		}
		args := []string{}
		for _, x := range cmd["args"].([]any) {
			args = append(args, x.(string))
		}
		if rsPublisherVerb(h.t, args) == "push" {
			pushes++
			if !allowRelease || phase != "release" || !strings.HasSuffix(args[len(args)-1], ":refs/tags/v1.0.1") {
				h.t.Errorf("SEMANTIC_MISMATCH: forbidden native push %q", args)
			}
		}
	}
	refs := rsDeletedRefs(h, f)
	want := map[string]string{}
	for k, v := range before.refs {
		want[k] = v
	}
	if allowRelease {
		if counts["pr"] != 0 || counts["merge"] != 0 || counts["release-note"] > 1 || pushes > 1 {
			h.t.Errorf("SEMANTIC_MISMATCH: release writes %+v pushes%d", counts, pushes)
		}
		want["refs/tags/v1.0.1"] = target
		if refs["refs/tags/v1.0.1"] != target {
			h.t.Error("SEMANTIC_MISMATCH: missing/wrong actual tag target")
		}
		wantPushes := 0
		if before.refs["refs/tags/v1.0.1"] == "" {
			wantPushes = 1
		}
		wantNotes := 0
		if len(before.notes) == 0 {
			wantNotes = 1
		}
		notes := after["notes"].([]any)
		if pushes != wantPushes || len(writes) != wantNotes || counts["release-note"] != wantNotes || len(notes) != 1 {
			h.t.Errorf("SEMANTIC_MISMATCH: exact release creation count pushes%d/%d notes%d/%d stored%d", pushes, wantPushes, counts["release-note"], wantNotes, len(notes))
		} else {
			note := notes[0].(map[string]any)
			if note["tag_name"] != "v1.0.1" || note["target_commitish"] != target {
				h.t.Error("SEMANTIC_MISMATCH: actual note identity")
			}
		}
		if !reflect.DeepEqual(after["prs"], before.prs) {
			h.t.Error("SEMANTIC_MISMATCH: release changed PR")
		}
	} else if len(writes) != 0 || pushes != 0 {
		h.t.Errorf("SEMANTIC_MISMATCH: forbidden dependent writes %+v pushes%d", counts, pushes)
	}
	if !reflect.DeepEqual(refs, want) {
		h.t.Errorf("SEMANTIC_MISMATCH: unexpected remote ref delta actual%v expected%v", refs, want)
	}
	if !allowRelease && (!reflect.DeepEqual(after["prs"], before.prs) || !reflect.DeepEqual(after["notes"], before.notes)) {
		h.t.Error("SEMANTIC_MISMATCH: unexpected PR/note mutation")
	}
	h.save(filepath.Base(o.directory)+"-effects-measured.json", rsCanonicalJSON(h.t, map[string]any{"before_refs": before.refs, "after_refs": refs, "http_writes": writes, "native_pushes": pushes, "release_creation_permitted": allowRelease}))
}

func rsDeletedArgs(phase, tree, manifest, plan, landed string) []string {
	if phase == "probe" {
		return []string{"--phase", "probe", "--repo", "PRO-Robotech/corelib", "--version", "v1.0.1", "--manifest", manifest, "--network-budget", "7", "--checks-budget", "7"}
	}
	a := []string{"--phase", phase, "--tree", tree, "--repo", "PRO-Robotech/corelib", "--version", "v1.0.1", "--manifest", manifest, "--via-pull-request", "--network-budget", "7", "--checks-budget", "7"}
	if phase == "release" {
		a = append(a, "--landed-sha", landed)
	}
	if phase != "plan" {
		a = append(a, "--commit", "PRO-Robotech/corelib", "--plan-sha256", plan)
	}
	return a
}

// Узкое поле evidence; полный verdict остаётся у schema/predicate/effect/write assertions.
func rsDeletedResultMatches(o rsPublisherObservation, want rsPublisherCase) bool {
	return o.result["outcome"] == want.Outcome && o.result["reason"] == want.Reason && o.result["stage"] == want.Stage
}

func rsDeletedInvoke(h *rsHarness, p *rsPublisherFixture, f *rsForgeProcess, binary, label string, args []string, want rsPublisherCase, plan, candidate, target string, primary bool) rsPublisherObservation {
	before := rsDeletedSnapshotOf(h, f, label+"-before")
	o := rsInvokePublisher(h, p, f, binary, label, args)
	rsCheckPublisher(h, want, o, plan, candidate)
	rsDeletedCheckMutation(h, f, before, o, want.Phase, want.Phase == "release" && want.Outcome == "GREEN", target)
	matched := rsDeletedResultMatches(o, want)
	h.save(label+"-cell.json", rsCanonicalJSON(h.t, map[string]any{"label": label, "primary_matrix_cell": primary, "profile": want.Name, "phase": want.Phase, "expected_outcome": want.Outcome, "expected_reason": want.Reason, "expected_stage": want.Stage, "actual_outcome": o.result["outcome"], "actual_reason": o.result["reason"], "actual_stage": o.result["stage"], "outcome_reason_stage_match": matched, "sut_invoked": true, "result_path": filepath.Join(o.directory, "sut.stdout")}))
	return o
}

func rsDeletedFixtureBirth(h *rsHarness, p *rsPublisherFixture, script string) {
	f := rsStartDeletedForge(h, p, "deleted-birth", script)
	plan := rsSHA([]byte("independent deleted-head native birth"))
	ref := "refs/heads/release/module-" + plan
	client := filepath.Join(h.root, "candidate-publisher-ready")
	h.must(client, "git", "-c", "protocol.allow=never", "-c", "protocol.file.allow=always", "push", "--receive-pack="+f.wrapper, "file://"+f.repository, p.published.revision+":"+ref)
	status, _, err := rsForgeCall(h, f, "POST", "/pulls", map[string]any{"head": strings.TrimPrefix(ref, "refs/heads/"), "base": "main", "body": "CI-RS-1 plan-sha256:" + plan})
	if err != nil || status != 201 {
		h.t.Fatal("HARNESS_NOT_EXECUTED: actual independent PR birth")
	}
	status, _, err = rsForgeCall(h, f, "PUT", "/pulls/101/merge", map[string]any{"sha": p.published.revision, "merge_method": "squash"})
	if err != nil || status != 200 {
		h.t.Fatal("HARNESS_NOT_EXECUTED: actual independent squash birth")
	}
	rsDeletedProve(h, f, p.published.revision, p.f.base, plan, "birth-present", false)
	h.must(h.root, "git", "--git-dir", f.repository, "update-ref", "-d", ref, p.published.revision)
	rsDeletedProve(h, f, p.published.revision, p.f.base, plan, "birth-deleted", true)
}

func rsDeletedNativeTag(h *rsHarness, f *rsForgeProcess, target string) {
	h.must(h.root, "git", "--git-dir", f.repository, "update-ref", "refs/tags/v1.0.1", target, strings.Repeat("0", 40))
	status, _, err := rsForgeCall(h, f, "POST", "/releases", map[string]any{"tag_name": "v1.0.1", "target_commitish": target, "body": "actual preexisting probe fixture note"})
	if err != nil || status != 201 {
		h.t.Fatal("HARNESS_NOT_EXECUTED: actual probe tag/note prerequisite")
	}
}

func TestReleaseSupplyPublisherDeletedHead(t *testing.T) {
	p := rsPreparePublisher(t)
	h := p.f.h
	script := rsDeletedHeadScript(h)
	rsDeletedFixtureBirth(h, p, script)
	binary := rsBuildPublisherBridge(h)
	cases := rsDeletedCases()
	cells := 0
	for _, c := range cases {
		cells += len(c.phases)
	}
	if len(cases) != 22 || cells != 37 {
		t.Fatal("HARNESS_NOT_EXECUTED: declared exact case census")
	}
	h.save("deleted-head-census.json", rsCanonicalJSON(t, map[string]any{"profiles": 22, "phase_case_cells": 37, "lawful_cells": 7, "negative_cells": 30, "extra_release_resume": 1, "extra_historical_diagnostic_cells": 2, "source": "frozen candidate resolved by bridge", "fixture_prerequisites": "actual Git one-parent squash, distinct SHA/equal tree, retained detail and deleted ref"}))
	for _, c := range cases {
		for _, phase := range c.phases {
			t.Run(c.name+"/"+phase, func(t *testing.T) {
				ch := *h
				ch.t = t
				ch.serial = 0
				label := c.name + "-" + phase
				if h.capture != "" {
					ch.capture = filepath.Join(h.capture, label)
					if err := os.MkdirAll(ch.capture, 0755); err != nil {
						t.Fatal(err)
					}
				}
				rsDeletedRunCell(&ch, p, script, binary, c.name, phase, label)
			})
		}
	}
	t.Run("historical-shape-diagnostic", func(t *testing.T) {
		ch := *h
		ch.t = t
		ch.serial = 0
		if h.capture != "" {
			ch.capture = filepath.Join(h.capture, "historical-shape-diagnostic")
			if err := os.MkdirAll(ch.capture, 0755); err != nil {
				t.Fatal(err)
			}
		}
		rsDeletedRunDiagnostic(&ch, p, script, binary)
	})
}

func rsDeletedSetup(h *rsHarness, p *rsPublisherFixture, f *rsForgeProcess, binary, label string, unmerged bool) (string, string, string, string) {
	manifest := filepath.Join(h.root, label+"-manifest.json")
	h.put(h.root, filepath.Base(manifest), string(rsCanonicalJSON(h.t, p.manifest)))
	planning := rsInvokePublisher(h, p, f, binary, label+"-plan", rsDeletedArgs("plan", p.tree, manifest, "", ""))
	if planning.result["outcome"] != "GREEN" {
		h.t.Fatalf("SEMANTIC_MISMATCH: valid ready-tree plan failed: %v/%v", planning.result["outcome"], planning.result["reason"])
	}
	plan, candidate := planning.result["plan_sha256"].(string), planning.result["candidate_sha"].(string)
	rsCheckPublisher(h, rsPublisherCase{Name: "fixture-plan", Phase: "plan", Outcome: "GREEN", Reason: "OK", Stage: "NONE", DryRun: true, Effects: map[string]string{}}, planning, plan, candidate)
	if unmerged {
		rsDeletedControl(h, f, map[string]any{"fault": "merge-rejected"})
	}
	delivered := rsInvokePublisher(h, p, f, binary, label+"-actual-delivery", rsDeletedArgs("deliver", p.tree, manifest, plan, ""))
	want := rsDeletedExpectation("lawful", "deliver", true)
	if unmerged {
		want.Outcome = "RED"
		want.Reason = "REMOTE_WRITE_REJECTED"
		want.Stage = "PR_OPEN"
		want.Effects["merge"] = "ABSENT"
	}
	rsCheckPublisher(h, want, delivered, plan, candidate)
	if delivered.result["outcome"] != want.Outcome || delivered.result["reason"] != want.Reason {
		h.t.Fatalf("SEMANTIC_MISMATCH: actual fixture setup SUT stopped at %v/%v", delivered.result["outcome"], delivered.result["reason"])
	}
	rsDeletedControl(h, f, nil)
	if unmerged {
		pr := rsDeletedReadPR(h, f)
		if pr["merged"] != false || pr["state"] != "open" || pr["merge_commit_sha"] != nil || pr["head"].(map[string]any)["sha"] != candidate {
			h.t.Fatal("HARNESS_NOT_EXECUTED: actual unmerged PR prerequisite")
		}
		h.save(label+"-open-pr-prerequisite.json", rsCanonicalJSON(h.t, pr))
		return manifest, plan, candidate, p.f.base
	}
	landed := rsDeletedProve(h, f, candidate, p.f.base, plan, label+"-setup", false)
	return manifest, plan, candidate, landed
}

func rsDeletedRunCell(h *rsHarness, p *rsPublisherFixture, script, binary, profile, phase, label string) {
	f := rsStartDeletedForge(h, p, label, script)
	manifest, plan, candidate, landed := rsDeletedSetup(h, p, f, binary, label, profile == "open-unmerged-without-head")
	ref := "refs/heads/release/module-" + plan
	if profile == "open-unmerged-without-head" {
		openWithHead := rsDeletedSnapshotOf(h, f, label+"-actual-open")
		h.must(h.root, "git", "--git-dir", f.repository, "update-ref", "-d", ref, candidate)
		before := rsDeletedSnapshotOf(h, f, label+"-open-deleted")
		pr := rsDeletedReadPR(h, f)
		if pr["merged"] != false || pr["head"].(map[string]any)["sha"] != candidate {
			h.t.Fatal("HARNESS_NOT_EXECUTED: retained actual unmerged head")
		}
		rsDeletedInvoke(h, p, f, binary, label+"-primary", rsDeletedArgs(phase, p.tree, manifest, plan, landed), rsDeletedExpectation(profile, phase, false), plan, candidate, landed, true)
		rsDeletedRestore(h, f, openWithHead, label+"-open-state-restore")
		status, _, err := rsForgeCall(h, f, "PUT", "/pulls/101/merge", map[string]any{"sha": candidate, "merge_method": "squash"})
		if err != nil || status != 200 {
			h.t.Fatal("HARNESS_NOT_EXECUTED: unmerged lawful twin needs actual merge")
		}
		landed = rsDeletedProve(h, f, candidate, p.f.base, plan, label+"-accepted-twin-present", false)
		h.must(h.root, "git", "--git-dir", f.repository, "update-ref", "-d", ref, candidate)
		rsDeletedProve(h, f, candidate, p.f.base, plan, label+"-accepted-twin-deleted", true)
		after := rsDeletedSnapshotOf(h, f, label+"-accepted-event")
		h.save(label+"-logical-event-delta.json", rsCanonicalJSON(h.t, map[string]any{"kind": "ACTUAL_ACCEPTED_MERGE_EVENT_NOT_SINGLE_FIELD", "before_refs": before.refs, "after_refs": after.refs, "before_prs": before.prs, "after_prs": after.prs, "old_landed_arg": p.f.base, "new_landed_arg": landed, "branch_absent_on_both_sides": true}))
		o := rsDeletedInvoke(h, p, f, binary, label+"-accepted-lawful", rsDeletedArgs(phase, p.tree, manifest, plan, landed), rsDeletedExpectation("lawful", phase, false), plan, candidate, landed, false)
		h.save(label+"-pair.json", rsCanonicalJSON(h.t, map[string]any{"primary": profile, "phase": phase, "lawful_twin_reached": true, "lawful_green": o.result["outcome"] == "GREEN", "pair_kind": "actual accepted merge event; not ref restoration"}))
		return
	}
	if phase == "probe" {
		rsDeletedNativeTag(h, f, landed)
	}
	if profile != "present-head" {
		h.must(h.root, "git", "--git-dir", f.repository, "update-ref", "-d", ref, candidate)
	}
	rsDeletedProve(h, f, candidate, p.f.base, plan, label+"-ready", profile != "present-head")
	args := rsDeletedArgs(phase, p.tree, manifest, plan, landed)
	if profile == "present-head" || profile == "deleted-head" {
		o := rsDeletedInvoke(h, p, f, binary, label+"-primary", args, rsDeletedExpectation(profile, phase, profile == "present-head"), plan, candidate, landed, true)
		if profile == "deleted-head" && phase == "release" {
			before := rsDeletedSnapshotOf(h, f, label+"-resume-before")
			repeated := rsDeletedInvoke(h, p, f, binary, label+"-explicit-release-resume", args, rsDeletedExpectation(profile, phase, false), plan, candidate, landed, false)
			rsDeletedCheckMutation(h, f, before, repeated, phase, false, landed)
			h.save(label+"-resume-precondition.json", rsCanonicalJSON(h.t, map[string]any{"kind": "EXPLICIT_RELEASE_RESUME_NOT_MATRIX_CELL", "prior_green": o.result["outcome"] == "GREEN", "same_argv": true}))
		}
		return
	}
	lawful := rsDeletedInvoke(h, p, f, binary, label+"-lawful-before", args, rsDeletedExpectation("lawful", phase, false), plan, candidate, landed, false)
	before := rsDeletedSnapshotOf(h, f, label+"-before-delta")
	control := map[string]any{"deleted_head_fault": profile}
	negativeTarget := landed
	switch profile {
	case "ordinary-main-advance":
		tree := h.must(h.root, "git", "--git-dir", f.repository, "rev-parse", landed+"^{tree}")
		next := rsFixtureCommit(h, f.repository, tree, landed, "owned ordinary main descendant")
		h.must(h.root, "git", "--git-dir", f.repository, "update-ref", "refs/heads/main", next, landed)
	case "ancestry-lost":
		h.must(h.root, "git", "--git-dir", f.repository, "update-ref", "refs/heads/main", p.f.base, landed)
		// Нужен именно доказанный false ancestry, а не недоступный объект.
		h.must(h.root, "git", "--git-dir", f.repository, "cat-file", "commit", landed)
		_, _, rc := h.run(h.root, nil, "git", "--git-dir", f.repository, "merge-base", "--is-ancestor", landed, "refs/heads/main")
		if rc != 1 {
			h.t.Fatal("HARNESS_NOT_EXECUTED: actual lost ancestry")
		}
	case "live-branch-conflict":
		h.must(h.root, "git", "--git-dir", f.repository, "update-ref", ref, p.f.base, strings.Repeat("0", 40))
	case "duplicate-discovery":
		control["fault"] = "pr-duplicate"
	case "required-checks-failed":
		control["fault"] = "checks-failed"
	case "merged-content-mismatch":
		tree := h.must(h.root, "git", "--git-dir", f.repository, "rev-parse", p.f.base+"^{tree}")
		wrong := rsFixtureCommit(h, f.repository, tree, p.f.base, "owned accepted merge with different native tree")
		changed := rsDeletedSnapshot{refs: map[string]string{}, prs: rsCloneJSON(map[string]any{"prs": before.prs})["prs"].([]any), notes: before.notes, writeCount: before.writeCount}
		for k, v := range before.refs {
			changed.refs[k] = v
		}
		changed.refs["refs/heads/main"] = wrong
		changed.prs[0].(map[string]any)["merge_commit_sha"] = wrong
		rsDeletedRestore(h, f, changed, label+"-native-content-event")
		negativeTarget = wrong
		args = rsDeletedArgs(phase, p.tree, manifest, plan, wrong)
		if tree == h.must(h.root, "git", "--git-dir", f.repository, "rev-parse", candidate+"^{tree}") {
			h.t.Fatal("HARNESS_NOT_EXECUTED: wrong content twin unchanged")
		}
	}
	rsDeletedControl(h, f, control)
	after := rsDeletedSnapshotOf(h, f, label+"-after-delta")
	h.save(label+"-computed-delta.json", rsCanonicalJSON(h.t, map[string]any{"profile": profile, "control": control, "before_refs": before.refs, "after_refs": after.refs, "before_prs": before.prs, "after_prs": after.prs, "old_landed_arg": landed, "new_landed_arg": negativeTarget, "input_manifest_sha256": rsSHA(mustRSRead(h.t, manifest)), "candidate_sha": candidate, "plan": plan}))
	primary := rsDeletedInvoke(h, p, f, binary, label+"-primary", args, rsDeletedExpectation(profile, phase, false), plan, candidate, negativeTarget, true)
	rsDeletedRestore(h, f, before, label+"-lawful-state-restored")
	restored := rsDeletedInvoke(h, p, f, binary, label+"-lawful-restored", rsDeletedArgs(phase, p.tree, manifest, plan, landed), rsDeletedExpectation("lawful", phase, false), plan, candidate, landed, false)
	h.save(label+"-pair.json", rsCanonicalJSON(h.t, map[string]any{"primary": profile, "phase": phase, "lawful_before_green": lawful.result["outcome"] == "GREEN", "lawful_restored_green": restored.result["outcome"] == "GREEN", "outcome_reason_stage_pair_matches": rsDeletedResultMatches(lawful, rsDeletedExpectation("lawful", phase, false)) && rsDeletedResultMatches(restored, rsDeletedExpectation("lawful", phase, false)) && rsDeletedResultMatches(primary, rsDeletedExpectation(profile, phase, false)), "full_verdict_requires_parent_assertions": true, "masked_cause_not_inferred": lawful.result["outcome"] != "GREEN" || restored.result["outcome"] != "GREEN"}))
}

// Эти два invocation изолируют deleted-ref отказ прежнего source. Enriched
// list явно исторический diagnostic input; обязательные37 cells используют
// настоящую форму list без detail-only полей.
func rsDeletedRunDiagnostic(h *rsHarness, p *rsPublisherFixture, script, binary string) {
	f := rsStartDeletedForge(h, p, "historical-shape-diagnostic", script)
	manifest, plan, candidate, landed := rsDeletedSetup(h, p, f, binary, "historical-shape-diagnostic", false)
	rsDeletedControl(h, f, map[string]any{"deleted_head_shape": "historical-enriched"})
	args := rsDeletedArgs("release", p.tree, manifest, plan, landed)
	present := rsDeletedInvoke(h, p, f, binary, "diagnostic-present", args, rsDeletedExpectation("lawful", "release", true), plan, candidate, landed, false)
	h.must(h.root, "git", "--git-dir", f.repository, "update-ref", "-d", "refs/heads/release/module-"+plan, candidate)
	absent := rsDeletedInvoke(h, p, f, binary, "diagnostic-deleted", args, rsDeletedExpectation("lawful", "release", false), plan, candidate, landed, false)
	h.save("historical-shape-attribution.json", rsCanonicalJSON(h.t, map[string]any{"kind": "DIAGNOSTIC_HISTORICAL_ENRICHED_LIST_NOT_REAL_API_MATRIX", "same_args": args, "single_delta": "actual receiving head ref deletion", "present_outcome": present.result["outcome"], "absent_outcome": absent.result["outcome"], "present_reason": present.result["reason"], "absent_reason": absent.result["reason"], "real_shape_cells_not_replaced": true}))
}

// Lifecycle собственного дочернего forge сохранён из rsStartForge; меняется
// только явный путь к временному derived script.
func rsStartDeletedForge(h *rsHarness, p *rsPublisherFixture, label, script string) *rsForgeProcess {
	h.t.Helper()
	dir := filepath.Join(h.root, "forge-"+label)
	if err := os.MkdirAll(dir, 0755); err != nil {
		h.t.Fatal(err)
	}
	origin := filepath.Join(dir, "receiving.git")
	h.must(h.root, "git", "clone", "--bare", "--no-local", p.f.origin, origin)
	control := filepath.Join(dir, "control.json")
	h.put(dir, "control.json", `{"fault":"lawful"}`)
	capture := filepath.Join(dir, "captures")
	state := filepath.Join(dir, "state.json")
	escaped, err := module.EscapePath(p.f.module)
	if err != nil {
		h.t.Fatal(err)
	}
	published := map[string]string{}
	for _, suffix := range []string{"v1.0.1.info", "v1.0.1.mod", "v1.0.1.zip"} {
		published[suffix] = filepath.Join(p.published.proxy, escaped, "@v", suffix)
	}
	config := map[string]any{"root": h.root, "captures": capture, "repository": origin, "control": control, "state": state, "http": p.f.http, "published": published, "wrong_zip": p.wrongPackage.path, "wrong_payload_zip": p.wrongPayload.path, "base": p.f.base}
	path := filepath.Join(dir, "config.json")
	h.put(dir, "config.json", string(rsCanonicalJSON(h.t, config)))
	ctx, cancel := context.WithCancel(context.Background())
	c := exec.CommandContext(ctx, "bash", script, "--fixture-forge", path)
	c.Dir = h.root
	c.Env = rsEnvironment()
	proc := &rsForgeProcess{cmd: c, control: control, state: state, capture: capture, repository: origin}
	c.Stderr = &proc.stderr
	stdout, e := c.StdoutPipe()
	if e != nil {
		h.t.Fatal(e)
	}
	if e = c.Start(); e != nil {
		h.t.Fatal(e)
	}
	lines := make(chan []byte, 1)
	go func() { line, _ := bufio.NewReader(stdout).ReadBytes('\n'); lines <- line }()
	select {
	case line := <-lines:
		var handshake struct {
			Endpoint string `json:"endpoint"`
			PID      int    `json:"pid"`
		}
		if e = json.Unmarshal(line, &handshake); e != nil || handshake.PID != c.Process.Pid {
			cancel()
			_ = c.Wait()
			h.t.Fatalf("HARNESS_NOT_EXECUTED: forge handshake %s %v", line, e)
		}
		proc.endpoint = handshake.Endpoint
		h.save(label+"-forge-handshake.json", line)
	case <-time.After(10 * time.Second):
		cancel()
		_ = c.Wait()
		h.t.Fatal("HARNESS_NOT_EXECUTED: forge startup deadline")
	}
	h.t.Cleanup(func() {
		if !proc.stopped {
			cancel()
			waitErr := c.Wait()
			proc.stopped = true
			h.save(label+"-forge-process.json", rsCanonicalJSON(h.t, map[string]any{"pid": c.Process.Pid, "wait_completed": true, "exit_code": c.ProcessState.ExitCode(), "cleanup_cancellation": true, "wait_result": fmt.Sprint(waitErr)}))
		}
		h.save(label+"-forge.stderr", proc.stderr.Bytes())
		rsCopyCaptures(h, proc.capture, label+"-forge")
		transportCapture := filepath.Join(filepath.Dir(proc.repository), "transport-captures")
		rsVerifyHookChildren(h, transportCapture)
		rsCopyCaptures(h, transportCapture, label+"-transport")
		if b, e := os.ReadFile(proc.state); e == nil {
			h.save(label+"-forge-state.json", b)
		}
	})
	proc.wrapper = rsInstallPublisherTransport(h, origin, "lawful", "", "")
	return proc
}
