// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// delete_finisher_test.go — начатое удаление обязано быть доведено до конца, даже
// если процесс, который его начал, умер.
//
// # Предмет
//
// Удаление ВМ — сага: сначала строка переводится в DELETING, затем у владельцев
// снимаются привязки интерфейсов и томов, и только последней удаляется сама
// строка. Порядок выбран так, что крах на любом шаге оставляет согласованное
// состояние — строка жива, значит привязки резолвятся и повтор доделает начатое.
//
// Повторять было НЕКОМУ. Разрешитель осиротевших операций по своему контракту
// рабочую функцию не перезапускает: он приводит статус операции в соответствие
// закоммиченной реальности — видит строку на месте и помечает операцию
// прерванной. После этого машина остаётся в DELETING навсегда, а её интерфейсы и
// тома — занятыми у владельцев, которые о случившемся не знают и узнать не могут:
// снятие привязки инициирует потребитель, владелец его не запрашивает.
//
// Наблюдаемое следствие: занятый том нельзя присоединить к другой машине, а
// занятый интерфейс удерживает адрес из ограниченного пула. Ошибок при этом нет
// ни у кого — удаление «прошло», просто не до конца.
//
// # Что здесь утверждается
//
// Не «добиватель вызвался», а ИСХОД: после его прохода привязки сняты у
// владельцев и строки машины нет. Утверждение о факте вызова осталось бы зелёным
// на добивателе, который зовёт снятие и игнорирует его отказ.
package instance

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	"github.com/PRO-Robotech/kacho/services/compute/internal/ports"
	"github.com/PRO-Robotech/kacho/services/compute/internal/ports/portmock"
)

// crashedDeleteFixture воспроизводит состояние ПОСЛЕ краха: машина уже переведена
// в DELETING, привязки ещё держатся у владельцев, строка на месте.
//
// Вход строится тем же переходом, которым его делает боевой путь
// (repo.MarkDeleting), а не подстановкой статуса руками: подставленный статус
// проверял бы выдумку, а не состояние, которое реально наблюдается.
func crashedDeleteFixture(t *testing.T) (*InstanceService, *fakeNicPeer, *fakeVolumePeer, string) {
	t.Helper()
	repo := portmock.NewInstanceRepo()
	id := seedInstance(repo, domain.InstanceStatusRunning).ID

	nics := &fakeNicPeer{attached: map[string][]string{id: {"nic-1", "nic-2"}}}
	vols := &fakeVolumePeer{attached: map[string][]string{id: {"vol-1"}}}

	svc := NewInstanceService(repo, nil, nil, nil, nil, nics, vols, nil)

	if _, err := repo.MarkDeleting(context.Background(), id); err != nil {
		t.Fatalf("предпосылка: перевод в DELETING обязан пройти: %v", err)
	}
	return svc, nics, vols, id
}

// TestFinishStuckDeletes_CompletesWhatTheCrashLeft — положительный случай.
func TestFinishStuckDeletes_CompletesWhatTheCrashLeft(t *testing.T) {
	svc, nics, vols, id := crashedDeleteFixture(t)
	ctx := context.Background()

	finished, err := svc.FinishStuckDeletes(ctx, 0)
	if err != nil {
		t.Fatalf("проход добивателя: %v", err)
	}
	if len(finished) != 1 || finished[0] != id {
		t.Fatalf("добиватель обязан назвать застрявшую машину, назвал %v", finished)
	}

	if got := nics.attached[id]; len(got) != 0 {
		t.Errorf("интерфейсы обязаны быть отвязаны у владельца, остались %v: "+
			"иначе они удерживают адрес из ограниченного пула навсегда", got)
	}
	if got := vols.attached[id]; len(got) != 0 {
		t.Errorf("тома обязаны быть отвязаны у владельца, остались %v: "+
			"иначе том нельзя присоединить ни к одной другой машине", got)
	}
	if _, err := svc.repo.Get(ctx, id); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("строка машины обязана быть удалена последней, получено %v", err)
	}
}

// TestFinishStuckDeletes_LeavesLiveInstanceAlone — отрицание в паре с
// положительным: машина, не входившая в удаление, добивателю не принадлежит.
//
// Без этой пробы «добиватель», сносящий всё подряд, оставался бы зелёным.
func TestFinishStuckDeletes_LeavesLiveInstanceAlone(t *testing.T) {
	repo := portmock.NewInstanceRepo()
	liveID := seedInstance(repo, domain.InstanceStatusRunning).ID
	nics := &fakeNicPeer{attached: map[string][]string{liveID: {"nic-9"}}}
	vols := &fakeVolumePeer{attached: map[string][]string{liveID: {"vol-9"}}}
	svc := NewInstanceService(repo, nil, nil, nil, nil, nics, vols, nil)

	finished, err := svc.FinishStuckDeletes(context.Background(), 0)
	if err != nil {
		t.Fatalf("проход добивателя: %v", err)
	}
	if len(finished) != 0 {
		t.Fatalf("живая машина добивателю не принадлежит, названо %v", finished)
	}
	if len(nics.attached[liveID]) != 1 || len(vols.attached[liveID]) != 1 {
		t.Errorf("привязки живой машины обязаны уцелеть: снятие отняло бы у работающей " +
			"машины её сеть и диски")
	}
	if _, err := svc.repo.Get(context.Background(), liveID); err != nil {
		t.Errorf("строка живой машины обязана уцелеть: %v", err)
	}
}

// TestFinishStuckDeletes_RespectsGrace — окно отсрочки. Свежее удаление прямо
// сейчас доделывает законный исполнитель; добиватель обязан его не трогать,
// иначе оба снимают одни и те же привязки наперегонки.
func TestFinishStuckDeletes_RespectsGrace(t *testing.T) {
	svc, nics, _, id := crashedDeleteFixture(t)
	ctx := context.Background()

	finished, err := svc.FinishStuckDeletes(ctx, time.Hour)
	if err != nil {
		t.Fatalf("проход добивателя: %v", err)
	}
	if len(finished) != 0 {
		t.Fatalf("удаление моложе отсрочки добивателю не принадлежит, названо %v", finished)
	}
	if len(nics.attached[id]) != 2 {
		t.Errorf("привязки свежего удаления трогать нельзя: их снимает законный исполнитель")
	}

	// Та же машина вне отсрочки — берётся. Пара доказывает, что отрицание выше про
	// ВОЗРАСТ, а не про то, что добиватель вообще ничего не находит.
	finished, err = svc.FinishStuckDeletes(ctx, 0)
	if err != nil {
		t.Fatalf("проход добивателя: %v", err)
	}
	if len(finished) != 1 {
		t.Fatalf("вне отсрочки машина обязана быть взята, названо %v", finished)
	}
}

// TestFinishStuckDeletes_PeerRefusalLeavesRowForRetry — владелец привязки
// недоступен ⇒ строку удалять НЕЛЬЗЯ.
//
// Удали её — и привязка осиротеет безвозвратно: списки привязок резолвятся ПО
// машине, и без её строки не останется ничего, по чему их можно найти. Ровно тот
// же довод, по которому строка удаляется последней.
func TestFinishStuckDeletes_PeerRefusalLeavesRowForRetry(t *testing.T) {
	svc, nics, _, id := crashedDeleteFixture(t)
	nics.detachErr = errors.New("peer unavailable")
	ctx := context.Background()

	if _, err := svc.FinishStuckDeletes(ctx, 0); err == nil {
		t.Fatal("отказ владельца привязки обязан быть виден вызывающему, а не проглочен")
	}
	if _, err := svc.repo.Get(ctx, id); err != nil {
		t.Errorf("строка обязана уцелеть до успешного снятия привязок: без неё "+
			"привязку не по чему найти и она осиротеет навсегда (%v)", err)
	}

	// Владелец вернулся — следующий проход доделывает. Добиватель обязан
	// самоисцеляться, а не выбывать на первом транзиентном отказе.
	nics.detachErr = nil
	finished, err := svc.FinishStuckDeletes(ctx, 0)
	if err != nil {
		t.Fatalf("повтор после возвращения владельца: %v", err)
	}
	if len(finished) != 1 {
		t.Fatalf("повтор обязан доделать начатое, названо %v", finished)
	}
	if _, err := svc.repo.Get(ctx, id); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("после успешного снятия строка обязана быть удалена, получено %v", err)
	}
}

// TestFinishStuckDeletes_IsIdempotent — повтор безопасен: второй проход не
// находит того, что уже доделано.
func TestFinishStuckDeletes_IsIdempotent(t *testing.T) {
	svc, _, _, _ := crashedDeleteFixture(t)
	ctx := context.Background()

	if _, err := svc.FinishStuckDeletes(ctx, 0); err != nil {
		t.Fatalf("первый проход: %v", err)
	}
	second, err := svc.FinishStuckDeletes(ctx, 0)
	if err != nil {
		t.Fatalf("второй проход: %v", err)
	}
	if len(second) != 0 {
		t.Fatalf("второй проход не вправе назвать уже доделанное, названо %v", second)
	}
}

// ---- фейки владельцев привязок ----
//
// Утверждается ИСХОД у владельца (привязка снята), а не факт вызова, поэтому
// фейки держат состояние, а не журнал обращений.

type fakeNicPeer struct {
	attached  map[string][]string
	detachErr error
}

func (f *fakeNicPeer) ListByInstance(_ context.Context, ids []string) ([]ports.NicAttachment, error) {
	var out []ports.NicAttachment
	for _, id := range ids {
		for _, nic := range f.attached[id] {
			out = append(out, ports.NicAttachment{NICID: nic, InstanceID: id})
		}
	}
	return out, nil
}

// Attach фейк не реализует по существу: добиватель его не зовёт, а порт требует
// полного набора. Возврат ошибки, а не nil: тихий успех на несуществующей
// реализации сделал бы зелёной пробу, случайно попавшую на этот путь.
func (f *fakeNicPeer) Attach(context.Context, ports.NicAttachSpec) (*ports.NicAttachment, error) {
	return nil, errors.New("fakeNicPeer.Attach не реализован: добиватель его не зовёт")
}

func (f *fakeNicPeer) Detach(_ context.Context, nicID, instanceID string) error {
	if f.detachErr != nil {
		return f.detachErr
	}
	kept := make([]string, 0, len(f.attached[instanceID]))
	for _, cur := range f.attached[instanceID] {
		if cur != nicID {
			kept = append(kept, cur)
		}
	}
	f.attached[instanceID] = kept
	return nil
}

type fakeVolumePeer struct {
	attached  map[string][]string
	detachErr error
}

func (f *fakeVolumePeer) ListAttachments(_ context.Context, ids []string) ([]ports.VolumeAttachmentInfo, error) {
	var out []ports.VolumeAttachmentInfo
	for _, id := range ids {
		for _, vol := range f.attached[id] {
			out = append(out, ports.VolumeAttachmentInfo{VolumeID: vol, InstanceID: id})
		}
	}
	return out, nil
}

// Attach — см. довод у fakeNicPeer.Attach.
func (f *fakeVolumePeer) Attach(context.Context, ports.VolumeAttachSpec) (*ports.VolumeAttachmentInfo, error) {
	return nil, errors.New("fakeVolumePeer.Attach не реализован: добиватель его не зовёт")
}

func (f *fakeVolumePeer) Detach(_ context.Context, volumeID, instanceID string) error {
	if f.detachErr != nil {
		return f.detachErr
	}
	kept := make([]string, 0, len(f.attached[instanceID]))
	for _, cur := range f.attached[instanceID] {
		if cur != volumeID {
			kept = append(kept, cur)
		}
	}
	f.attached[instanceID] = kept
	return nil
}
