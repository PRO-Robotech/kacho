// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Доказательство того, что гейт разрешимости перехода СПОСОБЕН упасть — и что
// падает он на существе, а не на форме.
//
// Все пробы гоняют ТУ ЖЕ функцию, что и гейт по дереву (`auditJumpTargets`):
// проба, повторяющая логику гейта своей копией, доказывала бы свойство копии.
//
// У КАЖДОГО ОТРИЦАНИЯ ЗДЕСЬ ЕСТЬ ПАРНЫЙ ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ. «Находок ноль»
// само по себе неотличимо от «распознаватель перехода ослеп», поэтому каждая
// молчащая проба дополнительно утверждает ОЖИДАЕМОЕ число увиденных переходов:
// гейт переход увидел и промолчал по существу, а не потому, что смотрел мимо.
//
// ФИКСТУРА ПРИВЯЗАНА К ДЕРЕВУ, А НЕ К ПАМЯТИ АВТОРА: последняя проба берёт
// НАСТОЯЩИЕ коллекции из индекса git и требует, чтобы разбор нашёл в них
// переходы. Сменится форма перехода в генераторах — она скажет об этом сама,
// вместо того чтобы синтетика продолжала доказывать свойство вчерашнего дерева.
package artifactgates

import (
	"sort"
	"strings"
	"testing"
)

// nmJumpAudit — прогон гейтовой функции по синтетическому дереву.
func nmJumpAudit(t *testing.T, folders ...nmItem) ([]nmJumpFinding, nmJumpCensus) {
	t.Helper()
	dir := t.TempDir()
	rel := nmWriteCollection(t, dir, "synthetic.postman_collection.json", folders...)
	f, cen, err := auditJumpTargets(dir, []string{rel})
	if err != nil {
		t.Fatal(err)
	}
	return f, cen
}

// nmJumpTo — шаг, буквально передающий управление соседу. Форма взята с натуры:
// так эмитят пере-заезд генераторы nlb и iam.
func nmJumpTo(name, target string) nmItem {
	return nmStep(name, "GET", "{{baseUrl}}/operations/{{opId}}",
		"const j = pm.response.json();",
		"if (!j.done) {",
		"  const _w = Date.now(); while (Date.now() - _w < 400) { /* wait */ }",
		"  pm.execution.setNextRequest('"+target+"');",
		"  return;",
		"}",
	)
}

// ─── красное на настоящем дефекте ────────────────────────────────────────────

// Дефект в его натуральном виде: шаг-цель переименован проходом обёртки окна
// видимости (`setup-lb` → `setup-lb-rya3`), ссылка на него осталась прежней.
func TestJumpTargetGateRedWhenTargetWasRenamed(t *testing.T) {
	findings, cen := nmJumpAudit(t, nmFolder("LST-CR-CRUD-OK — создание слушателя",
		nmStep("setup-lb-rya3", "POST", "{{baseUrl}}/nlb/v1/networkLoadBalancers",
			"pm.test('status 200', () => pm.expect(pm.response.code).to.eql(200));"),
		nmJumpTo("poll-op-1", "setup-lb"),
	))
	if cen.literalJumps != 1 {
		t.Fatalf("разобрано буквальных переходов %d, ожидался 1 — гейт смотрел мимо, "+
			"и его находка ничего бы не значила", cen.literalJumps)
	}
	if len(findings) != 1 {
		t.Fatalf("ожидалась ровно одна находка, получено %d: %v", len(findings), findings)
	}
	got := findings[0].String()
	// Находка обязана НАЗЫВАТЬ координату: чинят по её тексту, а не по счётчику.
	for _, want := range []string{
		"synthetic.postman_collection.json", "LST-CR-CRUD-OK", "poll-op-1", "setup-lb",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("находка не называет %q: %s", want, got)
		}
	}
	if !strings.Contains(got, "нет") {
		t.Errorf("находка не говорит, ЧТО именно неверно: %s", got)
	}
}

// Второй вид того же дефекта: цель нашлась, но не одна. Newman уйдёт в первый
// одноимённый шаг — то есть переход исполнится и уведёт не туда.
func TestJumpTargetGateRedOnAmbiguousTarget(t *testing.T) {
	findings, cen := nmJumpAudit(t, nmFolder("TG-DEL-NEG-INUSE",
		nmStep("create", "POST", "{{baseUrl}}/nlb/v1/targetGroups"),
		nmStep("create", "POST", "{{baseUrl}}/nlb/v1/targetGroups"),
		nmJumpTo("poll-op-1", "create"),
	))
	if cen.literalJumps != 1 {
		t.Fatalf("разобрано буквальных переходов %d, ожидался 1", cen.literalJumps)
	}
	if len(findings) != 1 {
		t.Fatalf("ожидалась ровно одна находка, получено %d: %v", len(findings), findings)
	}
	if !strings.Contains(findings[0].String(), "неоднозначен") {
		t.Errorf("находка не отличает неоднозначность от отсутствия: %s", findings[0])
	}
}

// ─── законные близнецы: та же форма, предмета нет ────────────────────────────

// Близнец 1: буквальный переход, который РАЗРЕШАЕТСЯ. Ровно та же конструкция,
// что и в находке выше, — отличается только тем, ради чего гейт заведён.
func TestJumpTargetGateSilentWhenTargetResolves(t *testing.T) {
	findings, cen := nmJumpAudit(t, nmFolder("LST-CR-CRUD-OK — создание слушателя",
		nmStep("setup-lb", "POST", "{{baseUrl}}/nlb/v1/networkLoadBalancers"),
		nmJumpTo("poll-op-1", "setup-lb"),
	))
	if cen.literalJumps != 1 {
		t.Fatalf("положительный контроль: разобрано переходов %d, ожидался 1 — "+
			"молчание гейта означало бы слепоту, а не исправность", cen.literalJumps)
	}
	if len(findings) != 0 {
		t.Fatalf("гейт краснеет на законном переходе: %v", findings)
	}
}

// Близнец 2: самоповтор. Целью он резолвит СЕБЯ всегда и буквальным переходом не
// является — иначе гейт краснел бы на каждом поллере операции в дереве.
func TestJumpTargetGateSilentOnSelfLoop(t *testing.T) {
	self := nmStep("poll-op-1", "GET", "{{baseUrl}}/operations/{{opId}}",
		"const j = pm.response.json();",
		"if (!j.done) {",
		"  const _w = Date.now(); while (Date.now() - _w < 400) { /* wait */ }",
		"  pm.execution.setNextRequest(pm.info.requestName);",
		"  return;",
		"}",
	)
	findings, cen := nmJumpAudit(t, nmFolder("NET-CR-CRUD-OK", self))
	if cen.selfLoops != 1 {
		t.Fatalf("положительный контроль: самоповторов увидено %d, ожидался 1", cen.selfLoops)
	}
	if cen.literalJumps != 0 {
		t.Fatalf("самоповтор засчитан буквальным переходом (%d) — гейт ловит форму, а не существо",
			cen.literalJumps)
	}
	if len(findings) != 0 {
		t.Fatalf("гейт краснеет на самоповторе: %v", findings)
	}
}

// Близнец 3: цель переименована обёрткой, и ССЫЛКА переименована вместе с ней —
// то есть ровно тот исход, ради которого гейт заведён. Он обязан молчать, иначе
// правило нельзя было бы исполнить.
func TestJumpTargetGateSilentWhenReferenceFollowsTheRename(t *testing.T) {
	findings, cen := nmJumpAudit(t, nmFolder("LST-CR-CRUD-OK — создание слушателя",
		nmStep("setup-lb-rya3", "POST", "{{baseUrl}}/nlb/v1/networkLoadBalancers"),
		nmJumpTo("poll-op-1", "setup-lb-rya3"),
	))
	if cen.literalJumps != 1 {
		t.Fatalf("положительный контроль: разобрано переходов %d, ожидался 1", cen.literalJumps)
	}
	if len(findings) != 0 {
		t.Fatalf("гейт краснеет на переходе, следующем за переименованием: %v", findings)
	}
}

// Близнец 4: переход в шаг ДРУГОЙ папки той же коллекции. Newman резолвит цель по
// имени в пределах прогона, а не папки, — гейт обязан считать так же.
func TestJumpTargetGateSilentAcrossFolders(t *testing.T) {
	findings, cen := nmJumpAudit(t,
		nmFolder("ACB-PRECLEAN", nmStep("grant-view", "POST", "{{baseUrl}}/iam/v1/accessBindings")),
		nmFolder("ACB-RD-AUTHZ-OK", nmJumpTo("await-del", "grant-view")),
	)
	if cen.literalJumps != 1 {
		t.Fatalf("положительный контроль: разобрано переходов %d, ожидался 1", cen.literalJumps)
	}
	if len(findings) != 0 {
		t.Fatalf("гейт краснеет на межпапочном переходе: %v", findings)
	}
}

// ─── фикстура из дерева, а не из памяти автора ───────────────────────────────

// Разбор обязан узнавать переход в НАСТОЯЩИХ коллекциях. Иначе синтетика выше
// доказывала бы свойство своих же строк: сменится форма перехода в генераторах —
// гейт по дереву станет вечнозелёным, а эти пробы этого не заметят.
func TestJumpTargetAuditReadsRealCollections(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)
	var cols []string
	for rel := range tt.files {
		if strings.Contains(rel, "/tests/newman/collections/") && strings.HasSuffix(rel, ".json") {
			cols = append(cols, rel)
		}
	}
	sort.Strings(cols)

	_, cen, err := auditJumpTargets(root, cols)
	if err != nil {
		t.Fatal(err)
	}
	if cen.collections == 0 || cen.steps == 0 {
		t.Fatalf("в индексе git прочитано коллекций %d, шагов %d — обход не нашёл дерева",
			cen.collections, cen.steps)
	}
	if cen.selfLoops == 0 {
		t.Fatalf("в %d коллекциях (%d шагов) не узнан НИ ОДИН самоповтор — "+
			"распознаватель читает не ту форму", cen.collections, cen.steps)
	}
	if cen.literalJumps == 0 {
		t.Fatalf("в %d коллекциях (%d шагов) не узнан НИ ОДИН буквальный переход — "+
			"либо форма сменилась, либо буквальных переходов в дереве больше нет; "+
			"второе законно, но требует снять эту пробу осознанно, а не оставить её "+
			"вечнозелёной", cen.collections, cen.steps)
	}
	t.Logf("настоящее дерево: коллекций %d, шагов %d, самоповторов %d, буквальных переходов %d",
		cen.collections, cen.steps, cen.selfLoops, cen.literalJumps)
}
