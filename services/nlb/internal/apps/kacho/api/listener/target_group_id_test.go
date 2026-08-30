// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package listener

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// seedListenerTG — seed a region-coherent TargetGroup so a listener may wire it
// (NLB-1b MIGRATE: Listener.Create prechecks the targetGroupId existence +
// region-coherence; the direct FK is the atomic backstop).
func seedListenerTG(repo *fakeRepo, id domain.ResourceID, projectID domain.ProjectID, regionID domain.RegionID) {
	repo.seedTG(&kachorepo.TargetGroupRecord{
		TargetGroup: domain.TargetGroup{
			ID: id, ProjectID: projectID, RegionID: regionID,
			Name: domain.LbName("wired-tg-" + string(id)), Status: domain.TargetGroupStatusActive, Port: 8080,
		},
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	})
}

// NLB-1-23 (F4, MIGRATE): Listener.Create wiring a targetGroupId that does not
// resolve is rejected synchronously with an actionable FAILED_PRECONDITION — the
// direct FK (0018) is the atomic backstop, this precheck gives the guidance.
func TestCreateListener_NLB_1_23_WireNonexistentTG_Actionable(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	ops := newFakeOpsRepo()
	lb := seedParentLB(t, repo)
	uc := newCreateUC(repo, ops)

	_, err := uc.Run(contextWithSubject("user:test-actor"), &lbv1.CreateListenerRequest{
		LoadBalancerId: string(lb.ID),
		Name:           "tcp-443",
		Protocol:       lbv1.Listener_TCP,
		Port:           443,
		TargetGroupId:  "tgr-doesnotexist0001",
	})
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	require.Contains(t, status.Convert(err).Message(), "create the TargetGroup first")
}

// NLB-1-23 (region-coherence): wiring a TG in a different region than the parent LB
// → FAILED_PRECONDITION (TG/LB must be region-coherent).
func TestCreateListener_NLB_1_23_WireCrossRegionTG_Rejected(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	ops := newFakeOpsRepo()
	lb := seedParentLB(t, repo)
	tgID := domain.ResourceID("tgr-crossregion00001")
	seedListenerTG(repo, tgID, lb.ProjectID, "other-region")
	uc := newCreateUC(repo, ops)

	_, err := uc.Run(contextWithSubject("user:test-actor"), &lbv1.CreateListenerRequest{
		LoadBalancerId: string(lb.ID),
		Name:           "tcp-443",
		Protocol:       lbv1.Listener_TCP,
		Port:           443,
		TargetGroupId:  string(tgID),
	})
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	require.Contains(t, status.Convert(err).Message(), "does not match")
}

// NLB-1b EXPAND (additive): Create with the new target_group_id wires the listener's
// TG reference (maps to the same DefaultTargetGroupID as the legacy field).
func TestCreateListener_NLB_1b_TargetGroupId(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	ops := newFakeOpsRepo()
	lb := seedParentLB(t, repo)
	seedListenerTG(repo, "tgr-wired00000000001", lb.ProjectID, lb.RegionID)
	uc := newCreateUC(repo, ops)

	op, err := uc.Run(contextWithSubject("user:test-actor"), &lbv1.CreateListenerRequest{
		LoadBalancerId: string(lb.ID),
		Name:           "tcp-443",
		Protocol:       lbv1.Listener_TCP,
		Port:           443,
		TargetGroupId:  "tgr-wired00000000001",
	})
	require.NoError(t, err)
	require.Nil(t, awaitOpDone(t, ops, op.ID, testTimeout).Error)

	got := listenerByLB(repo, string(lb.ID))
	require.Len(t, got, 1)
	v, ok := got[0].DefaultTargetGroupID.Maybe()
	require.True(t, ok)
	require.Equal(t, domain.ResourceID("tgr-wired00000000001"), v)
}

// ЗАМЕНА, А НЕ СНЯТИЕ. Здесь стояла проба приоритета: «когда присланы оба поля,
// побеждает target_group_id». Её предикат ИСЧЕЗ вместе с предметом — второго
// входного поля больше нет, приоритету не между чем возникать (задача продукта
// #1596). Оставить пробу как есть значило бы утверждать несуществующее; просто
// снять — потерять сценарий «клиент прислал оба», который как раз и стоил дорого.
// Поэтому проба утверждает НОВОЕ свойство того же сценария: оба поля в теле дают
// ЯВНЫЙ отказ, а не молчаливое разрешение в пользу одного из них.
func TestCreateListener_BothTargetGroupInputs_RejectedNotSilentlyResolved(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	ops := newFakeOpsRepo()
	lb := seedParentLB(t, repo)
	seedListenerTG(repo, "tgr-new0000000000001", lb.ProjectID, lb.RegionID)
	seedListenerTG(repo, "tgr-legacy0000000001", lb.ProjectID, lb.RegionID)
	uc := newCreateUC(repo, ops)

	_, err := uc.Run(contextWithSubject("user:test-actor"), &lbv1.CreateListenerRequest{
		LoadBalancerId:       string(lb.ID),
		Name:                 "tcp-443",
		Protocol:             lbv1.Listener_TCP,
		Port:                 443,
		TargetGroupId:        "tgr-new0000000000001",
		DefaultTargetGroupId: "tgr-legacy0000000001",
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err),
		"молчаливый выбор одного из двух присланных значений — тот самый дефект")
	require.Empty(t, listenerByLB(repo, string(lb.ID)),
		"отказ обязан наступать до записи")
}

// NLB-1-22 (F4, EXPAND): target_group_id is LIVE-mutable — repoint the listener to
// another region-coherent TG via Update.
func TestUpdateListener_NLB_1_22_RepointTargetGroupId(t *testing.T) {
	t.Parallel()
	suite := newUpdateSuite(t)
	tgID := domain.ResourceID(ids.NewID(ids.PrefixTargetGroup))
	suite.repo.seedTG(&kachorepo.TargetGroupRecord{
		TargetGroup: domain.TargetGroup{
			ID:        tgID,
			ProjectID: suite.listener.ProjectID,
			RegionID:  suite.listener.RegionID,
			Name:      domain.LbName("repoint-tg"),
			Status:    domain.TargetGroupStatusActive,
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	})

	op, err := suite.uc.Run(contextWithSubject("user:test-actor"), &lbv1.UpdateListenerRequest{
		ListenerId:    string(suite.listener.ID),
		UpdateMask:    &fieldmaskpb.FieldMask{Paths: []string{"target_group_id"}},
		TargetGroupId: string(tgID),
	})
	require.NoError(t, err)
	require.Nil(t, awaitOpDone(t, suite.ops, op.ID, time.Second).Error)

	got := suite.getListener(string(suite.listener.ID))
	v, ok := got.DefaultTargetGroupID.Maybe()
	require.True(t, ok)
	require.Equal(t, tgID, v)
}
