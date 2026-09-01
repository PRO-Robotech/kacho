// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package check

// catalog_seed_parity_test.go — гейты каталога модуля (kacho#1030, приёмка
// `services/iam/docs/engineering/acceptance/rule-segments-have-a-referent.md`,
// требования Т1, Т2, Т6).
//
// # Почему гейт дерева, а не проба сервиса
//
// «Посев согласен с литералом» — свойство ДЕРЕВА, и проба сервиса о нём не
// утверждает ничего: она читает базу, в которую миграция уже применена, и
// зеленеет при любом литерале. Литерал же правится свободно, а применённая
// миграция правке не подлежит (запрет #5) — значит расхождение неизбежно, и
// вопрос не «как его не допустить», а «как сделать его ВИДИМЫМ».
//
// Проверок здесь ДВЕ, и предметы у них разные: паритет посева и форма ключа.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/PRO-Robotech/kacho/services/iam/internal/authzmap"
	"github.com/PRO-Robotech/kacho/services/iam/internal/domain"
)

// catalogRepoRoot — корень модуля. Своя копия обхода вверх не заводится: рядом
// уже есть `monorepoRoot`, но он живёт во внешнем тестовом пакете
// (`check_test`), а этот гейт — во внутреннем, потому что зовёт неэкспортируемое
// ядро. Это единственная причина, по которой обход здесь повторён.
func catalogRepoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("рабочий каталог: %v", err)
	}
	dir := wd
	for {
		if _, serr := os.Stat(filepath.Join(dir, "go.mod")); serr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("корень модуля (go.mod) не найден от %s", wd)
		}
		dir = parent
	}
}

// catalogMigrationPath — единственная миграция, сеющая каталог модуля.
const catalogMigrationPath = "services/iam/internal/migrations/20260901113757_rule_segments_have_a_referent.sql"

// literalCatalog — перечень, ВЫВЕДЕННЫЙ единственным производителем
// (`authzmap.CatalogSeed*`), а не выписанный здесь. Второй производитель того же
// перечня разошёлся бы с первым молча — ровно в тот момент, когда расхождение и
// опасно.
func literalCatalog() (modules, resources, verbs []string) {
	modules = domain.KnownModules()
	for _, r := range authzmap.CatalogSeedResources() {
		resources = append(resources, r.Dotted)
	}
	for _, v := range authzmap.CatalogSeedVerbs() {
		verbs = append(verbs, v.Module+"."+v.Resource+"."+v.Verb)
	}
	return modules, resources, verbs
}

// TestIAMCT114_CatalogSeedMatchesTheLiteral — Т6.
func TestIAMCT114_CatalogSeedMatchesTheLiteral(t *testing.T) {
	root := catalogRepoRoot(t)
	body, err := os.ReadFile(filepath.Join(root, catalogMigrationPath))
	if err != nil {
		t.Fatalf("прочитать миграцию каталога: %v", err)
	}

	mods, res, verbs := literalCatalog()
	if len(mods) == 0 || len(res) == 0 || len(verbs) == 0 {
		t.Fatalf("литерал пуст (модулей %d, ресурсов %d, глаголов %d) — "+
			"сверять было бы не с чем, и «расхождений нет» означало бы «ничего не прочитано»",
			len(mods), len(res), len(verbs))
	}

	c, findings, aerr := auditCatalogSeed(string(body), mods, res, verbs)
	if aerr != nil {
		t.Fatalf("разобрать посев: %v", aerr)
	}
	t.Logf("осмотрено: литерал — модулей %d, ресурсов %d, глаголов %d; "+
		"посев — модулей %d, ресурсов %d, глаголов %d, снятых строк %d",
		len(mods), len(res), len(verbs),
		c.SeededModules, c.SeededResources, c.SeededVerbs, c.RetiredSeeded)

	if c.RetiredSeeded == 0 {
		t.Error("снятых строк посеяно ноль: снятие выражено запретительным списком в Go, " +
			"и перенос его строками — половина предмета #1030")
	}
	for _, f := range findings {
		t.Error(f)
	}
}

// TestIAMCT113_CatalogKeysCarryTheDeclaredForm — Т1 и Т2.
func TestIAMCT113_CatalogKeysCarryTheDeclaredForm(t *testing.T) {
	root := catalogRepoRoot(t)
	body, err := os.ReadFile(filepath.Join(root, catalogMigrationPath))
	if err != nil {
		t.Fatalf("прочитать миграцию каталога: %v", err)
	}

	immediateOnly := []string{"role_rule_ref_res_fk", "role_rule_ref_verb_fk", "role_verb_type_fk"}
	scanned, findings := auditKeyForm(string(body), immediateOnly)
	t.Logf("осмотрено объявлений ключа: %d; проверено на немедленность: %d",
		scanned, len(immediateOnly))
	if scanned == 0 {
		t.Fatal("объявлений ключа не прочитано ни одного — обход пуст, вердикт беспредметен")
	}
	for _, name := range immediateOnly {
		if !containsDeclaration(string(body), name) {
			t.Errorf("ключ %s не объявлен миграцией: гейт судил бы имя, которого в дереве нет", name)
		}
	}
	for _, f := range findings {
		t.Error(f)
	}
}

func containsDeclaration(body, name string) bool {
	return len(body) > 0 && len(name) > 0 &&
		indexOf(stripSQLComments(body), "ADD CONSTRAINT "+name) >= 0
}

func indexOf(hay, needle string) int {
	for i := 0; i+len(needle) <= len(hay); i++ {
		if hay[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
