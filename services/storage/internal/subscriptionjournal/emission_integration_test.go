// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionjournal_test

import (
	"context"
	"encoding/json"
	"testing"

	"google.golang.org/protobuf/proto"

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
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(rows[0].payload, &envelope); err != nil {
		t.Fatalf("нагрузка не разбирается: %v", err)
	}
	body, ok := envelope[pg.JournalStateKey]
	if !ok {
		t.Fatalf("в нагрузке нет конверта %q: без него строка неотличима от прежней формы, "+
			"и состояние по ней не собирается вовсе", pg.JournalStateKey)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("тело конверта не разбирается: %v", err)
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

// TestJournalStateEqualsWhatTheReadPathAnswers — состояние из журнала и ответ
// чтения — ОДИН И ТОТ ЖЕ том, поле в поле.
//
// # Что эта проба ЗАМЕНЯЕТ и почему заменяет, а не ослабляет
//
// На её месте стояла `TestTheRowAloneCannotProduceThePublicProjection` —
// ОСНОВАНИЕ прежнего решения «состояние предмета журнал не несёт»: она
// закрепляла, что строка `volumes` статуса `IN_USE` не выражает, и честно
// говорила про себя: «исчезнет расхождение — станет красной, и это верно: тогда
// у решения не будет основания, и состояние надо вводить».
//
// Расхождение исчезло: конверт несёт привязки, и статус выводится той же
// `domain.DeriveStatus`. Предмет прежней пробы отпал целиком. Ослабить её (снять
// утверждение о привязках) значило бы оставить утверждение, которое больше ни на
// что не смотрит; поэтому она заменена утверждением о ПОЛНОМ состоянии.
//
// # Что эта проба держит
//
// Проекция тома собирается ДВУМЯ разборщиками одной формы: `scanVolume` из строки
// запроса чтения и `pg.VolumeFromJournalPayload` из строки журнала. Расходятся они
// МОЛЧА и в одну сторону — колонка, добавленная в запрос чтения и в домен, во
// втором разборщике просто не появится, и подписчик получит том без неё, не
// отличив это от тома, у которого её значение пусто.
//
// Сравниваются КОНТРАКТЫ, а не структуры домена: контракт — то, что видит клиент,
// и именно на нём расхождение становится его проблемой.
//
// Проба держит и ВТОРУЮ пару об одном предмете: имя конверта записано дважды —
// литералом в миграции и константой `pg.JournalStateKey`. Разойдись они, состояние
// не соберётся ни разу, и здесь это красное; в проверке разборщика по отдельности
// оно невидимо, потому что обе стороны там берут одну константу.
func TestJournalStateEqualsWhatTheReadPathAnswers(t *testing.T) {
	s := newStand(t)
	repo := pg.NewVolumeRepo(s.pool)
	ctx := context.Background()
	v := s.createVolume(t, probeProject, "state-equals-read")
	if ok, err := reconciler.NewStore(s.pool).Confirm(ctx, reconciler.KindVolume, v.ID,
		blockbackend.Observed{State: blockbackend.ObservedReady}); err != nil || !ok {
		t.Fatalf("том не доведён до готовности (ok=%v, err=%v)", ok, err)
	}
	// Метки ставятся боевым путём правки: они — предмет решения, ради которого
	// состояние и вводится, и проба без них не отличила бы полное состояние от
	// состояния без меток.
	if _, _, err := repo.Update(ctx, v.ID, volume.VolumeUpdate{
		LabelsSet: true, Labels: map[string]string{"env": "prod", "tier": "db"},
	}); err != nil {
		t.Fatalf("метки не проставились: %v", err)
	}
	if err := repo.Attach(ctx, &domain.VolumeAttachment{
		VolumeID: v.ID, InstanceID: "epd-1234567890abcdef", InstanceName: "web-1",
		ProjectID: probeProject, ZoneID: probeZone, DeviceName: "sdb", AutoDelete: true,
	}); err != nil {
		t.Fatalf("привязка отказала: %v", err)
	}

	// Сторона ЧТЕНИЯ — боевой путь целиком.
	read, err := repo.Get(ctx, v.ID)
	if err != nil {
		t.Fatalf("том не прочитался: %v", err)
	}
	want := protoconv.Volume(read)

	// Сторона ЖУРНАЛА — последняя строка предмета, разобранная сборщиком владельца.
	rows := forID(journalSince(t, s, 0), v.ID)
	if len(rows) == 0 {
		t.Fatal("журнал не дал ни одной строки по тому — сравнивать не с чем")
	}
	last := rows[len(rows)-1]
	if last.change != "UPDATED" {
		t.Fatalf("последняя строка по тому — %q, ожидалась правка от привязки", last.change)
	}
	packed, absence, serr := subscriptionjournal.Journal().Mapping.State(subscription.Row{
		Kind: last.kind, ID: last.id, Change: last.change, Payload: last.payload,
	})
	if serr != nil {
		t.Fatalf("сборка состояния из строки журнала отказала: %v", serr)
	}
	if packed == nil {
		t.Fatalf("строка журнала не дала состояния (причина %v): либо конверт не записан "+
			"миграцией, либо его имя разошлось с константой разборщика — с этой стороны "+
			"два написания одного ключа неотличимы ничем другим", absence)
	}
	var got storagev1.Volume
	if err := packed.UnmarshalTo(&got); err != nil {
		t.Fatalf("упаковано не состояние тома: %v", err)
	}

	// Положительный контроль ДО сравнения: если обе стороны окажутся пустыми,
	// равенство выполнится тривиально.
	if want.Status != storagev1.Volume_IN_USE || len(want.Labels) != 2 || len(want.Attachments) != 1 {
		t.Fatalf("сторона чтения не наполнена (%v / %v / %d) — равенство ниже зеленело бы "+
			"на двух пустотах", want.Status, want.Labels, len(want.Attachments))
	}
	if !proto.Equal(want, &got) {
		t.Fatalf("состояние из журнала и ответ чтения РАЗОШЛИСЬ.\n  чтение: %v\n  журнал: %v\n"+
			"Подписчик, снявший опрос, держал бы у себя не тот том, что отдаёт Get, и "+
			"расхождение это ничем бы себя не выдало", want, &got)
	}
	t.Logf("совпало поле в поле: статус %v · меток %d · привязок %d · used_by %d",
		got.Status, len(got.Labels), len(got.Attachments), len(got.UsedBy))
}

// TestEnvelopeKeyIsNotAColumnOfTheSchema — имя конверта не совпадает НИ С ОДНОЙ
// колонкой схемы.
//
// Именно на этом стоит различение прежней строки от новой: ключи прежней нагрузки
// суть имена колонок, и конверт, названный как колонка, был бы неотличим от них.
// Проба формы ключа (в наборе разборщика) утверждает, что имя колонкой БЫТЬ не
// может; эта — что оно ею и НЕ ЯВЛЯЕТСЯ здесь и сейчас, по фактическому дереву
// схемы, а не по правилу об идентификаторах.
//
// Перепись печатает объём осмотренного: «совпадений ноль» обязано быть отличимо от
// «колонок прочитано ноль».
func TestEnvelopeKeyIsNotAColumnOfTheSchema(t *testing.T) {
	s := newStand(t)
	var total, clashes int
	if err := s.pool.QueryRow(context.Background(), `
		SELECT count(*), count(*) FILTER (WHERE column_name = $1)
		  FROM information_schema.columns
		 WHERE table_schema = 'kacho_storage'`, pg.JournalStateKey).Scan(&total, &clashes); err != nil {
		t.Fatalf("перепись колонок схемы не прочиталась: %v", err)
	}
	t.Logf("перепись: колонок схемы осмотрено %d · совпадений с именем конверта %d",
		total, clashes)
	if total == 0 {
		t.Fatal("колонок не прочитано ни одной — зелёное здесь неотличимо от пустого обхода")
	}
	if clashes != 0 {
		t.Fatalf("имя конверта %q совпало с колонкой схемы: `to_jsonb` её строки произведёт "+
			"тот же ключ, и строка ПРЕЖНЕЙ формы станет неотличима от новой", pg.JournalStateKey)
	}
}
