// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

// TestDocsRequestBodiesUseDeclaredFields — вердикт о НАСТОЯЩЕМ дереве.
//
// # Охват сужен до vpc, и это НАЗВАНО, а не подразумевается
//
// Класс живёт и у соседей: перепись по всем деревьям документации на день
// заведения давала пять находок вне vpc — compute (2), iam (2), nlb (1). Чинит
// их владелец домена; расширение `DocRoots` — правка одной строки, и после неё
// вердикт станет тревогой для тех, кто эти страницы ведёт. Перепись печатает обе
// величины (деревьев всего · судится), поэтому суженный охват виден в каждом
// прогоне.
//
// Способность падать доказывает не этот прогон, а инъекция
// (`clienttruth_vpc_docsbodyfields_injection_test.go`): здесь только вердикт.
func TestDocsRequestBodiesUseDeclaredFields(t *testing.T) {
	var log strings.Builder
	findings, census, err := AuditDocsBodyFields(DocsBodyFieldOptions{
		Tree:      clientTruthRepoTree(t),
		ProtoRoot: "proto",
		DocRoots:  []string{"services/vpc/docs/content"},
	}, &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))

	// Премиса: прочитано то, что заведомо есть. Без неё «ноль находок»
	// достигалось бы пустым обходом.
	if census.ProtoFiles < 20 {
		t.Fatalf("файлов контракта %d — маршруты выводить не из чего", census.ProtoFiles)
	}
	if census.Routes < 20 {
		t.Fatalf("маршрутов с телом %d — таблица маршрутов не построена", census.Routes)
	}
	if census.DocFiles < 10 {
		t.Fatalf("страниц документации %d — обход пуст, вердикт беспредметен", census.DocFiles)
	}
	// Вторая половина вердикта выносится ТОЛЬКО о ключах сопоставленных тел. Ноль
	// означал бы, что она не вынесена ни разу, — «находок ноль» получено даром.
	if census.Routed == 0 || census.Keys == 0 {
		t.Fatalf("тел сопоставлено %d, ключей рассужено %d — сверка не состоялась",
			census.Routed, census.Keys)
	}
	// Ни один путь vpc не обязан оставаться без маршрута: пример, чей путь не
	// резолвится, документирует несуществующий вызов, и это тоже находка — но
	// другого предиката, поэтому здесь она названа отдельно.
	if census.Unrouted != 0 {
		t.Errorf("тел без маршрута %d — пример документирует путь, которого контракт "+
			"не объявляет; прогоните анализатор и посмотрите, какой", census.Unrouted)
	}
	if census.DocTreesJudged >= census.DocTreesTotal {
		t.Logf("охват полон: судится %d деревьев из %d — можно снять оговорку о частичности",
			census.DocTreesJudged, census.DocTreesTotal)
	}

	if len(findings) == 0 {
		return
	}
	lines := make([]string, 0, len(findings))
	for _, f := range findings {
		lines = append(lines, "  "+f.String())
	}
	t.Errorf("пример тела запроса называет поле, которого у запроса нет "+
		"(тел %d, ключей %d):\n%s\n\n"+
		"Край отбрасывает неизвестный ключ МОЛЧА, поэтому клиент, скопировавший "+
		"пример, получает отказ по другому поводу — и ищет вслепую, потому что верного "+
		"имени на странице нет. Маршруты и поля выводятся из `proto/`; правьте страницу, "+
		"а не этот список.",
		census.Bodies, census.Keys, strings.Join(lines, "\n"))
}
