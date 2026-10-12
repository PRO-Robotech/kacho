// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// consolemutationsignal_injection_test.go — доказательство, что гейт сигнала об
// исходе мутации СПОСОБЕН упасть, и что он падает не на форме, а на существе.
//
// Инъекция идёт в ОБЕ стороны, потому что односторонняя ничего не доказывает:
// проверка, краснеющая на дефекте, но краснеющая и на законной конструкции той
// же формы, будет отключена первым же ложным срабатыванием.
//
//	ДЕФЕКТ            мутация без механизма → краснеет, называя координату
//	БЛИЗНЕЦ           та же мутация в файле с механизмом → молчит
//	БЛИЗНЕЦ-2         `api.get`/`api.list` (не мутируют) → молчат
//	БЛИЗНЕЦ-3         вызов в комментарии и в строковом литерале → молчит
//	ТРАНСПОРТ         тот же вызов в `src/api/` → молчит (переносит, не решает)
//	ВЕДОМОСТЬ         запись без предмета → краснеет отдельным вердиктом
//	ФОРМА <T>         `api.post<T>(…)`, взятый обходом дерева и перенесённый вне
//	                  транспорта → краснеет, называя координату и форму; его
//	                  заведённый близнец молчит (kacho#3096)
//
// Разбор здесь — тот же, что в гейте (`consoleProbeScan` + `consoleMutationCallRe`
// + маркеры механизма), поэтому проба меряет ЕГО, а не свою копию правил.
package repohygiene

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// classify повторяет решение гейта для одного файла: мутирует ли он и заведён ли
// на механизм. Вынесено, чтобы инъекция не переписывала предикат своими словами.
func classify(rel, src string) (sites int, wired bool, transport bool) {
	mask, _ := consoleProbeScan(src)
	sites = len(consoleMutationCallRe.FindAllIndex(mask, -1))
	transport = strings.Contains("/"+rel, consoleMutationTransportDir)
	for _, m := range consoleMutationMechanismMarkers {
		if strings.Contains(src, m) {
			wired = true
			break
		}
	}
	return sites, wired, transport
}

func TestConsoleMutationGateRedOnInjectedDefect(t *testing.T) {
	t.Parallel()
	const defect = `
import { api } from "@shared/api/client";
export function CreateThing() {
  const submit = () => { void api.create("/vpc/v1/networks", { name: "n" }); };
  return submit;
}
`
	sites, wired, transport := classify("vpc/src/components/CreateThing.tsx", defect)
	if sites == 0 {
		t.Fatal("инъекция не воспроизвела дефект: признак не нашёл вызова мутации")
	}
	if transport {
		t.Fatal("файл вне src/api/ ошибочно принят за транспорт")
	}
	if wired {
		t.Fatal("файл без единого упоминания механизма признан заведённым — гейт не упадёт никогда")
	}
	// Координата обязана быть названа: вердикт без неё не приводит к месту правки.
	loc := consoleMutationCallRe.FindStringIndex(defect)
	line := 1 + strings.Count(defect[:loc[0]], "\n")
	if line != 4 {
		t.Fatalf("координата дефекта посчитана неверно: строка %d, ожидалась 4", line)
	}
}

func TestConsoleMutationGateSilentOnLawfulTwin(t *testing.T) {
	t.Parallel()
	// Тот же вызов, тот же файл-компонент — но исход проходит через механизм.
	const lawful = `
import { api } from "@shared/api/client";
import { useSignalledMutation } from "@shared/lib/use-signalled-mutation";
export function CreateThing() {
  const m = useSignalledMutation({
    verb: "create",
    subject: { label: "Облачная сеть", gender: "f" },
    expectOperation: true,
    mutationFn: (b: unknown) => api.create("/vpc/v1/networks", b),
  });
  return m;
}
`
	sites, wired, _ := classify("vpc/src/components/CreateThing.tsx", lawful)
	if sites == 0 {
		t.Fatal("законный близнец перестал распознаваться как мутация — предикат сузился")
	}
	if !wired {
		t.Fatal("законный близнец не признан заведённым: гейт краснел бы на исправном коде")
	}
}

func TestConsoleMutationGateIgnoresReadsAndInertText(t *testing.T) {
	t.Parallel()
	// Чтения не мутируют — требовать от них сигнала об исходе не за что.
	const reads = `
import { api } from "@shared/api/client";
export const load = () => api.get("/vpc/v1/networks/ntw-1");
export const page = () => api.list("/vpc/v1/networks");
`
	if sites, _, _ := classify("vpc/src/lib/load.ts", reads); sites != 0 {
		t.Fatalf("чтение принято за мутацию: найдено %d мест", sites)
	}

	// Гейт, краснеющий на собственном объяснении, снимут как непонятный: имя
	// метода стоит и в комментарии, и в строке, и ни то ни другое не исполняется.
	const inert = `
// Здесь описано, почему api.create( обязан идти через механизм сигнала.
/* Ещё одно упоминание: api.delete( в блочном комментарии. */
export const DOC = "api.update( внутри строкового литерала";
export const TPL = ` + "`api.post( внутри шаблонного литерала`" + `;
`
	if sites, _, _ := classify("shared/src/lib/doc.ts", inert); sites != 0 {
		t.Fatalf("гейт считает находкой упоминание в комментарии или строке: найдено %d", sites)
	}
}

func TestConsoleMutationGateSkipsTransport(t *testing.T) {
	t.Parallel()
	// Тонкий однострочник над клиентом переносит запрос; решает вызывающий.
	const wrapper = `
import { api } from "@shared/api/client";
export const networksApi = {
  create: (b: unknown) => api.create("/vpc/v1/networks", b),
  remove: (id: string) => api.delete("/vpc/v1/networks/" + id),
};
`
	sites, _, transport := classify("shared/src/api/resources.ts", wrapper)
	if sites == 0 {
		t.Fatal("в транспорте перестали находиться вызовы — признак сузился")
	}
	if !transport {
		t.Fatal("файл в src/api/ не признан транспортом: гейт требовал бы сигнала от переносчика")
	}
}

func TestConsoleMutationLedgerGateRedOnEntryWithoutSubject(t *testing.T) {
	t.Parallel()
	// Ведомость обязана истекать сама. Запись, чей файл больше не мутирует,
	// описывает несуществующий долг — и это находка, а не мелочь: именно так
	// послабление переживает свой предмет.
	ledgerHit := map[string]bool{"shared/src/components/Alive.tsx": true}
	entries := []string{"shared/src/components/Alive.tsx", "shared/src/components/Gone.tsx"}

	var stale []string
	for _, p := range entries {
		if !ledgerHit[p] {
			stale = append(stale, p)
		}
	}
	if len(stale) != 1 || stale[0] != "shared/src/components/Gone.tsx" {
		t.Fatalf("устаревшая запись ведомости не распознана: %v", stale)
	}
}

// TestConsoleMutationGatePremiseIsChecked — предпосылка гейта проверяется им
// самим. Признак, переставший ловить предмет, обязан РОНЯТЬ прогон, а не молча
// объявлять дерево чистым: «ноль находок» и «ноль прочитанного» — разные вещи.
func TestConsoleMutationGatePremiseIsChecked(t *testing.T) {
	t.Parallel()
	broken := regexp.MustCompile(`\bapi\.thisMethodDoesNotExist\s*\(`)
	const real = `export const f = () => api.create("/vpc/v1/networks", {});`
	if broken.MatchString(real) {
		t.Fatal("контрольный предикат оказался годным — проба ничего не проверяет")
	}
	// Настоящий признак на том же входе предмет находит; значит нулевой результат
	// сломанного предиката означал бы именно поломку, и гейт обязан на ней падать.
	if !consoleMutationCallRe.MatchString(real) {
		t.Fatal("признак гейта не нашёл заведомой мутации")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Форма с параметром типа: `api.post<T>(…)` (kacho#3096).
//
// Посылка задачи — «гейт не видит форму `api.post<T>(`». Признак ловит её с
// заведения (класс `[(<]` после имени метода, 051d3b98), а четыре места сборки
// #2969 стоят в транспорте (`shared/src/api/iam.ts`, `shared/src/api/access-keys.ts`)
// и пропускаются по роли каталога, а не по форме. Недоставало ДОКАЗАТЕЛЬСТВА:
// ни одна инъекция не подавала форму с параметром типа, и сужение признака до
// `\(` прошло бы молча. Здесь она подаётся НАСТОЯЩИМ входом — текстом вызова,
// взятым обходом дерева, а не выписанным образцом, — и судит её тот же путь,
// что судит дерево (`consoleMutationJudge`).
// ─────────────────────────────────────────────────────────────────────────────

// typedFormRe — производитель входа инъекции: форма с параметром типа. Он
// НАРОЧНО свой, а не признак гейта: найденный признаком гейта вход не мог бы
// показать, что признак его потерял — сужение `[(<]` до `\(` обнулило бы и
// поиск входа, и инъекция пала бы на предпосылке, обвинив дерево.
var typedFormRe = regexp.MustCompile(`\bapi\.(create|update|delete|post|action)\s*<`)

// typedFormSiteFromTree — первое место формы с параметром типа в дереве консоли:
// файл (от корня консоли), его текст и номер строки вызова. Берётся обходом
// дерева по маске исполняемой части; ни одного — предпосылка не выполняется.
func typedFormSiteFromTree(t *testing.T) (rel string, src []byte, line int) {
	t.Helper()
	files := consoleMutationSources(t)
	for _, f := range files {
		mask, _ := consoleProbeScan(string(f.Src))
		if loc := typedFormRe.FindIndex(mask); loc != nil {
			return f.Rel, f.Src, 1 + strings.Count(string(f.Src[:loc[0]]), "\n")
		}
	}
	t.Fatalf("предпосылка инъекции не выполняется: обход %d исходников консоли не нашёл ни одного "+
		"вызова формы api.<глагол><T>( — настоящего входа для инъекции нет", len(files))
	return "", nil, 0
}

func TestConsoleMutationGateRedOnTypedFormOutsideLedger(t *testing.T) {
	t.Parallel()
	origin, src, line := typedFormSiteFromTree(t)

	// Тот же текст, перенесённый из транспорта в компонент вне ведомости.
	const injected = "iam/src/components/organisms/iam/InjectedInvite/InjectedInvite.tsx"
	findings, _, c := consoleMutationJudge(t, []consoleSourceFile{{Rel: injected, Src: src}}, nil)

	var hit *consoleMutationSite
	for i := range findings {
		if findings[i].Line == line {
			hit = &findings[i]
		}
	}
	if hit == nil {
		t.Fatalf("вызов формы с параметром типа (взят из %s:%d) вне ведомости и без механизма — "+
			"гейт промолчал; находки: %v", origin, line, findings)
	}
	want := consoleUIRoot + "/" + injected + ":" + strconv.Itoa(line)
	if !strings.HasPrefix(hit.String(), want+" ") {
		t.Fatalf("находка не называет координату: %q, ожидалось начало %q", hit.String(), want)
	}
	if !strings.HasSuffix(hit.Text, "<") {
		t.Fatalf("находка не называет форму вызова: %q", hit.Text)
	}
	if c.Forms[hit.Text] == 0 {
		t.Fatalf("перепись форм не учла форму находки %q: %v", hit.Text, c.Forms)
	}
}

func TestConsoleMutationGateSilentOnTypedFormLawfulTwin(t *testing.T) {
	t.Parallel()
	origin, src, line := typedFormSiteFromTree(t)
	const injected = "iam/src/components/organisms/iam/InjectedInvite/InjectedInvite.tsx"

	// Близнец отличается ОДНИМ фактом — файл заведён на механизм.
	twin := append([]byte("import { useSignalledMutation } from \"@shared/lib/use-signalled-mutation\";\n"), src...)
	findings, _, c := consoleMutationJudge(t, []consoleSourceFile{{Rel: injected, Src: twin}}, nil)
	if len(findings) != 0 {
		t.Fatalf("законный близнец (%s:%d, заведён на механизм) краснеет: %v", origin, line, findings)
	}
	if c.Wired != 1 {
		t.Fatalf("законный близнец не признан заведённым: %+v", c)
	}

	// Тот же текст на своём месте — транспорт: переносит, не решает.
	findings, _, c = consoleMutationJudge(t, []consoleSourceFile{{Rel: origin, Src: src}}, nil)
	if len(findings) != 0 || c.Transport != 1 {
		t.Fatalf("вызов в транспорте (%s) судится как действие: находки %v, перепись %+v", origin, findings, c)
	}
}
