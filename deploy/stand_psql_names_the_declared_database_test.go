// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

// stand_psql_names_the_declared_database_test.go — справочная цель стенда зовёт
// базу тем именем, которым её ОБЪЯВИЛ профиль, а не выведенным приставкой
// платформы.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Служба прав получила собственное имя продукта, и её база зовётся `kaname`
// (это уже держит iam_database_named_for_its_product_test.go — там судятся
// ОБЪЯВЛЕНИЯ чарта). Справочная цель стенда `make psql SVC=<служба>` собирала
// имя базы САМА — приставкой платформы плюс имя службы, — и потому вела не туда:
// `SVC=iam` давало `kacho_iam`, тогда как база называется `kaname`.
//
// Оператор при этом получает отказ, у которого НЕТ причины в продукте: Postgres
// отвечает «database does not exist» на имя, которого никто не объявлял. Рядом
// живёт рабочая цель `psql-iam`, называющая имя верно, — два места об одном
// предмете, из которых верно одно.
//
// Выведение задевало не только имя базы: пользователем цель называла имя
// службы, а профиль объявляет `kacho_nlb` для nlb; узел базы цель звала
// `pg-<служба>`, тогда как helm именует его `<релиз>-pg-<служба>`. То есть общая
// цель не работала НИ ДЛЯ ОДНОЙ службы, и это было ненаблюдаемо: цель
// интерактивная, её отказ видит только тот, кто её позвал.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ИМЕННО ЗАПРЕЩЕНО — ВЫВЕДЕНИЕ, А НЕ ПРИСТАВКА
//
// Запрет судит ПРОИСХОЖДЕНИЕ величины, а не её написание. Имя базы соседней
// службы законно начинается приставкой платформы (`kacho_vpc`) — эти службы
// остаются Kachō, и переименования не требуют. Находкой является другое: имя,
// СОБРАННОЕ рецептом из `$(SVC)`, потому что собранное имя перестаёт зависеть от
// того, что объявил профиль, и расходится с ним молча.
//
// Поэтому распознаватель ловит `$(SVC)` внутри аргумента `-d`/`-U`, а не
// подстроку «kacho_». Написание `-d kacho_$(SVC)` и написание `-d $(SVC)` — один
// и тот же класс: оба выводят, а не читают.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ПРОВЕРЯЕТСЯ ПАРА, А НЕ ОДНО ИМЯ
//
// У имени базы на стенде два объявителя: профиль (`pg-<служба>.auth.database`,
// по нему подчарт СОЗДАЁТ базу) и рецепт справочной цели (по нему оператор в неё
// СТУЧИТСЯ). Каноничность каждого по отдельности не даёт согласия: рецепт,
// назвавший `kacho_geo` там, где профиль объявил `kacho_storage`, каноничен по
// написанию и неверен по существу. Судится согласие ПАРЫ, стек за стеком.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЕДИНИЦА СЧЁТА — ПАРА «РЕЦЕПТ × СТЕК»
//
// Один и тот же рецепт судится столько раз, во скольких стеках объявлена его
// служба: вопрос не «правильно ли написано», а «куда попадёт оператор ЭТОГО
// стенда». Служба, чей профиль базы стек не объявляет вовсе (подчарт выключен
// условием), — НЕ находка: сверять не с чем. Это состояние печатается ОТДЕЛЬНОЙ
// величиной переписи, чтобы «ноль находок» было отличимо от «ноль сверенного».
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ЭТА ПРОВЕРКА НЕ ДЕЛАЕТ — сказано прямо
//
//   - она НЕ судит имя узла базы и имя релиза: и то и другое helm производит из
//     имени релиза и псевдонима подчарта, то есть выводится ЗАКОННО и по
//     правилу, которое принадлежит helm, а не нам;
//   - она НЕ судит пароль: он стендовый и объявлен профилем отдельно;
//   - она НЕ судит имена баз соседних служб на каноничность — этим владеет
//     iam_database_named_for_its_product_test.go для своей службы, а прочие
//     остаются Kachō;
//   - она читает ОБЪЯВЛЕНИЯ (Makefile и файлы значений), а не рендер: цель
//     `psql` в рендер не попадает вовсе.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕМ ДОКАЗАНА СПОСОБНОСТЬ УПАСТЬ
//
// Разбор вынесен в чистые функции parseStandPsqlRecipes и auditStandPsql,
// принимающие ТЕКСТ и ОБЪЯВЛЕНИЯ, а не пути: настоящее дерево и синтетический
// вход инъекции проходят одну и ту же функцию. Инъекция —
// stand_psql_names_the_declared_database_injection_test.go, по одной оси на
// каждую форму отказа плюс законный близнец на каждую.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// standMakefile — рецепты стенда. Одно объявление пути на файл.
const standMakefile = "Makefile"

// psqlDBVar / psqlUserVar — форма табличного объявления в Makefile: величина
// ВЫБИРАЕТСЯ по имени службы, а не собирается из него. Именно эту форму рецепт
// вправе подставлять в `-d`/`-U`.
const (
	psqlDBVar   = "$(PSQL_DB_$(SVC))"
	psqlUserVar = "$(PSQL_USER_$(SVC))"
)

// standPsqlRecipe — одно обращение к psql в рецепте цели стенда.
type standPsqlRecipe struct {
	target string // имя цели, в рецепте которой стоит обращение
	line   int    // строка Makefile — координата находки
	svc    string // служба, если цель её называет ("" у общей цели по $(SVC))
	user   string // аргумент -U как он написан
	db     string // аргумент -d как он написан
}

// standPsqlDecl — что профиль ОДНОГО стека объявляет о базе службы.
type standPsqlDecl struct {
	stack    string
	chain    string
	svc      string
	database string // pg-<svc>.auth.database; "" = стек подчарт не объявил
	username string // pg-<svc>.auth.username
}

// standPsqlCensus — объём осмотренного. Печатается всегда, включая зелёный
// прогон: «ноль находок» обязано быть отличимо от «ноль прочитанного».
type standPsqlCensus struct {
	makefileLines int // строк Makefile прочитано
	recipes       int // обращений к psql распознано
	tableEntries  int // табличных объявлений PSQL_DB_*/PSQL_USER_* прочитано
	stacks        int // стеков рассмотрено
	pairsJudged   int // пар «рецепт × стек» сверено
	pairsAgreed   int // из них сошлось
	notDeclared   int // пар, где стек базу службы не объявляет — сверять не с чем
}

var (
	// makeTargetLine — заголовок цели. Рецепт цели идёт до следующего
	// заголовка; так же его читает сам make.
	makeTargetLine = regexp.MustCompile(`^([A-Za-z0-9_.\-/%$()]+)\s*:(?:[^=]|$)`)
	// psqlInvocation — обращение к psql в строке рецепта.
	psqlInvocation = regexp.MustCompile(`(^|[\s;&|])psql\s`)
	// psqlArgU / psqlArgD — аргументы. Значение берётся ЦЕЛИКОМ до пробела:
	// `$(PSQL_DB_$(SVC))` содержит скобки и пробелов не содержит.
	psqlArgU = regexp.MustCompile(`-U\s+(\S+)`)
	psqlArgD = regexp.MustCompile(`-d\s+(\S+)`)
	// psqlTableEntry — табличное объявление величины по имени службы.
	psqlTableEntry = regexp.MustCompile(`^PSQL_(DB|USER)_([a-z0-9-]+)\s*:?=\s*(\S+)`)
	// psqlTargetSvc — имя службы в имени цели `psql-<служба>`.
	psqlTargetSvc = regexp.MustCompile(`^psql-([a-z0-9-]+)$`)
)

// parseStandPsqlRecipes читает ТЕКСТ Makefile и отдаёт обращения к psql плюс
// таблицу объявленных величин.
//
// Функция чистая: тем же входом её кормит инъекция.
func parseStandPsqlRecipes(makefile string) ([]standPsqlRecipe, map[string]string, int) {
	var (
		recipes []standPsqlRecipe
		table   = map[string]string{}
		target  string
	)
	lines := strings.Split(makefile, "\n")
	for i, line := range lines {
		if m := psqlTableEntry.FindStringSubmatch(line); m != nil {
			table[m[1]+"_"+m[2]] = m[3]
			continue
		}
		if !strings.HasPrefix(line, "\t") {
			if m := makeTargetLine.FindStringSubmatch(line); m != nil {
				target = m[1]
			}
			continue
		}
		if !psqlInvocation.MatchString(line) {
			continue
		}
		r := standPsqlRecipe{target: target, line: i + 1}
		if m := psqlTargetSvc.FindStringSubmatch(target); m != nil {
			r.svc = m[1]
		}
		if m := psqlArgU.FindStringSubmatch(line); m != nil {
			r.user = m[1]
		}
		if m := psqlArgD.FindStringSubmatch(line); m != nil {
			r.db = m[1]
		}
		recipes = append(recipes, r)
	}
	return recipes, table, len(lines)
}

// standPsqlResolve отвечает, чем аргумент рецепта является: литералом,
// табличным выбором или ВЫВЕДЕНИЕМ из имени службы.
//
// Три исхода, а не два: «выведено» и «не объявлено» — разные беды, и тексты
// находок обязаны их различать.
func standPsqlResolve(arg, tableVar string, table map[string]string, svc string) (value string, derived bool, missing bool) {
	switch {
	case arg == tableVar:
		key := strings.TrimSuffix(strings.TrimPrefix(tableVar, "$(PSQL_"), "_$(SVC))") + "_" + svc
		v, ok := table[key]
		if !ok {
			return "", false, true
		}
		return v, false, false
	case strings.Contains(arg, "$(SVC)"):
		return arg, true, false
	case strings.Contains(arg, "$("):
		// Иная подстановка — не выведение и не литерал. Судить её нечем:
		// величина известна только make. Это НЕ находка и НЕ согласие.
		return "", false, true
	default:
		return arg, false, false
	}
}

// auditStandPsql судит пары «рецепт × стек» и возвращает находки с переписью.
//
// СЛУЖБУ РЕЦЕПТА ОПРЕДЕЛЯЕТ НЕ ИМЯ ЦЕЛИ, А ТО, ЧТО РЕЦЕПТ НАЗЫВАЕТ. Форм три, и
// правило выбора у каждой своё:
//
//  1. рецепт подставляет табличные величины (`$(PSQL_DB_$(SVC))`) — служба
//     приходит параметром, поэтому судится по КАЖДОЙ службе, объявленной стеком:
//     оператор вправе позвать цель с любой;
//  2. имя цели называет службу (`psql-<служба>`) — судится она одна;
//  3. рецепт называет имя базы ЛИТЕРАЛОМ — служба выводится ИЗ ОБЪЯВЛЕНИЯ стека:
//     находится та, чью базу стек так и назвал. Ни одной такой службы в стеке —
//     находка: рецепт стучится в базу, которой на этом стенде нет.
//
// Третья форма несущая, а не полнота ради полноты: имя цели служб не называет
// (`wipe-iam-db`), и разбор по имени цели судил бы такой рецепт против всех
// служб сразу, выдавая по находке на каждую чужую. Это был бы прибор, у
// которого почти все находки ложны, — такой отключают первым.
func auditStandPsql(recipes []standPsqlRecipe, table map[string]string, decls []standPsqlDecl, makefileLines int) ([]string, standPsqlCensus) {
	var (
		findings []string
		census   = standPsqlCensus{makefileLines: makefileLines, recipes: len(recipes), tableEntries: len(table)}
	)

	stacks := map[string]bool{}
	byStackSvc := map[string]standPsqlDecl{}
	for _, d := range decls {
		stacks[d.stack] = true
		byStackSvc[d.stack+"/"+d.svc] = d
	}
	census.stacks = len(stacks)
	stackNames := standPsqlSortedStacks(stacks)

	// dbOwner — служба, чью базу стек назвал ЭТИМ именем. Имя базы уникально в
	// пределах одного экземпляра Postgres; здесь оно к тому же различает службы.
	dbOwner := map[string]standPsqlDecl{}
	for _, d := range decls {
		if d.database != "" {
			dbOwner[d.stack+"/"+d.database] = d
		}
	}

	// judge сверяет ОДНУ пару «рецепт × объявление».
	judge := func(r standPsqlRecipe, d standPsqlDecl, db, user string) {
		census.pairsJudged++
		switch {
		case db != d.database:
			findings = append(findings, fmt.Sprintf(
				"%s:%d (цель %q, служба %q, стек %q): рецепт стучится в базу %q, "+
					"а профиль объявил %q — оператор получит «database does not exist» "+
					"на имя, которого никто не создавал",
				standMakefile, r.line, r.target, d.svc, d.stack, db, d.database))
		case d.username != "" && user != "" && user != d.username:
			findings = append(findings, fmt.Sprintf(
				"%s:%d (цель %q, служба %q, стек %q): рецепт входит пользователем %q, "+
					"а профиль объявил %q",
				standMakefile, r.line, r.target, d.svc, d.stack, user, d.username))
		default:
			census.pairsAgreed++
		}
	}

	for _, r := range recipes {
		// Выведение — свойство САМОГО рецепта: оно не зависит ни от службы, ни
		// от стека, поэтому находка одна, а не по одной на каждую пару.
		if strings.Contains(r.db, "$(SVC)") && r.db != psqlDBVar {
			findings = append(findings, fmt.Sprintf(
				"%s:%d (цель %q): имя базы ВЫВЕДЕНО из имени службы — %q. Величину "+
					"объявляет профиль (pg-<служба>.auth.database); выведенная перестаёт "+
					"от него зависеть и расходится молча",
				standMakefile, r.line, r.target, r.db))
		}
		if strings.Contains(r.user, "$(SVC)") && r.user != psqlUserVar {
			findings = append(findings, fmt.Sprintf(
				"%s:%d (цель %q): пользователь базы ВЫВЕДЕН из имени службы — %q. "+
					"Профиль объявляет его отдельно (pg-<служба>.auth.username)",
				standMakefile, r.line, r.target, r.user))
		}
		if (strings.Contains(r.db, "$(SVC)") && r.db != psqlDBVar) ||
			(strings.Contains(r.user, "$(SVC)") && r.user != psqlUserVar) {
			continue
		}

		switch {
		case r.db == psqlDBVar:
			// Форма 1: служба приходит параметром.
			services := map[string]bool{}
			for _, d := range decls {
				services[d.svc] = true
			}
			for _, svc := range standPsqlSortedStacks(services) {
				db, _, dbMissing := standPsqlResolve(r.db, psqlDBVar, table, svc)
				user, _, userMissing := standPsqlResolve(r.user, psqlUserVar, table, svc)
				if dbMissing {
					findings = append(findings, fmt.Sprintf(
						"%s:%d (цель %q): таблица не объявляет PSQL_DB_%s — оператор, позвавший "+
							"цель с этой службой, получит пустое имя базы",
						standMakefile, r.line, r.target, svc))
					continue
				}
				if userMissing && r.user == psqlUserVar {
					findings = append(findings, fmt.Sprintf(
						"%s:%d (цель %q): таблица не объявляет PSQL_USER_%s",
						standMakefile, r.line, r.target, svc))
					continue
				}
				for _, stack := range stackNames {
					d, ok := byStackSvc[stack+"/"+svc]
					if !ok || d.database == "" {
						census.notDeclared++
						continue
					}
					judge(r, d, db, user)
				}
			}

		case r.svc != "":
			// Форма 2: службу называет имя цели.
			for _, stack := range stackNames {
				d, ok := byStackSvc[stack+"/"+r.svc]
				if !ok || d.database == "" {
					census.notDeclared++
					continue
				}
				judge(r, d, r.db, r.user)
			}

		case r.db != "" && !strings.Contains(r.db, "$("):
			// Форма 3: службу называет само имя базы.
			for _, stack := range stackNames {
				d, ok := dbOwner[stack+"/"+r.db]
				if !ok {
					census.pairsJudged++
					findings = append(findings, fmt.Sprintf(
						"%s:%d (цель %q, стек %q): рецепт стучится в базу %q, которой на этом "+
							"стенде не объявляет НИ ОДИН подчарт — сверять не с чем, и это не "+
							"«ноль находок», а «стучимся в никуда»",
						standMakefile, r.line, r.target, stack, r.db))
					continue
				}
				judge(r, d, r.db, r.user)
			}

		default:
			// Имя базы вычисляет make либо его нет вовсе — судить нечем.
			census.notDeclared++
		}
	}

	sort.Strings(findings)
	return findings, census
}

// standPsqlSortedStacks — имена стеков в устойчивом порядке: перечень находок
// обязан быть воспроизводим от прогона к прогону.
func standPsqlSortedStacks(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// readStandPsqlDecls собирает объявления баз по КАЖДОМУ стеку таблицы стендов.
//
// Перечень служб ВЫВОДИТСЯ из ключей `pg-*` дерева значений, а не выписан:
// выписанный разошёлся бы с деревом молча.
func readStandPsqlDecls(t *testing.T) []standPsqlDecl {
	t.Helper()
	var decls []standPsqlDecl
	stacks := deployStacks(t)
	for _, name := range sortedStackNames(stacks) {
		chain := stacks[name]
		tree := effectiveValues(t, chain)
		for key, node := range tree {
			svc, ok := strings.CutPrefix(key, "pg-")
			if !ok {
				continue
			}
			m, ok := node.(map[string]any)
			if !ok {
				continue
			}
			db, _ := leafString(m, []string{"auth", "database"})
			user, _ := leafString(m, []string{"auth", "username"})
			decls = append(decls, standPsqlDecl{
				stack: name, chain: strings.Join(chain, ","),
				svc: svc, database: db, username: user,
			})
		}
	}
	sort.Slice(decls, func(i, j int) bool {
		if decls[i].stack != decls[j].stack {
			return decls[i].stack < decls[j].stack
		}
		return decls[i].svc < decls[j].svc
	})
	return decls
}

// TestStandPsqlNamesTheDeclaredDatabase — гейт класса.
func TestStandPsqlNamesTheDeclaredDatabase(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(".", standMakefile))
	require.NoError(t, err, "рецепты стенда не читаются — предпосылка проверки исчезла, "+
		"а не дерево стало чистым")

	recipes, table, lines := parseStandPsqlRecipes(string(raw))
	decls := readStandPsqlDecls(t)
	findings, census := auditStandPsql(recipes, table, decls, lines)

	t.Logf("перепись: строк Makefile %d · обращений к psql %d · табличных объявлений %d · "+
		"стеков %d · пар «рецепт × стек» сверено %d (сошлось %d) · пар без объявления базы %d",
		census.makefileLines, census.recipes, census.tableEntries, census.stacks,
		census.pairsJudged, census.pairsAgreed, census.notDeclared)

	if census.recipes == 0 || census.stacks == 0 {
		t.Fatalf("обход пуст — вердикт беспредметен: обращений к psql %d, стеков %d",
			census.recipes, census.stacks)
	}

	if len(findings) > 0 {
		t.Fatalf("справочная цель стенда зовёт базу не тем именем, которым её объявил "+
			"профиль — %d находок:\n  %s", len(findings), strings.Join(findings, "\n  "))
	}

	require.NotZero(t, census.pairsAgreed,
		"положительный контроль пуст: не сошлось НИ ОДНОЙ пары «рецепт × стек» — "+
			"отрицание выше выполнилось бы на дереве, из которого вынесли всё")
}
