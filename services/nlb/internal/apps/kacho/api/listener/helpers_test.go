// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package listener

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// TestMapDomainErr_AllSentinels — each domain-sentinel maps to the right
// gRPC code; non-sentinel maps to Internal; pre-formed gRPC status passes through.
func TestMapDomainErr_AllSentinels(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		want codes.Code
	}{
		{"nil → nil", nil, codes.OK},
		{"NotFound", fmt.Errorf("%w: Listener xyz not found", domain.ErrNotFound), codes.NotFound},
		{"AlreadyExists", fmt.Errorf("%w: dup", domain.ErrAlreadyExists), codes.AlreadyExists},
		{"FailedPrecondition", fmt.Errorf("%w: bad state", domain.ErrFailedPrecondition), codes.FailedPrecondition},
		{"InvalidArg", fmt.Errorf("%w: bad input", domain.ErrInvalidArg), codes.InvalidArgument},
		{"Unavailable", fmt.Errorf("%w: peer down", domain.ErrUnavailable), codes.Unavailable},
		{"Internal", fmt.Errorf("%w: db crashed", domain.ErrInternal), codes.Internal},
		{"unknown raw error → Internal", errors.New("mystery"), codes.Internal},
		{"pre-formed gRPC status passes through", grpcstatus.Error(codes.PermissionDenied, "no access"), codes.PermissionDenied},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := mapDomainErr(tc.err)
			if tc.err == nil {
				require.NoError(t, got)
				return
			}
			require.Equal(t, tc.want, grpcstatus.Code(got))
		})
	}
}

// StripSentinel behaviour is now unit-tested centrally in
// internal/apps/kacho/api/shared/errmap_test.go (single source of truth after
// the mapper de-duplication). mapDomainErr coverage above
// still verifies listener делегирует в shared корректно.

// TestOperationToProto_FillsPrincipal — principal mapping correctness.
func TestOperationToProto_FillsPrincipal(t *testing.T) {
	t.Parallel()
	op := &operations.Operation{
		ID:          "nlb-op-1",
		Description: "test",
		CreatedAt:   time.Now().UTC(),
		ModifiedAt:  time.Now().UTC(),
		Done:        true,
		Principal: operations.Principal{
			Type:        "user",
			ID:          "usr-123",
			DisplayName: "Alice",
		},
		Error: &status.Status{Code: int32(codes.InvalidArgument), Message: "boom"},
	}
	pb := operationToProto(op)
	require.Equal(t, "user", pb.PrincipalType)
	require.Equal(t, "usr-123", pb.PrincipalId)
	require.Equal(t, "Alice", pb.PrincipalDisplayName)
	require.NotNil(t, pb.GetError())

	// Nil op → nil.
	require.Nil(t, operationToProto(nil))

	// Success response path.
	op.Error = nil
	op.Response = nil // no response
	pb = operationToProto(op)
	require.Equal(t, true, pb.Done)
	require.Nil(t, pb.GetError())
	require.Nil(t, pb.GetResponse())
}

// TestListenerPayloadMap_NilGuard — nil input returns nil.
func TestListenerPayloadMap_NilGuard(t *testing.T) {
	t.Parallel()
	require.Nil(t, listenerPayloadMap(nil))
}

// TestListenerPayloadCarriesTheWholeRecordUnderTheStateEnvelope — строитель
// нагрузки слушателя кладёт ЗАПИСЬ ЦЕЛИКОМ, под конвертом полного состояния.
//
// Здесь стояла проба про имя ключа `parent_resource_id` — предмет её исчез
// вместе с минимальным снимком: форма нагрузки заменена, а не дополнена (задача
// #1381). Ослабить её было нельзя, поэтому она ЗАМЕНЕНА утверждением о новой
// форме, и утверждение сделано ПО ПРОВОДУ — через настоящий JSON, а не через
// разборщик, собранный из тех же констант: круговой ход был бы истинен при любом
// их значении.
//
// Метки названы отдельно: клиентский отбор по меткам берёт источник ровно
// отсюда, и потеря их в кодировании оставила бы его без источника МОЛЧА.
func TestListenerPayloadCarriesTheWholeRecordUnderTheStateEnvelope(t *testing.T) {
	t.Parallel()
	rec := &kachorepo.ListenerRecord{}
	rec.ID = domain.ResourceID("nlb-listener-1")
	rec.LoadBalancerID = domain.ResourceID("nlb-1")
	rec.ProjectID = domain.ProjectID("prj-b")
	rec.Name = domain.LbName("front")
	rec.Labels = domain.LabelsFromMap(map[string]string{"env": "prod"})
	rec.Protocol = domain.ProtoTCP
	rec.Port = domain.LbPort(443)
	rec.Status = domain.ListenerStatusActive

	raw, err := json.Marshal(listenerPayloadMap(rec))
	require.NoError(t, err)

	var wire struct {
		State *kachorepo.ListenerRecord `json:"state"`
	}
	require.NoError(t, json.Unmarshal(raw, &wire))
	require.NotNil(t, wire.State,
		"нагрузка не несёт конверта полного состояния — сборщик журнала обязан отличать её "+
			"от строк прежней, минимальной формы, а по удаче разбора этого не сделать")
	require.Equal(t, "nlb-listener-1", string(wire.State.ID))
	require.Equal(t, "prj-b", string(wire.State.ProjectID))
	require.Equal(t, "nlb-1", string(wire.State.LoadBalancerID))
	require.Equal(t, map[string]string{"env": "prod"}, domain.LabelsToMap(wire.State.Labels),
		"метки не пережили круг — клиентский отбор по меткам остался бы без источника")
	require.Equal(t, domain.ProtoTCP, wire.State.Protocol)
	require.Equal(t, domain.LbPort(443), wire.State.Port)
	require.Equal(t, domain.ListenerStatusActive, wire.State.Status)
}

// TestListenerRecordToPb_NilGuard — nil → Internal.
func TestListenerRecordToPb_NilGuard(t *testing.T) {
	t.Parallel()
	_, err := listenerRecordToPb(nil)
	require.Error(t, err)
	require.Equal(t, codes.Internal, grpcstatus.Code(err))
}

// TestLoggerOrDiscard_Default — nil logger → slog.Default.
func TestLoggerOrDiscard_Default(t *testing.T) {
	t.Parallel()
	require.Same(t, slog.Default(), loggerOrDiscard(nil))
	custom := slog.New(slog.Default().Handler())
	require.Same(t, custom, loggerOrDiscard(custom))
}
