// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Доказательство того, что гейт захвата без утверждения СПОСОБЕН упасть — и что
// падает он на существе, а не на форме.
//
// Все пробы гоняют ТУ ЖЕ функцию, что и гейт по дереву
// (`auditCapturedVarAssertions`): проба, повторяющая логику гейта своей копией,
// доказывала бы свойство копии, а не гейта.
//
// У КАЖДОГО ОТРИЦАНИЯ ЗДЕСЬ ЕСТЬ ПАРНЫЙ ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ. «Находок ноль»
// само по себе неотличимо от «предикат захвата ослеп», поэтому каждая молчащая
// проба дополнительно утверждает ОЖИДАЕМОЕ число увиденных захватов: гейт захват
// увидел и промолчал по существу, а не потому, что смотрел мимо.
//
// ФИКСТУРА ПРИВЯЗАНА К ДЕРЕВУ, А НЕ К ПАМЯТИ АВТОРА. Синтетический шаг повторяет
// форму, которую эмитит `save_from_response`, — и отдельная проба берёт НАСТОЯЩИЙ
// шаг из закоммиченных коллекций, снимает с него утверждения и требует красного.
// Сменится форма захвата в генераторах — эта проба скажет об этом сама, вместо
// того чтобы синтетика продолжала доказывать свойство вчерашнего дерева.
package artifactgates

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// nmCapturedVarAudit — прогон гейтовой функции по синтетическому дереву.
func nmCapturedVarAudit(t *testing.T, folders ...nmItem) ([]nmCapturedVarFinding, nmCapturedVarCensus) {
	t.Helper()
	dir := t.TempDir()
	rel := nmWriteCollection(t, dir, "synthetic.postman_collection.json", folders...)
	f, cen, err := auditCapturedVarAssertions(dir, []string{rel})
	if err != nil {
		t.Fatal(err)
	}
	return f, cen
}

// nmCaptureLines — захват РОВНО в той форме, какую эмитит `save_from_response`
// каждого генератора: разбор тела в локальную `const j`, значение в локальную
// `const v`, публикация под именем окружения. Ни одного утверждения в этих
// строках нет — и в этом весь предмет: отвергнутая мутация тела не несёт,
// присваивание не исполняется, `catch` глотает разбор, шаг зеленеет.
func nmCaptureLines(envVar string) []string {
	return []string{
		"try {",
		"  const j = pm.response.json();",
		"  const v = (j.metadata && j.metadata.subnetId);",
		"  if (v !== undefined && v !== null) pm.environment.set('" + envVar + "', String(v));",
		"} catch (e) {}",
	}
}

// ─── красное на настоящем дефекте ────────────────────────────────────────────

func TestCapturedVarGateRedOnInjectedDefect(t *testing.T) {
	findings, cen := nmCapturedVarAudit(t, nmFolder("LST-CR-CRUD-OK — создание слушателя",
		nmStep("setup-subnet", "POST", "{{baseUrl}}/vpc/v1/subnets", nmCaptureLines("lstSubnetId")...),
	))
	if cen.capturing != 1 {
		t.Fatalf("предикат захвата не увидел публикации: capturing=%d — гейт смотрел мимо, "+
			"и его молчание ничего бы не значило", cen.capturing)
	}
	if len(findings) != 1 {
		t.Fatalf("ожидалась ровно одна находка, получено %d: %v", len(findings), findings)
	}
	// Находка обязана НАЗЫВАТЬ координату: коллекцию, кейс, шаг и захваченное имя.
	// Гейт, который лишь считает, чинить нечем — а чинят по его тексту.
	got := findings[0].String()
	for _, want := range []string{
		"synthetic.postman_collection.json", "LST-CR-CRUD-OK", "setup-subnet", "lstSubnetId", "POST",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("находка не называет %q: %s", want, got)
		}
	}
}

// ЛОВУШКА ТЕМЫ: тот же дефект, но рядом лежит комментарий, дословно описывающий
// снятое утверждение. Гейт по сырому тексту нашёл бы в нём и `pm.test`, и
// `pm.expect` — и промолчал бы тем увереннее, чем лучше защита описана.
// Комментарии такого вида дописывают сами проходы генератора, поэтому ловушка не
// выдумана.
func TestCapturedVarGateReadsCodeNotComment(t *testing.T) {
	script := append([]string{
		"// Здесь стояло: pm.test('status 200', () => pm.expect(pm.response.code).to.eql(200));",
		"/* и блоком тоже: pm.response.to.have.status(200) */",
	}, nmCaptureLines("lstSubnetId")...)

	findings, cen := nmCapturedVarAudit(t, nmFolder("LST-CR-CRUD-OK — создание слушателя",
		nmStep("setup-subnet", "POST", "{{baseUrl}}/vpc/v1/subnets", script...),
	))
	if cen.capturing != 1 {
		t.Fatalf("предикат захвата ослеп: capturing=%d", cen.capturing)
	}
	if len(findings) != 1 {
		t.Fatalf("комментарий, описывающий утверждение, зачтён за само утверждение: находок %d", len(findings))
	}
}

// ─── молчание на законных близнецах той же формы ─────────────────────────────

func TestCapturedVarGateSilentOnLawfulSameShape(t *testing.T) {
	cases := []struct {
		name          string
		step          nmItem
		wantCapturing int
		why           string
	}{
		{
			// Каноническая форма фикстуры: утверждение об успехе плюс захват.
			name: "утверждение об успехе рядом с захватом",
			step: nmStep("setup-subnet", "POST", "{{baseUrl}}/vpc/v1/subnets",
				append([]string{
					"pm.test('status 200', () => pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
				}, nmCaptureLines("lstSubnetId")...)...),
			wantCapturing: 1,
			why:           "утверждение есть — предмета запрета нет",
		},
		{
			// ГЛАВНЫЙ БЛИЗНЕЦ: шаг, чей ПРЕДМЕТ — отказ. Требовать от него успеха
			// было бы неверно, и гейт этого не делает: он требует НАЛИЧИЯ
			// утверждения, а не его содержания. Поэтому отрицательные кейсы
			// проходят by construction, и списка исключений заводить не нужно.
			name: "утверждение об ОТКАЗЕ рядом с захватом (предмет отрицательного кейса)",
			step: nmStep("create-cross-project", "POST", "{{baseUrl}}/nlb/v1/targetGroups",
				append([]string{
					"pm.test('cross-project create refused', () => " +
						"pm.expect(pm.response.code, pm.response.text()).to.be.oneOf([400, 403, 404]));",
				}, nmCaptureLines("tgCrossId")...)...),
			wantCapturing: 1,
			why:           "отказ утверждён — шаг может упасть, и падает он на своём предмете",
		},
		{
			// Мягкий посев, ничего не публикующий: у vpc это записанное решение
			// с доводом (зависимый кейс сам показывает отсутствие пула).
			// Захвата нет — предмета запрета нет.
			name: "мягкий посев без захвата",
			step: nmStep("_SETUP-POOL-ANYCAST", "POST", "{{internalBaseUrl}}/vpc/v1/addressPools",
				"// setup-only: посев мягкий, зависимый кейс показывает отсутствие пула сам."),
			wantCapturing: 0,
			why:           "публиковать нечего — координата никуда не уезжает",
		},
		{
			// Сброс имени в пустое — не захват: значение не происходит от ответа.
			// Требовать за него утверждения значило бы ловить форму вместо существа.
			name: "сброс имени в пустое",
			step: nmStep("cleanup", "POST", "{{baseUrl}}/nlb/v1/targetGroups/{{tgId}}:removeTargets",
				"pm.environment.set('opId', '');"),
			wantCapturing: 0,
			why:           "значение не из ответа — фантом отсюда не родится",
		},
		{
			// Настоящая форма из дерева (vpc, состязательный шаг): значение
			// собирается из ответов СОБСТВЕННЫХ подзапросов (`pm.sendRequest`), а
			// не из ответа шага. Утверждает о нём следующий шаг, читающий
			// `burstResults`. Гейт обязан молчать: его предмет — свой ответ.
			name: "захват из подзапросов, а не из своего ответа",
			step: nmStep("burst-create-overlap", "POST", "{{baseUrl}}/vpc/v1/networks",
				"const results = [];",
				"pm.sendRequest({url: pm.environment.get('baseUrl') + '/vpc/v1/subnets', method: 'POST'}, (err, res) => {",
				"  results.push({ code: res ? res.code : 0 });",
				"  pm.environment.set('burstResults', JSON.stringify(results));",
				"});"),
			wantCapturing: 0,
			why:           "своего ответа шаг не читает — утверждает о нём следующий",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			findings, cen := nmCapturedVarAudit(t, nmFolder("CASE-ID — синтетический кейс", tc.step))
			if cen.steps != 1 {
				t.Fatalf("обход не прочитал шаг: steps=%d", cen.steps)
			}
			if cen.capturing != tc.wantCapturing {
				t.Fatalf("захватов увидено %d, ожидалось %d — молчание гейта значило бы "+
					"не то, что проверяется", cen.capturing, tc.wantCapturing)
			}
			if len(findings) != 0 {
				t.Fatalf("гейт нашёл находку там, где её нет (%s): %v", tc.why, findings)
			}
		})
	}
}

// ─── инъекция НАСТОЯЩИМ входом из дерева ─────────────────────────────────────

// TestCapturedVarGateRedOnStrippedTreeStep берёт закоммиченный шаг, который
// захватывает имя И несёт утверждение, снимает с него строки утверждений и
// требует, чтобы гейт покраснел.
//
// Зачем при наличии синтетики: синтетический шаг доказывает свойство формы,
// которую автор ПОМНИЛ. Эта проба доказывает свойство формы, которая в дереве
// ЛЕЖИТ, и истекает сама — сменится форма захвата в генераторах, и предпосылка
// («в дереве есть шаг, который захватывает и утверждает») перестанет выполняться
// вслух, а не молча.
func TestCapturedVarGateRedOnStrippedTreeStep(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	var cols []string
	for rel := range tt.files {
		if strings.Contains(rel, "/tests/newman/collections/") && strings.HasSuffix(rel, ".json") {
			cols = append(cols, rel)
		}
	}
	sort.Strings(cols)
	if len(cols) == 0 {
		t.Fatal("в индексе git нет коллекций newman — инъекции нечем питаться")
	}

	// Донор — ПЕРВЫЙ по порядку обхода шаг, который захватывает и утверждает.
	// Порядок детерминирован (пути отсортированы), поэтому проба не зависит от
	// того, в каком порядке файловая система отдала имена.
	var donorCol, donorCase, donorStep string
	var donorScript []string
	var donorVars []string
	for _, rel := range cols {
		b, err := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 -- путь получен из индекса git этого модуля
		if err != nil {
			t.Fatalf("чтение %s: %v", rel, err)
		}
		var col nmCollection
		if err := json.Unmarshal(b, &col); err != nil {
			t.Fatalf("разбор %s: %v", rel, err)
		}
		var walk func(items []nmItem, path []string)
		walk = func(items []nmItem, path []string) {
			for _, it := range items {
				if donorScript != nil {
					return
				}
				if it.isFolder() {
					walk(it.Item, append(path, it.Name))
					continue
				}
				src := it.testScript()
				vars := nmCapturedFromResponse(src)
				if len(vars) == 0 || !nmCarriesAssertion(src) {
					continue
				}
				donorCol, donorCase, donorStep = rel, strings.Join(path, " / "), it.Name
				donorVars = vars
				for _, ev := range it.Event {
					if ev.Listen == "test" {
						donorScript = ev.Script.Exec
					}
				}
			}
		}
		walk(col.Item, nil)
		if donorScript != nil {
			break
		}
	}
	if donorScript == nil {
		t.Fatal("предпосылка инъекции не выполняется: в дереве нет НИ ОДНОГО шага, который " +
			"захватывает имя из своего ответа и несёт утверждение. Либо форма захвата " +
			"сменилась, либо обход смотрит мимо; чинить надо гейт, а не выходить успехом")
	}
	t.Logf("донор инъекции: %s :: %s :: %s (публикует %s)",
		donorCol, donorCase, donorStep, strings.Join(donorVars, ","))

	// Снимаем СТРОКИ утверждений — ровно то, что делает невнимательная правка.
	var stripped []string
	for _, line := range donorScript {
		if nmCarriesAssertion(line) {
			continue
		}
		stripped = append(stripped, line)
	}

	findings, cen := nmCapturedVarAudit(t, nmFolder(donorCase,
		nmStep(donorStep, "POST", "{{baseUrl}}/synthetic", stripped...)))

	// Предпосылка самой инъекции: снятие утверждений НЕ должно было унести захват.
	// Если унесло — инъекция не воспроизвела дефект, и «красное» ничего не доказывает.
	if cen.capturing != 1 {
		t.Fatalf("снятие утверждений унесло и захват (capturing=%d) — инъекция не "+
			"воспроизвела дефект; донор %s :: %s", cen.capturing, donorCol, donorStep)
	}
	if len(findings) != 1 {
		t.Fatalf("гейт не покраснел на настоящем шаге дерева с снятыми утверждениями "+
			"(%s :: %s): находок %d", donorCol, donorStep, len(findings))
	}
}

// ─── ось ОТЛОЖЕННОЙ ИНИЦИАЛИЗАЦИИ ────────────────────────────────────────────
//
// Пробы выше доказывают свойство гейта на форме `const j = pm.response.json()` —
// объявление С инициализатором. Ровно её и узнавал распознаватель связывания, и
// потому 114 шагов дерева публиковали координату НЕВИДИМО для него: разбор тела
// они записывают иначе — объявляют имя, а значение присваивают отдельным
// оператором внутри `try`.
//
// Форма не экзотическая и не выдуманная: это стандартная запись безопасного
// разбора в этом корпусе (`let j; try { j = pm.response.json(); } catch (e) { j =
// null; }`), и в ней класс жил при зелёном гейте. Ось проверяется отдельно,
// потому что молчание гейта на ней было неотличимо от чистого дерева.

// nmDeferredCaptureLines — захват с ОТЛОЖЕННОЙ инициализацией: имя объявлено на
// верхнем уровне, значение присвоено внутри `try`. Утверждений нет — предмет тот
// же, что у `nmCaptureLines`, отличается только запись связывания.
func nmDeferredCaptureLines(envVar string) []string {
	return []string{
		"let j; try { j = pm.response.json(); } catch (e) { j = {}; }",
		"const meta = j.metadata || {};",
		"const v = meta.subnetId || '';",
		"if (v) pm.environment.set('" + envVar + "', String(v));",
	}
}

func TestCapturedVarGateRedOnDeferredInitCapture(t *testing.T) {
	findings, cen := nmCapturedVarAudit(t, nmFolder("LST-CR-CRUD-OK — создание слушателя",
		nmStep("setup-subnet", "POST", "{{baseUrl}}/vpc/v1/subnets",
			nmDeferredCaptureLines("lstSubnetId")...),
	))
	if cen.capturing != 1 {
		t.Fatalf("захват с отложенной инициализацией не распознан: capturing=%d — "+
			"именно в этой слепоте класс и жил при зелёном гейте", cen.capturing)
	}
	if len(findings) != 1 {
		t.Fatalf("ожидалась ровно одна находка, получено %d: %v", len(findings), findings)
	}
	got := findings[0].String()
	for _, want := range []string{"LST-CR-CRUD-OK", "setup-subnet", "lstSubnetId"} {
		if !strings.Contains(got, want) {
			t.Errorf("находка не называет %q: %s", want, got)
		}
	}
}

// ПЕРЕПРИСВАИВАНИЕ УЖЕ ИНИЦИАЛИЗИРОВАННОГО ИМЕНИ — вторая запись той же слепоты и
// более коварная: имя объявлено с инициализатором, поэтому распознаватель его
// ВИДИТ, но связывает с безобидным начальным значением (`[]`), а присваивание из
// ответа приходит позже и глубже. Так устроен резолв каталога размещения в vpc,
// публикующий координату зоны для всего набора.
//
// Отдельно проверяется ОБЛАСТЬ ВИДИМОСТИ: присваивание стоит внутри `if { try { …
// } }`, а чтение — снаружи, на верхнем уровне. Считать глубиной связывания
// глубину присваивания значило бы закрыть имя вместе с блоком `try` — и гейт
// снова промолчал бы, теперь уже по другой причине.
func TestCapturedVarGateRedOnReassignedInitialisedName(t *testing.T) {
	findings, cen := nmCapturedVarAudit(t, nmFolder("SETUP — резолв каталога размещения",
		nmStep("_SETUP-ZONES", "GET", "{{baseUrl}}/geo/v1/zones",
			"const code = (pm.response && pm.response.code) || 0;",
			"let zs = [];",
			"if (code === 200) { try { zs = (pm.response.json().zones) || []; } catch (e) {} }",
			"const pick = zs.filter(z => !z.status);",
			"if (pick.length) {",
			"  pm.environment.set('existingZoneId', pick[0].id);",
			"}",
		),
	))
	if cen.capturing != 1 {
		t.Fatalf("переприсваивание инициализированного имени не распознано: capturing=%d", cen.capturing)
	}
	if len(findings) != 1 {
		t.Fatalf("ожидалась ровно одна находка, получено %d: %v", len(findings), findings)
	}
	if got := findings[0].String(); !strings.Contains(got, "existingZoneId") {
		t.Errorf("находка не называет опубликованное имя: %s", got)
	}
}

// ─── законные близнецы новой оси ─────────────────────────────────────────────
//
// Каждый утверждает ОЖИДАЕМОЕ число увиденных захватов: без этого «находок ноль»
// значило бы и «гейт промолчал по существу», и «гейт смотрел мимо».
func TestCapturedVarGateSilentOnLawfulDeferredShapes(t *testing.T) {
	cases := []struct {
		name          string
		lines         []string
		wantCapturing int
		why           string
	}{
		{
			name: "отложенная инициализация рядом с утверждением",
			lines: append([]string{
				"pm.test('status 200', () => pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
			}, nmDeferredCaptureLines("lstSubnetId")...),
			wantCapturing: 1,
			why:           "утверждение есть — предмета запрета нет",
		},
		{
			// Значение НЕ происходит от ответа: имя объявлено, присвоено из
			// окружения. Требовать за такую публикацию утверждения значило бы
			// ловить форму записи вместо существа — фантом отсюда не родится.
			name: "отложенная инициализация НЕ из ответа",
			lines: []string{
				"let v; try { v = pm.environment.get('runId'); } catch (e) { v = ''; }",
				"pm.environment.set('derivedName', 'sub-' + v);",
			},
			wantCapturing: 0,
			why:           "источник значения — окружение, а не ответ шага",
		},
		{
			// Присваивание ПОЛЮ объекта, а не имени: `acc.body = pm.response…`
			// связывает поле, а публикуется несвязанная константа. Распознаватель
			// обязан отсечь это предшествующей точкой, иначе первое же накопление
			// ответов в объект давало бы ложную находку.
			name: "присваивание полю объекта, публикация константы",
			lines: []string{
				"const acc = {};",
				"acc.body = pm.response.json();",
				"pm.environment.set('_marker', 'done');",
			},
			wantCapturing: 0,
			why:           "публикуется константа — координата ниоткуда не происходит",
		},
		{
			// Сравнение, а не присваивание: `code === 200` не связывает имя.
			// Заглядывание вперёд в распознавателе существует ради этого случая.
			name: "сравнение не считается связыванием",
			lines: []string{
				"let code; code = pm.response.code;",
				"if (code === 200) { pm.environment.set('_seen', '1'); }",
			},
			wantCapturing: 0,
			why:           "публикуется литерал; сравнение имени с числом связыванием не является",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			findings, cen := nmCapturedVarAudit(t, nmFolder("CASE-ID — синтетический кейс",
				nmStep("step", "POST", "{{baseUrl}}/vpc/v1/subnets", tc.lines...)))
			if cen.steps != 1 {
				t.Fatalf("обход не прочитал шаг: steps=%d", cen.steps)
			}
			if cen.capturing != tc.wantCapturing {
				t.Fatalf("захватов увидено %d, ожидалось %d — молчание гейта значило бы "+
					"не то, что проверяется", cen.capturing, tc.wantCapturing)
			}
			if len(findings) != 0 {
				t.Fatalf("гейт нашёл находку там, где её нет (%s): %v", tc.why, findings)
			}
		})
	}
}
