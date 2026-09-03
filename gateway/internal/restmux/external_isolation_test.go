// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package restmux

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/listenerorigin"
)

// muxAddrs — full backend address map so all Internal* routes are registered.
//
// `storage`/`storageInternal` are load-bearing: mux.go registers
// InternalStorageBackendService only under `storageInternal != ""`, and that
// service is the representative of the descriptor-classified class below. Omit
// the key and the storage row 404s on the INTERNAL listener too, which turns the
// census red for the wrong reason.
func muxAddrs() map[string]string {
	return map[string]string{
		"vpc":                  "127.0.0.1:1",
		"vpcInternal":          "127.0.0.1:1",
		"compute":              "127.0.0.1:1",
		"computeInternal":      "127.0.0.1:1",
		"iam":                  "127.0.0.1:1",
		"iamInternal":          "127.0.0.1:1",
		"loadbalancer":         "127.0.0.1:1",
		"loadbalancerInternal": "127.0.0.1:1",
		"storage":              "127.0.0.1:1",
		"storageInternal":      "127.0.0.1:1",
	}
}

// internalRESTPaths — Internal* REST paths that MUST be rejected on the external
// listener (Internal-методы не публикуются на external endpoint). Mirrors the
// gRPC HasInternalSuffix block, applied to REST.
var internalRESTPaths = []struct{ method, path string }{
	{"POST", "/iam/v1/internal/users:upsertFromIdentity"},
	{"POST", "/iam/v1/internal/iam:check"},
	{"POST", "/iam/v1/internal/iam:lookupSubject"},
	// Cluster-wide permission listing is served by the public
	// PermissionCatalogService.ListPermissionCatalog
	// (GET /iam/v1/permissionCatalog), not by an internal route — so it is not
	// part of this isolation list (the public replacement is covered by the
	// public-paths test / the allowlist + route-router tests).
	{"GET", "/iam/v1/internal/cluster"},
	// InternalOperationsService.ListIamOperations: cluster-wide IAM operations
	// dump, admin-only (:9091). MUST be 404 on the external listener and
	// reachable on the internal listener.
	{"GET", "/iam/v1/internal/operations"},
	// InternalInteractiveClientService.Create (IAM-INT-1 scenario 11): registers
	// the OAuth2 client a HUMAN signs in through. Whoever reaches it decides
	// where authorization codes may be redirected, i.e. who receives a
	// credential — so it MUST be 404 on the external listener and reachable on
	// the internal one.
	//
	// WHAT ACTUALLY KEEPS IT OFF THE EXTERNAL LISTENER, measured rather than
	// assumed, because two plausible-sounding accounts of this are wrong and
	// both would send the next reader to edit the wrong place:
	//   1. the path carries an `/internal/` segment, so isInternalPath already
	//      classifies it — for EVERY method, on the path alone. The method is
	//      NOT a discriminator here; matchesInternalRESTBinding is not reached
	//      for this route at all. (Pair-keying matters for routes whose REST
	//      path is shared with a PUBLIC binding and differs only by method —
	//      this is not one of them.)
	//   2. the handler is registered under `if mux == internalMux` (mux.go),
	//      so it is never mounted on the public mux in the first place. That
	//      holds for all 15 Internal* registrations — the "registered on BOTH
	//      mux'es" comment near the top of mux.go is scoped to PUBLIC handlers.
	// The two layers are independent: removing either leaves the other. This
	// entry is load-bearing regardless — proved by injection: forcing
	// isInternalRoute to return false for this exact path turns the internal
	// half of this census red and names the pair.
	{"POST", "/iam/v1/internal/interactiveClients"},
	// InternalModuleService (kacho#1991) — Plan/Apply/Get/List над каталогом
	// прав модуля. ЧЕТЫРЕ строки, а не одна представительная: у биндингов
	// РАЗНАЯ форма пути, и классифицируются они не одним и тем же куском
	// предиката. `/iam/v1/internal/modules` попадает под `HasSuffix(path,
	// "/internal")`? — НЕТ: сегмент `/internal/` здесь СРЕДИННЫЙ, и ловит его
	// первая ветвь `Contains(path, "/internal/")`. Строка с параметром пути
	// проверяет, что подстановка значения сегмент не разрывает, а строка с
	// суффиксом-глаголом (`:plan`, `:apply`) — что двоеточие в хвосте не уводит
	// путь мимо той же ветви.
	//
	// Apply вдобавок единственный здесь POST с телом: он несёт подтверждаемый
	// отпечаток состояния и два потолка последствий, поэтому у него метод и
	// форма отличаются от трёх соседей. Метод при этом НЕ дискриминатор — путь
	// классифицируется по сегменту для любого метода; строка стоит ради формы
	// пути, а не ради метода.
	{"GET", "/iam/v1/internal/modules"},
	{"GET", "/iam/v1/internal/modules/vpc"},
	{"GET", "/iam/v1/internal/modules/vpc:plan"},
	{"POST", "/iam/v1/internal/modules/vpc:apply"},
	// InternalStorageBackendService.List — the representative of the class that
	// carries NO `/internal` segment and is matched by NONE of the hand-written
	// path rules. It is classified by the DESCRIPTOR half,
	// `matchesInternalRESTBinding`, keyed on the service NAME through the
	// generated route table. Naming the MECHANISM rather than the bucket is the
	// point: a reader who removes the hand-written rules must be able to see that
	// this row does not rest on them.
	//
	// It replaced `/vpc/v1/addressPools` when the address-pool admin surface was
	// published (ADM-1 S1). A representative has to outlive what it represents;
	// the storage backend carries the backend coordinate and a reference to its
	// credentials, so it stays internal under any continuation of that work. The
	// coexistence the publication creates is pinned separately, by
	// TestExternalListener_PublishedAdminPathReachesPublicBackendOnly below.
	{"GET", "/storage/v1/storageBackends"},
	// :internal verb-suffix (InternalNetworkService.GetNetwork REST path) — the
	// real internal projection route. isInternalPath must match this too.
	{"GET", "/vpc/v1/networks/net-1:internal"},
}

// TestExternalListener_RejectsInternalPaths — an Internal* REST path arriving on
// the EXTERNAL TLS listener must never reach the administrative backend: the
// dispatcher must not route it to the internal sub-mux regardless of the
// requested path.
//
// WHY THIS NO LONGER PINS A LITERAL 404. A 404 written here BY THIS PACKAGE was a
// second producer, and its answer (text/plain, «404 page not found», plus
// X-Content-Type-Options) differed from the one grpc-gateway gives for a route
// the listener does not have (application/json, {"code":5,…}). The shape of the
// answer therefore told an outside caller whether an administrative path lives at
// that address — the very reconnaissance the hiding exists to deny. The refusal is
// now produced by serving the request on the public mux, which carries no
// administrative route, so grpc-gateway answers exactly as it would if the admin
// surface had never been registered. What must hold is «the admin backend is not
// reached», and that is what is asserted — by ADDRESS, which no shape can fake:
// public and administrative backends are put on two distinct dead literals.
//
// One path shows why the literal was wrong. `/vpc/v1/networks/{id}:internal`
// matches the PUBLIC pattern `/vpc/v1/networks/{network_id}` with the verb
// suffix as part of the id value, so a listener without the admin surface answers
// it from the public Network read (an invalid-id refusal). Pinning 404 there
// pinned the difference between «:internal is special» and every other id.
func TestExternalListener_RejectsInternalPaths(t *testing.T) {
	addrs, adminLiteral := splitAddrs(t)
	h, err := NewMux(context.Background(), addrs, nil, nil)
	if err != nil {
		t.Fatalf("NewMux: %v", err)
	}

	// Premise: the discriminator works at all — the administrative literal DOES
	// appear on the internal listener for these very paths. Otherwise «never seen
	// outside» would be an artefact, not a property.
	seen := 0
	for _, tc := range internalRESTPaths {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		req = req.WithContext(listenerorigin.WithInternal(req.Context()))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if strings.Contains(rec.Body.String(), adminLiteral) {
			seen++
		}
	}
	if seen == 0 {
		t.Fatalf("administrative literal %q never appeared even on the INTERNAL listener — the "+
			"discriminator is dead and «never outside» would be free", adminLiteral)
	}
	t.Logf("census: administrative literal reached on the internal listener for %d of %d paths",
		seen, len(internalRESTPaths))

	for _, tc := range internalRESTPaths {

		t.Run("EXT "+tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			// External is the fail-closed DEFAULT: a request with no
			// internal-origin marker (e.g. the ingress-facing plaintext cmux
			// listener, or the external TLS listener) is treated as external.
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if strings.Contains(rec.Body.String(), adminLiteral) {
				t.Errorf("Internal* path %s %s on EXTERNAL listener reached the ADMINISTRATIVE backend "+
					"(CRITICAL: internal path exposed on external endpoint): %s",
					tc.method, tc.path, rec.Body.String())
			}
			if ct := rec.Header().Get("X-Content-Type-Options"); ct != "" {
				t.Errorf("Internal* path %s %s answered by a SECOND producer (X-Content-Type-Options=%q) — "+
					"the shape of the answer distinguishes an administrative path from one that does not "+
					"exist, which is an existence-oracle", tc.method, tc.path, ct)
			}
		})
	}
}

// TestInternalListener_ServesInternalPaths — the SAME Internal* REST paths
// arriving on the INTERNAL listener (default origin, no
// marker) must remain reachable: the route is found and dispatched to the
// internal sub-mux. The backend at 127.0.0.1:1 is unreachable → a downstream
// gRPC error (NOT a route-level 404). A bare 404 here means the route was
// rejected — which would break UI / admin-tooling / port-forward / newman.
func TestInternalListener_ServesInternalPaths(t *testing.T) {
	h, err := NewMux(context.Background(), muxAddrs(), nil, nil)
	if err != nil {
		t.Fatalf("NewMux: %v", err)
	}
	for _, tc := range internalRESTPaths {

		t.Run("INT "+tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			// Explicit internal-origin marker → dedicated cluster-internal admin
			// REST listener (the ONLY listener that serves Internal* paths).
			req = req.WithContext(listenerorigin.WithInternal(req.Context()))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code == http.StatusNotFound {
				t.Errorf("Internal* path %s %s on INTERNAL listener: got 404 — route rejected, internal callers (UI/admin/newman) broken",
					tc.method, tc.path)
			}
		})
	}
}

// TestExternalListener_PublicPathsStillServed — public REST paths on the
// external listener are unaffected (not rejected).
func TestExternalListener_PublicPathsStillServed(t *testing.T) {
	h, err := NewMux(context.Background(), muxAddrs(), nil, nil)
	if err != nil {
		t.Fatalf("NewMux: %v", err)
	}
	publicPaths := []struct{ method, path string }{
		{"GET", "/vpc/v1/networks"},
		{"GET", "/iam/v1/projects/prj-1"},
		{"GET", "/compute/v1/instances"},
		{"GET", "/nlb/v1/networkLoadBalancers"},
	}
	for _, tc := range publicPaths {

		t.Run("EXT "+tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			// External is the fail-closed default (no internal-origin marker).
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code == http.StatusNotFound {
				t.Errorf("public path %s %s on EXTERNAL listener: got 404 — public route wrongly rejected", tc.method, tc.path)
			}
		})
	}
}
