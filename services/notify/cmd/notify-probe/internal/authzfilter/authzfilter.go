// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package authzfilter — словарь пробы notify-probe для общего сужателя потока
// подписки (`corelib/listnarrow`): тип объекта ленты, действие вопроса и
// предикат видимости строки журнала.
//
// Здесь, а не в объявлении журнала: журнал берёт слова у производителя, а не у
// своего пакета, — второе написание чужого словаря расходится молча.
package authzfilter

import "github.com/PRO-Robotech/corelib/notify/feed"

// ResourceTypeFeed — тип объекта модели прав ленты: объект проверки Claim и
// Ack и предмет сигнала журнала. Слово берётся у фундамента ленты.
const ResourceTypeFeed = string(feed.FeedObjectType)

// ActionFeedSubscribe — действие, которым поток спрашивает видимость строки
// ленты: разрешение глагола подписки по каталогу прав
// (`corelib.subscription.InternalSubscriptionService/Subscribe`).
const ActionFeedSubscribe = "platform.subscription.subscribe"

// RelationReader — отношение модели, дающее видимость ленты: то же, которого
// каталог прав требует у Claim и Ack. Видеть сигнал ленты вправе ровно тот,
// кто вправе её забирать.
const RelationReader = "reader"

// PageRelations — предикат видимости строки журнала по типу объекта. Тип у
// журнала пробы один; иного типа поток не несёт, и умолчания нет: строка
// чужого типа не видна никому.
var PageRelations = map[string][]string{
	ResourceTypeFeed: {RelationReader},
}
