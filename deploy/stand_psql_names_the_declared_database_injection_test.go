// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

// stand_psql_names_the_declared_database_injection_test.go — ДОКАЗАТЕЛЬСТВО
// СПОСОБНОСТИ УПАСТЬ для TestStandPsqlNamesTheDeclaredDatabase.
//
// Инъекция кормит те же чистые функции (parseStandPsqlRecipes, auditStandPsql),
// что и настоящее дерево, поэтому доказанное здесь верно для него. По каждой оси
// — ДВЕ стороны: внесённый дефект обязан дать находку, а законный близнец той же
// формы обязан молчать. Односторонняя ось зеленела бы на распознавателе,
// который не работает вовсе.
//
// Каждая ось меняет РОВНО ОДИН факт против своего близнеца: иначе неизвестно,
// какой из двух дал красное.

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// injStandDecls — объявления двух стеков о двух службах. Один вход на все оси:
// различаются только рецепты.
func injStandDecls() []standPsqlDecl {
	return []standPsqlDecl{
		{stack: "dev", chain: "values.dev.yaml", svc: "iam", database: "kaname", username: "iam"},
		{stack: "dev", chain: "values.dev.yaml", svc: "vpc", database: "kacho_vpc", username: "vpc"},
		{stack: "prod", chain: "values.prod.yaml", svc: "iam", database: "kaname", username: "iam"},
		{stack: "prod", chain: "values.prod.yaml", svc: "vpc", database: "kacho_vpc", username: "vpc"},
	}
}

// injStandTable — полная таблица величин, годная для обеих служб.
func injStandTable() map[string]string {
	return map[string]string{
		"DB_iam": "kaname", "USER_iam": "iam",
		"DB_vpc": "kacho_vpc", "USER_vpc": "vpc",
	}
}

// injStandAudit прогоняет рецепты из текста Makefile через настоящий разбор.
func injStandAudit(t *testing.T, makefile string, table map[string]string) []string {
	t.Helper()
	recipes, parsed, lines := parseStandPsqlRecipes(makefile)
	if table == nil {
		table = parsed
	}
	findings, census := auditStandPsql(recipes, table, injStandDecls(), lines)
	require.NotZero(t, census.recipes, "инъекция беспредметна: обращений к psql не распознано")
	return findings
}

func TestStandPsqlInjection_DerivedDatabaseIsAFinding(t *testing.T) {
	defect := "psql:\n\tkubectl exec -- psql -U $(PSQL_USER_$(SVC)) -d kacho_$(SVC)\n"
	twin := "psql:\n\tkubectl exec -- psql -U $(PSQL_USER_$(SVC)) -d $(PSQL_DB_$(SVC))\n"

	got := injStandAudit(t, defect, injStandTable())
	require.NotEmpty(t, got, "имя базы, собранное из $(SVC), обязано быть находкой")
	require.Contains(t, strings.Join(got, "\n"), "имя базы ВЫВЕДЕНО")

	require.Empty(t, injStandAudit(t, twin, injStandTable()),
		"законный близнец — табличный выбор — обязан молчать")
}

func TestStandPsqlInjection_DerivedUserIsAFinding(t *testing.T) {
	defect := "psql:\n\tkubectl exec -- psql -U $(SVC) -d $(PSQL_DB_$(SVC))\n"
	twin := "psql:\n\tkubectl exec -- psql -U $(PSQL_USER_$(SVC)) -d $(PSQL_DB_$(SVC))\n"

	got := injStandAudit(t, defect, injStandTable())
	require.NotEmpty(t, got, "пользователь, собранный из $(SVC), обязан быть находкой")
	require.Contains(t, strings.Join(got, "\n"), "пользователь базы ВЫВЕДЕН")

	require.Empty(t, injStandAudit(t, twin, injStandTable()),
		"законный близнец обязан молчать")
}

func TestStandPsqlInjection_MissingTableEntryIsAFinding(t *testing.T) {
	recipe := "psql:\n\tkubectl exec -- psql -U $(PSQL_USER_$(SVC)) -d $(PSQL_DB_$(SVC))\n"

	partial := injStandTable()
	delete(partial, "DB_vpc")
	got := injStandAudit(t, recipe, partial)
	require.NotEmpty(t, got, "служба, объявленная стеком и не объявленная таблицей, обязана быть находкой")
	require.Contains(t, strings.Join(got, "\n"), "PSQL_DB_vpc")

	require.Empty(t, injStandAudit(t, recipe, injStandTable()),
		"полная таблица обязана молчать")
}

func TestStandPsqlInjection_LiteralDisagreeingWithTheProfileIsAFinding(t *testing.T) {
	// Служба выводится из ИМЕНИ ЦЕЛИ: `psql-iam` называет её сама.
	defect := "psql-iam:\n\tkubectl exec -- psql -U iam -d kacho_iam\n"
	twin := "psql-iam:\n\tkubectl exec -- psql -U iam -d kaname\n"

	got := injStandAudit(t, defect, injStandTable())
	require.NotEmpty(t, got, "рецепт, стучащийся в отставленное имя базы, обязан быть находкой")
	require.Contains(t, strings.Join(got, "\n"), "database does not exist")

	require.Empty(t, injStandAudit(t, twin, injStandTable()),
		"рецепт, назвавший объявленное имя, обязан молчать")
}

func TestStandPsqlInjection_LiteralUserDisagreeingIsAFinding(t *testing.T) {
	defect := "psql-vpc:\n\tkubectl exec -- psql -U kacho_vpc -d kacho_vpc\n"
	twin := "psql-vpc:\n\tkubectl exec -- psql -U vpc -d kacho_vpc\n"

	got := injStandAudit(t, defect, injStandTable())
	require.NotEmpty(t, got, "рецепт, входящий не тем пользователем, обязан быть находкой")
	require.Contains(t, strings.Join(got, "\n"), "рецепт входит пользователем")

	require.Empty(t, injStandAudit(t, twin, injStandTable()), "законный близнец обязан молчать")
}

func TestStandPsqlInjection_DatabaseNoSubchartDeclaresIsAFinding(t *testing.T) {
	// Служба выводится из САМОГО ИМЕНИ БАЗЫ: имя цели её не называет.
	defect := "wipe-db:\n\tkubectl exec -- psql -U iam -d kacho_iam\n"
	twin := "wipe-db:\n\tkubectl exec -- psql -U iam -d kaname\n"

	got := injStandAudit(t, defect, injStandTable())
	require.NotEmpty(t, got, "имя базы, которого не объявляет ни один подчарт, обязано быть находкой")
	require.Contains(t, strings.Join(got, "\n"), "стучимся в никуда")

	require.Empty(t, injStandAudit(t, twin, injStandTable()),
		"то же имя цели с ОБЪЯВЛЕННОЙ базой обязано молчать — различие ровно в одном факте")
}

func TestStandPsqlInjection_ProseAboutPsqlIsNotARecipe(t *testing.T) {
	// Распознаватель обязан судить ИСПОЛНЯЕМУЮ часть: строка комментария,
	// объясняющая сам предмет, рецептом не является — иначе гейт краснел бы на
	// собственном объяснении.
	prose := "# psql -U $(SVC) -d kacho_$(SVC) — так БЫЛО, и это находка\n" +
		"psql:\n\tkubectl exec -- psql -U $(PSQL_USER_$(SVC)) -d $(PSQL_DB_$(SVC))\n"
	require.Empty(t, injStandAudit(t, prose, injStandTable()),
		"проза о выведении выведением не является")
}

func TestStandPsqlInjection_EmptyTraversalIsNotSilence(t *testing.T) {
	// Ноль рецептов — это «ноль прочитанного», а не «ноль находок». Перепись
	// обязана это показать: на ней стоит отказ самого гейта.
	recipes, table, lines := parseStandPsqlRecipes("all:\n\techo hi\n")
	findings, census := auditStandPsql(recipes, table, injStandDecls(), lines)
	require.Empty(t, findings)
	require.Zero(t, census.recipes,
		"обращений к psql нет — гейт обязан объявить обход пустым, а не вердикт зелёным")
	require.Zero(t, census.pairsAgreed,
		"положительный контроль на пустом обходе обязан быть пуст")
}
