// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionjournal_test

import (
	"strconv"
	"strings"
	"testing"

	"google.golang.org/protobuf/types/known/anypb"

	storagev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/storage/v1"
	"github.com/PRO-Robotech/kacho/pkg/subscription"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/pg"
	"github.com/PRO-Robotech/kacho/services/storage/internal/subscriptionjournal"
)

// volumeStateRow — строка журнала вида «том» с конвертом состояния, как её кладёт
// триггер.
//
// Ключи ВНУТРИ конверта — ИМЕНА КОЛОНОК, а не имена полей Go: нагрузку собирает
// `to_jsonb` строки. Отметки времени записаны так, как их отдаёт Postgres для
// `timestamptz`.
//
// Имя самого конверта берётся КОНСТАНТОЙ, а не переписывается: здесь судится
// разборщик, а не согласие двух написаний. Согласие Go-константы с литералом в
// миграции — предмет интеграционной пробы, которая проходит весь путь от мутации
// в базе до контракта у клиента; литерал в этой фикстуре его бы не проверил, а
// только удвоил.
func envelope(body string) []byte {
	return []byte(`{` + strconv.Quote(pg.JournalStateKey) + `:` + body + `}`)
}

func volumeStateRow(change string, body string) subscription.Row {
	return subscription.Row{
		Kind:    subscriptionjournal.JournalWordVolume,
		ID:      "vol-0000000000000001",
		Change:  change,
		Payload: envelope(body),
	}
}

// TestEnvelopeKeyCannotBeAColumnName — конверт отличим от прежней формы ПО
// ПОСТРОЕНИЮ, а не по везению.
//
// Прежняя нагрузка — `to_jsonb` строки, то есть её ключи суть ИМЕНА КОЛОНОК.
// Значит конвертом может служить только написание, которое именем колонки быть не
// может. Незакавыченный идентификатор Postgres состоит из букв, цифр, `_` и `$`;
// точка в него не входит.
//
// Проба утверждает СВОЙСТВО ключа, а не его текст: переименуют конверт — она
// потребует того же свойства от нового имени. Сверка с ФАКТИЧЕСКИМИ колонками
// схемы — отдельная, интеграционная: здесь базы нет, и «ни одна колонка так не
// зовётся» тут было бы обещанием.
//
// Цена отсутствия этой пробы измерена: первая редакция взяла у соседнего владельца
// имя `state`, одноимённое КОЛОНКЕ тома, и строка прежней формы стала давать отказ
// разбора вместо чистого «состояние не производилось».
func TestEnvelopeKeyCannotBeAColumnName(t *testing.T) {
	const unquotedIdentChars = "abcdefghijklmnopqrstuvwxyz0123456789_$"
	if pg.JournalStateKey == "" {
		t.Fatal("ключ конверта пуст — судить не о чем")
	}
	if !strings.ContainsFunc(pg.JournalStateKey, func(r rune) bool {
		return !strings.ContainsRune(unquotedIdentChars, r)
	}) {
		t.Fatalf("ключ конверта %q состоит только из знаков незакавыченного идентификатора: "+
			"колонка с таким именем законна, и `to_jsonb` её строки произвела бы тот же ключ. "+
			"Тогда строка ПРЕЖНЕЙ формы неотличима от новой, и разбор ответит отказом там, "+
			"где обязан ответить «состояние не производилось»", pg.JournalStateKey)
	}
	// Положительный контроль: свойство не выполняется тривиально — обычное имя
	// колонки проба обязана отвергнуть.
	if strings.ContainsFunc("state", func(r rune) bool {
		return !strings.ContainsRune(unquotedIdentChars, r)
	}) {
		t.Fatal("контроль провален: `state` — законное имя колонки, и признак обязан " +
			"считать его негодным конвертом; иначе утверждение выше выполняется на всём")
	}
}

// attachedVolumeBody — тело конверта: том со всеми тенантскими колонками и одной
// привязкой.
const attachedVolumeBody = `{
	"id":"vol-0000000000000001",
	"project_id":"prj-0000000000000001",
	"created_at":"2026-08-30T04:04:05.123456+00:00",
	"updated_at":"2026-08-30T05:06:07.654321+00:00",
	"name":"probe-volume",
	"description":"проба",
	"labels":{"env":"prod","tier":"db"},
	"zone_id":"zone-a",
	"disk_type_id":"dt-ssd",
	"size_bytes":10737418240,
	"block_size":4096,
	"source_snapshot_id":null,
	"source_image_id":null,
	"state":"READY",
	"used_bytes":null,
	"status_reason":"",
	"attachments":[
		{"instance_id":"epd-1234567890abcdef","instance_name":"web-1","device_name":"sdb",
		 "is_boot":false,"mode":"READ_WRITE","auto_delete":true,
		 "attached_at":"2026-08-30T06:00:00+00:00"}
	]
}`

// decodeVolume разворачивает упакованное состояние в контракт тома.
func decodeVolume(t *testing.T, packed *anypb.Any) *storagev1.Volume {
	t.Helper()
	if packed == nil {
		t.Fatal("состояние не отдано")
	}
	var out storagev1.Volume
	if err := packed.UnmarshalTo(&out); err != nil {
		t.Fatalf("упаковано не состояние тома (%v): подписчик развернёт его по типу "+
			"и получил бы отказ вместо предмета", err)
	}
	return &out
}

// TestVolumeStateIsFullAndCarriesLabels — состояние тома отдаётся ПОЛНЫМ, и метки
// в нём есть.
//
// Метки — не украшение: решение о единой форме подписки отдало клиенту отбор по
// меткам ИМЕННО на том основании, что событие несёт полное состояние. Событие без
// меток делает этот отбор неисполнимым, оставаясь на вид исправным.
//
// Утверждается и ВЫВЕДЕННОЕ поле — статус. Он получается из двух источников
// (`state` строки и наличие привязки), и ровно он был доводом против состояния;
// проба закрепляет, что довод снят делом, а не объявлен снятым.
func TestVolumeStateIsFullAndCarriesLabels(t *testing.T) {
	state := subscriptionjournal.Journal().Mapping.State
	got, absence, err := state(volumeStateRow("CREATED", attachedVolumeBody))
	if err != nil {
		t.Fatalf("сборка состояния отказала: %v", err)
	}
	if absence != subscription.StateAbsenceUnnamed {
		t.Fatalf("состояние отдано вместе с причиной отсутствия %v — это два "+
			"взаимоисключающих ответа", absence)
	}
	v := decodeVolume(t, got)

	if len(v.Labels) != 2 || v.Labels["env"] != "prod" || v.Labels["tier"] != "db" {
		t.Errorf("метки в состоянии %v; без них клиентский отбор по меткам неисполним, "+
			"а решение о единой форме подписки стоит именно на нём", v.Labels)
	}
	if v.Status != storagev1.Volume_IN_USE {
		t.Errorf("статус %v, ожидался IN_USE: привязанный том, уехавший как AVAILABLE, "+
			"есть утверждение о ресурсе, которого никто не делал", v.Status)
	}
	if len(v.Attachments) != 1 || v.Attachments[0].InstanceId != "epd-1234567890abcdef" {
		t.Errorf("привязок в состоянии %d (%v), ожидалась одна", len(v.Attachments), v.Attachments)
	}
	if len(v.UsedBy) != 1 {
		t.Errorf("обобщённая проекция used_by %v, ожидалась одна запись — она выводится "+
			"из тех же привязок и обязана ехать вместе с ними", v.UsedBy)
	}
	// Скалярные тенантские поля — положительный контроль: без него утверждения выше
	// зеленели бы на состоянии, собранном из одних привязок.
	if v.Id == "" || v.ProjectId == "" || v.ZoneId == "" || v.DiskTypeId == "" ||
		v.Name == "" || v.SizeBytes == 0 || v.CreatedAt == nil {
		t.Errorf("в состоянии не хватает скалярных тенантских полей: %+v", v)
	}
}

// TestVolumeWithoutAttachmentsIsAvailable — тот же вывод в другую сторону.
//
// Без этой половины «IN_USE» выполнялось бы и на выводе, который отвечает IN_USE
// всегда, — то есть утверждение выше не отличало бы деривацию от константы.
func TestVolumeWithoutAttachmentsIsAvailable(t *testing.T) {
	state := subscriptionjournal.Journal().Mapping.State
	got, _, err := state(volumeStateRow("UPDATED", `{
		"id":"vol-0000000000000001","project_id":"prj-0000000000000001",
		"created_at":"2026-08-30T04:04:05+00:00","updated_at":"2026-08-30T04:04:05+00:00",
		"name":"free-volume","labels":{},"zone_id":"zone-a","disk_type_id":"dt-ssd",
		"size_bytes":1073741824,"state":"READY","attachments":[]}`))
	if err != nil {
		t.Fatalf("сборка состояния отказала: %v", err)
	}
	v := decodeVolume(t, got)
	if v.Status != storagev1.Volume_AVAILABLE {
		t.Errorf("статус непривязанного тома %v, ожидался AVAILABLE", v.Status)
	}
	if len(v.Attachments) != 0 || len(v.UsedBy) != 0 {
		t.Errorf("у непривязанного тома привязки %v / used_by %v", v.Attachments, v.UsedBy)
	}
}

// TestOlderRowIsToldApartByConstruction — строка ПРЕЖНЕЙ формы отличается от новой
// ПО ПОСТРОЕНИЮ, а не по удаче разбора.
//
// Журнал не чистится, и подписчик вправе открыть поток с начала — значит строки,
// записанные до введения конверта, доезжают до сборщика и сегодня.
//
// Нагрузка ниже — ровно прежняя форма: те же имена колонок, тот же разбор. Она
// разобралась бы БЕЗ ОТКАЗА: `encoding/json` сопоставляет имена без учёта
// регистра, и `id`, `name`, `state`, `labels` легли бы в поля записи. Получился бы
// том без привязок и со статусом, выведенным из `state`, — то есть предмет,
// который РАЗОБРАЛСЯ и ЛОЖЕН. Различает только отсутствие конверта.
func TestOlderRowIsToldApartByConstruction(t *testing.T) {
	state := subscriptionjournal.Journal().Mapping.State
	got, absence, err := state(subscription.Row{
		Kind:   subscriptionjournal.JournalWordVolume,
		ID:     "vol-0000000000000001",
		Change: "CREATED",
		Payload: []byte(`{"id":"vol-0000000000000001","project_id":"prj-0000000000000001",
			"name":"legacy","labels":{"env":"prod"},"state":"READY","zone_id":"zone-a",
			"disk_type_id":"dt-ssd","size_bytes":1073741824}`),
	})
	if err != nil {
		t.Fatalf("строка прежней формы объявлена ОШИБКОЙ (%v): она состояния не "+
			"производила, и звать подписчика перечитать нечего", err)
	}
	if got != nil {
		t.Fatal("по строке прежней формы отдано состояние: она собрана БЕЗ привязок и " +
			"без части полей, а подписчик вправе прочитать непустую нагрузку как ПОЛНОЕ " +
			"состояние — он записал бы неполноту как факт")
	}
	if absence != subscription.StateNotProduced {
		t.Fatalf("причина отсутствия %v, ожидалась StateNotProduced", absence)
	}
}

// TestNullSourceIdsDecodeToEmpty — незаданный источник тома доезжает пустой
// строкой, а не отказом разбора.
//
// Колонки происхождения объявлены допускающими NULL, и путь чтения приводит их к
// пустой строке (`COALESCE(…, ”)`). Нагрузка же несёт `null` дословно. Проба
// закрепляет поведение, на которое сборка опирается: `null` в непустой тип
// значения не меняет и отказа не даёт. Свойство языка, а не наше, — и потому
// утверждается вызовом, а не комментарием.
func TestNullSourceIdsDecodeToEmpty(t *testing.T) {
	state := subscriptionjournal.Journal().Mapping.State
	got, _, err := state(volumeStateRow("CREATED", attachedVolumeBody))
	if err != nil {
		t.Fatalf("сборка отказала на `null` в колонках происхождения: %v", err)
	}
	v := decodeVolume(t, got)
	if v.SourceSnapshotId != "" || v.SourceImageId != "" {
		t.Errorf("источники тома %q/%q, ожидались пустыми", v.SourceSnapshotId, v.SourceImageId)
	}
}

// TestStateIsProducedForEveryKindAndNamedAbsentOnRemoval — состояние есть у КАЖДОГО
// вида, а у снятия отсутствие НАЗВАНО.
//
// # Это ТРЕТЬЯ редакция пробы, и каждый раз она ЗАМЕНЯЛАСЬ, а не ослаблялась
//
// Первая закрепляла «состояния нет нигде»; её предмет исчез, когда состояние
// появилось у тома. Вторая закрепляла «есть у тома, названо отсутствующим у
// остальных»; её предмет исчез здесь, когда производитель событий завёлся и у
// снимка с образом (задача продукта #1556).
//
// Ослабление было доступно оба раза и оба раза отвергнуто по одному признаку:
// снять вид из обхода значило бы вывести из наблюдения ровно тот вид, ради
// которого работа делалась. Проба поэтому МЕНЯЕТ УТВЕРЖДЕНИЕ, сохраняя обход
// полным.
//
// # Что она держит сейчас
//
// Обе половины по-прежнему обязаны быть непусты: состояние есть у пар
// «вид × {создание, правка}», названное отсутствие — у пар «вид × снятие». Одна
// половина без другой зеленела бы на реализации, отдающей состояние всегда либо
// никогда.
//
// # Чего она НЕ утверждает — и где это утверждается
//
// СОДЕРЖИМОЕ состояния. Нагрузка здесь одна на все виды, и это намеренно: предмет
// пробы — наличие конверта и род изменения, а не проекция. Равенство состояния
// ответу чтения по каждому виду держат интеграционные пробы
// (`TestJournalStateEqualsWhatTheReadPathAnswers`,
// `TestSnapshotStateEqualsWhatTheReadPathAnswers`,
// `TestImageStateEqualsWhatTheReadPathAnswers`), а свежесть у источников —
// `TestSnapshotStateStaysFreshWhenAVolumeIsSeededAndRemoved`.
// changeDeletedWord — слово владельца для снятия предмета. Выписано здесь, а не
// взято у пакета: константа там неэкспортируемая, и второе написание одного слова
// разошлось бы молча. Расхождение ловит сама проба — при неверном слове состояние
// оказалось бы «обязано быть» у снятия, и обход покраснел бы поимённо.
const changeDeletedWord = "DELETED"

func TestStateIsProducedForEveryKindAndNamedAbsentOnRemoval(t *testing.T) {
	journal := subscriptionjournal.Journal()
	kinds := journal.Mapping.Kinds
	changes := journal.Mapping.Changes
	if len(kinds) == 0 || len(changes) == 0 {
		t.Fatal("словарь видов или родов пуст — судить не о чем, и зелёное было бы пустым обходом")
	}
	state := journal.Mapping.State

	produced, absent := 0, 0
	for kind := range kinds {
		for change := range changes {
			// Нагрузка НЕСЁТ конверт у каждой пары: иначе «состояния нет»
			// выполнялось бы из-за бедной нагрузки, а не из-за решения о роде
			// изменения, и проба не отличала бы одно от другого.
			got, absence, err := state(subscription.Row{
				Kind:    kind,
				ID:      "probe",
				Change:  change,
				Payload: envelope(attachedVolumeBody),
			})
			if err != nil {
				t.Errorf("вид %q род %q: сборка отказала (%v)", kind, change, err)
				continue
			}
			wantState := change != changeDeletedWord
			switch {
			case wantState && got == nil:
				t.Errorf("вид %q род %q: состояния нет, а оно обязано быть — клиентский "+
					"отбор по меткам у этого вида объявлен исполнимым", kind, change)
			case !wantState && got != nil:
				t.Errorf("вид %q род %q: у снятия отдано состояние. Предмета больше нет — "+
					"собирать было нечего, и непустая нагрузка утверждала бы о нём как "+
					"о живом", kind, change)
			}
			if got != nil {
				produced++
				continue
			}
			absent++
			if absence != subscription.StateNotProduced {
				t.Errorf("вид %q род %q: причина отсутствия %v, ожидалась StateNotProduced — "+
					"неназванная доезжает клиенту как «владелец забыл»", kind, change, absence)
			}
		}
	}
	// Перепись печатает ОБЕ величины: одно число скрыло бы случай, ради которого
	// проба заведена, — согласие количеств при расхождении по видам.
	t.Logf("перепись: видов %d · родов %d · пар с состоянием %d · пар с названным отсутствием %d",
		len(kinds), len(changes), produced, absent)
	if produced == 0 {
		t.Error("состояния нет ни у одной пары — обход зелен пустотой")
	}
	if absent == 0 {
		t.Error("названного отсутствия нет ни у одной пары — вторая половина утверждения пуста")
	}
}
