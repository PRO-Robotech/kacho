// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package listfiltergate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The per-service tests (services/{compute,nlb,vpc}/tools/audit_list_filter_test.go)
// inject into each REAL tree and are the primary proof. What is locked here is the
// analyser's own discrimination, on fixtures small enough that each case isolates
// ONE property — and every property is asserted in BOTH directions, because a gate
// that only ever fires proves nothing about what it would let through.

// flatProfile mirrors compute: one package, resources told apart by receiver TYPE.
var flatProfile = Profile{
	Service:        "fixture-flat",
	AnchorRoot:     "internal/handler",
	PerPackage:     false,
	ReceiverSuffix: "Handler",
	Filters:        []string{"filterVisible", "FilterVisibleIDs"},
	Banned:         []string{"ListAllowedIDs", "ListObjects"},
}

// pkgProfile mirrors vpc/nlb: one package per resource, one transport type name.
var pkgProfile = Profile{
	Service:        "fixture-pkg",
	AnchorRoot:     "internal/apps/kacho/api",
	PerPackage:     true,
	ReceiverSuffix: "Handler",
	Filters:        []string{"FilterVisibleIDs", "FilterVisiblePage"},
	Banned:         []string{"ListAllowedIDs", "ListObjects"},
}

// run materialises a throwaway tree and audits it.
func run(t *testing.T, p Profile, files map[string]string, allow ...string) (string, error) {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	var buf strings.Builder
	_, err := Audit(p, Options{Root: root, Allow: allow}, &buf)
	return buf.String(), err
}

func TestAudit_DiscriminatesLeakFromLegitimateShape(t *testing.T) {
	cases := []struct {
		name    string
		profile Profile
		files   map[string]string
		allow   []string
		wantErr bool
	}{
		{
			name:    "compliant: List calls the filter",
			profile: flatProfile,
			files: map[string]string{"internal/handler/instance_handler.go": `package handler

type InstanceHandler struct{ listFilter authzfilter.Filter }

func (h *InstanceHandler) List(ctx context.Context) error {
	rows, _ := h.svc.List(ctx)
	visible, _ := filterVisible(ctx, h.listFilter, rows)
	_ = visible
	return nil
}
`},
		},
		{
			name:    "leak: the filter's name survives only as the field's TYPE",
			profile: flatProfile,
			// This is the shape that kept compute's previous gate green: searching
			// the FILE finds `authzfilter.Filter` in the struct declaration, which
			// says nothing whatever about what List does.
			files: map[string]string{"internal/handler/instance_handler.go": `package handler

type InstanceHandler struct{ listFilter authzfilter.Filter }

func (h *InstanceHandler) List(ctx context.Context) error {
	rows, _ := h.svc.List(ctx)
	_ = rows
	return nil
}
`},
			wantErr: true,
		},
		{
			name:    "leak: the filter is named only in a comment",
			profile: flatProfile,
			files: map[string]string{"internal/handler/instance_handler.go": `package handler

type InstanceHandler struct{}

func (h *InstanceHandler) List(ctx context.Context) error {
	// Visibility is narrowed per object via filterVisible / FilterVisibleIDs.
	rows, _ := h.svc.List(ctx)
	_ = rows
	return nil
}
`},
			wantErr: true,
		},
		{
			name:    "leak: a filtered SIBLING method must not vouch for List",
			profile: flatProfile,
			// Get filters; List does not. A file-scoped or package-scoped search sees
			// the call and passes. Only following what List itself calls catches it.
			files: map[string]string{"internal/handler/instance_handler.go": `package handler

type InstanceHandler struct{}

func (h *InstanceHandler) Get(ctx context.Context) error {
	one, _ := h.svc.Get(ctx)
	visible, _ := filterVisible(ctx, h.listFilter, one)
	_ = visible
	return nil
}

func (h *InstanceHandler) List(ctx context.Context) error {
	rows, _ := h.svc.List(ctx)
	_ = rows
	return nil
}
`},
			wantErr: true,
		},
		{
			name:    "compliant: the receiver variable is named anything at all",
			profile: flatProfile,
			// Identification is by the receiver's TYPE. `hd` here, `h` above; same
			// declaration, same verdict.
			files: map[string]string{"internal/handler/instance_handler.go": `package handler

type InstanceHandler struct{}

func (hd *InstanceHandler) List(ctx context.Context) error {
	rows, _ := hd.svc.List(ctx)
	visible, _ := filterVisible(ctx, hd.listFilter, rows)
	_ = visible
	return nil
}
`},
		},
		{
			name:    "compliant: the filter sits in a package-local helper List calls",
			profile: pkgProfile,
			// vpc's real shape: the use-case calls a helper, the helper calls the
			// port. The walk has to follow both hops.
			files: map[string]string{"internal/apps/kacho/api/network/handler.go": `package network

type Handler struct{ list *ListNetworksUseCase }

func (h *Handler) List(ctx context.Context) error {
	_, _ = h.list.Execute(ctx)
	return nil
}

type ListNetworksUseCase struct{ filter ListFilter }

func (u *ListNetworksUseCase) Execute(ctx context.Context) ([]string, error) {
	rows, _ := u.repo.List(ctx)
	return filterVisibleNetworks(ctx, u.filter, rows)
}

func filterVisibleNetworks(ctx context.Context, filter ListFilter, rows []string) ([]string, error) {
	return filter.FilterVisibleIDs(ctx, rows)
}
`},
		},
		{
			name:    "leak: the helper stops filtering, two hops down",
			profile: pkgProfile,
			files: map[string]string{"internal/apps/kacho/api/network/handler.go": `package network

type Handler struct{ list *ListNetworksUseCase }

func (h *Handler) List(ctx context.Context) error {
	_, _ = h.list.Execute(ctx)
	return nil
}

type ListNetworksUseCase struct{ filter ListFilter }

func (u *ListNetworksUseCase) Execute(ctx context.Context) ([]string, error) {
	rows, _ := u.repo.List(ctx)
	return filterVisibleNetworks(ctx, u.filter, rows)
}

func filterVisibleNetworks(ctx context.Context, filter ListFilter, rows []string) ([]string, error) {
	return rows, nil
}
`},
			wantErr: true,
		},
		{
			name:    "compliant: the declaration may live in any file of the package",
			profile: pkgProfile,
			files: map[string]string{
				"internal/apps/kacho/api/network/grpc_handler.go": `package network

type Handler struct{ list *ListNetworksUseCase }

func (h *Handler) List(ctx context.Context) error {
	_, _ = h.list.Execute(ctx)
	return nil
}
`,
				"internal/apps/kacho/api/network/list_usecase.go": `package network

type ListNetworksUseCase struct{ filter ListFilter }

func (u *ListNetworksUseCase) Execute(ctx context.Context) ([]string, error) {
	rows, _ := u.repo.List(ctx)
	return u.filter.FilterVisibleIDs(ctx, rows)
}
`,
			},
		},
		{
			name:    "reject: enumerate-then-narrow is not a per-page check",
			profile: pkgProfile,
			files: map[string]string{"internal/apps/kacho/api/network/handler.go": `package network

type Handler struct{ list *ListNetworksUseCase }

func (h *Handler) List(ctx context.Context) error {
	_, _ = h.list.Execute(ctx)
	return nil
}

type ListNetworksUseCase struct{ filter ListFilter }

func (u *ListNetworksUseCase) Execute(ctx context.Context) ([]string, error) {
	allowed, _ := u.filter.ListAllowedIDs(ctx)
	rows, _ := u.repo.ListByIDs(ctx, allowed)
	return u.filter.FilterVisibleIDs(ctx, rows)
}
`},
			wantErr: true,
		},
		{
			name:    "compliant: a comment may EXPLAIN why enumeration is banned",
			profile: pkgProfile,
			// The converse of the case above: documenting the ban must not trip it.
			files: map[string]string{"internal/apps/kacho/api/network/handler.go": `package network

type Handler struct{ list *ListNetworksUseCase }

func (h *Handler) List(ctx context.Context) error {
	_, _ = h.list.Execute(ctx)
	return nil
}

type ListNetworksUseCase struct{ filter ListFilter }

func (u *ListNetworksUseCase) Execute(ctx context.Context) ([]string, error) {
	// Never ListAllowedIDs/ListObjects here: that answer is capped server-side.
	rows, _ := u.repo.List(ctx)
	return u.filter.FilterVisibleIDs(ctx, rows)
}
`},
		},
		{
			name:    "not a resource: a package with no public List is not judged",
			profile: pkgProfile,
			files: map[string]string{
				"internal/apps/kacho/api/network/handler.go": `package network

type Handler struct{ list *ListNetworksUseCase }

func (h *Handler) List(ctx context.Context) error {
	_, _ = h.list.Execute(ctx)
	return nil
}

type ListNetworksUseCase struct{ filter ListFilter }

func (u *ListNetworksUseCase) Execute(ctx context.Context) ([]string, error) {
	rows, _ := u.repo.List(ctx)
	return u.filter.FilterVisibleIDs(ctx, rows)
}
`,
				// No List at all, and no filter either: it must not be a finding.
				"internal/apps/kacho/api/operation/handler.go": `package operation

type Handler struct{}

func (h *Handler) Get(ctx context.Context) error { return nil }
`,
			},
		},
		{
			name:    "not a resource: ListOperations is a different method",
			profile: flatProfile,
			files: map[string]string{"internal/handler/instance_handler.go": `package handler

type InstanceHandler struct{}

func (h *InstanceHandler) List(ctx context.Context) error {
	rows, _ := h.svc.List(ctx)
	visible, _ := filterVisible(ctx, h.listFilter, rows)
	_ = visible
	return nil
}

func (h *InstanceHandler) ListOperations(ctx context.Context) error {
	rows, _ := h.svc.ListOperations(ctx)
	_ = rows
	return nil
}
`},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := run(t, tc.profile, tc.files, tc.allow...)
			if (err != nil) != tc.wantErr {
				t.Fatalf("verdict: gotErr=%v wantErr=%v\n--- output ---\n%s", err != nil, tc.wantErr, out)
			}
		})
	}
}

// TestAudit_NothingExaminedIsNotOK — "zero findings" must be unreachable from "zero
// read". Both shapes below used to print OK and exit 0, which is the worst answer a
// gate can give: indistinguishable from a clean tree, so a gate pointed at the wrong
// place certifies a service it never opened.
func TestAudit_NothingExaminedIsNotOK(t *testing.T) {
	cases := []struct {
		name    string
		profile Profile
		files   map[string]string
	}{
		{
			name:    "flat: the anchor root does not exist",
			profile: flatProfile,
			files:   map[string]string{"internal/apps/kacho/api/network/handler.go": "package network\n"},
		},
		{
			name:    "flat: the anchor root holds no public List",
			profile: flatProfile,
			files:   map[string]string{"internal/handler/doc.go": "package handler\n"},
		},
		{
			name:    "per-package: the anchor root does not exist",
			profile: pkgProfile,
			files:   map[string]string{"internal/handler/instance_handler.go": "package handler\n"},
		},
		{
			name:    "per-package: the anchor root holds no package",
			profile: pkgProfile,
			files:   map[string]string{"internal/apps/kacho/api/doc.go": "package api\n"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := run(t, tc.profile, tc.files)
			if err == nil {
				t.Fatalf("the gate exited 0 having examined nothing — that is not a pass\n--- output ---\n%s", out)
			}
			if !strings.Contains(out, "examined") {
				t.Errorf("even a run that read nothing must state its census\n--- output ---\n%s", out)
			}
		})
	}
}

// TestAudit_ReportsWhatItExamined — a pass is only worth reading if it says what it
// judged, so the census is asserted rather than narrated.
func TestAudit_ReportsWhatItExamined(t *testing.T) {
	out, err := run(t, flatProfile, map[string]string{"internal/handler/instance_handler.go": `package handler

type InstanceHandler struct{}

func (h *InstanceHandler) List(ctx context.Context) error {
	rows, _ := h.svc.List(ctx)
	visible, _ := filterVisible(ctx, h.listFilter, rows)
	_ = visible
	return nil
}

type MachineTypeHandler struct{}

func (h *MachineTypeHandler) List(ctx context.Context) error { return nil }
`}, "machine_type")
	if err != nil {
		t.Fatalf("compliant fixture must pass: %v\n--- output ---\n%s", err, out)
	}
	for _, want := range []string{"1 file(s)", "2 resource(s)", "checked instance", "whitelisted machine_type"} {
		if !strings.Contains(out, want) {
			t.Errorf("the census must state what was examined (missing %q)\n--- output ---\n%s", want, out)
		}
	}
}

// TestAudit_ExpiredWhitelistEntryIsAFinding — an exclusion must expire by itself.
// Once no such resource exists the entry suppresses nothing, and the next resource
// to inherit that name inherits the blind spot in silence.
func TestAudit_ExpiredWhitelistEntryIsAFinding(t *testing.T) {
	files := map[string]string{"internal/handler/instance_handler.go": `package handler

type InstanceHandler struct{}

func (h *InstanceHandler) List(ctx context.Context) error {
	rows, _ := h.svc.List(ctx)
	visible, _ := filterVisible(ctx, h.listFilter, rows)
	_ = visible
	return nil
}
`}

	out, err := run(t, flatProfile, files, "disk_type")
	if err == nil {
		t.Fatalf("a whitelist entry matching no resource must be reported\n--- output ---\n%s", out)
	}
	if !strings.Contains(out, "disk_type") {
		t.Errorf("the finding must name the expired entry\n--- output ---\n%s", out)
	}

	// Converse: an entry that still has a subject must stay silent, so the rule is
	// not merely "flag every whitelist".
	files["internal/handler/machine_type_handler.go"] = `package handler

type MachineTypeHandler struct{}

func (h *MachineTypeHandler) List(ctx context.Context) error { return nil }
`
	if out, err := run(t, flatProfile, files, "machine_type"); err != nil {
		t.Fatalf("a whitelist entry that still matches a resource must not be a finding: %v\n--- output ---\n%s", err, out)
	}
}
