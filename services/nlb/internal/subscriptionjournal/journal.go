// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package subscriptionjournal — объявление ЖУРНАЛА nlb для общего сервера потока
// изменений (`pkg/subscription`).
//
// Здесь только ЗНАЧЕНИЯ: где журнал лежит, каким каналом будит, как его строка
// становится событием общей формы. Курсор, граница устоявшегося, пределы, сужение
// по правам и порядок отказов принадлежат общему серверу и владельцу не выдаются.
//
// # ЖУРНАЛ nlb СОСТОЯНИЯ НЕ НЕСЁТ, и это сказано вслух
//
// Нагрузка событий nlb — намеренно МИНИМАЛЬНЫЙ снимок (`kachorepo.LifecyclePayload`:
// идентификатор, родитель, проект, регион, имя, состояние, протокол, порт), и её
// собственный комментарий объявляет это решением: «outbox-event это уведомление,
// а не полный ресурс — consumer делает дополнительный Get(id) если нужно».
//
// Общая форма подписки разрешает читать НЕПУСТУЮ нагрузку как ПОЛНОЕ состояние
// предмета, и это единственный случай, когда подписчик вправе так делать. Значит
// отдать сюда частичный снимок нельзя: подписчик записал бы отсутствующие поля
// как факт — меток нет, зоны нет, целей нет. Поэтому события nlb доезжают с
// признаком «состояния не будет», а состояние подписчик берёт чтением по
// идентификатору.
//
// **Ценность потока от этого не исчезает, и её стоит назвать точно.** Подписчик
// узнаёт вид, идентификатор, якорь проекта и род изменения — то есть ЧТО
// перечитать и когда. Именно это снимает постоянный опрос списков: чтение
// случается на изменении, а не каждые три секунды.
//
// **Чего это НЕ закрывает — тоже названо, чтобы следующий не принял за
// закрытое.** Отбор по меткам на стороне клиента (решение владельца о единой форме подписки) исходил
// из того, что событие несёт полное состояние. Для журнала nlb посылка неверна:
// меток в нагрузке нет вовсе. Предмет заведён отдельной задачей — здесь он
// назван, а не починен, потому что чинится он ОБОГАЩЕНИЕМ ЖУРНАЛА nlb, а это
// правка его эмиттеров, а не подписки.
package subscriptionjournal

import (
	"fmt"

	"google.golang.org/protobuf/types/known/anypb"

	subscriptionv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"
	"github.com/PRO-Robotech/kacho/pkg/authz"
	"github.com/PRO-Robotech/kacho/pkg/subscription"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/authzfilter"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

const (
	// Table — таблица журнала, квалифицированная схемой.
	Table = "kacho_nlb.nlb_outbox"

	// Channel — канал пробуждения.
	//
	// Он НЕ выводится из имени таблицы, и nlb — ровно тот случай, ради которого
	// общая форма держит его отдельным полем: таблица здесь схемо-квалифицирована
	// (`kacho_nlb.nlb_outbox`), а канал триггера — нет (`pg_notify('nlb_outbox', …)`,
	// миграция 0001). Вывод одного из другого работал бы у большинства владельцев
	// и молча ошибался у этого.
	Channel = "nlb_outbox"
)

// Journal — объявление журнала nlb.
func Journal() subscription.Journal {
	return subscription.Journal{
		Channel: Channel,
		Storage: subscription.Storage{
			Table:          Table,
			PositionColumn: "sequence_no",
			// Имена колонок у nlb СВОИ: вид предмета зовётся `resource_type`, а
			// род изменения — `action`, тогда как у соседних журналов это
			// `resource_kind` и `event_type`. Разнобой форм хранения дефектом не
			// является: единой объявлена форма ПОДПИСКИ, а не форма журнала, и
			// ровно поэтому имена стоят здесь значениями, а не выводятся из
			// «стандартной схемы», которой нет.
			KindColumn:    "resource_type",
			IDColumn:      "resource_id",
			ChangeColumn:  "action",
			PayloadColumn: "payload",
			ProjectColumn: "project_id",
			// Якорь проекта у nlb лежит колонкой С БАЗОВОЙ СХЕМЫ — это
			// единственный ресурсный журнал дерева, у которого он был изначально.
			// Значит ось отбирается запросом, по частичному индексу
			// `nlb_outbox_project_idx (project_id, sequence_no) WHERE project_id <> ''`.
			Project: subscription.ProjectInColumn,
			// Журнал НЕ чистится: снятия строк у него нет ни на одном пути
			// (предикат — `git grep -in 'DELETE FROM kacho_nlb.nlb_outbox'` по
			// не-тестовому дереву даёт пусто). Заведётся чистка — эта величина
			// обязана поменяться вместе с ней.
			Retention: subscription.RetainsEverything,
		},
		Mapping: subscription.Mapping{
			// Словарь видов закрыт в обе стороны. Тип объекта модели прав и
			// действие взяты у ПРОИЗВОДИТЕЛЯ (`authzfilter`), а не выписаны.
			//
			// ВНИМАНИЕ на балансировщике: слово журнала и тип модели прав здесь
			// РАЗНЫЕ — `nlb_load_balancer` в таблице против
			// `nlb_network_load_balancer` в модели. Совпадение остальных двух пар
			// делает это различие невидимым на глаз, и вид, унаследовавший чужой
			// тип, спрашивал бы модель не о том объекте — оставаясь «зелёным».
			Kinds: map[string]subscription.Kind{
				kachorepo.OutboxResourceLoadBalancer: {
					ObjectType: authzfilter.ResourceTypeLoadBalancer,
					Action:     authzfilter.ActionLoadBalancerList,
				},
				kachorepo.OutboxResourceListener: {
					ObjectType: authzfilter.ResourceTypeListener,
					Action:     authzfilter.ActionListenerList,
				},
				kachorepo.OutboxResourceTargetGroup: {
					ObjectType: authzfilter.ResourceTypeTargetGroup,
					Action:     authzfilter.ActionTargetGroupList,
				},
			},
			// Словарь родов изменения — все слова, разрешённые ограничением базы
			// (`CHECK (action IN (...))`, миграция 0001). Слово вне словаря делает
			// строку НЕДОСТАВЛЯЕМОЙ, и потеря эта тихая, поэтому перечень взят у
			// ограничения, а не у сегодняшних эмиттеров.
			//
			// Две записи требуют объяснения, иначе их снимут как лишние:
			//
			//   MOVED — переезд между проектами. Отдаётся как правка: подписчик
			//   перечитывает предмет и видит новый проект. Событие переезда всегда
			//   сопровождается собственным UPDATED (эмиттер шлёт оба подряд),
			//   поэтому подписчик получит правку дважды — повтор безвреден, он
			//   идемпотентен, а вот пропуск был бы тихим.
			//
			//   FAILED — производителя в дереве СЕГОДНЯ НЕТ (единственный источник
			//   снят миграцией 0028). Запись оставлена не «на будущее», а ради
			//   ИСТОРИЧЕСКИХ строк долгоживущей базы: без неё они стали бы
			//   недоставляемыми и на каждом догоне писали бы в журнал процесса
			//   расхождение, которого нет. Появится новый производитель — он
			//   попадёт в уже объявленный род, а не заведёт свой.
			Changes: map[string]subscriptionv1.SubscriptionEvent_Change{
				kachorepo.OutboxActionCreated: subscriptionv1.SubscriptionEvent_CREATED,
				kachorepo.OutboxActionUpdated: subscriptionv1.SubscriptionEvent_UPDATED,
				kachorepo.OutboxActionDeleted: subscriptionv1.SubscriptionEvent_DELETED,
				kachorepo.OutboxActionMoved:   subscriptionv1.SubscriptionEvent_UPDATED,
				kachorepo.OutboxActionFailed:  subscriptionv1.SubscriptionEvent_UPDATED,
			},
			State: state,
		},
	}
}

// ProjectGate — страж оси `project_id`.
//
// Форма отказа берётся у производителя форм скрытия, а не сочиняется: она обязана
// быть неотличима от промаха владельца проектов, иначе подписка становится
// способом узнать существование чужого проекта.
func ProjectGate() (subscription.ProjectGate, error) {
	const projectObjectType = "project"
	form, ok := authz.OwnerNotFoundFormat(projectObjectType)
	if !ok {
		return subscription.ProjectGate{}, fmt.Errorf(
			"subscriptionjournal: у типа %q нет формы отсутствия у производителя (pkg/authz): "+
				"страж оси project_id отвечал бы текстом, отличимым от промаха владельца, "+
				"то есть выдавал бы существование чужого проекта", projectObjectType)
	}
	return subscription.ProjectGate{
		ObjectType:     projectObjectType,
		Action:         "iam.projects.get",
		Relations:      []string{"v_get"},
		NotFoundFormat: form,
	}, nil
}

// state — состояние предмета события; у журнала nlb его НЕТ ни у одного рода.
//
// Отсутствие возвращается БЕЗ ошибки: это не сбой сборки, а свойство журнала.
// Ошибка здесь означала бы «состояние есть, но собрать не удалось» — и звала бы
// следующего читателя чинить несуществующую поломку.
//
// Почему не собрать частичный предмет из минимального снимка: контракт формы
// разрешает подписчику читать непустую нагрузку как ПОЛНОЕ состояние. Частичный
// балансировщик означал бы «у него нет ни меток, ни целей, ни адреса» — то есть
// утверждение, которого владелец не делал.
//
// Функция объявлена ПОСТРОЧНОЙ, а не выключенной на весь журнал, потому что
// решение принадлежит РОДУ ПРЕДМЕТА: обогатит nlb нагрузку одного из трёх видов —
// ветка появится здесь, у этого вида, и остальные её не унаследуют.
func state(subscription.Row) (*anypb.Any, error) {
	return nil, nil
}
