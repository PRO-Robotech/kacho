// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain_test

import (
	"strings"
	"testing"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// fieldViolations собирает "<field>: <description>" из BadRequest-details
// gRPC-статуса (corelib InvalidArgument.AddFieldViolation).
func fieldViolations(st *status.Status) string {
	var parts []string
	for _, d := range st.Details() {
		br, ok := d.(*errdetails.BadRequest)
		if !ok {
			continue
		}
		for _, v := range br.GetFieldViolations() {
			parts = append(parts, v.GetField()+": "+v.GetDescription())
		}
	}
	return strings.Join(parts, " | ")
}

func validLB() domain.LoadBalancer {
	return domain.LoadBalancer{
		ID:                 "nlb-x",
		ProjectID:          "prj-x",
		RegionID:           "ru-central1",
		Name:               "edge-public",
		Description:        "edge L4",
		Labels:             domain.LabelsFromMap(map[string]string{"env": "prod"}),
		Type:               domain.LBTypeExternal,
		Status:             domain.LBStatusCreating,
		SessionAffinity:    domain.SessionAffinity5Tuple,
		DeletionProtection: false,
	}
}

func TestLoadBalancer_Validate_HappyPath(t *testing.T) {
	t.Parallel()
	if err := validLB().Validate(); err != nil {
		t.Fatalf("happy-path: %v", err)
	}
}

func TestLoadBalancer_Validate_PropagatesNameError(t *testing.T) {
	t.Parallel()
	lb := validLB()
	lb.Name = "Edge_Public!"
	if err := lb.Validate(); err == nil {
		t.Fatal("expected error: invalid name regex")
	}
}

func TestLoadBalancer_Validate_PropagatesTypeError(t *testing.T) {
	t.Parallel()
	lb := validLB()
	lb.Type = "PUBLIC"
	if err := lb.Validate(); err == nil {
		t.Fatal("expected error: invalid type")
	}
}

func TestLoadBalancer_Validate_PropagatesSessionAffinityError(t *testing.T) {
	t.Parallel()
	lb := validLB()
	lb.SessionAffinity = "STICKY"
	if err := lb.Validate(); err == nil {
		t.Fatal("expected error: invalid session_affinity")
	}
}

// TestLoadBalancer_Validate_PlacementType — placement пустой либо ZONAL/REGIONAL;
// прочее отвергается. Coupling placement с type проверяется в use-case (не здесь).
func TestLoadBalancer_Validate_PlacementType(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		placement domain.PlacementType
		wantErr   bool
	}{
		{"empty ok", domain.PlacementUnspecified, false},
		{"zonal ok", domain.PlacementZonal, false},
		{"regional ok", domain.PlacementRegional, false},
		{"garbage rejected", "SOMEWHERE", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			lb := validLB()
			lb.Type = domain.LBTypeInternal
			lb.PlacementType = tc.placement
			err := lb.Validate()
			if tc.wantErr && err == nil {
				t.Fatalf("expected placement error for %q", tc.placement)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestLoadBalancer_Equal(t *testing.T) {
	t.Parallel()
	a := validLB()
	b := validLB()
	if !a.Equal(b) {
		t.Fatal("identical LBs should be equal")
	}
	b.Name = "edge-private"
	if a.Equal(b) {
		t.Fatal("differing Name should make them unequal")
	}

	c := validLB()
	c.Type = domain.LBTypeInternal
	c.PlacementType = domain.PlacementRegional
	c.DisabledAnnounceZones = []string{"ru-central1-b"}
	d := validLB()
	d.Type = domain.LBTypeInternal
	d.PlacementType = domain.PlacementRegional
	d.DisabledAnnounceZones = []string{"ru-central1-b"}
	if !c.Equal(d) {
		t.Fatal("identical placement + drain sets should be equal")
	}
	d.DisabledAnnounceZones = []string{"ru-central1-a"}
	if c.Equal(d) {
		t.Fatal("differing drain set should make them unequal")
	}
}

// TestLoadBalancer_Validate_SecurityGroupsCardinality — BVA на cap
// security_group_ids. Каждый элемент набора стоит ОДНОГО синхронного peer-Get в
// vpc (+ FGA-Check) на request-path, поэтому кардинальность обязана быть
// ограничена доменом: без cap'а один дешёвый Create разворачивался в N внешних
// round-trip'ов (амплификация). Предел живёт РОВНО в одном месте — константе
// домена: прежде ту же величину объявлял и контракт, но не проверял никто, и
// объявление снято вместе со всем семейством (kacho#1255).
func TestLoadBalancer_Validate_SecurityGroupsCardinality(t *testing.T) {
	t.Parallel()

	mk := func(n int) []string {
		out := make([]string, 0, n)
		for i := 0; i < n; i++ {
			out = append(out, "sg-"+string(rune('a'+i%26))+string(rune('a'+i/26)))
		}
		return out
	}

	t.Run("at the limit — accepted", func(t *testing.T) {
		t.Parallel()
		lb := validLB()
		lb.Type = domain.LBTypeInternal
		lb.SecurityGroupIDs = mk(domain.MaxSecurityGroupsPerLB)
		if err := lb.Validate(); err != nil {
			t.Fatalf("exactly MaxSecurityGroupsPerLB must be accepted: %v", err)
		}
	})

	t.Run("over the limit — InvalidArgument with contract tone", func(t *testing.T) {
		t.Parallel()
		lb := validLB()
		lb.Type = domain.LBTypeInternal
		lb.SecurityGroupIDs = mk(domain.MaxSecurityGroupsPerLB + 1)
		err := lb.Validate()
		if err == nil {
			t.Fatal("expected error: security_group_ids over cardinality limit")
		}
		st, ok := status.FromError(err)
		if !ok || st.Code() != codes.InvalidArgument {
			t.Fatalf("expected InvalidArgument status, got %v", err)
		}
		// Текст по конвенции Kachō лежит в BadRequest field-violation detail
		// (corelib InvalidArgument.AddFieldViolation), не в status.Message —
		// именно он и есть machine-readable observable контракта.
		if got := fieldViolations(st); !strings.Contains(got, "security_group_ids: too many security groups (max 50)") {
			t.Fatalf("expected field violation %q, got %q", "security_group_ids: too many security groups (max 50)", got)
		}
	})
}
