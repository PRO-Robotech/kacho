// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package journal — объявление журнала подписки пробы-источника notify-probe
// для общего сервера потока изменений (`corelib/subscription`).
//
// # Что здесь есть
//
// Только ЗНАЧЕНИЯ: где журнал лежит, каким каналом будит, как его строка
// становится событием общей формы. Курсор, граница устоявшегося, пределы и
// сужение по правам принадлежат общему серверу.
//
// # Вид у журнала один, и это требование, а не пробел
//
// Журнал пробы несёт ровно ключ ленты [feed.JournalKey] (на проводе — вид
// `notification_feed`): строка будит notify, а письма он забирает `Claim`.
// Ресурсов, чьё состояние журнал нёс бы, у пробы нет. Отсюда следствие для
// выключенной доставки: словарь видов без ключа ленты был бы пуст, а пустой
// словарь фундамент отвергает при сборке журнала, — поэтому при выключенном
// флаге журнал не собирается, а сервер подписки не объявляется и не
// монтируется (NTF1-N07 (б)). Решает это корень по тому же флагу, которым
// поднимается сервер ленты.
package journal

import (
	"google.golang.org/protobuf/types/known/anypb"

	subscriptionv1 "github.com/PRO-Robotech/corelib/api/corelib/subscription"
	"github.com/PRO-Robotech/corelib/notify/feed"
	"github.com/PRO-Robotech/corelib/subscription"

	"github.com/PRO-Robotech/kacho/services/notify/cmd/notify-probe/internal/authzfilter"
)

const (
	// Module — имя модуля пробы: объект notification_feed:<Module>, метка
	// метрик ленты, имя пространства шаблонов в манифесте.
	Module = "notify-probe"

	// Service — префикс таблиц ленты, схемо-квалифицированный
	// (<схема>.<служба>): таблицы ленты живут в схеме пробы, а не там, куда
	// укажет путь поиска сессии. Тот же префикс стоит в заголовке миграции
	// ленты, которую написал `notifygen init`.
	Service = "kacho_notifyprobe.notifyprobe"

	// Table — таблица журнала, схемо-квалифицированная.
	Table = "kacho_notifyprobe.notifyprobe_outbox"

	// Channel — канал пробуждения. Он не выводится из имени таблицы:
	// `pg_notify` квалифицированного имени не принимает, и канал триггера
	// назван отдельно (миграция журнала).
	Channel = "notifyprobe_outbox"

	// ChangeUpdated — род изменения строки сигнала ленты. Словарь родов —
	// ровно это слово: другие строки журнал пробы не несёт, и CHECK таблицы
	// держит тот же словарь.
	ChangeUpdated = "UPDATED"
)

// Journal — объявление журнала пробы.
func Journal() subscription.Journal {
	return subscription.Journal{
		Channel: Channel,
		Storage: subscription.Storage{
			Table:          Table,
			PositionColumn: "sequence_no",
			KindColumn:     "resource_kind",
			IDColumn:       "resource_id",
			ChangeColumn:   "event_type",
			PayloadColumn:  "payload",
			// Якоря проекта у строки ленты нет: вид уровня кластера.
			Project: subscription.ProjectAbsent,
			// Журнал ЧИСТИТСЯ: строка пишется на каждую постановку письма, и
			// рост без уборки был бы монотонным. Окно объявлено платформой
			// (subscription.JournalRetention); отметку ставит умолчание колонки
			// часами базы — теми же, которыми судит уборщик.
			Retention:        subscription.RetainsFromEarliestRow,
			AgeColumn:        "created_at",
			InitiatorColumn:  "initiator",
			OccurredAtColumn: "created_at",
		},
		Mapping: subscription.Mapping{
			Kinds: map[string]subscription.Kind{
				feed.JournalKey: {
					ObjectType: authzfilter.ResourceTypeFeed,
					Action:     authzfilter.ActionFeedSubscribe,
					NameForm:   subscription.NameFormNone,
					Scope:      subscription.ScopeCluster,
				},
			},
			Changes: map[string]subscriptionv1.SubscriptionEvent_Change{
				ChangeUpdated: subscriptionv1.SubscriptionEvent_UPDATED,
			},
			// Состояния у ленты нет словом, а не пустым полем: событие будит
			// notify, строки он забирает Claim.
			State: func(subscription.Row) (*anypb.Any, subscription.StateAbsence, error) {
				return nil, subscription.StateNotProduced, nil
			},
		},
	}
}
