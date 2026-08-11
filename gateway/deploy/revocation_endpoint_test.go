// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// revocation_endpoint_test.go — every deployed stand must tell the gateway where
// to ask whether a token has been revoked.
//
// The gateway cannot work this address out. Introspection is served by the
// identity provider's ADMIN API, on a Service and port distinct from the public
// issuer, and reachable only inside the cluster — so a profile that leaves it
// out does not fall back to something workable, it leaves the check with nowhere
// to ask. The same holds for the admin base the logout handler uses to end the
// provider-side session.
//
// Both used to be DERIVED from the public issuer when unset, and no profile set
// either, so every stand ran with both aimed at a server that does not serve
// them. That is why the addresses are asserted here rather than assumed.
//
// This guard reads the DECLARATIONS, like its neighbour token_shape_test.go: the
// contract is what the profiles declare, it needs no chart dependencies, and it
// therefore can never skip. It merges each stack the way helm does, because the
// profiles are layered — the base carries the address and an overlay may correct
// it, so asking each FILE in isolation would demand redundant restatements and
// still miss an overlay that blanks the value.
package deploy_test

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readRepoFile reads a file addressed from the repository root. Like
// umbrellaValues it reads the declaration, not a render, so it needs nothing
// installed and cannot skip.
func readRepoFile(t *testing.T, parts ...string) string {
	t.Helper()
	path := filepath.Join(append([]string{"..", ".."}, parts...)...)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}

// deployableStacks — the `-f` chains helm is actually invoked with, in order. Kept in
// step with deploy/Makefile (dev-up / dev-prod-up), helm/umbrella/cutover-fe3455.sh and
// the identical table in deploy/tests/helm/outbox-autovacuum-naptime-test.sh.
//
// "dev" is an INTERMEDIATE chain, not a stand anybody leaves running: `dev-up` rolls it
// during the two-phase bootstrap and then upgrades onto "dev-prod", which is where the
// local stand ends up. It is listed because helm really is invoked with it — a chain
// that renders during bootstrap can still crash-loop a pod — and because every question
// asked here is one the intermediate state must also answer.
//
// values.fe3455-ory.yaml is deliberately absent — it is gitignored (Ory secrets)
// and carries no gateway configuration.
var deployableStacks = map[string][]string{
	"dev":      {"values.dev.yaml"},
	"dev-prod": {"values.dev.yaml", "values.dev-prod.yaml"},
	"prod":     {"values.prod.yaml"},
	"fe3455":   {"values.prod.yaml", "values.fe3455.yaml", "values.fe3455-prod.yaml"},
	// Три слоя, а не два. Средний (`values.dev-prod.yaml`) несёт посадку, и
	// шапка самой накладки объявляет его НЕ опциональным: она описывает только
	// образы и наследует безопасность у слоя под собой.
	//
	// Пока его здесь не было, состав читался «объявлено dev» — и обе проверки
	// транспорта административного перехода на этом стеке ПРОПУСКАЛИСЬ, потому
	// что их условие звучит «стек объявил боевую посадку». Между «объявил dev» и
	// «не объявил ничего» разница ровно та, из-за которой пропуск выглядел
	// законным: накладка образов не объявляет посадку вовсе, а набор судил её
	// так, будто она выбрала dev.
	//
	// Собранный по своей же документации, стек становится боевым и приходит под
	// проверки — как и должен, потому что именно им поднимают продукт на
	// управляемом кластере.
	"prorobotech": {"values.dev.yaml", "values.dev-prod.yaml", "values.prorobotech.yaml"},
}

// introspectionAdminPath — the path the provider's admin API serves token
// introspection on, mirrored from the gateway's own boot guard. An address
// ending anywhere else is the public API, which serves no introspection at all.
const introspectionAdminPath = "/admin/oauth2/introspect"

// mergeInto overlays src onto dst the way helm merges values files: maps merge
// key by key, anything else replaces wholesale.
func mergeInto(dst, src map[string]any) map[string]any {
	if dst == nil {
		dst = map[string]any{}
	}
	for k, v := range src {
		if sub, ok := v.(map[string]any); ok {
			if cur, ok := dst[k].(map[string]any); ok {
				dst[k] = mergeInto(cur, sub)
				continue
			}
		}
		dst[k] = v
	}
	return dst
}

// resolveStack merges a stack's profiles in order and returns the gateway value
// at the given path, or ("", false) when the stack never declares it.
func resolveStack(t *testing.T, stack []string, path ...string) (string, bool) {
	t.Helper()
	merged := map[string]any{}
	for _, profile := range stack {
		merged = mergeInto(merged, umbrellaValues(t, profile))
	}
	var cur any = merged
	for _, key := range append([]string{"api-gateway"}, path...) {
		m, ok := cur.(map[string]any)
		if !ok {
			return "", false
		}
		if cur, ok = m[key]; !ok {
			return "", false
		}
	}
	s, ok := cur.(string)
	return s, ok && strings.TrimSpace(s) != ""
}

// Every stack that deploys the gateway must name the introspection endpoint, and
// it must be the admin path — pointing it at the public API is exactly the state
// this contract exists to prevent.
func TestStacks_DeclareIntrospectionEndpoint(t *testing.T) {
	for name, stack := range deployableStacks {
		t.Run(name, func(t *testing.T) {
			got, ok := resolveStack(t, stack, "hydra", "introspectionUrl")
			if !ok {
				t.Fatalf("%s (%s): api-gateway.hydra.introspectionUrl is not declared — the "+
					"revocation check has nowhere to ask, so every token stays good until it "+
					"expires no matter what is revoked",
					name, strings.Join(stack, " + "))
			}
			if err := checkAdminEndpoint(got, introspectionAdminPath); err != nil {
				t.Errorf("%s: api-gateway.hydra.introspectionUrl %v", name, err)
			}
		})
	}
}

// And the admin base the logout handler needs to end the provider-side session.
// Unset, the session kill is skipped and signing out leaves the session alive.
func TestStacks_DeclareAdminEndpoint(t *testing.T) {
	for name, stack := range deployableStacks {
		t.Run(name, func(t *testing.T) {
			got, ok := resolveStack(t, stack, "hydra", "adminUrl")
			if !ok {
				t.Fatalf("%s (%s): api-gateway.hydra.adminUrl is not declared — signing out "+
					"then leaves the session alive at the identity provider",
					name, strings.Join(stack, " + "))
			}
			if err := checkAdminEndpoint(got, ""); err != nil {
				t.Errorf("%s: api-gateway.hydra.adminUrl %v", name, err)
			}
		})
	}
}

// The chart must still emit the environment variables these values drive. A
// value nothing renders is a decision that never reaches the process — the same
// way the sender-constrained token knob was documented for its whole life while
// no template emitted it.
func TestChart_EmitsRevocationEnv(t *testing.T) {
	deployment := readRepoFile(t, "gateway", "deploy", "templates", "deployment.yaml")
	for _, name := range []string{"KACHO_HYDRA_INTROSPECTION_URL", "KACHO_HYDRA_ADMIN_URL"} {
		if !strings.Contains(deployment, "name: "+name) {
			t.Errorf("the api-gateway template no longer emits %s — the values knob would be "+
				"silently inert and the profiles above would assert nothing", name)
		}
	}
}

// checkAdminEndpoint mirrors the gateway's boot guard: an absolute in-cluster
// http(s) URL and, for introspection, the admin path.
func checkAdminEndpoint(raw, wantPath string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("is not a valid URL: %v", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("must be an absolute http(s) URL, got %q", raw)
	}
	if u.Host == "" {
		return fmt.Errorf("has no host: %q", raw)
	}
	if wantPath != "" && strings.TrimRight(u.Path, "/") != wantPath {
		return fmt.Errorf("must address %q, got %q — the public OAuth2 API serves no "+
			"introspection, and the gateway refuses to start on an address shaped like it",
			wantPath, u.Path)
	}
	return nil
}
