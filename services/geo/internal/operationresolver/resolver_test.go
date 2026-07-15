// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package operationresolver_test

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/PRO-Robotech/kacho/pkg/operations"

	geov1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/geo/v1"

	geoerrors "github.com/PRO-Robotech/kacho/services/geo/internal/errors"
	"github.com/PRO-Robotech/kacho/services/geo/internal/operationresolver"
)

type stubRegionReader struct {
	region *geov1.Region
	err    error
}

func (s stubRegionReader) Get(_ context.Context, _ string) (*geov1.Region, error) {
	return s.region, s.err
}

type stubZoneReader struct {
	zone *geov1.Zone
	err  error
}

func (s stubZoneReader) Get(_ context.Context, _ string) (*geov1.Zone, error) {
	return s.zone, s.err
}

func mustAny(t *testing.T, m proto.Message) *anypb.Any {
	t.Helper()
	a, err := anypb.New(m)
	if err != nil {
		t.Fatalf("anypb.New: %v", err)
	}
	return a
}

func opWith(meta *anypb.Any) operations.Operation {
	return operations.Operation{ID: "geo-op-1", Metadata: meta}
}

// --- Region ---

func TestResolve_CreateRegion_present_done(t *testing.T) {
	rs := operationresolver.New(operationresolver.Readers{
		Region: stubRegionReader{region: &geov1.Region{Id: "region-1", Name: "Region 1"}},
	}, nil)
	res, err := rs.Resolve(context.Background(), opWith(mustAny(t, &geov1.CreateRegionMetadata{RegionId: "region-1"})))
	if err != nil {
		t.Fatalf("Resolve err = %v", err)
	}
	if res.Outcome != operations.OutcomeDone {
		t.Fatalf("outcome = %v, want Done", res.Outcome)
	}
	msg, err := res.Response.UnmarshalNew()
	if err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if r, ok := msg.(*geov1.Region); !ok || r.GetId() != "region-1" {
		t.Fatalf("response = %+v, want Region region-1", msg)
	}
}

func TestResolve_CreateRegion_absent_interrupted(t *testing.T) {
	rs := operationresolver.New(operationresolver.Readers{
		Region: stubRegionReader{err: geoerrors.ErrNotFound},
	}, nil)
	res, err := rs.Resolve(context.Background(), opWith(mustAny(t, &geov1.CreateRegionMetadata{RegionId: "ghost"})))
	if err != nil {
		t.Fatalf("Resolve err = %v", err)
	}
	if res.Outcome != operations.OutcomeInterrupted {
		t.Fatalf("outcome = %v, want Interrupted", res.Outcome)
	}
}

func TestResolve_UpdateRegion_present_done(t *testing.T) {
	rs := operationresolver.New(operationresolver.Readers{
		Region: stubRegionReader{region: &geov1.Region{Id: "region-1"}},
	}, nil)
	res, err := rs.Resolve(context.Background(), opWith(mustAny(t, &geov1.UpdateRegionMetadata{RegionId: "region-1"})))
	if err != nil {
		t.Fatalf("Resolve err = %v", err)
	}
	if res.Outcome != operations.OutcomeDone {
		t.Fatalf("outcome = %v, want Done", res.Outcome)
	}
}

func TestResolve_DeleteRegion_absent_doneEmpty(t *testing.T) {
	rs := operationresolver.New(operationresolver.Readers{
		Region: stubRegionReader{err: geoerrors.ErrNotFound},
	}, nil)
	res, err := rs.Resolve(context.Background(), opWith(mustAny(t, &geov1.DeleteRegionMetadata{RegionId: "region-1"})))
	if err != nil {
		t.Fatalf("Resolve err = %v", err)
	}
	if res.Outcome != operations.OutcomeDone {
		t.Fatalf("outcome = %v, want Done (delete confirmed)", res.Outcome)
	}
	if res.Response != nil {
		t.Fatalf("delete response = %v, want nil (Empty-семантика)", res.Response)
	}
}

func TestResolve_DeleteRegion_present_interrupted(t *testing.T) {
	rs := operationresolver.New(operationresolver.Readers{
		Region: stubRegionReader{region: &geov1.Region{Id: "region-1"}},
	}, nil)
	res, err := rs.Resolve(context.Background(), opWith(mustAny(t, &geov1.DeleteRegionMetadata{RegionId: "region-1"})))
	if err != nil {
		t.Fatalf("Resolve err = %v", err)
	}
	if res.Outcome != operations.OutcomeInterrupted {
		t.Fatalf("outcome = %v, want Interrupted (delete didn't commit)", res.Outcome)
	}
}

// --- Zone ---

func TestResolve_CreateZone_present_done(t *testing.T) {
	rs := operationresolver.New(operationresolver.Readers{
		Zone: stubZoneReader{zone: &geov1.Zone{Id: "region-1-a", RegionId: "region-1", Status: geov1.Zone_UP}},
	}, nil)
	res, err := rs.Resolve(context.Background(), opWith(mustAny(t, &geov1.CreateZoneMetadata{ZoneId: "region-1-a"})))
	if err != nil {
		t.Fatalf("Resolve err = %v", err)
	}
	if res.Outcome != operations.OutcomeDone {
		t.Fatalf("outcome = %v, want Done", res.Outcome)
	}
	msg, _ := res.Response.UnmarshalNew()
	if z, ok := msg.(*geov1.Zone); !ok || z.GetId() != "region-1-a" || z.GetStatus() != geov1.Zone_UP {
		t.Fatalf("response = %+v, want Zone region-1-a UP", msg)
	}
}

func TestResolve_DeleteZone_absent_doneEmpty(t *testing.T) {
	rs := operationresolver.New(operationresolver.Readers{
		Zone: stubZoneReader{err: geoerrors.ErrNotFound},
	}, nil)
	res, err := rs.Resolve(context.Background(), opWith(mustAny(t, &geov1.DeleteZoneMetadata{ZoneId: "region-1-a"})))
	if err != nil {
		t.Fatalf("Resolve err = %v", err)
	}
	if res.Outcome != operations.OutcomeDone || res.Response != nil {
		t.Fatalf("res = %+v, want Done+nil", res)
	}
}

// --- edge cases ---

// TestResolve_transientReadError_propagates — не-NotFound ошибка чтения ресурса
// возвращается движку (reconcile_errors++), orphan НЕ разрешается.
func TestResolve_transientReadError_propagates(t *testing.T) {
	boom := errors.New("connection reset")
	rs := operationresolver.New(operationresolver.Readers{
		Region: stubRegionReader{err: boom},
	}, nil)
	_, err := rs.Resolve(context.Background(), opWith(mustAny(t, &geov1.CreateRegionMetadata{RegionId: "region-1"})))
	if err == nil {
		t.Fatal("Resolve err = nil, want transient error propagated")
	}
}

// TestResolve_nilMetadata_skip — операция без метаданных пропускается (Skip).
func TestResolve_nilMetadata_skip(t *testing.T) {
	rs := operationresolver.New(operationresolver.Readers{}, nil)
	res, err := rs.Resolve(context.Background(), opWith(nil))
	if err != nil {
		t.Fatalf("Resolve err = %v", err)
	}
	if res.Outcome != operations.OutcomeSkip {
		t.Fatalf("outcome = %v, want Skip", res.Outcome)
	}
}

// TestResolve_unconfiguredReader_skip — metadata есть, но соответствующий reader
// не сконфигурирован (nil-порт) → Skip (не паника, не ложный Interrupted).
func TestResolve_unconfiguredReader_skip(t *testing.T) {
	rs := operationresolver.New(operationresolver.Readers{}, nil) // Zone==nil
	res, err := rs.Resolve(context.Background(), opWith(mustAny(t, &geov1.CreateZoneMetadata{ZoneId: "region-1-a"})))
	if err != nil {
		t.Fatalf("Resolve err = %v", err)
	}
	if res.Outcome != operations.OutcomeSkip {
		t.Fatalf("outcome = %v, want Skip (reader nil)", res.Outcome)
	}
}
