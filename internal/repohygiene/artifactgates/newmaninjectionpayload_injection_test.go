// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Доказательство того, что гейт соответствия «имя нагрузки ↔ её байты» СПОСОБЕН
// упасть — и что падает он на существе, а не на форме.
//
// Все пробы гоняют ТУ ЖЕ функцию, что и гейт по дереву
// (`auditInjectionPayloadNames`), на синтетических коллекциях: проба, повторяющая
// логику гейта своей копией, доказывала бы свойство копии.
//
// У КАЖДОГО МОЛЧАНИЯ ЗДЕСЬ ЕСТЬ ПАРНЫЙ КОНТРОЛЬ ПО ПЕРЕПИСИ. «Находок ноль» само
// по себе неотличимо от «предикат ослеп и нагрузки не увидел», поэтому каждая
// молчащая проба дополнительно требует ожидаемого `payloads`.
//
// Отрицательный вход берётся ДОСЛОВНО из дефекта #701: нагрузка `nullbyte`,
// несущая `x`, ПРОБЕЛ, `y`. Синтетический пример «лишь бы не совпало» доказывал
// бы, что гейт отличает нагрузку от мусора, а требуется, чтобы он отличал её от
// ПРАВДОПОДОБНОЙ подмены, дающей тот же исход на краю.
package artifactgates

import (
	"encoding/json"
	"strings"
	"testing"
)

// nmPayloadStep — шаг с телом: то, что генератор эмитит для нагрузки внедрения.
func nmPayloadStep(name, payload string) nmItem {
	body, err := json.Marshal(map[string]string{
		"projectId": "{{_suiteProjectId}}",
		"name":      payload,
	})
	if err != nil {
		panic(err)
	}
	return nmItem{
		Name: name,
		Request: &nmRequest{
			Method: "POST",
			URL:    json.RawMessage(`{"raw":"{{baseUrl}}/vpc/v1/networks"}`),
			Body:   &nmBody{Raw: string(body)},
		},
	}
}

// nmFixtureStep — шаг подготовки внутри того же кейса: тело есть, нагрузкой не
// является. Нужен, чтобы показать, что гейт не судит его как нагрузку.
func nmFixtureStep(name string) nmItem {
	return nmItem{
		Name: name,
		Request: &nmRequest{
			Method: "POST",
			URL:    json.RawMessage(`{"raw":"{{baseUrl}}/vpc/v1/networks"}`),
			Body:   &nmBody{Raw: `{"projectId": "{{_suiteProjectId}}", "name": "pre-net-{{runId}}"}`},
		},
	}
}

func nmPayloadAudit(t *testing.T, folders ...nmItem) ([]nmPayloadFinding, nmPayloadCensus) {
	t.Helper()
	dir := t.TempDir()
	rel := nmWriteCollection(t, dir, "synthetic.postman_collection.json", folders...)
	f, cen, err := auditInjectionPayloadNames(dir, []string{rel})
	if err != nil {
		t.Fatal(err)
	}
	return f, cen
}

// ─── красное на НАСТОЯЩЕМ дефекте #701 ───────────────────────────────────────

func TestInjectionPayloadGateRedOnNullbyteReplacedBySpace(t *testing.T) {
	findings, cen := nmPayloadAudit(t, nmFolder("NET-CR-SEC-NULLBYTE — Security probe: nullbyte in name",
		nmPayloadStep("cr-nullbyte-rya142", "x y"),
	))
	if cen.payloads != 1 {
		t.Fatalf("гейт не увидел нагрузки вовсе: payloads=%d — краснота ниже ничего бы не значила", cen.payloads)
	}
	if len(findings) != 1 {
		t.Fatalf("ожидалась ровно одна находка, получено %d: %v", len(findings), findings)
	}
	msg := findings[0].String()
	// Координата обязана быть в тексте: разбор по имени шага вместо текста отказа
	// стоит полного прогона (`testing.md` §«Чтение вердикта»).
	for _, want := range []string{"NET-CR-SEC-NULLBYTE", "cr-nullbyte-rya142", "nullbyte"} {
		if !strings.Contains(msg, want) {
			t.Errorf("текст находки не называет %q: %s", want, msg)
		}
	}
	// И БАЙТЫ: пробел от нулевого байта в выводе неотличим, если печатать строку
	// как есть, — а именно это различие и есть предмет #701.
	if !strings.Contains(msg, "782079") {
		t.Errorf("текст находки не называет байты нагрузки (ожидалось 78 20 79): %s", msg)
	}
}

// ─── законный близнец: та же нагрузка после фикса #701 ───────────────────────

func TestInjectionPayloadGateSilentOnRealNullByte(t *testing.T) {
	findings, cen := nmPayloadAudit(t, nmFolder("NET-CR-SEC-NULLBYTE — Security probe: nullbyte in name",
		nmPayloadStep("cr-nullbyte-rya142", "x\x00y"),
	))
	if cen.payloads != 1 {
		t.Fatalf("нагрузка не распознана: payloads=%d — молчание ниже неотличимо от слепоты", cen.payloads)
	}
	if len(findings) != 0 {
		t.Fatalf("на настоящем нулевом байте гейт краснеет: %v", findings)
	}
}

// ─── законные близнецы той же формы: шесть остальных видов ───────────────────

func TestInjectionPayloadGateSilentOnTheOtherSixKinds(t *testing.T) {
	folders := []nmItem{
		nmFolder("NET-CR-SEC-SQLI — sqli", nmPayloadStep("cr-sqli-r1", "test' OR 1=1--")),
		nmFolder("NET-CR-SEC-UNION — union", nmPayloadStep("cr-union-r2", "x' UNION SELECT * FROM operations--")),
		nmFolder("NET-CR-SEC-XSS — xss", nmPayloadStep("cr-xss-r3", "<script>alert(1)</script>")),
		nmFolder("NET-CR-SEC-CMD — cmd", nmPayloadStep("cr-cmd-r4", "; rm -rf / ;")),
		nmFolder("NET-CR-SEC-PATH — path", nmPayloadStep("cr-path-r5", "../../etc/passwd")),
		nmFolder("NET-CR-SEC-LONGPAYLOAD — long", nmPayloadStep("cr-longpayload-r6", strings.Repeat("A", 1000))),
	}
	findings, cen := nmPayloadAudit(t, folders...)
	if cen.payloads != 6 {
		t.Fatalf("распознано нагрузок %d из 6 — молчание ниже неотличимо от слепоты", cen.payloads)
	}
	if len(findings) != 0 {
		t.Fatalf("на законных нагрузках гейт краснеет: %v", findings)
	}
}

// Каждый предикат обязан иметь СВОЙ производитель отказа: без этого «шесть видов
// молчат» держалось бы на одном работающем предикате и пяти тождественно-истинных.
func TestEachKindPredicateHasItsOwnProducer(t *testing.T) {
	// Подмена — правдоподобная: строка того же назначения, у которой снят ровно
	// тот признак, который обещает имя вида.
	swapped := map[string]string{
		"sqli":        "test OR 1=1",                       // без апострофа
		"union":       "x' SELECT * FROM operations",       // без UNION
		"xss":         "&lt;script&gt;alert(1)",            // тег экранирован
		"cmd":         "rm -rf /",                          // без метасимвола
		"path":        "..\\..\\etc\\passwd",               // без `../`
		"nullbyte":    "x y",                               // пробел вместо 0x00
		"longpayload": strings.Repeat("A", nmMaxNameLen()), // ровно предел, не больше
	}
	for _, c := range nmInjectionClaims {
		payload, ok := swapped[c.kind]
		if !ok {
			t.Fatalf("для вида %q нет подмены — проба не покрывает всю таблицу гейта", c.kind)
		}
		folder := nmFolder("NET-CR-SEC-"+strings.ToUpper(c.kind)+" — probe",
			nmPayloadStep("cr-"+c.kind+"-r1", payload))
		findings, cen := nmPayloadAudit(t, folder)
		if cen.payloads != 1 {
			t.Fatalf("вид %q: нагрузка не распознана (payloads=%d)", c.kind, cen.payloads)
		}
		if len(findings) != 1 {
			t.Errorf("вид %q: подмена не поймана (находок %d) — предикат этого вида "+
				"тождественно истинен и ничего не проверяет", c.kind, len(findings))
		}
	}
}

// nmMaxNameLen — предел имени, тот же, что читает гейт. Вынесено функцией, чтобы
// проба не выписывала число: сменят форму имени — поедет и она.
func nmMaxNameLen() int {
	for _, c := range nmInjectionClaims {
		if c.kind == "longpayload" {
			// Двоичный поиск не нужен: достаточно найти наибольшую длину, на
			// которой предикат ещё молчит.
			for n := 1; n <= 4096; n++ {
				if c.holds(strings.Repeat("A", n)) {
					return n - 1
				}
			}
		}
	}
	return 0
}

// ─── фикстурный шаг внутри кейса нагрузкой НЕ считается ──────────────────────

func TestFixtureStepInsideSecCaseIsNotReadAsPayload(t *testing.T) {
	findings, cen := nmPayloadAudit(t, nmFolder("SUB-CR-SEC-PATH — path",
		nmFixtureStep("pre-create-net"),
		nmPayloadStep("cr-path-rya352", "../../etc/passwd"),
	))
	if cen.payloads != 1 {
		t.Fatalf("нагрузок распознано %d, ожидалась ровно одна: фикстурный шаг либо "+
			"засчитан нагрузкой, либо заслонил её", cen.payloads)
	}
	if len(findings) != 0 {
		t.Fatalf("гейт краснеет на законном кейсе с фикстурой: %v", findings)
	}
}

// ─── вторая форма имени шага (`<id> :: <шаг>`) читается наравне с первой ──────

func TestPrefixedStepNameIsRecognised(t *testing.T) {
	const folderName = "IAM-ACC-CR-SEC-INJECTION — Security: SQL injection in name"

	ok, cenOK := nmPayloadAudit(t, nmFolder(folderName,
		nmItemRename(nmPayloadStep("sec-sqli", "test' OR 1=1--"), "IAM-ACC-CR-SEC-INJECTION :: sec-sqli")))
	if cenOK.payloads != 1 {
		t.Fatalf("шаг с префиксом кейса не распознан: payloads=%d — половина дерева "+
			"была бы гейту невидима", cenOK.payloads)
	}
	if len(ok) != 0 {
		t.Fatalf("на законной нагрузке в префиксной форме гейт краснеет: %v", ok)
	}

	bad, cenBad := nmPayloadAudit(t, nmFolder(folderName,
		nmItemRename(nmPayloadStep("sec-sqli", "test OR 1=1"), "IAM-ACC-CR-SEC-INJECTION :: sec-sqli")))
	if cenBad.payloads != 1 || len(bad) != 1 {
		t.Fatalf("в префиксной форме подмена не поймана: payloads=%d, находок %d",
			cenBad.payloads, len(bad))
	}
}

func nmItemRename(it nmItem, name string) nmItem {
	it.Name = name
	return it
}

// ─── кейс внедрения без узнанной нагрузки ────────────────────────────────────

func TestSecCaseWithoutRecognisedPayloadIsAFinding(t *testing.T) {
	findings, cen := nmPayloadAudit(t, nmFolder("NET-CR-SEC-UNICODE — новый вид, предиката нет",
		nmPayloadStep("cr-unicode-r1", "‮evil"),
	))
	if cen.secFolders != 1 {
		t.Fatalf("кейс внедрения не распознан: secFolders=%d", cen.secFolders)
	}
	if len(findings) != 1 {
		t.Fatalf("новый вид без предиката прошёл молча (находок %d) — это слепая зона "+
			"ровно на следующей нагрузке: %v", len(findings), findings)
	}
	if !strings.Contains(findings[0].String(), "NET-CR-SEC-UNICODE") {
		t.Errorf("находка не называет кейс: %s", findings[0])
	}
}

// ─── запись списка исключений: молчит при предмете, истекает без него ────────

func TestWaivedSecCaseIsSilentAndIsCountedAsUsed(t *testing.T) {
	const waived = "LST-CR-SEC-TG-CROSS-PROJECT"
	if _, ok := nmSecFoldersWithoutPayload[waived]; !ok {
		t.Fatalf("проба опирается на запись %q, которой в списке гейта нет — "+
			"предпосылка пробы сломана, её молчание ничего не доказывает", waived)
	}
	findings, cen := nmPayloadAudit(t, nmFolder(waived+" — Create listener wiring a Target Group",
		nmFixtureStep("setup-tg-cross"),
	))
	if len(findings) != 0 {
		t.Fatalf("гейт краснеет на кейсе, у которого нагрузки нет by construction: %v", findings)
	}
	if !cen.waiversUsed[waived] {
		t.Errorf("запись %q не отмечена как имеющая предмет — тогда гейт по дереву "+
			"объявил бы её истёкшей, хотя предмет есть", waived)
	}
}

func TestWaiverIsReportedUnusedWhenItsSubjectIsGone(t *testing.T) {
	_, cen := nmPayloadAudit(t, nmFolder("NET-CR-SEC-PATH — path",
		nmPayloadStep("cr-path-r1", "../../etc/passwd"),
	))
	if len(cen.waiversUsed) != 0 {
		t.Fatalf("на дереве без исключаемого кейса запись всё равно отмечена как "+
			"имеющая предмет (%v) — самоистечение не работает", cen.waiversUsed)
	}
}

// ─── перепись отличает «ноль находок» от «ноль прочитанного» ─────────────────

func TestEmptyTreeYieldsZeroCensusNotSilentGreen(t *testing.T) {
	findings, cen := nmPayloadAudit(t, nmFolder("NET-CR-CRUD-OK — обычный кейс",
		nmFixtureStep("cr-net"),
	))
	if len(findings) != 0 {
		t.Fatalf("гейт краснеет на кейсе, который к внедрению не относится: %v", findings)
	}
	if cen.secFolders != 0 || cen.payloads != 0 {
		t.Fatalf("обычный кейс засчитан внедрением: secFolders=%d, payloads=%d",
			cen.secFolders, cen.payloads)
	}
	// Именно на таком дереве гейт по дереву обязан упасть по СВОЕЙ предпосылке —
	// «нагрузок распознано 0» это не зелёный вердикт.
}
