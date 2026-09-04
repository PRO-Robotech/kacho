// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// membershipreadrelation_injection_test.go — доказательство того, что гейт
// IAM-ID-2-06 способен упасть, и падает на предмете.
//
// Три возврата дефекта, каждый настоящим входом, и законный близнец к каждому.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// mrrTree — синтетическое дерево: каталог прав и модель.
func mrrTree(t *testing.T, catalog, model string) *treecorpus.Tree {
	t.Helper()
	root := t.TempDir()
	cdir := filepath.Join(root, filepath.FromSlash(filepath.Dir(mrrCatalog)))
	mdir := filepath.Join(root, filepath.FromSlash(filepath.Dir(mrrModel)))
	for _, d := range []string{cdir, mdir} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatalf("каталог: %v", err)
		}
	}
	write(t, filepath.Join(root, filepath.FromSlash(mrrCatalog)), catalog)
	write(t, filepath.Join(root, filepath.FromSlash(mrrModel)), model)
	tree, err := treecorpus.SyntheticTree(root)
	if err != nil {
		t.Fatalf("состав синтетического дерева: %v", err)
	}
	return tree
}

// mrrModelLawful — модель, устроенная как настоящая: ярусные отношения выводятся
// друг из друга, глагольные — НЕТ.
const mrrModelLawful = `
type cluster
  relations
    define system_admin: [user, service_account]

type account
  relations
    define owner: [user]
    define admin: [user, service_account, group#member] or owner
    define editor: [user, service_account, group#member] or admin
    define viewer: [user, service_account, group#member] or editor
    define v_get: [user, service_account, group#member] or owner
    define v_list: [user, service_account, group#member] or owner
`

func mrrCatalogWith(rel string, scopeFiltered bool) string {
	sf := ""
	if scopeFiltered {
		sf = `,"scope_filtered":true`
	}
	row := func(m string) string {
		return `{"fqn":"kacho.cloud.iam.v1.MembershipService/` + m + `",` +
			`"permission":"iam.memberships.` + strings.ToLower(m) + `",` +
			`"required_relation":"` + rel + `",` +
			`"scope_extractor":{"object_type":"account","from_request_field":"account_id"}` + sf + `}`
	}
	// Соседняя запись — законный близнец: чтение САМОГО объекта аккаунта
	// глагольным отношением. Гейт её не судит, и это его названная граница.
	twin := `{"fqn":"kacho.cloud.iam.v1.AccountService/Get","permission":"iam.accounts.get",` +
		`"required_relation":"v_get",` +
		`"scope_extractor":{"object_type":"account","from_request_field":"account_id"}}`
	return "[" + row("Get") + "," + row("List") + "," + twin + "]"
}

func mrrFindings(t *testing.T, catalog, model string) []string {
	t.Helper()
	c, err := SurveyMembershipReadRelation(mrrTree(t, catalog, model))
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	return c.Findings
}

// TestMRR_SilentOnTheLawfulDeclaration — КОНТРОЛЬ. Без него всякое красное ниже
// доказывало бы лишь то, что гейт краснеет всегда.
func TestMRR_SilentOnTheLawfulDeclaration(t *testing.T) {
	got := mrrFindings(t, mrrCatalogWith("viewer", false), mrrModelLawful)
	if len(got) != 0 {
		t.Fatalf("гейт краснеет на законном объявлении: %v", got)
	}
}

// TestMRR_RedWhenTheEntryIsMissing — первый возврат: запись каталога снята.
func TestMRR_RedWhenTheEntryIsMissing(t *testing.T) {
	only := `[{"fqn":"kacho.cloud.iam.v1.MembershipService/Get","permission":"p",` +
		`"required_relation":"viewer",` +
		`"scope_extractor":{"object_type":"account","from_request_field":"account_id"}}]`
	got := mrrFindings(t, only, mrrModelLawful)
	if !mrrNames(got, "MembershipService/List") {
		t.Fatalf("гейт не назвал КООРДИНАТУ RPC, оставшегося без записи каталога: %v", got)
	}
}

// TestMRR_RedOnAWildcardSatisfiableRelation — второй возврат: отношение,
// выполнимое подстановочным кортежем.
func TestMRR_RedOnAWildcardSatisfiableRelation(t *testing.T) {
	model := strings.Replace(mrrModelLawful,
		"define viewer: [user, service_account, group#member] or editor",
		"define viewer: [user, user:*, service_account] or editor", 1)
	got := mrrFindings(t, mrrCatalogWith("viewer", false), model)
	if !mrrNames(got, "MembershipService/Get") || !strings.Contains(strings.Join(got, " "), "user:*") {
		t.Fatalf("гейт не заметил отношение, отвечающее «да» каждому аутентифицированному: %v", got)
	}
}

// TestMRR_RedWhenTheVerbRelationIsSubstituted — ТРЕТИЙ возврат, и он
// обязателен: подмена выглядит уместной (глагольное отношение на глагольном
// чтении), проходит все прочие проверки каталога и ломает ровно адресата.
func TestMRR_RedWhenTheVerbRelationIsSubstituted(t *testing.T) {
	got := mrrFindings(t, mrrCatalogWith("v_list", false), mrrModelLawful)
	joined := strings.Join(got, " ")
	if !mrrNames(got, "MembershipService/List") || !strings.Contains(joined, "v_list") {
		t.Fatalf("гейт не назвал ОТНОШЕНИЕ при подмене ярусного на глагольное: %v", got)
	}
	if !strings.Contains(joined, "распорядител") {
		t.Fatalf("отказ не называет, ЧТО именно ломается подменой: %v", got)
	}
}

// TestMRR_RedOnDataNarrowingLane — полоса сужения на данных у этих чтений
// объявлена быть не может.
func TestMRR_RedOnDataNarrowingLane(t *testing.T) {
	got := mrrFindings(t, mrrCatalogWith("viewer", true), mrrModelLawful)
	if len(got) == 0 {
		t.Fatal("гейт принял полосу сужения на данных у аккаунт-скоупного чтения")
	}
}

// TestMRR_LegalTwinIsNotJudged — законный близнец: чтение САМОГО объекта
// аккаунта глагольным отношением находкой не становится.
//
// Утверждается ПРЯМО, а не выводится из молчания контроля: молчание могло бы
// означать и «граница предмета проведена», и «гейт вообще ничего не читает».
func TestMRR_LegalTwinIsNotJudged(t *testing.T) {
	c, err := SurveyMembershipReadRelation(mrrTree(t, mrrCatalogWith("viewer", false), mrrModelLawful))
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if c.CatalogRows != 3 {
		t.Fatalf("гейт прочитал %d строк каталога вместо 3 — близнец до него не дошёл, "+
			"и его молчание ничего не доказывает", c.CatalogRows)
	}
	for _, f := range c.Findings {
		if strings.Contains(f, "AccountService/Get") {
			t.Fatalf("гейт судит чтение самого объекта аккаунта: %s — там глагольное "+
				"отношение выбрано осознанно, и это его названная граница", f)
		}
	}
}

// TestMRR_ComparisonOfTwoDeclarationsIsLive — если глагольные отношения начнут
// читать ярус, сравнение перестанет различать, и гейт обязан это сказать.
func TestMRR_ComparisonOfTwoDeclarationsIsLive(t *testing.T) {
	c, err := SurveyMembershipReadRelation(mrrTree(t, mrrCatalogWith("viewer", false), mrrModelLawful))
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if !c.TierReaders["viewer"] {
		t.Fatal("КОНТРОЛЬ: ярусное отношение обязано читать ярус на законной модели")
	}
	for _, verb := range c.VerbRelations {
		if c.TierReaders[verb] {
			t.Fatalf("КОНТРОЛЬ: глагольное %q не должно читать ярус на законной модели", verb)
		}
	}
	model := strings.Replace(mrrModelLawful,
		"define v_list: [user, service_account, group#member] or owner",
		"define v_list: [user, service_account, group#member] or admin", 1)
	c2, err := SurveyMembershipReadRelation(mrrTree(t, mrrCatalogWith("viewer", false), model))
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if !c2.TierReaders["v_list"] {
		t.Fatal("глагольное отношение стало читать ярус, а гейт этого не заметил — " +
			"сравнение двух объявлений мертво, и выбор им больше не доказывается")
	}
}

func mrrNames(findings []string, want string) bool {
	for _, f := range findings {
		if strings.Contains(f, want) {
			return true
		}
	}
	return false
}
