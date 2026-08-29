// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionjournal_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/PRO-Robotech/kacho/services/storage/internal/blockbackend"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/protoconv"
	"github.com/PRO-Robotech/kacho/services/storage/internal/reconciler"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/pg"

	storagev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/storage/v1"
)

// journalRow — строка журнала, как её видит читатель.
type journalRow struct {
	kind    string
	id      string
	project string
	change  string
	payload []byte
}

// journalSince читает журнал начиная с названного номера — тем же порядком, каким
// его читает поток.
func journalSince(t *testing.T, s *stand, from int64) []journalRow {
	t.Helper()
	rows, err := s.pool.Query(context.Background(), `
		SELECT sequence_no, resource_kind, resource_id, project_id, event_type, payload
		  FROM kacho_storage.storage_outbox
		 WHERE sequence_no > $1
		 ORDER BY sequence_no ASC`, from)
	if err != nil {
		t.Fatalf("журнал не прочитался: %v", err)
	}
	defer rows.Close()
	var out []journalRow
	for rows.Next() {
		var (
			seq int64
			r   journalRow
		)
		if err := rows.Scan(&seq, &r.kind, &r.id, &r.project, &r.change, &r.payload); err != nil {
			t.Fatalf("строка журнала не разобралась: %v", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("чтение журнала оборвалось: %v", err)
	}
	return out
}

// mark отдаёт текущий конец журнала, чтобы проба судила ТОЛЬКО то, что произошло
// после её собственного действия.
func mark(t *testing.T, s *stand) int64 {
	t.Helper()
	var seq int64
	if err := s.pool.QueryRow(context.Background(),
		`SELECT COALESCE(MAX(sequence_no), 0) FROM kacho_storage.storage_outbox`).Scan(&seq); err != nil {
		t.Fatalf("конец журнала не прочитался: %v", err)
	}
	return seq
}

// forID оставляет строки одного предмета.
func forID(rows []journalRow, id string) []journalRow {
	var out []journalRow
	for _, r := range rows {
		if r.id == id {
			out = append(out, r)
		}
	}
	return out
}

// TestReconcilerConfirmEmitsAnUpdate — переход `CREATING → READY`, совершаемый
// СВЕРЩИКОМ мимо репозиториев, попадает в журнал.
//
// Это тот самый переход, ради которого консоль опрашивает списки. Журнал,
// эмитирующий из репозиториев, о нём не узнал бы вовсе: `reconciler.Store.Confirm`
// пишет прямо на пуле.
func TestReconcilerConfirmEmitsAnUpdate(t *testing.T) {
	s := newStand(t)
	v := s.createVolume(t, probeProject, "confirmed")

	from := mark(t, s)
	ok, err := reconciler.NewStore(s.pool).Confirm(context.Background(), reconciler.KindVolume, v.ID,
		blockbackend.Observed{State: blockbackend.ObservedReady, SizeBytes: 1 << 30})
	if err != nil {
		t.Fatalf("подтверждение сверщика отказало: %v", err)
	}
	if !ok {
		t.Fatalf("подтверждение не применилось — предпосылка пробы неверна, и «событие есть» " +
			"проверялось бы на не случившемся переходе")
	}

	rows := forID(journalSince(t, s, from), v.ID)
	if len(rows) != 1 {
		t.Fatalf("строк журнала по тому %d, ожидалась ровно одна: %+v", len(rows), rows)
	}
	if rows[0].change != "UPDATED" {
		t.Fatalf("род изменения %q, ожидалась правка", rows[0].change)
	}
	if rows[0].kind != "Volume" || rows[0].project != probeProject {
		t.Fatalf("вид %q, якорь %q — ожидались Volume и %q", rows[0].kind, rows[0].project, probeProject)
	}
}

// TestReconcilerObserveEmitsNothing — ОТРИЦАНИЕ: наблюдение, не меняющее существа
// строки, события НЕ порождает.
//
// Сверщик выполняет `Observe` на КАЖДОМ проходе. Без сужения журнал получал бы
// строку на каждый тик по каждому ресурсу — подписка была бы шумнее опроса,
// который она заменяет.
//
// Положительный контроль внутри той же пробы обязателен: без него утверждение
// «событий нет» зеленело бы и на триггере, не эмитирующем НИЧЕГО.
func TestReconcilerObserveEmitsNothing(t *testing.T) {
	s := newStand(t)
	v := s.createVolume(t, probeProject, "observed")
	store := reconciler.NewStore(s.pool)
	ctx := context.Background()

	from := mark(t, s)
	for i := 0; i < 3; i++ {
		if err := store.Observe(ctx, reconciler.KindVolume, v.ID,
			blockbackend.Observed{State: blockbackend.ObservedReady}); err != nil {
			t.Fatalf("наблюдение отказало: %v", err)
		}
	}
	if rows := forID(journalSince(t, s, from), v.ID); len(rows) != 0 {
		t.Fatalf("три прохода наблюдения дали %d строк журнала: %+v. Наблюдение не есть факт "+
			"о ресурсе для арендатора, и подписка стала бы шумнее опроса", len(rows), rows)
	}

	// Положительный контроль: правка ПО СУЩЕСТВУ в тот же журнал доезжает.
	if ok, err := store.Confirm(ctx, reconciler.KindVolume, v.ID,
		blockbackend.Observed{State: blockbackend.ObservedReady}); err != nil || !ok {
		t.Fatalf("подтверждение не применилось (ok=%v, err=%v) — положительный контроль не выполнен", ok, err)
	}
	if rows := forID(journalSince(t, s, from), v.ID); len(rows) != 1 {
		t.Fatalf("после правки по существу строк журнала %d, ожидалась одна: сужение "+
			"отбрасывает не только наблюдение", len(rows))
	}
}

// TestReconcilerForgetEmitsARemovalWithItsAnchor — снятие сверщиком несёт ЯКОРЬ.
//
// `Forget` выполняет `DELETE … WHERE id = $1 AND state = 'DELETING'` без
// `RETURNING project_id`. Триггер берёт якорь из СНИМАЕМОЙ строки, поэтому он
// есть; при вызове-эмиссии его было бы взять неоткуда.
func TestReconcilerForgetEmitsARemovalWithItsAnchor(t *testing.T) {
	s := newStand(t)
	v := s.createVolume(t, probeProject, "forgotten")
	ctx := context.Background()

	if _, err := s.pool.Exec(ctx, `UPDATE volumes SET state = 'DELETING' WHERE id = $1`, v.ID); err != nil {
		t.Fatalf("том не переведён в снятие: %v", err)
	}
	from := mark(t, s)
	if err := reconciler.NewStore(s.pool).Forget(ctx, reconciler.KindVolume, v.ID); err != nil {
		t.Fatalf("сверщик не снял строку: %v", err)
	}

	rows := forID(journalSince(t, s, from), v.ID)
	if len(rows) != 1 || rows[0].change != "DELETED" {
		t.Fatalf("строк по снятому тому %d (%+v), ожидалось одно снятие", len(rows), rows)
	}
	if rows[0].project != probeProject {
		t.Fatalf("у снятия якорь %q, ожидался %q: пустой якорь означает «предмет уровня "+
			"аккаунта», и подписка с осью проекта его не пропустила бы", rows[0].project, probeProject)
	}
}

// TestAttachEmitsAVolumeUpdate — привязка и отвязка доезжают до журнала как
// изменение ТОМА.
//
// `VolumeRepo.Attach` вставляет строку в `volume_attachments` и НЕ ТРОГАЕТ
// `volumes`. При этом публичный статус тома меняется по существованию этой
// строки. Без триггера на привязке подписчик не узнавал бы о самом частом
// изменении тома за его жизнь.
func TestAttachEmitsAVolumeUpdate(t *testing.T) {
	s := newStand(t)
	repo := pg.NewVolumeRepo(s.pool)
	ctx := context.Background()
	v := s.createVolume(t, probeProject, "attachable")
	if ok, err := reconciler.NewStore(s.pool).Confirm(ctx, reconciler.KindVolume, v.ID,
		blockbackend.Observed{State: blockbackend.ObservedReady}); err != nil || !ok {
		t.Fatalf("том не доведён до готовности (ok=%v, err=%v): привязка к неготовому "+
			"отвергается, и проба падала бы по причине, которой не закрепляет", ok, err)
	}

	const instanceID = "epd-1234567890abcdef"
	from := mark(t, s)
	if err := repo.Attach(ctx, &domain.VolumeAttachment{
		VolumeID: v.ID, InstanceID: instanceID, InstanceName: "web-1",
		ProjectID: probeProject, ZoneID: probeZone, DeviceName: "sdb",
	}); err != nil {
		t.Fatalf("привязка отказала: %v", err)
	}
	rows := forID(journalSince(t, s, from), v.ID)
	if len(rows) != 1 || rows[0].change != "UPDATED" || rows[0].kind != "Volume" {
		t.Fatalf("привязка дала %d строк (%+v); ожидалась одна правка ТОМА — строка `volumes` "+
			"привязкой не трогается, и без триггера на привязке событие не появилось бы вовсе",
			len(rows), rows)
	}
	if rows[0].project != probeProject {
		t.Fatalf("якорь у события привязки %q, ожидался %q", rows[0].project, probeProject)
	}

	from = mark(t, s)
	if err := repo.Detach(ctx, v.ID, instanceID); err != nil {
		t.Fatalf("отвязка отказала: %v", err)
	}
	rows = forID(journalSince(t, s, from), v.ID)
	if len(rows) != 1 || rows[0].change != "UPDATED" {
		t.Fatalf("отвязка дала %d строк (%+v); ожидалась одна правка тома", len(rows), rows)
	}
}

// TestPayloadCarriesColumnNamesAndNoInfra — нагрузка записана ИМЕНАМИ КОЛОНОК и
// не несёт инфра-полей.
//
// Ключи — имена колонок, а не полей Go: нагрузку собирает `to_jsonb` строки, и
// внутренний рефактор Go молча их не переименует. Инфра-полей в ней нет по
// решению: журнал кормит ручку, доступную арендатору, а адрес объекта у бэкенда и
// наблюдение живут только на внутреннем слушателе.
//
// Утверждение об ОТСУТСТВИИ стоит в паре с положительным контролем: без него
// «инфра-полей нет» выполнялось бы и на пустой нагрузке.
func TestPayloadCarriesColumnNamesAndNoInfra(t *testing.T) {
	s := newStand(t)
	from := mark(t, s)
	v := s.createVolume(t, probeProject, "payload-probe")

	rows := forID(journalSince(t, s, from), v.ID)
	if len(rows) != 1 {
		t.Fatalf("строк по созданному тому %d, ожидалась одна", len(rows))
	}
	var got map[string]any
	if err := json.Unmarshal(rows[0].payload, &got); err != nil {
		t.Fatalf("нагрузка не разбирается: %v", err)
	}

	// Положительный контроль: тенантские колонки на месте, и ключи их — ИМЕНА
	// КОЛОНОК (`project_id`), а не имена полей Go (`ProjectID`).
	for _, key := range []string{"id", "project_id", "zone_id", "disk_type_id", "size_bytes", "state", "name"} {
		if _, ok := got[key]; !ok {
			t.Errorf("в нагрузке нет тенантской колонки %q — утверждение об отсутствии "+
				"инфра-полей зеленело бы на пустой нагрузке", key)
		}
	}
	if _, ok := got["ProjectID"]; ok {
		t.Error("нагрузка записана именами полей Go: рефактор переименовал бы их молча")
	}
	// Отрицание: инфра-колонки и наблюдение сюда не попадают.
	for _, key := range []string{
		"binding_id", "desired_binding_id", "backend_object", "backend_namespace",
		"observed_state", "observed_at", "observed_size_bytes",
	} {
		if _, ok := got[key]; ok {
			t.Errorf("в нагрузке инфра-колонка %q: журнал кормит ручку, доступную арендатору, "+
				"а размещение и адрес у бэкенда живут только на внутреннем слушателе", key)
		}
	}
	// `used_bytes` НЕ исключено намеренно: потребление тенантское. Утверждается
	// присутствие ключа, а не его значение: бэкенд мог ничего не сказать.
	if _, ok := got["used_bytes"]; !ok {
		t.Error("из нагрузки исключено `used_bytes` — это тенантское поле " +
			"(`storagev1.Volume.used_bytes`), и подписчик перестал бы просыпаться на его смену")
	}
}

// TestTheRowAloneCannotProduceThePublicProjection — ОСНОВАНИЕ решения «состояние
// предмета журнал не несёт», проверенное вызовом, а не объявленное прозой.
//
// Публичная проекция тома выводится ЧЕРЕЗ таблицы: `domain.DeriveStatus(state,
// attached)` даёт `IN_USE` по существованию строки в `volume_attachments`, тогда
// как сама строка `volumes` несёт `state = 'READY'` и различить `AVAILABLE` от
// `IN_USE` не может. Собери мы состояние из нагрузки — привязанный том уезжал бы
// подписчику как доступный и без единой привязки, а подписчик вправе читать
// непустую нагрузку как ПОЛНОЕ состояние предмета.
//
// Проба закрепляет РАСХОЖДЕНИЕ. Исчезнет оно — станет красной, и это верно: тогда
// у решения не будет основания, и состояние надо вводить.
func TestTheRowAloneCannotProduceThePublicProjection(t *testing.T) {
	s := newStand(t)
	repo := pg.NewVolumeRepo(s.pool)
	ctx := context.Background()
	v := s.createVolume(t, probeProject, "derived-status")
	if ok, err := reconciler.NewStore(s.pool).Confirm(ctx, reconciler.KindVolume, v.ID,
		blockbackend.Observed{State: blockbackend.ObservedReady}); err != nil || !ok {
		t.Fatalf("том не доведён до готовности (ok=%v, err=%v)", ok, err)
	}
	if err := repo.Attach(ctx, &domain.VolumeAttachment{
		VolumeID: v.ID, InstanceID: "epd-1234567890abcdef", InstanceName: "web-1",
		ProjectID: probeProject, ZoneID: probeZone, DeviceName: "sdb",
	}); err != nil {
		t.Fatalf("привязка отказала: %v", err)
	}

	// Публичная проекция, собранная боевым путём чтения.
	got, err := repo.Get(ctx, v.ID)
	if err != nil {
		t.Fatalf("том не прочитался: %v", err)
	}
	public := protoconv.Volume(got)
	if public.Status != storagev1.Volume_IN_USE {
		t.Fatalf("публичная проекция привязанного тома объявляет статус %v, ожидался IN_USE — "+
			"предпосылка решения неверна, и его надо пересмотреть", public.Status)
	}
	if len(public.Attachments) != 1 {
		t.Fatalf("в публичной проекции привязок %d, ожидалась одна", len(public.Attachments))
	}

	// А вот что несёт строка таблицы — то есть ровно то, чем располагает триггер.
	rows := journalSince(t, s, 0)
	var last journalRow
	for _, r := range forID(rows, v.ID) {
		last = r
	}
	var payload map[string]any
	if err := json.Unmarshal(last.payload, &payload); err != nil {
		t.Fatalf("нагрузка не разбирается: %v", err)
	}
	if payload["state"] != "READY" {
		t.Fatalf("строка таблицы несёт state=%v, ожидалось READY", payload["state"])
	}
	if _, ok := payload["attachments"]; ok {
		t.Fatal("нагрузка внезапно несёт привязки: основание решения изменилось, и состояние " +
			"предмета надлежит вводить, а не продолжать объявлять недоступным")
	}
	t.Logf("расхождение подтверждено: публичная проекция %v, строка таблицы state=%v, "+
		"привязок в нагрузке нет — собрать проекцию в триггере можно было бы только второй "+
		"реализацией protoconv на SQL", public.Status, payload["state"])
}
