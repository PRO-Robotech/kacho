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
// классифицирует отказ в правах как ТЕРМИНАЛЬНЫЙ и травит строку навсегда, а
// повторяющий клиент (`retry.OnUnavailable` повторяет только недоступность)
// перестаёт переживать мигание, которое пережил бы.
//
// # Что именно ищется — форма, в которой ошибка НЕВЫРАЗИМА
//
//	if allowed, err := checker.Check(ctx, subj, rel, obj); err == nil && allowed {
//	    return nil
//	}
//	return PermissionDenied()
//
// Здесь `err` объявлен в `Init` условного оператора, поэтому живёт ТОЛЬКО внутри
// него, а единственное его употребление — конъюнкт булева условия. Никакого
// другого исхода из этой ошибки получить нельзя **by construction**: она не
// «забыта», она невыразима. Поэтому предикат не эвристика — он описывает форму,
// в которой различить два отказа невозможно.
//
// Законная форма той же проверки под гейт НЕ подпадает: ошибка связана обычным
// присваиванием и потому доступна ниже по функции —
//
//	allowed, err := checker.Check(ctx, subj, rel, obj)
//	if err != nil { return AuthzBackendUnavailable() }
//	if !allowed  { return PermissionDenied() }
//
// как и `if allowed, err := …; err != nil { … }` — там условие называет ошибку
// одну, без конъюнкции, то есть ветвь по ней и есть исход.
//
// # Почему разбор дерева, а не поиск по образцу
//
// Слово `Check` встречается в этом корпусе в комментариях сотнями, а сама эта
// проба цитирует дефектную форму дословно двумя абзацами выше. Поиск по тексту
// нашёл бы собственное объяснение — ровно тот класс, который гейт и ловит.

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

	"github.com/PRO-Robotech/kacho/internal/treecorpus"
)

// checkArity — арность вопроса к модели прав: `Check(ctx, subject, relation,
// object)`. Разъедется с портом — перепись найдёт ноль вопросов, и гейт скажет
// об этом отдельной строкой, а не промолчит.
const checkArity = 4

// collapsedCheck — одно найденное схлопывание.
type collapsedCheck struct {
	file string
	line int
	// errName — имя, под которым ошибка связана и в котором она невыразима.
	errName string
}

// checkedRoots — каталоги прод-кода, которые гейт обязан осмотреть в НАСТОЯЩЕМ
// дереве. Перечень назван, чтобы исчезновение каталога было отказом, а не тихим
// сужением области: обход, потерявший `pkg`, отчитался бы зелёным о том, чего не
// читал.
var checkedRoots = []string{"services", "pkg", "gateway"}

// TestRelationCheckOutcomeLanesAreNotCollapsed — по всему прод-дереву: ни один
// страж не связывает ошибку вопроса о правах так, что её нельзя отличить от
// отказа.
func TestRelationCheckOutcomeLanesAreNotCollapsed(t *testing.T) {
	root := repoRoot(t)

	found, questions, files, read, err := scanCollapsedRelationChecks(root)
	if err != nil {
		t.Fatalf("%v", err)
	}

	// Перепись печатается ВСЕГДА: «ноль находок» обязано быть отличимо от «ноль
	// прочитанного».
	t.Logf("осмотрено: каталогов=%d (%s), файлов Go прочитано=%d, вопросов к модели прав найдено=%d, схлопываний=%d",
		len(read), strings.Join(read, ", "), files, questions, len(found))

	if len(read) != len(checkedRoots) {
		t.Fatalf("осмотрены не все каталоги прод-кода: прочитано %v, объявлено %v — "+
			"обход, потерявший каталог, отчитался бы зелёным о том, чего не читал",
			read, checkedRoots)
	}

	// Предпосылка гейта: вопросы к модели в дереве есть. Ноль означает, что
	// изменилась форма порта (имя метода либо арность), — и тогда гейт судит
	// пустоту, а зелёное на нём ничего не значит.
	if questions == 0 {
		t.Fatalf("предпосылка гейта нарушена: ни одного вопроса вида Check(ctx, …) с %d доводами "+
			"в дереве не найдено — порт переименован либо сменил арность; пока это не выяснено, "+
			"гейт не судит ничего (файлов прочитано %d)", checkArity, files)
	}

	sort.Slice(found, func(i, j int) bool {
		if found[i].file != found[j].file {
			return found[i].file < found[j].file
		}
		return found[i].line < found[j].line
	})
	for _, c := range found {
		t.Errorf("%s:%d — ошибка вопроса о правах связана именем %q внутри условия и там же "+
			"поглощена конъюнкцией: другого исхода из неё получить НЕЛЬЗЯ, поэтому «хранилище "+
			"прав не ответило» неотличимо от «не положено». Свяжи ответ обычным присваиванием и "+
			"разведи исходы: err != nil → недоступность (повтор осмыслен), !allowed → отказ "+
			"(повтор бессмыслен)",
			c.file, c.line, c.errName)
	}
}

// scanCollapsedRelationChecks — обход прод-дерева: где спрашивают модель прав и
// где ответ схлопнут.
//
// Состав берётся у индекса git: обход диска прочитал бы игнорируемое, и вердикт
// стал бы свойством рабочего каталога, а не коммита.
func scanCollapsedRelationChecks(root string) (
	found []collapsedCheck, questions, files int, read []string, err error,
) {
	fset := token.NewFileSet()
	for _, dir := range checkedRoots {
		abs := filepath.Join(root, dir)
		// Отсутствующий каталог ПРОПУСКАЕТСЯ, а не роняет обход: инъекция строит
		// синтетическое дерево из одного каталога, и требование всех трёх сделало
		// бы функцию непроверяемой. Что каталог не прочитан — видно вызывающему:
		// он получает перечень прочитанного и сам решает, полон ли тот.
		if st, serr := os.Stat(abs); serr != nil || !st.IsDir() {
			continue
		}
		read = append(read, dir)
		tracked, terr := treecorpus.Under(abs)
		if terr != nil {
			return nil, 0, 0, nil, fmt.Errorf("состав дерева под %s не читается: %w", dir, terr)
		}
		for _, path := range tracked {
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				continue
			}
			rel, rerr := filepath.Rel(root, path)
			if rerr != nil {
				return nil, 0, 0, nil, fmt.Errorf("относительный путь для %s: %w", path, rerr)
			}
			files++
			f, perr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if perr != nil {
				return nil, 0, 0, nil, fmt.Errorf("разбор %s: %w", rel, perr)
			}
			q, c := inspectRelationChecks(fset, f, filepath.ToSlash(rel))
			questions += q
			found = append(found, c...)
		}
	}
	return found, questions, files, read, nil
}

// inspectRelationChecks — вопросы к модели прав в одном файле и те из них, чей
// ответ схлопнут.
func inspectRelationChecks(fset *token.FileSet, f *ast.File, rel string) (questions int, found []collapsedCheck) {
	ast.Inspect(f, func(n ast.Node) bool {
		if isRelationCheckCall(n) {
			questions++
		}
		ifs, ok := n.(*ast.IfStmt)
		if !ok || ifs.Init == nil {
			return true
		}
		assign, ok := ifs.Init.(*ast.AssignStmt)
		if !ok || len(assign.Rhs) != 1 || len(assign.Lhs) != 2 {
			return true
		}
		if !isRelationCheckCall(assign.Rhs[0]) {
			return true
		}
		// Второе связанное имя — ошибка. `_` означает, что её выбросили явно; это
		// тоже схлопывание, и оно называется тем же именем.
		errIdent, ok := assign.Lhs[1].(*ast.Ident)
		if !ok {
			return true
		}
		if !swallowedInCondition(ifs.Cond, errIdent.Name) {
			return true
		}
		found = append(found, collapsedCheck{
			file:    rel,
			line:    fset.Position(ifs.Pos()).Line,
			errName: errIdent.Name,
		})
		return true
	})
	return questions, found
}

// isRelationCheckCall — вызов вида `<что-то>.Check(ctx, a, b, c)`.
//
// Разбирается ФОРМА порта, а не его тип: разрешать типы значило бы тянуть сюда
// загрузку пакетов, а форма (имя метода, арность, контекст первым доводом)
// принадлежит именно вопросу о правах и ничему другому в этом дереве.
func isRelationCheckCall(n ast.Node) bool {
	call, ok := n.(*ast.CallExpr)
	if !ok || len(call.Args) != checkArity {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Check" {
		return false
	}
	first, ok := call.Args[0].(*ast.Ident)
	return ok && first.Name == "ctx"
}

// swallowedInCondition — употреблено ли имя ошибки ТОЛЬКО как конъюнкт булева
// условия.
//
// Именно конъюнкция делает ошибку невыразимой: `err != nil` в одиночку — это
// ветвь ПО ошибке, то есть её исход, а `err == nil && allowed` превращает её в
// часть одного-единственного булева ответа, из которого второй код не достать.
func swallowedInCondition(cond ast.Expr, errName string) bool {
	mentions := false
	ast.Inspect(cond, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name == errName {
			mentions = true
		}
		return true
	})
	if !mentions {
		// Ошибку в условии не назвали вовсе — она молча выброшена. Это тоже
		// схлопывание: исход один при обоих ответах хранилища.
		return true
	}
	bin, ok := cond.(*ast.BinaryExpr)
	return ok && (bin.Op == token.LAND || bin.Op == token.LOR)
}
