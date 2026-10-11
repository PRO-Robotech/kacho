// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// console_health_point_external_test.go — СЛУЖЕБНАЯ ТОЧКА ЖИВОСТИ РАЗДАЧИ
// КОНСОЛИ НЕ ОТДАЁТСЯ НА ВНЕШНЕМ ВХОДЕ (kacho#3030).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// `location = /healthz` объявлен в каждом серверном блоке раздачи (оболочка и
// восемь модулей) — для проб живости и готовности кластера. Внешний вход
// консоли (`publicFront`) слушает тем же серверным блоком оболочки, что и
// внутренний порт: второй копии маршрутов нет намеренно. Поэтому служебная
// точка отвечала и снаружи — двумя путями:
//
//   - `/healthz` на TLS-порту внешнего входа — блок оболочки;
//   - `/<модуль>-remote/healthz` — полоса ассетов модуля срезает префикс и
//     отдаёт адрес серверу модуля, у которого `/healthz` свой.
//
// Утверждается, КАКОЙ блок раздача выберет для адреса (порядок разрешения —
// console_serving_template_test.go), а не наличие строки: блок, который
// проигрывает другому по правилам разрешения, ничего не закрывает.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЗАКОННЫЙ БЛИЗНЕЦ
//
// Внутренняя точка живости обязана отвечать по-прежнему: на неё смотрят пробы
// кластера (`deployment-*.yaml`, порт `http`). Поэтому отказ на внешнем порту
// судится вместе с ответом `200` того же блока, а точки модулей — числом: их
// столько же, сколько полос модулей у оболочки, и каждая отвечает.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ПРОБА НЕ УТВЕРЖДАЕТ
//
// Внешний вход через Ingress приходит на внутренний порт оболочки, и порт его
// от пробы кластера не отличает; эта проба судит вход `publicFront` (TLS на
// самой раздаче). Стендовое утверждение — ответ работающей раздачи — судит
// проба после выкатки, а не эта.
package deploy_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// externalListenRe — TLS-слушатель внешнего входа: выражение порта из values.
// За `ssl` может стоять условный приём заголовка PROXY от балансировщика
// площадки (kacho#3115) — параметр того же слушателя, а не другой слушатель.
var externalListenRe = regexp.MustCompile(`(?m)^\s*listen\s+\{\{-?\s*(\.Values\.publicFront\.[A-Za-z]+)\s*-?\}\}\s+ssl(?:\{\{\s*if\s+\$proxied\s*\}\}\s+proxy_protocol\{\{\s*end\s*\}\})?\s*;`)

// remoteLaneRe — полоса ассетов модуля в серверном блоке оболочки.
var remoteLaneRe = regexp.MustCompile(`^/([a-z][a-z0-9-]*)-remote/$`)

// healthAnswerRe — блок отвечает точкой живости.
var healthAnswerRe = regexp.MustCompile(`(?m)^\s*return\s+200\b`)

// refusalRe — блок отказывает адресу.
var refusalRe = regexp.MustCompile(`(?m)^\s*return\s+404\s*;`)

// healthCensus — объём осмотренного: «находок нет» отличимо от «судить было нечего».
type healthCensus struct {
	ExternalPort  string // выражение порта внешнего входа
	RemoteLanes   int    // полос модулей у оболочки
	ModuleAnswers int    // серверов модулей, чья точка живости отвечает
}

func (c healthCensus) String() string {
	return fmt.Sprintf("порт внешнего входа %s · полос модулей у оболочки %d · точек живости модулей, "+
		"отвечающих внутри, %d", c.ExternalPort, c.RemoteLanes, c.ModuleAnswers)
}

// externalPortRefusalRe — отказ по порту внешнего входа внутри блока.
func externalPortRefusalRe(expr string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^\s*if\s*\(\s*\$server_port\s*=\s*\{\{-?\s*` + regexp.QuoteMeta(expr) +
		`\s*-?\}\}\s*\)\s*\{\s*return\s+404\s*;\s*\}`)
}

// judgeHealthPointExposure — НАХОДКИ по тексту объявления раздачи. Чистая
// функция от текста: инъекция подаёт ей настоящий шаблон с одним изменённым
// фактом.
func judgeHealthPointExposure(t *testing.T, serving string) ([]string, healthCensus) {
	t.Helper()
	var (
		findings []string
		census   healthCensus
	)
	m := externalListenRe.FindStringSubmatch(serving)
	if m == nil {
		return []string{"TLS-слушатель внешнего входа (`listen {{ .Values.publicFront.… }} ssl;`) не найден — " +
			"судить точку живости на внешнем входе не по чему"}, census
	}
	census.ExternalPort = m[1]

	servers := parseServingTemplate(t, serving)
	console := -1
	for i, srv := range servers {
		for _, l := range srv.locs {
			if edgeUpstreamRe.MatchString(l.body) {
				console = i
			}
		}
	}
	if console < 0 {
		return []string{"серверный блок оболочки (с восходящим узлом края) не найден"}, census
	}
	host := servers[console]

	// Оболочка: `/healthz` на внешнем порту — отказ; внутри — ответ.
	i, why := selectLocation("/healthz", host.locs)
	switch {
	case i < 0 || host.locs[i].mod != "=":
		findings = append(findings, fmt.Sprintf("оболочка: адрес `/healthz` достаётся не точке живости (%s) — "+
			"проба кластера на нём не отвечала бы", why))
	default:
		loc := host.locs[i]
		ans := healthAnswerRe.FindStringIndex(loc.body)
		ref := externalPortRefusalRe(census.ExternalPort).FindStringIndex(loc.body)
		if ans == nil {
			findings = append(findings, fmt.Sprintf("оболочка: %s не отвечает `200` — внутренняя точка живости "+
				"для пробы кластера снята вместе с внешней", loc.name()))
		}
		switch {
		case ref == nil:
			findings = append(findings, fmt.Sprintf("оболочка: %s отвечает и на внешнем входе — в блоке нет "+
				"отказа `if ($server_port = {{ %s }}) { return 404; }`, а слушает он тем же серверным блоком, "+
				"что и порт %s (kacho#3030)", loc.name(), census.ExternalPort, census.ExternalPort))
		case ans != nil && ref[0] > ans[0]:
			findings = append(findings, fmt.Sprintf("оболочка: в %s отказ внешнему порту стоит ПОСЛЕ ответа "+
				"`200` — ответ исполняется первым, и отказ мёртв", loc.name()))
		}
	}

	// Полосы модулей: `/<модуль>-remote/healthz` не уходит к серверу модуля.
	for _, l := range host.locs {
		if l.mod != "^~" {
			continue
		}
		rm := remoteLaneRe.FindStringSubmatch(l.spec)
		if rm == nil {
			continue
		}
		census.RemoteLanes++
		uri := "/" + rm[1] + "-remote/healthz"
		j, why := selectLocation(uri, host.locs)
		if j < 0 {
			findings = append(findings, fmt.Sprintf("оболочка: адрес `%s` не выбрал ни одного блока (%s)", uri, why))
			continue
		}
		sel := host.locs[j]
		if strings.Contains(sel.body, "proxy_pass") || !refusalRe.MatchString(sel.body) {
			findings = append(findings, fmt.Sprintf("оболочка: адрес `%s` достаётся %s (%s), и он не отказывает "+
				"`return 404;` — полоса ассетов отдаёт точку живости модуля на внешнем входе (kacho#3030)",
				uri, sel.name(), why))
		}
	}

	// Законный близнец: точки живости серверов модулей отвечают внутри.
	for k, srv := range servers {
		if k == console {
			continue
		}
		if j, _ := selectLocation("/healthz", srv.locs); j >= 0 && srv.locs[j].mod == "=" &&
			healthAnswerRe.MatchString(srv.locs[j].body) {
			census.ModuleAnswers++
		}
	}
	if census.RemoteLanes == 0 {
		findings = append(findings, "у оболочки нет ни одной полосы модуля `^~ /<модуль>-remote/` — обход пуст, "+
			"и «находок нет» означало бы «судить было нечего»")
	}
	if census.ModuleAnswers != census.RemoteLanes {
		findings = append(findings, fmt.Sprintf("точек живости модулей, отвечающих внутри, %d, а полос модулей %d — "+
			"проба кластера модуля потеряла бы ответ либо модуль без точки живости", census.ModuleAnswers,
			census.RemoteLanes))
	}
	return findings, census
}

// TestConsoleHealthPointIsNotServedOnTheExternalEntry — суд по дереву.
func TestConsoleHealthPointIsNotServedOnTheExternalEntry(t *testing.T) {
	root := repoRootFromTest(t)
	findings, census := judgeHealthPointExposure(t, readTreeFile(t, root, servingTemplateRel))
	t.Logf("перепись: %s", census)
	for _, f := range findings {
		t.Error(f)
	}
}
