// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package authzformbench

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
)

// BenchType — the leaf resource type the shapes are measured on.
//
// A leaf, project-anchored, four-verb type is the shape 22 of the model's 24
// verb-bearing types have, so it is the representative case rather than a corner.
// It is a constant and not a parameter because the C/D transforms below rewrite
// THIS type's block, and a transform that silently matched nothing would leave the
// shape indistinguishable from A while claiming to be C.
const BenchType = "vpc_network"

// ── the two model transforms ──────────────────────────────────────────────────
//
// C and D are expressed as transforms OF the canonical text, never as a hand-written
// model of their own. A hand-written model would be a second place saying what the
// authorization graph is, and the two would drift — silently, because a drift in
// prose gives no merge conflict. Transforming means the only difference between the
// measured model and the deployed one is the difference under study.

var (
	// A verb line as the canon writes it, e.g.
	//   "    define v_get: [user, service_account, group#member] or super_admin"
	verbLineRe = regexp.MustCompile(`^(\s*define (v_[a-z]+): )(.+)$`)
	typeLineRe = regexp.MustCompile(`^type (\S+)\s*$`)
)

// readVerbs are the verbs a "viewer"-shaped role grants; the rest belong to
// "editor". Kept here, next to the transforms that must agree with them, so a
// change cannot land in one and not the other.
var readVerbs = map[string]bool{"v_get": true, "v_list": true}

// transformResult carries what a transform DID, so a caller can assert it did
// something. A transform that matched nothing must not be able to masquerade as a
// successful one — that is the whole failure mode of text rewriting.
type transformResult struct {
	DSL          string
	VerbsTouched []string
	AddedLines   int
}

// ModelC — role relations ON the object. Verbs stop being separately granted and
// derive from one relation per role.
//
//	define grant_viewer: [user, service_account, group#member]
//	define grant_editor: [user, service_account, group#member]
//	define v_get: <canon> or grant_viewer or grant_editor
//	define v_update: <canon> or grant_editor
//
// The cost this makes visible: C can only express the verb subsets it DECLARES. Two
// role shapes are two relations; an arbitrary subset is a model change and a new
// model id. That is not a detail of the harness, it is the price of folding M.
func ModelC(canon string) (transformResult, error) {
	return rewriteType(canon, BenchType,
		[]string{
			"    define grant_viewer: [user, service_account, group#member]",
			"    define grant_editor: [user, service_account, group#member]",
		},
		func(verb, rhs string) string {
			if readVerbs[verb] {
				return rhs + " or grant_viewer or grant_editor"
			}
			return rhs + " or grant_editor"
		}, "")
}

// ModelD — a container type that IS the materialized label set. The object points
// at it with ONE tuple; the grant is written on the container.
//
//	type label_set
//	  relations
//	    define grant_viewer: [user, service_account, group#member]
//	    define grant_editor: [user, service_account, group#member]
//
//	define labels: [label_set]
//	define v_get: <canon> or grant_viewer from labels or grant_editor from labels
//
// D is also the model form B+C+D uses: `group#member` is already an accepted
// subject on grant_*, so the combined shape needs no further model change — it is a
// discipline of WHO the grant names, not of what the model declares.
func ModelD(canon string) (transformResult, error) {
	return rewriteType(canon, BenchType,
		[]string{"    define labels: [label_set]"},
		func(verb, rhs string) string {
			if readVerbs[verb] {
				return rhs + " or grant_viewer from labels or grant_editor from labels"
			}
			return rhs + " or grant_editor from labels"
		},
		strings.Join([]string{
			"",
			"# authzformbench form D — the container that IS the materialized label set.",
			"# Not part of the canonical model; added by the harness transform only.",
			"type label_set",
			"  relations",
			"    define grant_viewer: [user, service_account, group#member]",
			"    define grant_editor: [user, service_account, group#member]",
		}, "\n"))
}

// rewriteType inserts `insert` lines into the named type's block, rewrites every
// v_* line through `rhs`, and appends `appendix` to the model.
//
// It FAILS when the type is not found or when no verb line was rewritten. Silence
// is the failure mode being guarded: a transform that matches nothing produces a
// perfectly valid model — the canonical one — and the shape would then be measured
// as itself while wearing another shape's name.
func rewriteType(canon, typeName string, insert []string, rhs func(verb, cur string) string, appendix string) (transformResult, error) {
	lines := strings.Split(canon, "\n")
	start, end := -1, len(lines)
	for i, ln := range lines {
		m := typeLineRe.FindStringSubmatch(ln)
		if m == nil {
			continue
		}
		if m[1] == typeName {
			start = i
			continue
		}
		if start >= 0 {
			end = i
			break
		}
	}
	if start < 0 {
		return transformResult{}, fmt.Errorf("type %q not found in the canonical model", typeName)
	}

	var out []string
	var touched []string
	out = append(out, lines[:start+1]...)
	// "  relations" is the line right after "type X"; the inserts go after it so the
	// block stays well-formed whatever else the type declares.
	if start+1 >= end || strings.TrimSpace(lines[start+1]) != "relations" {
		return transformResult{}, fmt.Errorf("type %q does not open with a relations block", typeName)
	}
	out = append(out, lines[start+1])
	out = append(out, insert...)
	for _, ln := range lines[start+2 : end] {
		if m := verbLineRe.FindStringSubmatch(ln); m != nil {
			touched = append(touched, m[2])
			out = append(out, m[1]+rhs(m[2], m[3]))
			continue
		}
		out = append(out, ln)
	}
	out = append(out, lines[end:]...)

	if len(touched) == 0 {
		return transformResult{}, fmt.Errorf("type %q: no v_* relation was rewritten — the transform matched nothing", typeName)
	}
	added := len(insert)
	if appendix != "" {
		// A new type goes BEFORE the first `condition` block, never at EOF: the DSL
		// grammar puts every type declaration ahead of every condition, and appending
		// at the end is refused outright ("mismatched input 'type' expecting <EOF>").
		// Found by the transform failing loudly rather than by reading the grammar —
		// which is the point of making the transform prove it did something.
		at := len(out)
		for i, ln := range out {
			if strings.HasPrefix(ln, "condition ") {
				at = i
				break
			}
		}
		app := append([]string{}, strings.Split(appendix, "\n")...)
		app = append(app, "")
		out = append(out[:at], append(app, out[at:]...)...)
		added += len(app)
	}
	return transformResult{
		DSL:          strings.Join(out, "\n"),
		VerbsTouched: touched,
		AddedLines:   added,
	}, nil
}

// ── DSL → JSON, via the pinned CLI image ──────────────────────────────────────
//
// The same transform the deploy bootstrap uses, and the same one fgatest uses.
// Cached per process AND per DSL, because this harness needs three distinct models
// where fgatest needs one.

var (
	cliMu    sync.Mutex
	cliCache = map[string][]byte{}
)

// TransformDSL turns a DSL into the JSON WriteAuthorizationModel accepts.
func TransformDSL(dsl string) ([]byte, error) {
	cliMu.Lock()
	defer cliMu.Unlock()
	if v, ok := cliCache[dsl]; ok {
		return v, nil
	}
	image := envOr("AUTHZFORMBENCH_CLI_IMAGE", defaultCLIImage)
	args := []string{"run", "--rm", "-i", image, "model", "transform",
		dsl, "--input-format", "fga", "--output-format", "json"}
	// Fixed "docker" binary and a pinned image; the only variable argument is the
	// DSL this package produced. Tool-only, never linked into a service binary.
	cmd := exec.Command("docker", args...) // #nosec G204 -- tool-only, pinned image
	if h := os.Getenv("DOCKER_HOST"); h != "" {
		cmd.Env = append(os.Environ(), "DOCKER_HOST="+h)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("openfga/cli model transform: %w: %s", err, stderr.String())
	}
	b := stdout.Bytes()
	cliCache[dsl] = b
	return b, nil
}
