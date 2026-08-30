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
// Расходятся они молча и в одну сторону: колонка, добавленная в `volumeSelectCols`
// и в `domain.Volume`, здесь просто не появится, и подписчик получит том без неё,
// не отличив это от тома, у которого её значение пусто. Поэтому обе стороны лежат
// РЯДОМ, в одном пакете, и держатся не соседством, а пробой
// `TestJournalStateEqualsWhatTheReadPathAnswers`: она читает один и тот же том
// обоими путями и требует совпадения контрактов.
//
// # ЧТО ЗДЕСЬ НАМЕРЕННО НЕ РАЗБИРАЕТСЯ
//
// `status_reason` и `used_bytes` в нагрузке ЕСТЬ, а здесь не читаются — потому что
// их не читает и путь чтения: `volumeSelectCols` их не выбирает, и `Get`/`List`
// отдают их пустыми при живых колонках. Разбирать их здесь значило бы сделать
// поток БОГАЧЕ чтения: подписчик видел бы у тома поле, которое `Get` того же тома
// отрицает. Расхождение двух проекций одного предмета — тот самый класс, ради
// которого состояние и вводится.
//
// Это не одобрение того, что путь чтения их теряет: предмет заведён своей задачей.
// Когда он закроется, проба равенства покраснеет и потребует править ОБЕ стороны
// одним изменением — что и требуется.

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
	State       string              `json:"state"`
	Attachments []journalAttachment `json:"attachments"`
}

// journalAttachment — строка привязки в конверте.
type journalAttachment struct {
	InstanceID   string    `json:"instance_id"`
	InstanceName string    `json:"instance_name"`
	DeviceName   string    `json:"device_name"`
	IsBoot       bool      `json:"is_boot"`
	Mode         string    `json:"mode"`
	AutoDelete   bool      `json:"auto_delete"`
	AttachedAt   time.Time `json:"attached_at"`
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
	return rec.volume(), nil
}

// volume собирает `domain.Volume` — тем же порядком, каким его собирает
// `scanVolume` на чтении.
func (r journalVolume) volume() *domain.Volume {
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
	}
	// Статус — ТА ЖЕ деривация, что на чтении, и от того же признака: существует ли
	// хоть одна привязка. Ветка `IN_USE` тем самым проверяема с обеих сторон.
	v.Status = domain.DeriveStatus(r.State, len(r.Attachments) > 0)
	for _, a := range r.Attachments {
		v.Attachments = append(v.Attachments, domain.VolumeAttachment{
			VolumeID:     r.ID,
			InstanceID:   a.InstanceID,
			InstanceName: a.InstanceName,
			ProjectID:    r.ProjectID,
			ZoneID:       r.ZoneID,
			DeviceName:   a.DeviceName,
			IsBoot:       a.IsBoot,
			Mode:         attachModeFromDB(a.Mode),
			AutoDelete:   a.AutoDelete,
			AttachedAt:   a.AttachedAt,
		})
	}
	return v
}
