// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package client

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// OutcomeKind — исход вызова края.
//
// Корзины «прочее» здесь НЕТ намеренно. Неклассифицированный ответ — это состояние, а не
// третья политика: он останавливает apply, называя полученное дословно, и не трактуется ни
// как отсутствие ресурса, ни как повод повторить. Корзина «прочее» тем и опасна, что
// выбирает политику за того, кто её не выбирал.
type OutcomeKind int

const (
	// OutcomeOK — край ответил успехом.
	OutcomeOK OutcomeKind = iota

	// OutcomeMalformed — ответ НЕ несёт конверта google.rpc.Status.
	//
	// Это ошибка НАСТРОЙКИ, а не отсутствие ресурса: так отвечает чужой прокси, ingress
	// без маршрута или опечатка в адресе. Отдельный исход существует ровно потому, что
	// «404 от неверного адреса» и «404 от владельца» неразличимы по коду, и первый,
	// принятый за второй, означает «вся инфраструктура удалена».
	OutcomeMalformed

	// OutcomeNotFound — владелец сообщает, что ресурса нет. Только этот исход вправе
	// участвовать в подтверждении отсутствия.
	OutcomeNotFound

	// OutcomeDenied — отказ в доступе. На свежесозданном ресурсе может быть окном
	// материализации прав; в остальных местах — настоящий отказ.
	OutcomeDenied

	// OutcomeUnauthenticated — учётные данные негодны. НЕ ретраится: ожидание здесь
	// маскировало бы истёкший или подменённый токен под окно прав.
	OutcomeUnauthenticated

	// OutcomeConflict — имя занято. Никогда не обрабатывается усыновлением: усыновление
	// применяется только после потерянного ответа, а не как реакция на любой конфликт.
	OutcomeConflict

	// OutcomeInvalid — край отверг запрос по существу (валидация, состояние ресурса).
	OutcomeInvalid

	// OutcomeRetryable — единственный безусловно повторяемый случай: край недоступен.
	OutcomeRetryable

	// OutcomeTerminal — ответ распознан, но продолжать нельзя.
	OutcomeTerminal
)

// String — для диагностики и текстов ошибок.
func (k OutcomeKind) String() string {
	switch k {
	case OutcomeOK:
		return "OK"
	case OutcomeMalformed:
		return "Malformed"
	case OutcomeNotFound:
		return "NotFound"
	case OutcomeDenied:
		return "Denied"
	case OutcomeUnauthenticated:
		return "Unauthenticated"
	case OutcomeConflict:
		return "Conflict"
	case OutcomeInvalid:
		return "Invalid"
	case OutcomeRetryable:
		return "Retryable"
	case OutcomeTerminal:
		return "Terminal"
	}
	return fmt.Sprintf("OutcomeKind(%d)", int(k))
}

// Outcome — классифицированный ответ.
type Outcome struct {
	Kind OutcomeKind
	// Code — код из конверта google.rpc.Status; -1, если конверта не было.
	Code int
	// Message — сообщение края дословно либо диагностика провайдера.
	Message string
}

// статус — минимальная форма конверта. Разбор ТЕРПИМ к неизвестным полям: край добавляет
// их вперёд провайдера, и строгий разбор ломал бы каждое чтение на первом же новом поле.
type statusEnvelope struct {
	Code    *int   `json:"code"`
	Message string `json:"message"`
}

// Classify устанавливает СНАЧАЛА форму ответа и лишь затем его смысл.
//
// Порядок принципиален. Смысл кодов определён только для ответов края; для всего
// остального «404» — это факт о сети и настройке, а не о существовании ресурса.
func Classify(resp *Response) Outcome {
	if resp == nil {
		return Outcome{Kind: OutcomeMalformed, Code: -1,
			Message: "ответа нет вовсе — проверьте адрес края (endpoint) и доступность сети"}
	}

	env, ok := parseEnvelope(resp)
	if !ok {
		ct := resp.ContentType
		if ct == "" {
			ct = "<без типа>"
		}
		preview := strings.TrimSpace(string(resp.Body))
		if len(preview) > 120 {
			preview = preview[:120] + "…"
		}
		return Outcome{
			Kind: OutcomeMalformed,
			Code: -1,
			Message: fmt.Sprintf(
				"ответ HTTP %d не является ответом края Kachō (тип %s, начало тела: %q). "+
					"Это ошибка настройки: проверьте адрес края (endpoint) — так отвечает чужой "+
					"прокси или маршрут, за которым Kachō нет. Такой ответ НЕ означает, что "+
					"ресурс удалён",
				resp.StatusCode, ct, preview),
		}
	}

	code := -1
	if env.Code != nil {
		code = *env.Code
	}
	msg := env.Message

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated, http.StatusAccepted, http.StatusNoContent:
		return Outcome{Kind: OutcomeOK, Code: code, Message: msg}
	case http.StatusNotFound:
		return Outcome{Kind: OutcomeNotFound, Code: code, Message: msg}
	case http.StatusForbidden:
		return Outcome{Kind: OutcomeDenied, Code: code, Message: msg}
	case http.StatusUnauthorized:
		return Outcome{Kind: OutcomeUnauthenticated, Code: code, Message: msg}
	case http.StatusConflict:
		return Outcome{Kind: OutcomeConflict, Code: code, Message: msg}
	case http.StatusBadRequest:
		// 400 производят и INVALID_ARGUMENT, и FAILED_PRECONDITION, и OUT_OF_RANGE —
		// поэтому смысл несёт код, а не статус. Обе линии для провайдера терминальны:
		// повтор идентичного запроса не пройдёт.
		return Outcome{Kind: OutcomeInvalid, Code: code, Message: msg}
	case http.StatusServiceUnavailable:
		return Outcome{Kind: OutcomeRetryable, Code: code, Message: msg}
	}

	return Outcome{
		Kind: OutcomeTerminal,
		Code: code,
		Message: fmt.Sprintf("край ответил HTTP %d %s (код %d): %s",
			resp.StatusCode, http.StatusText(resp.StatusCode), code, msg),
	}
}

// parseEnvelope — распознан ли ответ как ответ края.
//
// Признак — разбираемый JSON-объект с полем code. Он есть у каждого ответа края: успех
// несёт его нулём, отказ — кодом gRPC. HTML прокси, пустое тело и чужой JSON признака не
// имеют, и это ровно то различение, ради которого функция существует.
func parseEnvelope(resp *Response) (statusEnvelope, bool) {
	body := strings.TrimSpace(string(resp.Body))
	if body == "" || !strings.HasPrefix(body, "{") {
		return statusEnvelope{}, false
	}
	var env statusEnvelope
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		return statusEnvelope{}, false
	}
	if env.Code == nil {
		// Успешные ответы ресурсов (список, объект) поля code не несут — но они и не
		// проходят через классификацию как ошибки: успех распознаётся по статусу.
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			zero := 0
			return statusEnvelope{Code: &zero}, true
		}
		return statusEnvelope{}, false
	}
	return env, true
}
