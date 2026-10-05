// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionjournal_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	subscriptionv1 "github.com/PRO-Robotech/corelib/api/corelib/subscription"
	"github.com/PRO-Robotech/corelib/ids"
	"github.com/PRO-Robotech/corelib/journaltx"
	"github.com/PRO-Robotech/corelib/listnarrow/narrowtest"
	"github.com/PRO-Robotech/corelib/operations"
	"github.com/PRO-Robotech/corelib/outbox"
	"github.com/PRO-Robotech/corelib/subscription"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/authzfilter"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/helpers"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/subscriptionjournal"
)

// emitAs пишет строку журнала тем же способом, что писатель модуля, в
// транзакции помощника с ЗАДАННЫМ принципалом — проба знает, какого инициатора
// ждать на событии.
func (s *stand) emitAs(t *testing.T, ctx context.Context, kind, id, projectID, change string, payload map[string]any) {
	t.Helper()
	tx, err := journaltx.Begin(ctx, s.pool, journaltx.NewOptions(false))
	if err != nil {
		t.Fatalf("фикстура: транзакция помощника не открылась: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := outbox.EmitAnchored(ctx, tx, subscriptionjournal.Table, kind, id, projectID, change, payload); err != nil {
		t.Fatalf("фикстура: строка журнала не записалась: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("фикстура: транзакция не зафиксировалась: %v", err)
	}
}

// TestVpcJournal_NTF359_DeletionCarriesTheNameSnapshot — NTF3-59: снятие сети
// `net-a` доезжает событием `DELETED` с `name = "net-a"`, инициатором
// `user:<usr-A>` и причиной отсутствия состояния `NOT_PRODUCED`.
//
// Близнец в той же пробе: событие `CREATED` той же сети несёт состояние, а
// `name` на нём пуст — имя живёт внутри состояния (одно значение — одним
// способом). Отличие строк одно: род изменения.
func TestVpcJournal_NTF359_DeletionCarriesTheNameSnapshot(t *testing.T) {
	s := newStand(t)
	usrA := ids.NewHyphenID(ids.PrefixUser)
	actx := operations.WithPrincipal(context.Background(), operations.Principal{Type: "user", ID: usrA})

	n := network(probeNetwork, probeProject, "net-a")
	s.emitAs(t, actx, subscriptionjournal.KindNetwork, n.ID, n.ProjectID, "CREATED", networkPayload(n))
	s.emitAs(t, actx, subscriptionjournal.KindNetwork, n.ID, n.ProjectID, "DELETED",
		map[string]any{"id": n.ID, subscription.NamePayloadKey: "net-a"})

	ctx, cancel := context.WithTimeout(journalPrincipalCtx(context.Background()), 20*time.Second)
	defer cancel()
	stream := subscribe(t, s, ctx, &subscriptionv1.SubscriptionRequest{
		Kinds:     []string{authzfilter.ResourceTypeNetwork},
		ProjectId: probeProject,
	})

	created := recv(t, stream)
	if created.Change != subscriptionv1.SubscriptionEvent_CREATED {
		t.Fatalf("первое событие %v, ожидалось создание", created.Change)
	}
	if created.GetState() == nil {
		t.Error("близнец: CREATED без состояния")
	}
	if created.Name != "" {
		t.Errorf("близнец: CREATED несёт name %q — имя обязано жить только в состоянии", created.Name)
	}

	removed := recv(t, stream)
	if removed.Change != subscriptionv1.SubscriptionEvent_DELETED {
		t.Fatalf("второе событие %v, ожидалось снятие", removed.Change)
	}
	if removed.Name != "net-a" {
		t.Errorf("DELETED net-a: name = %q, ожидалось \"net-a\"", removed.Name)
	}
	if want := "user:" + usrA; removed.Initiator != want {
		t.Errorf("DELETED net-a: initiator = %q, ожидался %q", removed.Initiator, want)
	}
	if r := removed.GetStateUnavailable().GetReason(); r != subscriptionv1.SubscriptionEvent_StateUnavailable_NOT_PRODUCED {
		t.Errorf("DELETED net-a: state_unavailable.reason = %v, ожидалось NOT_PRODUCED", r)
	}
}

func addressPoolPayload(id string) map[string]any {
	return helpers.AddressPoolDomainPayload(&domain.AddressPool{
		ID: id, Name: domain.RcNameVPC("apl-1"), Kind: domain.AddressPoolKindExternalPublic,
		V4CIDRBlocks: []string{"198.51.100.0/24"},
	})
}

// TestVpcJournal_NTF361_AddressPoolCreationReachesTheClusterAdmin — NTF3-61
// (половина vpc): создание пула `apl-1` доезжает событием `CREATED` до
// подписчика-администратора кластера с `v_get` на пул; якоря у события нет.
func TestVpcJournal_NTF361_AddressPoolCreationReachesTheClusterAdmin(t *testing.T) {
	apl := ids.NewHyphenID("apl")
	s := newStandWithNarrower(t, narrowtest.Allowing(apl))
	s.emitAs(t, journalPrincipalCtx(context.Background()), "AddressPool", apl, helpers.NoProjectAnchor, "CREATED",
		addressPoolPayload(apl))

	ctx, cancel := context.WithTimeout(journalPrincipalCtx(context.Background()), 20*time.Second)
	defer cancel()
	stream := subscribe(t, s, ctx, &subscriptionv1.SubscriptionRequest{Kinds: []string{addressPoolObjectType}})
	ev := recv(t, stream)
	if ev.ResourceId != apl || ev.Change != subscriptionv1.SubscriptionEvent_CREATED {
		t.Fatalf("пришло %q род %v, ожидалось CREATED %q", ev.ResourceId, ev.Change, apl)
	}
	if ev.Kind != addressPoolObjectType {
		t.Errorf("вид на проводе %q, ожидался %q", ev.Kind, addressPoolObjectType)
	}
	if ev.ProjectId != "" {
		t.Errorf("у пула уровня кластера якорь %q", ev.ProjectId)
	}
}

// TestVpcJournal_NTF361_AddressPoolIsWithheldFromASubscriberWithoutVGet —
// близнец NTF3-61: подписчик без `v_get` на пул (`usr-X`) его события не
// получает. Единственный изменённый факт — сужатель; живость потока доказывает
// видимое событие сети, записанное следом.
func TestVpcJournal_NTF361_AddressPoolIsWithheldFromASubscriberWithoutVGet(t *testing.T) {
	apl := ids.NewHyphenID("apl")
	s := newStandWithNarrower(t, narrowtest.Allowing(probeProject, probeNetwork))
	s.emitAs(t, journalPrincipalCtx(context.Background()), "AddressPool", apl, helpers.NoProjectAnchor, "CREATED",
		addressPoolPayload(apl))
	n := network(probeNetwork, probeProject, "visible")
	s.emit(t, subscriptionjournal.KindNetwork, n.ID, n.ProjectID, "CREATED", networkPayload(n))

	ctx, cancel := context.WithTimeout(journalPrincipalCtx(context.Background()), 20*time.Second)
	defer cancel()
	stream := subscribe(t, s, ctx, &subscriptionv1.SubscriptionRequest{
		Kinds: []string{addressPoolObjectType, authzfilter.ResourceTypeNetwork},
	})
	ev := recv(t, stream)
	if ev.ResourceId == apl {
		t.Fatalf("подписчик без v_get получил событие пула %s", apl)
	}
	if ev.ResourceId != probeNetwork {
		t.Fatalf("первым пришло %q, ожидалось видимое событие сети %q", ev.ResourceId, probeNetwork)
	}
}

// TestVpcJournal_UK313a_AnchorFollowsTheKindsScope — УК3-13 (а), CX3B-31:
// функция фундамента с журналом vpc отказывает проектному виду с пустым якорем
// по ИМЕНИ вида и принимает пул уровня кластера без якоря.
//
// Близнецы: проектный вид с якорем ложится; пул с якорем отвергается.
func TestVpcJournal_UK313a_AnchorFollowsTheKindsScope(t *testing.T) {
	s := newStand(t)
	j := subscriptionjournal.Journal()
	want := map[string]bool{ // вид → проектный
		subscriptionjournal.KindNetwork: true, subscriptionjournal.KindSubnet: true,
		subscriptionjournal.KindSecurityGroup: true, subscriptionjournal.KindRouteTable: true,
		subscriptionjournal.KindAddress: true, subscriptionjournal.KindGateway: true,
		subscriptionjournal.KindNetworkInterface: true, subscriptionjournal.KindCidrGroup: true,
		"AddressPool": false,
	}
	for word, project := range want {
		t.Run(word, func(t *testing.T) {
			id := ids.NewHyphenID("uk3")
			bare := emitViaFoundation(t, s, j, subscription.Entry{Kind: word, ID: id, Change: "CREATED",
				Payload: map[string]any{"id": id}})
			anchored := emitViaFoundation(t, s, j, subscription.Entry{Kind: word, ID: id, ProjectID: probeProject,
				Change: "CREATED", Payload: map[string]any{"id": id}})
			if project {
				wantText := "пустой якорь у проектного вида " + word
				if !errors.Is(bare, subscription.ErrEntryRefused) || !strings.Contains(bare.Error(), wantText) {
					t.Errorf("пустой якорь: ожидался отказ %q, получено %v", wantText, bare)
				}
				if anchored != nil {
					t.Errorf("близнец с якорем отвергнут: %v", anchored)
				}
				return
			}
			if bare != nil {
				t.Errorf("вид уровня кластера без якоря отвергнут: %v", bare)
			}
			wantText := "якорь у вида уровня кластера " + word
			if !errors.Is(anchored, subscription.ErrEntryRefused) || !strings.Contains(anchored.Error(), wantText) {
				t.Errorf("якорь у пула: ожидался отказ %q, получено %v", wantText, anchored)
			}
		})
	}
}

func emitViaFoundation(t *testing.T, s *stand, j subscription.Journal, e subscription.Entry) error {
	t.Helper()
	ctx := journalPrincipalCtx(context.Background())
	tx, err := journaltx.Begin(ctx, s.pool, journaltx.NewOptions(false))
	if err != nil {
		t.Fatalf("фикстура: транзакция помощника не открылась: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := j.Emit(ctx, tx, e); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("фикстура: транзакция не зафиксировалась: %v", err)
	}
	return nil
}
