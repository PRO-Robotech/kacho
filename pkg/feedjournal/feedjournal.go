// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: Apache-2.0

// Package feedjournal — вид ленты извещений в словаре видов журнала подписки
// модуля kacho (приёмка NTF-3, NTF3-65 и NTF3-67; замысел issue-2918 З11).
//
// # Что здесь есть
//
// Одно объявление вида на пять модулей (compute, nlb, registry, storage, vpc):
// ключ журнала [feed.JournalKey] (`notification`) едет на провод видом
// `notification_feed` — словом фундамента ленты [feed.FeedObjectType], а не
// вторым написанием. Вид уровня кластера и без имени: объект строки —
// `notification_feed:<модуль>`, и иного вида фундамент не примет
// (`feed.JournalSignal`).
//
// # Флаг решает, объявлен ли вид
//
// [Declare] кладёт вид в словарь ровно при включённом флаге модуля
// `KACHO_<MODULE>_NOTIFICATIONS_ENABLED`. Флаг прочитан загрузчиком модуля один
// раз; то же значение получают писатели журнала (`journaltx.Options`). При
// выключенном флаге вида в словаре нет, и подписка с `kinds:
// ["notification_feed"]` отвергается фундаментом подписки полным текстом
// (NTF3-65); прочие виды модуля от флага не зависят.
package feedjournal

import (
	"github.com/PRO-Robotech/corelib/notify/feed"
	"github.com/PRO-Robotech/corelib/subscription"
)

// ActionSubscribe — действие, которым поток подписки спрашивает видимость
// строки ленты: право глагола подписки по каталогу прав
// (`corelib.subscription.InternalSubscriptionService/Subscribe`, аннотация
// контракта corelib). Значение — то же, что у источника notify-probe.
const ActionSubscribe = "platform.subscription.subscribe"

// Kind — объявление вида ленты в словаре видов журнала модуля.
func Kind() subscription.Kind {
	return subscription.Kind{
		ObjectType: string(feed.FeedObjectType),
		Action:     ActionSubscribe,
		NameForm:   subscription.NameFormNone,
		Scope:      subscription.ScopeCluster,
	}
}

// Relation — отношение модели прав, дающее видимость строки сигнала ленты
// (`notification_feed:<модуль>`): единственное отношение типа ленты в модели
// службы доступа (`define reader: [service]`), и держит его `service:notify`.
// Видеть сигнал ленты вправе ровно тот, кто вправе её забирать.
const Relation = "reader"

// Relations — предикат видимости сужателя модуля, дополненный типом ленты.
//
// Сужатель модуля спрашивает о строке журнала отношение по ТИПУ объекта, а
// тип, не названный поимённо, получает умолчание карты (`v_get`, у реестра
// `v_list`). У типа ленты такого отношения нет, и служба доступа отказом это
// не считает: план вопроса не строится, ответ — «неизвестно» на всю партию.
// Тогда первая же строка сигнала в журнале модуля обрывала бы поток подписки у
// всякого подписчика, не сузившего виды, и notify не видел бы её никогда.
// Поэтому вид ленты и его отношение объявлены здесь же, рядом: модуль,
// объявивший вид ленты [Declare], строит сужатель потока поверх этой карты.
//
// page не меняется; возвращается копия. Своя запись типа ленты в page —
// ошибка программы модуля: второе написание отношения разошлось бы с этим.
func Relations(page map[string][]string) map[string][]string {
	if _, own := page[string(feed.FeedObjectType)]; own {
		panic("feedjournal.Relations: предикат модуля уже называет тип " + string(feed.FeedObjectType) +
			" — отношение типа ленты объявляется только здесь")
	}
	out := make(map[string][]string, len(page)+1)
	for k, v := range page {
		out[k] = v
	}
	out[string(feed.FeedObjectType)] = []string{Relation}
	return out
}

// Declare объявляет вид ленты в словаре kinds ровно при enabled; при
// выключенном флаге словарь не меняется. kinds — словарь, собранный
// объявлением журнала модуля.
func Declare(kinds map[string]subscription.Kind, enabled bool) {
	if enabled {
		kinds[feed.JournalKey] = Kind()
	}
}
