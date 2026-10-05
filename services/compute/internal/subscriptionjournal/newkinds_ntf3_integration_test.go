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
	"github.com/PRO-Robotech/corelib/subscription"
	"github.com/PRO-Robotech/kacho/services/compute/internal/authzfilter"
	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	"github.com/PRO-Robotech/kacho/services/compute/internal/repo"
	"github.com/PRO-Robotech/kacho/services/compute/internal/subscriptionjournal"
)

// seedPlacementGroupAndKey создаёт группу размещения и гостевой ключ НАСТОЯЩИМ
// репозиторием compute — тем же писателем, что и глагол создания. Строку журнала
// проба не пишет сама: предмет NTF3-61 — что глагол создания этих видов доходит
// до подписчика, а своя вставка доказала бы только словарь.
func seedPlacementGroupAndKey(t *testing.T, s *stand, projectID string) (plgID, gakID string) {
	t.Helper()
	// Строка учёта — то, что в продукте заводит материализация перед
	// писателем: «строки нет» — отдельный отказ, и без неё фикстура не создаётся.
	for _, kind := range []string{"compute.placementGroup", "compute.guestAccessKey"} {
		if _, err := s.pool.Exec(context.Background(),
			`INSERT INTO project_resource_quotas
			     (carrier_type, carrier_id, kind, used, limit_value,
			      source_scope, source_scope_id, limit_revision, account_id)
			 VALUES ('project', $1, $2, 0, 10, 'DEFAULT', '', 1, $3)`,
			projectID, kind, "acc-"+projectID); err != nil {
			t.Fatalf("фикстура: строка учёта %s не заведена: %v", kind, err)
		}
	}
	ctx := journalPrincipalCtx(context.Background())
	g, _, err := repo.NewPlacementGroupRepo(s.pool).Insert(ctx, &domain.PlacementGroup{
		ID:            ids.NewHyphenID("plg"),
		ProjectID:     projectID,
		Name:          "plg-1",
		Strategy:      domain.PlacementStrategySpread,
		PlacementType: domain.PlacementTypeZonal,
		ZoneID:        "ru-central1-a",
	})
	if err != nil {
		t.Fatalf("фикстура: группа размещения не создана: %v", err)
	}
	k, _, err := repo.NewGuestAccessKeyRepo(s.pool).Insert(ctx, &domain.GuestAccessKey{
		ID:          ids.NewHyphenID("gak"),
		ProjectID:   projectID,
		Name:        "gak-1",
		PublicKey:   "ssh-ed25519 AAAAgak-1",
		Fingerprint: "SHA256:gak-1",
	})
	if err != nil {
		t.Fatalf("фикстура: гостевой ключ не создан: %v", err)
	}
	return g.ID, k.ID
}

// TestComputeJournal_NTF361_NewKindsReachTheSubscriberWhoMaySeeThem — NTF3-61
// (половина compute): создание группы размещения и гостевого ключа доходит
// подписчику с `v_get` на них событием `CREATED`.
func TestComputeJournal_NTF361_NewKindsReachTheSubscriberWhoMaySeeThem(t *testing.T) {
	// Сужатель разрешает ровно предметы фикстуры: ид известен только после
	// создания, поэтому стенд поднимается на разрешающем всё сужателе, а
	// отрицательный близнец ниже стоит на своём стенде.
	s := newStand(t)
	plgID, gakID := seedPlacementGroupAndKey(t, s, probeProject)

	// Производитель: глагол создания пишет ровно одну строку журнала своего вида.
	for id, word := range map[string]string{plgID: "PlacementGroup", gakID: "GuestAccessKey"} {
		var n int
		if err := s.pool.QueryRow(context.Background(),
			`SELECT count(*) FROM compute_outbox WHERE resource_kind = $1 AND resource_id = $2 AND event_type = 'CREATED'`,
			word, id).Scan(&n); err != nil {
			t.Fatalf("журнал compute не прочитан: %v", err)
		}
		if n != 1 {
			t.Errorf("создание %s %s оставило строк журнала CREATED %d, ожидалась 1: писателя журнала у вида нет", word, id, n)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stream, err := s.client.Subscribe(ctx, &subscriptionv1.SubscriptionRequest{
		Kinds:     []string{authzfilter.ResourceTypePlacementGroup, authzfilter.ResourceTypeGuestAccessKey},
		ProjectId: probeProject,
		Start:     &subscriptionv1.SubscriptionRequest_Anchor{Anchor: subscriptionv1.SubscriptionAnchor_BEGINNING},
	})
	if err != nil {
		t.Fatalf("подписка на %s и %s не открылась: %v",
			authzfilter.ResourceTypePlacementGroup, authzfilter.ResourceTypeGuestAccessKey, err)
	}

	seen := map[string]*subscriptionv1.SubscriptionEvent{}
	for len(seen) < 2 {
		ev := recv(t, stream)
		seen[ev.ResourceId] = ev
	}
	for id, wantKind := range map[string]string{
		plgID: authzfilter.ResourceTypePlacementGroup,
		gakID: authzfilter.ResourceTypeGuestAccessKey,
	} {
		ev, ok := seen[id]
		if !ok {
			t.Errorf("создание %s (%s) не доехало до подписчика", id, wantKind)
			continue
		}
		if ev.Change != subscriptionv1.SubscriptionEvent_CREATED {
			t.Errorf("%s: род %v, ожидалось создание", id, ev.Change)
		}
		if ev.Kind != wantKind {
			t.Errorf("%s: вид на проводе %q, ожидался %q", id, ev.Kind, wantKind)
		}
		if ev.ProjectId != probeProject {
			t.Errorf("%s: якорь %q, ожидался %q", id, ev.ProjectId, probeProject)
		}
	}
}

// TestComputeJournal_NTF361_NewKindsAreWithheldFromASubscriberWithoutVGet —
// близнец NTF3-61: подписчик без `v_get` на группу и ключ их событий не получает.
//
// Единственный изменённый факт против положительной пробы — сужатель. Живость
// потока доказывает видимое событие машины, записанное ПОСЛЕ: пришло оно
// первым — значит поток дочитал журнал и именно отсеял чужие предметы.
func TestComputeJournal_NTF361_NewKindsAreWithheldFromASubscriberWithoutVGet(t *testing.T) {
	s := newStandWithNarrower(t, narrowtest.Allowing(probeMachine))
	plgID, gakID := seedPlacementGroupAndKey(t, s, probeProject)
	s.emit(t, subscriptionjournal.JournalWordInstance, probeMachine, probeProject, "CREATED",
		instancePayload(&domain.Instance{ID: probeMachine, ProjectID: probeProject, Name: "visible"}))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stream, err := s.client.Subscribe(ctx, &subscriptionv1.SubscriptionRequest{
		Kinds: []string{authzfilter.ResourceTypeInstance, authzfilter.ResourceTypePlacementGroup,
			authzfilter.ResourceTypeGuestAccessKey},
		Start: &subscriptionv1.SubscriptionRequest_Anchor{Anchor: subscriptionv1.SubscriptionAnchor_BEGINNING},
	})
	if err != nil {
		t.Fatalf("подписка не открылась: %v", err)
	}
	ev := recv(t, stream)
	if ev.ResourceId == plgID || ev.ResourceId == gakID {
		t.Fatalf("подписчик без v_get получил событие %s (%s)", ev.ResourceId, ev.Kind)
	}
	if ev.ResourceId != probeMachine {
		t.Fatalf("первым пришло %q, ожидалось видимое событие машины %q", ev.ResourceId, probeMachine)
	}
}

// TestComputeJournal_UK313a_EmptyAnchorOfAProjectKindIsRefusedByName — УК3-13 (а),
// CX3B-31: функция фундамента, пишущая строку проектного вида compute с пустым
// якорем, отказывает с ИМЕНЕМ вида, а не подставляет уровень кластера.
//
// Близнец в той же пробе — та же запись с якорем ложится. Отличие одно: якорь.
func TestComputeJournal_UK313a_EmptyAnchorOfAProjectKindIsRefusedByName(t *testing.T) {
	s := newStand(t)
	j := subscriptionjournal.Journal()
	if len(j.Mapping.Kinds) == 0 {
		t.Fatal("словарь видов пуст — проба судила бы пустоту")
	}
	for word := range j.Mapping.Kinds {
		t.Run(word, func(t *testing.T) {
			id := ids.NewHyphenID("uk3")
			err := emitViaFoundation(t, s, j, subscription.Entry{
				Kind: word, ID: id, ProjectID: "", Change: "CREATED", Payload: map[string]any{"id": id},
			})
			wantText := "пустой якорь у проектного вида " + word
			if !errors.Is(err, subscription.ErrEntryRefused) || !strings.Contains(err.Error(), wantText) {
				t.Errorf("пустой якорь: ожидался отказ %q, получено %v", wantText, err)
			}

			twin := ids.NewHyphenID("uk3")
			if err := emitViaFoundation(t, s, j, subscription.Entry{
				Kind: word, ID: twin, ProjectID: probeProject, Change: "CREATED", Payload: map[string]any{"id": twin},
			}); err != nil {
				t.Errorf("близнец с якорем отвергнут: %v", err)
			}
		})
	}
}

// emitViaFoundation пишет запись функцией фундамента в транзакции помощника и
// фиксирует её; ошибка Emit возвращается, транзакция при ней откатывается.
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
