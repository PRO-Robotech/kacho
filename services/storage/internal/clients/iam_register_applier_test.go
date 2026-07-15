// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package clients

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/outbox/drainer"
	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"

	"github.com/PRO-Robotech/kacho/services/storage/internal/fgaregister"
)

// fakeIAMRegister — тестовый двойник IAMRegisterRPC: записывает вызовы
// RegisterResource/UnregisterResource и возвращает заранее заданные ошибки.
type fakeIAMRegister struct {
	registerCalls   []*iamv1.RegisterResourceRequest
	unregisterCalls []*iamv1.UnregisterResourceRequest
	registerErr     error
	unregisterErr   error
}

func (f *fakeIAMRegister) RegisterResource(_ context.Context, in *iamv1.RegisterResourceRequest, _ ...grpc.CallOption) (*iamv1.RegisterResourceResponse, error) {
	f.registerCalls = append(f.registerCalls, in)
	return &iamv1.RegisterResourceResponse{}, f.registerErr
}

func (f *fakeIAMRegister) UnregisterResource(_ context.Context, in *iamv1.UnregisterResourceRequest, _ ...grpc.CallOption) (*iamv1.UnregisterResourceResponse, error) {
	f.unregisterCalls = append(f.unregisterCalls, in)
	return &iamv1.UnregisterResourceResponse{}, f.unregisterErr
}

// TestIAMRegisterApplier_Register — EventRegister → RegisterResource с правильным
// owner-tuple (project:<id> #project @storage_volume:<id>).
func TestIAMRegisterApplier_Register(t *testing.T) {
	f := &fakeIAMRegister{}
	apply := NewIAMRegisterApplier(f)

	p := fgaregister.Payload{Tuple: fgaregister.StorageVolume("prj-1", "vol-abc"), ParentProjectID: "prj-1"}
	require.NoError(t, apply(context.Background(), fgaregister.EventRegister, p))

	require.Len(t, f.registerCalls, 1)
	require.Empty(t, f.unregisterCalls)
	require.Equal(t, "project:prj-1", f.registerCalls[0].SubjectId)
	require.Equal(t, "project", f.registerCalls[0].Relation)
	require.Equal(t, "storage_volume:vol-abc", f.registerCalls[0].Object)
	require.Equal(t, "prj-1", f.registerCalls[0].ParentProjectId)
}

// TestIAMRegisterApplier_Unregister — EventUnregister → UnregisterResource с tuple.
func TestIAMRegisterApplier_Unregister(t *testing.T) {
	f := &fakeIAMRegister{}
	apply := NewIAMRegisterApplier(f)

	p := fgaregister.Payload{Tuple: fgaregister.StorageSnapshot("prj-1", "snp-x")}
	require.NoError(t, apply(context.Background(), fgaregister.EventUnregister, p))

	require.Len(t, f.unregisterCalls, 1)
	require.Empty(t, f.registerCalls)
	require.Equal(t, "storage_snapshot:snp-x", f.unregisterCalls[0].Object)
}

// TestIAMRegisterApplier_UnknownEvent — неизвестный event_type → permanent (poison).
func TestIAMRegisterApplier_UnknownEvent(t *testing.T) {
	apply := NewIAMRegisterApplier(&fakeIAMRegister{})
	err := apply(context.Background(), "fga.bogus", fgaregister.Payload{Tuple: fgaregister.StorageVolume("p", "v")})
	require.Error(t, err)
	require.True(t, errors.Is(err, drainer.ErrPermanent))
}

// TestClassifyRegisterErr — InvalidArgument → permanent; Unavailable/PermissionDenied
// → transient (ретрай, intent durable — grant fga_writer мог ещё не осесть).
func TestClassifyRegisterErr(t *testing.T) {
	require.NoError(t, classifyRegisterErr(nil))
	require.True(t, errors.Is(classifyRegisterErr(status.Error(codes.InvalidArgument, "x")), drainer.ErrPermanent))
	require.False(t, errors.Is(classifyRegisterErr(status.Error(codes.Unavailable, "x")), drainer.ErrPermanent))
	require.False(t, errors.Is(classifyRegisterErr(status.Error(codes.PermissionDenied, "x")), drainer.ErrPermanent))
}

// TestDecodeFGARegisterPayload — валидный payload декодируется; malformed / неполный
// tuple → permanent (drainer не ретраит бесконечно).
func TestDecodeFGARegisterPayload(t *testing.T) {
	good, err := fgaregister.Encode(fgaregister.Payload{Tuple: fgaregister.StorageVolume("prj-1", "vol-1")})
	require.NoError(t, err)
	p, err := DecodeFGARegisterPayload(good)
	require.NoError(t, err)
	require.Equal(t, "storage_volume:vol-1", p.Object)

	_, err = DecodeFGARegisterPayload([]byte("{not-json"))
	require.True(t, errors.Is(err, drainer.ErrPermanent))

	incomplete, err := json.Marshal(map[string]string{"subject_id": "project:p"}) // relation/object пусты
	require.NoError(t, err)
	_, err = DecodeFGARegisterPayload(incomplete)
	require.True(t, errors.Is(err, drainer.ErrPermanent))
}
