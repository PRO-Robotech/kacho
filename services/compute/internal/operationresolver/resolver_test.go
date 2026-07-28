// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package operationresolver

import (
	"context"
	"errors"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/operations"

	"google.golang.org/protobuf/proto"

	computev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"

	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	"github.com/PRO-Robotech/kacho/services/compute/internal/ports"
)

// instanceReader — конфигурируемый read-порт: Get возвращает либо заранее
// заданный ресурс (present), либо ports.ErrNotFound (absent), либо произвольную
// transient-ошибку.
type instanceReader struct {
	inst *domain.Instance
	err  error
}

func (r instanceReader) Get(ctx context.Context, id string) (*domain.Instance, error) {
	if r.err != nil {
		return nil, r.err
	}
	if r.inst == nil {
		return nil, ports.ErrNotFound
	}
	return r.inst, nil
}

func newOp(t *testing.T, meta proto.Message) operations.Operation {
	t.Helper()
	op, err := operations.New("epd", "test op", meta)
	if err != nil {
		t.Fatalf("operations.New: %v", err)
	}
	return op
}

func TestResolve_CreateInstancePresent_Done(t *testing.T) {
	r := New(Readers{Instance: instanceReader{inst: &domain.Instance{ID: "ins1"}}})
	op := newOp(t, &computev1.CreateInstanceMetadata{InstanceId: "ins1"})

	res, err := r.Resolve(context.Background(), op)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Outcome != operations.OutcomeDone {
		t.Fatalf("outcome = %v, want Done", res.Outcome)
	}
	if res.Response == nil {
		t.Fatal("Response nil, want marshalled Instance")
	}
	got, err := res.Response.UnmarshalNew()
	if err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	d, ok := got.(*computev1.Instance)
	if !ok || d.GetId() != "ins1" {
		t.Fatalf("response = %T %v, want Instance{id:ins1}", got, got)
	}
}

func TestResolve_CreateInstanceAbsent_Interrupted(t *testing.T) {
	r := New(Readers{Instance: instanceReader{inst: nil}})
	op := newOp(t, &computev1.CreateInstanceMetadata{InstanceId: "ins1"})

	res, err := r.Resolve(context.Background(), op)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Outcome != operations.OutcomeInterrupted {
		t.Fatalf("outcome = %v, want Interrupted", res.Outcome)
	}
}

func TestResolve_DeleteInstanceAbsent_Done(t *testing.T) {
	r := New(Readers{Instance: instanceReader{inst: nil}})
	op := newOp(t, &computev1.DeleteInstanceMetadata{InstanceId: "epd9"})

	res, err := r.Resolve(context.Background(), op)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Outcome != operations.OutcomeDone {
		t.Fatalf("outcome = %v, want Done", res.Outcome)
	}
	if res.Response != nil {
		t.Fatalf("Response = %v, want nil (Empty semantics) for delete", res.Response)
	}
}

func TestResolve_DeleteInstancePresent_Interrupted(t *testing.T) {
	r := New(Readers{Instance: instanceReader{inst: &domain.Instance{ID: "epd9"}}})
	op := newOp(t, &computev1.DeleteInstanceMetadata{InstanceId: "epd9"})

	res, err := r.Resolve(context.Background(), op)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Outcome != operations.OutcomeInterrupted {
		t.Fatalf("outcome = %v, want Interrupted", res.Outcome)
	}
}

func TestResolve_StopInstancePresent_DoneWithInstance(t *testing.T) {
	r := New(Readers{Instance: instanceReader{inst: &domain.Instance{ID: "epd9"}}})
	op := newOp(t, &computev1.StopInstanceMetadata{InstanceId: "epd9"})

	res, err := r.Resolve(context.Background(), op)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Outcome != operations.OutcomeDone {
		t.Fatalf("outcome = %v, want Done", res.Outcome)
	}
	got, err := res.Response.UnmarshalNew()
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if in, ok := got.(*computev1.Instance); !ok || in.GetId() != "epd9" {
		t.Fatalf("response = %T, want Instance{id:epd9}", got)
	}
}

func TestResolve_TransientReadError_Propagates(t *testing.T) {
	boom := errors.New("db down")
	r := New(Readers{Instance: instanceReader{err: boom}})
	op := newOp(t, &computev1.UpdateInstanceMetadata{InstanceId: "ins1"})

	_, err := r.Resolve(context.Background(), op)
	if err == nil {
		t.Fatal("want error on transient read failure, got nil")
	}
}

func TestResolve_NilMetadata_Skip(t *testing.T) {
	r := New(Readers{})
	res, err := r.Resolve(context.Background(), operations.Operation{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Outcome != operations.OutcomeSkip {
		t.Fatalf("outcome = %v, want Skip", res.Outcome)
	}
}

func TestResolve_UnknownMetadata_Skip(t *testing.T) {
	r := New(Readers{Instance: instanceReader{inst: &domain.Instance{ID: "ins1"}}})
	// A blocked/unwired metadata type (the resolver's switch has no case for it,
	// so it is not a resource this resolver reconciles) → Skip. MachineType is an
	// Internal*-only admin catalog, deliberately outside the orphan-resolver.
	op := newOp(t, &computev1.CreateMachineTypeMetadata{})

	res, err := r.Resolve(context.Background(), op)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Outcome != operations.OutcomeSkip {
		t.Fatalf("outcome = %v, want Skip", res.Outcome)
	}
}
