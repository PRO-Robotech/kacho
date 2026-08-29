// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package kacho

// Имена ключей JSON-нагрузки `nlb_outbox` — единый источник для тех
// производителей, которые собирают её ТИПИЗИРОВАННО (шесть мест: два у
// слушателя, два у балансировщика, два у целевой группы).
//
// # У НАГРУЗКИ НЕТ ЧИТАТЕЛЯ, и это сказано вслух (задача #1452)
//
// Прежняя редакция называла потребителя в настоящем времени —
// «InternalResourceLifecycleService.Subscribe → kacho-iam FGA-sync», — и это
// было неверно по трём независимым осям сразу:
//
//  1. названный контракт СНЯТ (задача #814, надгробие `retiredRPCSurface`;
//     его читатель снят задачей #1043). Потребителей у него не было ни одного
//     и до снятия;
//  2. зеркало прав ходит ДРУГОЙ очередью — `fga_register_outbox` (миграция
//     0002, где несовместимость схем с этой таблицей объявлена решением). К
//     `nlb_outbox` оно не обращается;
//  3. «разделяемый КАЖДЫМ producer'ом» тоже не выполнялось: из ДЕВЯТИ живых
//     путей, пишущих сюда нагрузку, ТРИ собирают её мимо этого словаря — джоба
//     освобождения адреса (`reason`), джоба слива целей (`reason`) и триггерная
//     функция пересчёта статуса (`recomputed`).
//
// Цена ошибки была не в стиле. Задача #1381 (обогатить журнал состоянием)
// упирается в вопрос «что сломается, если поменять форму нагрузки»; по этому
// комментарию ответ был «зеркало прав у соседа» — довод против, — а по дереву
// ответ «ничего».
//
// # Что читает нагрузку СЕГОДНЯ
//
// Ничего. Общий сервер потока (`pkg/subscription`) выбирает колонку запросом и
// отдаёт её объявлению журнала владельца; объявление nlb
// (`internal/subscriptionjournal`) состояния из неё НЕ собирает и параметр
// строки не именует — по решению, названному там же. Разборщика нагрузки в
// дереве больше нет: он был зеркалом собственного строителя (обе стороны брали
// одни и те же константы), поэтому его проба круговым ходом не могла отличить
// верное имя ключа от любого другого — переименование значения константы
// оставляло её зелёной.
//
// Поэтому обе стороны утверждаются теперь ПО ПРОВОДУ: пробы производителей
// называют строковые литералы ключей, а не константы, которыми эти ключи
// собраны.
//
// # Почему нагрузка при этом ОСТАЁТСЯ
//
// Читатель у неё будет — это задача #1381, живая и в той же релизной линии;
// решение о СОДЕРЖАНИИ нагрузки принадлежит ей, а не этой записи. Снять
// нагрузку целиком нельзя и по второй причине: один из производителей — тело
// триггерной функции в ПРИМЕНЁННОЙ миграции, и остановить его можно только новой
// миграцией, переписывающей это тело, — работа с измеренной ценой и нулевым
// измеренным выигрышем. Предикат появления потребителя и цена отвергнутых
// исходов записаны решением в
// `services/nlb/docs/engineering/architecture/08-known-divergences.md`.
const (
	PayloadKeyID               = "id"
	PayloadKeyProjectID        = "project_id"
	PayloadKeyRegionID         = "region_id"
	PayloadKeyName             = "name"
	PayloadKeyStatus           = "status"
	PayloadKeyType             = "type"
	PayloadKeyProtocol         = "protocol"
	PayloadKeyPort             = "port"
	PayloadKeyTrigger          = "trigger"
	PayloadKeyParentResourceID = "parent_resource_id"
	PayloadKeyOldProjectID     = "old_project_id"
	PayloadKeyNewProjectID     = "new_project_id"
)

// LifecyclePayload — типизированный снимок нагрузки `nlb_outbox`. Производитель
// заполняет относящиеся к его виду поля и зовёт Map(); пустые поля в нагрузку не
// попадают.
//
// Читателя у этих полей сегодня нет ни одного (см. запись выше). Поля не сняты
// потому, что содержание нагрузки — предмет задачи #1381, а не этой; здесь
// снята только ЛОЖНОСТЬ объявления и разборщик, у которого не было вызывающих.
type LifecyclePayload struct {
	// ID — идентификатор ресурса (балансировщик / слушатель / целевая группа).
	ID string
	// ParentResourceID — идентификатор родителя (слушатель → его балансировщик).
	// Пусто у ресурсов проекта, у которых родителя нет.
	ParentResourceID string
	ProjectID        string
	RegionID         string
	Name             string
	Status           string
	// Type — только у балансировщика.
	Type string
	// Protocol / Port — только у слушателя.
	Protocol string
	Port     int32
	// Trigger — диагностический маркер перекрёстного эмита правки
	// (`listener_created` / `listener_deleted` / …).
	Trigger string
	// OldProjectID — исходный проект ресурса для события переезда.
	OldProjectID string
	// NewProjectID — целевой проект ресурса для события переезда.
	NewProjectID string
}

// Map — строит `map[string]any` для OutboxEmitter.Emit, включая только непустые
// поля (минимальный снимок). Ключи — из констант выше.
func (p LifecyclePayload) Map() map[string]any {
	m := make(map[string]any, 12)
	putNonEmpty(m, PayloadKeyID, p.ID)
	putNonEmpty(m, PayloadKeyParentResourceID, p.ParentResourceID)
	putNonEmpty(m, PayloadKeyProjectID, p.ProjectID)
	putNonEmpty(m, PayloadKeyRegionID, p.RegionID)
	putNonEmpty(m, PayloadKeyName, p.Name)
	putNonEmpty(m, PayloadKeyStatus, p.Status)
	putNonEmpty(m, PayloadKeyType, p.Type)
	putNonEmpty(m, PayloadKeyProtocol, p.Protocol)
	putNonEmpty(m, PayloadKeyTrigger, p.Trigger)
	putNonEmpty(m, PayloadKeyOldProjectID, p.OldProjectID)
	putNonEmpty(m, PayloadKeyNewProjectID, p.NewProjectID)
	if p.Port != 0 {
		m[PayloadKeyPort] = p.Port
	}
	return m
}

func putNonEmpty(m map[string]any, key, val string) {
	if val != "" {
		m[key] = val
	}
}
