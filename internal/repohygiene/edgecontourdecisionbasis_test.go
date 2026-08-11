// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// edgecontourdecisionbasis_test.go — основание, на котором стоит решение
// оставить КРАЙ вне контура носителя.
//
// # Зачем отдельная проба
//
// Запись в реестре гейта усыновления говорит: край не переводится, потому что он
// прокси, а носителю прокси-форма невыразима. Это утверждение О НОСИТЕЛЕ, и оно
// может стать ложным без единой правки края: достаточно, чтобы у дескриптора
// появилось поле обработчика неизвестной службы или мультиплексора — и запись
// останется стоять, объявляя невозможным то, что стало возможным.
//
// Поэтому основание пинится ЗДЕСЬ и истекает от ВНЕШНЕГО факта: появления такого
// поля. Тем же порядком, каким основание решения по владельцу модели держится
// пробой на его пять рубежей.
//
// # Что проба НЕ утверждает
//
// Она не говорит, что край нельзя перевести никогда, и не оправдывает объём его
// собственной сборки. Она отвечает на один вопрос: выразима ли форма края
// дескриптором СЕГОДНЯ. Станет выразима — проба покраснеет и потребует пересмотра
// записи, а не молча переживёт свой предмет.
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/treecorpus"
)

// proxyShapedFields — имена полей дескриптора, появление которых означало бы,
// что прокси-форма стала выразимой объявлением.
//
// Ключ — подстрока имени поля в нижнем регистре; значение — что именно она
// сделала бы возможным. Совпадение по подстроке намеренно: поле, названное
// `UnknownServiceHandler` или `UnknownHandler`, — один и тот же предмет.
var proxyShapedFields = map[string]string{
	"unknownservice": "обработчик неизвестной службы: сервис смог бы принести СВОЮ маршрутизацию " +
		"чужих методов, и «набор служб объявлен» перестало бы быть свойством контура",
	"unknownhandler": "то же под другим именем",
	"cmux":           "мультиплексор: два протокола на одном слушателе — форма края, не сервиса",
	"multiplex":      "то же под общим именем",
}

// TestCarrierStillCannotExpressAProxyEdge — у дескриптора нет полей, которыми
// прокси-форма выражалась бы объявлением.
func TestCarrierStillCannotExpressAProxyEdge(t *testing.T) {
	ex, declared := hostAdoptionExceptions[edgeService]
	if !declared || ex.kind != adoptionDecided {
		t.Skip("край больше не объявлен решением владельца — основание держать не за что; " +
			"предмет этой пробы задаёт запись в servicehostadoption_test.go")
	}
	if strings.TrimSpace(ex.basis) == "" {
		t.Fatal("запись-решение обязана нести основание: без него нечего пинить, и самоистечение " +
			"по внешнему факту становится объявлением без предмета")
	}

	root := repoRoot(t)
	dir := filepath.Join(root, "pkg", "servicecontract")
	tracked, err := treecorpus.Under(dir)
	if err != nil {
		t.Fatalf("состав пакета дескриптора не читается: %v", err)
	}

	fset := token.NewFileSet()
	files, fields := 0, 0
	var found []string
	for _, abs := range tracked {
		if !strings.HasSuffix(abs, ".go") || strings.HasSuffix(abs, "_test.go") {
			continue
		}
		f, perr := parser.ParseFile(fset, abs, nil, parser.SkipObjectResolution)
		if perr != nil {
			t.Fatalf("разбор %s: %v", abs, perr)
		}
		files++
		ast.Inspect(f, func(n ast.Node) bool {
			st, ok := n.(*ast.StructType)
			if !ok || st.Fields == nil {
				return true
			}
			for _, fld := range st.Fields.List {
				for _, name := range fld.Names {
					fields++
					low := strings.ToLower(name.Name)
					for needle, what := range proxyShapedFields {
						if strings.Contains(low, needle) {
							pos := fset.Position(name.Pos())
							rel, _ := filepath.Rel(root, abs)
							found = append(found, fmt.Sprintf("%s:%d %s — %s",
								filepath.ToSlash(rel), pos.Line, name.Name, what))
						}
					}
				}
			}
			return true
		})
	}
	sort.Strings(found)

	t.Logf("перепись: файлов дескриптора прочитано %d, полей структур осмотрено %d, "+
		"словарь прокси-форм %d записей, совпадений %d",
		files, fields, len(proxyShapedFields), len(found))

	if files == 0 || fields == 0 {
		t.Fatal("пакет дескриптора не прочитан — «совпадений нет» здесь означало бы «ничего не " +
			"читал», а не невыразимость прокси-формы")
	}

	for _, f := range found {
		t.Errorf("основание записи о крае истекло: у дескриптора появилось поле, которым "+
			"прокси-форма выражается объявлением — %s.\n"+
			"Значит «носителю край невыразим» больше не факт. Пересмотри запись %q в "+
			"hostAdoptionExceptions: либо переводи край, либо назови НОВОЕ основание.",
			f, edgeService)
	}
}

// TestCarrierRegistersOnlyDeclaredServices — вторая половина того же основания:
// службы попадают в слушатель ТОЛЬКО через переданные регистраторы.
//
// Без неё первая проба закрывала бы лишь одну дверь: поля нет, но если бы носитель
// сам дотягивался до маршрутизации чужих методов, форма края была бы выразима
// мимо дескриптора.
func TestCarrierRegistersOnlyDeclaredServices(t *testing.T) {
	root := repoRoot(t)
	path := filepath.Join(root, "pkg", "servicehost", "serve.go")
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("разбор носителя: %v", err)
	}

	var unknownHandlerUse []string
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if strings.Contains(strings.ToLower(sel.Sel.Name), "unknownservice") {
			pos := fset.Position(sel.Pos())
			unknownHandlerUse = append(unknownHandlerUse, fmt.Sprintf("serve.go:%d %s", pos.Line, sel.Sel.Name))
		}
		return true
	})

	t.Logf("перепись: осмотрен %s, обращений к обработчику неизвестной службы %d",
		filepath.Base(path), len(unknownHandlerUse))

	for _, u := range unknownHandlerUse {
		t.Errorf("носитель обращается к обработчику неизвестной службы (%s) — значит он умеет "+
			"маршрутизировать чужие методы, и основание записи о крае («форма невыразима») "+
			"перестало быть верным", u)
	}
}
