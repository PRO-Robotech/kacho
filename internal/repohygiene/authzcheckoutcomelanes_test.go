// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// authzcheckoutcomelanes_test.go — страж, спрашивающий модель прав, НЕ ВПРАВЕ
// схлопывать «вопрос остался без ответа» в «не положено».
//
// # Предмет
//
// У вопроса о правах три исхода, и два из них — отказы с РАЗНЫМ смыслом. «Модель
// ответила нет» говорит вызывающему, что повтор бессмыслен: решение зависит от
// тройки (субъект, отношение, объект), и одинаковый повтор не меняет ни одного
// из трёх. «Хранилище не ответило» о правах не говорит ничего — тот же вопрос
// мгновением позже получает ответ.
//
// Схлопывание не снимает fail-closed (доступа никто не получает) и потому
// выглядит безобидно. Цена приходит у вызывающего: дренаж очередей
// классифицирует отказ в правах как ТЕРМИНАЛЬНЫЙ и травит строку навсегда;
// повторяющий клиент (`retry.OnUnavailable` повторяет только недоступность)
// перестаёт переживать мигание, которое пережил бы; а СПИСОЧНЫЙ путь отдаёт
// well-formed `200` с молча суженной страницей, неотличимый от настоящего
// отзыва прав.
//
// # Что ищется — СВОЙСТВО, а не написание
//
// Ищется не форма записи, а невыразимость: **ни одно употребление ошибки не
// способно дать исход, отличный от булева ответа**. Ошибка при этом не «забыта»
// — из неё нечего достать by construction.
//
// Обе идиоматичные записи этого дефекта равноправны и ловятся одинаково:
//
//	// A — ошибка живёт только внутри условия
//	if allowed, err := checker.Check(ctx, subj, rel, obj); err == nil && allowed {
//	    return nil
//	}
//	return PermissionDenied()
//
//	// B — раздельное присваивание; имя доступно ниже, но НИ ОДНО его
//	//     употребление ветвью по ошибке не является
//	allowed, err := checker.Check(ctx, subj, rel, obj)
//	return err == nil && allowed
//
// Прежняя редакция этого гейта знала только A. Рецензент вернул схлопывание в
// записи B — гейт сосчитал её ВОПРОСОМ и промолчал, а в дереве в этот момент
// лежали два живых экземпляра формы B, и один из них питал шесть списочных
// путей. Гейт, держащий подформу класса, тем и опасен, что о нём говорят
// «класс закрыт».
//
// Законная форма под гейт НЕ подпадает — у ошибки есть СВОЙ исход:
//
//	allowed, err := checker.Check(ctx, subj, rel, obj)
//	if err != nil { return AuthzBackendUnavailable() }
//	if !allowed  { return PermissionDenied() }
//
// как и `if allowed, err := …; err != nil { … }`, и любая запись, где ошибка
// возвращается, оборачивается или передаётся дальше как ЗНАЧЕНИЕ.
//
// # Почему узнавание идёт по ВЫЗЫВАЕМОМУ, а не по имени переменной
//
// Прежняя редакция требовала, чтобы первый довод звался ровно `ctx`. Рецензент
// снял гейт ПЕРЕИМЕНОВАНИЕМ переменной в `cctx` — написанием, которым
// `pkg/authz/interceptor.go` пользуется уже сегодня: перепись просела с 24 до 23,
// схлопывание уехало из наблюдения, гейт прошёл. Проверка, снимаемая
// переименованием, защищает написание, а не свойство, поэтому вопрос узнаётся по
// вызываемому и арности, а имена доводов не читаются вовсе.
//
// # Почему разбор дерева, а не поиск по образцу
//
// Слово `Check` встречается в этом корпусе в комментариях сотнями, а сама эта
// проба цитирует дефектную форму дословно выше. Поиск по тексту нашёл бы
// собственное объяснение — ровно тот класс, который гейт и ловит.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// checkArity — арность вопроса к модели прав: `Check(<контекст>, subject,
// relation, object)`. Разъедется с портом — перепись найдёт ноль вопросов, и
// гейт скажет об этом отдельной строкой, а не промолчит.
const checkArity = 4

// collapsedCheck — одно найденное схлопывание.
type collapsedCheck struct {
	file string
	line int
	// errName — имя, под которым ошибка связана и в котором она невыразима;
	// пустое, если её выбросили в `_`.
	errName string
	// form — какой записью схлопнуто: важно для правки, а не для вердикта.
	form string
}

// scanReport — что именно осмотрено. Печатается ВСЕГДА: «ноль находок» обязано
// быть отличимо от «ноль прочитанного».
type scanReport struct {
	roots     []string
	files     int
	generated int
	questions int
	found     []collapsedCheck
}

// prodGoRoots — каталоги верхнего уровня, несущие НЕ-тестовый код Go.
//
// ВЫВОДИТСЯ ИЗ ДЕРЕВА, а не выписывается, и это не стиль. Прежняя редакция
// держала перечень литералом `{"services", "pkg", "gateway"}` и проверяла лишь,
// что объявленный каталог existует. Рецензент вычеркнул из литерала `pkg` и
// `gateway` — обход прочитал один каталог, перепись честно напечатала
// «каталогов=1», и гейт ПРОШЁЛ: сужение области оказалось неотличимо от её
// отсутствия. Заодно выяснилось, что литерал и до того не покрывал `internal`,
// `tools` и `terraform`.
//
// Выведенный перечень закрывает оба: вычеркнуть из него нечего, а новый каталог
// с кодом попадает под наблюдение сам, без правки этого файла.
func prodGoRoots(root string) ([]string, error) {
	tracked, err := treecorpus.Under(root)
	if err != nil {
		return nil, fmt.Errorf("состав дерева не читается: %w", err)
	}
	seen := map[string]struct{}{}
	for _, path := range tracked {
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			continue
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return nil, fmt.Errorf("относительный путь для %s: %w", path, rerr)
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) < 2 {
			continue // .go в корне репозитория каталогом не является
		}
		seen[parts[0]] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for d := range seen {
		out = append(out, d)
	}
	sort.Strings(out)
	return out, nil
}

// TestRelationCheckOutcomeLanesAreNotCollapsed — по всему прод-дереву: ни один
// страж не связывает ошибку вопроса о правах так, что её нельзя отличить от
// отказа.
func TestRelationCheckOutcomeLanesAreNotCollapsed(t *testing.T) {
	root := repoRoot(t)

	roots, err := prodGoRoots(root)
	if err != nil {
		t.Fatalf("%v", err)
	}
	rep, err := scanCollapsedRelationChecks(root, roots)
	if err != nil {
		t.Fatalf("%v", err)
	}

	t.Logf("осмотрено: каталогов=%d (%s), файлов Go прочитано=%d (из них сгенерённых пропущено=%d), "+
		"вопросов к модели прав найдено=%d, схлопываний=%d",
		len(rep.roots), strings.Join(rep.roots, ", "), rep.files, rep.generated, rep.questions, len(rep.found))

	// Предпосылка обхода: перечень каталогов выведен из дерева и непуст. Пустой
	// означает, что состав дерева не прочитан, — тогда гейт судит пустоту.
	if len(rep.roots) == 0 {
		t.Fatalf("предпосылка гейта нарушена: в дереве не найдено ни одного каталога с не-тестовым кодом Go — "+
			"состав дерева не прочитан, и зелёное на этом гейте не значит ничего (файлов %d)", rep.files)
	}

	// Предпосылка гейта: вопросы к модели в дереве есть. Ноль означает, что
	// изменилась форма порта (имя метода либо арность), — и тогда гейт судит
	// пустоту, а зелёное на нём ничего не значит.
	if rep.questions == 0 {
		t.Fatalf("предпосылка гейта нарушена: ни одного вопроса вида .Check(…) с %d доводами "+
			"в дереве не найдено — порт переименован либо сменил арность; пока это не выяснено, "+
			"гейт не судит ничего (файлов прочитано %d)", checkArity, rep.files)
	}

	found := rep.found
	sort.Slice(found, func(i, j int) bool {
		if found[i].file != found[j].file {
			return found[i].file < found[j].file
		}
		return found[i].line < found[j].line
	})
	for _, c := range found {
		bound := fmt.Sprintf("именем %q", c.errName)
		if c.errName == "" {
			bound = "выброшена в `_`"
		}
		t.Errorf("%s:%d [%s] — ошибка вопроса о правах %s, и НИ ОДНО её употребление не является "+
			"исходом: другого ответа из неё получить нельзя, поэтому «хранилище прав не ответило» "+
			"неотличимо от «не положено». Разведи исходы: err != nil → недоступность (повтор "+
			"осмыслен), !allowed → отказ (повтор бессмыслен). На списочном пути схлопывание даёт "+
			"молча суженную страницу, неотличимую от отзыва прав",
			c.file, c.line, c.form, bound)
	}
}

// scanCollapsedRelationChecks — обход прод-дерева: где спрашивают модель прав и
// где ответ схлопнут.
//
// Состав берётся у индекса git: обход диска прочитал бы игнорируемое, и вердикт
// стал бы свойством рабочего каталога, а не коммита.
func scanCollapsedRelationChecks(root string, roots []string) (scanReport, error) {
	var rep scanReport
	fset := token.NewFileSet()
	for _, dir := range roots {
		abs := filepath.Join(root, dir)
		if st, serr := os.Stat(abs); serr != nil || !st.IsDir() {
			continue
		}
		rep.roots = append(rep.roots, dir)
		tracked, terr := treecorpus.Under(abs)
		if terr != nil {
			return scanReport{}, fmt.Errorf("состав дерева под %s не читается: %w", dir, terr)
		}
		for _, path := range tracked {
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				continue
			}
			rel, rerr := filepath.Rel(root, path)
			if rerr != nil {
				return scanReport{}, fmt.Errorf("относительный путь для %s: %w", path, rerr)
			}
			f, perr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution|parser.ParseComments)
			if perr != nil {
				return scanReport{}, fmt.Errorf("разбор %s: %w", rel, perr)
			}
			// Сгенерённое НЕ считается: у порождённого кода нет автора, которому
			// адресован упрёк, а его вызовы `Check` — заглушки транспорта, а не
			// вопросы о правах. Прежняя перепись включала два таких файла
			// (`*.pb.gw.go`) и потому завышала число вопросов.
			if isGeneratedFile(f) {
				rep.generated++
				continue
			}
			rep.files++
			q, c := inspectRelationChecks(fset, f, filepath.ToSlash(rel))
			rep.questions += q
			rep.found = append(rep.found, c...)
		}
	}
	return rep, nil
}

// isGeneratedFile — файл несёт канонический заголовок порождения
// (https://go.dev/s/generatedcode).
func isGeneratedFile(f *ast.File) bool {
	for _, g := range f.Comments {
		for _, c := range g.List {
			line := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(c.Text, "//"), "/*"))
			if strings.HasPrefix(line, "Code generated ") && strings.Contains(line, "DO NOT EDIT.") {
				return true
			}
		}
	}
	return false
}

// inspectRelationChecks — вопросы к модели прав в одном файле и те из них, чей
// ответ схлопнут.
//
// Обход идёт по ТЕЛАМ ФУНКЦИЙ: область видимости имени ошибки — функция, и
// судить об употреблениях можно только внутри неё.
func inspectRelationChecks(fset *token.FileSet, f *ast.File, rel string) (questions int, found []collapsedCheck) {
	ast.Inspect(f, func(n ast.Node) bool {
		if isRelationCheckCall(n) {
			questions++
		}
		var body *ast.BlockStmt
		switch fn := n.(type) {
		case *ast.FuncDecl:
			body = fn.Body
		case *ast.FuncLit:
			body = fn.Body
		default:
			return true
		}
		if body == nil {
			return true
		}
		found = append(found, collapsedInBody(fset, body, rel)...)
		return true
	})
	return questions, found
}

// collapsedInBody — схлопывания внутри одного тела функции.
//
// Присваивания из `Init` условных операторов собираются ПЕРВЫМ проходом и во
// втором пропускаются. Без этого одно и то же схлопывание формы A считается
// дважды: обход посещает `Init` и как часть оператора, и как самостоятельное
// присваивание. Поймано инъекцией — она требовала РОВНО одну находку, а
// получила две с одной координатой.
func collapsedInBody(fset *token.FileSet, body *ast.BlockStmt, rel string) (found []collapsedCheck) {
	inits := map[ast.Stmt]struct{}{}
	ast.Inspect(body, func(n ast.Node) bool {
		if ifs, ok := n.(*ast.IfStmt); ok && ifs.Init != nil {
			inits[ifs.Init] = struct{}{}
		}
		return true
	})

	ast.Inspect(body, func(n ast.Node) bool {
		switch st := n.(type) {
		case *ast.IfStmt:
			// Форма A: ошибка связана в Init условного оператора и живёт только
			// внутри него — область поиска употреблений и есть этот оператор.
			assign, ok := relationCheckAssign(st.Init)
			if !ok {
				return true
			}
			if c, bad := verdict(fset, assign, st, rel, "внутри условия"); bad {
				found = append(found, c)
			}
		case *ast.AssignStmt:
			if _, isInit := inits[ast.Stmt(st)]; isInit {
				return true // уже рассмотрено формой A
			}
			// Форма B: обычное присваивание — область поиска есть тело функции.
			assign, ok := relationCheckAssign(st)
			if !ok {
				return true
			}
			if c, bad := verdict(fset, assign, body, rel, "раздельным присваиванием"); bad {
				found = append(found, c)
			}
		}
		return true
	})
	return found
}

// relationCheckAssign — присваивание вида `<что-то>, <что-то> := …Check(…)`.
func relationCheckAssign(s ast.Stmt) (*ast.AssignStmt, bool) {
	assign, ok := s.(*ast.AssignStmt)
	if !ok || len(assign.Rhs) != 1 || len(assign.Lhs) != 2 {
		return nil, false
	}
	if !isRelationCheckCall(assign.Rhs[0]) {
		return nil, false
	}
	return assign, true
}

// verdict — способна ли ошибка этого присваивания дать исход, отличный от
// булева ответа.
func verdict(fset *token.FileSet, assign *ast.AssignStmt, scope ast.Node, rel, form string) (collapsedCheck, bool) {
	line := fset.Position(assign.Pos()).Line
	errIdent, ok := assign.Lhs[1].(*ast.Ident)
	if !ok {
		return collapsedCheck{}, false
	}
	// `_` — ошибку выбросили явно. Исход при обоих ответах хранилища один.
	if errIdent.Name == "_" {
		return collapsedCheck{file: rel, line: line, form: form}, true
	}
	values, branches, conjuncts := classifyErrUses(scope, errIdent.Name, assign.End())
	// Схлопнуто, когда ошибка НИ РАЗУ не выступила ни значением, ни поводом
	// ветвиться. Ноль употреблений вовсе — тот же случай: имя связано и мертво.
	if values > 0 || branches > 0 {
		return collapsedCheck{}, false
	}
	_ = conjuncts
	return collapsedCheck{file: rel, line: line, errName: errIdent.Name, form: form}, true
}

// classifyErrUses — как употреблено имя ошибки после её связывания.
//
//   - values    — ошибка взята КАК ЗНАЧЕНИЕ: возвращена, обёрнута, передана,
//     залогирована. Из такого употребления исход получить можно.
//   - branches  — сравнение с nil, составляющее условие ЦЕЛИКОМ: это ветвь по
//     ошибке, то есть её собственный исход.
//   - conjuncts — сравнение с nil ВНУТРИ `&&`/`||`: ошибка растворена в булевом
//     ответе, отдельного исхода из неё не достать.
//
// Различие между вторым и третьим и есть предмет гейта: `err != nil` в одиночку
// — исход; `err == nil && allowed` — его отсутствие.
func classifyErrUses(scope ast.Node, errName string, after token.Pos) (values, branches, conjuncts int) {
	var stack []ast.Node
	ast.Inspect(scope, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		defer func() { stack = append(stack, n) }()

		id, ok := n.(*ast.Ident)
		if !ok || id.Name != errName || id.Pos() < after {
			return true
		}
		// Ближайший предок — сравнение с nil?
		if len(stack) == 0 {
			values++
			return true
		}
		bin, ok := stack[len(stack)-1].(*ast.BinaryExpr)
		if !ok || (bin.Op != token.EQL && bin.Op != token.NEQ) || !comparesNil(bin) {
			values++
			return true
		}
		// Есть ли НАД сравнением булева связка? Тогда ошибка растворена.
		for i := len(stack) - 2; i >= 0; i-- {
			outer, ok := stack[i].(*ast.BinaryExpr)
			if !ok {
				break // дошли до выражения-не-бинарника: связки над сравнением нет
			}
			if outer.Op == token.LAND || outer.Op == token.LOR {
				conjuncts++
				return true
			}
		}
		branches++
		return true
	})
	return values, branches, conjuncts
}

// comparesNil — одна из сторон сравнения есть литерал nil.
func comparesNil(bin *ast.BinaryExpr) bool {
	for _, side := range []ast.Expr{bin.X, bin.Y} {
		if id, ok := side.(*ast.Ident); ok && id.Name == "nil" {
			return true
		}
	}
	return false
}

// isRelationCheckCall — вызов вида `<что-то>.Check(<контекст>, a, b, c)`.
//
// Разбирается ФОРМА порта, а не его тип: разрешать типы значило бы тянуть в гейт
// загрузку пакетов ради 1477 файлов.
//
// # Три условия, и третье выведено ЗАМЕРОМ, а не вкусом
//
// Имя метода и арность отсекают почти всё. Третье условие — первый довод есть
// ГОЛОЕ ИМЯ — отделяет вопрос о правах от одноимённого метода той же арности:
// проверяльщик типов Go зовётся `cfg.Check(p.ImportPath, fset, syn, info)`, и
// первым доводом у него стоит поле, а не переменная. Контекст на месте вызова
// переменной быть обязан.
//
// Замер по дереву в день заведения условия: вызовов `.Check` с четырьмя
// доводами — 35, из них голое имя первым доводом у 34, и ровно один оставшийся
// вопросом о правах не является. Имена при этом РАЗНЫЕ (`ctx`, `cctx`, `gctx`),
// поэтому условие остаётся name-agnostic: оно требует переменную, а не
// написание.
//
// Граница названа честно: это синтаксический заменитель типа, а не тип. Разъедется
// — будет видно по переписи, которую гейт печатает всегда.
func isRelationCheckCall(n ast.Node) bool {
	call, ok := n.(*ast.CallExpr)
	if !ok || len(call.Args) != checkArity {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Check" {
		return false
	}
	_, firstIsVar := call.Args[0].(*ast.Ident)
	return firstIsVar
}
