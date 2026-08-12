// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// token_shape_test.go — the deployed profile must mint tokens the api-gateway can
// actually verify.
//
// The gateway validates a bearer by SIGNATURE, against the JWKS the identity
// facade publishes. That only works on a self-contained JWT. The identity
// provider's built-in default for the access-token strategy is OPAQUE — a handle
// with no signature and no claims, which the verifier has nothing to check. So
// an access-token strategy that is merely LEFT OUT of a profile does not degrade
// gracefully: every bearer minted under it fails verification, and the profile
// authenticates nobody.
//
// (This header used to say the handle is "verifiable only by introspecting it,
// which the gateway does not do". The gateway DOES introspect — per request,
// for REVOCATION, after the signature has been verified; see
// revocation_endpoint_test.go next door. That is a different question from
// whether the token can be read at all, and it does not rescue an opaque one.)
//
// values.dev.yaml declared the strategy; values.prod.yaml did not. The profiles
// are NOT layered — values.prod.yaml renders standalone and never on top of the
// dev file — so nothing in the dev profile ever reached production, and the
// canonical production profile inherited the opaque default.
//
// This guard reads the declarations rather than a rendered template on purpose:
// the contract is what the profile DECLARES, it needs no chart dependencies, and
// it can therefore never skip. The neighbouring render-guards cover template
// logic; this one covers a value whose absence is silent.
//
// Related: the production-posture workflow proves every service BOOTS under this
// profile, and says in its own header that it does not prove tokens work. This
// is the gap it names.
package deploy_test

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"gopkg.in/yaml.v3"
)

// umbrellaValues loads one umbrella profile as a generic tree.
func umbrellaValues(t *testing.T, profile string) map[string]any {
	t.Helper()
	path := filepath.Join("..", "..", "deploy", "helm", "umbrella", profile)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var tree map[string]any
	if err := yaml.Unmarshal(raw, &tree); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return tree
}

// baseProfiles — профили, стоящие ПЕРВЫМИ в цепочках стендов, без повторов и в
// устойчивом порядке. Выводятся из таблицы стеков, а не выписываются: здесь
// стояли два имени, и профиль, ставший базой нового стенда, приходил бы под
// проверку только правкой этого файла.
//
// Вопрос этого файла — «объявляет ли ПРОФИЛЬ своё», а не «что получает релиз»
// (второй задаёт login_console_test.go складыванием цепочки). Поэтому единицей
// здесь остаётся отдельный файл, и берётся именно база: накладка, молчащая о
// ключе, ничего не меняет, а база — единственное место, где значение обязано
// быть объявлено.
func baseProfiles(t *testing.T) []string {
	t.Helper()
	seen := map[string]bool{}
	var out []string
	for _, chain := range deployableStacks(t) {
		if len(chain) == 0 || seen[chain[0]] {
			continue
		}
		seen[chain[0]] = true
		out = append(out, chain[0])
	}
	sort.Strings(out)
	if len(out) == 0 {
		t.Fatal("ни одна цепочка стенда не назвала базового профиля — проверять нечего, " +
			"и это отказ, а не пустой успех")
	}
	return out
}

// dig walks a path of map keys, reporting the first missing one.
func dig(t *testing.T, tree map[string]any, profile string, path ...string) any {
	t.Helper()
	var cur any = tree
	for i, key := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			t.Fatalf("%s: %v is not a mapping, cannot read %q", profile, path[:i], key)
		}
		cur, ok = m[key]
		if !ok {
			t.Fatalf("%s: %v is not declared", profile, path[:i+1])
		}
	}
	return cur
}

// tokenStrategyPath — where the access-token strategy lives in the umbrella
// values tree (the identity provider ships as a nested subchart, hence the
// repeated segment).
var tokenStrategyPath = []string{"hydra", "hydra", "config", "strategies", "access_token"}

// Every deployable profile must declare a self-contained access token. Omitting
// the key is not a neutral default — it selects the opaque handle the gateway
// cannot verify at all.
func TestProfiles_DeclareVerifiableAccessToken(t *testing.T) {
	for _, profile := range baseProfiles(t) {
		got := dig(t, umbrellaValues(t, profile), profile, tokenStrategyPath...)
		if got != "jwt" {
			t.Errorf("%s: access-token strategy is %q, want \"jwt\" — the gateway verifies by "+
				"signature against the published JWKS, and anything else is unverifiable to it",
				profile, got)
		}
	}
}

// The scope claim shape is part of the same contract: the gateway reads scopes as
// a list. A profile that mints JWTs with the provider's default string encoding
// verifies fine and then authorizes wrongly, which is worse than failing shut.
func TestProfiles_DeclareListScopeClaim(t *testing.T) {
	for _, profile := range baseProfiles(t) {
		got := dig(t, umbrellaValues(t, profile), profile,
			"hydra", "hydra", "config", "strategies", "jwt", "scope_claim")
		if got != "list" {
			t.Errorf("%s: jwt scope_claim is %q, want \"list\"", profile, got)
		}
	}
}

// The two profiles disagree on token lifetime ON PURPOSE — production is short,
// the local stand is widened because serial e2e waves outrun it. That is the only
// sanctioned disagreement in this block, so it is pinned: production must stay at
// the documented lifetime, and the dev widening must stay visibly a dev artifact.
func TestProfiles_AccessTokenLifetime(t *testing.T) {
	if got := dig(t, umbrellaValues(t, "values.prod.yaml"), "values.prod.yaml",
		"hydra", "hydra", "config", "ttl", "access_token"); got != "15m" {
		t.Errorf("values.prod.yaml: access-token lifetime is %q, want \"15m\" (documented "+
			"production posture; absence silently inherits the provider default of 1h)", got)
	}
	if got := dig(t, umbrellaValues(t, "values.dev.yaml"), "values.dev.yaml",
		"hydra", "hydra", "config", "ttl", "access_token"); got == "15m" {
		t.Errorf("values.dev.yaml: the local stand deliberately widens the lifetime past 15m " +
			"so serial newman waves do not expire mid-run; if that is no longer needed, " +
			"remove the widening and this guard together")
	}
}
