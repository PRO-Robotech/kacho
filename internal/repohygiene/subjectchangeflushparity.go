// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// subjectchangeflushparity.go — анализатор «две полосы одного механизма сверяются
// МЕЖДУ СОБОЙ».
//
// # Предмет
//
// Смена прав доезжает до кэша решений края ДВУМЯ полосами, и они независимы:
//
//   - НЕМЕДЛЕННО, на реплике, обслужившей мутацию, — самосбросом по полному имени
//     метода (`subjectChangingFQNs` у края);
//   - С ЗАДЕРЖКОЙ, на соседних репликах, — через очередь смены субъекта
//     (`subject_change_outbox`), которую край читает сам.
//
// Полосы кормит ОДНО событие, но перечни у них РАЗНЫЕ и ведутся врозь. Заведя
// шестого производителя очереди, легко не вспомнить о самосбросе: соседние
// реплики сойдутся, а та, что мутацию обслужила, продолжит отвечать по
// закешированному вердикту — то есть по ОТОЗВАННОМУ праву, и дольше всех именно
// там, где пользователь только что нажал «отозвать».
//
// Заметить это по одной полосе нельзя: каждая исправна сама по себе. Поэтому
// гейт спрашивает не «верен ли перечень» (это и есть спорный вопрос), а
// «РЕШАЛ ЛИ КТО-НИБУДЬ, что они различаются».
//
// # Перепись печатает ОБЕ величины
//
// «Производителей N · самосброс покрывает M». Одно число скрывает ровно тот
// случай, ради которого гейт заведён.
//
// # Что судится, а что нет
//
// Судится ЧИСЛО и РАЗРЕШИМОСТЬ имени: каждое имя набора самосброса обязано
// называть существующий метод существующей службы контракта — опечатка иначе
// молча не совпала бы ни с одним запросом, оставаясь на вид записью.
//
// НЕ судится соответствие «этот производитель ↔ это имя»: производитель —
// ОБРАЩЕНИЕ к методу порта в use-case, имя метода ему не принадлежит и
// механически из него не выводится.
// Заведи здесь ведомость соответствий — и это стало бы третьим местом об одном
// предмете, расходящимся молча. Число и разрешимость ловят настоящий класс
// (полоса пополнилась, вторая нет), не выдумывая связи, которой в дереве нет.
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
)

// emitSubjectChangeSelector — имя метода порта, которым производится строка
// очереди смены субъекта.
//
// # Обращений к нему в дереве ДВЕ формы, и обе законны
//
//	w.AccessBindingsW().EmitSubjectChangeEvent(ctx, …)  // вызов на месте
//	fanout(ctx, …, w.AccessBindingsW().EmitSubjectChangeEvent, …)  // передан значением
//
// Вторая появляется, когда строка пишется НА КАЖДОГО субъекта привязки общим
// развёртывателем: сам вызов уезжает в него, а производителем остаётся
// use-case, который метод отдал.
//
// Распознаватель, знавший только первую, объявлял вторую отсутствующей — не
// нарушением, а НЕВИДИМОСТЬЮ: два живых производителя пропали из переписи, и
// гейт покраснел на дереве, где обе полосы сходятся. Поэтому судится узел
// ОБРАЩЕНИЯ (`*ast.SelectorExpr`), а не узел вызова: вызов такой узел содержит,
// поэтому обе формы считаются по разу и ни одна дважды.
const emitSubjectChangeSelector = "EmitSubjectChangeEvent"

// selfFlushSetName — имя набора самосброса у края.
const selfFlushSetName = "subjectChangingFQNs"

// SubjectChangeFlushParityOptions — посадка анализатора.
type SubjectChangeFlushParityOptions struct {
	Root string
	// ProducerRoot — каталог use-case владельца прав.
	ProducerRoot string
	// SelfFlushFile — файл края, несущий набор самосброса.
	SelfFlushFile string
	// MethodExists — разрешима ли пара «служба/метод» в контракте. Ноль означает
	// «не спрашивать»: инъекция работает на синтетическом дереве, где контракта
	// нет вовсе.
	MethodExists func(service, method string) bool
}

// SubjectChangeFlushParityCensus — ОБЕ величины полос плюс объём осмотренного.
type SubjectChangeFlushParityCensus struct {
	GoFiles      int
	Producers    []string
	SelfFlushSet []string
}

// SubjectChangeFlushParityFinding — одно расхождение.
type SubjectChangeFlushParityFinding struct{ What string }

// AuditSubjectChangeFlushParity сверяет полосы между собой.
func AuditSubjectChangeFlushParity(
	opts SubjectChangeFlushParityOptions, log io.Writer,
) ([]SubjectChangeFlushParityFinding, SubjectChangeFlushParityCensus, error) {
	var (
		findings []SubjectChangeFlushParityFinding
		census   SubjectChangeFlushParityCensus
	)

	producerDir := filepath.Join(opts.Root, opts.ProducerRoot)
	err := filepath.WalkDir(producerDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		census.GoFiles++
		fset := token.NewFileSet()
		file, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return fmt.Errorf("разбор %s: %w", path, perr)
		}
		rel, _ := filepath.Rel(opts.Root, path)
		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != emitSubjectChangeSelector {
				return true
			}
			census.Producers = append(census.Producers,
				fmt.Sprintf("%s:%d", rel, fset.Position(sel.Sel.Pos()).Line))
			return true
		})
		return nil
	})
	if err != nil {
		return nil, census, err
	}

	flushPath := filepath.Join(opts.Root, opts.SelfFlushFile)
	fset := token.NewFileSet()
	file, perr := parser.ParseFile(fset, flushPath, nil, 0)
	if perr != nil {
		return nil, census, fmt.Errorf("разбор %s: %w", opts.SelfFlushFile, perr)
	}
	found := false
	ast.Inspect(file, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok || len(vs.Names) != 1 || vs.Names[0].Name != selfFlushSetName {
			return true
		}
		found = true
		for _, v := range vs.Values {
			lit, ok := v.(*ast.CompositeLit)
			if !ok {
				continue
			}
			for _, el := range lit.Elts {
				kv, ok := el.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				key, ok := kv.Key.(*ast.BasicLit)
				if !ok || key.Kind != token.STRING {
					continue
				}
				s, uerr := strconv.Unquote(key.Value)
				if uerr != nil {
					continue
				}
				census.SelfFlushSet = append(census.SelfFlushSet, s)
			}
		}
		return false
	})
	if !found {
		return nil, census, fmt.Errorf(
			"набор самосброса %s не найден в %s — предпосылка гейта неверна, "+
				"вердикт был бы беспредметен", selfFlushSetName, opts.SelfFlushFile)
	}

	if opts.MethodExists != nil {
		for _, fqn := range census.SelfFlushSet {
			svc, method, ok := strings.Cut(fqn, "/")
			if !ok || svc == "" || method == "" {
				findings = append(findings, SubjectChangeFlushParityFinding{
					What: fmt.Sprintf("имя %q не имеет формы «служба/метод»", fqn)})
				continue
			}
			if !opts.MethodExists(svc, method) {
				findings = append(findings, SubjectChangeFlushParityFinding{
					What: fmt.Sprintf("имя %q не разрешается в контракте: самосброс по нему "+
						"не сработает ни разу, оставаясь на вид записью", fqn)})
			}
		}
	}

	if len(census.Producers) != len(census.SelfFlushSet) {
		findings = append(findings, SubjectChangeFlushParityFinding{
			What: fmt.Sprintf(
				"полосы разошлись: производителей очереди %d, самосброс покрывает %d.\n"+
					"  производители: %s\n  самосброс:     %s\n"+
					"  Реплика, обслужившая мутацию, отвечает по закешированному вердикту до "+
					"следующего чтения очереди — то есть по отозванному праву, и дольше всего "+
					"там, где пользователь только что нажал «отозвать».",
				len(census.Producers), len(census.SelfFlushSet),
				strings.Join(census.Producers, ", "), strings.Join(census.SelfFlushSet, ", "))})
	}

	if log != nil {
		_, _ = fmt.Fprintf(log, "перепись: файлов Go прочитано %d · производителей очереди %d · самосброс покрывает %d · находок %d\n",
			census.GoFiles, len(census.Producers), len(census.SelfFlushSet), len(findings))
	}
	return findings, census, nil
}
