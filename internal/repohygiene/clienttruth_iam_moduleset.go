// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// clienttruth_iam_moduleset.go — анализатор «перечень модулей платформы, названный
// клиентской поверхностью, полон».
//
// # Предмет
//
// Набор модулей закрыт, и его ЕДИНСТВЕННЫЙ источник — литерал `knownModules` в
// `services/iam/internal/domain/module_set.go`. Модуль — то, чем клиент выражает
// грант: `Rule.module`. Перечень, назвавший меньше, чем принимает сервер,
// сообщает клиенту, что тонко выдать доступ к недостающему домену НЕЛЬЗЯ, — а
// единственный документированный выход при таком чтении есть системная роль на
// весь уровень, то есть заведомое расширение доступа. Цена неполноты здесь не в
// удобстве, а в безопасности.
//
// Замер на день заведения (kacho#1627): сервер принимал ШЕСТЬ модулей, а называли
// ЧЕТЫРЕ и справочная страница ролей, и комментарий поля контракта. Рядом,
// третьим местом, стояла шапка самой функции `IsKnownModule` — она называла ПЯТЬ.
// То есть разошлись не документ с кодом, а три места об одном предмете, из
// которых верно было ни одно.
//
// # Почему это не удержала проба набора
//
// Проба `TestIsKnownModule_ClosedSet` несла собственный перечень и заголовок
// «EXACTLY», но утверждала ЧЛЕНСТВО каждого имени, а не равенство наборов.
// Шестое имя добавилось, ничего не покраснив. Точный состав пинит
// `module_set_drift_test.go` — но он сверяет домен с каталогом типов, а не с
// ПРОЗОЙ, и о клиентских поверхностях не знает by construction.
//
// # Что судит анализатор
//
// Набор ВЫВОДИТСЯ разбором объявления `knownModules` (узел-литерал, не текст:
// имена модулей встречаются и в комментариях рядом, и гейт по подстроке краснел
// бы на собственном объяснении).
//
// В клиентских поверхностях — `services/iam/docs/content/**` и
// `proto/kacho/cloud/iam/**` — распознаётся ПЕРЕЧЕНЬ: имена модулей в
// код-форматировании (“ `имя` “ либо `<code>имя</code>`), соединённые косой
// чертой, ТРИ и более подряд. Каждый такой перечень обязан назвать весь набор.
//
// # ЧЕГО ОН НЕ СУДИТ, и это названо числом, а не умолчанием
//
//  1. СПАН ИЗ ДВУХ ИМЁН перечнем не считается — это законная пара («vpc/compute
//     остаются label-selectable»). В ЭТИХ ДВУХ поверхностях таких сегодня НОЛЬ,
//     и число печатается переписью, а не утверждается здесь: первая редакция
//     этого абзаца называла четыре — столько давал греп, не отличавший пару
//     внутри перечня от самостоятельной, — и гейт опроверг её на первом же
//     прогоне. Порог в три оставлен: пара — законная форма в дереве вообще
//     (комментарии Go, соседние домены), и гейт, у которого находки ложные,
//     отключают первым. Способность молчать на паре доказана инъекцией, а не
//     этим числом.
//
//  2. ПРОЗА без код-форматирования не судится: «модули iam, vpc и compute»
//     распознавателю не видны. Расширение на голый текст не замерялось и потому
//     не вводится — оно находило бы имена модулей в любом предложении, где они
//     соседствуют законно.
//
//  3. НЕ-КЛИЕНТСКИЕ комментарии Go вне охвата. Шапка `IsKnownModule` чинилась
//     руками и здесь не судится: её предмет — реализация, а не то, что обещано
//     клиенту.
//
//  4. ПОРЯДОК не судится — только состав.
//
// # Падает на ПУСТОМ ОБХОДЕ
//
// Ноль выведенных модулей, ноль прочитанных файлов поверхности либо ноль
// распознанных перечней — «находок ноль» неотличимо от «прочитано ноль».
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/PRO-Robotech/kacho/internal/treecorpus"
)

// ClientTruthIAMModuleSetOptions — вход анализатора.
type ClientTruthIAMModuleSetOptions struct {
	// Tree — СОСТАВ дерева, а не его корень: гейт берёт индекс git
	// (`treecorpus.NewTree`), инъекционная проба — синтетическое дерево
	// (`treecorpus.SyntheticTree`). Разбор — clienttruth_treefiles.go.
	Tree *treecorpus.Tree
	// ModuleSetFile — путь (от корня дерева) к объявлению закрытого набора.
	ModuleSetFile string
	// ModuleSetVar — имя переменной, чей литерал и есть набор.
	ModuleSetVar string
	// Surfaces — каталоги клиентских поверхностей (от корня дерева).
	Surfaces []string
	// SurfaceExts — расширения файлов поверхности.
	SurfaceExts []string
}

// ClientTruthIAMModuleSetCensus — объём осмотренного.
type ClientTruthIAMModuleSetCensus struct {
	// Modules — сколько имён выведено из объявления.
	Modules int
	// SurfaceFiles — сколько файлов поверхности прочитано.
	SurfaceFiles int
	// Enumerations — сколько перечней (три имени и более) распознано и рассужено.
	Enumerations int
	// PairSpans — сколько спанов РОВНО из двух имён встречено и НЕ рассужено.
	// Это объявленная слепая зона, а не находка.
	PairSpans int
}

// ClientTruthIAMModuleSetFinding — один неполный перечень.
type ClientTruthIAMModuleSetFinding struct {
	File    string
	Line    int
	Named   []string
	Missing []string
	Span    string
}

func (f ClientTruthIAMModuleSetFinding) String() string {
	return fmt.Sprintf("%s:%d: перечень называет %d из %d — не назван %s (%s)",
		f.File, f.Line, len(f.Named), len(f.Named)+len(f.Missing),
		strings.Join(f.Missing, ", "), f.Span)
}

// codeSpanPattern — образец, которым распознаётся имя в код-форматировании:
// `имя` либо <code>имя</code>. Подставляется через `%[1]s`.
//
// Имя ГОВОРИТ «образец», а не «лексема», и это не вкусовщина. Прежнее
// `codeToken` понимало «token» в смысле «лексема», но сканер безопасности судит
// имена констант по образцу `(?i)…|token|…` и объявлял находку «potential
// hardcoded credentials» (G101) на регулярке, к учётным данным отношения не
// имеющей. Подавление здесь было бы лечением экземпляра: имя, читающееся как
// «учётные данные», читается так и человеком, бегло просматривающим файл.
// Заодно оно стало точнее — это не сама лексема, а то, чем её находят.
const codeSpanPattern = "(?:`(%[1]s)`|<code>(%[1]s)</code>)"

// AuditClientTruthIAMModuleSet выводит закрытый набор модулей из его объявления и
// требует, чтобы каждый перечень клиентской поверхности назвал набор целиком.
func AuditClientTruthIAMModuleSet(
	opts ClientTruthIAMModuleSetOptions, log io.Writer,
) ([]ClientTruthIAMModuleSetFinding, ClientTruthIAMModuleSetCensus, error) {
	var census ClientTruthIAMModuleSetCensus

	modules, err := parseModuleSet(opts.Tree, opts.ModuleSetFile, opts.ModuleSetVar)
	if err != nil {
		return nil, census, err
	}
	census.Modules = len(modules)
	if len(modules) < 3 {
		return nil, census, fmt.Errorf(
			"из %s выведено %d имён — набор не прочитан, судить перечни не по чему",
			opts.ModuleSetFile, len(modules))
	}

	alt := make([]string, 0, len(modules))
	for _, m := range modules {
		alt = append(alt, regexp.QuoteMeta(m))
	}
	one := fmt.Sprintf(codeSpanPattern, "(?:"+strings.Join(alt, "|")+")")
	// Перечень — три и более имени подряд; пара ловится отдельно и НЕ судится.
	enumRe := regexp.MustCompile(one + `(?:\s*/\s*` + one + `){2,}`)
	pairRe := regexp.MustCompile(one + `\s*/\s*` + one)
	nameRe := regexp.MustCompile(one)

	want := map[string]bool{}
	for _, m := range modules {
		want[m] = true
	}

	var findings []ClientTruthIAMModuleSetFinding
	for _, surface := range opts.Surfaces {
		for _, rel := range clientTruthTreeFiles(opts.Tree, surface, true, opts.SurfaceExts...) {
			raw, rerr := clientTruthReadTreeFile(opts.Tree, rel)
			if rerr != nil {
				return nil, census, fmt.Errorf("чтение %s: %w", rel, rerr)
			}
			census.SurfaceFiles++
			for i, line := range strings.Split(string(raw), "\n") {
				enums := enumRe.FindAllString(line, -1)
				for _, span := range enums {
					census.Enumerations++
					named := map[string]bool{}
					for _, m := range nameRe.FindAllStringSubmatch(span, -1) {
						named[firstNonEmpty(m[1:])] = true
					}
					var missing []string
					for m := range want {
						if !named[m] {
							missing = append(missing, m)
						}
					}
					if len(missing) == 0 {
						continue
					}
					sort.Strings(missing)
					namedList := make([]string, 0, len(named))
					for m := range named {
						namedList = append(namedList, m)
					}
					sort.Strings(namedList)
					findings = append(findings, ClientTruthIAMModuleSetFinding{
						File: rel, Line: i + 1, Named: namedList, Missing: missing, Span: span,
					})
				}
				// Пары считаются ТОЛЬКО там, где перечня нет: иначе перечень из
				// трёх дал бы ещё и две «пары» и раздул объявленную слепую зону.
				if len(enums) == 0 {
					census.PairSpans += len(pairRe.FindAllString(line, -1))
				}
			}
		}
	}

	if log != nil {
		_, _ = fmt.Fprintf(log, "перепись: модулей выведено %d (%s) · файлов поверхности %d · "+
			"перечней рассужено %d · спанов из двух имён встречено %d (НЕ судятся — законная пара)\n",
			census.Modules, strings.Join(modules, ", "),
			census.SurfaceFiles, census.Enumerations, census.PairSpans)
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Line < findings[j].Line
	})
	return findings, census, nil
}

// parseModuleSet выводит набор РАЗБОРОМ объявления, а не чтением текста: имена
// модулей стоят и в комментариях рядом с ним.
func parseModuleSet(tree *treecorpus.Tree, rel, varName string) ([]string, error) {
	src, rerr := clientTruthReadTreeFile(tree, rel)
	if rerr != nil {
		return nil, fmt.Errorf("чтение %s: %w", rel, rerr)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, rel, src, 0)
	if err != nil {
		return nil, fmt.Errorf("разбор %s: %w", rel, err)
	}
	var out []string
	ast.Inspect(file, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for i, name := range vs.Names {
			if name.Name != varName || i >= len(vs.Values) {
				continue
			}
			lit, ok := vs.Values[i].(*ast.CompositeLit)
			if !ok {
				continue
			}
			for _, el := range lit.Elts {
				bl, ok := el.(*ast.BasicLit)
				if !ok || bl.Kind != token.STRING {
					continue
				}
				if s, uerr := strconv.Unquote(bl.Value); uerr == nil && s != "" {
					out = append(out, s)
				}
			}
		}
		return true
	})
	if len(out) == 0 {
		return nil, fmt.Errorf("в %s не найдено объявление %s со строковым литералом", rel, varName)
	}
	return out, nil
}

func firstNonEmpty(ss []string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
