// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// session_carrier_names.go — ЕДИНСТВЕННОЕ объявление имён носителя браузерной
// сессии, которые край гасит (F4d-26, приёмка Ф3 §4.1 п.9).
//
// # Предмет
//
// Носитель гасится в двух местах: отказом F4d-22 на полосе личности
// (`auth.go`) и обработчиком выхода (`handler/logout_handler.go`). До Ф3 имя
// носителя стояло литералом в обоих — два перечня об одном предмете, и наше имя
// не значилось ни в одном. Второй перечень расходится с первым молча: имя,
// добавленное сюда и забытое там, гасится отказом и переживает выход.
//
// # Два имени, и у каждого свой читатель
//
//   - [OurSessionCarrierName] — печенье НАШЕЙ сессии (Ф3 Р3); читает полоса
//     личности под посадкой `own` (`tryOwnSession`) и маршрут «кто я»;
//   - [providerSessionCarrierName] — печенье прежнего поставщика; читает полоса
//     под `external` (`tryKratosSession`) и тот же маршрут — до S3
//     (`kacho#1276`), когда читатель снимается и имя становится находкой гейта.
//
// Гейт `session_carrier_names_gate_test.go` требует у каждого имени
// ПРОИЗВОДИТЕЛЯ — читателя на пути аутентификации — и объявляет второе
// объявление любого из имён находкой: имя, которое гасят и никто не читает, есть
// печенье, стёртое у клиента без основания.
package middleware

import "net/http"

// OurSessionCarrierName — имя печенья нашей сессии человека, выданной службой
// (Ф3 Р3). Значение непрозрачно для края: он передаёт его службе как есть и
// ничего из него не читает.
const OurSessionCarrierName = "kaname_session"

// providerSessionCarrierName — имя печенья сессии прежнего поставщика
// удостоверений. Читается ТОЛЬКО под посадкой `external`.
const providerSessionCarrierName = "ory_kratos_session"

// sessionCarrierNames — перечень гасимых имён. Элементы — константы выше, не
// литералы: гейт связывает каждое имя с его читателем по идентификатору.
var sessionCarrierNames = []string{OurSessionCarrierName, providerSessionCarrierName}

// SessionCarrierNames отдаёт КОПИЮ перечня — читателям, которым нужен состав, а
// не действие (пробы, самоотчёт). Правка копии перечня не меняет.
func SessionCarrierNames() []string {
	out := make([]string, len(sessionCarrierNames))
	copy(out, sessionCarrierNames)
	return out
}

// EndSessionCarriers заканчивает носитель браузерной сессии — по ВСЕМУ перечню
// и одной формой.
//
// Одно место продукта гасит печенье одним способом: путь отказа F4d-22 и
// обработчик выхода зовут ЭТУ функцию, иначе «выход» и «отказ» оставляли бы у
// одного и того же браузера разное состояние. Атрибуты те же, что у выдачи
// (Ф1 §4.1: `Path=/`, `HttpOnly`, `Secure`, `SameSite=Lax`): браузер сопоставляет
// печенье по имени, пути и домену, и гашение с другим путём не нашло бы его.
func EndSessionCarriers(w http.ResponseWriter) {
	for _, c := range sessionCarrierNames {
		http.SetCookie(w, &http.Cookie{
			Name:     c,
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteLaxMode,
		})
	}
}
