// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Доказательство того, что гейт ветви по коду ответа СПОСОБЕН упасть — и что
// падает он на существе, а не на совпадении чисел.
//
// Все пробы гоняют ТУ ЖЕ функцию, что и гейт по дереву (`auditCodeBranchArm`).
//
// Ось «законного близнеца» здесь несущая, а не формальная: господствующая форма
// в этом корпусе — таблица решений `if/else`, и гейт, её не различающий, дал бы
// 203 находки из 203 ложных. Поэтому каждая молчащая проба дополнительно
// утверждает, что шаг гейтом УВИДЕН (перепись ветвей и разложение по долям), —
// иначе молчание неотличимо от слепоты.
package artifactgates

import (
	"strings"
	"testing"
)

func rcbAudit(t *testing.T, folders ...nmItem) ([]rcbFinding, rcbCensus) {
	t.Helper()
	dir := t.TempDir()
	rel := nmWriteCollection(t, dir, "synthetic.postman_collection.json", folders...)
	f, cen, err := auditCodeBranchArm(dir, []string{rel})
	if err != nil {
		t.Fatal(err)
	}
	return f, cen
}

const rcbURL = "{{baseUrl}}/nlb/v1/networkLoadBalancers?projectId={{garbageProjectId}}"

// ─── контроль: чистый шаг молчит, и он ПРОЧИТАН ──────────────────────────────

func TestRCB_CleanDecisionTableIsSilentAndSeen(t *testing.T) {
	step := nmStep("viewer", "GET", rcbURL,
		"pm.test('no data leaked', () => {",
		"  if (pm.response.code === 200) {",
		"    pm.expect((pm.response.json().networkLoadBalancers || []).length).to.eql(0);",
		"  } else {",
		"    pm.expect(pm.response.code).to.be.oneOf([403, 404]);",
		"  }",
		"});",
	)
	f, cen := rcbAudit(t, nmFolder("AZD-NLB-LST-STRANGER-DENIED", step))
	if len(f) != 0 {
		t.Fatalf("гейт покраснел на ЗАКОННОЙ таблице решений — он ловит совпадение чисел, а не существо: %v", f)
	}
	if cen.branches != 1 || cen.admissions != 1 {
		t.Fatalf("шаг гейтом не прочитан — молчание было бы слепотой: ветвей %d, допусков %d",
			cen.branches, cen.admissions)
	}
	if cen.exclusiveArms != 1 {
		t.Fatalf("пара «ветка вне допуска» не разложена в долю противоположной ветви: %d", cen.exclusiveArms)
	}
}

// ─── ось: противоречие в ОДНОЙ И ТОЙ ЖЕ ветви ────────────────────────────────

func TestRCB_BranchOutsideAdmissionInTheSameArmIsAFinding(t *testing.T) {
	step := nmStep("lst-stranger", "GET", rcbURL,
		"pm.test('refused', () => pm.expect(pm.response.code).to.be.oneOf([403, 404]));",
		"if (pm.response.code === 200) {",
		"  pm.test('empty page', () => pm.expect((pm.response.json().items || []).length).to.eql(0));",
		"}",
	)
	f, cen := rcbAudit(t, nmFolder("AZD-LST-LST-STRANGER-DENIED", step))
	if cen.branches != 1 || cen.admissions != 1 {
		t.Fatalf("гейт не увидел ни ветви, ни допуска: %d / %d", cen.branches, cen.admissions)
	}
	if len(f) == 0 {
		t.Fatal("ветка вне допуска в ТОЙ ЖЕ ветви не дала находки — гейт не способен упасть на своём предмете")
	}
	for _, x := range f {
		if x.step != "lst-stranger" {
			t.Fatalf("находка не называет координату: %q", x.step)
		}
		if x.branchCode != 200 {
			t.Fatalf("находка не называет код ветви: %d", x.branchCode)
		}
		if !strings.Contains(x.String(), "[403 404]") {
			t.Fatalf("находка не называет состав допуска — чинить по ней нельзя: %s", x)
		}
	}
}

// ─── законные близнецы ───────────────────────────────────────────────────────

// Полоса повтора: ветвь по коду вне допуска, но шаг несёт переход — ответ, на
// который она смотрит, до утверждений не доходит.
func TestRCB_RetryLaneIsLawfulAndCounted(t *testing.T) {
	step := nmStep("get-fresh", "GET", "{{baseUrl}}/vpc/v1/networks/{{netId}}",
		"if (pm.response.code === 403) {",
		"  pm.execution.setNextRequest(pm.info.requestName);",
		"  return;",
		"}",
		"pm.test('ok', () => pm.expect(pm.response.code).to.eql(200));",
	)
	f, cen := rcbAudit(t, nmFolder("NET-GET-CRUD-OK", step))
	if len(f) != 0 {
		t.Fatalf("гейт покраснел на законной полосе повтора: %v", f)
	}
	if cen.retryLane != 1 {
		t.Fatalf("полоса повтора не попала в перепись — молчание неотличимо от слепоты: %d", cen.retryLane)
	}
}

// Допуск содержит код ветви: ветка — его пин, а не противоречие.
func TestRCB_BranchInsideAdmissionIsLawfulAndSilent(t *testing.T) {
	step := nmStep("list", "GET", rcbURL,
		"pm.test('page or refusal', () => pm.expect(pm.response.code).to.be.oneOf([200, 403]));",
		"if (pm.response.code === 200) {",
		"  pm.test('array', () => pm.expect(pm.response.json().items).to.be.an('array'));",
		"}",
	)
	f, cen := rcbAudit(t, nmFolder("LF-NLB-LST-OK", step))
	if len(f) != 0 {
		t.Fatalf("гейт покраснел там, где допуск код ветви СОДЕРЖИТ: %v", f)
	}
	if cen.branches != 1 || cen.admissions != 1 {
		t.Fatalf("шаг гейтом не прочитан: %d / %d", cen.branches, cen.admissions)
	}
}

// Ветвь по ЧУЖОМУ полю (`j.code` внутри тела) — не предмет: допуск шага говорит
// про код края. Иначе гейт краснел бы на разборе тела ответа.
func TestRCB_BranchOnForeignFieldIsNotABranchAtAll(t *testing.T) {
	step := nmStep("del-system", "DELETE", "{{baseUrl}}/iam/v1/roles/{{sysRoleId}}",
		"const j = pm.response.json();",
		"pm.test('sync refusal', () => pm.expect(pm.response.code).to.be.oneOf([400]));",
		"if (j.code === 9) { pm.test('precondition', () => pm.expect(j.code).to.eql(9)); }",
	)
	f, cen := rcbAudit(t, nmFolder("IAM-ROL-RD-DL-SYSTEM-NEG", step))
	if len(f) != 0 {
		t.Fatalf("гейт принял ветвь по чужому полю за ветвь по коду края: %v", f)
	}
	if cen.branches != 0 {
		t.Fatalf("ветвь по `j.code` засчитана веткой по коду ответа — предмет гейта размыт: %d", cen.branches)
	}
	if cen.admissions != 0 && cen.steps != 1 {
		t.Fatalf("шаг гейтом не прочитан: шагов %d", cen.steps)
	}
}

// Допуск, записанный оператором в НЕСКОЛЬКО СТРОК, обязан быть узнан: этой
// формой в корпусе записано большинство допусков, и построчный распознаватель
// вывел бы их все из-под наблюдения — не находкой, а невидимостью.
func TestRCB_MultiLineAdmissionIsRecognised(t *testing.T) {
	step := nmStep("lst-stranger", "GET", rcbURL,
		"pm.test('refused', () =>",
		"  pm.expect(pm.response.code, pm.response.text())",
		"    .to.be.oneOf([403, 404]));",
		"if (pm.response.code === 200) {",
		"  pm.test('empty page', () => pm.expect((pm.response.json().items || []).length).to.eql(0));",
		"}",
	)
	f, cen := rcbAudit(t, nmFolder("AZD-TGR-LST-STRANGER-DENIED", step))
	if cen.admissions != 1 {
		t.Fatalf("многострочный допуск не узнан — эта форма господствует в корпусе: %d", cen.admissions)
	}
	if len(f) == 0 {
		t.Fatal("многострочный допуск узнан, но противоречие по нему не найдено")
	}
}
