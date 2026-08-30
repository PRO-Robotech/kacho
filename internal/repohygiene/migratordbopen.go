// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// migratordbopen.go — «открыть базу, дождаться её готовности и настроить goose
// под диалект» объявлено ОДИН раз на дерево.
//
// # Предмет: не «две формы», а СЕМЬ объявлений одного шага
//
// Соседний гейт [TestMigratorDbFreeTractIsDeclaredOnce] стережёт ту половину
// тракта, что проверяется без базы: предусловия и разбор `--target`. Он молчит о
// шаге, который к базе обращается, — а тот жил семью объявлениями: три в
// обёртках `services/{vpc,iam,nlb}/internal/apps/migrator/postgres.go`
// (`openPgxDB`/`setupGoose`) и четыре встроенно в
// `services/{compute,geo,registry,storage}/cmd/migrator/main.go`.
//
// # Цена измерена, а не предположена (#1383)
//
// Из трёх текстов отказа на этом пути ДВА расходились по формам, и расходились
// в сторону меньшей информации:
//
//	предмет                     прямая четвёрка        делегирующая тройка
//	sql.Open не удался          "open db: %w"          "open db (driver=%s): %w"
//	goose.SetDialect отверг     "goose dialect: %w"    "goose set dialect %q: %w"
//	база не приняла соединение  одинаково              одинаково
//
// То есть оператор, читающий отказ init-контейнера, узнавал ИЛИ не узнавал имя
// драйвера и имя диалекта в зависимости от того, какой сервис накатывает. Третий
// текст был побайтово одинаков — и именно поэтому опасен: семь копий одного
// правила расходятся молча, ровно как разошлись два соседних (#1461, #1544).
//
// Дефекта поведения здесь не было. Предметом было ТРЕТЬЕ (и седьмое) объявление
// одного шага — тот же класс, что закрыт у предусловий и у приоритета DSN.
//
// # Что требуется
//
//  1. ПОЛОЖИТЕЛЬНАЯ половина: общий пакет [migratorSharedTractHome] объявляет
//     сам шаг ([migratorDBOpenFunc], [migratorDBGooseFunc]) и ВСЕ тексты его
//     отказа. Без неё отрицание ниже стало бы вакуумным в день переименования:
//     искать было бы нечего, гейт молчал бы, и молчание выглядело бы исправной
//     работой (testing.md §«Гейт на класс», п.9).
//  2. ОТРИЦАТЕЛЬНАЯ половина: ни один файл тракта вне общего пакета не открывает
//     базу сам, не ставит свой барьер готовности, не настраивает goose под
//     диалект, не объявляет заново метаданные диалекта и не переобъявляет
//     текстов отказа.
//
// # Чего гейт НЕ утверждает
//
// Что накат сведён. `goose.Up*` по-прежнему зовут семь точек, и их сведение
// ждёт своих проб — предусловие названо в [migratorTractDecisionDoc]. Здесь
// судится шаг ДО первого применяющего вызова: он к базе обращается, но цепочки
// не применяет, поэтому его сведение проверяется контейнером, а не стендом.
//
// # Почему разбор, а не поиск подстроки
//
// `sql.Open`, `dbready.Wait` и тексты отказа стоят в этом же дереве в
// комментариях — в том числе в шапке выше, объясняющей сам запрет. Гейт по
// подстроке краснел бы на собственном объяснении. Поэтому вызовы берутся из
// узлов-выражений, тексты — из узлов-литералов, а объявления — из узлов
// деклараций: комментарий узлом не является by construction.
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"
)

const (
	// migratorDBOpenFunc — имя общего открытия базы с барьером готовности.
	migratorDBOpenFunc = "OpenDB"
	// migratorDBGooseFunc — имя общей настройки goose под диалект.
	migratorDBGooseFunc = "SetupGoose"
	// migratorDBSpecType — имя общего типа метаданных диалекта.
	migratorDBSpecType = "DialectSpec"
	// migratorDBSpecVar — имя общего значения метаданных диалекта.
	migratorDBSpecVar = "SpecPostgres"
)

// migratorDBOpenMarkers — тексты отказа шага «открыть базу и настроить goose».
// Подстроки, а не целые сообщения: хвост у каждого собирается форматом, и полное
// совпадение проверяло бы форматную строку, а не предмет.
var migratorDBOpenMarkers = []string{
	"open db",
	"database connection check failed",
	"goose set dialect",
}

// migratorDBOpenCensus — объём осмотренного. Отдельное утверждение: «ноль
// находок» обязано быть отличимо от «ноль прочитанного».
type migratorDBOpenCensus struct {
	FilesRead       int
	SharedFiles     int
	TractFiles      int
	MarkersDeclared int
	SharedOpen      bool
	SharedGoose     bool
	SharedSpec      bool
	OwnOpen         int
	OwnReadyWait    int
	OwnGooseSetup   int
	OwnSpecDecl     int
	Redeclarations  int
}

func (c migratorDBOpenCensus) String() string {
	return fmt.Sprintf(
		"перепись: прочитано файлов %d (общий пакет %d · тракт %d) · "+
			"общий пакет объявляет шаг %v/%v/%v (открытие/goose/метаданные), "+
			"текстов отказа %d из %d · "+
			"своих открытий базы %d · своих барьеров готовности %d · "+
			"своих настроек goose %d · своих метаданных диалекта %d · "+
			"переобъявлений текста %d",
		c.FilesRead, c.SharedFiles, c.TractFiles,
		c.SharedOpen, c.SharedGoose, c.SharedSpec,
		c.MarkersDeclared, len(migratorDBOpenMarkers),
		c.OwnOpen, c.OwnReadyWait, c.OwnGooseSetup, c.OwnSpecDecl,
		c.Redeclarations)
}

// migratorDBOpenSource — что гейт вычитал из одного файла. Узлами, не словами.
type migratorDBOpenSource struct {
	OpensDB       bool
	WaitsReady    bool
	SetsUpGoose   bool
	DeclaresSpec  bool
	DeclaresOpen  bool
	DeclaresSetup bool
	Markers       []string
}

// readMigratorDBOpenSource разбирает файл и отвечает признаками-узлами.
//
// Вынесено из пробы, чтобы инъекция звала ТО ЖЕ, что и гейт: доказательство,
// проверяющее свою копию разбора, не доказывает ничего о гейте.
func readMigratorDBOpenSource(rel, src string) (migratorDBOpenSource, error) {
	var out migratorDBOpenSource
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, rel, src, 0)
	if err != nil {
		return out, fmt.Errorf("%s: разбор не удался: %w", rel, err)
	}

	for _, d := range file.Decls {
		switch decl := d.(type) {
		case *ast.FuncDecl:
			if decl.Recv != nil {
				continue
			}
			switch decl.Name.Name {
			case migratorDBOpenFunc:
				out.DeclaresOpen = true
			case migratorDBGooseFunc:
				out.DeclaresSetup = true
			}
		case *ast.GenDecl:
			for _, spec := range decl.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					if s.Name.Name == migratorDBSpecType {
						out.DeclaresSpec = true
					}
				case *ast.ValueSpec:
					for _, n := range s.Names {
						if n.Name == migratorDBSpecVar {
							out.DeclaresSpec = true
						}
					}
				}
			}
		}
	}

	ast.Inspect(file, func(n ast.Node) bool {
		if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			for _, marker := range migratorDBOpenMarkers {
				if strings.Contains(lit.Value, marker) {
					out.Markers = append(out.Markers, marker)
				}
			}
			return true
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		switch {
		case pkg.Name == "sql" && sel.Sel.Name == "Open":
			out.OpensDB = true
		case pkg.Name == "dbready" && sel.Sel.Name == "Wait":
			out.WaitsReady = true
		case pkg.Name == "goose" && (sel.Sel.Name == "SetDialect" || sel.Sel.Name == "SetBaseFS"):
			out.SetsUpGoose = true
		}
		return true
	})
	return out, nil
}

// migratorDBOpenFindings формулирует находки одного файла тракта так, чтобы
// каждая называла причину, а не симптом.
func migratorDBOpenFindings(rel string, s migratorDBOpenSource) []migratorTractFinding {
	var out []migratorTractFinding
	add := func(what string) {
		out = append(out, migratorTractFinding{Rel: rel, What: what})
	}
	if s.OpensDB {
		add("открывает базу сам (sql.Open) — зови " +
			migratorSharedTractHome + migratorDBOpenFunc)
	}
	if s.WaitsReady {
		add("ставит свой барьер готовности базы (dbready.Wait) — он внутри " +
			migratorSharedTractHome + migratorDBOpenFunc)
	}
	if s.SetsUpGoose {
		add("настраивает goose под диалект сам (goose.SetBaseFS/SetDialect) — зови " +
			migratorSharedTractHome + migratorDBGooseFunc)
	}
	if s.DeclaresSpec {
		add("заново объявляет метаданные диалекта (" + migratorDBSpecType +
			"/" + migratorDBSpecVar + ") — они в " + migratorSharedTractHome)
	}
	for _, marker := range dedupSortedMarkers(s.Markers) {
		add("заново объявляет текст отказа \"" + marker + "\"")
	}
	return out
}

// dedupSortedMarkers — один и тот же текст, встреченный дважды в файле, остаётся одной
// находкой: перечень адресован читателю, а не счётчику.
func dedupSortedMarkers(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, v := range in {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// migratorDBOpenFindingText — текст находки: читатель должен понять, что делать,
// не открывая этот файл.
func migratorDBOpenFindingText(f migratorTractFinding) string {
	return fmt.Sprintf("%s: %s. Шаг «открыть базу и настроить goose» объявлен "+
		"один раз — в %s (%s)",
		f.Rel, f.What, migratorSharedTractHome, migratorTractDecisionDoc)
}

func sortedDBOpenFindingTexts(findings []migratorTractFinding) []string {
	out := make([]string, 0, len(findings))
	for _, f := range findings {
		out = append(out, migratorDBOpenFindingText(f))
	}
	sort.Strings(out)
	return out
}
