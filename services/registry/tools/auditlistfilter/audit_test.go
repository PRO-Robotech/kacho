// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// audit_test.go locks the DETECTION LOGIC of the listauthz gate against synthetic
// trees, plus the gate's verdict on the real kacho-registry tree.
//
// A gate is only worth having if it goes red on the shape it claims to catch, so
// every rule below is paired with a fixture that violates exactly that rule and
// nothing else. The fixtures are built from one compliant baseline with a single
// method (or file) swapped out — a fixture that breaks two things at once cannot
// tell you which rule fired.
package auditlistfilter

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// fixture construction
// ---------------------------------------------------------------------------

const handlerHeader = `package handler

type RegistryHandler struct {
	uc    *registry.UseCase
	authz repoAuthz
}
`

// Compliant handler methods — the five List-shaped RPCs of RegistryService, in the
// two enforcement shapes the service actually uses: a per-object ROW FILTER for a
// page of separately-authorizable objects, and a single-object GATE for a page that
// lives inside one already-authorizable object.
const (
	listCompliant = `
func (h *RegistryHandler) List(ctx context.Context, req *registryv1.ListRegistriesRequest) (*registryv1.ListRegistriesResponse, error) {
	items, next, err := h.uc.List(ctx, registry.ListQuery{ProjectID: req.GetProjectId()})
	if err != nil {
		return nil, mapErr(err)
	}
	filtered, err := h.authz.filterRegistries(ctx, items)
	if err != nil {
		return nil, err
	}
	resp := &registryv1.ListRegistriesResponse{NextPageToken: next}
	for _, r := range filtered {
		resp.Registries = append(resp.Registries, h.uc.ProtoRegistry(r))
	}
	return resp, nil
}
`
	listRepositoriesCompliant = `
func (h *RegistryHandler) ListRepositories(ctx context.Context, req *registryv1.ListRepositoriesRequest) (*registryv1.ListRepositoriesResponse, error) {
	registryID := req.GetRegistryId()
	if err := h.authz.namespaceGate(ctx, registryID); err != nil {
		return nil, err
	}
	window, next, err := h.uc.ListRepositories(ctx, registry.RepoListQuery{RegistryID: registryID})
	if err != nil {
		return nil, mapErr(err)
	}
	filtered, err := h.authz.filterRepos(ctx, registryID, window)
	if err != nil {
		return nil, err
	}
	resp := &registryv1.ListRepositoriesResponse{NextPageToken: next}
	for _, r := range filtered {
		resp.Repositories = append(resp.Repositories, toProtoRepository(r))
	}
	return resp, nil
}
`
	listOperationsCompliant = `
func (h *RegistryHandler) ListOperations(ctx context.Context, req *registryv1.ListRegistryOperationsRequest) (*registryv1.ListRegistryOperationsResponse, error) {
	registryID := req.GetRegistryId()
	ops, next, err := h.uc.ListOperations(ctx, registry.ListOperationsQuery{RegistryID: registryID})
	if err != nil {
		return nil, mapErr(err)
	}
	filtered, err := h.authz.filterOperations(ctx, registryID, ops)
	if err != nil {
		return nil, err
	}
	resp := &registryv1.ListRegistryOperationsResponse{NextPageToken: next}
	for i := range filtered {
		resp.Operations = append(resp.Operations, operationToProto(&filtered[i]))
	}
	return resp, nil
}
`
	listTagsCompliant = `
func (h *RegistryHandler) ListTags(ctx context.Context, req *registryv1.ListTagsRequest) (*registryv1.ListTagsResponse, error) {
	registryID, repository := req.GetRegistryId(), req.GetRepository()
	if err := h.authz.checkRepo(ctx, registryID, repository, relationVList); err != nil {
		return nil, err
	}
	page, next, err := h.uc.ListTags(ctx, registry.TagListQuery{RegistryID: registryID})
	if err != nil {
		return nil, mapErr(err)
	}
	resp := &registryv1.ListTagsResponse{NextPageToken: next}
	for _, t := range page {
		resp.Tags = append(resp.Tags, toProtoTag(t))
	}
	return resp, nil
}
`
	listReferrersCompliant = `
func (h *RegistryHandler) ListReferrers(ctx context.Context, req *registryv1.ListReferrersRequest) (*registryv1.ListReferrersResponse, error) {
	registryID, repository := req.GetRegistryId(), req.GetRepository()
	if err := h.authz.checkRepository(ctx, registryID, repository, relationVGet); err != nil {
		return nil, err
	}
	referrers, err := h.uc.ListReferrers(ctx, registry.ReferrersQuery{RegistryID: registryID})
	if err != nil {
		return nil, mapErr(err)
	}
	resp := &registryv1.ListReferrersResponse{}
	for _, r := range referrers {
		resp.Referrers = append(resp.Referrers, toProtoReferrer(r))
	}
	return resp, nil
}
`
)

// Compliant repoAuthz: the helpers the handler leans on. Every filter returns
// (nil, err) on an authorization failure — never a half-filtered page.
const listauthzCompliant = `package handler

type repoAuthz struct{ az Authorizer }

func (a repoAuthz) namespaceGate(ctx context.Context, registryID string) error {
	allowed, err := a.check(ctx, relationVList, registryObjectRef(registryID))
	if err != nil {
		return errAuthzUnavailable()
	}
	if !allowed {
		return errHideExistence()
	}
	return nil
}

func (a repoAuthz) checkRepo(ctx context.Context, registryID, repository, relation string) error {
	allowed, err := a.check(ctx, relation, repositoryObjectRef(registryID, repository))
	if err != nil {
		return errAuthzUnavailable()
	}
	if !allowed {
		return errHideExistence()
	}
	return nil
}

func (a repoAuthz) checkRepository(ctx context.Context, registryID, repository, relation string) error {
	allowed, err := a.check(ctx, relation, repositoryObjectRef(registryID, repository))
	if err != nil {
		return errAuthzUnavailable()
	}
	if !allowed {
		return errRepoHideExistence()
	}
	return nil
}

func (a repoAuthz) filterRegistries(ctx context.Context, regs []*domain.Registry) ([]*domain.Registry, error) {
	if a.az == nil {
		return regs, nil
	}
	out, err := a.keep(ctx, regs)
	if err != nil {
		return nil, errAuthzUnavailable()
	}
	return out, nil
}

func (a repoAuthz) filterRepos(ctx context.Context, registryID string, repos []*domain.Repository) ([]*domain.Repository, error) {
	if a.az == nil {
		return repos, nil
	}
	out, err := a.keep(ctx, repos)
	if err != nil {
		return nil, errAuthzUnavailable()
	}
	return out, nil
}

func (a repoAuthz) filterOperations(ctx context.Context, registryID string, ops []operations.Operation) ([]operations.Operation, error) {
	if a.az == nil {
		return ops, nil
	}
	out, err := a.keep(ctx, ops)
	if err != nil {
		return nil, errAuthzUnavailable()
	}
	return out, nil
}
`

const serveCompliant = `package main

func run() error {
	var listAuthz handler.Authorizer
	if authzConn != nil {
		listAuthz = check.NewIAMCheckClient(authzConn)
	}
	registryv1.RegisterRegistryServiceServer(grpcSrv, handler.NewRegistryHandler(registryUC, listAuthz))
	return nil
}
`

// methodNames fixes the order in which the compliant methods are concatenated, so a
// fixture's byte layout does not depend on map iteration.
var methodNames = []string{"List", "ListRepositories", "ListOperations", "ListTags", "ListReferrers"}

func compliantMethods() map[string]string {
	return map[string]string{
		"List":             listCompliant,
		"ListRepositories": listRepositoriesCompliant,
		"ListOperations":   listOperationsCompliant,
		"ListTags":         listTagsCompliant,
		"ListReferrers":    listReferrersCompliant,
	}
}

// tree materialises a service tree: the compliant baseline with `methods` swapped in
// (an empty replacement DELETES that handler method), plus optional whole-file
// overrides for listauthz.go and cmd/kacho-registry/serve.go.
type tree struct {
	methods   map[string]string // method name → replacement source ("" deletes it)
	extra     string            // appended verbatim to internal/handler/public.go
	listauthz string            // "" ⇒ compliant baseline
	serve     string            // "" ⇒ compliant baseline
}

func (tr tree) write(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	methods := compliantMethods()
	for name, src := range tr.methods {
		methods[name] = src
	}
	var public strings.Builder
	public.WriteString(handlerHeader)
	for _, name := range methodNames {
		public.WriteString(methods[name])
	}
	public.WriteString(tr.extra)

	listauthz := tr.listauthz
	if listauthz == "" {
		listauthz = listauthzCompliant
	}
	serve := tr.serve
	if serve == "" {
		serve = serveCompliant
	}

	for rel, content := range map[string]string{
		"internal/handler/public.go":    public.String(),
		"internal/handler/listauthz.go": listauthz,
		"cmd/kacho-registry/serve.go":   serve,
	} {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	return root
}

// ---------------------------------------------------------------------------
// detection logic
// ---------------------------------------------------------------------------

func TestAnalyze(t *testing.T) {
	cases := []struct {
		name string
		tr   tree
		// want is a substring that must appear in some finding; "" ⇒ expect a clean
		// tree (no findings at all).
		want string
	}{
		{
			name: "compliant: every List enforces per-object visibility",
			tr:   tree{},
			want: "",
		},
		{
			// The shape storage shipped with: rows are read and returned, and nothing
			// ever asks whether the caller may see THESE objects.
			name: "leak: List returns the page without filtering it per object",
			tr: tree{methods: map[string]string{"List": `
func (h *RegistryHandler) List(ctx context.Context, req *registryv1.ListRegistriesRequest) (*registryv1.ListRegistriesResponse, error) {
	items, next, err := h.uc.List(ctx, registry.ListQuery{ProjectID: req.GetProjectId()})
	if err != nil {
		return nil, mapErr(err)
	}
	resp := &registryv1.ListRegistriesResponse{NextPageToken: next}
	for _, r := range items {
		resp.Registries = append(resp.Registries, h.uc.ProtoRegistry(r))
	}
	return resp, nil
}
`}},
			want: "filterRegistries",
		},
		{
			// Subtler and more dangerous: the filter IS called, so a grep-shaped gate
			// is satisfied, but the response is built from the unfiltered slice.
			name: "leak: filter is called and its result is dropped on the floor",
			tr: tree{methods: map[string]string{"List": `
func (h *RegistryHandler) List(ctx context.Context, req *registryv1.ListRegistriesRequest) (*registryv1.ListRegistriesResponse, error) {
	items, next, err := h.uc.List(ctx, registry.ListQuery{ProjectID: req.GetProjectId()})
	if err != nil {
		return nil, mapErr(err)
	}
	filtered, err := h.authz.filterRegistries(ctx, items)
	if err != nil {
		return nil, err
	}
	_ = filtered
	resp := &registryv1.ListRegistriesResponse{NextPageToken: next}
	for _, r := range items {
		resp.Registries = append(resp.Registries, h.uc.ProtoRegistry(r))
	}
	return resp, nil
}
`}},
			want: "unfiltered",
		},
		{
			// Prose is not code: a comment naming the filter must not satisfy the gate.
			name: "leak: the filter is only named in a comment",
			tr: tree{methods: map[string]string{"List": `
func (h *RegistryHandler) List(ctx context.Context, req *registryv1.ListRegistriesRequest) (*registryv1.ListRegistriesResponse, error) {
	items, next, err := h.uc.List(ctx, registry.ListQuery{ProjectID: req.GetProjectId()})
	if err != nil {
		return nil, mapErr(err)
	}
	// Visibility is narrowed per object by h.authz.filterRegistries below.
	resp := &registryv1.ListRegistriesResponse{NextPageToken: next}
	for _, r := range items {
		resp.Registries = append(resp.Registries, h.uc.ProtoRegistry(r))
	}
	return resp, nil
}
`}},
			want: "filterRegistries",
		},
		{
			// …and the converse: documenting WHY enumeration is banned must not fail a
			// List that does the right thing.
			name: "compliant: a comment may explain why enumeration is banned",
			tr: tree{methods: map[string]string{"List": `
func (h *RegistryHandler) List(ctx context.Context, req *registryv1.ListRegistriesRequest) (*registryv1.ListRegistriesResponse, error) {
	items, next, err := h.uc.List(ctx, registry.ListQuery{ProjectID: req.GetProjectId()})
	if err != nil {
		return nil, mapErr(err)
	}
	// Never ListAllowedIDs/ListObjects here: that enumeration is capped server-side.
	filtered, err := h.authz.filterRegistries(ctx, items)
	if err != nil {
		return nil, err
	}
	resp := &registryv1.ListRegistriesResponse{NextPageToken: next}
	for _, r := range filtered {
		resp.Registries = append(resp.Registries, h.uc.ProtoRegistry(r))
	}
	return resp, nil
}
`}},
			want: "",
		},
		{
			// "Enumerate everything the subject may see, then narrow to it" is capped
			// server-side with no continuation token, so a tenant's own repository
			// silently falls outside the prefix and becomes invisible.
			name: "reject: enumerate-all-allowed-ids instead of checking the page",
			tr: tree{methods: map[string]string{"ListRepositories": `
func (h *RegistryHandler) ListRepositories(ctx context.Context, req *registryv1.ListRepositoriesRequest) (*registryv1.ListRepositoriesResponse, error) {
	registryID := req.GetRegistryId()
	allowed, err := h.authz.ListAllowedIDs(ctx, registryID)
	if err != nil {
		return nil, err
	}
	window, next, err := h.uc.ListRepositoriesByIDs(ctx, allowed)
	if err != nil {
		return nil, mapErr(err)
	}
	filtered, err := h.authz.filterRepos(ctx, registryID, window)
	if err != nil {
		return nil, err
	}
	resp := &registryv1.ListRepositoriesResponse{NextPageToken: next}
	for _, r := range filtered {
		resp.Repositories = append(resp.Repositories, toProtoRepository(r))
	}
	return resp, nil
}
`}},
			want: "ListAllowedIDs",
		},
		{
			name: "leak: ListTags loses its per-repository gate",
			tr: tree{methods: map[string]string{"ListTags": `
func (h *RegistryHandler) ListTags(ctx context.Context, req *registryv1.ListTagsRequest) (*registryv1.ListTagsResponse, error) {
	registryID := req.GetRegistryId()
	page, next, err := h.uc.ListTags(ctx, registry.TagListQuery{RegistryID: registryID})
	if err != nil {
		return nil, mapErr(err)
	}
	resp := &registryv1.ListTagsResponse{NextPageToken: next}
	for _, t := range page {
		resp.Tags = append(resp.Tags, toProtoTag(t))
	}
	return resp, nil
}
`}},
			want: "checkRepo",
		},
		{
			// The gate runs but its verdict is thrown away — the most literal form of
			// "the check has a shape and no substance".
			name: "leak: the gate is called and its verdict is discarded",
			tr: tree{methods: map[string]string{"ListReferrers": `
func (h *RegistryHandler) ListReferrers(ctx context.Context, req *registryv1.ListReferrersRequest) (*registryv1.ListReferrersResponse, error) {
	registryID, repository := req.GetRegistryId(), req.GetRepository()
	_ = h.authz.checkRepository(ctx, registryID, repository, relationVGet)
	referrers, err := h.uc.ListReferrers(ctx, registry.ReferrersQuery{RegistryID: registryID})
	if err != nil {
		return nil, mapErr(err)
	}
	resp := &registryv1.ListReferrersResponse{}
	for _, r := range referrers {
		resp.Referrers = append(resp.Referrers, toProtoReferrer(r))
	}
	return resp, nil
}
`}},
			want: "verdict",
		},
		{
			// Gating after the read means an unauthorized caller still moves the data
			// plane, and the deny is decided on data it was never allowed to touch.
			name: "leak: the gate runs after the use-case has already read the page",
			tr: tree{methods: map[string]string{"ListTags": `
func (h *RegistryHandler) ListTags(ctx context.Context, req *registryv1.ListTagsRequest) (*registryv1.ListTagsResponse, error) {
	registryID, repository := req.GetRegistryId(), req.GetRepository()
	page, next, err := h.uc.ListTags(ctx, registry.TagListQuery{RegistryID: registryID})
	if err != nil {
		return nil, mapErr(err)
	}
	if err := h.authz.checkRepo(ctx, registryID, repository, relationVList); err != nil {
		return nil, err
	}
	resp := &registryv1.ListTagsResponse{NextPageToken: next}
	for _, t := range page {
		resp.Tags = append(resp.Tags, toProtoTag(t))
	}
	return resp, nil
}
`}},
			want: "before",
		},
		{
			// A new List RPC must not be able to ship unnoticed: the gate has no
			// opinion about it yet, and no opinion means fail closed.
			name: "fail-closed: a List RPC nobody declared enforcement for",
			tr: tree{extra: `
func (h *RegistryHandler) ListWebhooks(ctx context.Context, req *registryv1.ListWebhooksRequest) (*registryv1.ListWebhooksResponse, error) {
	hooks, next, err := h.uc.ListWebhooks(ctx, req.GetRegistryId())
	if err != nil {
		return nil, mapErr(err)
	}
	resp := &registryv1.ListWebhooksResponse{NextPageToken: next}
	for _, w := range hooks {
		resp.Webhooks = append(resp.Webhooks, toProtoWebhook(w))
	}
	return resp, nil
}
`},
			want: "ListWebhooks",
		},
		{
			// …and the mirror: a table entry for a handler that no longer exists is a
			// rule that silently stopped applying.
			name: "fail-closed: enforcement declared for a handler that is gone",
			tr:   tree{methods: map[string]string{"ListOperations": ""}},
			want: "ListOperations",
		},
		{
			// Returning rows alongside an authorization error hands out exactly the
			// page the check could not vouch for.
			name: "leak: a filter returns its rows together with an error",
			tr: tree{listauthz: strings.Replace(listauthzCompliant,
				`func (a repoAuthz) filterRepos(ctx context.Context, registryID string, repos []*domain.Repository) ([]*domain.Repository, error) {
	if a.az == nil {
		return repos, nil
	}
	out, err := a.keep(ctx, repos)
	if err != nil {
		return nil, errAuthzUnavailable()
	}
	return out, nil
}`,
				`func (a repoAuthz) filterRepos(ctx context.Context, registryID string, repos []*domain.Repository) ([]*domain.Repository, error) {
	if a.az == nil {
		return repos, nil
	}
	out, err := a.keep(ctx, repos)
	if err != nil {
		return repos, errAuthzUnavailable()
	}
	return out, nil
}`, 1)},
			want: "filterRepos",
		},
		{
			// Every filter above is a no-op when the handler is built without an
			// authorizer, so the wiring is part of the invariant, not background.
			name: "leak: the composition root wires a nil authorizer",
			tr: tree{serve: `package main

func run() error {
	registryv1.RegisterRegistryServiceServer(grpcSrv, handler.NewRegistryHandler(registryUC, nil))
	return nil
}
`},
			want: "nil",
		},
		{
			name: "fail-closed: the composition root never builds the handler",
			tr: tree{serve: `package main

func run() error {
	return nil
}
`},
			want: "NewRegistryHandler",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := tc.tr.write(t)
			findings, err := Analyze(root)
			if err != nil {
				t.Fatalf("Analyze: %v", err)
			}
			if tc.want == "" {
				if len(findings) != 0 {
					t.Fatalf("expected a clean tree, got findings:\n%s", strings.Join(findings, "\n"))
				}
				return
			}
			if len(findings) == 0 {
				t.Fatalf("expected a finding mentioning %q, gate reported nothing", tc.want)
			}
			for _, f := range findings {
				if strings.Contains(f, tc.want) {
					return
				}
			}
			t.Fatalf("no finding mentions %q; got:\n%s", tc.want, strings.Join(findings, "\n"))
		})
	}
}

// serviceRoot returns …/services/registry, two levels above this test file.
func serviceRoot(t *testing.T) string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(self)))
}

// TestAnalyzeRealTree runs the gate against the real kacho-registry tree, inside the
// ordinary `go test ./...` run. The make target is what CI issues; this is what makes
// the gate travel with the tests wherever they run.
func TestAnalyzeRealTree(t *testing.T) {
	findings, err := Analyze(serviceRoot(t))
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("listauthz gate must pass against the real kacho-registry tree:\n%s",
			strings.Join(findings, "\n"))
	}
}
