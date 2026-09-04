// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// trustedforwarderscircle_test.go — гейты на КЛАСС вокруг круга отправителей
// чужой личности.
//
// # Предмет
//
// Круг решает, кто вправе ГОВОРИТЬ ЗА пользователя. Его читают трое: транспорт
// (собирает опцию извлечения личности), стража старта (решает, поднимать ли
// процесс) и самоотчёт о посадке (докладывает, сужен ли круг). Пока круг был
// срезом строк, каждый из троих отвечал на вопрос «сужено ли» СВОИМ предикатом:
// четыре копии одинакового тела в трёх сервисах, причём у одного стража и отчёт
// звали РАЗНЫЕ функции в РАЗНЫХ пакетах. Согласие держалось тем, что три автора
// написали одинаково.
//
// # Два разных свойства, поэтому два гейта
//
// Первый — про ОДИН предикат: сырое поле круга читает ровно одно место (аксессор,
// строящий тип), а все решения принимаются по типу. Второй — про ОТКАЗ СТАРТА: у
// каждого, кто круг объявляет, есть стража, которая на несуженном круге не даёт
// процессу подняться.
//
// Ни один не выводится из другого: можно иметь один предикат и не иметь стражи, и
// наоборот.
//
// # Что читается
//
// Разбор AST не-тестовых исходников `services/`, а не текст: имя поля в
// комментарии или в строковом литерале ни предикатом, ни стражей не является.
// Предпосылка проверяется, объём осмотренного печатается — «ноль находок»
// обязано быть отличимо от «ноль прочитанного».
package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

const (
	// circleFieldSuffix — окончание имени поля, несущего СЫРОЙ круг (список строк
	// из настроек). Именно его нельзя читать нигде, кроме одного места.
	circleFieldSuffix = "TrustedForwarderSANs"
	// circleAccessorName — единственная функция, которой позволено читать сырое
	// поле: она строит из него тип общего фундамента.
	circleAccessorName = "TrustedForwarders"
	// circleGuardCall — метод типа, которым принимается решение о старте.
	circleGuardCall = "Require"
	// circleGuardArg — тип аргумента этого решения. Проверяем именно его, а не
	// одно лишь имя `Require`: методов с таким именем в дереве может завестись
	// сколько угодно, и гейт по имени начал бы засчитывать чужую конструкцию.
	circleGuardArg = "ForwarderGate"
	// circleHostKnobs — тип, которым круг и имена его ручек передаются
	// конструктору дескриптора сервиса. Вторая законная форма стражи: решение о
	// старте принимает конструктор, один на все сервисы, тем же `Require`.
	circleHostKnobs = "ForwarderKnobs"
)

// circleSourceFiles — не-тестовые исходники сервисов, разобранные в AST.
func circleSourceFiles(t *testing.T) (map[string]*ast.File, int) {
	t.Helper()
	root := repoRoot(t)
	files, err := treecorpus.UnderWithSuffix(filepath.Join(root, "services"), ".go")
	if err != nil {
		t.Fatalf("предпосылка гейта нарушена: состав дерева сервисов не читается: %v", err)
	}
	out := map[string]*ast.File{}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			t.Fatalf("parse %s: %v", path, perr)
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			t.Fatalf("relativise %s: %v", path, rerr)
		}
		out[filepath.ToSlash(rel)] = f
	}
	return out, len(out)
}

// circleServiceOf — имя сервиса по относительному пути services/<svc>/...
func circleServiceOf(rel string) string {
	parts := strings.Split(rel, "/")
	if len(parts) >= 2 && parts[0] == "services" {
		return parts[1]
	}
	return ""
}

// TestRawTrustedForwarderCircleIsReadInExactlyOnePlacePerService — сырой круг
// читает только аксессор, строящий тип.
//
// Любое другое чтение — это второй предикат: оно решает про круг само, и его
// ответ может разойтись с тем, что реально уедет в транспорт. Ровно так и было:
// стража считала длину сырого среза, а транспорт отбрасывал пустые записи,
// поэтому список из одних пустых строк проходил стражу и возвращал «доверяем
// любому».
func TestRawTrustedForwarderCircleIsReadInExactlyOnePlacePerService(t *testing.T) {
	files, read := circleSourceFiles(t)
	if read == 0 {
		t.Fatal("предпосылка гейта нарушена: не разобрано ни одного исходника сервисов")
	}

	declarations := 0 // объявления поля в структуре — предмет гейта
	accessorReads := 0
	type offence struct{ file, fn string }
	var offences []offence

	for rel, f := range files {
		// Объявления поля считаем отдельно от чтений: если поля в дереве нет
		// вовсе, гейту нечего охранять и молчать об этом нельзя.
		ast.Inspect(f, func(n ast.Node) bool {
			field, ok := n.(*ast.Field)
			if !ok {
				return true
			}
			for _, name := range field.Names {
				if strings.HasSuffix(name.Name, circleFieldSuffix) {
					declarations++
				}
			}
			return true
		})

		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok || !strings.HasSuffix(sel.Sel.Name, circleFieldSuffix) {
					return true
				}
				if fn.Name.Name == circleAccessorName {
					accessorReads++
					return true
				}
				offences = append(offences, offence{file: rel, fn: fn.Name.Name})
				return true
			})
		}
	}

	t.Logf("осмотрено: исходников сервисов разобрано=%d, объявлений сырого круга=%d, "+
		"чтений в аксессоре=%d, чтений вне аксессора=%d",
		read, declarations, accessorReads, len(offences))

	if declarations == 0 {
		t.Fatalf("предпосылка гейта нарушена: разобрано %d исходников, но поля `*%s` "+
			"в дереве нет вовсе — поле переименовали, и гейт охраняет пустоту",
			read, circleFieldSuffix)
	}
	if accessorReads == 0 {
		t.Fatalf("предпосылка гейта нарушена: разобрано %d исходников, но ни одна функция "+
			"`%s` сырой круг не читает — аксессор переименовали либо круг собирается иначе",
			read, circleAccessorName)
	}

	sort.Slice(offences, func(i, j int) bool { return offences[i].file < offences[j].file })
	for _, o := range offences {
		t.Errorf("%s: функция %s читает сырой круг напрямую — это ВТОРОЙ предикат о том же "+
			"значении.\nРешение о круге принимается по типу общего фундамента "+
			"(grpcsrv.TrustedForwarders.IsNarrowed), потому что транспорт получает именно его: "+
			"собственный подсчёт расходится с тем, что реально сузит круг, и расходится молча.",
			o.file, o.fn)
	}
}

// TestEveryServiceDeclaringTheCircleRefusesToStartUnnarrowed — у каждого, кто
// круг объявляет, есть отказ старта на несуженном круге.
//
// Круг сужает только когда непуст: на пустом транспорт принимает переданную
// личность от ЛЮБОГО пира, прошедшего проверку сертификата. Это действующая,
// намеренная семантика общей библиотеки, и защиту от «забыл заполнить» даёт не
// она, а отказ старта. Сервис, объявивший ручку и не заведший стражу, выглядит
// сузившим круг и не сужает ничего.
func TestEveryServiceDeclaringTheCircleRefusesToStartUnnarrowed(t *testing.T) {
	files, read := circleSourceFiles(t)
	if read == 0 {
		t.Fatal("предпосылка гейта нарушена: не разобрано ни одного исходника сервисов")
	}

	declaring := map[string]string{} // сервис → файл объявления
	guarded := map[string]string{}   // сервис → файл стражи

	for rel, f := range files {
		svc := circleServiceOf(rel)
		if svc == "" {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.Field:
				for _, name := range node.Names {
					if strings.HasSuffix(name.Name, circleFieldSuffix) {
						if _, seen := declaring[svc]; !seen {
							declaring[svc] = rel
						}
					}
				}
			case *ast.CompositeLit:
				// ВТОРАЯ законная форма стражи: сервис ОТДАЁТ круг и имена его
				// ручек дескриптору, а решение о старте принимает конструктор
				// дескриптора — тем же `Require` и с тем же аргументом.
				//
				// Почему это засчитывается, а не читается как пропуск: страж не
				// исчез, он переехал в ОДНО место на все сервисы, и переехал
				// вместе с предметом — сборкой контура. Признаком взят литерал
				// `servicecontract.ForwarderKnobs`: тип существует РОВНО для
				// этой передачи, поэтому его наличие означает, что круг уехал в
				// конструктор. Безусловность самого отказа держится пробами
				// конструктора, а не этим именем, — здесь только адрес.
				typ, ok := node.Type.(*ast.SelectorExpr)
				if !ok || typ.Sel.Name != circleHostKnobs {
					return true
				}
				if _, seen := guarded[svc]; !seen {
					guarded[svc] = rel
				}
			case *ast.CallExpr:
				sel, ok := node.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != circleGuardCall || len(node.Args) != 1 {
					return true
				}
				lit, ok := node.Args[0].(*ast.CompositeLit)
				if !ok {
					return true
				}
				typ, ok := lit.Type.(*ast.SelectorExpr)
				if !ok || typ.Sel.Name != circleGuardArg {
					return true
				}
				if _, seen := guarded[svc]; !seen {
					guarded[svc] = rel
				}
			}
			return true
		})
	}

	var services []string
	for svc := range declaring {
		services = append(services, svc)
	}
	sort.Strings(services)

	t.Logf("осмотрено: исходников сервисов разобрано=%d, сервисов объявляет круг=%d (%s), "+
		"сервисов несёт отказ старта=%d",
		read, len(declaring), strings.Join(services, ", "), len(guarded))

	if len(declaring) == 0 {
		t.Fatalf("предпосылка гейта нарушена: разобрано %d исходников, но ни один сервис "+
			"круга не объявляет — поле переименовали, и гейт охраняет пустоту", read)
	}

	for _, svc := range services {
		if _, ok := guarded[svc]; !ok {
			t.Errorf("%s объявляет круг отправителей (%s), но ни одно место его дерева не "+
				"решает по нему судьбу старта (%s с аргументом %s).\n"+
				"Несуженный круг означает не «никому», а «любому пиру с проверенным "+
				"сертификатом»: сервис выглядит сузившим круг и не сужает ничего.",
				svc, declaring[svc], circleGuardCall, circleGuardArg)
		}
	}

	// Самоистечение: стража у сервиса, который круга больше не объявляет, —
	// находка. Иначе перепись переживёт свой предмет.
	var stale []string
	for svc, file := range guarded {
		if _, ok := declaring[svc]; !ok {
			stale = append(stale, svc+" ("+file+")")
		}
	}
	sort.Strings(stale)
	for _, s := range stale {
		t.Errorf("стража круга без предмета: %s решает судьбу старта по кругу отправителей, "+
			"но самого круга этот сервис не объявляет. Ручку убрали — сними и стражу.", s)
	}
}
