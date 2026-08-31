// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// modulemockfactory_test.go — подмена модуля не добывает замену динамическим
// импортом внутри своей фабрики.
//
// # Предмет
//
//	jest.unstable_mockModule("antd", async () => (await import("./double")).double);
//
// Фабрику зовёт линкер ESM в момент связывания подменяемого модуля. Динамический
// импорт внутри неё ставит вычисление ЕЩЁ ОДНОГО модуля в тот же незавершённый
// граф, и порядок между ними не закреплён ничем. Когда замена оказывается в
// графе уже вычисляемой, пространство имён возвращается с необъявленными
// связываниями, и обращение к ним падает:
//
//	ReferenceError: Cannot access 'antdDouble' before initialization
//
// Падает суита ЦЕЛИКОМ — до первой пробы, — и падает не всегда: исход зависит от
// расписания прогона.
//
// # Почему это дефект, а не неудобство
//
// Опасно не то, что суита иногда красная, а то, что она **иногда зелёная**:
// вердикт её проб становится функцией расписания. Краснота такого гейта
// неотличима от настоящей находки — и первая же настоящая будет списана на
// «флейк», потому что предыдущие три раза «то же самое проходило».
//
// Наблюдалось: `ui-future/iam`, шесть суит по одному образцу, примерно одно
// падение на четыре прогона (PRO-Robotech/kacho#428).
//
// # Годная форма — и она же преобладающая в дереве
//
//	import { antdStub } from "./antd-stub";
//	jest.unstable_mockModule("antd", () => antdStub());
//
// Замена вычислена статическим импортом ДО вызова: к моменту работы фабрики её
// связывание завершено, и вычислять внутри графа нечего. `unstable_mockModule`
// не поднимается наверх (в отличие от `jest.mock`), поэтому статический импорт
// заведомо успевает.
//
// # Предикат
//
// Находка = вызов `unstable_mockModule(`, в исполняемой части аргументов
// которого встречается `import(`. Разбор идёт по коду, а не по тексту: тела
// строк, шаблонов и регулярных литералов вычищены, комментарии удалены (`tsScan`).
// Иначе гейт нашёл бы образец в абзаце, который сам же его и запрещает, и
// остался бы зелёным при снятой защите (`testing.md` §«Гейт на класс», п. 4).
//
// # Объём осмотренного
//
// Печатается перепись: сколько файлов осмотрено, сколько вызовов подмены
// найдено, сколько из них добывают замену статически. Непустой счётчик годных
// вызовов доказывает, что дискриминатору есть что отличать от находки: «ноль
// находок» обязано быть отличимо от «ноль прочитанного».
//
// # Способность упасть
//
// Доказана инъекцией в обе стороны — `modulemockfactory_injection_test.go`.
package repohygiene

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// Предикат класса.
// ─────────────────────────────────────────────────────────────────────────────

const mockCallMarker = "unstable_mockModule("

type mockFactoryFinding struct {
	File string
	Call string // исполняемая часть вызова, обрезанная до читаемой длины
}

// auditModuleMockFactories — предикат класса. Вход — соответствие «путь → его
// исходник», чтобы инъекция гоняла ТУ ЖЕ функцию, что и обход дерева, а не свою
// копию логики.
//
// Возвращает находки, общее число осмотренных вызовов подмены и число тех из
// них, что добывают замену статически (законные близнецы).
func auditModuleMockFactories(sources map[string]string) (findings []mockFactoryFinding, calls, static int) {
	for rel, src := range sources {
		code, _ := tsScan(src)
		for _, arg := range mockFactoryArgs(code) {
			calls++
			if callsJSName(arg, "import") {
				findings = append(findings, mockFactoryFinding{File: rel, Call: trimCall(arg)})
				continue
			}
			static++
		}
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Call < findings[j].Call
	})
	return findings, calls, static
}

// mockFactoryArgs выдаёт текст аргументов каждого вызова подмены модуля.
//
// Скобки считаются, а не ищутся по первой закрывающей: фабрика — это функция, и
// её тело скобок содержит сколько угодно. Обрыв по первой закрывающей отрезал бы
// фабрику целиком, и гейт молчал бы на ВСЁМ, ни разу об этом не сказав.
func mockFactoryArgs(code string) []string {
	var out []string
	for i := 0; ; {
		j := strings.Index(code[i:], mockCallMarker)
		if j < 0 {
			return out
		}
		start := i + j + len(mockCallMarker)
		depth := 1
		k := start
		for ; k < len(code) && depth > 0; k++ {
			switch code[k] {
			case '(':
				depth++
			case ')':
				depth--
			}
		}
		if depth != 0 {
			// Незакрытая скобка — разбор сломан, и молчание по этому файлу было бы
			// молчанием «не прочитал». Отдаём то, что есть: находка лучше тишины.
			out = append(out, code[start:])
			return out
		}
		out = append(out, code[start:k-1])
		i = k
	}
}

func trimCall(arg string) string {
	s := strings.Join(strings.Fields(arg), " ")
	if len(s) > 160 {
		return s[:160] + "…"
	}
	return s
}

// ─────────────────────────────────────────────────────────────────────────────
// Гейт по дереву.
// ─────────────────────────────────────────────────────────────────────────────

// consoleTypeScriptSources — весь отслеживаемый TypeScript консоли. Именно весь,
// а не только пробы: подмену модуля объявляют и файлы подготовки набора
// (`test/setup.ts`), и они подпадают под то же свойство.
func consoleTypeScriptSources(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, rel := range trackedPaths(t, root) {
		if !strings.HasPrefix(rel, "ui-future/") {
			continue
		}
		if !strings.HasSuffix(rel, ".ts") && !strings.HasSuffix(rel, ".tsx") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("%s: %v — состав исходников консоли неизвестен, значит вердикт был бы утверждением ни о чём", rel, err)
		}
		out[rel] = string(b)
	}
	return out
}

// TestModuleMockFactoryResolvesItsDoubleStatically — фабрика подмены модуля не
// добывает замену динамическим импортом.
func TestModuleMockFactoryResolvesItsDoubleStatically(t *testing.T) {
	root := repoRoot(t)
	sources := consoleTypeScriptSources(t, root)

	if len(sources) == 0 {
		t.Fatal("обход не нашёл ни одного исходника консоли — гейт беспредметен. " +
			"Либо каталог переехал, либо расширение изменилось; в обоих случаях " +
			"зелёный вердикт ниже был бы получен даром.")
	}

	findings, calls, static := auditModuleMockFactories(sources)

	// Предпосылка дискриминатора: он отличает динамическую добычу замены от
	// статической. Если в дереве нет ни одного вызова подмены — отличать нечего,
	// и молчание означало бы поломку разбора, а не чистоту.
	if calls == 0 {
		t.Error("ни один исходник консоли не объявляет подмену модуля — распознавание вызова сломано. " +
			"«Ноль находок» здесь неотличимо от «ноль прочитанного».")
	}
	if static == 0 {
		t.Error("ни одна подмена не добывает замену статически — дискриминатору нечего отличать от находки, " +
			"значит молчание ниже ничего не стоит.")
	}

	for _, f := range findings {
		t.Errorf("подмена модуля в %s добывает замену динамическим импортом:\n  %s\n\n"+
			"Фабрику зовёт линкер в момент связывания подменяемого модуля; импорт внутри неё "+
			"ставит замену в тот же незавершённый граф, и порядок не закреплён ничем. Когда "+
			"замена оказывается уже вычисляемой, обращение к её связыванию падает "+
			"(«Cannot access … before initialization») и роняет суиту целиком — но не всегда.\n"+
			"Опасна не краснота, а зелень через раз: вердикт проб становится функцией расписания.\n"+
			"Исход: внести замену статическим импортом наверху файла и вернуть её из "+
			"синхронной фабрики — `jest.unstable_mockModule(\"antd\", () => antdDouble)`. "+
			"`unstable_mockModule` наверх не поднимается, поэтому импорт заведомо успевает.",
			f.File, f.Call)
	}

	t.Logf("перепись: исходников консоли осмотрено %d, вызовов подмены модуля %d, "+
		"добывают замену статически %d, находок %d", len(sources), calls, static, len(findings))
}
