// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestLenientScopeDoubleNamesALiveHolder — пакет, чей дублёр области видимости
// объявляет её НЕОГРАНИЧЕННОЙ, обязан называть пакет, где отбор проверяется
// на самом деле, и названный пакет обязан существовать и нести пробы.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ, И ПОЧЕМУ ПРОВЕРКА ТРЕБУЕТ ИМЕННО ЭТОГО
//
// Снисходительность здесь НАМЕРЕННА, и её цена названа в самих пробах: строк
// выдачи у их фикстуры нет вовсе, поэтому назвать кандидатов она не может, а
// сузив набор до пустого — вернула бы пустую страницу везде и стёрла бы предмет
// своих же проб (вердикт: каким отношением судится строка страницы). Требовать
// «почините дублёров» значило бы сломать работающее.
//
// Опасно другое. Отбор кандидатов держит РОВНО ОДИН пакет, и держит он его на
// настоящей базе. Выпадет он из набора — по недосмотру, из-за недоступных
// контейнеров, при переименовании — и свойство перестанет проверяться НИГДЕ, а
// семь ссылок на него останутся и будут читаться как покрытие. Форма остаётся,
// содержание исчезает молча; это `checks-with-form-but-no-substance` с
// отложенным сроком.
//
// Поэтому проверка требует не сужения у дублёров, а РЕЗОЛВИМОСТИ ссылки: снятие
// держателя роняет прогон с ЕГО ИМЕНЕМ, а не тишиной.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ РАЗБОР СИНТАКСИСА ДЛЯ ОДНОЙ ЧАСТИ И ТЕКСТ ДЛЯ ДРУГОЙ
//
// Снисходительность — свойство ИСПОЛНЯЕМОГО кода (значение поля в литерале
// области), и ищется разбором: поиск подстроки нашёл бы слово в комментарии,
// объясняющем эту же проверку. Ссылка на держателя — наоборот, живёт ИМЕННО в
// комментарии, потому что она адресована читателю; её и ищем в комментариях,
// разобранных парсером, а не в сыром тексте файла.
//
// ─────────────────────────────────────────────────────────────────────────────
// ГРАНИЦА, ОБЪЯВЛЕННАЯ САМОЙ ПРОВЕРКОЙ
//
// Судятся только пакеты, которые снисходительного дублёра ЗАВОДЯТ. Пакет без
// него под проверку не подпадает — там нечему быть снисходительным. «Ноль
// находок» означает «у каждого снисходительного назван живой держатель», а не
// «снисходительных нет»: перепись печатает оба числа.
func TestLenientScopeDoubleNamesALiveHolder(t *testing.T) {
	lenientScopeAudit(t, "../../services/iam/internal/apps/kaname/api")
}

// namesPackage — членство в срезе; ссылающийся пакет называется РОВНО ОДИН раз,
// иначе перечень в сообщении об отказе нечитаем, а нечитаемый перечень
// перестают читать.
func namesPackage(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// lenientScopeAudit — тело проверки, вынесенное ради инъекции: она обязана
// звать ТУ ЖЕ логику, а не свою копию, иначе доказывает свойство копии.
func lenientScopeAudit(t *testing.T, apiRoot string) {
	t.Helper()
	census, findings, err := auditLenientScopes(apiRoot)
	if err != nil {
		t.Fatalf("%v", err)
	}
	t.Log(census)
	for _, f := range findings {
		t.Error(f)
	}
}

// auditLenientScopes ВОЗВРАЩАЕТ находки, а не роняет прогон сам: инъекция
// обязана уметь их прочитать, чтобы утверждать, ЧТО именно нашлось, а не
// только «покраснело». Отказ предпосылки остаётся ошибкой — на слепом обходе
// говорить не о чем.
func auditLenientScopes(apiRoot string) (string, []string, error) {

	// Путь-держатель, названный в комментарии. Ищется как путь внутри дерева
	// сервиса, а не по имени пакета: имя переживает переезд, путь — нет.
	holderRef := regexp.MustCompile(`services/iam/internal/apps/kaname/api/([a-z_]+)`)

	entries, err := os.ReadDir(apiRoot)
	if err != nil {
		return "", nil, fmt.Errorf("обход пакетов use-case iam: %w", err)
	}

	var packages, lenient, named int
	holders := map[string][]string{}
	var findings []string

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		packages++
		dir := filepath.Join(apiRoot, e.Name())
		files, gerr := filepath.Glob(filepath.Join(dir, "*_test.go"))
		if gerr != nil {
			return "", nil, fmt.Errorf("обход проб %s: %w", e.Name(), gerr)
		}

		var isLenient bool
		var refs []string
		for _, f := range files {
			fset := token.NewFileSet()
			node, perr := parser.ParseFile(fset, f, nil, parser.ParseComments)
			if perr != nil {
				return "", nil, fmt.Errorf("разбор %s: %w", f, perr)
			}
			ast.Inspect(node, func(n ast.Node) bool {
				lit, ok := n.(*ast.CompositeLit)
				if !ok {
					return true
				}
				sel, ok := lit.Type.(*ast.SelectorExpr)
				if !ok || sel.Sel == nil || sel.Sel.Name != "Scope" {
					return true
				}
				if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "visibility" {
					return true
				}
				for _, el := range lit.Elts {
					kv, ok := el.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					if key, ok := kv.Key.(*ast.Ident); !ok || key.Name != "Unrestricted" {
						continue
					}
					if id, ok := kv.Value.(*ast.Ident); ok && id.Name == "true" {
						isLenient = true
					}
				}
				return true
			})
			for _, cg := range node.Comments {
				for _, m := range holderRef.FindAllStringSubmatch(cg.Text(), -1) {
					if m[1] != e.Name() {
						refs = append(refs, m[1])
					}
				}
			}
		}

		if !isLenient {
			continue
		}
		lenient++
		if len(refs) == 0 {
			findings = append(findings, e.Name()+": дублёр области снисходительнее продукта, "+
				"но держатель свойства не назван — исчезновение единственного покрытия будет тихим")
			continue
		}
		named++
		for _, r := range refs {
			if !namesPackage(holders[r], e.Name()) {
				holders[r] = append(holders[r], e.Name())
			}
		}
	}

	// Названный держатель обязан существовать и нести пробы: ссылка на пустой
	// каталог — та же тишина, только с адресом.
	var holderTests int
	for h, referrers := range holders {
		hdir := filepath.Join(apiRoot, h)
		files, gerr := filepath.Glob(filepath.Join(hdir, "*_test.go"))
		if gerr != nil || len(files) == 0 {
			findings = append(findings, h+": назван держателем отбора пакетами ["+
				strings.Join(referrers, ", ")+"], но проб не несёт — "+
				"ссылка читается как покрытие, которого нет")
			continue
		}
		n := 0
		for _, f := range files {
			fset := token.NewFileSet()
			node, perr := parser.ParseFile(fset, f, nil, 0)
			if perr != nil {
				return "", nil, fmt.Errorf("разбор %s: %w", f, perr)
			}
			for _, d := range node.Decls {
				fn, ok := d.(*ast.FuncDecl)
				if ok && fn.Recv == nil && strings.HasPrefix(fn.Name.Name, "Test") {
					n++
				}
			}
		}
		holderTests += n
		if n == 0 {
			findings = append(findings, h+": назван держателем отбора пакетами ["+
				strings.Join(referrers, ", ")+"], но ни одной пробы в нём нет")
		}
	}

	census := fmt.Sprintf("осмотрено: пакетов use-case %d; со снисходительным дублёром %d, "+
		"из них называют держателя %d; держателей различных %d, проб у них суммарно %d",
		packages, lenient, named, len(holders), holderTests)

	if packages == 0 {
		return census, nil, fmt.Errorf("пакетов use-case не найдено ни одного — обход слеп, "+
			"и «ноль находок» здесь неотличимо от «ноль прочитанного» (корень %q)", apiRoot)
	}
	if lenient == 0 {
		return census, nil, errors.New("снисходительных дублёров не найдено ни одного — либо " +
			"форма объявления области сменилась, либо разбор слеп; проверка обязана заявить " +
			"об этом, а не выйти зелёной")
	}
	return census, findings, nil
}
