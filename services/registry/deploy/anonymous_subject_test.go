// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// anonymous_subject_test.go — ручка анонимного субъекта плоскости данных
// (`KACHO_REGISTRY_ANONYMOUS_SUBJECT_ID`) имеет ОДИН источник на обе стороны
// (приёмка REG-PUB-1 Р9 п. 2, kacho#3274).
//
// У значения две стороны: служба доступа чеканит его в `sub` анонимного токена
// (`api-server.registry-token.anonymous-client-id`), а реестр по нему узнаёт
// анонима и спрашивает модель от имени класса `system_class:anonymous`.
// Стороны не симметричны: чеканка анонимного токена включается лишь единым
// объявлением (почему — kaname.anonymousClientId). Поэтому
// объявление одно — `global.kacho.registry.anonymousSubjectId`, — читают его оба
// подчарта, как `serviceAud` (lane_service_aud_test.go), а собственная ручка
// есть только у узнающей стороны — у реестра (сторона службы доступа —
// deploy/helm/umbrella/iam_anonymous_client_id_test.go, ось (б)).
//
// Оси (те же, что у serviceAud, плюс пустая):
//
//	(а) единый источник доезжает до переменной реестра;
//	(б) без него действует собственная ручка (одиночная установка чарта);
//	(в) объявлены обе и не сходятся ⇒ рендер ОТКАЗЫВАЕТ, называя обе ручки и обе величины;
//	(г) законный близнец: объявлены обе и сходятся ⇒ рендер молчит;
//	(д) не объявлено ни одной ⇒ переменной в поде нет вовсе (поток выключен),
//	    положительный контроль рядом.
package deploy_test

import (
	"strings"
	"testing"
)

const anonSubjectEnv = "KACHO_REGISTRY_ANONYMOUS_SUBJECT_ID"

// anonSubjectFromRender — значение переменной в отрендеренном поде и признак
// того, что переменная там есть.
func anonSubjectFromRender(rendered string) (string, bool) {
	lines := strings.Split(rendered, "\n")
	for i, ln := range lines {
		if !strings.Contains(ln, "- name: "+anonSubjectEnv) || i+1 >= len(lines) {
			continue
		}
		v := strings.TrimPrefix(strings.TrimSpace(lines[i+1]), "value:")
		return strings.Trim(strings.TrimSpace(v), `"`), true
	}
	return "", false
}

// TestRegistryAnonymousSubjectComesFromTheSingleDeclaration — оси (а) и (б).
func TestRegistryAnonymousSubjectComesFromTheSingleDeclaration(t *testing.T) {
	t.Parallel()

	got, ok := anonSubjectFromRender(helmTemplate(t, withZot(
		"global.kacho.registry.anonymousSubjectId=registry-anonymous")...))
	if !ok || got != "registry-anonymous" {
		t.Errorf("единый источник не доехал до реестра: %s = %q (есть: %v), ждали registry-anonymous",
			anonSubjectEnv, got, ok)
	}

	got, ok = anonSubjectFromRender(helmTemplate(t, withZot("anonymousSubjectId=solo-anonymous")...))
	if !ok || got != "solo-anonymous" {
		t.Errorf("без единого источника собственная ручка обязана действовать: %s = %q (есть: %v)",
			anonSubjectEnv, got, ok)
	}
}

// TestRegistryAnonymousSubjectRefusesUnlinkedSides — оси (в) и (г).
func TestRegistryAnonymousSubjectRefusesUnlinkedSides(t *testing.T) {
	t.Parallel()

	differ := withZot("global.kacho.registry.anonymousSubjectId=registry-anonymous",
		"anonymousSubjectId=other-anonymous")
	for _, want := range []string{
		"global.kacho.registry.anonymousSubjectId", "registry.anonymousSubjectId",
		"registry-anonymous", "other-anonymous",
	} {
		helmTemplateMustFail(t, want, differ...)
	}

	got, ok := anonSubjectFromRender(helmTemplate(t, withZot(
		"global.kacho.registry.anonymousSubjectId=same-anonymous", "anonymousSubjectId=same-anonymous")...))
	if !ok || got != "same-anonymous" {
		t.Errorf("сошедшиеся объявления обязаны рендериться: %s = %q (есть: %v)", anonSubjectEnv, got, ok)
	}
}

// TestRegistryAnonymousSubjectAbsentWhenUndeclared — ось (д).
func TestRegistryAnonymousSubjectAbsentWhenUndeclared(t *testing.T) {
	t.Parallel()
	if got, ok := anonSubjectFromRender(helmTemplate(t, withZot()...)); ok {
		t.Errorf("ничего не объявлено, а %s в поде есть (%q) — чарт выдумал значение потока, "+
			"который оператор не включал", anonSubjectEnv, got)
	}
	// Положительный контроль: без него утверждение выше зеленело бы на чарте,
	// который не рендерит переменную вовсе.
	if _, ok := anonSubjectFromRender(helmTemplate(t, withZot("anonymousSubjectId=named-anonymous")...)); !ok {
		t.Errorf("объявленное значение обязано доезжать до %s", anonSubjectEnv)
	}
}
