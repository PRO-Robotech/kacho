// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_dev_proxy_lane_test.go — КАЖДАЯ полоса сборщика, ведущая к службе
// личности, объявлена и раздачей стенда (задача #2733).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// У консоли два разных объявления одного и того же: карта проксирования
// сборщика (`vite.config.ts`, разработка) и раздача образа
// (`configmap-nginx.yaml`, стенд). Сосед по файлу —
// `TestServingBandCoversEveryRouteTheConsoleSendsToTheIdentityService` —
// сверяет ОДНУ полосу: экраны входа (`kratosUiRoutes`). Полосы, ведущие к
// публичному КРАЮ службы личности, он не читает вовсе, и расхождение по ним
// живёт молча.
//
// Цена измерена на дереве: сборщик объявлял полосу `"/self-service"` к краю
// службы личности, а раздача такого блока не объявляет НИ ОДНОГО. Адресата у
// этой полосы в консоли тоже нет — клиент ходит под приставкой
// `/.ory/kratos/public`, — но существование полосы означает ровно то, что первый
// же код, написавший голый `/self-service`, работал бы в разработке и уходил бы
// на стенде в заглушку одностраничного приложения: код `200`, пустая оболочка,
// отказ выглядит успехом. Различие такого рода обнаруживается не раньше стенда.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДИКАТ, И ОН РЕЖЕТ В ОБЕ СТОРОНЫ
//
// Сторона сборщика: ключ карты проксирования, чья цель — переменная адреса
// службы личности. Разбирается само объявление (`"<путь>": { target: <цель> }`),
// а не текст файла: полоса, дописанная к другой службе, сюда не попадает.
//
// Сторона раздачи: блок, чьё тело называет восходящий узел службы личности
// (`identityUpstreamRe`, общий с соседями по файлу). Префиксный блок даёт
// приставку, блок-регулярка — сегменты своей полосы, именованный запасной блок
// адресом не является и пропускается.
//
// Пустой обход — падение с обеих сторон: ни «раздача не объявила ни одного
// блока», ни «не прочитано ни одной конфигурации сборщика» утверждением о
// согласии полос не являются.

package deploy_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/treecorpus"
)

// devProxyLaneRe — ключ карты проксирования вместе с целью: `"<путь>": { …
// target: <имя> …`. Цель читается по имени переменной: адрес службы личности
// объявлен в том же файле одной константой, и по ней полоса и опознаётся.
var devProxyLaneRe = regexp.MustCompile(`"(/[^"]*)"\s*:\s*\{[^{}]*?target:\s*([A-Za-z_$][\w$]*)`)

// devIdentityTargetRe — имена целей, ведущих к службе личности.
var devIdentityTargetRe = regexp.MustCompile(`^kratos(Ui)?$`)

// firstSegment — первый сегмент пути без ведущей косой черты; пусто для корня.
func firstSegment(p string) string {
	trimmed := strings.TrimPrefix(p, "/")
	if i := strings.Index(trimmed, "/"); i >= 0 {
		return trimmed[:i]
	}
	return trimmed
}

// laneCoveredByServing — объявляет ли раздача адрес полосы: приставкой
// (точное совпадение либо подпуть) либо сегментом полосы-регулярки.
func laneCoveredByServing(lanePath string, prefixes []string, segments map[string]bool) bool {
	if segments[firstSegment(lanePath)] {
		return true
	}
	p := strings.TrimSuffix(lanePath, "/")
	for _, pre := range prefixes {
		if p == pre || strings.HasPrefix(p, pre+"/") {
			return true
		}
	}
	return false
}

// TestEveryDevProxyLaneToTheIdentityServiceIsDeclaredByTheServing — множество
// полос сборщика к службе личности покрыто объявлением раздачи.
func TestEveryDevProxyLaneToTheIdentityServiceIsDeclaredByTheServing(t *testing.T) {
	root := repoRootFromTest(t)

	// ── сторона раздачи ──────────────────────────────────────────────────────
	servers, servingPath := servingTemplate(t)
	var prefixes []string
	segments := map[string]bool{}
	blocksFound := 0
	for _, srv := range servers {
		for _, l := range srv.locs {
			if !identityUpstreamRe.MatchString(l.body) {
				continue
			}
			if strings.HasPrefix(l.spec, "@") {
				// Именованный запасной блок адресом не является: в него уходят
				// из другого блока, снаружи по нему не обращаются.
				continue
			}
			blocksFound++
			if l.re == nil {
				prefixes = append(prefixes, strings.TrimSuffix(l.spec, "/"))
				continue
			}
			segs, err := bandSegments(l.spec)
			if err != nil {
				t.Fatalf("%s: полосу %q прочитать не удалось: %v", servingPath, l.spec, err)
			}
			for _, s := range segs {
				segments[s] = true
			}
		}
	}

	// ── сторона сборщика ─────────────────────────────────────────────────────
	files, err := treecorpus.Under(filepath.Join(root, "ui-future"))
	if err != nil {
		t.Fatalf("состав ui-future: %v — без индекса «ноль находок» неотличимо от «ноль прочитанного»", err)
	}
	type devLane struct {
		path string
		in   string
	}
	var lanes []devLane
	filesRead := 0
	for _, abs := range files {
		if filepath.Base(abs) != "vite.config.ts" {
			continue
		}
		body, rerr := os.ReadFile(abs) // #nosec G304 -- путь пришёл из индекса git этого дерева
		if rerr != nil {
			t.Fatalf("%s: %v", abs, rerr)
		}
		filesRead++
		rel, _ := filepath.Rel(root, abs)
		for _, m := range devProxyLaneRe.FindAllStringSubmatch(string(body), -1) {
			if !devIdentityTargetRe.MatchString(m[2]) {
				continue
			}
			lanes = append(lanes, devLane{path: m[1], in: rel})
		}
	}

	t.Logf("осмотрено: блоков раздачи к службе личности %d (приставки %v, сегменты %v); "+
		"конфигураций сборщика прочитано %d, полос к службе личности %d",
		blocksFound, prefixes, sortedKeys(segments), filesRead, len(lanes))

	switch {
	case blocksFound == 0:
		t.Fatal("раздача не объявляет НИ ОДНОГО блока к службе личности — сверять не с чем, " +
			"и требование покрытия стало бы тождественно-истинным")
	case filesRead == 0:
		t.Fatal("не прочитано ни одной конфигурации сборщика — прочитано ноль, и молчание " +
			"проверки утверждением о согласии полос не является")
	case len(lanes) == 0:
		t.Fatal("ни одна конфигурация сборщика не объявляет полосы к службе личности — " +
			"внешнего источника «что обязано быть покрыто» не осталось")
	}

	for _, lane := range lanes {
		if laneCoveredByServing(lane.path, prefixes, segments) {
			continue
		}
		t.Errorf("сборщик (%s) проксирует к службе личности путь %q, а раздача стенда "+
			"его НЕ объявляет (приставки %v, сегменты %v). В разработке адрес отвечает "+
			"службой, на стенде — заглушкой одностраничного приложения с кодом 200: "+
			"отказ выглядит успехом, и различие обнаруживается не раньше стенда.",
			lane.in, lane.path, prefixes, sortedKeys(segments))
	}
}

// TestDevProxyLanePredicateCutsBothWays — признак и правило покрытия судят обе
// стороны каждой оси. Без этой пробы зелень гейта над деревом не говорила бы о
// его работоспособности ничего: после снятия голой полосы `/self-service` на
// настоящем дереве находок нет, и «не находит» стало бы неотличимо от «не
// умеет находить».
func TestDevProxyLanePredicateCutsBothWays(t *testing.T) {
	const synthetic = `
      "/vpc": { target: apiGateway, changeOrigin: true },
      "/.ory/kratos/public": { target: kratos, changeOrigin: true },
      "/self-service": { target: kratos, changeOrigin: true },
      "/login": { target: kratosUi, changeOrigin: true },
`
	judged := map[string]bool{}
	for _, m := range devProxyLaneRe.FindAllStringSubmatch(synthetic, -1) {
		if devIdentityTargetRe.MatchString(m[2]) {
			judged[m[1]] = true
		}
	}
	t.Logf("осмотрено: полос в синтетическом объявлении 4, судятся %d %v", len(judged), sortedKeys(judged))

	// Ось «цель»: полоса к своему краю не судится, полосы к службе личности — да.
	if judged["/vpc"] {
		t.Error("полоса к собственному краю попала под суд — признак шире предмета, " +
			"и находки гейта стали бы ложными")
	}
	for _, want := range []string{"/.ory/kratos/public", "/self-service", "/login"} {
		if !judged[want] {
			t.Errorf("полоса %q к службе личности НЕ попала под суд — признак уже предмета, "+
				"и расхождение по ней прошло бы молча", want)
		}
	}

	// Ось «покрытие»: приставка, сегмент и непокрытый адрес.
	prefixes := []string{"/.ory/kratos/public"}
	segments := map[string]bool{"login": true}
	for path, want := range map[string]bool{
		"/.ory/kratos/public":          true,  // точное совпадение с приставкой
		"/.ory/kratos/public/sessions": true,  // подпуть приставки
		"/login":                       true,  // сегмент полосы-регулярки
		"/self-service":                false, // не покрыт ничем — находка
		"/.ory/kratos/publicity":       false, // приставка НЕ посимвольная: чужой адрес
	} {
		if got := laneCoveredByServing(path, prefixes, segments); got != want {
			t.Errorf("покрытие %q: получено %v, ожидалось %v", path, got, want)
		}
	}
}
