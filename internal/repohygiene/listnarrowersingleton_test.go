// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// listnarrowersingleton_test.go — гейт против ВТОРОЙ реализации сужения списков.
//
// # Что охраняется
//
// Механика сужения страницы по правам жила в четырёх почти дословных копиях — 3753
// строки в семнадцати отслеживаемых не-тестовых файлах, с побайтово одинаковыми
// бюджетами и различием только в словаре ресурсов. Разошлись копии не в бюджетах, а
// там, где расхождение не видно из диффа: в полярности ответа безымянному
// вызывающему и в том, что означает «модель прав не провязана». Один сервис отвечал
// отказом, другой — пустой страницей, третий — всей страницей.
//
// Единственность держит КОМПИЛЯТОР ровно в той части, где сервис вызывает общий код.
// Чего компилятор не держит — появления РЯДОМ второй такой же механики: она
// собирается и работает, а разъезжается молча.
//
// # Как узнаётся вторая реализация
//
// По СВОЙСТВУ, а не по имени файла или пакета: не-тестовый Go под `services/`,
// который сам конструирует запрос к `AuthorizeService.BatchCheck`
// (`iamv1.BatchAuthorizeCheckRequest`). Это и есть форма вопроса, ради единственности
// которой всё делалось: кто её строит — тот и решает про партии, веер, бюджет,
// окно вердиктов и полярность отказа.
//
// Признак — значение (тип из сгенерированных стабов), а не написание, поэтому
// переименование переменной или файла его не снимает.
//
// # Ноль вхождений — и почему это НЕ делает гейт вакуумным
//
// Под `services/` конструкторов сегодня ноль, и это верное состояние: сторона,
// ОТВЕЧАЮЩАЯ на вопрос (kaname), строит не запрос, а ответ, поэтому под признак не
// подпадает вовсе. Ноль находок на нуле конструкторов сам по себе ничего не
// доказывал бы — он неотличим от сломанного распознавания, — поэтому предпосылка
// проверяется отдельно и на НАСТОЯЩЕМ входе: общий сужатель обязан узнаваться этим же
// признаком. Перестанет — гейт скажет, что стережёт пустое место, вместо того чтобы
// промолчать зелёным.
//
// Список исключений при этом заведён пустым намеренно: он понадобится первому
// законному конструктору, и тогда запись обязана нести ПРИЧИНУ и самоистекать.
package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// batchCheckRequestType — тип запроса, конструирование которого и означает
// «собственная реализация вопроса о видимости страницы».
const batchCheckRequestType = "BatchAuthorizeCheckRequest"

// sharedNarrowerPkgDir — единственный дом механики.
const sharedNarrowerPkgDir = "pkg/listnarrow"

// sharedNarrowerAdapterDir — единственный дом ПЕРЕВОДА механики в контракт
// владельца модели.
//
// Заведён отдельной приставкой, потому что предпосылка гейта переехала: тип
// запроса теперь конструирует адаптер, а не сам сужатель. Порт `listnarrow`
// говорит типами фундамента (приёмка K3-1 §7.2, задача #2131) — иначе фундамент
// импортировал бы контракт службы доступа, и после разъезда на три модуля
// `corelib` потребовал бы `kaname`.
//
// Гейт от этого не ослаб и не сузился: предмет у него прежний — ВТОРАЯ механика
// под `services/`, — а поменялось лишь место, где стоит его положительный
// контроль. Не перенеси мы контроль, гейт стерёг бы пустое место и молчал бы на
// всём (`testing.md` §«Гейт на класс», п.9: проверка, чей предмет сняли,
// замолкает, и отличить это от исправной работы нечем). Ровно это он и сказал.
const sharedNarrowerAdapterDir = "pkg/listnarrow/narrowiam"

// narrowerConstructionExceptions — файлы под `services/`, которым конструировать этот
// запрос ПОЛОЖЕНО, и причина у каждого.
//
// Запись, у которой не осталось предмета (файла нет либо он больше не строит запрос),
// — находка: TestListNarrowerExceptionsHaveSubject требует предмета по каждой.
var narrowerConstructionExceptions = map[string]string{}

// TestListNarrowingHasExactlyOneImplementation — ни один сервис не строит вопрос о
// видимости страницы сам.
//
// Что делать, если гейт сработал, — три исхода, четвёртого нет:
//
//  1. это сужение страницы → звать `pkg/listnarrow` (партии, веер, бюджет, окно
//     вердиктов и полярность отказа приходят вместе с ним, и переписывать их
//     заново — значит завести пятую копию);
//  2. это НЕ сужение страницы, а другая работа с тем же запросом → сузить
//     распознавание ниже, а не заводить запись;
//  3. это законное исключение (сторона, отвечающая на вопрос) → внести запись с
//     ПРИЧИНОЙ в narrowerConstructionExceptions.
func TestListNarrowingHasExactlyOneImplementation(t *testing.T) {
	root := repoRoot(t)

	var findings []string
	scanned, constructors := 0, 0
	usedExceptions := map[string]bool{}

	for _, rel := range narrowerScanFiles(t, root) {
		scanned++
		if !fileConstructsBatchCheckRequest(t, root, rel) {
			continue
		}
		constructors++
		if _, ok := narrowerConstructionExceptions[rel]; ok {
			usedExceptions[rel] = true
			continue
		}
		findings = append(findings, rel)
	}

	// «Ноль находок» обязано быть отличимо от «ноль прочитанного» И от «сломанного
	// распознавания»: обход, не нашедший ни одного конструктора вообще, ничего не
	// доказывает — законный он видеть обязан.
	if scanned == 0 {
		t.Fatalf("гейт не прочитал ни одного прод-файла под services/ — предпосылка обхода сломана")
	}
	// Предпосылка на НАСТОЯЩЕМ входе: признак обязан узнавать ЕДИНСТВЕННЫЙ
	// законный конструктор — адаптер общего сужателя. Без неё «ноль
	// конструкторов под services/» неотличим от сломанного распознавания.
	premise := 0
	for _, name := range []string{"client.go"} {
		if fileConstructsBatchCheckRequest(t, root, filepath.ToSlash(
			filepath.Join(sharedNarrowerAdapterDir, name))) {
			premise++
		}
	}
	if premise == 0 {
		t.Fatalf("признак не узнаёт НИ ОДНОГО конструктора %s в %s — распознавание сломано "+
			"либо перевод переехал; гейт стережёт пустое место, и его молчание ничего "+
			"не доказывает", batchCheckRequestType, sharedNarrowerAdapterDir)
	}

	t.Logf("перепись: прод-файлов под services/ прочитано = %d; конструкторов %s под services/ = %d; "+
		"конструкторов в %s (предпосылка) = %d; записанных исключений = %d (использовано %d)",
		scanned, batchCheckRequestType, constructors, sharedNarrowerAdapterDir, premise,
		len(narrowerConstructionExceptions), len(usedExceptions))

	if len(findings) > 0 {
		sort.Strings(findings)
		t.Errorf("вторая реализация вопроса о видимости страницы (%d):\n  %s\n\n"+
			"Следствие: партии, веер, бюджет операции, окно вердиктов и полярность отказа "+
			"безымянному вызывающему получают ВТОРОЙ ответ, и разъедутся они молча — "+
			"именно так четыре копии разошлись в том, что означает «модель не провязана».\n\n"+
			"Исход: звать %s либо внести запись с причиной.",
			len(findings), strings.Join(findings, "\n  "), sharedNarrowerPkgDir)
	}
}

// TestListNarrowerExceptionsHaveSubject — исключение живёт, пока у него есть предмет.
func TestListNarrowerExceptionsHaveSubject(t *testing.T) {
	root := repoRoot(t)
	for rel, why := range narrowerConstructionExceptions {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Errorf("исключение %q (%s): файла нет — записи больше нечего освобождать, "+
				"и она унаследует следующее слепое пятно", rel, why)
			continue
		}
		if !fileConstructsBatchCheckRequest(t, root, rel) {
			t.Errorf("исключение %q (%s): файл больше не строит %s — запись пережила свой предмет",
				rel, why, batchCheckRequestType)
		}
	}
	t.Logf("перепись исключений: %d", len(narrowerConstructionExceptions))
}

// TestListNarrowerGateDiscriminates — инъекция в обе стороны, на синтетике.
//
// Половина «краснеет»: конструкция запроса узнаётся. Половина «молчит»: ЗАКОННЫЙ
// близнец той же формы — файл, который лишь ПРИНИМАЕТ такой запрос аргументом и
// читает его, — конструктором не считается. Без второй половины гейт краснел бы на
// стороне, отвечающей на вопрос, и его отключили бы первым.
func TestListNarrowerGateDiscriminates(t *testing.T) {
	constructs := `package x

func ask(cli C) {
	_, _ = cli.BatchCheck(ctx, &iamv1.BatchAuthorizeCheckRequest{Checks: checks})
}
`
	reads := `package x

func serve(req *iamv1.BatchAuthorizeCheckRequest) int {
	return len(req.GetChecks())
}
`
	if !srcConstructsBatchCheckRequest(t, constructs) {
		t.Error("конструирование запроса не узнано — гейт не увидит вторую реализацию")
	}
	if srcConstructsBatchCheckRequest(t, reads) {
		t.Error("чтение запроса засчитано конструированием — гейт покраснеет на стороне, " +
			"которая на вопрос ОТВЕЧАЕТ, и будет отключён первым")
	}
}

// fileConstructsBatchCheckRequest — строит ли файл запрос о видимости страницы.
func fileConstructsBatchCheckRequest(t *testing.T, root, rel string) bool {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		return false
	}
	return srcConstructsBatchCheckRequest(t, string(b))
}

func srcConstructsBatchCheckRequest(t *testing.T, src string) bool {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", src, parser.SkipObjectResolution)
	if err != nil {
		return false
	}
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		cl, ok := n.(*ast.CompositeLit)
		if !ok || cl.Type == nil {
			return true
		}
		// Композитный литерал типа — это КОНСТРУИРОВАНИЕ. Параметр функции того же
		// типа, поле структуры, разыменование — нет.
		switch tp := cl.Type.(type) {
		case *ast.SelectorExpr:
			if tp.Sel.Name == batchCheckRequestType {
				found = true
			}
		case *ast.Ident:
			if tp.Name == batchCheckRequestType {
				found = true
			}
		}
		return !found
	})
	return found
}

// narrowerScanFiles — отслеживаемые не-тестовые `.go` под `services/`.
//
// Единица счёта — ОТСЛЕЖИВАЕМЫЙ git-элемент, а не то, что лежит на диске: иначе
// рабочая копия соседа, забытая сборка или каталог под игнором меняли бы объём
// осмотренного, и число в переписи перестало бы что-либо значить.
func narrowerScanFiles(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	for _, line := range gitLsFiles(t, root) {
		i := strings.IndexByte(line, '\t')
		if i < 0 {
			continue
		}
		rel := line[i+1:]
		if !strings.HasPrefix(rel, "services/") || !strings.HasSuffix(rel, ".go") {
			continue
		}
		if strings.HasSuffix(rel, "_test.go") {
			continue
		}
		out = append(out, rel)
	}
	sort.Strings(out)
	return out
}
