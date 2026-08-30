// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package listener

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/H-BF/corlib/pkg/option"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// TestUpdateListener_GWT_LST_018_MutableFields_HappyPath — name + description
// in mask → applied + outbox UPDATED.
func TestUpdateListener_GWT_LST_018_MutableFields(t *testing.T) {
	t.Parallel()
	suite := newUpdateSuite(t)
	op, err := suite.uc.Run(context.Background(), &lbv1.UpdateListenerRequest{
		ListenerId:  string(suite.listener.ID),
		UpdateMask:  &fieldmaskpb.FieldMask{Paths: []string{"name", "description"}},
		Name:        "https",
		Description: "edge listener",
	})
	require.NoError(t, err)
	done := awaitOpDone(t, suite.ops, op.ID, time.Second)
	require.Nil(t, done.Error)

	got := suite.getListener(string(suite.listener.ID))
	require.Equal(t, domain.LbName("https"), got.Name)
	require.Equal(t, domain.LbDescription("edge listener"), got.Description)

	events := suite.repo.pendingOutbox()
	require.Len(t, events, 1)
	require.Equal(t, kachorepo.OutboxActionUpdated, events[0].Action)
	require.Equal(t, "nlb_listener", events[0].ResourceType)
}

// TestUpdateListener_GWT_LST_019_ImmutableLoadBalancerID — immutable in mask
// → InvalidArgument фиксированный текст.
func TestUpdateListener_GWT_LST_019_ImmutableLoadBalancerID(t *testing.T) {
	t.Parallel()
	suite := newUpdateSuite(t)
	_, err := suite.uc.Run(context.Background(), &lbv1.UpdateListenerRequest{
		ListenerId: string(suite.listener.ID),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"load_balancer_id"}},
	})
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Equal(t, "load_balancer_id is immutable after Listener.Create",
		status.Convert(err).Message())
}

// TestUpdateListener_GWT_LST_020_ImmutableFields — all immutable mask paths
// individually rejected. VIP консолидирован на LoadBalancer: address_id/ip_version/
// subnet_id/region_id сняты с листенера (proto reserved) — в immutable-списке их
// больше нет (адресовать в mask нельзя, путь → "not recognised"). По той же
// причине здесь нет target_port: backend-порт снят с контракта и живёт на группе
// целей, поэтому путь маски с этим именем неизвестен, а не неизменяем — это
// закреплено отдельно, в target_port_retired_test.go.
func TestUpdateListener_GWT_LST_020_ImmutableFields(t *testing.T) {
	t.Parallel()
	//
	// Текст называется ДОСЛОВНО по каждому полю, а не выводится из имени поля общим
	// шаблоном (#1671). Общий шаблон здесь не годится дважды: он не различает
	// область владения, у которой есть следующий шаг, — и он же переживает правку
	// контракта молча, потому что проверяет ту часть текста, которую сам и
	// построил.
	immutable := map[string]string{
		"protocol": "protocol is immutable after Listener.Create",
		"port":     "port is immutable after Listener.Create",
		"project_id": "project_id is immutable after Listener.Create; " +
			"use NetworkLoadBalancerService.Move on the parent load balancer",
	}
	for field, want := range immutable {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			suite := newUpdateSuite(t)
			_, err := suite.uc.Run(context.Background(), &lbv1.UpdateListenerRequest{
				ListenerId: string(suite.listener.ID),
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{field}},
			})
			require.Error(t, err)
			require.Equal(t, codes.InvalidArgument, status.Code(err))
			require.Equal(t, want, status.Convert(err).Message())
		})
	}
}

// TestUpdateListener_EmptyMask_FullPATCH — пустой/nil mask → full-object PATCH:
// применяются ВСЕ mutable-поля из тела запроса (api-conventions update_mask
// discipline, parity с loadbalancer/targetgroup applyUpdateMask). НЕ
// InvalidArgument.
func TestUpdateListener_EmptyMask_FullPATCH(t *testing.T) {
	t.Parallel()
	suite := newUpdateSuite(t)
	op, err := suite.uc.Run(context.Background(), &lbv1.UpdateListenerRequest{
		ListenerId:  string(suite.listener.ID),
		UpdateMask:  nil,
		Name:        "https",
		Description: "edge listener",
	})
	require.NoError(t, err)
	done := awaitOpDone(t, suite.ops, op.ID, time.Second)
	require.Nil(t, done.Error)

	got := suite.getListener(string(suite.listener.ID))
	require.Equal(t, domain.LbName("https"), got.Name)
	require.Equal(t, domain.LbDescription("edge listener"), got.Description)
}

// TestUpdateListener_UnknownMaskField — unknown path → InvalidArgument.
func TestUpdateListener_UnknownMaskField(t *testing.T) {
	t.Parallel()
	suite := newUpdateSuite(t)
	_, err := suite.uc.Run(context.Background(), &lbv1.UpdateListenerRequest{
		ListenerId: string(suite.listener.ID),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"made_up_field"}},
	})
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Contains(t, err.Error(), "made_up_field")
}

// TestUpdateListener_NotFound — listener_id doesn't exist → NotFound sync.
func TestUpdateListener_NotFound(t *testing.T) {
	t.Parallel()
	suite := newUpdateSuite(t)
	_, err := suite.uc.Run(context.Background(), &lbv1.UpdateListenerRequest{
		ListenerId: "lstNOTEXISTS000001",
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"name"}},
		Name:       "any",
	})
	require.Error(t, err)
	require.Equal(t, codes.NotFound, status.Code(err))
}

// TestUpdateListener_GWT_LST_021_DefaultTGRegionMismatch — TG in another region
// → FailedPrecondition фиксированный текст.
func TestUpdateListener_GWT_LST_021_DefaultTGRegionMismatch(t *testing.T) {
	t.Parallel()
	suite := newUpdateSuite(t)
	tgID := domain.ResourceID(ids.NewID(ids.PrefixTargetGroup))
	tg := &kachorepo.TargetGroupRecord{
		TargetGroup: domain.TargetGroup{
			ID:        tgID,
			ProjectID: suite.listener.ProjectID,
			RegionID:  "ru-central2", // different region
			Name:      domain.LbName("other-region-tg"),
			Status:    domain.TargetGroupStatusActive,
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	suite.repo.seedTG(tg)

	_, err := suite.uc.Run(contextWithSubject("user:test-actor"), &lbv1.UpdateListenerRequest{
		ListenerId:           string(suite.listener.ID),
		UpdateMask:           &fieldmaskpb.FieldMask{Paths: []string{"default_target_group_id"}},
		DefaultTargetGroupId: string(tgID),
	})
	require.Error(t, err)
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	require.Contains(t, err.Error(), "default target group region")
	require.Contains(t, err.Error(), "does not match listener region")
}

// TestUpdateListener_DefaultTGSameRegion — same-region TG accepted.
func TestUpdateListener_DefaultTGSameRegion(t *testing.T) {
	t.Parallel()
	suite := newUpdateSuite(t)
	tgID := domain.ResourceID(ids.NewID(ids.PrefixTargetGroup))
	tg := &kachorepo.TargetGroupRecord{
		TargetGroup: domain.TargetGroup{
			ID:        tgID,
			ProjectID: suite.listener.ProjectID,
			RegionID:  suite.listener.RegionID,
			Name:      domain.LbName("same-region-tg"),
			Status:    domain.TargetGroupStatusActive,
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	suite.repo.seedTG(tg)
	op, err := suite.uc.Run(contextWithSubject("user:test-actor"), &lbv1.UpdateListenerRequest{
		ListenerId:           string(suite.listener.ID),
		UpdateMask:           &fieldmaskpb.FieldMask{Paths: []string{"default_target_group_id"}},
		DefaultTargetGroupId: string(tgID),
	})
	require.NoError(t, err)
	done := awaitOpDone(t, suite.ops, op.ID, time.Second)
	require.Nil(t, done.Error)

	got := suite.getListener(string(suite.listener.ID))
	v, ok := got.DefaultTargetGroupID.Maybe()
	require.True(t, ok)
	require.Equal(t, tgID, v)
}

// TestUpdateListener_ClearDefaultTG — passing empty string in mask → clear.
func TestUpdateListener_ClearDefaultTG(t *testing.T) {
	t.Parallel()
	suite := newUpdateSuite(t)
	// Pre-set default TG.
	suite.listener.DefaultTargetGroupID = option.MustNewOption(domain.ResourceID("tgrPREEXIST00000001"))
	suite.repo.seedListener(suite.listener)

	op, err := suite.uc.Run(context.Background(), &lbv1.UpdateListenerRequest{
		ListenerId:           string(suite.listener.ID),
		UpdateMask:           &fieldmaskpb.FieldMask{Paths: []string{"default_target_group_id"}},
		DefaultTargetGroupId: "",
	})
	require.NoError(t, err)
	done := awaitOpDone(t, suite.ops, op.ID, time.Second)
	require.Nil(t, done.Error)
	got := suite.getListener(string(suite.listener.ID))
	require.True(t, got.DefaultTargetGroupID.IsNone())
}

// TestUpdateListener_InvalidNameRegex — invalid name → InvalidArgument sync.
func TestUpdateListener_InvalidNameRegex(t *testing.T) {
	t.Parallel()
	suite := newUpdateSuite(t)
	_, err := suite.uc.Run(context.Background(), &lbv1.UpdateListenerRequest{
		ListenerId: string(suite.listener.ID),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"name"}},
		Name:       "Bad_Name",
	})
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

// ---- shared helpers ----

type updateSuite struct {
	t        *testing.T
	repo     *fakeRepo
	ops      *fakeOpsRepo
	listener *kachorepo.ListenerRecord
	uc       *UpdateUseCase
}

func newUpdateSuiteWith(t *testing.T, withDecider bool) *updateSuite {
	t.Helper()
	repo := newFakeRepo()
	lb := newRecordLB(t, "prj01TESTPROJ0000001", "ru-central1", domain.LBTypeExternal, "parent-lb")
	repo.seedLB(lb)
	listener := &kachorepo.ListenerRecord{
		Listener: domain.Listener{
			ID:             domain.ResourceID(ids.NewID(ids.PrefixListener)),
			ProjectID:      lb.ProjectID,
			LoadBalancerID: lb.ID,
			RegionID:       lb.RegionID,
			Name:           domain.LbName("initial"),
			Description:    "initial",
			Labels:         domain.LbLabels{},
			Protocol:       domain.ProtoTCP,
			Port:           80,
			Status:         domain.ListenerStatusActive,
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	repo.seedListener(listener)
	ops := newFakeOpsRepo()
	// Решатель доступа — ЯВНЫЙ разрешающий двойник, а не nil: nil означает «звена
	// решения нет», и с некоторых пор это отказ (`shared.AuthorizeObject`), а не
	// пропуск. Посадка БЕЗ решателя собирается флагом ниже.
	uc := NewUpdateUseCase(repo, ops, slog.Default())
	if withDecider {
		uc.WithCheckClient(&fakeTGCheckClient{allowed: true})
	}
	return &updateSuite{t: t, repo: repo, ops: ops, listener: listener, uc: uc}
}

// newUpdateSuite — обычная посадка: решатель подключён и разрешает.
func newUpdateSuite(t *testing.T) *updateSuite { return newUpdateSuiteWith(t, true) }

// newUpdateSuiteNoDecider — посадка без решателя доступа (пробы fail-closed).
func newUpdateSuiteNoDecider(t *testing.T) *updateSuite { return newUpdateSuiteWith(t, false) }

func (s *updateSuite) getListener(id string) *kachorepo.ListenerRecord {
	s.repo.mu.Lock()
	defer s.repo.mu.Unlock()
	c := *s.repo.listeners[id]
	return &c
}
