// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// bodycapsinglesource_injection_test.go — доказательство того, что
// TestBodyCapIsDeclaredExactlyOnce СПОСОБЕН упасть, и падает он на существе.
//
// Инъекция гоняет ТУ ЖЕ функцию разбора (ScanBodyCapCalls), что и гейт.
//
// Пара обязательна в обе стороны. Первая сторона дешёвая: вернуть вторую
// реализацию. Вторая тяжелее — потребителей у потолка должно быть МНОГО, и
// гейт, который спутает потребителя с реализацией, запретил бы ровно то, ради
// чего пакет-владелец заведён.
package repohygiene

import (
	"strings"
	"testing"
)

// bodyCapInjectedSecondImplementation — ВТОРАЯ реализация потолка, в двух
// написаниях: обычным именем пакета и под псевдонимом.
//
// Псевдоним здесь не украшение: разбор по ИМЕНИ пакета вторую строку не увидел
// бы, и обойти такой гейт можно было бы одной буквой в объявлении импорта.
const bodyCapInjectedSecondImplementation = `package tokenhttp

import (
	"net/http"
	nethttp "net/http"
)

func handle(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	r.Body = nethttp.MaxBytesReader(w, r.Body, 1<<20)
}
`

// bodyCapInjectedLegitimateConsumers — то же место БЕЗ второй реализации,
// вместе с законными соседями.
//
// Соседи, каждый со своим способом обмануть разбор:
//
//   - ТРИ вызова `httpbody.Cap` — потребители, и их сколько угодно;
//   - `httpbody.Cap` под псевдонимом `bodycap` — тот же потребитель, другое имя;
//   - `myhttp.MaxBytesReader` из ЧУЖОГО пакета — одноимённая функция, к
//     `net/http` отношения не имеющая: разбор по имени функции объявил бы её
//     находкой, разбор по пути импорта — нет;
//   - `srv.MaxBytesReader(...)` — метод значения, а не функция пакета.
const bodyCapInjectedLegitimateConsumers = `package tokenhttp

import (
	"net/http"

	"github.com/PRO-Robotech/kacho/pkg/httpbody"
	bodycap "github.com/PRO-Robotech/kacho/pkg/httpbody"
	myhttp "example.com/vendorlib/http"
)

func handleA(w http.ResponseWriter, r *http.Request) {
	if httpbody.Cap(w, r, 1<<20) {
		return
	}
}

func handleB(w http.ResponseWriter, r *http.Request) {
	if httpbody.Cap(w, r, 4<<10) {
		return
	}
}

func handleC(w http.ResponseWriter, r *http.Request, srv *server) {
	if bodycap.Cap(w, r, 8<<10) {
		return
	}
	_ = myhttp.MaxBytesReader(w, r.Body, 1<<20)
	_ = srv.MaxBytesReader(w, r.Body, 1<<20)
}
`

// TestBodyCapScannerFindsASecondImplementation — сторона (а): внесённый дефект
// становится находкой, и находка несёт координату.
func TestBodyCapScannerFindsASecondImplementation(t *testing.T) {
	impls, consumers, census, err := ScanBodyCapCalls(
		"synthetic/tokenhttp/handler.go", []byte(bodyCapInjectedSecondImplementation),
		bodyCapImplementation, bodyCapConsumer)
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if census.Calls == 0 {
		t.Fatalf("осмотрено ноль вызовов — разбирается не то дерево")
	}
	if len(impls) != 2 {
		t.Fatalf("реализаций найдено %d, ожидалось 2 (обычное имя пакета и псевдоним): %+v",
			len(impls), impls)
	}
	for _, s := range impls {
		if s.File == "" || s.Line == 0 {
			t.Errorf("находка без координаты — по такому отказу нечего чинить: %+v", s)
		}
		if s.Callee != bodyCapImplementation {
			t.Errorf("находка описана не тем вызовом: %+v", s)
		}
	}
	if impls[0].Line == impls[1].Line {
		t.Errorf("обе находки на одной строке (%d) — разбор считает вызовы, а не строки: %+v",
			impls[0].Line, impls)
	}
	if len(consumers) != 0 {
		t.Errorf("реализация опознана потребителем (%+v) — тогда гейт запрещал бы "+
			"употребление вместо второй реализации", consumers)
	}

	// И то же самое глазами гейта: место лежит вне владельца, значит это находка.
	outside := 0
	for _, s := range impls {
		if !strings.HasPrefix(s.File, bodyCapOwner) {
			outside++
		}
	}
	if outside != 2 {
		t.Fatalf("вне %s найдено %d реализаций, ожидалось 2 — гейт на этом дефекте "+
			"остался бы зелёным", bodyCapOwner, outside)
	}
}

// TestBodyCapScannerIsSilentOnConsumers — сторона (б): законные потребители,
// сколько бы их ни было, находкой не становятся.
func TestBodyCapScannerIsSilentOnConsumers(t *testing.T) {
	impls, consumers, census, err := ScanBodyCapCalls(
		"synthetic/tokenhttp/handler.go", []byte(bodyCapInjectedLegitimateConsumers),
		bodyCapImplementation, bodyCapConsumer)
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if census.Calls < 5 {
		t.Fatalf("осмотрено вызовов %d — разбирается не то дерево", census.Calls)
	}
	if len(impls) != 0 {
		t.Fatalf("разбор объявил реализацией законное место (%+v). Либо потребитель "+
			"спутан с реализацией — тогда гейт запрещает то, ради чего заведён "+
			"пакет-владелец; либо одноимённая функция ЧУЖОГО пакета принята за "+
			"net/http — тогда разбор судит по имени, а не по пути импорта.", impls)
	}
	if len(consumers) != 3 {
		t.Fatalf("потребителей найдено %d, ожидалось 3 (в том числе под псевдонимом): %+v",
			len(consumers), consumers)
	}
}

// TestBodyCapScannerNamesItsBlindSpot — предпосылка разбора проверяется, а не
// объявляется.
//
// Точечный импорт лишает вызов псевдонима: привести его к пути импорта нечем, и
// разбор его не видит. Это записано слепой зоной в заголовке
// bodycapsinglesource.go; проба держит то утверждение честным — вырастет радиус,
// и здесь станет видно, что заголовок пора править.
//
// Счётчик точечных импортов при этом ОБЯЗАН вырасти: молчание разбора должно
// быть заметно тому, кто читает перепись.
func TestBodyCapScannerNamesItsBlindSpot(t *testing.T) {
	const src = `package tokenhttp

import . "net/http"

func handle(w ResponseWriter, r *Request) {
	r.Body = MaxBytesReader(w, r.Body, 1<<20)
}
`
	impls, _, census, err := ScanBodyCapCalls(
		"synthetic/tokenhttp/dot.go", []byte(src), bodyCapImplementation, bodyCapConsumer)
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if census.DotImports != 1 {
		t.Fatalf("точечных импортов насчитано %d, ожидался 1 — перепись не показывает "+
			"той формы, которой разбор не видит, и его молчание нечем взвесить",
			census.DotImports)
	}
	if len(impls) != 0 {
		t.Fatalf("разбор увидел реализацию за точечным импортом (%+v) — радиус вырос, и "+
			"заголовок bodycapsinglesource.go, объявляющий это слепой зоной, стал ложным. "+
			"Правь заголовок вместе с разбором.", impls)
	}
}
