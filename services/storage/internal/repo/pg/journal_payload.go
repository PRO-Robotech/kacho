// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg

import (
	"encoding/json"
	"time"

	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
)

// journal_payload.go — ВТОРАЯ сторона той же проекции тома: разбор строки журнала
// подписки в `domain.Volume`.
//
// # ПОЧЕМУ ЭТО ЛЕЖИТ ЗДЕСЬ, А НЕ РЯДОМ С ОБЪЯВЛЕНИЕМ ЖУРНАЛА
//
// Первая сторона — `scanVolume` в соседнем файле: она собирает `domain.Volume` из
// строки ЗАПРОСА ЧТЕНИЯ (`volumeSelectCols`). Эта — из строки ЖУРНАЛА, которую
// собрал триггер. Источники разные (курсор pgx и JSON), собираемое — ОДНО.
//
// Расходились они молча и в одну сторону: колонка, добавленная в
// `volumeSelectCols` и в `domain.Volume`, здесь просто не появлялась, и подписчик
// получал том без неё, не отличив это от тома, у которого её значение пусто.
// Поэтому обе стороны лежат РЯДОМ, в одном пакете, и держатся пробой
// `TestJournalStateEqualsWhatTheReadPathAnswers`: она читает один и тот же том
// обоими путями и требует совпадения контрактов.
//
// # ПРИВЯЗКИ ИЗ-ПОД ЭТОГО РИСКА ВЫВЕДЕНЫ — разборщик у них ОДИН
//
// Здесь стоял раздел «где стороны расходятся ЗАКОННО»: конверт нёс все привязки, а
// `scanVolume` — одну, потому что запрос чтения соединял их без агрегации. Это был
// дефект ЧТЕНИЯ (задача продукта #1559), а не расхождение сборки, и раздел честно
// это называл.
//
// Дефект закрыт: запрос чтения собирает привязки ТЕМ ЖЕ `jsonb_agg`, с теми же
// именами полей и тем же порядком, что триггер журнала, а разбирает обе стороны
// ОДНА `decodeAttachments`. Своей структуры привязки здесь больше нет — значит по
// этому полю стороны не могут разойтись ВОВСЕ, а не «разойдутся и будут пойманы».
//
// # `status_reason` И `used_bytes` — ЗДЕСЬ РАЗБИРАЮТСЯ, и это половина одного изменения
//
// Здесь стояло «намеренно не разбираются»: их не читал и путь чтения
// (`volumeSelectCols` их не выбирал), поэтому разбор сделал бы поток БОГАЧЕ `Get` —
// подписчик видел бы у тома поле, которое чтение того же тома отрицает.
//
// Основание отпало вместе с дефектом: путь чтения починен (задача продукта #1557).
// Оставь мы разбор снятым — расхождение просто поменяло бы знак, и беднее стал бы
// уже поток. Поэтому обе стороны выровнены ОДНИМ изменением, как и предсказывала
// прежняя редакция этого абзаца.

// JournalStateKey — ключ КОНВЕРТА, под которым триггер кладёт в нагрузку журнала
// ПОЛНОЕ состояние предмета.
//
// # Почему конверт, а не поля вровень с прежними
//
// Журнал не чистится (объявление подписки удерживает всё), а подписчик вправе
// открыть поток с начала. Значит строки ПРЕЖНЕЙ, минимальной формы доезжают до
// сборщика состояния и сегодня, и отличить их надо ОДНОЗНАЧНО.
//
// Удача разбора этого не даёт. Прежняя нагрузка — та же строка `volumes`, те же
// имена колонок; она разобралась бы БЕЗ ОТКАЗА и дала бы том без привязок, то есть
// привязанный том уехал бы как доступный. Контракт формы разрешает подписчику
// читать непустое состояние как ПОЛНОЕ, поэтому он записал бы это как факт.
//
// Конверт снимает вопрос по построению: его ключ прежняя форма не писала ни разу,
// значит «состояние есть» — наблюдаемое свойство строки, а не вывод из того, что
// разбор не отказал.
//
// # ТОЧКА В ИМЕНИ — НЕ УКРАШЕНИЕ, и цена её отсутствия измерена
//
// Ключи прежней нагрузки — ИМЕНА КОЛОНОК: её собирает `to_jsonb` строки. Значит
// конвертом не может служить ни одно написание, которое бывает именем колонки, —
// а первая редакция этого файла взяла у соседнего владельца имя `state`, у
// которого в `volumes` есть ОДНОИМЁННАЯ КОЛОНКА. Строка прежней формы несла
// `"state":"READY"`, разбор доставал под этим ключом СТРОКУ вместо объекта и
// отказывал: событие уезжало подписчику причиной «собрать не удалось» — то есть
// звало перечитывать вечно то, чего никто не терял. Поймано собственной пробой
// (`TestOlderRowIsToldApartByConstruction`), а не рассуждением.
//
// Точка закрывает класс ЦЕЛИКОМ, а не этот случай: незакавыченное имя колонки её
// содержать не может, а закавыченных в этой схеме нет ни одного. Поэтому
// `to_jsonb` любой строки любой её таблицы такого ключа не произведёт никогда — ни
// сегодня, ни после колонки, которую заведут завтра. Свойство утверждается двумя
// пробами: формой самого ключа и переписью колонок схемы.
//
// Вторая половина имени взята у поля контракта (`SubscriptionEvent.state`) — того
// самого, которое конверт и наполняет.
const JournalStateKey = "subscription.state"

// journalVolume — строка тома и её привязки, как их кладёт в конверт триггер.
//
// Имена — ИМЕНА КОЛОНОК: нагрузку собирает `to_jsonb` строки, поэтому внутренний
// рефактор Go их молча не переименует. Обратная сторона названа честно: колонка,
// переименованная миграцией, разойдётся с этими тегами — и это ловит проба
// равенства с путём чтения, а не компилятор.
type journalVolume struct {
	ID          string            `json:"id"`
	ProjectID   string            `json:"project_id"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Labels      map[string]string `json:"labels"`
	ZoneID      string            `json:"zone_id"`
	DiskTypeID  string            `json:"disk_type_id"`
	SizeBytes   int64             `json:"size_bytes"`
	// Колонки происхождения объявлены допускающими NULL, и путь чтения приводит их
	// к пустой строке (`COALESCE(…, '')`). Здесь тот же исход достигается тем, что
	// `null` в непустой тип значения не меняет и отказа не даёт; свойство языка, а
	// не наше, поэтому оно утверждается вызовом (`TestNullSourceIdsDecodeToEmpty`).
	SourceSnapshot string `json:"source_snapshot_id"`
	SourceImage    string `json:"source_image_id"`
	// State — persisted-состояние строки. В публичный статус оно превращается
	// ЕДИНСТВЕННОЙ деривацией `domain.DeriveStatus`, той же, что на чтении: второй
	// её реализации — хоть на SQL, хоть здесь — не заводится.
	State string `json:"state"`
	// StatusReason и UsedBytes — те же две колонки, что читает путь чтения.
	//
	// Здесь они прежде НЕ разбирались, и это было верно ровно пока их не читал и
	// путь чтения: разбери мы их раньше — поток стал бы БОГАЧЕ `Get`, и подписчик
	// видел бы у тома поле, которое чтение того же тома отрицает. Путь чтения
	// починен (#1557), и стороны обязаны были выровняться ТЕМ ЖЕ изменением —
	// иначе расхождение просто поменяло бы знак.
	//
	// `used_bytes` — указатель: колонка допускает NULL, и «бэкенд не сказал»
	// обязано остаться отличимым от нуля.
	StatusReason string `json:"status_reason"`
	UsedBytes    *int64 `json:"used_bytes"`
	// Attachments приезжает СЫРЫМ JSON и разбирается общей `decodeAttachments` —
	// той же, что разбирает перечень на пути чтения. Своей структуры здесь больше
	// нет: два разбора одной формы расходились бы молча.
	Attachments json.RawMessage `json:"attachments"`
}

// attachmentJSON — строка привязки, как её кладут в JSON ОБА производителя:
// триггер журнала (`storage_outbox_volume_state`) и запрос чтения
// (`volumeAttachmentsJSON` в соседнем volume_repo.go).
//
// Форма у них ОДНА намеренно, и разбирает её тоже одна `decodeAttachments`. До
// этого перечень привязок собирался дважды — здесь из JSON, там из колонок
// соединения, — и стороны расходились молча: поле, добавленное привязке, приезжало
// в одну и не приезжало в другую. Теперь расхождение непредставимо: разборщик один,
// и он же диктует обоим производителям имена полей.
type attachmentJSON struct {
	InstanceID   string    `json:"instance_id"`
	InstanceName string    `json:"instance_name"`
	DeviceName   string    `json:"device_name"`
	IsBoot       bool      `json:"is_boot"`
	Mode         string    `json:"mode"`
	AutoDelete   bool      `json:"auto_delete"`
	AttachedAt   time.Time `json:"attached_at"`
}

// decodeAttachments разбирает перечень привязок тома из JSON.
//
// Проект и зона берутся у САМОГО ТОМА, а не из строки привязки: привязка своей
// строкой их дублирует (колонки `project_id`/`zone_id` существуют ради CAS-сверки
// при вставке), и класть в перечень второй экземпляр той же величины значило бы
// завести два источника одного факта. Публичная проекция их не отдаёт вовсе —
// `protoconv.VolumeAttachment` этих полей не несёт.
//
// Пустой вход — законный: `[]` от обоих производителей, `nil` у строки, которая
// перечня не несла. Обе формы дают ноль привязок и НЕ дают ошибки: «привязок нет» —
// это факт, а не сбой разбора.
func decodeAttachments(raw []byte, volumeID, projectID, zoneID string) ([]domain.VolumeAttachment, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var rows []attachmentJSON
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	out := make([]domain.VolumeAttachment, 0, len(rows))
	for _, a := range rows {
		out = append(out, domain.VolumeAttachment{
			VolumeID:     volumeID,
			InstanceID:   a.InstanceID,
			InstanceName: a.InstanceName,
			ProjectID:    projectID,
			ZoneID:       zoneID,
			DeviceName:   a.DeviceName,
			IsBoot:       a.IsBoot,
			Mode:         attachModeFromDB(a.Mode),
			AutoDelete:   a.AutoDelete,
			AttachedAt:   a.AttachedAt,
		})
	}
	return out, nil
}

// VolumeFromJournalPayload — `domain.Volume` из нагрузки строки журнала, если
// строка несёт конверт состояния.
//
// Возвращает `(nil, nil)`, когда конверта НЕТ. Это не сбой и не ошибка: строка
// прежней формы состояния не производила, и назвать её сбоем значило бы звать
// подписчика перечитать то, чего никто не терял.
//
// Ключ берётся КОНСТАНТОЙ выше, а не повторяется тегом структуры: два написания
// одного ключа разошлись бы молча — строитель писал бы одно, читатель искал другое,
// и каждая сторона по отдельности выглядела бы исправной.
func VolumeFromJournalPayload(raw []byte) (*domain.Volume, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	body, ok := fields[JournalStateKey]
	if !ok || string(body) == "null" {
		return nil, nil
	}
	var rec journalVolume
	if err := json.Unmarshal(body, &rec); err != nil {
		return nil, err
	}
	return rec.volume()
}

// volume собирает `domain.Volume` — тем же порядком, каким его собирает
// `scanVolume` на чтении.
func (r journalVolume) volume() (*domain.Volume, error) {
	v := &domain.Volume{
		ID:             r.ID,
		ProjectID:      r.ProjectID,
		Name:           r.Name,
		Description:    r.Description,
		Labels:         r.Labels,
		ZoneID:         r.ZoneID,
		DiskTypeID:     r.DiskTypeID,
		SizeBytes:      r.SizeBytes,
		SourceSnapshot: r.SourceSnapshot,
		SourceImage:    r.SourceImage,
		CreatedAt:      r.CreatedAt,
		UpdatedAt:      r.UpdatedAt,
		StatusReason:   domain.StatusReason(r.StatusReason),
	}
	if r.UsedBytes != nil {
		v.Observation.UsedBytes = *r.UsedBytes
		v.Observation.HasUsedBytes = true
	}
	// Привязки разбирает ТА ЖЕ функция, что и на чтении, из ТОЙ ЖЕ формы.
	// Отказ разбора здесь возможен только на испорченной нагрузке, и он обязан
	// доехать до вызывающего: «привязок нет» и «разобрать не удалось» — разные
	// факты, и склеить их значило бы отдать привязанный том как доступный.
	atts, err := decodeAttachments(r.Attachments, r.ID, r.ProjectID, r.ZoneID)
	if err != nil {
		return nil, err
	}
	v.Attachments = atts
	// Статус — ТА ЖЕ деривация, что на чтении, и от того же признака: существует ли
	// хоть одна привязка. Ветка `IN_USE` тем самым проверяема с обеих сторон.
	v.Status = domain.DeriveStatus(r.State, len(atts) > 0)
	return v, nil
}
