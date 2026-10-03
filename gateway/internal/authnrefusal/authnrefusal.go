// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package authnrefusal — ЕДИНСТВЕННЫЙ производитель отказа `401` края: удостоверение
// не принято (приёмка KA1, Р2; kacho#2958).
//
// # Почему один производитель, а не одна строка в каждом
//
// Отказ обязан быть побайтово одинаковым для ВСЕХ причин на своей поверхности:
// нет удостоверения, негодная подпись, чужой издатель, истёкший срок, отзыв, нет
// субъекта, нет привязки, неизвестное или неверное базовое удостоверение, пол
// уровня у полосы без церемонии повышения, негодное доказательство владения.
// Различимый ответ — оракул: по нему вызывающий узнаёт, что именно в его
// удостоверении годно. Причина остаётся в журнале края и только в нём.
//
// Прежде у края было пять писателей `401` с десятком текстов, и неразличимость
// держалась только там, где её помнил автор строки. Свойство, которое держится
// памятью каждого писателя, держится до первого нового писателя. Здесь оно
// держится построением: текста, заголовка и тела в продукте нет нигде, кроме этого
// пакета, а перепись производителей `401` в нетестовом дереве края
// (`internal/repohygiene`, TestEdgeUnauthenticatedHasOneProducer) краснеет на
// втором.
//
// Пакет — лист графа: его зовут слой аутентификации (`middleware`), обработчик
// выхода (`handler`) и ручка потока (`subscriptionstream`), а последняя не может
// импортировать слой аутентификации (тот импортирует её).
//
// # Что сюда НЕ входит
//
// Указание повысить уровень (RFC 9470, `insufficient_user_authentication`) — не
// отказ, а следующий шаг держателю ГОДНОГО удостоверения (Р3); его строит
// `middleware.BuildStepUpChallenge`. Ответ «состояние удостоверения не
// установлено» (`503`, Р1) — тоже не отказ в удостоверении.
package authnrefusal

import (
	"net/http"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// Message — текст отказа на обеих поверхностях.
	Message = "authentication failed"
	// Reason и Domain — `google.rpc.ErrorInfo` отказа. Значение Domain — то же, что
	// несут отказы `403` края (`permission_denied_response.go`); его смена —
	// предмет приёмки XC-1 и делается там одновременно для `401` и `403`.
	Reason = "AUTHN_REQUIRED"
	Domain = "kaname.cloud.iam.v1"
	// Challenge — вызов `WWW-Authenticate`. Соединяет `realm` и `error="invalid_token"`:
	// по последнему консоль повторяет вопрос о сессии и выбирает действие, а
	// `error_description`, нёсший причину, снят. Отступление от «SHOULD NOT»
	// RFC 6750 §3.1 для запроса без удостоверения — решение Р2: неразличимость
	// причин весит больше.
	Challenge = `Bearer realm="kacho", error="invalid_token"`
)

// body — тело REST-отказа. Литерал, а не сериализация на каждом запросе: порядок
// полей и форма обязаны быть одинаковыми побайтово, а у сериализатора JSON
// стабильность порядка не контракт. Согласие с нативной формой держит
// TestRESTBodyIsTheNativeStatus.
const body = `{"code":16,"message":"authentication failed","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"AUTHN_REQUIRED","domain":"kaname.cloud.iam.v1"}]}`

// WriteHTTP пишет отказ REST-поверхности. Носителей сессии не трогает: гашение
// носителя — решение полосы сессии, которая зовёт его ДО этого писателя.
func WriteHTTP(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("WWW-Authenticate", Challenge)
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(body))
}

// Status — отказ нативной поверхности: UNAUTHENTICATED, текст Message и ровно одна
// деталь `ErrorInfo` без `metadata` (прежде там лежали имя глагола и причина
// отказа — то самое, что отказ обязан не различать).
func Status() *status.Status {
	st, err := status.New(codes.Unauthenticated, Message).WithDetails(&errdetails.ErrorInfo{
		Reason: Reason,
		Domain: Domain,
	})
	if err != nil {
		// ErrorInfo — общеизвестный тип; упасть здесь может только сборка без
		// него. Голый статус хуже, чем никакого: он отличим от настоящего
		// отказа, — но паника на пути отказа хуже обоих.
		return status.New(codes.Unauthenticated, Message)
	}
	return st
}

// Err — Status в форме ошибки gRPC.
func Err() error { return Status().Err() }
