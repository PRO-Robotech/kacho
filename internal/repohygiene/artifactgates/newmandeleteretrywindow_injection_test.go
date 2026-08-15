// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Доказательство того, что гейт окна удаления СПОСОБЕН упасть — и что падает он
// на существе, а не на форме.
//
// Все пробы гоняют ТУ ЖЕ функцию, что и гейт по дереву
// (`auditDeleteRetryWindow`): проба, повторяющая логику гейта своей копией,
// доказывала бы свойство копии.
//
// У КАЖДОГО ОТРИЦАНИЯ ЗДЕСЬ ЕСТЬ ПАРНЫЙ ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ. «Находок ноль»
// само по себе неотличимо от «распознаватель ослеп», поэтому каждая молчащая
// проба дополнительно утверждает, что обёртка БЫЛА УВИДЕНА, — гейт промолчал по
// существу, а не потому, что смотрел мимо.
//
// ПОСЛЕДНЯЯ ПРОБА ПРИВЯЗАНА К ДЕРЕВУ, А НЕ К ПАМЯТИ АВТОРА: она берёт настоящие
// коллекции из индекса git и требует, чтобы разбор нашёл в них ОБА механизма.
// Сменится форма в генераторах — она скажет об этом сама, вместо того чтобы
// синтетика продолжала доказывать свойство вчерашнего дерева.
package artifactgates

import (
	"sort"
	"strings"
	"testing"
)

func nmDelRetryAudit(t *testing.T, folders ...nmItem) ([]nmDeleteRetryFinding, nmDeleteRetryCensus) {
	t.Helper()
	dir := t.TempDir()
	rel := nmWriteCollection(t, dir, "synthetic.postman_collection.json", folders...)
	f, cen, err := auditDeleteRetryWindow(dir, []string{rel})
	if err != nil {
		t.Fatal(err)
	}
	return f, cen
}

// nmRetryGuard — обёртка окна видимости в той форме, какую эмитят генераторы.
func nmRetryGuard(codes string) []string {
	return []string{
		"// bounded read-your-writes retry over the owner-tuple materialization window",
		"if (pm.environment.get('_authRetryStarted') !== pm.info.requestName) {",
		"  pm.environment.set('_authRetryCount', '0');",
		"}",
		"const _arc = parseInt(pm.environment.get('_authRetryCount') || '0', 10);",
		"if ([" + codes + "].includes(pm.response.code) && _arc < 25) {",
		"  pm.environment.set('_authRetryCount', String(_arc + 1));",
		"  pm.execution.setNextRequest(pm.info.requestName);",
		"  return;",
		"}",
	}
}

// nmDefaultDeleteAssert — ДОПИСАННОЕ утверждение шага удаления, дословно как у
// генераторов (маркер + сам assert).
func nmDefaultDeleteAssert() []string {
	return []string{
		"// " + nmDeleteDefaultAssertMark + ": без него шаг зеленел бы и на",
		"// отказе, а следующий опрос уехал бы на opId предыдущей операции.",
		"pm.test('delete accepted: status 200', () => " +
			"pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
	}
}

func nmDeleteStep(name string, script ...string) nmItem {
	return nmStep(name, "DELETE", "{{baseUrl}}/nlb/v1/networkLoadBalancers/{{nlbId}}", script...)
}

// ─── инъекция: настоящий дефект ──────────────────────────────────────────────

// Утверждает 200, ждёт только 403 — ровно та форма, что упала на стволе.
func TestDeleteRetryWindowGate_FailsOnAssert200WithoutWaiting404(t *testing.T) {
	script := append(nmRetryGuard("403"), "pm.environment.set('opId', pm.response.json().id);")
	script = append(script, nmDefaultDeleteAssert()...)

	findings, cen := nmDelRetryAudit(t, nmFolder("CASE-1", nmDeleteStep("cleanup-del-lb-rya1", script...)))

	if len(findings) != 1 {
		t.Fatalf("гейт обязан найти шаг, утверждающий 200 и не ждущий 404; найдено %d", len(findings))
	}
	if !strings.Contains(findings[0].String(), "cleanup-del-lb-rya1") {
		t.Errorf("находка обязана называть координату, получено: %s", findings[0])
	}
	if cen.defaultAssertedWrapped != 1 {
		t.Errorf("перепись обязана увидеть 1 обёрнутый шаг с дописанным утверждением, увидела %d",
			cen.defaultAssertedWrapped)
	}
}

// ─── законные близнецы: гейт обязан МОЛЧАТЬ ──────────────────────────────────

// Тот же шаг, но окно ждёт и 404 — починенная форма.
func TestDeleteRetryWindowGate_SilentWhenDeleteWaitsOut404(t *testing.T) {
	script := append(nmRetryGuard("403,404"), "pm.environment.set('opId', pm.response.json().id);")
	script = append(script, nmDefaultDeleteAssert()...)

	findings, cen := nmDelRetryAudit(t, nmFolder("CASE-1", nmDeleteStep("cleanup-del-lb-rya1", script...)))

	if len(findings) != 0 {
		t.Errorf("на починенной форме гейт обязан молчать, получено: %v", findings)
	}
	// Положительный контроль: молчание по существу, а не по слепоте.
	if cen.wrapped != 1 || cen.defaultAssertedWrapped != 1 || cen.waits404 != 1 {
		t.Errorf("гейт обязан был УВИДЕТЬ обёртку и утверждение: wrapped=%d, defaultAssertedWrapped=%d, waits404=%d",
			cen.wrapped, cen.defaultAssertedWrapped, cen.waits404)
	}
}

// Шаг НАЗВАЛ 404 своим законным исходом и потому его не ждёт. Дописанного
// утверждения у него нет по построению — своё утверждение есть. Гейт обязан
// молчать: иначе он требовал бы пережидать ровно то, ради чего проба написана.
func TestDeleteRetryWindowGate_SilentWhenStepDeclares404AsItsOutcome(t *testing.T) {
	script := append(nmRetryGuard("403"),
		"pm.test('already gone', () => pm.expect(pm.response.code).to.eql(404));")

	findings, cen := nmDelRetryAudit(t, nmFolder("CASE-1", nmDeleteStep("cleanup-idempotent-rya2", script...)))

	if len(findings) != 0 {
		t.Errorf("404, названный исходом шага, находкой не является, получено: %v", findings)
	}
	if cen.wrapped != 1 {
		t.Errorf("гейт обязан был увидеть обёртку (иначе молчание — слепота): wrapped=%d", cen.wrapped)
	}
	if cen.defaultAsserted != 0 {
		t.Errorf("у шага со СВОИМ утверждением дописанного быть не может: defaultAsserted=%d", cen.defaultAsserted)
	}
}

// Шаг удаления с дописанным утверждением, но БЕЗ обёртки вовсе. Это другой
// предмет (его держит `newmanfreshreadwrap_test.go`), и здесь он находкой не
// является — иначе гейт судил бы о том, о чём не заявлял.
func TestDeleteRetryWindowGate_SilentWhenStepIsNotWrappedAtAll(t *testing.T) {
	script := append([]string{"pm.environment.set('opId', pm.response.json().id);"},
		nmDefaultDeleteAssert()...)

	findings, cen := nmDelRetryAudit(t, nmFolder("CASE-1", nmDeleteStep("cleanup-del-lb", script...)))

	if len(findings) != 0 {
		t.Errorf("необёрнутый шаг — предмет соседнего гейта, получено: %v", findings)
	}
	if cen.defaultAsserted != 1 || cen.defaultAssertedWrapped != 0 {
		t.Errorf("перепись обязана отличать обёрнутые от необёрнутых: defaultAsserted=%d, wrapped=%d",
			cen.defaultAsserted, cen.defaultAssertedWrapped)
	}
}

// ─── предпосылка, привязанная к ДЕРЕВУ ───────────────────────────────────────

// Оба механизма, о которых судит гейт, обязаны существовать в НАСТОЯЩИХ
// коллекциях. Пропадут — синтетика выше продолжит зеленеть, доказывая свойство
// вчерашнего дерева; эта проба скажет об этом прямо.
func TestDeleteRetryWindowGate_MechanismsPresentInRealCollections(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)
	var cols []string
	for rel := range tt.files {
		if strings.Contains(rel, "/tests/newman/collections/") && strings.HasSuffix(rel, ".json") {
			cols = append(cols, rel)
		}
	}
	sort.Strings(cols)

	_, cen, err := auditDeleteRetryWindow(root, cols)
	if err != nil {
		t.Fatal(err)
	}
	if cen.wrapped == 0 {
		t.Error("в дереве не нашлось ни одной обёртки окна видимости — форма сменилась, гейт ослеп")
	}
	if cen.defaultAsserted == 0 {
		t.Error("в дереве не нашлось ни одного дописанного утверждения удаления — маркер сменился, гейт ослеп")
	}
	t.Logf("предпосылка на дереве: обёрток %d, дописанных утверждений %d (из них обёрнутых %d)",
		cen.wrapped, cen.defaultAsserted, cen.defaultAssertedWrapped)
}
