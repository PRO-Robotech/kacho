// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// clienttokensurface_test.go — путь токен-эндпоинта зарегистрирован на
// поверхности, объявленной ВНЕШНЕ ДОСЯГАЕМОЙ, и ни на какой другой
// (приёмка F2, сценарий F2-45; ban #6).
package repohygiene_test

import (
	"testing"

	"github.com/PRO-Robotech/kacho/internal/repohygiene"
)

// clientTokenPath — путь эндпоинта так, как он назван в композиционном корне.
//
// Пишется парой «пакет.константа», а не строкой адреса: адрес живёт у пакета
// обработчика одним объявлением, и гейт стережёт МЕСТО регистрации, а не
// значение — значение может смениться, а требование к месту останется.
const clientTokenPath = "clienttokenhttp.TokenPath"

func TestClientTokenEndpointIsRegisteredOnAnExternallyReachableSurface(t *testing.T) {
	regs, census, findings, err := repohygiene.ScanSurfaceRegistrations(
		repohygiene.CompositionRootDir(repoRootFor(t)))
	if err != nil {
		t.Fatalf("разбор композиционного корня: %v", err)
	}

	t.Logf("осмотрено: файлов %d, записей поверхностей %d, регистраций пути %d, из них не связано с поверхностью %d",
		census.Files, census.Surfaces, census.Registrations, census.Unlinked)

	// Предпосылка гейта: он обоснован тем, что корень объявляет поверхности и
	// регистрирует на них пути. Ноль того или другого означает, что гейт не
	// читал ничего, — и это находка, а не тишина.
	if census.Files == 0 {
		t.Fatal("композиционный корень пуст — гейту нечего осматривать")
	}
	if census.Surfaces == 0 {
		t.Fatal("корень не объявляет ни одной поверхности — связать регистрацию не с чем")
	}
	if census.Registrations == 0 {
		t.Fatal("корень не регистрирует ни одного пути — предмет гейта отсутствует")
	}

	for _, f := range findings {
		t.Errorf("%s", f)
	}

	got := repohygiene.RegistrationsOf(regs, clientTokenPath)
	if len(got) != 1 {
		t.Fatalf("путь %s зарегистрирован %d раз(а), ожидалась ровно одна регистрация: "+
			"утверждение о единственном маршруте оставалось бы зелёным, уедь второй не туда",
			clientTokenPath, len(got))
	}
	r := got[0]
	if r.Reach != repohygiene.ExternalReach {
		t.Errorf("%s: путь %s зарегистрирован на поверхности %q с досягаемостью %q, "+
			"а обязан быть на внешне досягаемой: эндпоинт выдачи предъявляют клиенты, "+
			"и поверхность, объявленная внутренней, либо не обслужит их, либо выставит "+
			"наружу то, что объявлено внутренним (ban #6)",
			r.Pos, clientTokenPath, r.SurfaceName, r.Reach)
	}

	// Законный близнец В ТОМ ЖЕ ДЕРЕВЕ: авторитет отзыва живёт на внутренней
	// поверхности, и гейт обязан на нём МОЛЧАТЬ. Без него проверка ловила бы
	// форму — «всякая регистрация обязана быть внешней», — и первый же
	// внутренний маршрут стал бы ложной находкой.
	twin := repohygiene.RegistrationsOf(regs, "tokenintrospecthttp.IntrospectPath")
	if len(twin) == 0 {
		t.Fatal("законный близнец (внутренний маршрут) не найден — гейт остался бы без контроля в обратную сторону")
	}
	for _, tw := range twin {
		if tw.Reach == repohygiene.ExternalReach {
			t.Errorf("%s: авторитет отзыва оказался на внешне досягаемой поверхности %q", tw.Pos, tw.SurfaceName)
		}
	}
}
