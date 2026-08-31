// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// symmetric_secret_absent_test.go — no deployment profile may put the gateway on a
// symmetric (shared-key) bearer posture.
//
// A shared signing key means every holder of the key is also an issuer: whoever can
// read the profile can mint a bearer the edge accepts, and the edge has no way to tell
// that token from one the identity provider signed. Asymmetric verification does not
// have that property — the edge holds only public material. The norm is
// `security.md` §"Production-mode — ОБЯЗАТЕЛЕН ВЕЗДЕ": a deployed stand runs the
// production posture, and the symmetric stand-in is allowed only inside in-process
// fixtures, which never read these files.
//
// WHY THIS READS DECLARATIONS, NOT A RENDER. Same reason as its neighbours
// token_shape_test.go and revocation_endpoint_test.go: the contract is what a profile
// DECLARES, the check then needs no chart tooling, and it therefore can never skip.
// A guard that can skip is the one that will skip on the day it matters.
//
// WHY BY STACK AND NOT BY FILE. Profiles are layered: a file that declares nothing
// inherits whatever the base underneath it declared. Asking each file in isolation
// would read an overlay as safe while the stand it produces is not. Both questions are
// asked here — merged stacks (what actually gets deployed) and every file on its own
// (so a profile that is not in a stack yet cannot introduce the declaration quietly).
package deploy_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// symmetricSecretKeys — the value paths under `api-gateway.authn` by which a profile
// could hand the edge a shared signing key. Both are named because the chart used to
// accept either, and a guard that knew only one of them would have read the other as
// clean.
var symmetricSecretKeys = []string{"devSecret", "devSecretSecretRef"}

// symmetricSecretEnv — the same posture expressed through the chart's generic
// environment passthrough. The typed knobs above are gone from the chart, so this is
// the remaining way a profile could reach the process, and it is exactly the way an
// author would reach for once the typed one is missing.
const symmetricSecretEnv = "KACHO_API_GATEWAY_AUTHN_DEV_SECRET"

// acceptedAuthnModes — the postures under which the edge requires a signature it can
// verify against published public material. Anything else (notably the relaxed mode)
// is a posture where an unauthenticated request is answered as somebody.
var acceptedAuthnModes = map[string]bool{"production": true, "production-strict": true}

// gatewayValues digs `api-gateway` out of a merged tree.
func gatewayValues(merged map[string]any) map[string]any {
	gw, _ := merged["api-gateway"].(map[string]any)
	return gw
}

// mergedStack merges a stack's profiles in helm order.
func mergedStack(t *testing.T, stack []string) map[string]any {
	t.Helper()
	merged := map[string]any{}
	for _, profile := range stack {
		merged = mergeInto(merged, umbrellaValues(t, profile))
	}
	return merged
}

// findSymmetricSecret reports every way the given merged tree hands the gateway a
// shared signing key. It returns the value paths found, so a failure names a
// coordinate the reader can open rather than only asserting that something is wrong.
//
// This is THE predicate of this file. It is exercised in both directions by
// TestPredicate_FindsAndIgnores below: a gate whose subject is an ABSENCE stays silent
// when the subject is gone AND when the predicate itself broke, and those two must not
// look the same.
func findSymmetricSecret(merged map[string]any) []string {
	var found []string
	gw := gatewayValues(merged)
	if gw == nil {
		return nil
	}
	if authn, ok := gw["authn"].(map[string]any); ok {
		for _, key := range symmetricSecretKeys {
			v, present := authn[key]
			if !present || v == nil {
				continue
			}
			// An explicitly-empty declaration is not a symmetric posture, but it is a
			// declaration of a knob the chart no longer has — reported so it cannot
			// linger as dead configuration that looks meaningful.
			if s, isStr := v.(string); isStr && s == "" {
				found = append(found, "api-gateway.authn."+key+" (declared empty — the chart has no such knob)")
				continue
			}
			found = append(found, "api-gateway.authn."+key)
		}
	}
	if extra, ok := gw["extraEnv"].(map[string]any); ok {
		if v, present := extra[symmetricSecretEnv]; present && v != nil {
			found = append(found, "api-gateway.extraEnv."+symmetricSecretEnv)
		}
	}
	return found
}

// ── the gate's own premise ───────────────────────────────────────────────────
//
// Everything below reads files named by deployableStacks. If one of those names stops
// resolving, every question asked of it answers "nothing declared" — which reads
// exactly like "clean". So the names are checked first, and the number of files
// examined is stated, because "0 findings" must be distinguishable from "0 files read".

func TestPremise_EveryStackProfileExists(t *testing.T) {
	if len(deployableStacks(t)) == 0 {
		t.Fatal("deployableStacks is empty — this file would examine nothing and report success")
	}
	seen := map[string]bool{}
	for name, stack := range deployableStacks(t) {
		if len(stack) == 0 {
			t.Errorf("stack %q names no profile — it cannot be checked", name)
		}
		for _, profile := range stack {
			path := filepath.Join("..", "..", "deploy", "helm", "umbrella", profile)
			if _, err := os.Stat(path); err != nil {
				t.Errorf("stack %q names %s, which does not resolve (%v) — every question "+
					"asked of it would answer \"nothing declared\"", name, profile, err)
			}
			seen[profile] = true
		}
	}
	t.Logf("premise: %d stacks naming %d distinct profiles, all resolved", len(deployableStacks(t)), len(seen))
}

// ── the positive half ────────────────────────────────────────────────────────
//
// Paired with the absence-assertions below on purpose. "No symmetric secret declared"
// is also true of a stack that declares no gateway at all, or of a merge that silently
// produced an empty tree. Requiring an explicit, accepted posture makes that state
// visible instead of green.

func TestStacks_DeclareAnAsymmetricAuthnPosture(t *testing.T) {
	for name, stack := range deployableStacks(t) {
		t.Run(name, func(t *testing.T) {
			merged := mergedStack(t, stack)
			gw := gatewayValues(merged)
			if gw == nil {
				t.Fatalf("%s (%s): no api-gateway values at all — an absence-check over this "+
					"stack would pass for the wrong reason", name, strings.Join(stack, " + "))
			}
			// Посадка адресуется каноном `authMode` в корне значений сервиса.
			// Прежний адрес (`authn.mode`) читается следом и только потому, что
			// шаблон его тоже пока принимает: стек, оставшийся на нём, обязан
			// проверяться, а не молча выпадать из проверки.
			mode, _ := gw["authMode"].(string)
			if mode == "" {
				if authn, ok := gw["authn"].(map[string]any); ok {
					mode, _ = authn["mode"].(string)
				}
			}
			if mode == "" {
				t.Fatalf("%s (%s): api-gateway.authMode is not declared — the posture is then "+
					"whatever a chart default happens to be, which is the state this file exists "+
					"to make impossible to reach by omission", name, strings.Join(stack, " + "))
			}
			if !acceptedAuthnModes[mode] {
				t.Errorf("%s (%s): api-gateway.authMode=%q — a deployed stand must require a "+
					"signature it can verify against published public material", name,
					strings.Join(stack, " + "), mode)
			}
		})
	}
}

// ── the absence half ─────────────────────────────────────────────────────────

func TestStacks_DeclareNoSymmetricSecret(t *testing.T) {
	for name, stack := range deployableStacks(t) {
		t.Run(name, func(t *testing.T) {
			found := findSymmetricSecret(mergedStack(t, stack))
			if len(found) > 0 {
				t.Errorf("%s (%s) hands the gateway a shared signing key: %s. A shared key makes "+
					"every holder of the profile an issuer, and the edge cannot tell such a token "+
					"from a provider-signed one",
					name, strings.Join(stack, " + "), strings.Join(found, ", "))
			}
		})
	}
}

// Per FILE as well as per stack: a profile that no stack names yet is still a profile
// somebody will deploy, and the stack table is maintained by hand.
func TestEveryUmbrellaProfile_DeclaresNoSymmetricSecret(t *testing.T) {
	dir := filepath.Join("..", "..", "deploy", "helm", "umbrella")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	examined := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "values") || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		examined++
		tree := umbrellaValues(t, e.Name())
		if found := findSymmetricSecret(tree); len(found) > 0 {
			t.Errorf("%s declares %s", e.Name(), strings.Join(found, ", "))
		}
	}
	if examined == 0 {
		t.Fatalf("examined 0 profiles under %s — nothing was read, which is not the same as "+
			"nothing being wrong", dir)
	}
	t.Logf("examined %d umbrella value profiles", examined)
}

// ── the predicate under control, both ways ───────────────────────────────────
//
// Without this, a typo in findSymmetricSecret would make every assertion above green
// and indistinguishable from a clean tree — the failure mode a check whose subject is
// an absence has by construction.
func TestPredicate_FindsAndIgnores(t *testing.T) {
	mustFind := map[string]string{
		"inline value": `
api-gateway:
  authn:
    mode: production-strict
    devSecret: some-shared-string
`,
		"secret reference": `
api-gateway:
  authn:
    mode: production-strict
    devSecretSecretRef:
      name: whatever
      key: k
`,
		"generic env passthrough": `
api-gateway:
  authn:
    mode: production-strict
  extraEnv:
    KACHO_API_GATEWAY_AUTHN_DEV_SECRET: some-shared-string
`,
		"declared empty — a knob the chart no longer has": `
api-gateway:
  authn:
    mode: production-strict
    devSecret: ""
`,
	}
	for name, doc := range mustFind {
		t.Run("finds/"+name, func(t *testing.T) {
			var tree map[string]any
			if err := yaml.Unmarshal([]byte(doc), &tree); err != nil {
				t.Fatalf("fixture does not parse: %v", err)
			}
			if found := findSymmetricSecret(tree); len(found) == 0 {
				t.Fatalf("predicate found nothing in a tree that declares one — every silence " +
					"it produces elsewhere is then meaningless")
			}
		})
	}

	// The legitimate twin: the shape this file wants everywhere. If the predicate flags
	// this, the first honest profile turns the gate off.
	mustIgnore := map[string]string{
		"asymmetric posture, nothing else": `
api-gateway:
  authn:
    mode: production-strict
    enforceStepUp: true
`,
		"asymmetric posture with unrelated env": `
api-gateway:
  authn:
    mode: production
  extraEnv:
    KACHO_APP_ENV: production
`,
	}
	for name, doc := range mustIgnore {
		t.Run("ignores/"+name, func(t *testing.T) {
			var tree map[string]any
			if err := yaml.Unmarshal([]byte(doc), &tree); err != nil {
				t.Fatalf("fixture does not parse: %v", err)
			}
			if found := findSymmetricSecret(tree); len(found) > 0 {
				t.Fatalf("predicate flagged a legitimate profile: %s", strings.Join(found, ", "))
			}
		})
	}
}

// ── the chart must not offer the knob at all ─────────────────────────────────
//
// Removing the value from the profiles leaves the way back in one line long. The chart
// template is the producer of the process environment, so the question worth asking is
// whether the template can emit the variable at all.
//
// PREMISE, stated because it is what makes the check meaningful: this reads the
// template source with comment lines removed. It is not a parse — a Helm template is
// not YAML until it is rendered. The positive control below is what keeps that
// honest: the same predicate must still find a variable the template does emit,
// otherwise "found nothing" would mean "read nothing".
func TestChartTemplate_CannotEmitSymmetricSecret(t *testing.T) {
	raw := readRepoFile(t, "gateway", "deploy", "templates", "deployment.yaml")

	var executable []string
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		executable = append(executable, line)
	}
	body := strings.Join(executable, "\n")

	// Positive control: a variable this template certainly emits. If this stops being
	// found, the predicate or the path is broken and the absence-check below means
	// nothing.
	const control = "KACHO_API_GATEWAY_AUTHN_MODE"
	if !strings.Contains(body, control) {
		t.Fatalf("control %q not found in the template body — the file was not read as expected, "+
			"so its silence about anything else proves nothing", control)
	}

	if strings.Contains(body, symmetricSecretEnv) {
		t.Errorf("the gateway chart template can still emit %s. While the template offers it, a "+
			"profile can put a deployed stand on a shared signing key with one line", symmetricSecretEnv)
	}
	t.Logf("examined %d executable lines of the gateway deployment template", len(executable))
}
