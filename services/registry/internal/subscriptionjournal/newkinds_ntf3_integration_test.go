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
	"github.com/PRO-Robotech/corelib/outbox"
	"github.com/PRO-Robotech/corelib/subscription"
	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
	"github.com/PRO-Robotech/kacho/services/registry/internal/subscriptionjournal"
)

// emitRow пишет строку журнала реестра в транзакции помощника — так же, как
// пишет её писатель модуля (инициатор — принципал контекста).
func (s *stand) emitRow(t *testing.T, kind, id, projectID, change string, payload map[string]any) {
	t.Helper()
	ctx := journalPrincipalCtx(context.Background())
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

func openStream(t *testing.T, s *stand, ctx context.Context, project string, kinds ...string) subscriptionv1.InternalSubscriptionService_SubscribeClient {
	t.Helper()
	stream, err := s.client.Subscribe(ctx, &subscriptionv1.SubscriptionRequest{Kinds: kinds, ProjectId: project, Start: begin()})
	if err != nil {
		t.Fatalf("подписка не открылась: %v", err)
	}
	return stream
}

// TestRegistryJournal_UK313b_RepositoryDeletionCarriesNoName — УК3-13 (б),
// CX3B-30: снятие репозитория доезжает событием `DELETED` с пустым `name` ПО
// ОБЪЯВЛЕНИЮ (`NameFormNone`), даже когда в нагрузке строки ключ имени есть:
// имя репозитория — не DNS-метка, и на событии его нет.
func TestRegistryJournal_UK313b_RepositoryDeletionCarriesNoName(t *testing.T) {
	s := newStand(t)
	repoID := ids.NewID(ids.PrefixRegistry) + "/app"
	s.emitRow(t, "Repository", repoID, probeProject, "DELETED", map[string]any{"id": repoID, "name": "app"})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ev := recv(t, openStream(t, s, ctx, probeProject, domain.FGAObjectTypeRepository))
	if ev.ResourceId != repoID || ev.Change != subscriptionv1.SubscriptionEvent_DELETED {
		t.Fatalf("пришло %q род %v, ожидалось DELETED %q", ev.ResourceId, ev.Change, repoID)
	}
	if ev.Name != "" {
		t.Errorf("DELETED Repository несёт name %q — у вида NameFormNone имя снятия пусто по объявлению", ev.Name)
	}
}

// TestRegistryJournal_UK313b_RegistryDeletionCarriesTheName — близнец УК3-13 (б):
// снятие РЕЕСТРА настоящим репозиторием (строку пишет функция базы
// `registries_journal_emit`) доезжает событием `DELETED` с именем реестра.
// Отличие от пробы выше одно: вид (NameFormDNS против NameFormNone).
func TestRegistryJournal_UK313b_RegistryDeletionCarriesTheName(t *testing.T) {
	s := newStand(t)
	reg := s.create(t, probeProject, "reg-del", nil)
	ctx0 := journalPrincipalCtx(context.Background())
	if _, err := s.repo.MarkDeleting(ctx0, reg.ID); err != nil {
		t.Fatalf("фикстура: перевод в DELETING не прошёл: %v", err)
	}
	if err := s.repo.Delete(ctx0, reg.ID, domain.UnregisterIntentForDelete(reg.ID, probeProject)); err != nil {
		t.Fatalf("фикстура: удаление не прошло: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stream := openStream(t, s, ctx, probeProject, domain.FGAObjectTypeRegistry)
	var removal *subscriptionv1.SubscriptionEvent
	for i := 0; i < 3 && removal == nil; i++ {
		if ev := recv(t, stream); ev.Change == subscriptionv1.SubscriptionEvent_DELETED {
			removal = ev
		}
	}
	if removal == nil {
		t.Fatal("снятие реестра не доехало за три события")
	}
	if removal.Name != "reg-del" {
		t.Errorf("DELETED Registry: name = %q, ожидалось \"reg-del\" (снимок из OLD строки)", removal.Name)
	}
}

// TestRegistryJournal_NTF361_RepositoryCreationReachesItsViewer — NTF3-61
// (половина registry): создание репозитория `reg-1/app` доезжает событием
// `CREATED` вида `registry_repository` с id `reg-1/app` до подписчика с `v_get`.
func TestRegistryJournal_NTF361_RepositoryCreationReachesItsViewer(t *testing.T) {
	repoID := ids.NewID(ids.PrefixRegistry) + "/app"
	s := newStandWithNarrower(t, narrowtest.Allowing(probeProject, repoID))
	s.emitRow(t, "Repository", repoID, probeProject, "CREATED", map[string]any{"id": repoID})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ev := recv(t, openStream(t, s, ctx, probeProject, domain.FGAObjectTypeRepository))
	if ev.ResourceId != repoID || ev.Change != subscriptionv1.SubscriptionEvent_CREATED {
		t.Fatalf("пришло %q род %v, ожидалось CREATED %q", ev.ResourceId, ev.Change, repoID)
	}
	if ev.Kind != domain.FGAObjectTypeRepository {
		t.Errorf("вид на проводе %q, ожидался %q", ev.Kind, domain.FGAObjectTypeRepository)
	}
}

// TestRegistryJournal_NTF361_RepositoryIsWithheldFromASubscriberWithoutVGet —
// близнец NTF3-61: без `v_get` на репозиторий его событие не приходит; живость
// потока доказывает видимое событие реестра, записанное следом.
func TestRegistryJournal_NTF361_RepositoryIsWithheldFromASubscriberWithoutVGet(t *testing.T) {
	repoID := ids.NewID(ids.PrefixRegistry) + "/app"
	reg := newReg(probeProject, "visible", nil)
	visible := reg.ID
	s := newStandWithNarrower(t, narrowtest.Allowing(visible))
	s.emitRow(t, "Repository", repoID, probeProject, "CREATED", map[string]any{"id": repoID})
	if _, _, err := s.repo.Insert(journalPrincipalCtx(context.Background()), reg,
		domain.RegisterIntentForCreate(reg, "user", "usr-alice")); err != nil {
		t.Fatalf("фикстура: видимый реестр не создан: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ev := recv(t, openStream(t, s, ctx, "", domain.FGAObjectTypeRepository, domain.FGAObjectTypeRegistry))
	if ev.ResourceId == repoID {
		t.Fatalf("подписчик без v_get получил событие репозитория %s", repoID)
	}
	if ev.ResourceId != visible {
		t.Fatalf("первым пришло %q, ожидалось видимое событие реестра %q", ev.ResourceId, visible)
	}
}

// TestRegistryJournal_UK313a_EmptyAnchorOfAProjectKindIsRefusedByName — УК3-13
// (а): функция фундамента с журналом реестра отказывает проектному виду с
// пустым якорем по имени вида; близнец с якорем ложится.
func TestRegistryJournal_UK313a_EmptyAnchorOfAProjectKindIsRefusedByName(t *testing.T) {
	s := newStand(t)
	j := subscriptionjournal.Journal(probeEndpointBase, false)
	for _, word := range []string{subscriptionjournal.JournalWordRegistry, "Repository"} {
		t.Run(word, func(t *testing.T) {
			id := ids.NewID(ids.PrefixRegistry)
			bare := emitViaFoundation(t, s, j, subscription.Entry{Kind: word, ID: id, Change: "CREATED",
				Payload: map[string]any{"id": id}})
			wantText := "пустой якорь у проектного вида " + word
			if !errors.Is(bare, subscription.ErrEntryRefused) || !strings.Contains(bare.Error(), wantText) {
				t.Errorf("пустой якорь: ожидался отказ %q, получено %v", wantText, bare)
			}
			if err := emitViaFoundation(t, s, j, subscription.Entry{Kind: word, ID: id, ProjectID: probeProject,
				Change: "CREATED", Payload: map[string]any{"id": id}}); err != nil {
				t.Errorf("близнец с якорем отвергнут: %v", err)
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
