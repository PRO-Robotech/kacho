// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build helmcharts && consolefront

// console_public_front_health_point_test.go — НАСТОЯЩАЯ раздача из рендера
// цепочки не отдаёт служебную точку живости на внешнем входе и отдаёт её на
// внутреннем порту (kacho#3030).
//
// Декларативная проба того же предмета — ui-future/deploy/
// console_health_point_external_test.go: она судит объявление по модели
// порядка разрешения блоков. Это утверждение спрашивает поднятую раздачу саму
// (образ, карта настройки, порты пода — из рендера), и модель разрешения ему не
// нужна. Зовёт его проба формы через TLS-вход
// (TestConsolePublicFrontCarriesTheFormToALegitimateClientOnly): раздача
// поднимается один раз, а конвейер исполняет её поимённо. Стенд оно не
// заменяет — работающую выкатку судит проба после выкатки.
//
// Полоса ассетов модуля в контейнере ведёт в закрытый порт: дошедший до неё
// адрес получил бы `502`, а не `404`, — поэтому `404` на
// `/<модуль>-remote/healthz` доказывает, что адрес до полосы не дошёл.
package deploy_test

import (
	"net/http"
	"testing"
)

// assertHealthPointInsideOnly — внешний вход отказывает `404` на точке живости
// оболочки и на точках модулей через полосы ассетов; внутренний порт отвечает
// `200` (законный близнец: на него смотрит проба кластера).
func assertHealthPointInsideOnly(t *testing.T, c *http.Client, internal, external string) {
	t.Helper()
	get := func(url string) (int, error) {
		resp, err := c.Get(url)
		if err != nil {
			return 0, err
		}
		resp.Body.Close()
		return resp.StatusCode, nil
	}
	for _, cs := range []struct{ path, why string }{
		{"/healthz", "точка живости оболочки"},
		{"/vpc-remote/healthz", "точка живости модуля через полосу ассетов"},
		{"/storage-remote/healthz", "точка живости модуля через полосу ассетов"},
	} {
		code, err := get(external + cs.path)
		switch {
		case err != nil:
			t.Errorf("внешний вход %s: %v", cs.path, err)
		case code != http.StatusNotFound:
			t.Errorf("внешний вход отдаёт %s (%s): код %d, ожидался 404 (kacho#3030)", cs.path, cs.why, code)
		}
	}
	if code, err := get(internal + "/healthz"); err != nil || code != http.StatusOK {
		t.Errorf("внутренняя точка живости: код %d, ошибка %v — проба кластера на ней не прошла бы", code, err)
	}
}
