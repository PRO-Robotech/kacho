// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware

import (
	"testing"
)

// TestLoginLanePaths_F3_51_OneDeclarationFeedsThePublicListAndTheLane — четыре
// глагола формы (Р2) объявлены ОДИН раз, и это объявление читают все, кому
// нужны эти пути: перечень путей без записи каталога (`isPublicHTTPPath`, Р8),
// ветка полосы сессии (Р7) и регистрация ретрансляции (композиционный корень).
//
// Второе объявление тех же четырёх путей разошлось бы с первым молча — путь,
// освобождённый от каталога, но не ретранслируемый, отвечал бы 404 краем; путь
// ретранслируемый, но не освобождённый, отвергался бы каталогом до службы.
func TestLoginLanePaths_F3_51_OneDeclarationFeedsThePublicListAndTheLane(t *testing.T) {
	routes := LoginLaneRoutes()
	// Четыре глагола Ф3 плюс шесть глаголов второго фактора Ф12 (kacho#1281,
	// приёмка Р4): те же полоса, признак и ретрансляция — одно объявление.
	if len(routes) != 10 {
		t.Fatalf("глаголов формы объявлено %d, ожидалось 10 (Ф3 Р2: 4 · Ф12 Р4: 6)", len(routes))
	}
	want := map[string]string{
		"login":                      "/iam/v1/auth/login",
		"logout":                     "/iam/v1/auth/logout",
		"password":                   "/iam/v1/auth/password",
		"csrf":                       "/iam/v1/auth/csrf",
		"second-factor-status":       "/iam/v1/auth/second-factor",
		"second-factor-enroll":       "/iam/v1/auth/second-factor/enroll",
		"second-factor-confirm":      "/iam/v1/auth/second-factor/confirm",
		"second-factor-remove":       "/iam/v1/auth/second-factor/remove",
		"second-factor-backup-codes": "/iam/v1/auth/second-factor/backup-codes",
		"step-up":                    "/iam/v1/auth/step-up",
	}
	for _, rt := range routes {
		if want[rt.Verb] != rt.Path {
			t.Errorf("глагол %q объявлен на пути %q, ожидалось %q", rt.Verb, rt.Path, want[rt.Verb])
		}
		if !IsLoginLanePath(rt.Path) {
			t.Errorf("ветка полосы не узнаёт объявленный путь %q", rt.Path)
		}
		if !isPublicHTTPPath(rt.Path) {
			t.Errorf("перечень путей без записи каталога не несёт глагол формы %q (Р8: восемь путей)", rt.Path)
		}
	}
	// Прежние четыре освобождения на месте — расширение, а не замена.
	for _, p := range []string{"/healthz", "/readyz", "/oauth/logout", "/iam/v1/auth/me"} {
		if !isPublicHTTPPath(p) {
			t.Errorf("прежнее освобождение %q снято", p)
		}
	}
	// Отрицательный контроль: соседний путь того же семейства НЕ освобождён и
	// НЕ глагол формы — точное совпадение, не приставка (§1.8: приставка была
	// снята ровно из-за наследования освобождения).
	for _, p := range []string{"/iam/v1/auth/login/", "/iam/v1/auth/loginx", "/iam/v1/auth", "/iam/v1/auth/csrf/x",
		"/iam/v1/auth/second-factor/", "/iam/v1/auth/second-factor/enrollx", "/iam/v1/auth/step-up/x"} {
		if IsLoginLanePath(p) || isPublicHTTPPath(p) {
			t.Errorf("путь %q признан глаголом формы или освобождённым — совпадение обязано быть точным", p)
		}
	}
	t.Logf("перепись: глаголов формы %d · освобождённых путей всего %d", len(routes), 4+len(routes))
}
