// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// readinesscarrier_injection_test.go — ИНЪЕКЦИЯ: обе стороны гоняют ТУ ЖЕ
// функцию, что и гейт по дереву (`scanReadinessCarriers`), а не её копию.
//
// # Почему прогонов ТРИ, а не два
//
// У гейта ДВЕ оси: «монтирование построено объявленным носителем» и «свой тип
// именованной проверки в том же пакете не объявлен». Инъекция, роняющая обе
// сразу, ничего не доказывает про каждую: красное пришло бы от соседа, и любая
// из осей могла бы оказаться вакуумной, не показав этого ничем
// (`testing.md` §«Гейт на класс», п.2в).
//
// Поэтому:
//
//	контроль          — всё цело: молчат ОБЕ оси;
//	инъекция новой    — снято ТОЛЬКО построение носителем: краснеет ось 1, ось 2 молчит;
//	инъекция соседней — заведён ТОЛЬКО свой тип: краснеет ось 2, ось 1 молчит.
//
// Форма инъекции — «снять свойство у элемента, чьё соседнее на месте», а не
// «завести ещё один элемент»: новый элемент нарушал бы всё, что требуется от
// элементов вообще.

import (
	"strings"
	"testing"
)

// carrierMountSrc — законная форма: носитель импортирован, обработчик произведён
// им же.
const carrierMountSrc = `package main

import (
	"net/http"

	"github.com/PRO-Robotech/kacho/pkg/observability/health"
)

func diagnosticMux(agg *health.Aggregator) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("GET /healthz", agg.LiveHandler())
	mux.Handle("GET /readyz", agg.ReadyHandler())
	return mux
}
`

// ownMountSrc — дефект оси 1: свой обработчик, носитель не при чём.
const ownMountSrc = `package main

import "net/http"

func readinessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }
}

func diagnosticMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/readyz", readinessHandler())
	return mux
}
`

// ownCheckerTypeSrc — дефект оси 2: свой тип именованной проверки, лежащий в том
// же пакете, что монтирование.
const ownCheckerTypeSrc = `package main

import "context"

type ReadinessChecker struct {
	Name  string
	Check func(context.Context) error
}
`

// Контроль: всё цело — молчат ОБЕ оси. Без него красное соседней пробы
// неотличимо от красного гейта, а зелёное — от гейта, не способного упасть.
func TestReadinessCarrierGateSilentOnAConformingTree(t *testing.T) {
	root := synthCarrierTree(t, map[string]string{
		"services/x/cmd/x/observability.go": carrierMountSrc,
	})
	reach, err := scanReadinessCarriers(root)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	if reach.filesRead == 0 {
		t.Fatal("синтетическое дерево не прочитано — инъекция беспредметна")
	}
	if len(reach.services) != 1 {
		t.Fatalf("сервисов, поднимающих /readyz, найдено %d — ожидался один: %v",
			len(reach.services), reach.services)
	}
	if !reach.mounts["x"].byCarrier {
		t.Fatalf("гейт нашёл дефект в законном дереве: обработчик произведён носителем (%s), "+
			"а не засчитан", reach.mounts["x"].coord)
	}
	if reach.mounts["x"].localCheckerType != "" {
		t.Fatalf("своего типа проверки нет, а гейт его нашёл: %q", reach.mounts["x"].localCheckerType)
	}
}

// Инъекция ОСИ 1: снято построение носителем — краснеет ось 1, ось 2 молчит.
func TestReadinessCarrierGateRedWhenTheMountIsNotBuiltByTheCarrier(t *testing.T) {
	root := synthCarrierTree(t, map[string]string{
		"services/x/cmd/x/observability.go": ownMountSrc,
	})
	reach, err := scanReadinessCarriers(root)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	m, ok := reach.mounts["x"]
	if !ok {
		t.Fatalf("монтирование /readyz не найдено вовсе — распознаватель не знает формы "+
			"`HandleFunc(\"/readyz\", …)`, и всё записанное в ней вне наблюдения: %v", reach.services)
	}
	if m.byCarrier {
		t.Fatal("обработчик свой, а гейт засчитал его носителем — ось 1 вакуумна")
	}
	if !strings.Contains(m.coord, "services/x/cmd/x/observability.go") {
		t.Fatalf("находка не называет координату — по ней нечего чинить: %q", m.coord)
	}
	// Ось 2 при этом обязана молчать: инъекция роняет ТОЛЬКО проверяемое.
	if m.localCheckerType != "" {
		t.Fatalf("инъекция оси 1 уронила ось 2 (%q) — красное придёт от соседа, и о самой оси 1 "+
			"вердикт ничего не скажет", m.localCheckerType)
	}
}

// Инъекция ОСИ 2: монтирование законное, но рядом заведён свой тип именованной
// проверки — краснеет ось 2, ось 1 молчит.
func TestReadinessCarrierGateRedOnALocalCheckerTypeBesideALegitimateMount(t *testing.T) {
	root := synthCarrierTree(t, map[string]string{
		"services/x/cmd/x/observability.go": carrierMountSrc,
		"services/x/cmd/x/checker.go":       ownCheckerTypeSrc,
	})
	reach, err := scanReadinessCarriers(root)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	m := reach.mounts["x"]
	if !m.byCarrier {
		t.Fatal("инъекция оси 2 уронила ось 1 — красное придёт от соседа")
	}
	if m.localCheckerType == "" {
		t.Fatal("свой тип именованной проверки объявлен, а гейт его не нашёл — ось 2 вакуумна")
	}
	if !strings.Contains(m.localCheckerType, "checker.go") {
		t.Fatalf("находка оси 2 не называет координату типа: %q", m.localCheckerType)
	}
}

// Отрицательный контроль РАСПОЗНАВАНИЯ, часть 1: вызов `ReadyHandler()` без
// импорта носителя носителем не делает — иначе одноимённый метод чужого типа
// прошёл бы за общий носитель.
func TestReadinessCarrierGateIgnoresAReadyHandlerCallWithoutTheImport(t *testing.T) {
	root := synthCarrierTree(t, map[string]string{
		"services/x/cmd/x/observability.go": `package main

import "net/http"

type own struct{}

func (own) ReadyHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }
}

func diagnosticMux(a own) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("GET /readyz", a.ReadyHandler())
	return mux
}
`,
	})
	reach, err := scanReadinessCarriers(root)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	if reach.mounts["x"].byCarrier {
		t.Fatal("одноимённый метод СВОЕГО типа засчитан объявленным носителем — тогда носителем " +
			"станет всякий, кто назовёт свой метод так же")
	}
}

// Отрицательный контроль РАСПОЗНАВАНИЯ, часть 2: импорт носителя без вызова
// носителем не делает — иначе импорт ради константы зеленил бы гейт.
func TestReadinessCarrierGateIgnoresTheImportWithoutTheCall(t *testing.T) {
	root := synthCarrierTree(t, map[string]string{
		"services/x/cmd/x/observability.go": `package main

import (
	"net/http"

	"github.com/PRO-Robotech/kacho/pkg/observability/health"
)

var errWired = health.ErrDependencyNotWired

func diagnosticMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	return mux
}
`,
	})
	reach, err := scanReadinessCarriers(root)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	if reach.mounts["x"].byCarrier {
		t.Fatal("импорт носителя без вызова засчитан построением — гейт зеленел бы на импорте " +
			"ради константы")
	}
}

// Отрицательный контроль ТЕКСТА: путь в комментарии монтированием не является.
//
// `/readyz` встречается в этом дереве в шапках, комментариях и в перечне
// публичных путей края. Текстовый поиск принял бы объяснение за исполнение —
// тот самый класс, ради которого гейт разбирает синтаксическое дерево.
func TestReadinessCarrierGateIgnoresThePathInAComment(t *testing.T) {
	root := synthCarrierTree(t, map[string]string{
		"services/x/cmd/x/doc.go": `package main

// Здесь когда-нибудь появится "/readyz" — но пока его не монтирует никто.
const why = "/readyz"
`,
	})
	reach, err := scanReadinessCarriers(root)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	if reach.filesRead == 0 {
		t.Fatal("синтетическое дерево не прочитано — контроль беспредметен")
	}
	if len(reach.services) != 0 {
		t.Fatalf("монтированием засчитано УПОМИНАНИЕ пути (%v): гейт читает текст, а не код",
			reach.services)
	}
}

// Обе законные формы образца распознаются: голый путь и путь с методом.
//
// Форма, о которой распознаватель не знает, не даёт ни красного, ни зелёного —
// она МОЛЧИТ, и всё записанное в ней оказывается вне наблюдения
// (`testing.md` §«Гейт на класс», п.7). В этом дереве обе формы законны и обе
// встречаются.
func TestReadinessCarrierGateKnowsBothPatternForms(t *testing.T) {
	for name, pattern := range map[string]string{"голый путь": "/readyz", "путь с методом": "GET /readyz"} {
		t.Run(name, func(t *testing.T) {
			root := synthCarrierTree(t, map[string]string{
				"services/x/cmd/x/observability.go": `package main

import (
	"net/http"

	"github.com/PRO-Robotech/kacho/pkg/observability/health"
)

func diagnosticMux(agg *health.Aggregator) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("` + pattern + `", agg.ReadyHandler())
	return mux
}
`,
			})
			reach, err := scanReadinessCarriers(root)
			if err != nil {
				t.Fatalf("обход синтетического дерева: %v", err)
			}
			if len(reach.services) != 1 {
				t.Fatalf("форма %q распознавателю неизвестна — записанное в ней вне наблюдения", pattern)
			}
		})
	}
}

// Предпосылка, часть 1: «ноль прочитанного» отличимо от «ноль находок».
//
// Гейт на таком обходе обязан отказать (`filesRead == 0` — его собственный
// `Fatalf`), а не объявить дерево чистым.
func TestReadinessCarrierScanReportsAnEmptyWalk(t *testing.T) {
	root := synthCarrierTree(t, map[string]string{"services/README.md": "нет ни одного файла Go\n"})
	reach, err := scanReadinessCarriers(root)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	if reach.filesRead != 0 || len(reach.services) != 0 {
		t.Fatalf("на дереве без Go-файлов обход насчитал прочитанных=%d, поднимающих=%d — "+
			"перепись не отражает объём осмотренного", reach.filesRead, len(reach.services))
	}
}

// Предпосылка, часть 2: каталога сервисов НЕТ — это ОТКАЗ обхода, а не пустой
// результат. Иначе переезд каталога читался бы как «нарушений не найдено».
func TestReadinessCarrierScanRefusesWhenTheServicesDirIsGone(t *testing.T) {
	root := synthCarrierTree(t, map[string]string{"README.md": "каталога services/ нет вовсе\n"})
	if _, err := scanReadinessCarriers(root); err == nil {
		t.Fatal("каталога сервисов нет, а обход вернул успех — переезд каталога читался бы " +
			"как «нарушений не найдено»")
	}
}
