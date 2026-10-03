// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware

// credential_state_unknown.go — ОДИН ответ края на молчание НАШЕГО авторитета
// (приёмка KA1, Р1; kacho#2728).
//
// # Предмет
//
// Полос, которые спрашивают наш авторитет о предъявленном, три: предъявитель
// (оба читателя отзыва — авторитет нашей чеканки и запись отзыва), наша
// браузерная сессия (вопрос о сессии и вопрос об отсечке) и базовое
// удостоверение. «Не ответил» на каждой — одно состояние: нет ответа в пределах
// бюджета, UNAVAILABLE, UNIMPLEMENTED на вопрос, без которого полоса не может
// решить, ответ не той формы. И ответ на него один: `503` / UNAVAILABLE, один
// текст, без вызова `WWW-Authenticate` и без `Set-Cookie` — носитель цел.
//
// Прежде полосы отвечали по-разному: предъявитель — `503` своим текстом, базовое
// удостоверение — `503` текстом, но не JSON, сессия — `401` текстом отказа с
// вызовом. Последнее посылало человека чинить исправное: консоль на прочем `401`
// уводит на экран входа при живой сессии. Довод «различимый ответ — оракул
// исправности соседа» в этом дереве не держится: та же величина отдаётся
// общедоступным `/readyz`.
//
// Текст — тот, что уже производила полоса базового удостоверения: он верен для
// всех трёх вопросов (о сессии, об отсечке, об отзыве), а прежний текст полосы
// предъявителя — только для одного.
//
// Сюда НЕ входит недоступный набор проверочных ключей (`keySourceUnavailableReason`):
// это не вопрос нашему авторитету о предъявленном, и у него свой текст.

import (
	"net/http"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// credentialStateUnknownReason — текст ответа Р1 на обеих поверхностях.
const credentialStateUnknownReason = "credential state could not be established" // #nosec G101 -- текст ответа на молчание авторитета, а не удостоверение

// writeCredentialStateUnknown — ответ Р1 REST-поверхности: единственный писатель
// для всех трёх полос.
func writeCredentialStateUnknown(w http.ResponseWriter) {
	writeHTTPServiceUnavailable(w, credentialStateUnknownReason)
}

// credentialStateUnknownError — ответ Р1 нативной поверхности: тот же текст, без
// деталей.
func credentialStateUnknownError() error {
	return status.Error(codes.Unavailable, credentialStateUnknownReason)
}
