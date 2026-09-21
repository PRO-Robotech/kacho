// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// gatescopedeclared_test.go — ГЕЙТ, ОБХОДЯЩИЙ ДЕРЕВО ОТ ВЫПИСАННОГО КОРНЯ,
// ОБЯЗАН НАЗВАТЬ СВОЮ ОБЛАСТЬ И СВОЙ ОСТАТОК.
//
// # Почему заведён МЕХАНИЗМ, а не ещё одна починка
//
// Один и тот же род дефекта пришёл тремя кругами ревью подряд: ОБЪЯВЛЕНИЕ ШИРЕ
// ИСПОЛНЯЕМОГО. Гейт обещает шапкой «всякое», «каждое», «ни одного вне», а
// обходит три каталога из двадцати одного — и печатает «вне области 0» о
// знаменателе, поставленном так, чтобы ноль получился. Каждый раз чинился
// конкретный гейт; род воспроизводился, потому что ничто не мешало написать
// следующий такой же.
//
// Мешает — это.
//
// # Что судится — и ЧТО ИМЕННО ЭТОТ РАЗБОР ВИДИТ
//
// У обхода есть КОРЕНЬ, и он бывает двух родов: ВЫВЕДЕННЫЙ (приходит из вызова,
// находящего область — корень модуля, перечень производителя, временный
// каталог синтетики) и ВЫПИСАННЫЙ (литерал либо величина, которой литерал
// присвоен). Первый нельзя сузить, не тронув источник; второй сужается молча, и
// знаменатель переписи становится тем, каким его сделали.
//
// Требование одно: либо вывести корень, либо назвать ОБЕ части — что осмотрено
// и что осталось вне осмотра.
//
// # ГРАНИЦА РАЗБОРА, НАЗВАННАЯ ПОИМЁННО, А НЕ ОБЕЩАНИЕМ ПОЛНОТЫ
//
// Распознавание кода по тексту и по мелкому разбору ПРИНЦИПИАЛЬНО НЕПОЛНО, и
// гнаться за полнотой здесь — заведомо проигранная гонка: каждое расширение
// находит следующую форму. Поэтому обещание сужено до ИЗМЕРЕННОГО, а невидимое
// названо списком — так, чтобы читатель знал цену зелёного, а не верил ему.
//
// ОСЬ КОРНЯ. Видны ДВЕ формы: строковый литерал прямо в вызове обхода и
// величина того же файла, которой присвоен литерал (в том числе через
// `filepath.Join`).
//
// НЕ ВИДНЫ одиннадцать, и вот они: корень ПАРАМЕТРОМ помощнику · возврат
// функции · чтение окружения · соединение строк операцией · поле структуры ·
// перебор списка путей · псевдоним импорта `path/filepath` · файловая система
// как значение (`fs.FS`) · другой пакет работы с путями · форматирование
// (`fmt.Sprintf`) · значение таблицы подпроб.
//
// ОСЬ ОБЕЩАНИЯ. Видна ОДНА форма: слово из закрытого перечня ниже, с
// разделителем после него. НЕ ВИДНЫ пять: «ни один» · «все» · «исчерпывающий» ·
// «полон» · англоязычные формулировки.
//
// ОСЬ ОБЪЯВЛЕНИЯ. Видна ОДНА форма: обе канонические части СЫРЫМ ТЕКСТОМ в
// файле. НЕ ВИДНА вторая: части, собранные из констант, — предикат читает
// текст, а не значения.
//
// # ЭТОТ ГЕЙТ В СОБСТВЕННОЕ МНОЖЕСТВО НЕ ВХОДИТ, и это не освобождение
//
// Прежняя редакция утверждала, что гейт «подчиняется себе». Как МАШИННОЕ
// свойство это ложь, и она опровергнута подстановкой: файл не входит в судимое
// множество вовсе, а со снятыми обоими объявлениями остаётся молчалив — потому
// что его части объявлены константами, которых предикат не читает.
//
// Как есть: область этого гейта ВЫВЕДЕНА (состав индекса, дельта против
// ствола), и потому он не судится — судятся те, у кого корень выписан. Добавь
// ему выписанный обход — войдёт. Это следствие правила, а не изъятие из него,
// и сказано здесь ровно так.
//
// # ЧТО ГЕЙТ ВСЁ ЖЕ ДАЁТ
//
// Он поймал собственный гейт автора раньше автора. Частичный предикат, ЧЕСТНО
// ОБЪЯВЛЕННЫЙ частичным, полезнее полного обещания, которое обходится молча:
// находка сдвигается с «никто не спросил» на «сказано неверно», а второе
// ловится чтением.
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

	"github.com/PRO-Robotech/corelib/gitenv"
)

// Канонические части объявления области. Две, и обе обязательны: одна без
// второй — это «что я смотрю» без «чего я не смотрю», то есть ровно та
// половина, которой и не хватало.
const (
	gateScopeMarker     = "ОБЛАСТЬ ОБХОДА:"
	gateRemainderMarker = "ОСТАТОК:"
)

// treeWalkCallees — вызовы, которыми в этом дереве обходят каталоги.
var treeWalkCallees = map[string]int{
	// имя → номер аргумента, несущего корень
	"ReadDir":  0,
	"Walk":     0,
	"WalkDir":  0,
	"Glob":     0,
	"ReadFile": 0,
}

// walkRoot — один корень обхода и его род.
type walkRoot struct {
	pos     string
	callee  string
	written bool // корень выписан литералом
	how     string
}

// changedTestFiles — проверочные файлы Go, добавленные либо изменённые
// относительно ствола. ОБЛАСТЬ СУДА, выведенная из дельты.
//
// Пустая дельта — ЗАКОННЫЙ исход, а не отказ: на стволе судить нечего, и
// падение здесь означало бы, что проба краснеет на достижении своей цели.
// Перепись при этом говорит «дельта пуста» словами, чтобы ноль находок не
// читался как ноль прочитанного.
func changedTestFiles(t *testing.T, root, base string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	raw, err := gitenv.Command(root, "diff", "--name-only", "--diff-filter=ACMR",
		base+"...HEAD").Output()
	if err != nil {
		t.Fatalf("перечислить изменённые файлы относительно %s: %v", base, err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasSuffix(line, "_test.go") {
			out[filepath.ToSlash(line)] = true
		}
	}
	return out
}

// TestGateScopeIsDeclaredWhereTheWalkIsWrittenOut — «в области: обещают полноту
// M · объявили область K», требуется K = M; остаток модуля измерен числом.
func TestGateScopeIsDeclaredWhereTheWalkIsWrittenOut(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	base := requireTrunkRef(t, root)
	delta := changedTestFiles(t, root, base)
	files := goTestFilesUnder(t, root)
	if len(files) == 0 {
		t.Fatal("обход пуст: проверочных файлов Go не найдено — гейт судил бы о непрочитанном")
	}

	walking, written, promising := 0, 0, 0
	inScopePromising, outsideScopePromising := 0, 0
	var undeclared []string
	for _, path := range files {
		// #nosec G304 -- путь пришёл из ИНДЕКСА репозитория, а не от вызывающего
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, src, parser.ParseComments)
		if err != nil {
			// Файл, который не разбирается, судить нечем; о нём говорит сборка.
			continue
		}
		roots := walkRootsOf(fset, f)
		if len(roots) == 0 {
			continue
		}
		walking++
		hasWritten := false
		for _, r := range roots {
			if r.written {
				hasWritten = true
			}
		}
		if !hasWritten {
			continue
		}
		written++
		// ФИНДИНГ — НЕ УЗКИЙ ОБХОД, А ОБЕЩАНИЕ ШИРЕ ОБХОДА.
		//
		// Узкая область сама по себе законна: бывает, что смотреть надо именно
		// сюда. Дефект — когда шапка обещает полноту («всякое», «каждое», «ни
		// одного вне»), а корень выписан: тогда «вне области 0» читается как
		// утверждение о дереве, а относится к трём каталогам.
		//
		// Здесь предикат по СЛОВАМ законен, и это не отступление от «гейт судит
		// код, а не текст»: предмет ЕСТЬ текст. Судится расхождение между тем,
		// что обещано словами, и тем, что исполняется узлами.
		promise := completenessPromiseIn(f)
		if promise == "" {
			continue
		}
		promising++
		rel, _ := filepath.Rel(root, path)
		if !delta[filepath.ToSlash(rel)] {
			outsideScopePromising++
			continue
		}
		inScopePromising++
		if declaresScope(string(src)) {
			continue
		}
		var how []string
		for _, r := range roots {
			if r.written {
				how = append(how, r.callee+" "+r.how)
			}
		}
		sort.Strings(how)
		undeclared = append(undeclared, rel+" — обещает «"+promise+"», обходит "+
			strings.Join(how, "; "))
	}

	sort.Strings(undeclared)
	for _, u := range undeclared {
		t.Errorf("ОБЪЯВЛЕНИЕ ШИРЕ ИСПОЛНЯЕМОГО: %s.\n"+
			"    Шапка обещает полноту, а корень обхода выписан — сузить его можно молча, и\n"+
			"    знаменатель переписи станет тем, каким его сделали. Либо выведите корень из\n"+
			"    источника области, либо назовите обе части: «%s <что осмотрено>» и\n"+
			"    «%s <что вне осмотра>».",
			u, gateScopeMarker, gateRemainderMarker)
	}
	scope := "проверочные файлы Go, изменённые относительно " + base
	if len(delta) == 0 {
		scope = "дельта относительно " + base + " ПУСТА — на стволе судить нечего"
	}
	// СЛЕПЫЕ ЗОНЫ — ЧИСЛАМИ, а не прозой.
	//
	// «Корень выведен» по этому разбору означает одно из двух: корень
	// действительно выведен ЛИБО выписан в форме, которой разбор не видит
	// (список форм — в шапке). Различить их разбором нечем, и величина названа
	// тем, чем является: зоной, о которой гейт не высказывается.
	rootBlind := walking - written
	promiseBlind := written - promising
	t.Logf("перепись: ОБЛАСТЬ ОБХОДА: %s (%d файлов) · ОСТАТОК: остальной модуль — ИЗМЕРЕН, "+
		"не назван прозой: файлов того же рода вне дельты %d, предмет отдельной задачи.\n"+
		"    Прочитано файлов модуля %d · обходят дерево %d · от выписанного корня %d · "+
		"обещают полноту %d (в дельте %d, вне дельты %d) · в дельте объявили область %d.\n"+
		"    СЛЕПЫЕ ЗОНЫ, числами: корень назван выведенным у %d обходов — среди них и "+
		"настоящие выведенные, и выписанные в 11 формах, которых разбор не видит (перечень в "+
		"шапке); обещание не опознано у %d файлов с выписанным корнем — среди них и молчащие "+
		"о полноте, и обещающие в 5 формах вне перечня. Этот гейт судит ДВЕ формы корня из "+
		"тринадцати и ОДНУ форму обещания из шести.",
		scope, len(delta), outsideScopePromising,
		len(files), walking, written, promising, inScopePromising, outsideScopePromising,
		inScopePromising-len(undeclared), rootBlind, promiseBlind)
}

// completenessPromiseIn — слово шапки, обещающее полноту; "" если такого нет.
//
// Ищется в КОММЕНТАРИЯХ, а не в строках: текст отказа, называющий «каждый»,
// обещанием не является — он объясняет одну находку.
func completenessPromiseIn(f *ast.File) string {
	for _, group := range f.Comments {
		for _, c := range group.List {
			// Слово ищется как СЛОВО, а не как подстрока с хвостовым пробелом:
			// прежде требование пробела делало слово в конце строки невидимым.
			// Это одна форма, а не общее решение оси — остальные пять названы
			// в шапке и остаются вне наблюдения.
			for _, w := range completenessWords {
				if containsWord(strings.ToLower(c.Text), w) {
					return w
				}
			}
		}
	}
	return ""
}

// completenessWords — формы обещания полноты. Перечень закрыт и назван: форма
// вне его останется вне наблюдения, и это сказано вслух здесь, а не выяснится
// на ревью.
var completenessWords = []string{
	"всякое", "всякий", "всякую", "всяком",
	"каждое", "каждый", "каждую", "каждом", "каждого",
	"ни одного", "ни одной",
	"полнота", "полное", "полный", "полностью",
	"по всему дереву", "во всём дереве", "всё дерево",
}

// containsWord — вхождение как ОТДЕЛЬНОГО слова: границей считается всё, что не
// буква и не цифра. Без этого слово в конце строки не опознавалось.
func containsWord(text, word string) bool {
	for i := 0; ; {
		j := strings.Index(text[i:], word)
		if j < 0 {
			return false
		}
		start := i + j
		end := start + len(word)
		if !scopeWordByteAt(text, start-1) && !scopeWordByteAt(text, end) {
			return true
		}
		i = start + 1
		if i >= len(text) {
			return false
		}
	}
}

// scopeWordByteAt — стоит ли по индексу буква либо цифра (вне строки — нет).
// Имя своё: в пакете уже живёт `isWordByte` другого предмета.
func scopeWordByteAt(text string, at int) bool {
	if at < 0 || at >= len(text) {
		return false
	}
	c := text[at]
	return c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c >= 0x80
}

// declaresScope — стоят ли в файле ОБЕ части объявления.
func declaresScope(src string) bool {
	return strings.Contains(src, gateScopeMarker) && strings.Contains(src, gateRemainderMarker)
}

// walkRootsOf — корни обходов файла с их родом.
func walkRootsOf(fset *token.FileSet, f *ast.File) []walkRoot {
	// Имена величин, которым в этом файле присвоен ЛИТЕРАЛ (прямо или через
	// соединение путей из литералов). Такой корень выписан, даже если в вызове
	// стоит имя, а не строка.
	literalIdents := literalPathIdents(f)

	var out []walkRoot
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || (pkg.Name != "os" && pkg.Name != "filepath") {
			return true
		}
		argIdx, ok := treeWalkCallees[sel.Sel.Name]
		if !ok || argIdx >= len(call.Args) {
			return true
		}
		// ReadFile по одному файлу обходом дерева не является: у него нет
		// области, есть адресат. Судятся только каталожные обходы.
		if sel.Sel.Name == "ReadFile" {
			return true
		}
		w, how := rootIsWritten(call.Args[argIdx], literalIdents)
		out = append(out, walkRoot{
			pos:     fset.Position(call.Pos()).String(),
			callee:  pkg.Name + "." + sel.Sel.Name,
			written: w,
			how:     how,
		})
		return true
	})
	return out
}

// rootIsWritten — выписан ли корень, и чем именно.
func rootIsWritten(e ast.Expr, literalIdents map[string]string) (bool, string) {
	switch x := e.(type) {
	case *ast.BasicLit:
		if x.Kind == token.STRING {
			return true, "литерал " + x.Value
		}
	case *ast.Ident:
		if how, ok := literalIdents[x.Name]; ok {
			return true, "величина " + x.Name + " = " + how
		}
	case *ast.CallExpr:
		// filepath.Join(<литералы>) — тот же выписанный корень другой записью.
		if sel, ok := x.Fun.(*ast.SelectorExpr); ok {
			if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "filepath" && sel.Sel.Name == "Join" {
				for _, a := range x.Args {
					if w, how := rootIsWritten(a, literalIdents); w {
						return true, "filepath.Join с " + how
					}
				}
			}
		}
	}
	return false, ""
}

// literalPathIdents — величины файла, которым присвоен строковый литерал либо
// соединение путей из литералов.
func literalPathIdents(f *ast.File) map[string]string {
	out := map[string]string{}
	record := func(lhs []ast.Expr, rhs []ast.Expr) {
		for i, l := range lhs {
			id, ok := l.(*ast.Ident)
			if !ok || i >= len(rhs) {
				continue
			}
			if w, how := rootIsWritten(rhs[i], out); w {
				out[id.Name] = how
			}
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignStmt:
			record(x.Lhs, x.Rhs)
		case *ast.ValueSpec:
			lhs := make([]ast.Expr, 0, len(x.Names))
			for _, nm := range x.Names {
				lhs = append(lhs, nm)
			}
			record(lhs, x.Values)
		}
		return true
	})
	return out
}

// goTestFilesUnder — проверочные файлы Go, ВЗЯТЫЕ У ИНДЕКСА репозитория.
//
// Не обходом диска: под корнем лежат каталоги, которых в репозитории нет —
// рабочие копии агентов, отчёты прогонов, локальные оверлеи. Прочитав их, гейт
// сделал бы свой вердикт свойством ЧУЖОГО рабочего каталога, а не коммита, и
// ошибался бы в обе стороны: краснел на файле, которого в репозитории нет, и
// молчал в свежем checkout там, где обязан говорить.
//
// Это ровно тот же род, что гейт и судит: область, выведенная не из того
// источника, тихо перестаёт быть тем, чем названа.
func goTestFilesUnder(t *testing.T, root string) []string {
	t.Helper()
	tree := newTrackedTree(t, root)
	var out []string
	for rel := range tree.files {
		if strings.HasSuffix(rel, "_test.go") {
			out = append(out, filepath.Join(root, rel))
		}
	}
	sort.Strings(out)
	return out
}
