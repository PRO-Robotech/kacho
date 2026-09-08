// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// membershiporacle_injection_test.go — доказательство того, что гейт трёх полос
// СПОСОБЕН упасть, и падает на предмете, а не на форме.
//
// Инъекция идёт ПО КАЖДОЙ ПОЛОСЕ и НАСТОЯЩИМ входом — той конструкцией, которую
// в дерево и внесли бы. Рядом с каждой стоит ЗАКОННЫЙ БЛИЗНЕЦ той же формы, на
// котором гейт обязан молчать: без него гейт ловит форму, а не существо, и
// первый же ложный срабат его отключит.
//
// Дерево синтетическое, поэтому проба детерминирована и не поплывёт от
// следующего контракта продукта. Настоящее дерево остаётся предметом самого
// гейта.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// oracleFixture — состав синтетического дерева одной пробы.
type oracleFixture struct {
	// protoExtra — дополнительный файл контракта.
	protoExtra string
	// userExtraFields — поля, добавленные ресурсу человека.
	userExtraFields string
	// whitelistTerms — термы белого списка списочного чтения БЕЗ обязательного
	// аккаунта (ресурс `widget`).
	whitelistTerms string
	// dropIDMark — снять предпосылку полосы C.
	dropIDMark bool
	// idMigrationName — имя файла корпуса, в котором стоит деривация.
	//
	// Параметр, а не константа: предпосылка ищется по КОРПУСУ, и привязка к
	// имени файла её уже однажды убила — свод миграций iam (2026-09-04) снял
	// файл, в котором деривация была заведена, при том что само выражение
	// переехало в свод байт-в-байт. Пустое значение — имя по умолчанию.
	idMigrationName string
}

// oracleInjectionTree собирает дерево: базовая поверхность плюс правка.
//
// БАЗА — это законные близнецы всех трёх полос сразу:
//   - `MembershipService` — аккаунт-скоупные список и одиночное чтение (гасят «в»,
//     и они же близнецы полос B и C);
//   - `UserService/Get` — вход называет человека, ответ аккаунта НЕ называет (гасит «б»);
//   - `WidgetService/List` — список без обязательного аккаунта, чей ответ аккаунт
//     называет, но человека вход НЕ называет (гасит «а»).
func oracleInjectionTree(t *testing.T, f oracleFixture) *treecorpus.Tree {
	t.Helper()
	root := t.TempDir()

	protoDir := filepath.Join(root, filepath.FromSlash(oracleProtoDir))
	if err := os.MkdirAll(protoDir, 0o750); err != nil {
		t.Fatalf("каталог контрактов: %v", err)
	}
	// Поля стоят ПО ОДНОМУ НА СТРОКУ — как в настоящих контрактах. Фикстура,
	// написанная в одну строку, не воспроизводит вход, который распознаватель
	// читает: инъекция на ней молчала бы, и молчание это доказывало бы не
	// исправность гейта, а непохожесть фикстуры.
	base := `
message Membership {
  string id = 1;
  string account_id = 2;
  string user_id = 3;
}
message GetMembershipRequest {
  string account_id = 1;
  string membership_id = 2;
}
message ListMembershipsRequest {
  string account_id = 1;
  string filter = 2;
}
message ListMembershipsResponse {
  repeated Membership memberships = 1;
}
message GetUserRequest {
  string user_id = 1;
}
message User {
  string id = 1;
  string email = 2;
` + f.userExtraFields + `}
message ListWidgetsRequest {
  string page_token = 1;
}
message Widget {
  string id = 1;
  string account_id = 2;
}
message ListWidgetsResponse {
  repeated Widget widgets = 1;
}

service MembershipService {
  rpc Get (GetMembershipRequest) returns (Membership) {
    option (google.api.http) = { get: "/iam/v1/accounts/{account_id}/memberships/{membership_id}" };
  }
  rpc List (ListMembershipsRequest) returns (ListMembershipsResponse) {
    option (google.api.http) = { get: "/iam/v1/accounts/{account_id}/memberships" };
  }
}

service UserService {
  rpc Get (GetUserRequest) returns (User) {
    option (google.api.http) = { get: "/iam/v1/users/{user_id}" };
  }
}

service WidgetService {
  rpc List (ListWidgetsRequest) returns (ListWidgetsResponse) {
    option (google.api.http) = { get: "/iam/v1/widgets" };
  }
}
`
	write(t, filepath.Join(protoDir, "base.proto"), base)
	if f.protoExtra != "" {
		write(t, filepath.Join(protoDir, "extra.proto"), f.protoExtra)
	}

	// Белые списки: членства (законный — аккаунт обязателен) и `widget`.
	mdir := filepath.Join(root, "services", "iam", "internal", "repo", "kaname", "membership")
	if err := os.MkdirAll(mdir, 0o750); err != nil {
		t.Fatalf("каталог порта: %v", err)
	}
	write(t, filepath.Join(mdir, "filter.go"),
		"package membership\n\nconst FilterFieldUserID = \"userId\"\n\n"+
			"func ParseListFilter(e string) { filter.Parse(e, []string{FilterFieldUserID}) }\n")

	wdir := filepath.Join(root, "services", "iam", "internal", "repo", "kaname", "pg")
	if err := os.MkdirAll(wdir, 0o750); err != nil {
		t.Fatalf("каталог репозитория: %v", err)
	}
	terms := f.whitelistTerms
	if terms == "" {
		terms = `"name"`
	}
	write(t, filepath.Join(wdir, "widget_repo.go"),
		"package pg\n\nfunc list(f F) { parseListFilter(f.Filter, "+terms+") }\n")

	// Доказательство гасящей записи условия «г».
	adir := filepath.Join(root, "services", "iam", "internal", "apps", "kaname", "api", "access_binding")
	if err := os.MkdirAll(adir, 0o750); err != nil {
		t.Fatalf("каталог use-case: %v", err)
	}
	// Доказательство читается РАЗБОРОМ, поэтому фикстура несёт настоящий ВЫЗОВ, а
	// не строку с его именем: файл, где имя стоит только в комментарии, — предмет
	// отдельного контроля ниже.
	quench := "package access_binding\n\nfunc e() { visibleOnNarrowedPage(nil, nil, nil) }\n"
	write(t, filepath.Join(adir, "list_by_subject.go"), quench)
	write(t, filepath.Join(adir, "list_subject_privileges.go"), quench)

	// Предпосылка полосы C.
	migDir := filepath.Join(root, "services", "iam", "internal", "migrations")
	if err := os.MkdirAll(migDir, 0o750); err != nil {
		t.Fatalf("каталог миграций: %v", err)
	}
	mark := oracleMembershipIDMark
	if f.dropIDMark {
		mark = "-- предпосылка снята"
	}
	idFile := f.idMigrationName
	if idFile == "" {
		idFile = "470001_memberships_expand.sql"
	}
	write(t, filepath.Join(migDir, idFile), mark+" || p_user_id)\n")

	tree, err := treecorpus.SyntheticTree(root)
	if err != nil {
		t.Fatalf("состав синтетического дерева: %v", err)
	}
	return tree
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("пишу %s: %v", path, err)
	}
}

// oracleLanes — полосы находок по именам, чтобы утверждать ИМЯ, а не счётчик:
// гейт, краснеющий не на том, счётчиком неотличим от исправного.
func oracleLanes(t *testing.T, f oracleFixture) map[string]string {
	t.Helper()
	c, err := SurveyMembershipOracle(oracleInjectionTree(t, f))
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	out := map[string]string{}
	for _, fd := range c.Findings {
		out[fd.FQN] = fd.Lane
	}
	return out
}

// TestOracleGate_SilentOnTheLawfulSurface — КОНТРОЛЬ. Без него всякое красное
// ниже доказывало бы лишь то, что гейт краснеет всегда.
func TestOracleGate_SilentOnTheLawfulSurface(t *testing.T) {
	got := oracleLanes(t, oracleFixture{})
	if len(got) != 0 {
		t.Fatalf("гейт краснеет на законной поверхности: %v — все три близнеца "+
			"обязаны молчать (аккаунт обязателен · ответ аккаунта не называет · "+
			"вход человека не называет)", got)
	}
}

// TestOracleGate_LaneA_SubjectFieldOnAnUnscopedRead — инъекция A.
func TestOracleGate_LaneA_SubjectFieldOnAnUnscopedRead(t *testing.T) {
	// Списочному чтению БЕЗ обязательного аккаунта, чей ответ аккаунт называет,
	// добавлено поле субъекта — ровно тот вход, который и заводят.
	got := oracleLanes(t, oracleFixture{protoExtra: `
message ListWidgetsBySubjectRequest {
  string user_id = 1;
}
service WidgetBySubjectService {
  rpc List (ListWidgetsBySubjectRequest) returns (ListWidgetsResponse) {
    option (google.api.http) = { get: "/iam/v1/widgets:bySubject" };
  }
}
`})
	if got["WidgetBySubjectService/List"] != "A" {
		t.Fatalf("полоса A НЕ назвала координату чтения, отвечающего на запретный вопрос: %v", got)
	}
}

// TestOracleGate_LaneA_AccountFieldBackOnTheUserResource — инъекция A-bis.
//
// Ею полоса A становится МАШИННЫМ исполнителем границы объёма: «возврат поля
// аккаунта на ресурс пользователя» перестаёт держаться обещанием.
func TestOracleGate_LaneA_AccountFieldBackOnTheUserResource(t *testing.T) {
	got := oracleLanes(t, oracleFixture{userExtraFields: "  string account_id = 3;\n"})
	if got["UserService/Get"] != "A" {
		t.Fatalf("полоса A не заметила возвращённое поле аккаунта на ресурсе человека: %v", got)
	}
}

// TestOracleGate_LaneA_MembershipsFieldOnTheUserResource — инъекция A-ter.
//
// Ею полоса A закрывает форму «перечень членств вместе с человеком».
func TestOracleGate_LaneA_MembershipsFieldOnTheUserResource(t *testing.T) {
	got := oracleLanes(t, oracleFixture{userExtraFields: "  repeated Membership memberships = 3;\n"})
	if got["UserService/Get"] != "A" {
		t.Fatalf("полоса A не заметила поле членств на ресурсе человека: %v", got)
	}
}

// TestOracleGate_LaneB_SubjectTermInAnUnscopedWhitelist — инъекция B.
func TestOracleGate_LaneB_SubjectTermInAnUnscopedWhitelist(t *testing.T) {
	got := oracleLanes(t, oracleFixture{whitelistTerms: `"name", "userId"`})
	if got["WidgetService/List"] != "B" {
		t.Fatalf("полоса B не заметила терм субъекта в белом списке чтения без "+
			"обязательного аккаунта: %v", got)
	}
}

// TestOracleGate_LaneB_SubjectTermStaysLawfulWhenAccountIsMandatory — законный
// близнец полосы B: тот же терм на аккаунт-скоупном чтении находкой НЕ является,
// потому что действует ВНУТРИ названного аккаунта.
//
// Он стоит в БАЗЕ каждой пробы (белый список членства), поэтому здесь
// утверждается прямо: контроль выше молчит именно из-за него, а не потому, что
// полоса B ничего не читает.
func TestOracleGate_LaneB_SubjectTermStaysLawfulWhenAccountIsMandatory(t *testing.T) {
	c, err := SurveyMembershipOracle(oracleInjectionTree(t, oracleFixture{}))
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	seen := false
	for _, w := range c.Whitelists {
		for _, term := range w.Terms {
			if term == "userId" && w.Bound == "MembershipService/List" {
				seen = true
			}
		}
	}
	if !seen {
		t.Fatal("полоса B не ВИДИТ терм субъекта, объявленный КОНСТАНТОЙ, — значит её " +
			"молчание ничего не доказывает: всё, записанное этой формой, лежит вне " +
			"наблюдения, а не признано законным")
	}
	for _, f := range c.Findings {
		if f.Lane == "B" {
			t.Fatalf("полоса B краснеет на законном близнеце: %s — терм действует "+
				"внутри названного аккаунта", f.FQN)
		}
	}
}

// TestOracleGate_LaneC_FlatMembershipRead — инъекция C. ОБЯЗАТЕЛЬНА: без неё
// полоса C остаётся объявлением, а гейт зелен ровно на том входе, ради которого
// заведён.
func TestOracleGate_LaneC_FlatMembershipRead(t *testing.T) {
	got := oracleLanes(t, oracleFixture{protoExtra: `
message GetFlatMembershipRequest {
  string membership_id = 1;
}
service FlatMembershipService {
  rpc Get (GetFlatMembershipRequest) returns (Membership) {
    option (google.api.http) = { get: "/iam/v1/memberships/{membership_id}" };
  }
}
`})
	if got["FlatMembershipService/Get"] != "C" {
		t.Fatalf("полоса C не заметила плоское чтение членства по одному "+
			"идентификатору: %v", got)
	}
	// И тот же вход НА АККАУНТ-СКОУПНОМ пути находкой не становится — законный
	// близнец полосы C стоит в базе и молчит (проверено контролем выше).
}

// TestOracleGate_LaneC_PremiseIsCheckedNotAssumed — предпосылка полосы C.
//
// Перестанет идентификатор быть вычислимым — запрет обязан быть ПЕРЕСМОТРЕН, а
// не унаследован молча. Гейт обязан это ЗАМЕТИТЬ.
func TestOracleGate_LaneC_PremiseIsCheckedNotAssumed(t *testing.T) {
	c, err := SurveyMembershipOracle(oracleInjectionTree(t, oracleFixture{}))
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if !c.IDComputable {
		t.Fatal("КОНТРОЛЬ: на законном дереве предпосылка обязана быть верна")
	}
	c2, err := SurveyMembershipOracle(oracleInjectionTree(t, oracleFixture{dropIDMark: true}))
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if c2.IDComputable {
		t.Fatal("предпосылка полосы C снята с дерева, а гейт этого НЕ ЗАМЕТИЛ — " +
			"запрет остался бы по наследству, без основания")
	}

	// ТРЕТЬЯ ОСЬ: та же деривация в файле с ДРУГИМ именем.
	//
	// Прежде предпосылка читала один файл по координате, и координата умерла
	// 2026-09-04: свод миграций iam снял файл, в котором деривация была
	// заведена, — а само выражение переехало в свод байт-в-байт. Гейт объявил
	// предпосылку ложной, будучи неправ: он пережил не факт, а раскладку файлов.
	//
	// Применённую миграцию не правят (ban #5), поэтому деривация переезжает при
	// каждом своде и будет переезжать впредь. Ось стережёт ровно это.
	c3, err := SurveyMembershipOracle(oracleInjectionTree(t, oracleFixture{
		idMigrationName: "0001_initial.sql",
	}))
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if !c3.IDComputable {
		t.Fatal("деривация переехала в файл с другим именем, а гейт объявил предпосылку " +
			"ложной — он привязан к КООРДИНАТЕ вместо предмета и переживает не факт, " +
			"а раскладку файлов")
	}
	if c3.IDCorpusFiles == 0 {
		t.Fatal("корпус объявлен непрочитанным при найденной деривации — «читать было " +
			"нечего» и «признака нет» перестали различаться")
	}
}

// TestOracleGate_QuenchEntryExpiresWithItsProof — гасящая запись самоистекает.
func TestOracleGate_QuenchEntryExpiresWithItsProof(t *testing.T) {
	tree := oracleInjectionTree(t, oracleFixture{})
	proofs := SurveyOracleQuenchProofs(tree)
	if len(proofs) == 0 {
		t.Fatal("гасящих записей нет — проба рассматривает пустоту")
	}
	for _, p := range proofs {
		if !p.Found {
			t.Fatalf("КОНТРОЛЬ: доказательство %s обязано находиться на законном дереве", p.FQN)
		}
	}
	// Снимаем сужение — КАЖДАЯ запись обязана потерять основание. Снимается
	// по одной: гейт, замечающий только пропажу первой, неотличим от исправного,
	// пока записей не станет две.
	for _, q := range oracleQuenchedByNarrowing {
		root := tree.Root()
		write(t, filepath.Join(root, filepath.FromSlash(q.File)),
			"package access_binding\n\nfunc e() {}\n")
		tree2, err := treecorpus.SyntheticTree(root)
		if err != nil {
			t.Fatalf("состав дерева: %v", err)
		}
		expired := false
		for _, p := range SurveyOracleQuenchProofs(tree2) {
			if p.FQN == q.FQN && !p.Found {
				expired = true
			}
			if p.FQN != q.FQN && !p.Found {
				t.Fatalf("снят вызов у %s, а основание потеряла ЧУЖАЯ запись %s — "+
					"доказательства перепутаны местами", q.FQN, p.FQN)
			}
		}
		if !expired {
			t.Fatalf("вызов сужения снят, а гасящая запись %s всё ещё считает себя "+
				"доказанной — близнец пережил своё основание", q.FQN)
		}
		// Возвращаем, чтобы следующая итерация судила снятие ОДНОЙ записи.
		write(t, filepath.Join(root, filepath.FromSlash(q.File)),
			"package access_binding\n\nfunc e() { visibleOnNarrowedPage(nil, nil, nil) }\n")
	}
}

// TestOracleGate_QuenchProofIsNotSatisfiedByProse — КОНТРОЛЬ обратной стороны:
// имя сужения, стоящее ТОЛЬКО в комментарии, доказательством не является.
//
// Оба файла, на которые указывают записи, несут развёрнутый разбор сужения
// прозой и называют в нём то же имя. Подстрочный поиск нашёл бы этот разбор и
// остался бы зелёным при снятом сужении — гейт удостоверял бы собственное
// объяснение.
func TestOracleGate_QuenchProofIsNotSatisfiedByProse(t *testing.T) {
	tree := oracleInjectionTree(t, oracleFixture{})
	root := tree.Root()
	target := oracleQuenchedByNarrowing[0]
	write(t, filepath.Join(root, filepath.FromSlash(target.File)),
		"package access_binding\n\n// Страница сужается вызовом visibleOnNarrowedPage.\nfunc e() {}\n")
	tree2, err := treecorpus.SyntheticTree(root)
	if err != nil {
		t.Fatalf("состав дерева: %v", err)
	}
	for _, p := range SurveyOracleQuenchProofs(tree2) {
		if p.FQN == target.FQN && p.Found {
			t.Fatalf("проза о сужении зачтена за сужение: запись %s считает себя "+
				"доказанной комментарием, в котором названо имя вызова", p.FQN)
		}
	}
}

// TestOracleGate_CensusIsNotVacuous — перепись обязана быть НЕПУСТОЙ, иначе
// «ноль находок» означает «ноль прочитанного».
func TestOracleGate_CensusIsNotVacuous(t *testing.T) {
	c, err := SurveyMembershipOracle(oracleInjectionTree(t, oracleFixture{}))
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	switch {
	case c.ProtoFiles == 0:
		t.Fatal("файлов контрактов прочитано ноль")
	case c.Messages == 0:
		t.Fatal("сообщений разобрано ноль")
	case c.PublicReads == 0:
		t.Fatal("публичных чтений распознано ноль")
	case c.LaneBSeen == 0:
		t.Fatal("белых списков рассмотрено ноль")
	}
	if !strings.Contains(strings.Join(c.Dictionary, ","), "account_id") {
		t.Fatal("словарь условия «б» пуст либо потерял несущее имя")
	}
}
