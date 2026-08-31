// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package kacho

import (
	"encoding/json"
	"time"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// Формы нагрузки `nlb_outbox` — по одной на вид, и у каждой ДВЕ стороны:
// строитель и читатель. Они стоят рядом намеренно: разъехаться они могут только
// молча.
//
// # Минимального снимка БОЛЬШЕ НЕ СУЩЕСТВУЕТ — и это перемерено, а не объявлено
//
// Здесь стоял словарь ключей `LifecyclePayload` — типизированный минимальный
// снимок, которым собирались нагрузки всех трёх видов. Его собственная запись
// называла предикат снятия: «поля снимаются ВМЕСТЕ С ТИПОМ, когда состояние
// появится у балансировщика». Оно появилось (#1551), и писателей у типа не
// осталось НИ ОДНОГО:
//
//	git grep -n 'kachorepo\.LifecyclePayload{' -- 'services/nlb/**/*.go'
//
// Предикат назван по ФОРМЕ ВЫЗОВА, а не по имени: имя стоит в этом же разборе и в
// соседних, поэтому поиск по нему отвечал бы своими же объяснениями — «ноль
// писателей» стало бы неотличимо от «три упоминания в прозе».
//
// Поэтому тип снят целиком, а вместе с ним — три поля (`Protocol`, `Port`,
// `ParentResourceID`), потерявшие писателя ещё на обогащении слушателя (#1381),
// и пробы, называвшие его ключи по проводу: у них исчез предмет, а не строгость.
//
// СТРОКИ ПРЕЖНЕЙ ФОРМЫ ПРИ ЭТОМ ЖИВЫ, и уборка журнала (#1735) этого не
// отменяет. Прежняя редакция выводила их живучесть из того, что журнал НЕ
// ЧИСТИТСЯ (`RetainsEverything`); основание снято — окно удержания теперь
// конечно ([subscription.JournalRetention]). Утверждение осталось верным по
// ДРУГОЙ причине, и она сильнее прежней: окно ограничивает возраст строки, а не
// её ФОРМУ, — внутри любого окна лежат строки всех форм, писавшихся в этом окне,
// и следующая смена формы заведёт их снова. Отличать их обязан КОНВЕРТ
// (`PayloadKeyState` ниже), а не удача разбора; читатели конверта отвечают по
// таким строкам «состояние не производилось».
//
// # Стороны утверждаются ПО ПРОВОДУ, а не круговым ходом
//
// Прежний разборщик был зеркалом собственного строителя (обе стороны брали одни
// и те же константы), поэтому его проба не могла отличить верное имя ключа от
// любого другого: переименование значения константы оставляло её зелёной.
// Читатели конверта этому не противоречат — они берут запись ЦЕЛИКОМ и
// проверяются сквозными пробами журнала, от строки в базе до контракта у
// клиента. У балансировщика вторая сторона вообще НЕ НАША: строку пишет ещё и
// триггер базы, и его форма и есть внешний якорь имён (см.
// `LoadBalancerJournalRow`).

// PayloadKeyState — ключ КОНВЕРТА, под которым в нагрузке `nlb_outbox` лежит
// ПОЛНОЕ состояние предмета.
//
// # Почему конверт, а не поля вровень с прежними
//
// Строки прежней, МИНИМАЛЬНОЙ формы доезжают до сборщика состояния и сегодня, и
// отличить их надо ОДНОЗНАЧНО. Уборка журнала (#1735) этого не меняет: окно
// удержания ограничивает ВОЗРАСТ строки, а не её форму, — внутри окна лежат
// строки всех форм, писавшихся в этом окне.
//
// Удача разбора этого не даёт. `encoding/json` сопоставляет имена БЕЗ УЧЁТА
// РЕГИСТРА, поэтому ключи прежней нагрузки `id`/`name`/`protocol`/`port`/`status`
// попали бы в одноимённые поля записи, а `project_id`, родитель, отметка создания
// и МЕТКИ — нет: их прежние имена содержат подчёркивание либо отсутствуют вовсе.
// Получился бы предмет, который разобрался, перенёсся в контракт и оказался
// ЛОЖНЫМ — слушатель без проекта и без меток. Контракт формы разрешает читать
// непустое состояние как ПОЛНОЕ, поэтому подписчик записал бы это как факт.
//
// Конверт снимает вопрос по построению: его ключ прежняя форма не писала ни
// разу, значит «состояние есть» становится наблюдаемым свойством строки, а не
// выводом из того, что разбор не отказал.
//
// Имя `state` взято у поля контракта (`SubscriptionEvent.state`), а не у
// соседнего `status`: соседство обманчиво, и различать их обязан читатель
// нагрузки, а не догадка.
const PayloadKeyState = "state"

// StateEnvelope — нагрузка строки журнала, несущая ПОЛНОЕ состояние предмета.
//
// Предмет кладётся ЦЕЛИКОМ, своей записью репозитория, а не пересобирается по
// полям: пересборка завела бы ВТОРУЮ проекцию ресурса рядом с той, которой
// отвечает чтение, и расходились бы они молча — подписчик получал бы предмет,
// отличный от того, что отдаёт `Get`.
func StateEnvelope(subject any) map[string]any {
	return map[string]any{PayloadKeyState: subject}
}

// ListenerStatePayload — нагрузка строки `nlb_outbox` для вида `nlb_listener`:
// ЗАПИСЬ ЦЕЛИКОМ, под конвертом полного состояния.
//
// # Строитель у вида ОДИН, и живёт он ЗДЕСЬ, а не в пакете use-case
//
// Контракт единой формы разрешает подписчику читать непустое состояние как
// ПОЛНОЕ, поэтому одна точка эмиссии с частичным снимком делает ложным ВЕСЬ вид —
// и делает тихо. Точки эмиссии вида лежат в РАЗНЫХ пакетах use-case: слушателя
// эмитит его собственный, а каскадный переезд — пакет балансировщика, который
// правит проект слушателей вместе со своим (#1549). Строитель, спрятанный в одном
// из них, второму недоступен, и второй завёл бы свой — то есть ровно ту вторую
// форму, против которой правило и написано.
//
// Тип аргумента здесь несущий: строитель принимает `*ListenerRecord`, поэтому
// подать в конверт вида чужую запись нельзя by construction — этого не даёт ни
// один общий `StateEnvelope`, принимающий `any`.
//
// Строитель стоит РЯДОМ со своим читателем (`ListenerStateFromPayload` ниже)
// намеренно: у формы нагрузки две стороны, и разъехаться они могут только молча.
func ListenerStatePayload(rec *ListenerRecord) map[string]any {
	if rec == nil {
		return nil
	}
	return StateEnvelope(rec)
}

// ListenerStateFromPayload — ПОЛНОЕ состояние слушателя из нагрузки строки
// журнала, если строка его несёт.
//
// Возвращает `(nil, nil)`, когда конверта в нагрузке НЕТ. Это не сбой и не
// ошибка: строка прежней, минимальной формы состояния не производила, и назвать
// её сбоем значило бы звать подписчика перечитать то, чего никто не терял.
//
// Ключ берётся КОНСТАНТОЙ, а не повторяется тегом структуры: два написания
// одного ключа разошлись бы молча, и разошлись бы именно в ту сторону, где
// расхождение невидимо — строитель писал бы одно, читатель искал другое, и
// каждая сторона по отдельности выглядела бы исправной.
func ListenerStateFromPayload(raw []byte) (*ListenerRecord, error) {
	return stateFromPayload[ListenerRecord](raw)
}

// TargetGroupStatePayload — нагрузка строки `nlb_outbox` для вида
// `nlb_target_group`: ЗАПИСЬ ЦЕЛИКОМ, под конвертом полного состояния.
//
// # Запись обязана нести НАБОР ЦЕЛЕЙ С СОСТОЯНИЕМ
//
// Публичная проекция группы строится не из её строки, а из `TargetStates` (см.
// `TargetGroupRecord`). Запись, у которой этот набор не заполнен, разберётся,
// перенесётся в контракт и объявит группу БЕЗ ЦЕЛЕЙ — а пустой массив читается
// подписчиком как «целей нет», не как «это событие поле не заполняет». Клиент,
// ведущий состояние, предложит создать цели заново.
//
// Поэтому путь записи, эмитящий строку этого вида, обязан подать запись,
// прочитанную ВМЕСТЕ С ЦЕЛЯМИ, в своей же транзакции. Строитель этого не
// исправляет и исправлять не должен: подмена пустого набора на «как было»
// вернула бы ровно то ложное утверждение, только молча.
func TargetGroupStatePayload(rec *TargetGroupRecord) map[string]any {
	if rec == nil {
		return nil
	}
	return StateEnvelope(rec)
}

// TargetGroupStateFromPayload — ПОЛНОЕ состояние группы из нагрузки строки
// журнала, если строка его несёт.
//
// Возвращает `(nil, nil)`, когда конверта в нагрузке НЕТ: строка прежней,
// минимальной формы состояния не производила, и назвать это сбоем значило бы
// звать подписчика перечитать то, чего никто не терял.
func TargetGroupStateFromPayload(raw []byte) (*TargetGroupRecord, error) {
	return stateFromPayload[TargetGroupRecord](raw)
}

// stateFromPayload — общий разбор конверта состояния.
//
// Ключ берётся КОНСТАНТОЙ, а не повторяется тегом структуры: два написания
// одного ключа разошлись бы молча, и разошлись бы именно в ту сторону, где
// расхождение невидимо — строитель писал бы одно, читатель искал другое, и
// каждая сторона по отдельности выглядела бы исправной.
func stateFromPayload[T any](raw []byte) (*T, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	body, ok := fields[PayloadKeyState]
	if !ok || string(body) == "null" {
		return nil, nil
	}
	var rec T
	if err := json.Unmarshal(body, &rec); err != nil {
		return nil, err
	}
	return &rec, nil
}

// LoadBalancerJournalRow — нагрузка вида `nlb_load_balancer` в том виде, в каком
// она лежит на проводе: СТРОКА ТАБЛИЦЫ `kacho_nlb.load_balancers`, ключи —
// ИМЕНА КОЛОНОК.
//
// # Почему у этого вида СВОЙ тип на проводе, а у двух соседних — нет
//
// У слушателя и группы все точки эмиссии на Go, поэтому в конверт кладётся
// запись репозитория как есть. У балансировщика точек ВОСЕМЬ, и одна из них —
// ТРИГГЕР БАЗЫ (`lb_status_recompute`): он пишет строку целиком, `to_jsonb`, и
// вторую проекцию ресурса на другом языке не заводит. Ключи у него получаются
// ИМЁН КОЛОНОК, а у записи Go — имён полей Go. Форма на проводе обязана быть
// ОДНА, поэтому за неё принята форма ТРИГГЕРА: имя колонки устойчивее — молчаливый
// рефактор Go его не трогает, и авторитет у неё один, база.
//
// Своими тегами обойтись нельзя, и это измерено, а не предположено: `Labels` у
// записи — `dict.HDict`, а его JSON есть МАССИВ ПАР (`[{"K":…,"V":…}]`), тогда
// как `to_jsonb(labels)` даёт ОБЪЕКТ. Запись с тегами колонок разобрала бы
// строку триггера всюду, кроме меток, — то есть ровно там, ради чего обогащение
// и делается.
//
// # Что здесь НЕ является второй проекцией ресурса
//
// Соответствие «поле записи ↔ колонка» уже существует — им живёт разбор строки
// на пути чтения (`pg.scanLB`). Здесь оно только НАЗВАНО, а не заведено заново.
// Проекция в контракт по-прежнему ОДНА и общая с чтением (`dto/type2pb`): этот
// тип до неё не доходит, он отдаёт запись.
//
// # Отметки времени
//
// `to_jsonb` рендерит `timestamptz` по часовому поясу СЕССИИ, поэтому триггер
// перекрывает обе отметки явной формой RFC 3339 в UTC. Форма нагрузки не вправе
// зависеть от настройки того, кто пишет.
//
// # Чего здесь нет и не будет
//
// `xmin` — снимок оптимистичной блокировки, а не состояние: `to_jsonb(строки)`
// системных колонок не отдаёт, и класть его сюда со стороны Go значило бы
// объявить полем то, чем следующий читатель попробует воспользоваться.
type LoadBalancerJournalRow struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	RegionID  string `json:"region_id"`
	// Зона балансировщика — своя координата размещения, а не производная от
	// региона: у ZONAL она названа, у REGIONAL пуста. На проводе обязана быть,
	// иначе подписчик получит «полное состояние» без поля, которое чтение несёт.
	ZoneID      string            `json:"zone_id"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Labels      map[string]string `json:"labels"`
	Type        string            `json:"type"`
	Status      string            `json:"status"`

	SessionAffinity       string   `json:"session_affinity"`
	DeletionProtection    bool     `json:"deletion_protection"`
	PlacementType         string   `json:"placement_type"`
	DisabledAnnounceZones []string `json:"disabled_announce_zones"`
	IPFamilies            []string `json:"ip_families"`

	AddressV4   string `json:"address_v4"`
	AddressV6   string `json:"address_v6"`
	AddressIDV4 string `json:"address_id_v4"`
	AddressIDV6 string `json:"address_id_v6"`
	VipOriginV4 string `json:"vip_origin_v4"`
	VipOriginV6 string `json:"vip_origin_v6"`

	AdminState       string   `json:"admin_state"`
	Placement        string   `json:"placement"`
	CrossZoneEnabled bool     `json:"cross_zone_enabled"`
	SecurityGroupIDs []string `json:"security_group_ids"`
}

// loadBalancerJournalRowOf — запись → строка на проводе.
func loadBalancerJournalRowOf(rec *LoadBalancerRecord) LoadBalancerJournalRow {
	return LoadBalancerJournalRow{
		ID:                    string(rec.ID),
		ProjectID:             string(rec.ProjectID),
		RegionID:              string(rec.RegionID),
		ZoneID:                string(rec.ZoneID),
		CreatedAt:             rec.CreatedAt,
		UpdatedAt:             rec.UpdatedAt,
		Name:                  string(rec.Name),
		Description:           string(rec.Description),
		Labels:                domain.LabelsToMap(rec.Labels),
		Type:                  string(rec.Type),
		Status:                string(rec.Status),
		SessionAffinity:       string(rec.SessionAffinity),
		DeletionProtection:    rec.DeletionProtection,
		PlacementType:         string(rec.PlacementType),
		DisabledAnnounceZones: rec.DisabledAnnounceZones,
		IPFamilies:            ipFamilyStrings(rec.IPFamilies),
		AddressV4:             string(rec.AddressV4),
		AddressV6:             string(rec.AddressV6),
		AddressIDV4:           string(rec.AddressIDV4),
		AddressIDV6:           string(rec.AddressIDV6),
		VipOriginV4:           string(rec.VipOriginV4),
		VipOriginV6:           string(rec.VipOriginV6),
		AdminState:            string(rec.AdminState),
		Placement:             string(rec.Placement),
		CrossZoneEnabled:      rec.CrossZoneEnabled,
		SecurityGroupIDs:      rec.SecurityGroupIDs,
	}
}

// Record — строка на проводе → запись репозитория.
//
// Пустой набор становится `nil`, а не пустым срезом: ровно так его отдаёт разбор
// строки на пути чтения, и различие наблюдаемо — публичная проекция копирует
// набор как есть, а `nil` и пустой срез дают на проводе контракта разное.
func (r LoadBalancerJournalRow) Record() LoadBalancerRecord {
	var rec LoadBalancerRecord
	rec.ID = domain.ResourceID(r.ID)
	rec.ProjectID = domain.ProjectID(r.ProjectID)
	rec.RegionID = domain.RegionID(r.RegionID)
	rec.CreatedAt = r.CreatedAt
	rec.UpdatedAt = r.UpdatedAt
	rec.Name = domain.LbName(r.Name)
	rec.Description = domain.LbDescription(r.Description)
	rec.Labels = domain.LabelsFromMap(r.Labels)
	rec.Type = domain.LBType(r.Type)
	rec.Status = domain.LBStatus(r.Status)
	rec.SessionAffinity = domain.SessionAffinity(r.SessionAffinity)
	rec.DeletionProtection = r.DeletionProtection
	rec.PlacementType = domain.PlacementType(r.PlacementType)
	rec.DisabledAnnounceZones = nilIfEmpty(r.DisabledAnnounceZones)
	rec.IPFamilies = ipFamiliesOf(r.IPFamilies)
	rec.AddressV4 = domain.IPAddress(r.AddressV4)
	rec.AddressV6 = domain.IPAddress(r.AddressV6)
	rec.AddressIDV4 = domain.AddressID(r.AddressIDV4)
	rec.AddressIDV6 = domain.AddressID(r.AddressIDV6)
	rec.VipOriginV4 = domain.VipOrigin(r.VipOriginV4)
	rec.VipOriginV6 = domain.VipOrigin(r.VipOriginV6)
	rec.AdminState = domain.AdminState(r.AdminState)
	rec.Placement = domain.Placement(r.Placement)
	rec.CrossZoneEnabled = r.CrossZoneEnabled
	rec.SecurityGroupIDs = nilIfEmpty(r.SecurityGroupIDs)
	return rec
}

func nilIfEmpty(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	return append([]string(nil), s...)
}

func ipFamilyStrings(fams []domain.IPVersion) []string {
	if len(fams) == 0 {
		return nil
	}
	out := make([]string, len(fams))
	for i, f := range fams {
		out[i] = string(f)
	}
	return out
}

func ipFamiliesOf(raw []string) []domain.IPVersion {
	if len(raw) == 0 {
		return nil
	}
	out := make([]domain.IPVersion, len(raw))
	for i, s := range raw {
		out[i] = domain.IPVersion(s)
	}
	return out
}

// LoadBalancerStatePayload — нагрузка строки `nlb_outbox` для вида
// `nlb_load_balancer`: СТРОКА ТАБЛИЦЫ, под конвертом полного состояния.
//
// # Строитель у вида ОДИН, и живёт он ЗДЕСЬ
//
// Точки эмиссии этого вида лежат в ДВУХ пакетах use-case: пять — в собственном
// (создание · правка · снятие · переезд, у которого их две), две — в пакете
// СЛУШАТЕЛЯ (создание и снятие слушателя объявляют правку своего
// балансировщика). Строитель, спрятанный в одном из них, второму недоступен, и
// второй завёл бы свой — вторую форму нагрузки того же вида. Держит это разбор
// дерева use-case'ов (`TestEveryEmissionOfAStatefulKindBuildsTheSamePayload`).
//
// # Восьмая точка — ТРИГГЕР, и разбор её не видит
//
// Пересчёт статуса пишет свою строку сам (`lb_status_recompute`), и никакой
// разбор Go его не увидит. Согласие двух форм держит сквозная проба над
// НАСТОЯЩЕЙ схемой (`TestLoadBalancerStateIsTheSameFromTheTriggerAndFromGo`):
// она заставляет триггер сработать настоящим оператором и сверяет обе нагрузки
// на ОДНОЙ строке. Гейт по тексту миграций судить триггер не может — живое тело
// функции есть последнее из череды переопределений, а прежние лежат в
// ПРИМЕНЁННЫХ миграциях, править которые нельзя (ban #5).
//
// Тип аргумента здесь несущий: строитель принимает `*LoadBalancerRecord`,
// поэтому подать в конверт вида чужую запись нельзя by construction.
func LoadBalancerStatePayload(rec *LoadBalancerRecord) map[string]any {
	if rec == nil {
		return nil
	}
	return StateEnvelope(loadBalancerJournalRowOf(rec))
}

// LoadBalancerStateFromPayload — ПОЛНОЕ состояние балансировщика из нагрузки
// строки журнала, если строка его несёт.
//
// Возвращает `(nil, nil)`, когда конверта в нагрузке НЕТ: строка прежней,
// минимальной формы состояния не производила, и назвать это сбоем значило бы
// звать подписчика перечитать то, чего никто не терял.
func LoadBalancerStateFromPayload(raw []byte) (*LoadBalancerRecord, error) {
	row, err := stateFromPayload[LoadBalancerJournalRow](raw)
	if err != nil || row == nil {
		return nil, err
	}
	rec := row.Record()
	return &rec, nil
}
