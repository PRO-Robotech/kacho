// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionjournal_test

import (
	"context"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/subscription"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/volume"
	"github.com/PRO-Robotech/kacho/services/storage/internal/blockbackend"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/protoconv"
	"github.com/PRO-Robotech/kacho/services/storage/internal/reconciler"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/pg"
	"github.com/PRO-Robotech/kacho/services/storage/internal/subscriptionjournal"

	storagev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/storage/v1"
)

// source_state_integration_test.go — состояние предмета у ИСТОЧНИКОВ томов
// (`Snapshot`, `Image`) и производитель их событий (задача продукта #1556).
//
// Предмет здесь ДВОЙНОЙ, и обе половины обязаны утверждаться порознь:
//
//  1. состояние СОБИРАЕТСЯ и равно ответу чтения — иначе подписчик держит не тот
//     предмет, что отдаёт Get;
//  2. состояние остаётся СВЕЖИМ при мутации тома — иначе оно устаревает молча, и
//     это ровно та ложь, ради устранения которой состояние вводится. Именно этой
//     половины не было, и именно она была причиной, по которой #1552 состояние
//     этим двум видам не отдала.
//
// Проба первой половины без второй зеленела бы на реализации, у которой нет
// производителя событий вовсе: состояние собралось бы один раз, при создании
// снимка, и никогда бы не обновилось.

// stateOf собирает состояние из ПОСЛЕДНЕЙ строки журнала по предмету — тем же
// сборщиком владельца, каким его собирает поток.
func stateOf(t *testing.T, s *stand, kind, id string) *storagev1.Snapshot {
	t.Helper()
	packed := packedStateOf(t, s, kind, id)
	var got storagev1.Snapshot
	if err := packed.UnmarshalTo(&got); err != nil {
		t.Fatalf("упаковано не состояние снимка: %v", err)
	}
	return &got
}

func packedStateOf(t *testing.T, s *stand, kind, id string) interface {
	UnmarshalTo(proto.Message) error
} {
	t.Helper()
	rows := forID(journalSince(t, s, 0), id)
	if len(rows) == 0 {
		t.Fatalf("журнал не дал ни одной строки по %s %s — сравнивать не с чем", kind, id)
	}
	last := rows[len(rows)-1]
	packed, absence, err := subscriptionjournal.Journal().Mapping.State(subscription.Row{
		Kind: last.kind, ID: last.id, Change: last.change, Payload: last.payload,
	})
	if err != nil {
		t.Fatalf("сборка состояния из строки журнала отказала: %v", err)
	}
	if packed == nil {
		t.Fatalf("строка журнала по %s %s не дала состояния (причина %v): у этого вида "+
			"состояние не производится, и клиентский отбор по меткам по нему неисполним",
			kind, id, absence)
	}
	return packed
}

// probeRegion — регион зоны фикстур. Образ РЕГИОНАЛЕН (anycast), и его засев
// требует, чтобы зона тома принадлежала этому региону; связь «зона → её регион»
// разрешает владелец Geography и передаёт параметром — из имени зоны она не
// выводится.
const probeRegion = "region-1"

// seedVolumeFrom заводит том, засеянный НАЗВАННЫМ источником, боевым путём записи.
// Ровно один из двух идентификаторов непуст: домен требует одного источника.
func seedVolumeFrom(t *testing.T, s *stand, name, snapshotID, imageID string) *domain.Volume {
	t.Helper()
	v, _, err := pg.NewVolumeRepo(s.pool).Insert(context.Background(), &domain.Volume{
		ID:             ids.NewID(domain.PrefixVolume),
		ProjectID:      probeProject,
		Name:           name,
		ZoneID:         probeZone,
		DiskTypeID:     probeDiskType,
		SizeBytes:      1 << 30,
		SourceSnapshot: snapshotID,
		SourceImage:    imageID,
	}, probeRegion)
	if err != nil {
		t.Fatalf("том не засеялся боевым путём записи: %v", err)
	}
	return v
}

// createSnapshot заводит ГОТОВЫЙ снимок ТЕМ ЖЕ репозиторием, каким его заводит
// сервис: снимок снимается с тома, поэтому у фикстуры сперва появляется том.
//
// Готовность доводится тем же вызовом сверщика, каким она наступает на живом
// стенде: снимок в намерении томов не засевает, и проба, забывшая довести его,
// утверждала бы про состояние, которого не создавала.
func createSnapshot(t *testing.T, s *stand, name string) *domain.Snapshot {
	t.Helper()
	ctx := context.Background()
	src := s.createVolume(t, probeProject, name+"-src")
	confirmReady(t, s, reconciler.KindVolume, src.ID)
	sn, _, err := pg.NewSnapshotRepo(s.pool).Insert(ctx, &domain.Snapshot{
		ID:             ids.NewID(domain.PrefixSnapshot),
		ProjectID:      probeProject,
		Name:           name,
		ZoneID:         probeZone,
		SourceVolumeID: src.ID,
		SizeBytes:      1 << 30,
	})
	if err != nil {
		t.Fatalf("снимок не завёлся боевым путём записи: %v", err)
	}
	confirmReady(t, s, reconciler.KindSnapshot, sn.ID)
	return sn
}

// confirmReady доводит ресурс до готовности тем же вызовом, каким его подтверждает
// сверщик, увидев объект у бэкенда. Применение утверждается явно: подтверждение,
// не тронувшее ни одной строки, оставило бы ресурс в намерении, а фикстура
// выглядела бы исполненной.
func confirmReady(t *testing.T, s *stand, kind reconciler.Kind, id string) {
	t.Helper()
	ok, err := reconciler.NewStore(s.pool).Confirm(context.Background(), kind, id,
		blockbackend.Observed{State: blockbackend.ObservedReady, SizeBytes: 1 << 30})
	if err != nil || !ok {
		t.Fatalf("%s %s не доведён до готовности (ok=%v, err=%v)", kind, id, ok, err)
	}
}

// TestSnapshotStateEqualsWhatTheReadPathAnswers — состояние снимка из журнала и
// ответ чтения совпадают поле в поле, ВКЛЮЧАЯ перечень засеянных томов.
//
// Положительный контроль обязателен и здесь: у снимка без детей `used_by` пуст, и
// равенство выполнилось бы на двух пустотах — то есть проба молчала бы ровно о том
// поле, ради которого состояние этому виду и заводится.
func TestSnapshotStateEqualsWhatTheReadPathAnswers(t *testing.T) {
	s := newStand(t)
	ctx := context.Background()
	repo := pg.NewSnapshotRepo(s.pool)
	sn := createSnapshot(t, s, "snap-state-equals-read")

	// Ребёнок заводится боевым путём: том, засеянный этим снимком.
	seedVolumeFrom(t, s, "vol-seeded-by-snap", sn.ID, "")

	read, err := repo.Get(ctx, sn.ID)
	if err != nil {
		t.Fatalf("снимок не прочитался: %v", err)
	}
	want := protoconv.Snapshot(read)

	got := stateOf(t, s, "Snapshot", sn.ID)

	if len(want.UsedBy) != 1 {
		t.Fatalf("сторона чтения не наполнена (used_by %d) — равенство ниже зеленело бы "+
			"на двух пустотах, и перечень детей остался бы непроверенным", len(want.UsedBy))
	}
	if !proto.Equal(want, got) {
		t.Fatalf("состояние из журнала и ответ чтения РАЗОШЛИСЬ.\n  чтение: %v\n  журнал: %v",
			want, got)
	}
	t.Logf("совпало поле в поле: статус %v · меток %d · used_by %d",
		got.Status, len(got.Labels), len(got.UsedBy))
}

// TestSnapshotStateStaysFreshWhenAVolumeIsSeededAndRemoved — ВТОРАЯ половина, и
// именно она была причиной, по которой состояние этому виду не отдавали.
//
// `used_by` снимка — перечень ТОМОВ, засеянных им; строка снимка при этом не
// меняется. Без производителя событий по строке ТОМА подписчик держал бы снимок с
// пустым `used_by` при живых детях и получил бы отказ удаления, которого его
// собственное состояние не объясняет.
//
// Утверждаются ОБА перехода. Только появление — и проба зеленела бы на
// производителе, который умеет добавлять и не умеет снимать; только снятие — и она
// не заметила бы, что перечень не наполняется вовсе.
func TestSnapshotStateStaysFreshWhenAVolumeIsSeededAndRemoved(t *testing.T) {
	s := newStand(t)
	sn := createSnapshot(t, s, "snap-freshness")

	// ── исходное состояние: детей нет ──────────────────────────────────────────
	if got := stateOf(t, s, "Snapshot", sn.ID); len(got.UsedBy) != 0 {
		t.Fatalf("у снимка без засеянных томов used_by = %d, ожидался пустой", len(got.UsedBy))
	}

	// ── появление ребёнка обязано ДОЕХАТЬ до состояния снимка ─────────────────
	v := seedVolumeFrom(t, s, "vol-freshness", sn.ID, "")
	got := stateOf(t, s, "Snapshot", sn.ID)
	if len(got.UsedBy) != 1 || got.UsedBy[0].GetReferrer().GetId() != v.ID {
		t.Fatalf("том засеян, а состояние снимка его не знает (used_by %v): у входа нет "+
			"производителя событий, и состояние устаревает МОЛЧА — подписчик получил бы "+
			"отказ удаления, которого его состояние не объясняет", got.UsedBy)
	}

	// ── снятие ребёнка обязано доехать так же ─────────────────────────────────
	if _, err := s.pool.Exec(context.Background(),
		`DELETE FROM volumes WHERE id = $1`, v.ID); err != nil {
		t.Fatalf("том не снялся: %v", err)
	}
	if got := stateOf(t, s, "Snapshot", sn.ID); len(got.UsedBy) != 0 {
		t.Fatalf("том снят, а состояние снимка держит его (used_by %v): производитель "+
			"умеет добавлять и не умеет снимать — подписчик считал бы снимок занятым вечно",
			got.UsedBy)
	}
}

// TestVolumeUpdateDoesNotWakeItsSourceSubscribers — отрицательный полюс той же
// пары: правка тома, НЕ меняющая перечня, источнику события не эмитирует.
//
// Без него производитель, будящий источник на каждой мутации тома, был бы
// неотличим от верного: обе прежние пробы остались бы зелёными. Цена такого
// производителя — событие подписчикам снимка на переименовании чужого тома.
func TestVolumeUpdateDoesNotWakeItsSourceSubscribers(t *testing.T) {
	s := newStand(t)
	ctx := context.Background()
	sn := createSnapshot(t, s, "snap-no-spurious")
	v := seedVolumeFrom(t, s, "vol-no-spurious", sn.ID, "")

	from := mark(t, s)
	if _, _, err := pg.NewVolumeRepo(s.pool).Update(ctx, v.ID, volume.VolumeUpdate{
		LabelsSet: true, Labels: map[string]string{"env": "prod"},
	}); err != nil {
		t.Fatalf("метки не проставились: %v", err)
	}

	// Положительный контроль: событие ТОМУ пришло — значит правка состоялась и
	// журнал жив. Без него «ноль событий снимку» зеленело бы на несделанной правке.
	if n := len(forID(journalSince(t, s, from), v.ID)); n == 0 {
		t.Fatal("правка тома не дала события даже самому тому — журнал не наблюдает правку, " +
			"и утверждение ниже беспредметно")
	}
	if n := len(forID(journalSince(t, s, from), sn.ID)); n != 0 {
		t.Errorf("правка меток тома дала %d событий его СНИМКУ: перечень засеянных томов "+
			"она не меняет, и будить подписчиков снимка на переименовании чужого тома "+
			"значит платить за событие, которое ничего не сообщает", n)
	}
}

// TestImageStateEqualsWhatTheReadPathAnswers — то же для образа: у него свой
// перечень (`volumes.source_image_id`) и своя ветка сборки, и зелёное на снимке о
// нём не говорит ничего.
func TestImageStateEqualsWhatTheReadPathAnswers(t *testing.T) {
	s := newStand(t)
	ctx := context.Background()
	repo := pg.NewImageRepo(s.pool)
	img, _, err := repo.Insert(ctx, &domain.Image{
		ID:           ids.NewID(domain.PrefixImage),
		ProjectID:    probeProject,
		Name:         "img-state-equals-read",
		RegionID:     probeRegion,
		SizeBytes:    1 << 30,
		MinDiskBytes: 1 << 30,
	}, []string{probeZone})
	if err != nil {
		t.Fatalf("образ не завёлся боевым путём записи: %v", err)
	}
	confirmReady(t, s, reconciler.KindImage, img.ID)

	seedVolumeFrom(t, s, "vol-seeded-by-image", "", img.ID)

	read, err := repo.Get(ctx, img.ID)
	if err != nil {
		t.Fatalf("образ не прочитался: %v", err)
	}
	want := protoconv.Image(read)

	packed := packedStateOf(t, s, "Image", img.ID)
	var got storagev1.Image
	if err := packed.UnmarshalTo(&got); err != nil {
		t.Fatalf("упаковано не состояние образа: %v", err)
	}

	if len(want.UsedBy) != 1 {
		t.Fatalf("сторона чтения не наполнена (used_by %d) — равенство ниже зеленело бы "+
			"на двух пустотах", len(want.UsedBy))
	}
	if !proto.Equal(want, &got) {
		t.Fatalf("состояние из журнала и ответ чтения РАЗОШЛИСЬ.\n  чтение: %v\n  журнал: %v",
			want, &got)
	}
	t.Logf("совпало поле в поле: статус %v · размещение %v · used_by %d",
		got.Status, got.PlacementType, len(got.UsedBy))
}
