// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package listener

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEveryListenerEmissionBuildsTheSamePayload — ВСЁ-ИЛИ-НИЧЕГО В ПРЕДЕЛАХ ВИДА.
//
// # Предмет
//
// Контракт единой формы подписки разрешает подписчику читать непустое состояние
// события как ПОЛНОЕ состояние предмета. Значит один эмиттер вида `nlb_listener`,
// положивший в нагрузку частичный снимок, делает ложным ВЕСЬ вид — и делает это
// тихо: событие приходит, разбирается, переносится в контракт и утверждает, что
// у слушателя нет ни меток, ни проекта.
//
// Свойство принадлежит не одной функции, а ПАКЕТУ: точек эмиссии у вида
// несколько, они лежат в разных файлах, и добавить следующую можно не тронув ни
// одной из существующих. Поэтому судится дерево пакета, а не вызов.
//
// # Почему разбор, а не поиск по образцу
//
// Слова «listenerPayloadMap» и «outboxResourceTypeListener» стоят в самих
// объяснениях — и в этом файле тоже. Проверка по подстроке краснела бы на
// собственном комментарии. Здесь судится УЗЕЛ вызова: аргумент вида и аргумент
// нагрузки берутся позиционно из `Emit`.
//
// # Чего проверка НЕ утверждает — названо, чтобы её не приняли шире
//
// Она не утверждает, что нагрузка полна: полноту утверждает проба строителя
// (`TestListenerPayloadCarriesTheWholeRecordUnderTheStateEnvelope`) и сквозная
// проба журнала. Она утверждает, что строитель у всех точек ОДИН — то есть что
// вторая, частичная форма не заведётся мимо первой.
func TestEveryListenerEmissionBuildsTheSamePayload(t *testing.T) {
	const (
		emitName    = "Emit"
		kindArgIdx  = 1
		payloadArg  = 5
		builderName = "listenerPayloadMap"
	)

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("каталог пакета не читается: %v", err)
	}
	fset := token.NewFileSet()

	var filesRead, emitsSeen, listenerEmits int
	var findings []string

	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, parseErr := parser.ParseFile(fset, filepath.Clean(name), nil, parser.SkipObjectResolution)
		if parseErr != nil {
			t.Fatalf("%s не разобран: %v — гейт судит по узлам, и неосмотренный файл его "+
				"молчания не оправдывает", name, parseErr)
		}
		filesRead++
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel == nil || sel.Sel.Name != emitName {
				return true
			}
			if len(call.Args) <= payloadArg {
				return true
			}
			emitsSeen++
			kind, ok := call.Args[kindArgIdx].(*ast.Ident)
			if !ok || kind.Name != "outboxResourceTypeListener" {
				return true
			}
			listenerEmits++
			builder, ok := call.Args[payloadArg].(*ast.CallExpr)
			if ok {
				if id, isIdent := builder.Fun.(*ast.Ident); isIdent && id.Name == builderName {
					return true
				}
			}
			findings = append(findings, fset.Position(call.Args[payloadArg].Pos()).String())
			return true
		})
	}

	// Объём осмотренного печатается всегда: «ноль находок» обязано быть отличимо
	// от «ноль прочитанного».
	t.Logf("перепись: файлов пакета осмотрено %d · вызовов Emit найдено %d · из них по виду "+
		"слушателя %d · строитель нагрузки %q", filesRead, emitsSeen, listenerEmits, builderName)

	if filesRead == 0 {
		t.Fatal("не осмотрено ни одного файла пакета — проверка беспредметна, а не пройдена")
	}
	if listenerEmits == 0 {
		t.Fatal("в пакете не найдено ни одной точки эмиссии вида слушателя — предмета у " +
			"проверки нет. Если эмиссия переехала, проверка обязана покраснеть, а не молча " +
			"одобрить любой пакет")
	}
	for _, pos := range findings {
		t.Errorf("%s: нагрузка вида слушателя собрана НЕ общим строителем %q.\n"+
			"Контракт формы разрешает читать непустое состояние как ПОЛНОЕ, поэтому одна "+
			"частичная нагрузка делает ложным ВЕСЬ вид — и делает это тихо: событие придёт, "+
			"разберётся и объявит, что у слушателя нет ни меток, ни проекта", pos, builderName)
	}
}
