// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// clienttruth_iam_moduleset.go — анализатор «перечень модулей платформы, названный
// клиентской поверхностью, полон».
//
// # Предмет
//
// Набор модулей закрыт, и его ЕДИНСТВЕННЫЙ источник — приставки ключей закрытой
// таблицы типов объекта (`objectTypes` пакета `services/iam/internal/authzmap`),
// которые продукт выводит производителем `authzmap.CatalogSeedModules`. Модуль —
// то, чем клиент выражает грант: `Rule.module`.
//
// # ПРЕДМЕТ — ПАКЕТ, А НЕ ФАЙЛ (задача продукта #1944)
//
// Таблица типов ПОРОЖДАЕТСЯ из манифестов модулей (#1092) и лежит теперь в
// `tables_gen.go`; прежде она была рукописной и лежала в `fga_types.go`. Гейт
// был привязан к имени файла и переезда не пережил: он отказал не находкой, а
// невозможностью отработать — «не найдено объявление», — то есть третьей
// категорией, поданной как красное.
//
// Здесь чинится КЛАСС, а не его экземпляр: объявление разрешается по ПАКЕТУ
// (`pkgvardecl.go`), потому что пакет и есть единица области видимости Go —
// package-level имя в нём ровно одно by construction, и перенос объявления между
// файлами о нём ничего не меняет. Второй экземпляр того же класса — проверка
// глаголов фикстур ролей — сломался тем же переездом и переведён тем же образом.
//
// Читается ПОРОЖДЁННОЕ объявление, а не манифесты, и это выбор: разбор манифеста
// завёл бы ВТОРУЮ форму его чтения (первая — загрузчик `services/iam/internal/
// manifest`, закрытый правилом видимости Go для пакетов вне сервиса), и две формы
// разошлись бы молча. Текст порождённого файла безопасен: его свежесть сверяется
// побайтово своим гейтом (`authzmapgen.TestGeneratedTablesAreFresh`), поэтому
// «текст отстал от манифеста» здесь невыразимо.
//
// # ИСТОЧНИК ПЕРЕЕЗЖАЛ ДВАЖДЫ, И ВТОРОЙ РАЗ ГЕЙТ ЗА НИМ НЕ ПОШЁЛ
//
// Первый переезд — задача продукта #1927. До неё анализатор выводил набор из
// отдельного литерала домена `knownModules`.
// Литерал снят: набор модулей стал строками каталога, а канон дерева выводится
// из той же таблицы типов, из которой посеяны ресурсы. Анализатор перенесён на
// новый источник тем же изменением — оставить его на снятой координате значило
// бы получить гейт, отказывающий по причине, к его предмету отношения не
// имеющей (`testing.md` §«Гейт на класс», п. 9).
//
// Второй переезд — #1092, и вот за ним анализатор НЕ пошёл: см. раздел «предмет —
// пакет, а не файл» выше. Привязка к пакету заведена именно затем, чтобы третьего
// раза не было: она переживает перенос объявления внутри пакета by construction.
//
// Предмет при этом НЕ изменился: перечень клиентской поверхности по-прежнему
// обязан назвать набор целиком, и цена неполноты по-прежнему в безопасности, а
// не в удобстве. Перечень, назвавший меньше, чем принимает сервер,
// сообщает клиенту, что тонко выдать доступ к недостающему домену НЕЛЬЗЯ, — а
// единственный документированный выход при таком чтении есть системная роль на
// весь уровень, то есть заведомое расширение доступа. Цена неполноты здесь не в
// удобстве, а в безопасности.
//
// Замер на день заведения (kacho#1627): сервер принимал ШЕСТЬ модулей, а называли
// ЧЕТЫРЕ и справочная страница ролей, и комментарий поля контракта. Рядом,
// третьим местом, стояла шапка функции членства — она называла ПЯТЬ. То есть
// разошлись не документ с кодом, а три места об одном предмете, из которых верно
// было ни одно. (Обе координаты того замера — функция и её проба — сняты вместе с
// литералом задачей #1927 и здесь написаны прозой: свидетельство остаётся, живого
// адреса не заводится.)
//
// # Почему это не удержала проба набора
//
// Проба набора несла собственный перечень и заголовок «EXACTLY», но утверждала
// ЧЛЕНСТВО каждого имени, а не равенство наборов. Шестое имя добавилось, ничего
// не покраснив. Точный состав пинит `module_set_drift_test.go` — но он сверяет
// производителя с объявлением написаний, а не с ПРОЗОЙ, и о клиентских
// поверхностях не знает by construction.
//
// # Что судит анализатор
//
// Набор ВЫВОДИТСЯ разбором объявления таблицы типов: узлы-ключи составного
// литерала, а не текст (имена модулей встречаются и в комментариях рядом, и гейт
// по подстроке краснел бы на собственном объяснении). Из каждого ключа берётся
// приставка до ПЕРВОЙ точки — то же разбиение, каким его делает
// `authzmap.SplitObjectType`; ключ без точки модуля не даёт, а не даёт себя
// целиком.
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
//  3. НЕ-КЛИЕНТСКИЕ комментарии Go вне охвата. Шапка функции членства чинилась
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
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/PRO-Robotech/kacho/internal/treecorpus"
)

// ClientTruthIAMModuleSetOptions — вход анализатора.
type ClientTruthIAMModuleSetOptions struct {
	// Tree — СОСТАВ дерева, а не его корень: гейт берёт индекс git
	// (`treecorpus.NewTree`), инъекционная проба — синтетическое дерево
	// (`treecorpus.SyntheticTree`). Разбор — clienttruth_treefiles.go.
	Tree *treecorpus.Tree
	// ModuleSetPkg — каталог ПАКЕТА (от корня дерева), объявляющего закрытую
	// таблицу типов, приставки ключей которой и есть набор модулей.
	//
	// Пакет, а не файл: объявление уже дважды переезжало между файлами пакета,
	// и оба раза привязка к имени файла давала отказ «не отработал» вместо
	// вердикта (#1927, #1944).
	ModuleSetPkg string
	// ModuleSetVar — имя переменной, чей составной литерал разбирается.
	ModuleSetVar string
	// Surfaces — каталоги клиентских поверхностей (от корня дерева).
	Surfaces []string
	// SurfaceExts — расширения файлов поверхности.
	SurfaceExts []string
}

// ClientTruthIAMModuleSetCensus — объём осмотренного.
type ClientTruthIAMModuleSetCensus struct {
	// Modules — сколько РАЗЛИЧНЫХ приставок выведено из ключей объявления.
	Modules int
	// TypeKeys — сколько ключей объявления прочитано. Печатается рядом с
	// Modules: приставок мало by construction, и одна их величина не отличила
	// бы «таблица прочитана» от «прочитаны две строки из двадцати семи».
	TypeKeys int
	// PkgFiles — сколько не-тестовых файлов пакета осмотрено в поисках
	// объявления. Без этой величины «объявление найдено» неотличимо от
	// «прочитан один файл, и повезло».
	PkgFiles int
	// DeclFile — где объявление нашлось. Печатается, потому что именно это
	// место и переезжало: читатель обязан видеть, О ЧЁМ вынесен вердикт, не
	// заглядывая в исходник гейта.
	DeclFile string
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

	modules, keys, decl, err := parseModuleSet(opts.Tree, opts.ModuleSetPkg, opts.ModuleSetVar)
	census.Modules, census.TypeKeys = len(modules), keys
	census.PkgFiles, census.DeclFile = decl.PkgFiles, decl.DeclFile
	if err != nil {
		return nil, census, err
	}
	if len(modules) < 3 {
		return nil, census, fmt.Errorf(
			"из %s (пакет %s) выведено %d имён — набор не прочитан, судить перечни не по чему",
			decl.DeclFile, opts.ModuleSetPkg, len(modules))
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
		_, _ = fmt.Fprintf(log, "перепись: файлов пакета %s осмотрено %d · объявление в %s · "+
			"ключей типа прочитано %d · модулей выведено %d (%s) · "+
			"файлов поверхности %d · перечней рассужено %d · спанов из двух имён встречено %d "+
			"(НЕ судятся — законная пара)\n",
			opts.ModuleSetPkg, census.PkgFiles, census.DeclFile,
			census.TypeKeys, census.Modules, strings.Join(modules, ", "),
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
// модулей стоят и в комментариях рядом с ним, и внутри разбора переноса, которым
// объяснён переезд самого объявления.
//
// Объявление разрешается по ПАКЕТУ (`findPackageVarLiteral`), а не по файлу: см.
// раздел «предмет — пакет, а не файл» в шапке. Отказ разрешения возвращается как
// есть — он уже называет пакет, имя и объём прочитанного.
//
// Возвращает РАЗЛИЧНЫЕ приставки ключей (отсортированно), число прочитанных
// ключей и перепись разрешения. Второе — объём осмотренного: приставок шесть при
// двадцати семи ключах, и по одному их числу «таблица прочитана» неотличимо от
// «прочитаны две строки».
func parseModuleSet(
	tree *treecorpus.Tree, pkgDir, varName string,
) ([]string, int, pkgVarDeclCensus, error) {
	lit, decl, err := findPackageVarLiteral(tree, pkgDir, varName)
	if err != nil {
		return nil, 0, decl, err
	}
	keysList := pkgVarLiteralStringKeys(lit)
	seen := map[string]bool{}
	var out []string
	for _, key := range keysList {
		// Приставка — до ПЕРВОЙ точки, то же разбиение, каким его делает
		// `authzmap.SplitObjectType`. Ключ без точки модуля не даёт: он не
		// «модуль без ресурса», а ключ неверной формы, и записать его в набор
		// значило бы объявить модулем то, чем таблица его не называет.
		dot := strings.IndexByte(key, '.')
		if dot <= 0 {
			continue
		}
		if m := key[:dot]; !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	if len(out) == 0 {
		return nil, len(keysList), decl, fmt.Errorf(
			"в %s объявление %s не дало НИ ОДНОГО ключа вида \"модуль.ресурс\" "+
				"(ключей прочитано %d) — форма ключа сменилась",
			decl.DeclFile, varName, len(keysList))
	}
	sort.Strings(out)
	return out, len(keysList), decl, nil
}

func firstNonEmpty(ss []string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
