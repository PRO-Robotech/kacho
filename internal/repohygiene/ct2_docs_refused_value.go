// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// ct2_docs_refused_value.go — анализатор «страница, называющая значение, которое
// условие ОТВЕРГАЕТ ЯВНО, обязана сказать об этом словами самого отказа».
//
// # Предмет
//
// Близнец запрета «принято-и-проигнорировано» с обратной стороны: возможность
// ОБЪЯВЛЕНА и неисполнима. Контракт несёт значение, страница называет его
// рабочим, а условие отвергает его синхронно и по имени поля. Вызывающий строит
// запрос по странице и платит круг запроса за каждое такое обещание, причём
// отказ не называет страницу, которая ввела в заблуждение.
//
// Замер на день заведения (kacho#1642): значений, отвергаемых явно, у вычислений
// ДВА — `CONTAINER` и `registry.image`. Страниц, называющих их, тоже две, и они
// расходились: быстрый старт отказ проговаривал, страница ресурса — нет. То есть
// два места об одном предмете, из которых верно одно.
//
// # Что судит анализатор
//
// Словарь ВЫВОДИТСЯ из кода сервиса, а не выписывается:
//
//  1. по не-тестовым `.go` собираются строковые литералы — РАЗБОРОМ, а не
//     поиском по тексту, иначе проза о сообщении считалась бы за сообщение;
//  2. из них отбираются сообщения ОТКАЗА («is not accepted / creatable /
//     supported»), и в ЧАСТИ ДО этой связки ищется отвергаемое значение —
//     значение перечисления (`CONTAINER`) либо точечный дискриминатор
//     (`registry.image`);
//  3. страница, называющая такое значение, обязана нести сообщение отказа,
//     которое это значение и называет.
//
// Сверка идёт по тексту с СВЁРНУТЫМИ пробелами: сообщение в странице переносится
// по строкам, и дословное вхождение без свёртки не нашлось бы никогда — гейт
// краснел бы на верной странице, а такую проверку отключают первой.
//
// # ЧЕГО ОН НЕ СУДИТ, и это названо, а не подразумевается
//
//  1. ПЕРЕЧИСЛЕНИЕ ДОПУСТИМЫХ — не отказ, и это несущее различие. Сообщение
//     «`placementStrategy` must be one of SPREAD, PACK» называет значения,
//     которые условие ПРИНИМАЕТ; распознаватель, судящий по одному лишь
//     присутствию в тексте отказа, объявил бы нарушением каждую страницу,
//     называющую `STANDARD`, `GPU`, `ZONAL`. Замер: наивный отбор давал 11
//     значений, отбор по связке — 2, и лишние девять были именно допустимыми.
//     Поэтому значение берётся из части ДО связки отказа, то есть из подлежащего,
//     а не из объяснения.
//
//  2. ПОЛНОТА не судится. Страница, назвавшая один отвергаемый исход из двух,
//     верна в том, что назвала; «названы ли все» — другой предикат.
//
//  3. ОТКАЗ, СФОРМУЛИРОВАННЫЙ ИНОЙ СВЯЗКОЙ, в словарь не попадает: связка —
//     объявленный лексикон («is not accepted / creatable / supported»), а тон
//     сообщений в этом дереве есть часть контракта и меняется осознанно.
//     Привязки к ИМЕНИ построителя отказа здесь намеренно нет: тот же отказ
//     строится и прямым вызовом, и локальным замыканием-накопителем, и
//     распознаватель по имени читал бы 2 сообщения из 15. Перепись печатает
//     число сообщений со связкой, поэтому пустой словарь виден числом и роняет
//     вердикт премисой, а не проходит молча.
//
//  4. ДОМЕНЫ ВНЕ ОХВАТА не судятся: перечень судимых объявлен явно и печатается.
//
// # Падает на ПУСТОМ ОБХОДЕ
//
// Ноль прочитанных исходников, ноль сообщений отказа, ноль отвергаемых значений
// либо ноль страниц — «находок ноль» неотличимо от «прочитано ноль».
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// DocsRefusedValueService — один судимый сервис: где его код и где его страницы.
type DocsRefusedValueService struct {
	Name    string
	CodeDir string
	DocsDir string
}

// DocsRefusedValueOptions — вход анализатора.
type DocsRefusedValueOptions struct {
	Root     string
	Services []DocsRefusedValueService
}

// DocsRefusedValueCensus — объём осмотренного.
type DocsRefusedValueCensus struct {
	GoFiles        int
	StringLiterals int
	RefusalMsgs    int
	RefusedValues  int
	RefusedNames   []string
	DocFiles       int
	Mentions       int
	Judged         int
	ServicesJudged []string
}

// DocsRefusedValueFinding — страница называет отвергаемое значение и молчит о том,
// что оно отвергается.
type DocsRefusedValueFinding struct {
	File    string
	Line    int
	Value   string
	Message string
}

func (f DocsRefusedValueFinding) String() string {
	return fmt.Sprintf("%s:%d: страница называет `%s`, но не говорит, что условие его отвергает — %q",
		f.File, f.Line, f.Value, f.Message)
}

var (
	// docsRefusalPhraseRe — связка отказа. Отделяет ПОДЛЕЖАЩЕЕ отказа (слева) от
	// объяснения (справа); значения берутся только слева.
	docsRefusalPhraseRe = regexp.MustCompile(`is not (?:accepted|creatable|supported)`)
	// docsRefusedEnumRe — значение перечисления: три и более прописных.
	docsRefusedEnumRe = regexp.MustCompile(`\b[A-Z][A-Z0-9_]{2,}\b`)
	// docsRefusedDottedRe — точечный дискриминатор, ОБЕ части строчные:
	// `registry.image` — значение, а `bootSource.type` — путь поля, не значение.
	docsRefusedDottedRe = regexp.MustCompile(`\b[a-z][a-z0-9]*\.[a-z][a-z0-9]*\b`)
	docsWhitespaceRe    = regexp.MustCompile(`\s+`)
)

// docsCollapse сворачивает пробелы: сообщение в странице переносится по строкам.
func docsCollapse(s string) string {
	return docsWhitespaceRe.ReplaceAllString(s, " ")
}

// AuditDocsRefusedValue выносит вердикт о дереве.
func AuditDocsRefusedValue(
	opts DocsRefusedValueOptions, log io.Writer,
) ([]DocsRefusedValueFinding, DocsRefusedValueCensus, error) {
	var census DocsRefusedValueCensus
	var findings []DocsRefusedValueFinding
	allValues := map[string]bool{}

	for _, svc := range opts.Services {
		census.ServicesJudged = append(census.ServicesJudged, svc.Name)

		refused, err := docsRefusedValues(opts.Root, svc, &census)
		if err != nil {
			return nil, census, err
		}
		for v := range refused {
			allValues[v] = true
		}

		pages, err := docsRefusedValuePages(opts.Root, svc)
		if err != nil {
			return nil, census, err
		}
		census.DocFiles += len(pages)

		for _, rel := range pages {
			// #nosec G304 -- путь получен обходом дерева документации ЭТОГО репозитория, не извне
			raw, err := os.ReadFile(filepath.Join(opts.Root, rel))
			if err != nil {
				return nil, census, fmt.Errorf("%s: %w", rel, err)
			}
			text := string(raw)
			collapsed := docsCollapse(text)

			for value, msgs := range refused {
				line, ok := docsFirstWordLine(text, value)
				if !ok {
					continue
				}
				census.Mentions++
				census.Judged++
				said := false
				for _, m := range msgs {
					if strings.Contains(collapsed, docsCollapse(m)) {
						said = true
						break
					}
				}
				if said {
					continue
				}
				findings = append(findings, DocsRefusedValueFinding{
					File: rel, Line: line, Value: value, Message: msgs[0],
				})
			}
		}
	}

	census.RefusedValues = len(allValues)
	for v := range allValues {
		census.RefusedNames = append(census.RefusedNames, v)
	}
	sort.Strings(census.RefusedNames)

	if log != nil {
		_, _ = fmt.Fprintf(log,
			"перепись: судимые сервисы %v · исходников Go %d · строковых литералов %d "+
				"(из них со связкой отказа %d) · отвергаемых значений %d %v · страниц %d · "+
				"упоминаний встречено %d · рассужено %d\n",
			census.ServicesJudged, census.GoFiles, census.StringLiterals, census.RefusalMsgs,
			census.RefusedValues, census.RefusedNames, census.DocFiles,
			census.Mentions, census.Judged)
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Value < findings[j].Value
	})
	return findings, census, nil
}

// docsFirstWordLine — номер первой строки, где значение стоит ЦЕЛЫМ словом.
// Целым: `CONTAINER` не должно находиться внутри `containerSpec`, а `VM` внутри
// `KVM`.
func docsFirstWordLine(text, value string) (int, bool) {
	re := regexp.MustCompile(`\b` + regexp.QuoteMeta(value) + `\b`)
	for i, line := range strings.Split(text, "\n") {
		if re.MatchString(line) {
			return i + 1, true
		}
	}
	return 0, false
}

// docsRefusedValues строит словарь «отвергаемое значение → сообщения, его
// называющие» разбором кода сервиса.
func docsRefusedValues(
	root string, svc DocsRefusedValueService, census *DocsRefusedValueCensus,
) (map[string][]string, error) {
	msgs := map[string]bool{}
	fset := token.NewFileSet()
	codeRoot := filepath.Join(root, filepath.FromSlash(svc.CodeDir))

	err := filepath.WalkDir(codeRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "vendor", "node_modules", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return nil //nolint:nilerr // предмет анализатора — отказы, а не синтаксис чужого файла
		}
		census.GoFiles++
		// Собираются ВСЕ строковые литералы разбором. Дискриминатор здесь —
		// связка отказа, а НЕ имя построителя: тот же отказ строится и прямым
		// вызовом `serviceerr.InvalidArg`, и локальным замыканием-накопителем
		// (`add(field, msg)`). Распознаватель, привязанный к имени построителя,
		// читал бы 2 сообщения отказа из 15 — и молчал бы об остальных
		// тринадцати не потому, что они верны, а потому что не увидел их.
		// Комментарии в словарь не попадают by construction: разбор видит узел
		// литерала, а не текст.
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			v, uerr := strconv.Unquote(lit.Value)
			if uerr != nil {
				return true
			}
			msgs[v] = true
			return true
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	census.StringLiterals += len(msgs)

	refused := map[string][]string{}
	for m := range msgs {
		loc := docsRefusalPhraseRe.FindStringIndex(m)
		if loc == nil {
			continue
		}
		census.RefusalMsgs++
		// Подлежащее отказа — СЛЕВА от связки. Справа стоит объяснение, и
		// значения оттуда отказом не являются.
		head := m[:loc[0]]
		for _, v := range docsRefusedEnumRe.FindAllString(head, -1) {
			refused[v] = append(refused[v], m)
		}
		for _, v := range docsRefusedDottedRe.FindAllString(head, -1) {
			refused[v] = append(refused[v], m)
		}
	}
	for v := range refused {
		sort.Strings(refused[v])
	}
	return refused, nil
}

// docsRefusedValuePages собирает клиентские страницы сервиса.
func docsRefusedValuePages(root string, svc DocsRefusedValueService) ([]string, error) {
	var out []string
	docsRoot := filepath.Join(root, filepath.FromSlash(svc.DocsDir))
	if _, err := os.Stat(docsRoot); err != nil {
		return nil, nil
	}
	err := filepath.WalkDir(docsRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".md") && !strings.HasSuffix(path, ".mdx") {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}
