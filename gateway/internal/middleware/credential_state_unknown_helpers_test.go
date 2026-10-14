// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware

import (
	"net/http"
	"net/http/httptest"
)

// credentialStateUnknownBody — тело ответа Р1 приёмки KA1, произведённое его
// единственным писателем: эталон, с которым ответ каждой полосы на молчание
// нашего авторитета обязан совпасть побайтово.
func credentialStateUnknownBody() string {
	rec := httptest.NewRecorder()
	writeCredentialStateUnknown(rec)
	return rec.Body.String()
}

// isCredentialStateUnknown — ответ полосы есть ответ Р1: 503, эталонное тело,
// без вызова и без Set-Cookie (носитель цел).
func isCredentialStateUnknown(rec *httptest.ResponseRecorder) bool {
	return rec.Code == http.StatusServiceUnavailable &&
		rec.Body.String() == credentialStateUnknownBody() &&
		rec.Header().Get("WWW-Authenticate") == "" &&
		len(rec.Result().Header["Set-Cookie"]) == 0
}
