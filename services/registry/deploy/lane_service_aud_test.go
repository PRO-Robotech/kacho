// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// lane_service_aud_test.go — имя службы реестра у докерной полосы выдачи имеет
// ОДИН источник на обе стороны (задача #1184).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// У полосы две стороны. Реестр называет докер-клиенту имя своей службы
// (`KACHO_REGISTRY_SERVICE_AUD`) в вызове на аутентификацию; клиент возвращает
// это имя в `?service=`; iam чеканит удостоверение адресату, которого объявил
// сам. Пока iam адресата не сверял, расхождение сторон было НЕВИДИМО: клиент
// echo-ит услышанное, реестр это и ждёт, iam чеканил что просят. Сверка введена
// — и расхождение стало отказом во входе докера.
//
// Сводить умолчания бессмысленно: имя реестра законно СВОЁ у каждого кластера.
// Стороны обязаны быть СВЯЗАНЫ, а не равны, поэтому объявление одно —
// `global.kacho.registry.serviceAud`, — и читают его ОБА подчарта. Расходиться
// нечему by construction.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЗДЕСЬ УТВЕРЖДАЕТСЯ — четыре оси, разведённые намеренно
//
//	(а) объявленный единый источник ДОХОДИТ до переменной реестра;
//	(б) без него (одиночная установка чарта) действует собственная ручка —
//	    связывать не с чем, и отказывать не за что;
//	(в) объявлены ОБЕ и не сходятся ⇒ рендер ОТКАЗЫВАЕТ и называет ОБЕ величины;
//	(г) законный близнец: объявлены обе и сходятся ⇒ рендер молчит;
//	(д) не объявлено НИ ОДНОЙ ⇒ чарт НЕ ВЫДУМЫВАЕТ имени — рендерит пустое и
//	    оставляет отказ стражу старта процесса, который знает посадку.
//
// Без (г) отказ (в) был бы неотличим от отказа на всякой паре объявлений; без
// (б) гейт требовал бы единого источника там, где второй стороны вовсе нет.
package deploy_test

import (
	"strings"
	"testing"
)

// zotCreds — минимум, без которого чарт отказывается рендериться по СВОЕЙ,
// посторонней здесь причине (слой хранения обязан аутентифицировать вызывающих).
var zotCreds = []string{"zot.auth.username=u", "zot.auth.password=p"}

func withZot(sets ...string) []string { return append(append([]string{}, zotCreds...), sets...) }

// laneAudEnv — значение KACHO_REGISTRY_SERVICE_AUD в отрендеренном поде.
func laneAudEnv(t *testing.T, rendered string) string {
	t.Helper()
	const key = "- name: KACHO_REGISTRY_SERVICE_AUD"
	lines := strings.Split(rendered, "\n")
	for i, ln := range lines {
		if !strings.Contains(ln, key) {
			continue
		}
		if i+1 >= len(lines) {
			break
		}
		v := strings.TrimSpace(lines[i+1])
		v = strings.TrimPrefix(v, "value:")
		return strings.Trim(strings.TrimSpace(v), `"`)
	}
	t.Fatalf("в рендере нет переменной KACHO_REGISTRY_SERVICE_AUD (осмотрено строк %d)", len(lines))
	return ""
}

// TestRegistryLaneServiceAudComesFromTheSingleDeclaration — оси (а) и (б).
func TestRegistryLaneServiceAudComesFromTheSingleDeclaration(t *testing.T) {
	t.Parallel()

	// (а) единый источник объявлен — он и доезжает, невзирая на умолчание чарта.
	got := laneAudEnv(t, helmTemplate(t, withZot("global.kacho.registry.serviceAud=lane.example")...))
	if got != "lane.example" {
		t.Errorf("объявленный единый источник не доехал до реестра: KACHO_REGISTRY_SERVICE_AUD = %q, ждали lane.example", got)
	}

	// (б) единого источника нет (чарт установлен сам по себе) — действует своя ручка.
	got = laneAudEnv(t, helmTemplate(t, withZot("serviceAud=solo.example")...))
	if got != "solo.example" {
		t.Errorf("без единого источника собственная ручка обязана действовать: KACHO_REGISTRY_SERVICE_AUD = %q, ждали solo.example", got)
	}
}

// TestRegistryLaneServiceAudRefusesUnlinkedSides — оси (в) и (г).
func TestRegistryLaneServiceAudRefusesUnlinkedSides(t *testing.T) {
	t.Parallel()

	// (в) два объявления одного предмета, и они не сходятся.
	helmTemplateMustFail(t, "global.kacho.registry.serviceAud",
		withZot("global.kacho.registry.serviceAud=lane.example", "serviceAud=other.example")...)
	helmTemplateMustFail(t, "lane.example",
		withZot("global.kacho.registry.serviceAud=lane.example", "serviceAud=other.example")...)
	helmTemplateMustFail(t, "other.example",
		withZot("global.kacho.registry.serviceAud=lane.example", "serviceAud=other.example")...)

	// (г) законный близнец — та же форма, значения сходятся: рендер молчит.
	got := laneAudEnv(t, helmTemplate(t, withZot(
		"global.kacho.registry.serviceAud=same.example", "serviceAud=same.example")...))
	if got != "same.example" {
		t.Errorf("сошедшиеся объявления обязаны рендериться: KACHO_REGISTRY_SERVICE_AUD = %q, ждали same.example", got)
	}
}

// TestRegistryLaneServiceAudIsNeverInvented — ось (д).
//
// Ничего не объявлено — чарт обязан отдать ПУСТОЕ, а не подставить имя. Всякая
// подстановка здесь есть второе объявление предмета: величина у каждого
// кластера своя, и выбрать её за оператора нельзя — тем более молча. Ровно это
// умолчание и расходилось со стороной личности, пока сверки адресата не было.
//
// Отказ выносит СТРАЖ СТАРТА процесса (buildTokenVerifier: незаданный ожидаемый
// адресат означает «принимаем любого»): он знает посадку, а шаблон не знает.
func TestRegistryLaneServiceAudIsNeverInvented(t *testing.T) {
	t.Parallel()
	if got := laneAudEnv(t, helmTemplate(t, withZot()...)); got != "" {
		t.Errorf("чарт подставил имя службы полосы (%q) — это второе объявление предмета, "+
			"молча расходящееся со стороной личности", got)
	}

	// ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ рядом: без него утверждение выше зеленело бы на
	// чарте, который не рендерит переменную вовсе.
	if got := laneAudEnv(t, helmTemplate(t, withZot("serviceAud=named.example")...)); got != "named.example" {
		t.Errorf("объявленное имя обязано доезжать: %q", got)
	}
}
