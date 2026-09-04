// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// Вложенный вид, объявленный в каталоге, обязан ИМЕТЬ СПИСАНИЕ.
//
// ПРЕДМЕТ (задача `PRO-Robotech/kacho#353`). Величина вложенного потолка
// («сколько детей помещается в ОДНОМ родителе») задаётся администратором облака
// и посеяна миграцией. Если её никто не списывает, администратор видит величину,
// вправе её изменить, получает успешный ответ — и предел не применяется ни при
// каких условиях. Это «принято-и-проигнорировано» (`api-conventions.md`) на
// уровне подсистемы: продукт обещает возможность, которой нет.
//
// ПОЧЕМУ ГЕЙТ, А НЕ ВНИМАНИЕ. Заметить это по поведению НЕЛЬЗЯ: отсутствующий
// предел не отказывает, а «не отказал» выглядит ровно как «место было». Ни одна
// проба домена такого не увидит — у неё нет повода спросить про предел, которого
// нет. Единственное место, где два факта встречаются, — дерево.
//
// ЧТО ИМЕННО УТВЕРЖДАЕТСЯ: множество вложенных видов каталога совпадает с
// множеством видов, по которым списывает триггер.
// nestedKindsWithoutAChargerYet — виды, у которых списания ещё НЕТ, с причиной
// и предметом.
//
// СЕЙЧАС ПУСТ, и это цель, а не поломка: три записи vpc сняты В ТОМ ЖЕ
// изменении, где появилась их замена. Освобождение, которому больше нечего
// освобождать, — находка; перечень поэтому и проверяется на ПРЕДМЕТ, а не только
// на причину, и пустым он проходит, объявляя перепись.
//
// Это не «известное красное» и не отговорка: каждая запись называет ПОЧЕМУ
// списание не заведено вместе с гейтом, и почему заводить его наспех было бы
// хуже. Запись САМОИСТЕКАЕТ в обе стороны: вид, получивший списание, обязан уйти
// отсюда (иначе освобождение переживёт свой предмет), а вид, исчезнувший из
// каталога, — тем более.
//
// Гейт с этим перечнем всё равно строже, чем его отсутствие: ЧЕТВЁРТЫЙ такой вид
// завести уже нельзя — он покраснеет здесь в тот же день, а не через полгода на
// вопрос «почему предел не действует».
var nestedKindsWithoutAChargerYet = map[string]string{}

// TestNestedChargerExemptionsStillHaveSubject — освобождение живёт, пока у него
// есть предмет.
//
// Запись, которой больше нечего освобождать, не безобидна: она создаёт
// впечатление разобранного случая, и её унаследует следующая слепая зона.
func TestNestedChargerExemptionsStillHaveSubject(t *testing.T) {
	t.Parallel()

	root := repoRootFor(t)
	declared := nestedKindsOfCatalogue(t, root)
	charged, _, _ := nestedKindChargers(t, root)

	declaredSet := map[string]bool{}
	for _, k := range declared {
		declaredSet[k] = true
	}
	for kind, why := range nestedKindsWithoutAChargerYet {
		require.NotEmptyf(t, why, "освобождение %q обязано нести причину", kind)
		require.Truef(t, declaredSet[kind],
			"освобождению %q больше нечего освобождать: каталог такого вида не объявляет", kind)
		_, isCharged := charged[kind]
		require.Falsef(t, isCharged,
			"вид %q УЖЕ списывается — освобождение пережило свой предмет и обязано уйти "+
				"вместе с ним", kind)
	}
	t.Logf("перепись: освобождений прочитано %d, все с предметом", len(nestedKindsWithoutAChargerYet))
}

func TestEveryNestedQuotaKindHasACharger(t *testing.T) {
	t.Parallel()

	root := repoRootFor(t)

	declared := nestedKindsOfCatalogue(t, root)
	require.NotEmpty(t, declared,
		"вложенных видов в каталоге не найдено — предпосылка гейта сломана, и его "+
			"молчание не отличимо от согласия")

	charged, lifecycle, sqlSeen := nestedKindChargers(t, root)

	// Положительный контроль ПРЕДИКАТА, а не дерева: он обязан находить списание
	// там, где оно есть. Ноль здесь означал бы, что предикат мерит форму записи, а
	// не факт, — и первая редакция этого предиката именно такой и была: она искала
	// `carrier_type = '<литерал>'` и дала ноль при двух работающих списаниях,
	// потому что триггер адресует строку родителя ПЕРЕМЕННОЙ.
	require.NotEmptyf(t, charged,
		"предикат не нашёл НИ ОДНОГО списания по носителю-родителю на %d миграциях — "+
			"он мерит форму записи, а не факт", sqlSeen)
	require.NotEmpty(t, lifecycle,
		"предикат не нашёл НИ ОДНОЙ строки учёта родителя: без неё списывать нечего, "+
			"и «списание есть» было бы утверждением ни о чём")

	var findings []string
	for _, kind := range declared {
		if _, exempt := nestedKindsWithoutAChargerYet[kind]; exempt {
			continue
		}
		_, hasCharge := charged[kind]
		_, hasRow := lifecycle[kind]
		switch {
		case !hasCharge && !hasRow:
			findings = append(findings, kind+
				" — величина задаётся администратором и не делает НИЧЕГО: ни строки учёта "+
				"родителя, ни списания. Предел объявлен и не применяется ни при каких условиях")
		case !hasCharge:
			findings = append(findings, kind+
				" — строка учёта родителя заводится, но её никто не списывает: счётчик "+
				"стоит на нуле вечно, и потолок не наступает никогда")
		case !hasRow:
			findings = append(findings, kind+
				" — списание есть, а строки учёта родителя никто не заводит: списывать "+
				"нечего, и КАЖДАЯ вставка ребёнка отвергается «потолок не назван»")
		}
	}

	// Зеркальная форма: списание по виду, которого каталог не объявляет. Оно тише
	// и потому хуже — величины у такого вида нет, значит строка учёта родителя
	// заводится пустой либо не заводится вовсе, а ребёнок отвергается навсегда.
	declaredSet := map[string]bool{}
	for _, k := range declared {
		declaredSet[k] = true
	}
	for kind, where := range charged {
		// Зеркало смотрит ТОЛЬКО на трёхчастные токены: аргументами списания
		// законно стоят и плоские виды, и имена столбцов, и они каталогом
		// вложенных видов не объявляются by construction.
		if strings.Count(kind, ".") != 2 {
			continue
		}
		if !declaredSet[kind] {
			findings = append(findings, kind+
				" — списывается ("+where+"), но каталог такого вложенного вида не объявляет: "+
				"величины у него нет, и отказ наступит на первой же вставке ребёнка")
		}
	}
	sort.Strings(findings)

	// В переписи называется пересечение с каталогом, а не всё, что нашёл предикат:
	// аргументами списания законно стоят и плоские виды, и имена столбцов, и
	// печатать их числом значило бы называть величину, которая ничего не меряет.
	chargedNested, rowNested := 0, 0
	for _, k := range declared {
		if _, ok := charged[k]; ok {
			chargedNested++
		}
		if _, ok := lifecycle[k]; ok {
			rowNested++
		}
	}
	t.Logf("перепись: миграций осмотрено %d; вложенных видов каталога %d (%s); "+
		"из них со списанием %d, со строкой учёта родителя %d; освобождений %d",
		sqlSeen, len(declared), strings.Join(declared, ", "), chargedNested, rowNested,
		len(nestedKindsWithoutAChargerYet))

	require.Empty(t, findings,
		"вложенный потолок, который задаётся и не действует, — обещание, за которое никто "+
			"не отвечает: администратор меняет число, ответ успешен, предел не наступает:\n%s",
		strings.Join(findings, "\n"))
}

// nestedKindsOfCatalogue читает вложенные виды из ТАБЛИЦЫ КАТАЛОГА разбором AST.
//
// Разбором, а не поиском по тексту: запись каталога есть пара «вид, носитель», и
// вложенным вид делает НОСИТЕЛЬ, а не число точек в имени. Поиск по форме имени
// ошибся бы на `iam.project` (двухчастный, носитель — аккаунт) в одну сторону и
// на любом будущем трёхчастном виде с корневым носителем — в другую.
//
// Каталог живёт под `services/iam/internal/`, поэтому импортировать его отсюда
// нельзя by construction (правило `internal` языка), и это причина разбора, а не
// лень.
func nestedKindsOfCatalogue(t *testing.T, root string) []string {
	t.Helper()

	path := filepath.Join(root, "services", "iam", "internal", "domain", "limit.go")
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	require.NoErrorf(t, err, "разбор каталога видов %s", path)

	// Корни аренды — носители, которые НЕ являются типами ресурсов. Читаются из
	// ОБЪЯВЛЕННЫХ констант того же файла — и по ИМЕНИ константы, а не по её
	// значению: в таблице каталога носитель-корень стоит идентификатором
	// (`CarrierProject`), а носитель-родитель — литералом. Гейт, знающий только
	// значения, не разрешил бы идентификатор и объявил бы вложенными ВСЕ записи;
	// именно так первая редакция и повела себя — назвала находкой все 32 вида.
	rootByName := map[string]bool{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || len(vs.Names) == 0 || len(vs.Values) == 0 {
				continue
			}
			if !strings.HasPrefix(vs.Names[0].Name, "Carrier") {
				continue
			}
			if lit, ok := vs.Values[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
				if _, err := strconv.Unquote(lit.Value); err == nil {
					rootByName[vs.Names[0].Name] = true
				}
			}
		}
	}
	require.NotEmpty(t, rootByName,
		"корней аренды в каталоге не найдено — без них «вложенный» неотличим от «корневого»")

	var nested []string
	ast.Inspect(file, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok || len(vs.Names) == 0 || vs.Names[0].Name != "countableKinds" {
			return true
		}
		lit, ok := vs.Values[0].(*ast.CompositeLit)
		if !ok {
			return true
		}
		for _, el := range lit.Elts {
			entry, ok := el.(*ast.CompositeLit)
			if !ok || len(entry.Elts) != 2 {
				continue
			}
			kind, kok := astStringOf(entry.Elts[0])
			if !kok {
				continue
			}
			// Носитель-КОРЕНЬ стоит идентификатором объявленной константы;
			// носитель-РОДИТЕЛЬ — строковым литералом. Вложенным вид делает второе.
			if ident, isIdent := entry.Elts[1].(*ast.Ident); isIdent {
				require.Truef(t, rootByName[ident.Name],
					"носитель %q вида %q — идентификатор, но не объявленная константа корня: "+
						"гейт не знает, корневой он или родительский, и молчать здесь нельзя",
					ident.Name, kind)
				continue
			}
			if _, isLit := astStringOf(entry.Elts[1]); !isLit {
				continue
			}
			nested = append(nested, kind)
		}
		return false
	})
	sort.Strings(nested)
	return nested
}

// astStringOf отдаёт значение строкового литерала.
func astStringOf(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	v, err := strconv.Unquote(lit.Value)
	return v, err == nil
}

// nestedKindChargers отдаёт виды, по которым списывает триггер, и виды, у
// которых заводится строка учёта родителя.
//
// Предикат АРНОСТИ НЕ ЗНАЕТ, и это несущее решение. У разных владельцев позиции
// аргументов разные: nlb передаёт «вид ребёнка, столбец родителя, вложенный вид»,
// а vpc занимает вторую позицию булевым столбцом системного ребёнка. Гейт,
// знающий позицию, объявил бы несписывающим владельца, который списывает, — то
// есть мерил бы форму записи вместо факта. Утверждение здесь ровно одно:
// **триггер списания НАЗЫВАЕТ этот вложенный вид**.
//
// Вызов вдобавок переносится по строкам, поэтому текст нормализуется по пробелам.
func nestedKindChargers(t *testing.T, root string) (charged, lifecycle map[string]string, sqlSeen int) {
	t.Helper()

	files, err := treecorpus.UnderWithSuffix(filepath.Join(root, "services"), ".sql")
	require.NoError(t, err, "перечень миграций берётся у индекса дерева, а не обходом диска")

	charged, lifecycle = map[string]string{}, map[string]string{}
	chargeRe := regexp.MustCompile(`kacho_quota_count\(([^)]*)\)`)
	lifeRe := regexp.MustCompile(`kacho_quota_carrier_lifecycle\(([^)]*)\)`)
	// `[^']*`, а НЕ `[^']+`: среди аргументов законно стоит ПУСТАЯ строка (у vpc
	// вторая позиция — булев столбец системного ребёнка, и у подсетей его нет).
	// Требуя непустоты, выражение не сопоставляло пустую пару кавычек как целое и
	// разъезжалось по кавычкам дальше: следующий настоящий аргумент оказывался
	// «внутри» ложной пары и не читался вовсе. Ошибка тихая — предикат находил
	// ЧАСТЬ аргументов и объявлял вид несписываемым при работающем списании.
	argRe := regexp.MustCompile(`'([^']*)'`)
	space := regexp.MustCompile(`\s+`)

	for _, path := range files {
		if !strings.Contains(path, "/internal/migrations/") {
			continue
		}
		raw, rerr := os.ReadFile(path)
		require.NoError(t, rerr, "чтение %s", path)
		sqlSeen++
		body := space.ReplaceAllString(string(raw), " ")

		rel := path
		if i := strings.Index(path, "/services/"); i >= 0 {
			rel = path[i+1:]
		}
		for _, m := range chargeRe.FindAllStringSubmatch(body, -1) {
			for _, a := range argRe.FindAllStringSubmatch(m[1], -1) {
				if a[1] == "" {
					continue
				}
				charged[a[1]] = rel
			}
		}
		for _, m := range lifeRe.FindAllStringSubmatch(body, -1) {
			for _, a := range argRe.FindAllStringSubmatch(m[1], -1) {
				if a[1] == "" {
					continue
				}
				lifecycle[a[1]] = rel
			}
		}
	}
	return charged, lifecycle, sqlSeen
}
