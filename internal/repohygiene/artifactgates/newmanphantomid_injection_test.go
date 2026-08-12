// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Доказательство того, что гейт фантомного идентификатора СПОСОБЕН упасть — и что
// падает он на существе, а не на форме.
//
// Все пробы гоняют ТУ ЖЕ функцию, что и гейт по дереву (`auditPublishedIdOutcome`),
// на синтетических коллекциях: проба, повторяющая логику гейта своей копией,
// доказывала бы свойство копии.
//
// У КАЖДОГО ОТРИЦАНИЯ ЗДЕСЬ ЕСТЬ ПАРНЫЙ ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ. «Находок ноль»
// само по себе неотличимо от «предикат ослеп и не увидел публикации», поэтому
// каждая молчащая проба дополнительно требует `publishing == 1`: гейт публикацию
// УВИДЕЛ и промолчал по существу, а не потому, что смотрел мимо.
package artifactgates

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ─── строительство синтетической коллекции ───────────────────────────────────

func nmStep(name, method, url string, script ...string) nmItem {
	return nmItem{
		Name:    name,
		Request: &nmRequest{Method: method, URL: json.RawMessage(`{"raw":` + mustJSON(url) + `}`)},
		Event:   []nmEvent{{Listen: "test", Script: nmScript{Exec: script}}},
	}
}

func mustJSON(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// nmCreateStep — шаг, публикующий идентификатор ресурса ровно в той форме, какую
// эмитит `save_from_response`: ручка операции и координата ресурса захватываются
// каждая в СВОЁМ блоке через одноимённую локальную `const v`.
func nmCreateStep(name, envVar string) nmItem {
	return nmStep(name, "POST", "{{baseUrl}}/vpc/v1/networks",
		"pm.test('status 200', () => pm.expect(pm.response.code).to.eql(200));",
		"pm.environment.set('opId', '');",
		"try {",
		"  const j = pm.response.json();",
		"  const v = (j.id);",
		"  if (v !== undefined && v !== null) pm.environment.set('opId', String(v));",
		"} catch (e) {}",
		"try {",
		"  const j = pm.response.json();",
		"  const v = (j.metadata && j.metadata.networkId);",
		"  if (v !== undefined && v !== null) pm.environment.set('"+envVar+"', String(v));",
		"} catch (e) {}",
	)
}

// nmPollStep — опрос операции. `tail` — то, что после утверждения `done`: пусто =
// дефект (утверждается только завершение), либо одна из законных записей исхода.
func nmPollStep(tail ...string) nmItem {
	base := []string{
		"if (!pm.environment.get('opId')) { return; }",
		"const j = pm.response.json();",
		"pm.test('operation done', () => pm.expect(j.done, JSON.stringify(j)).to.eql(true));",
	}
	return nmStep("poll-op-1", "GET", "{{baseUrl}}/operations/{{opId}}", append(base, tail...)...)
}

func nmWriteCollection(t *testing.T, dir, name string, folders ...nmItem) string {
	t.Helper()
	col := map[string]any{
		"info": map[string]any{"name": "synthetic"},
		"item": folders,
	}
	b, err := json.MarshalIndent(col, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	rel := filepath.Join("services", "synthetic", "tests", "newman", "collections", name)
	if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(rel)), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, rel), b, 0o600); err != nil {
		t.Fatal(err)
	}
	return rel
}

func nmFolder(name string, steps ...nmItem) nmItem {
	return nmItem{Name: name, Item: steps}
}

func nmAudit(t *testing.T, folders ...nmItem) ([]nmFinding, nmCensus) {
	t.Helper()
	dir := t.TempDir()
	rel := nmWriteCollection(t, dir, "synthetic.postman_collection.json", folders...)
	f, cen, err := auditPublishedIdOutcome(dir, []string{rel})
	if err != nil {
		t.Fatal(err)
	}
	return f, cen
}

// ─── красное на настоящем дефекте ────────────────────────────────────────────

func TestPhantomIdGateRedOnInjectedDefect(t *testing.T) {
	findings, cen := nmAudit(t, nmFolder("NET-CR-CRUD-OK — создание сети",
		nmCreateStep("create-net", "netId"),
		nmPollStep(), // исход не назван — только done
	))
	if cen.publishing != 1 {
		t.Fatalf("предикат публикации не увидел захвата: publishing=%d", cen.publishing)
	}
	if len(findings) != 1 {
		t.Fatalf("ожидалась ровно одна находка, получено %d: %v", len(findings), findings)
	}
	// Находка обязана НАЗЫВАТЬ координату: коллекцию, кейс, шаг и имя, которое он
	// опубликовал. Гейт, который лишь считает, чинить нечем.
	got := findings[0].String()
	for _, want := range []string{"synthetic.postman_collection.json", "NET-CR-CRUD-OK", "create-net", "netId"} {
		if !strings.Contains(got, want) {
			t.Errorf("находка не называет %q: %s", want, got)
		}
	}
}

// ЛОВУШКА ТЕМЫ: тот же дефект, но рядом лежит комментарий, дословно описывающий
// снятую защиту. Гейт по сырому тексту нашёл бы в нём и `pm.expect`, и `j.error`,
// и промолчал бы — тем увереннее, чем лучше защита описана.
func TestPhantomIdGateReadsCodeNotComment(t *testing.T) {
	findings, cen := nmAudit(t, nmFolder("NET-CR-CRUD-OK — создание сети",
		nmCreateStep("create-net", "netId"),
		nmPollStep(
			"// Здесь стояло: pm.test('operation succeeded', () => pm.expect(j.error).to.be.undefined);",
			"/* и блоком тоже: pm.expect(j.error, JSON.stringify(j)).to.not.exist */",
		),
	))
	if cen.publishing != 1 {
		t.Fatalf("предикат публикации не увидел захвата: publishing=%d", cen.publishing)
	}
	if len(findings) != 1 {
		t.Fatalf("комментарий, описывающий защиту, зачтён за саму защиту: находок %d", len(findings))
	}
}

// ─── молчание на законных близнецах той же формы ─────────────────────────────

func TestPhantomIdGateSilentOnLawfulSameShape(t *testing.T) {
	cases := []struct {
		name    string
		folders []nmItem
	}{
		{
			// Каноническая форма, которую дописывает проход генератора.
			name: "исход назван успехом",
			folders: []nmItem{nmFolder("NET-CR-CRUD-OK",
				nmCreateStep("create-net", "netId"),
				nmPollStep(
					"(function () {",
					"  var _po; try { _po = pm.response.json(); } catch (e) { return; }",
					"  if (_po.error) { pm.environment.unset('netId'); }",
					"  pm.test('operation succeeded (no phantom netId)', function () {",
					"    pm.expect(_po.error && JSON.stringify(_po.error), 'operation.error').to.eql(undefined);",
					"  });",
					"})();",
				))},
		},
		{
			// Зеркальная форма: ПРЕДМЕТ кейса — отказ операции. Потребовать здесь
			// «операция успешна» значило бы сломать кейс на верном поведении.
			name: "исход назван отказом",
			folders: []nmItem{nmFolder("NET-CR-NEG-QUOTA",
				nmCreateStep("create-net", "netId"),
				nmPollStep("pm.test('op refused', () => pm.expect(j.error && j.error.code, JSON.stringify(j)).to.eql(9));"))},
		},
		{
			// Запись через переменную окружения — та, которую скелет со стёртыми
			// строками потерял бы, а гейт обязан признать.
			name: "исход прочитан из lastOpError",
			folders: []nmItem{nmFolder("NET-CR-CRUD-OK",
				nmCreateStep("create-net", "netId"),
				nmPollStep("pm.test('op ok', () => pm.expect(pm.environment.get('lastOpError')||'').to.eql(''));"))},
		},
		{
			// Синхронная операция: `done:true` приходит в ответе на саму мутацию,
			// опрашивать нечего, и исход названа она сама. Требовать опроса здесь
			// значило бы требовать невозможного.
			name: "синхронная мутация назвала исход у себя",
			folders: []nmItem{nmFolder("IRG-CR-CRUD-OK",
				nmStep("create-region", "POST", "{{baseUrl}}/geo/v1/internal/regions",
					"pm.test('status 200', () => pm.expect(pm.response.code).to.eql(200));",
					"pm.test('accepted', () => {",
					"  const j = pm.response.json();",
					"  pm.expect(Boolean(j.error), JSON.stringify(j.error || {})).to.eql(false);",
					"});",
					"try {",
					"  const j = pm.response.json();",
					"  const v = (j.metadata && j.metadata.regionId);",
					"  if (v !== undefined && v !== null) pm.environment.set('regionId', String(v));",
					"} catch (e) {}",
				))},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			findings, cen := nmAudit(t, tc.folders...)
			// ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: молчание засчитывается только если публикацию
			// гейт увидел. Иначе «ноль находок» означало бы «ноль прочитанного».
			if cen.publishing != 1 {
				t.Fatalf("гейт не увидел публикации — молчание ничего не значит: publishing=%d", cen.publishing)
			}
			if len(findings) != 0 {
				t.Fatalf("законная форма помечена находкой: %v", findings)
			}
		})
	}
}

// Ручка операции — не координата ресурса: шаг, публикующий ТОЛЬКО `opId`, предметом
// этого гейта не является. Без учёта области видимости локальной `const v` он был
// бы принят за публикацию ресурса, и проход дописывал бы защиту в пустоту.
func TestPhantomIdGateIgnoresOperationHandleCapture(t *testing.T) {
	findings, cen := nmAudit(t, nmFolder("NET-DEL-CRUD-OK",
		nmStep("del-net", "DELETE", "{{baseUrl}}/vpc/v1/networks/{{netId}}",
			"pm.environment.set('opId', '');",
			"try {",
			"  const j = pm.response.json();",
			"  const v = (j.id);",
			"  if (v !== undefined && v !== null) pm.environment.set('opId', String(v));",
			"} catch (e) {}"),
		nmPollStep(),
	))
	if cen.chains != 1 {
		t.Fatalf("цепочка мутация→опрос не построена: chains=%d", cen.chains)
	}
	if cen.publishing != 0 || len(findings) != 0 {
		t.Fatalf("захват ручки операции принят за публикацию ресурса: publishing=%d, findings=%v",
			cen.publishing, findings)
	}
}

// Между созданием и его опросом законно стоит другая мутация — отмена ТОЙ ЖЕ
// операции. Правило «опрос принадлежит последней мутации» отдало бы опрос отмене,
// и создание осталось бы без читателя исхода: гейт молчал бы на настоящем дефекте.
func TestPhantomIdGateAttributesPollByOperationVariable(t *testing.T) {
	build := func(tail ...string) []nmItem {
		return []nmItem{nmFolder("AZD-OP-CANCEL-NON-CREATOR-DENIED",
			nmCreateStep("cr-as-A", "netId"),
			nmStep("cancel-as-B", "POST", "{{baseUrl}}/operations/{{opId}}:cancel",
				"pm.test('rejected', () => pm.expect(pm.response.code).to.be.oneOf([400, 403, 404, 409]));"),
			nmPollStep(tail...),
		)}
	}

	findings, cen := nmAudit(t, build()...)
	if cen.publishing != 1 {
		t.Fatalf("предикат публикации не увидел захвата: publishing=%d", cen.publishing)
	}
	if len(findings) != 1 || !strings.Contains(findings[0].String(), "cr-as-A") {
		t.Fatalf("опрос отнесён не к тому шагу — дефект создания не назван: %v", findings)
	}

	findings, cen = nmAudit(t, build(
		"pm.test('operation succeeded (no phantom netId)', () => "+
			"pm.expect(j.error, JSON.stringify(j)).to.be.undefined);")...)
	if cen.publishing != 1 {
		t.Fatalf("предикат публикации не увидел захвата: publishing=%d", cen.publishing)
	}
	if len(findings) != 0 {
		t.Fatalf("исход назван, а гейт всё равно нашёл находку: %v", findings)
	}
}
