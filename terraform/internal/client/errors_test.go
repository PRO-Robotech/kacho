// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package client

import (
	"net/http"
	"strings"
	"testing"
)

// Сценарий 35 приёмки — САМЫЙ ДОРОГОЙ подслучай чтения.
//
// Ответ «не найдено» от НЕВЕРНОГО АДРЕСА неотличим по коду от ответа владельца. Без
// проверки формы опечатка в адресе края превращается в «вся инфраструктура удалена», и
// провайдер это добросовестно применит. Поэтому форма устанавливается ПЕРВОЙ, и только
// опознанный конверт доезжает до логики отсутствия.
func TestNonEnvelopeResponseIsMisconfiguration(t *testing.T) {
	cases := []struct {
		name string
		resp Response
	}{
		{"HTML от чужого прокси", Response{
			StatusCode: http.StatusNotFound, ContentType: "text/html",
			Body: []byte("<html><body>404 Not Found</body></html>")}},
		{"пустое тело", Response{StatusCode: http.StatusNotFound, Body: nil}},
		{"JSON без поля code", Response{
			StatusCode: http.StatusNotFound, ContentType: "application/json",
			Body: []byte(`{"error":"nope"}`)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Classify(&tc.resp)
			if got.Kind != OutcomeMalformed {
				t.Errorf("получено %v, ожидалось OutcomeMalformed.\n"+
					"Ответ без конверта не вправе участвовать в решении о снятии ресурса из "+
					"состояния: это ошибка настройки, а не отсутствие ресурса.", got.Kind)
			}
			if !strings.Contains(strings.ToLower(got.Message), "адрес") {
				t.Errorf("текст не указывает на настройку адреса: %q", got.Message)
			}
		})
	}
}

// Парный положительный: настоящий конверт края доезжает до логики отсутствия.
// Без него предыдущий тест зеленел бы на классификаторе, объявляющем всё подряд
// ошибкой настройки.
func TestEnvelopeNotFoundIsRecognised(t *testing.T) {
	resp := Response{
		StatusCode:  http.StatusNotFound,
		ContentType: "application/json",
		Body:        []byte(`{"code":5,"message":"Network net123 not found","details":[]}`),
	}
	got := Classify(&resp)
	if got.Kind != OutcomeNotFound {
		t.Fatalf("получено %v, ожидалось OutcomeNotFound", got.Kind)
	}
	if !strings.Contains(got.Message, "not found") {
		t.Errorf("сообщение края потеряно: %q", got.Message)
	}
}

// Сценарий 36: у классификатора НЕТ корзины «прочее».
//
// Неклассифицированный ответ — состояние, а не третья политика: apply останавливается,
// называя полученный статус дословно, и такой ответ не трактуется ни как «ресурса нет»,
// ни как «повторим».
func TestUnknownStatusIsTerminalAndNamed(t *testing.T) {
	for _, code := range []int{http.StatusTooManyRequests, http.StatusInternalServerError,
		http.StatusNotImplemented, http.StatusGatewayTimeout} {
		resp := Response{StatusCode: code, ContentType: "application/json",
			Body: []byte(`{"code":13,"message":"boom"}`)}
		got := Classify(&resp)
		if got.Kind == OutcomeRetryable {
			t.Errorf("HTTP %d объявлен повторяемым — повторяем ТОЛЬКО 503", code)
		}
		if got.Kind == OutcomeNotFound {
			t.Errorf("HTTP %d принят за отсутствие ресурса", code)
		}
		if !strings.Contains(got.Message, http.StatusText(code)) &&
			!strings.Contains(got.Message, "13") && !strings.Contains(got.Message, "boom") {
			t.Errorf("HTTP %d: текст не называет полученное дословно: %q", code, got.Message)
		}
	}
}

// 503 — единственный безусловно повторяемый код (сценарий 31).
func TestUnavailableIsTheOnlyRetryable(t *testing.T) {
	resp := Response{StatusCode: http.StatusServiceUnavailable, ContentType: "application/json",
		Body: []byte(`{"code":14,"message":"peer unavailable"}`)}
	if got := Classify(&resp); got.Kind != OutcomeRetryable {
		t.Errorf("503 не признан повторяемым: %v", got.Kind)
	}
}

// Отказ в доступе и конфликт имени различаются: первый участвует в подтверждении
// отсутствия, второй — никогда (сценарий 30: усыновление не является обработчиком 409).
func TestDeniedAndConflictAreDistinct(t *testing.T) {
	denied := Response{StatusCode: http.StatusForbidden, ContentType: "application/json",
		Body: []byte(`{"code":7,"message":"no authorization path"}`)}
	if got := Classify(&denied); got.Kind != OutcomeDenied {
		t.Errorf("403: получено %v, ожидалось OutcomeDenied", got.Kind)
	}
	conflict := Response{StatusCode: http.StatusConflict, ContentType: "application/json",
		Body: []byte(`{"code":6,"message":"already exists"}`)}
	if got := Classify(&conflict); got.Kind != OutcomeConflict {
		t.Errorf("409: получено %v, ожидалось OutcomeConflict", got.Kind)
	}
}

// 401 отдельно от 403 (сценарий 13): истёкший или подменённый токен не ретраится вовсе
// и сообщается своим текстом, иначе он выглядел бы как окно материализации прав.
func TestUnauthenticatedIsItsOwnOutcome(t *testing.T) {
	resp := Response{StatusCode: http.StatusUnauthorized, ContentType: "application/json",
		Body: []byte(`{"code":16,"message":"credentials required"}`)}
	got := Classify(&resp)
	if got.Kind != OutcomeUnauthenticated {
		t.Fatalf("получено %v, ожидалось OutcomeUnauthenticated", got.Kind)
	}
	if got.Kind == OutcomeDenied {
		t.Error("401 слит с 403 — тогда ожидание окна прав маскировало бы негодный токен")
	}
}

// Успех остаётся успехом.
func TestSuccessIsOK(t *testing.T) {
	resp := Response{StatusCode: http.StatusOK, ContentType: "application/json", Body: []byte(`{}`)}
	if got := Classify(&resp); got.Kind != OutcomeOK {
		t.Errorf("200 не признан успехом: %v", got.Kind)
	}
}

// Сценарий 41: разбор ответа терпим к неизвестным полям — край добавляет их вперёд
// провайдера, и строгий разбор ломал бы каждое чтение на первом же новом поле.
func TestDecodeToleratesUnknownFields(t *testing.T) {
	resp := Response{StatusCode: http.StatusOK, ContentType: "application/json",
		Body: []byte(`{"code":0,"message":"","brandNewFieldFromTheFuture":42}`)}
	if got := Classify(&resp); got.Kind != OutcomeOK {
		t.Errorf("неизвестное поле сломало разбор: %v (%s)", got.Kind, got.Message)
	}
}
