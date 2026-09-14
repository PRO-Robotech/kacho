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

	"github.com/PRO-Robotech/corelib/treecorpus"
)

// oracleScopeFilteredLine — строка объявления пообъектного сужения для глагола
// фикстуры.
//
// Формы три, и все три нужны: объявление (законный близнец), его отсутствие
// (инъекция) и объявление, стоящее ТОЛЬКО прозой (контроль п.4 — гейт обязан
// судить исполняемую часть, а не слово).
func oracleScopeFilteredLine(f oracleFixture, fqn string) string {
	if f.scopeFilteredAsProse == fqn {
		return "    // option (corelib.authz.v1.scope_filtered) = true;\n"
	}
	if f.dropScopeFilteredOn == fqn {
		return ""
	}
	return "    option (corelib.authz.v1.scope_filtered) = true;\n"
}

// oracleFixture — состав синтетического дерева одной пробы.
type oracleFixture struct {
	// protoExtra — дополнительный файл контракта.
	protoExtra string
	// userExtraFields — поля, добавленные ресурсу человека.
	userExtraFields string
	// dropScopeFilteredOn — снять объявление пообъектного сужения у названного
	// глагола контракта (форма `Service/Method`). Так снимается ДОКАЗАТЕЛЬСТВО
	// гасящей записи — по одному глаголу зараз.
	dropScopeFilteredOn string
	// scopeFilteredAsProse — объявить сужение ТОЛЬКО комментарием внутри тела
	// глагола. Контроль п.4: гейт, читающий слово, зачёл бы собственное
	// объяснение за исполнение.
	scopeFilteredAsProse string
	// dropIDMark — снять предпосылку полосы C.
	dropIDMark bool
	// idMigrationName — имя файла КОРПУСА КОНТРАКТА, в котором стоит объявление
	// деривации.
	//
	// Параметр, а не константа: предпосылка ищется по КОРПУСУ, и привязка к
	// имени файла её уже однажды убила — свод миграций службы доступа
	// (2026-09-04) снял файл, где деривация была заведена, при том что само
	// выражение переехало в свод байт-в-байт. Пустое значение — имя по умолчанию.
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
message ListBySubjectRequest {
  string user_id = 1;
}
message ListBySubjectResponse {
  repeated Membership memberships = 1;
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

service AccessBindingService {
  rpc ListBySubject (ListBySubjectRequest) returns (ListBySubjectResponse) {
    option (google.api.http) = { get: "/iam/v1/accessBindings:listBySubject" };
` + oracleScopeFilteredLine(f, "AccessBindingService/ListBySubject") + `  }
  rpc ListSubjectPrivileges (ListBySubjectRequest) returns (ListBySubjectResponse) {
    option (google.api.http) = { get: "/iam/v1/accessBindings:listSubjectPrivileges" };
` + oracleScopeFilteredLine(f, "AccessBindingService/ListSubjectPrivileges") + `  }
}
`
	write(t, filepath.Join(protoDir, "base.proto"), base)
	if f.protoExtra != "" {
		write(t, filepath.Join(protoDir, "extra.proto"), f.protoExtra)
	}

	// Фикстуры белых списков фильтра здесь БЫЛИ и сняты вместе с полосой B:
	// её предметом были списки, объявленные в прод-коде службы доступа, а служба
	// вынесена отдельным продуктом.

	// Предпосылка полосы C живёт в КОНТРАКТЕ: реализация вынесена отдельным
	// продуктом, и схемы, которую можно было бы прочесть, в этом дереве нет.
	//
	// Файл ОТДЕЛЬНЫЙ, а не приписка к базовому: предпосылка ищется по КОРПУСУ, и
	// ось «та же фраза в файле с другим именем» ниже стережёт ровно это.
	mark := oracleMembershipIDMark
	if f.dropIDMark {
		mark = "предпосылка снята"
	}
	idFile := f.idMigrationName
	if idFile == "" {
		idFile = "membership_service.proto"
	}
	write(t, filepath.Join(protoDir, idFile),
		"syntax = \"proto3\";\npackage kaname.cloud.iam.v1;\n\n// Идентификатор членства "+mark+"\n")

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
	t.Parallel()
	got := oracleLanes(t, oracleFixture{})
	if len(got) != 0 {
		t.Fatalf("гейт краснеет на законной поверхности: %v — все три близнеца "+
			"обязаны молчать (аккаунт обязателен · ответ аккаунта не называет · "+
			"вход человека не называет)", got)
	}
}

// TestOracleGate_LaneA_SubjectFieldOnAnUnscopedRead — инъекция A.
func TestOracleGate_LaneA_SubjectFieldOnAnUnscopedRead(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	got := oracleLanes(t, oracleFixture{userExtraFields: "  string account_id = 3;\n"})
	if got["UserService/Get"] != "A" {
		t.Fatalf("полоса A не заметила возвращённое поле аккаунта на ресурсе человека: %v", got)
	}
}

// TestOracleGate_LaneA_MembershipsFieldOnTheUserResource — инъекция A-ter.
//
// Ею полоса A закрывает форму «перечень членств вместе с человеком».
func TestOracleGate_LaneA_MembershipsFieldOnTheUserResource(t *testing.T) {
	t.Parallel()
	got := oracleLanes(t, oracleFixture{userExtraFields: "  repeated Membership memberships = 3;\n"})
	if got["UserService/Get"] != "A" {
		t.Fatalf("полоса A не заметила поле членств на ресурсе человека: %v", got)
	}
}

// ЗДЕСЬ БЫЛИ ДВЕ ПРОБЫ ПОЛОСЫ B — инъекция терма субъекта в белый список
// чтения без обязательного аккаунта и её законный близнец. Обе сняты вместе с
// полосой: её предметом были белые списки фильтра, объявленные в прод-коде
// службы доступа, а служба вынесена отдельным продуктом. Довод, по которому
// корень полосы нельзя было расширить на живые домены, — в шапке
// `membershiporacle.go`.

// TestOracleGate_LaneC_FlatMembershipRead — инъекция C. ОБЯЗАТЕЛЬНА: без неё
// полоса C остаётся объявлением, а гейт зелен ровно на том входе, ради которого
// заведён.
func TestOracleGate_LaneC_FlatMembershipRead(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	// 2026-09-04: свод миграций службы доступа снял файл, в котором деривация
	// была заведена, — а само выражение переехало в свод байт-в-байт. Гейт
	// объявил предпосылку ложной, будучи неправ: он пережил не факт, а раскладку
	// файлов.
	//
	// Производитель с тех пор сменился на контракт (реализация уехала отдельным
	// продуктом), но класс ошибки от этого не изменился: объявление переезжает
	// между файлами корпуса при каждом переустройстве контракта. Ось стережёт
	// ровно это.
	c3, err := SurveyMembershipOracle(oracleInjectionTree(t, oracleFixture{
		idMigrationName: "membership.proto",
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
//
// Доказательство читается в КОНТРАКТЕ (объявление `scope_filtered` в теле
// глагола), потому что реализация службы доступа живёт в другом продукте, а
// контракт правится здесь. Снятие идёт ПО ОДНОМУ глаголу: гейт, замечающий
// только пропажу первой записи, неотличим от исправного, пока записей не станет
// две.
func TestOracleGate_QuenchEntryExpiresWithItsProof(t *testing.T) {
	t.Parallel()
	proofs := SurveyOracleQuenchProofs(oracleInjectionTree(t, oracleFixture{}))
	if len(proofs) == 0 {
		t.Fatal("гасящих записей нет — проба рассматривает пустоту")
	}
	for _, p := range proofs {
		if !p.Found {
			t.Fatalf("КОНТРОЛЬ: доказательство %s обязано находиться на законном дереве", p.FQN)
		}
	}

	for _, q := range oracleQuenchedByNarrowing {
		tree := oracleInjectionTree(t, oracleFixture{dropScopeFilteredOn: q.FQN})
		expired := false
		for _, p := range SurveyOracleQuenchProofs(tree) {
			if p.FQN == q.FQN && !p.Found {
				expired = true
			}
			if p.FQN != q.FQN && !p.Found {
				t.Fatalf("сужение снято у %s, а основание потеряла ЧУЖАЯ запись %s — "+
					"доказательства перепутаны местами", q.FQN, p.FQN)
			}
		}
		if !expired {
			t.Fatalf("объявление сужения снято, а гасящая запись %s всё ещё считает себя "+
				"доказанной — близнец пережил своё основание", q.FQN)
		}
	}
}

// TestOracleGate_QuenchedReadBecomesAFindingWhenTheProofGoes — вторая половина
// того же: потеряв доказательство, чтение обязано СТАТЬ НАХОДКОЙ, а не просто
// «незадоказанной записью».
//
// Без неё самоистечение доказывало бы лишь то, что перепись изменилась, — а
// предмет гейта в том, ЧТО он говорит о поверхности.
func TestOracleGate_QuenchedReadBecomesAFindingWhenTheProofGoes(t *testing.T) {
	t.Parallel()
	target := oracleQuenchedByNarrowing[0].FQN

	base := oracleLanes(t, oracleFixture{})
	if lane, ok := base[target]; ok {
		t.Fatalf("КОНТРОЛЬ: чтение %s объявлено находкой полосы %q при живом "+
			"объявлении сужения — гашение не действует", target, lane)
	}

	got := oracleLanes(t, oracleFixture{dropScopeFilteredOn: target})
	if got[target] != "A" {
		t.Fatalf("объявление сужения снято, а чтение %s находкой полосы A не стало: %v — "+
			"гашение держится ведомостью, а не доказательством", target, got)
	}
}

// TestOracleGate_QuenchProofIsNotSatisfiedByProse — КОНТРОЛЬ обратной стороны:
// объявление сужения, стоящее ТОЛЬКО комментарием, доказательством не является.
//
// Тела обоих глаголов несут развёрнутый разбор сужения прозой и называют в нём
// то же имя опции. Поиск по слову нашёл бы этот разбор и остался бы зелёным при
// снятом объявлении — гейт удостоверял бы собственное объяснение
// (`testing.md` §«Гейт на класс», п.4).
func TestOracleGate_QuenchProofIsNotSatisfiedByProse(t *testing.T) {
	t.Parallel()
	target := oracleQuenchedByNarrowing[0].FQN
	tree := oracleInjectionTree(t, oracleFixture{scopeFilteredAsProse: target})
	for _, p := range SurveyOracleQuenchProofs(tree) {
		if p.FQN == target && p.Found {
			t.Fatalf("проза о сужении зачтена за сужение: запись %s считает себя "+
				"доказанной комментарием, в котором названо имя опции", p.FQN)
		}
	}
}

// TestOracleGate_CensusIsNotVacuous — перепись обязана быть НЕПУСТОЙ, иначе
// «ноль находок» означает «ноль прочитанного».
func TestOracleGate_CensusIsNotVacuous(t *testing.T) {
	t.Parallel()
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
	}
	if !strings.Contains(strings.Join(c.Dictionary, ","), "account_id") {
		t.Fatal("словарь условия «б» пуст либо потерял несущее имя")
	}
}
