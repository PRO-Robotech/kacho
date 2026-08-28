// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionstream_test

import (
	"strings"
	"testing"

	subscriptionv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"
)

// TestKindDictionaryReachesTheBrowserFrame — словарь видов доезжает до браузера
// ТЕМ ЖЕ кадром, которым доезжает позиция, и без единой правки проекции.
//
// # Зачем это утверждение отдельно, если проекция сериализует сообщение целиком
//
// Именно потому, что целиком: свойство «новое поле контракта видно клиенту»
// держится здесь НЕ кодом края, а тем, как собран сериализатор. Значит его
// нечем заметить при правке — и ровно так поле контракта уже однажды не выходило
// наружу, потому что край не пересобрали. Утверждение стоит на КАДРЕ, а не на
// сообщении: между ними лежит сериализация, и предметом является она.
//
// # Отрицательная половина
//
// Рядом утверждается, что кадр НЕ несёт слова хранилища владельца. Без неё
// проба зеленела бы на крае, который отдаёт оба написания сразу, — а именно это
// и было бы возвращением дефекта, только по другому пути.
func TestKindDictionaryReachesTheBrowserFrame(t *testing.T) {
	owner := &ownerStub{script: []*subscriptionv1.SubscriptionMessage{
		{
			Message: &subscriptionv1.SubscriptionMessage_Opened{
				Opened: &subscriptionv1.SubscriptionOpened{
					Position:          "pos-0",
					CaughtUp:          true,
					RetainsEverything: true,
					KnownKinds:        []string{"probe_instance", "probe_network_load_balancer"},
				},
			},
		},
	}}
	h := newHandler(t, owner)

	rec := serve(t, h, request("owner=probe"))
	got := frames(t, rec.Body.String())
	if len(got) == 0 {
		t.Fatalf("кадров ноль: %q", rec.Body.String())
	}
	if got[0].event != "opened" {
		t.Fatalf("первый кадр %q, ожидалось служебное сообщение открытия", got[0].event)
	}
	for _, kind := range []string{"probe_instance", "probe_network_load_balancer"} {
		if !strings.Contains(got[0].data, kind) {
			t.Errorf("кадр открытия не несёт вида %q — клиенту в браузере словарь читать неоткуда: %s",
				kind, got[0].data)
		}
	}
	if !strings.Contains(got[0].data, "knownKinds") {
		t.Errorf("кадр открытия не называет поле словаря: %s", got[0].data)
	}
	// Слово ХРАНИЛИЩА владельца сюда не попадает ни при каком устройстве: его
	// нет в сообщении вовсе.
	if strings.Contains(got[0].data, "\"Instance\"") {
		t.Errorf("кадр несёт слово хранилища — у одного предмета два написания: %s", got[0].data)
	}
}
