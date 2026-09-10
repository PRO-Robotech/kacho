// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// migratornoticedelivery.go — соединение наката открывается С ОБРАБОТЧИКОМ
// уведомлений, и приёмник смотрит в поток самого процесса.
//
// # Предмет: возможность объявлена и неисполнима
//
// Уведомление сервера (`RAISE NOTICE`/`RAISE WARNING` в миграции, а также то,
// что Postgres говорит сам на `DROP … IF EXISTS`) библиотека отдаёт клиенту
// ТОЛЬКО при заданном обработчике. Незаданный роняет КАЖДОЕ сообщение — молча,
// полностью и всегда. Накат при этом выходит нулём, миграции применяются, и
// «сказали оператору» становится неотличимо от «промолчали» (#2544).
//
// Дефект тихий в обе стороны: снять обработчик обратно можно одной строкой, и ни
// одна проба поведения продукта не покраснеет — цепочка применится ровно так же.
// Поэтому свойство держится гейтом дерева, а не памятью.
//
// # Что требуется — три половины, и первая делает остальные непустыми
//
//  1. ПОЛОЖИТЕЛЬНАЯ: общий пакет [migratorSharedTractHome] объявляет приёмник
//     ([migratorNoticeRelayType]), его конструктор ([migratorNoticeCtor]),
//     ЗАДАЁТ обработчик ([migratorNoticeField]) и ТРЕБУЕТ приёмник параметром
//     [migratorDBOpenFunc]. Последнее — не украшение: параметр делает «забыть»
//     невыразимым, потому что не даёт скомпилировать. Без этой половины
//     отрицания ниже стали бы вакуумными в день переименования: искать было бы
//     нечего, гейт молчал бы, и молчание выглядело бы исправной работой.
//  2. ОТРИЦАТЕЛЬНАЯ «открыл — провяжи»: файл ОБЩЕГО тракта наката
//     ([migratorNoticeHomes]), открывающий соединение, обязан иметь заданный
//     обработчик в своём каталоге. Второй открыватель, заведённый без него,
//     вернул бы дефект целиком.
//  3. ОТРИЦАТЕЛЬНАЯ «доставка не в никуда»: приёмник, собранный в прод-коде,
//     обязан смотреть в поток самого процесса. `io.Discard` компилируется,
//     выглядит настроенным и означает ровно то состояние, ради которого гейт
//     заведён.
//
// # Чего гейт НЕ утверждает
//
// Что тракт наката сведён и что сервисы не открывают базу сами — это предмет
// соседнего [TestMigratorDatabaseOpeningIsDeclaredOnce], и половина 2 намеренно
// сужена его домом, чтобы одна инъекция не роняла оба и доказательство каждого
// оставалось действительным.
//
// # Почему разбор, а не поиск подстроки
//
// `OnNotice`, `NoticeRelay` и `io.Discard` стоят в этом дереве в комментариях —
// в том числе в шапке выше, объясняющей сам запрет. Гейт по подстроке краснел бы
// на собственном объяснении. Поэтому присваивания берутся из узлов-присваиваний,
// вызовы — из узлов-выражений, объявления — из узлов деклараций, а назначение
// приёмника — из узла-аргумента: комментарий узлом не является by construction.
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"sort"
	"strings"
)

const (
	// migratorNoticeField — поле конфигурации соединения, которым библиотека
	// решает, доставлять уведомление или выбросить.
	migratorNoticeField = "OnNotice"
	// migratorNoticeRelayType — тип приёмника уведомлений.
	migratorNoticeRelayType = "NoticeRelay"
	// migratorNoticeCtor — его конструктор.
	migratorNoticeCtor = "NewNoticeRelay"
)

// migratorNoticeHomes — каталоги ОБЩЕГО тракта наката: тот, кто открывает
// соединение, и тот, кто заводит приёмник. Половина «открыл — провяжи» судит
// только их: дом сервисов принадлежит соседнему гейту целиком.
var migratorNoticeHomes = []string{"pkg/migratorcli/", "pkg/migratorrun/"}

// migratorNoticeOpeners — ВСЕ законные формы «открыть соединение к Postgres» в
// этом дереве.
//
// Перечень, а не одна форма, и это несущее требование к распознавателю: форма,
// о которой он не знает, даёт не красное и не зелёное, а МОЛЧАНИЕ — всё, что ею
// написано, уходит из-под наблюдения. Разбор конфигурации (`ParseConfig`)
// включён наравне с самим открытием: именно на ней задаётся обработчик, и файл,
// разбирающий её без него, уже потерял уведомления.
var migratorNoticeOpeners = []string{
	"sql.Open",
	"sql.OpenDB",
	"stdlib.OpenDB",
	"stdlib.OpenDBFromPool",
	"stdlib.RegisterConnConfig",
	"pgx.ParseConfig",
	"pgx.Connect",
	"pgx.ConnectConfig",
	"pgxpool.New",
	"pgxpool.NewWithConfig",
	"pgxpool.ParseConfig",
}

// migratorNoticeSinks — куда приёмнику законно смотреть: только в поток самого
// процесса. Оператор читает вывод init-контейнера, и другого пути у него нет.
var migratorNoticeSinks = []string{"os.Stderr", "os.Stdout"}

// migratorNoticeCensus — объём осмотренного. Отдельное утверждение: «ноль
// находок» обязано быть отличимо от «ноль прочитанного».
type migratorNoticeCensus struct {
	FilesRead      int
	HomeFiles      int
	TractFiles     int
	SharedRelay    bool
	SharedCtor     bool
	SharedHandler  bool
	OpenTakesRelay bool
	OpenersSeen    int
	RelaysBuilt    int
	OpenersUnwired int
	SinksElsewhere int
}

func (c migratorNoticeCensus) String() string {
	return fmt.Sprintf(
		"перепись: прочитано файлов %d (общий тракт %d · точки наката %d) · "+
			"общий пакет объявляет приёмник %v · конструктор %v · задаёт обработчик %v · "+
			"требует приёмник параметром %v · "+
			"форм открытия в словаре %d · встречено открытий %d · собрано приёмников %d · "+
			"открытий без обработчика %d · приёмников мимо потока процесса %d",
		c.FilesRead, c.HomeFiles, c.TractFiles,
		c.SharedRelay, c.SharedCtor, c.SharedHandler, c.OpenTakesRelay,
		len(migratorNoticeOpeners), c.OpenersSeen, c.RelaysBuilt,
		c.OpenersUnwired, c.SinksElsewhere)
}

// migratorNoticeSource — что гейт вычитал из одного файла. Узлами, не словами.
type migratorNoticeSource struct {
	SetsHandler    bool
	DeclaresRelay  bool
	DeclaresCtor   bool
	OpenTakesRelay bool
	Opens          []string
	Sinks          []string
}

// readMigratorNoticeSource разбирает файл и отвечает признаками-узлами.
func readMigratorNoticeSource(rel, src string) (migratorNoticeSource, error) {
	var out migratorNoticeSource
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution)
	if err != nil {
		return out, fmt.Errorf("разбор %s: %w", rel, err)
	}

	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.TypeSpec:
			if node.Name != nil && node.Name.Name == migratorNoticeRelayType {
				out.DeclaresRelay = true
			}
		case *ast.FuncDecl:
			if node.Name == nil {
				return true
			}
			switch node.Name.Name {
			case migratorNoticeCtor:
				out.DeclaresCtor = true
			case migratorDBOpenFunc:
				if funcTakesNoticeRelay(node) {
					out.OpenTakesRelay = true
				}
			}
		case *ast.AssignStmt:
			for _, lhs := range node.Lhs {
				sel, ok := lhs.(*ast.SelectorExpr)
				if ok && sel.Sel != nil && sel.Sel.Name == migratorNoticeField {
					out.SetsHandler = true
				}
			}
		case *ast.KeyValueExpr:
			// Вторая законная форма того же: поле задано в литерале структуры.
			if key, ok := node.Key.(*ast.Ident); ok && key.Name == migratorNoticeField {
				out.SetsHandler = true
			}
		case *ast.CallExpr:
			name := noticeCalleeName(node)
			if name == "" {
				return true
			}
			for _, opener := range migratorNoticeOpeners {
				if name == opener {
					out.Opens = append(out.Opens, name)
				}
			}
			if strings.HasSuffix(name, migratorNoticeCtor) && len(node.Args) > 0 {
				out.Sinks = append(out.Sinks, noticeExprText(fset, node.Args[len(node.Args)-1]))
			}
		}
		return true
	})

	sort.Strings(out.Opens)
	sort.Strings(out.Sinks)
	return out, nil
}

// funcTakesNoticeRelay — принимает ли объявление параметр типа *NoticeRelay.
func funcTakesNoticeRelay(fn *ast.FuncDecl) bool {
	if fn.Type == nil || fn.Type.Params == nil {
		return false
	}
	for _, param := range fn.Type.Params.List {
		star, ok := param.Type.(*ast.StarExpr)
		if !ok {
			continue
		}
		switch t := star.X.(type) {
		case *ast.Ident:
			if t.Name == migratorNoticeRelayType {
				return true
			}
		case *ast.SelectorExpr:
			if t.Sel != nil && t.Sel.Name == migratorNoticeRelayType {
				return true
			}
		}
	}
	return false
}

// noticeCalleeName — «пакет.Функция» для вызова через селектор, иначе имя функции.
func noticeCalleeName(call *ast.CallExpr) string {
	switch fn := call.Fun.(type) {
	case *ast.SelectorExpr:
		if pkg, ok := fn.X.(*ast.Ident); ok && fn.Sel != nil {
			return pkg.Name + "." + fn.Sel.Name
		}
		if fn.Sel != nil {
			return fn.Sel.Name
		}
	case *ast.Ident:
		return fn.Name
	}
	return ""
}

// noticeExprText — исходный текст выражения. Нужен затем, что назначение приёмника
// сверяется ЗНАЧЕНИЕМ (`os.Stderr`), а не именем переменной.
func noticeExprText(fset *token.FileSet, expr ast.Expr) string {
	var b strings.Builder
	if err := printer.Fprint(&b, fset, expr); err != nil {
		return ""
	}
	return b.String()
}

// migratorNoticeSinkIsProcessStream — законно ли назначение приёмника.
func migratorNoticeSinkIsProcessStream(text string) bool {
	for _, ok := range migratorNoticeSinks {
		if text == ok {
			return true
		}
	}
	return false
}

// migratorNoticeFindings формулирует находки одного файла так, чтобы читатель
// понял, что делать, не открывая этот гейт.
func migratorNoticeFindings(rel string, s migratorNoticeSource, wiredInDir bool) []migratorTractFinding {
	var out []migratorTractFinding

	if len(s.Opens) > 0 && !wiredInDir {
		out = append(out, migratorTractFinding{Rel: rel, What: fmt.Sprintf(
			"открывает соединение (%s) и НЕ задаёт %s ни в одном файле своего каталога. "+
				"Без обработчика библиотека выбрасывает каждое уведомление сервера молча: "+
				"миграция печатает, накат выходит нулём, оператор не видит ничего. "+
				"Провяжите приёмник %s.%s",
			strings.Join(dedupSortedMarkers(s.Opens), ", "), migratorNoticeField,
			migratorSharedTractHome, migratorNoticeCtor)})
	}

	for _, sink := range s.Sinks {
		if migratorNoticeSinkIsProcessStream(sink) {
			continue
		}
		out = append(out, migratorTractFinding{Rel: rel, What: fmt.Sprintf(
			"собирает %s с назначением %q — это не поток процесса. Оператор читает вывод "+
				"init-контейнера, и приёмник, смотрящий в другое место, доставляет ровно "+
				"столько же, сколько незаданный обработчик. Допустимо: %s",
			migratorNoticeCtor, sink, strings.Join(migratorNoticeSinks, " либо "))})
	}

	return out
}

// migratorNoticeJudge — единственный судья одного файла: сужает домен половины
// «открыл — провяжи» до общего тракта и формулирует находки.
//
// Вынесен затем, чтобы инъекция звала ТО ЖЕ, что и обход дерева. Своя копия
// сужения доказывала бы что-то о копии, а не о гейте.
func migratorNoticeJudge(rel string, s migratorNoticeSource, wiredInDir bool) []migratorTractFinding {
	if !migratorNoticeIsHome(rel) {
		// Дом сервисов принадлежит соседнему гейту целиком: там открывать базу
		// нельзя ВОВСЕ, и вторая находка о том же файле сделала бы обе инъекции
		// недоказательными.
		s.Opens = nil
	}
	return migratorNoticeFindings(rel, s, wiredInDir)
}

// migratorNoticeFindingText — текст находки.
func migratorNoticeFindingText(f migratorTractFinding) string {
	return fmt.Sprintf("%s: %s (целевая форма — %s)", f.Rel, f.What, migratorTractDecisionDoc)
}

func sortedNoticeFindingTexts(findings []migratorTractFinding) []string {
	out := make([]string, 0, len(findings))
	for _, f := range findings {
		out = append(out, migratorNoticeFindingText(f))
	}
	sort.Strings(out)
	return out
}

// migratorNoticeIsHome — файл принадлежит общему тракту наката.
func migratorNoticeIsHome(rel string) bool {
	for _, home := range migratorNoticeHomes {
		if strings.HasPrefix(rel, home) {
			return true
		}
	}
	return false
}
