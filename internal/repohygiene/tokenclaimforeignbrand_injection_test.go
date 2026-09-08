// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// tokenclaimforeignbrand_injection_test.go — доказательство того, что гейт
// клейм СПОСОБЕН упасть, и падает он на существе.
//
// Инъекция гоняет ТЕ ЖЕ функции (ScanTokenClaimNames, DeriveClaimVocabulary,
// FindClaimTwins), что и гейт: доказательство, читающее свою копию разбора,
// доказывает свойство копии.
//
// # Вторая сторона пары здесь тяжелее первой
//
// Приставку словаря носят ещё три вокабуляра — метрики, схемы и типы ресурсов
// модуля инфраструктуры. Все три законны и остаются платформе. Гейт, спутавший
// метрику с клеймом, потребовал бы переименовать то, что переименовывать
// нельзя, — и был бы снят первым же, кто на него наткнётся. Поэтому каждая
// инъекция идёт с ЗАКОННЫМ БЛИЗНЕЦОМ, на котором гейт обязан молчать.
package repohygiene

import (
	"sort"
	"strings"
	"testing"
)

// claimInjMint — место чеканки: семя словаря. Пять клейм своего словаря.
const claimInjMint = `package service

func (s *Svc) userClaims(u user) map[string]any {
	return map[string]any{
		"kaname_user_id":        u.ID,
		"kaname_account_id":     u.AccountID,
		"kaname_principal_type": "user",
		"kaname_audience":       s.cfg.Domain,
		"kaname_issued_at":      s.now().Unix(),
	}
}
`

// claimInjForeignMint — то же место, но одно клеймо осталось в чужом словаре.
// Изменённый факт РОВНО ОДИН: имя одного ключа.
const claimInjForeignMint = `package service

func (s *Svc) userClaims(u user) map[string]any {
	return map[string]any{
		"kaname_user_id":        u.ID,
		"kacho_account_id":      u.AccountID,
		"kaname_principal_type": "user",
		"kaname_audience":       s.cfg.Domain,
		"kaname_issued_at":      s.now().Unix(),
	}
}
`

// claimInjNamesakes — ЗАКОННЫЕ однофамильцы, каждый со своим способом обмануть
// разбор:
//
//   - метрика платформы стоит доводом вызова — форма «имя в вызове»;
//   - имя схемы платформы объявлено константой — форма «объявление константы»;
//   - тип ресурса модуля инфраструктуры стоит ключом отображения — форма
//     «ключ состава», та самая, которой опознаётся чеканка.
//
// Ни один из троих не связан с местом чеканки, поэтому в словарь не входит и
// молчание гейта на нём — не удача, а построение.
const claimInjNamesakes = `package observability

const schemaOfThePreviousInstall = "kacho_iam"

func register(r *prometheus.Registry) {
	r.MustRegister(newCounter("kacho_vpc_outbox_backlog_depth"))
	_ = map[string]string{
		"kacho_vpc_network":        "vpc.network",
		"kacho_vpc_subnet":         "vpc.subnet",
		"kacho_registry_repository": "registry.repository",
	}
}
`

// claimInjReaderOnly — читатель, который клеймо НЕ чеканит: он назвал имя из
// словаря, поэтому в область входит, и остальные имена того же файла в словарь
// добираются. Так гейт видит клеймо, у которого чеканщика нет вовсе.
const claimInjReaderOnly = `package middleware

func (e *Extractor) fill(ext map[string]any, out map[string]any) {
	if v, ok := ext["kaname_user_id"].(string); ok {
		out["user_id"] = v
	}
	if v, ok := ext["kacho_sa_id"].(string); ok {
		out["sa_id"] = v
	}
	for k, v := range ext {
		if !strings.HasPrefix(k, "kacho_") {
			continue
		}
		out[k] = v
	}
}
`

// claimInjScan — разбор одного синтетического файла теми же функциями, что и гейт.
func claimInjScan(t *testing.T, path, src string) ClaimFileScan {
	t.Helper()
	uses, census, err := ScanTokenClaimNames(path, []byte(src), claimNamespaces)
	if err != nil {
		t.Fatalf("разбор %s: %v", path, err)
	}
	if census.Literals == 0 {
		t.Fatalf("%s: прочитано ноль литералов — синтетика не разобрана, "+
			"и вердикт по ней беспредметен", path)
	}
	mint, err := ScanClaimMint(path, []byte(src), claimNamespaces, claimMinKeys)
	if err != nil {
		t.Fatalf("разбор составов %s: %v", path, err)
	}
	return ClaimFileScan{Uses: uses, Assembled: mint}
}

// claimInjForeign — имена чужого словаря в выведенном словаре.
func claimInjForeign(v ClaimVocabulary) []string {
	var out []string
	for name := range v.Names {
		if strings.HasPrefix(name, claimForeignNamespace+"_") {
			out = append(out, name)
		}
	}
	return out
}

// TestClaimBrandControl_CleanTreeIsSilent — контроль: всё цело, оба
// однофамильца на месте, гейт молчит. Без него краснота инъекций ничего не
// доказывает: проверка, краснеющая всегда, находит и на чистом дереве.
func TestClaimBrandControl_CleanTreeIsSilent(t *testing.T) {
	t.Parallel()
	v := DeriveClaimVocabulary(map[string]ClaimFileScan{
		"synthetic/service/mint.go":      claimInjScan(t, "synthetic/service/mint.go", claimInjMint),
		"synthetic/observability/reg.go": claimInjScan(t, "synthetic/observability/reg.go", claimInjNamesakes),
	})
	if got := claimInjForeign(v); len(got) != 0 {
		t.Fatalf("на целом дереве найдено %d имён чужого словаря (%s) — гейт краснеет "+
			"на однофамильцах, и его находки нечитаемы", len(got), strings.Join(got, ", "))
	}
	if len(v.Names) != 5 {
		t.Fatalf("словарь выведен из %d имён, ожидалось 5 (только клеймы чеканки): %v",
			len(v.Names), v.Names)
	}
	if v.Files["synthetic/observability/reg.go"] {
		t.Fatal("файл однофамильцев попал в область клейм — значит разбор судит " +
			"приставку, а не связь с местом чеканки")
	}
}

// TestClaimBrandInjection_ForeignMintIsFound — сторона (а): клеймо чужого
// словаря в месте чеканки становится находкой, и находка несёт координату.
func TestClaimBrandInjection_ForeignMintIsFound(t *testing.T) {
	t.Parallel()
	v := DeriveClaimVocabulary(map[string]ClaimFileScan{
		"synthetic/service/mint.go":      claimInjScan(t, "synthetic/service/mint.go", claimInjForeignMint),
		"synthetic/observability/reg.go": claimInjScan(t, "synthetic/observability/reg.go", claimInjNamesakes),
	})
	got := claimInjForeign(v)
	if len(got) != 1 || got[0] != "kacho_account_id" {
		t.Fatalf("ожидалась ровно одна находка kacho_account_id, получено %v", got)
	}
	if u := v.Names["kacho_account_id"]; u.File != "synthetic/service/mint.go" {
		t.Fatalf("находка обязана нести координату, получено %+v", u)
	}
	if len(claimInjForeign(v)) != 1 {
		t.Fatal("инъекция обязана ронять ТОЛЬКО проверяемое: однофамильцы не в счёт")
	}
}

// TestClaimBrandInjection_ReadOnlyClaimIsDerived — сторона (а), вторая ось:
// клеймо, которое НИКТО не чеканит, входит в словарь через связь с местом
// чеканки. Без этого шага чужое имя, только читаемое, оставалось бы вне
// наблюдения — не находкой и не чистотой, а невидимостью.
func TestClaimBrandInjection_ReadOnlyClaimIsDerived(t *testing.T) {
	t.Parallel()
	v := DeriveClaimVocabulary(map[string]ClaimFileScan{
		"synthetic/service/mint.go":      claimInjScan(t, "synthetic/service/mint.go", claimInjMint),
		"synthetic/middleware/read.go":   claimInjScan(t, "synthetic/middleware/read.go", claimInjReaderOnly),
		"synthetic/observability/reg.go": claimInjScan(t, "synthetic/observability/reg.go", claimInjNamesakes),
	})
	if _, ok := v.Names["kacho_sa_id"]; !ok {
		t.Fatalf("клеймо, которое только читают, обязано войти в словарь через связь "+
			"с местом чеканки; выведено: %v", v.Names)
	}
	if v.Minted["kacho_sa_id"] {
		t.Fatal("оно НЕ чеканится — гейт обязан различать чеканку и чтение, " +
			"иначе отказ говорит неправду о том, кто автор имени")
	}
	if v.Files["synthetic/observability/reg.go"] {
		t.Fatal("рост области не вправе захватывать файл однофамильцев")
	}
}

// TestClaimBrandInjection_PrefixIsFound — приставка ЦЕЛОГО словаря, отданная
// предикату, находится отдельно от имён: она переносит словарь разом и при
// смене имён молча перестаёт совпадать.
func TestClaimBrandInjection_PrefixIsFound(t *testing.T) {
	t.Parallel()
	fs := claimInjScan(t, "synthetic/middleware/read.go", claimInjReaderOnly)
	var prefixes []ClaimNameUse
	for _, u := range fs.Uses {
		if u.Form == ClaimFormPrefix {
			prefixes = append(prefixes, u)
		}
	}
	if len(prefixes) != 1 || prefixes[0].Namespace != claimForeignNamespace {
		t.Fatalf("ожидалась одна приставка чужого словаря, получено %+v", prefixes)
	}
	if prefixes[0].Line == 0 {
		t.Fatal("находка приставки обязана нести строку")
	}
}

// TestClaimBrandInjection_PrefixOfOwnNamespaceIsSilent — законный близнец
// приставки: та же форма, свой словарь. Без него гейт ловил бы ФОРМУ, а не
// существо, и первый же законный предикат его отключил бы.
func TestClaimBrandInjection_PrefixOfOwnNamespaceIsSilent(t *testing.T) {
	t.Parallel()
	src := strings.ReplaceAll(claimInjReaderOnly, `"kacho_`, `"kaname_`)
	fs := claimInjScan(t, "synthetic/middleware/read.go", src)
	for _, u := range fs.Uses {
		if u.Form == ClaimFormPrefix && u.Namespace == claimForeignNamespace {
			t.Fatalf("приставка своего словаря объявлена находкой: %+v", u)
		}
	}
}

// TestClaimTwinInjection_TwinOutsideGoIsFound — ось Б: имя вне Go.
//
// Посевной набор и профиль развёртывания клеймо называют, а позиции у них нет.
// Судится пара, выведенная из места чеканки.
func TestClaimTwinInjection_TwinOutsideGoIsFound(t *testing.T) {
	t.Parallel()
	twins := map[string]string{"kacho_user_id": "kaname_user_id"}

	const profile = `allowed_top_level_claims:
  - kaname_principal_type
  - kacho_user_id
`
	hits := FindClaimTwins(profile, twins)
	if len(hits) != 1 || hits[0].Line != 3 {
		t.Fatalf("двойник в профиле обязан находиться со строкой, получено %+v", hits)
	}
	if hits[0].Of != "kaname_user_id" {
		t.Fatalf("находка обязана называть, чьим двойником она является: %+v", hits[0])
	}
}

// TestClaimTwinInjection_NamesakeAndSubstringAreSilent — законные близнецы оси Б,
// оба обязательны:
//
//   - однофамилец платформы (`kacho_vpc_network`) двойником не является: пары
//     для него не выведено, потому что клейма с таким телом нет;
//   - имя, ВНУТРИ которого стоит искомое (`kacho_user_id_legacy`), находкой не
//     является: без границы токена отказ называл бы имя, которого в тексте нет.
func TestClaimTwinInjection_NamesakeAndSubstringAreSilent(t *testing.T) {
	t.Parallel()
	twins := map[string]string{"kacho_user_id": "kaname_user_id"}

	const lawful = `metrics:
  - kacho_vpc_network
  - kacho_iam_account
columns:
  - kacho_user_id_legacy
  - x_kacho_user_id
`
	if hits := FindClaimTwins(lawful, twins); len(hits) != 0 {
		t.Fatalf("законные однофамильцы и подстроки объявлены находками: %+v", hits)
	}
}

// claimInjSetLedger — ВЕДОМОСТЬ: закрытый перечень функций схемы общего
// фундамента. Величина здесь стоит КЛЮЧОМ, а значение лишь помечает членство,
// поэтому составом клейм это не является и семенем словаря стать не должно.
//
// Фикстура не выдумана: ровно эта ведомость живёт в дереве и ровно она сделала
// гейт красным через 21 минуту после его посадки — семь имён схемы вошли в
// словарь клейм, и гейт потребовал переименовать применённую миграцию.
const claimInjSetLedger = `package repohygiene

var foundationSchemaFunctions = map[string]bool{
	"kacho_quota_admit":  true,
	"kacho_quota_count":  true,
	"kacho_quota_refuse": true,
	"kacho_labels_valid": true,
}
`

// claimInjSetLedgerAsStructSet — та же ведомость второй записью того же приёма.
// Изменённый факт РОВНО ОДИН против фикстуры выше: тип значения.
const claimInjSetLedgerAsStructSet = `package repohygiene

var foundationSchemaFunctions = map[string]struct{}{
	"kacho_quota_admit":  {},
	"kacho_quota_count":  {},
	"kacho_quota_refuse": {},
	"kacho_labels_valid": {},
}
`

// claimInjSetLedgerAsComposition — те же имена, те же значения, тот же файл;
// изменённый факт РОВНО ОДИН — тип значения `bool` заменён на `any`. Тогда это
// уже не множество, а состав, и семенем оно обязано стать: иначе различитель
// закрыл бы вместе с ведомостью и настоящую чеканку.
const claimInjSetLedgerAsComposition = `package repohygiene

var foundationSchemaFunctions = map[string]any{
	"kacho_quota_admit":  true,
	"kacho_quota_count":  true,
	"kacho_quota_refuse": true,
	"kacho_labels_valid": true,
}
`

// TestClaimBrandControl_MembershipSetIsNotAMint — сторона (б): законный
// близнец. Ведомость членства семенем не становится — ни в записи через `bool`,
// ни в записи через пустой `struct{}`.
//
// Проверяется ИСХОД разбора, а не объявление: `ScanClaimMint` обязана вернуть
// ноль ключей, и тогда словарь по такому файлу не выводится вовсе.
func TestClaimBrandControl_MembershipSetIsNotAMint(t *testing.T) {
	t.Parallel()
	for name, src := range map[string]string{
		"значение bool":     claimInjSetLedger,
		"значение struct{}": claimInjSetLedgerAsStructSet,
	} {
		mint, err := ScanClaimMint("synthetic/repohygiene/ledger.go", []byte(src),
			claimNamespaces, claimMinKeys)
		if err != nil {
			t.Fatalf("%s: разбор ведомости: %v", name, err)
		}
		keys := mint.Keys
		if len(keys) != 0 {
			t.Fatalf("%s: ведомость членства принята за состав — семенем стали %v.\n\n"+
				"У множества значения нет: величина стоит ключом, а значение лишь "+
				"помечает членство. Приняв ведомость за чеканку, гейт сеет словарь "+
				"клейм именами схемы и требует переименовать применённую миграцию — "+
				"после чего его снимает первый же, кто на него наткнётся.", name, keys)
		}
		v := DeriveClaimVocabulary(map[string]ClaimFileScan{
			"synthetic/repohygiene/ledger.go": claimInjScan(t,
				"synthetic/repohygiene/ledger.go", src),
		})
		if got := claimInjForeign(v); len(got) != 0 {
			t.Fatalf("%s: на ведомости выведено %d имён чужого словаря (%s)",
				name, len(got), strings.Join(got, ", "))
		}
	}
}

// TestClaimBrandInjection_CompositionOfTheSameNamesIsAMint — сторона (а) для
// того же различителя: одно-фактная инъекция в ОБРАТНУЮ сторону.
//
// Без неё различитель мог бы закрыть предмет целиком — то есть перестать видеть
// чеканку вовсе, — и его молчание на ведомости было бы неотличимо от молчания
// мёртвого разбора.
func TestClaimBrandInjection_CompositionOfTheSameNamesIsAMint(t *testing.T) {
	t.Parallel()
	mint, err := ScanClaimMint("synthetic/repohygiene/ledger.go",
		[]byte(claimInjSetLedgerAsComposition), claimNamespaces, claimMinKeys)
	if err != nil {
		t.Fatalf("разбор состава: %v", err)
	}
	keys := mint.Keys
	if len(keys) != 4 {
		t.Fatalf("состав из четырёх ключей дал %d — различитель закрыл вместе с "+
			"ведомостью и настоящую чеканку: %v", len(keys), keys)
	}
	v := DeriveClaimVocabulary(map[string]ClaimFileScan{
		"synthetic/repohygiene/ledger.go": claimInjScan(t,
			"synthetic/repohygiene/ledger.go", claimInjSetLedgerAsComposition),
	})
	got := claimInjForeign(v)
	sort.Strings(got)
	if len(got) != 4 {
		t.Fatalf("состав обязан посеять словарь: ожидалось 4 имени, получено %v", got)
	}
	if u := v.Names["kacho_quota_admit"]; u.File != "synthetic/repohygiene/ledger.go" {
		t.Fatalf("находка обязана нести координату, получено %+v", u)
	}
}

// ============================================================================
// Ось В: у имени клейма есть АВТОР.
//
// Инъекции гоняют ТЕ ЖЕ функции, что и гейт (`ScanClaimMint`,
// `DeriveClaimVocabulary`, `AuditClaimAuthors`): доказательство, читающее свою
// копию суждения, доказывает свойство копии.
// ============================================================================

// claimInjMintByConst — место чеканки, где часть ключей стоит КОНСТАНТОЙ.
//
// Не выдумка: ровно так собирает состав выпуск — вид и идентификатор принципала
// приезжают объявлением из соседнего пакета, а прочее стоит литералом.
const claimInjMintByConst = `package service

import "svc/domain"

func (s *Svc) saClaims(a acct) map[string]any {
	return map[string]any{
		"kaname_external_id":       a.Subject,
		"kaname_sa_key_id":         a.KeyID,
		"kaname_audience":          s.cfg.Domain,
		domain.ClaimPrincipalType:  "service_account",
		domain.ClaimPrincipalID:    a.SvaID,
	}
}
`

// claimInjConstHome — единственный дом имени, которым пользуются оба конца.
const claimInjConstHome = `package domain

const (
	ClaimPrincipalType = "kaname_principal_type"
	ClaimPrincipalID   = "kaname_principal_id"
)
`

// claimInjOwnReaderOnly — читатель СВОЕГО словаря, чеканщика у имени нет.
// Изменённый факт против чистого дерева ровно один: лишнее читаемое имя.
const claimInjOwnReaderOnly = `package middleware

func (e *Extractor) fill(ext map[string]any, out map[string]any) {
	if v, ok := ext["kaname_external_id"].(string); ok {
		out["external_id"] = v
	}
	if v, ok := ext["kaname_device_id"].(string); ok {
		out["device_id"] = v
	}
}
`

// claimInjName — имя клейма для утверждений СОБИРАЕТСЯ, а не пишется литералом.
//
// Не украшение, а необходимость, и цена измерена. Разбор читает ЭТОТ файл как
// всякий другой: имя клейма, записанное здесь литералом в позиции (ключ
// отображения, чтение по индексу, довод вызова), делает файл членом области —
// а членство области втягивает в словарь ВСЕ имена файла, включая те, что
// синтетика называет нарочно. На первом же заходе это дало каскад: словарь
// вырос с 23 имён до 35, ось А нашла 10 «чужих» имён, ось Б — 28 двойников, и
// все 38 находок были фикстурами гейта и ведомостями схемы соседних гейтов.
//
// Собранное из частей имя ни одной позиции не занимает: `"kaname"` и `"_"`
// формы имени клейма не имеют, а хвост вроде `"device_id"` имеет её со
// словарём `device`, которого в словарях клейм нет.
//
// Фикстуры-исходники этого не требуют: они лежат в СЫРЫХ строках, а сырая
// строка есть один литерал целиком, и имена внутри неё узлами не являются.
func claimInjName(body string) string { return claimOwnNamespace + "_" + body }

// claimInjAuthorTree — дерево оси В: чеканка ключами-константами, её дом и
// читатель. Пути НЕ несут суффикса проб: область оси В — не-тестовое дерево.
func claimInjAuthorTree(t *testing.T, withReader bool) (ClaimVocabulary, map[string]ClaimFileScan) {
	t.Helper()
	files := map[string]ClaimFileScan{
		"synthetic/service/mint.go":  claimInjScan(t, "synthetic/service/mint.go", claimInjMintByConst),
		"synthetic/domain/claims.go": claimInjScan(t, "synthetic/domain/claims.go", claimInjConstHome),
	}
	if withReader {
		files["synthetic/middleware/read.go"] = claimInjScan(t,
			"synthetic/middleware/read.go", claimInjOwnReaderOnly)
	}
	return DeriveClaimVocabulary(files), files
}

// claimInjArea — область: имя названо НЕ-тестовым файлом. Тот же разрез, что у
// гейта, только на синтетике.
func claimInjArea(files map[string]ClaimFileScan) func(string) bool {
	return func(name string) bool {
		for path, fs := range files {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			for _, u := range fs.Uses {
				if u.Form != ClaimFormPrefix && u.Name == name {
					return true
				}
			}
		}
		return false
	}
}

// TestClaimAuthorControl_MintedTreeIsSilent — контроль: у каждого имени есть
// чеканщик, ведомость пуста, гейт молчит.
//
// Без него краснота инъекций ничего не доказывает: проверка, краснеющая всегда,
// находит и на чистом дереве.
func TestClaimAuthorControl_MintedTreeIsSilent(t *testing.T) {
	t.Parallel()
	v, files := claimInjAuthorTree(t, false)
	a := AuditClaimAuthors(v, claimInjArea(files), map[string]string{})
	if len(a.Missing) != 0 {
		t.Fatalf("на дереве, где чеканится всё, найдено %d имён без автора: %v",
			len(a.Missing), a.Missing)
	}
	if len(a.Stale) != 0 {
		t.Fatalf("пустая ведомость дала %d записей без предмета: %v", len(a.Stale), a.Stale)
	}
	if len(a.Area) != 5 {
		t.Fatalf("область выведена из %d имён, ожидалось 5: %v", len(a.Area), a.Area)
	}
}

// TestClaimAuthorControl_ConstKeyIsAMint — сторона (б) распознавателя: ключ,
// стоящий КОНСТАНТОЙ, есть чеканка.
//
// Различие между двумя записями ключа лежит в тексте, а не в том, попадёт ли
// клеймо в токен. Распознаватель, знающий одну запись, объявил бы чеканимое имя
// читаемым — и гейт потребовал бы автора у имени, у которого автор есть.
func TestClaimAuthorControl_ConstKeyIsAMint(t *testing.T) {
	t.Parallel()
	mint, err := ScanClaimMint("synthetic/service/mint.go", []byte(claimInjMintByConst),
		claimNamespaces, claimMinKeys)
	if err != nil {
		t.Fatalf("разбор состава: %v", err)
	}
	if len(mint.Keys) != 3 {
		t.Fatalf("ключей-литералов %d, ожидалось 3: %v", len(mint.Keys), mint.Keys)
	}
	sort.Strings(mint.Idents)
	if len(mint.Idents) != 2 ||
		mint.Idents[0] != "ClaimPrincipalID" || mint.Idents[1] != "ClaimPrincipalType" {
		t.Fatalf("ключей-идентификаторов ожидалось два поимённо, получено %v", mint.Idents)
	}

	v, _ := claimInjAuthorTree(t, false)
	for _, name := range []string{claimInjName("principal_type"), claimInjName("principal_id")} {
		if !v.Minted[name] {
			t.Fatalf("%s объявлено читаемым, хотя стоит ключом состава через "+
				"константу; выведено: %v", name, v.Minted)
		}
		if !v.MintedByIdent[name] {
			t.Fatalf("%s не отмечено пришедшим через ключ-константу — перепись "+
				"перестала различать формы, и расширение распознавателя невидимо", name)
		}
	}
}

// TestClaimAuthorInjection_UnresolvedIdentInventsNoAuthor — сторона (а) того же
// распознавателя, одно-фактная инъекция В ОБРАТНУЮ сторону: дом константы
// СНЯТ, всё прочее на месте.
//
// Без неё распознаватель мог бы объявлять чеканкой любой идентификатор — и его
// молчание на настоящей чеканке было бы неотличимо от молчания разбора,
// выдумывающего авторов.
func TestClaimAuthorInjection_UnresolvedIdentInventsNoAuthor(t *testing.T) {
	t.Parallel()
	v := DeriveClaimVocabulary(map[string]ClaimFileScan{
		"synthetic/service/mint.go": claimInjScan(t, "synthetic/service/mint.go", claimInjMintByConst),
	})
	for _, name := range []string{claimInjName("principal_type"), claimInjName("principal_id")} {
		if v.Minted[name] {
			t.Fatalf("%s объявлено чеканимым при СНЯТОМ доме константы — "+
				"распознаватель выдумал автора", name)
		}
	}
	if !v.Minted[claimInjName("external_id")] {
		t.Fatal("вместе с домом константы разбор потерял и чеканку литералом — " +
			"инъекция уронила не только проверяемое")
	}
}

// TestClaimAuthorInjection_AmbiguousIdentIsNotResolved — идентификатор,
// объявленный в дереве под ДВУМЯ разными именами, не раскрывается ни в одно из
// них: раскрыть его значило бы выбрать за автора.
func TestClaimAuthorInjection_AmbiguousIdentIsNotResolved(t *testing.T) {
	t.Parallel()
	const otherHome = `package other

const ClaimPrincipalType = "kaname_audience"
`
	v := DeriveClaimVocabulary(map[string]ClaimFileScan{
		"synthetic/service/mint.go":  claimInjScan(t, "synthetic/service/mint.go", claimInjMintByConst),
		"synthetic/domain/claims.go": claimInjScan(t, "synthetic/domain/claims.go", claimInjConstHome),
		"synthetic/other/claims.go":  claimInjScan(t, "synthetic/other/claims.go", otherHome),
	})
	if v.MintedByIdent[claimInjName("principal_type")] {
		t.Fatal("неоднозначный идентификатор раскрыт — разбор выбрал автора за дерево")
	}
	if len(v.AmbiguousIdents) != 1 || v.AmbiguousIdents[0] != "ClaimPrincipalType" {
		t.Fatalf("неоднозначность обязана называться поимённо, получено %v", v.AmbiguousIdents)
	}
	if !v.MintedByIdent[claimInjName("principal_id")] {
		t.Fatal("однозначный идентификатор перестал раскрываться — инъекция уронила " +
			"не только проверяемое")
	}
}

// TestClaimAuthorInjection_ReadWithoutMinterIsFound — сторона (а) самого гейта:
// имя, которое дерево только читает, становится находкой.
//
// Изменённый факт против контроля ровно один: добавлен файл-читатель.
func TestClaimAuthorInjection_ReadWithoutMinterIsFound(t *testing.T) {
	t.Parallel()
	v, files := claimInjAuthorTree(t, true)
	a := AuditClaimAuthors(v, claimInjArea(files), map[string]string{})
	deviceClaim := claimInjName("device_id")
	if len(a.Missing) != 1 || a.Missing[0] != deviceClaim {
		t.Fatalf("ожидалась ровно одна находка %s, получено %v", deviceClaim, a.Missing)
	}
	if u := v.Names[deviceClaim]; u.File != "synthetic/middleware/read.go" || u.Line == 0 {
		t.Fatalf("находка обязана нести координату, получено %+v", u)
	}
}

// TestClaimAuthorControl_RecordedDecisionIsSilent — законный близнец: то же
// дерево, но у имени есть ЗАПИСЬ решения. Изменённый факт ровно один —
// ведомость.
//
// Без него гейт не имел бы способа принять законный случай, и его снял бы
// первый, кто на такой случай наткнётся.
func TestClaimAuthorControl_RecordedDecisionIsSilent(t *testing.T) {
	t.Parallel()
	v, files := claimInjAuthorTree(t, true)
	a := AuditClaimAuthors(v, claimInjArea(files), map[string]string{
		claimInjName("device_id"): "решено там-то",
	})
	if len(a.Missing) != 0 {
		t.Fatalf("запись решения не принята: %v", a.Missing)
	}
	if len(a.Stale) != 0 {
		t.Fatalf("запись С предметом объявлена истёкшей: %v", a.Stale)
	}
}

// TestClaimAuthorInjection_LedgerExpiresByItself — послабление ИСТЕКАЕТ САМО, и
// обе формы истечения проверяются порознь:
//
//   - имя начало чеканиться — запись больше ничего не исключает;
//   - не-тестовое дерево перестало имя читать — предмета нет вовсе.
//
// Без самоистечения запись переживает свой предмет и молча прощает следующего,
// кто унаследует ровно эту слепую зону.
func TestClaimAuthorInjection_LedgerExpiresByItself(t *testing.T) {
	t.Parallel()
	v, files := claimInjAuthorTree(t, true)
	area := claimInjArea(files)

	minted := claimInjName("external_id")
	a := AuditClaimAuthors(v, area, map[string]string{minted: "оно чеканится"})
	if len(a.Stale) != 1 || !strings.Contains(a.Stale[0], minted) ||
		!strings.Contains(a.Stale[0], "уже чеканится") {
		t.Fatalf("запись на чеканимое имя обязана истечь с причиной, получено %v", a.Stale)
	}

	a = AuditClaimAuthors(v, area, map[string]string{claimInjName("never_read"): "выдумка"})
	if len(a.Stale) != 1 || !strings.Contains(a.Stale[0], "не читает") {
		t.Fatalf("запись на нечитаемое имя обязана истечь с причиной, получено %v", a.Stale)
	}
}

// TestClaimAuthorControl_TestOnlyNameIsOutOfArea — законный близнец разреза:
// имя, названное ТОЛЬКО пробой, автора не требует.
//
// Синтетика проб объявляет имена нарочно — так доказывается, что край проносит
// незнакомое клеймо насквозь. Судя её, гейт краснел бы на собственном
// доказательстве, а починка состояла бы в том, чтобы доказательство
// обессмыслить.
func TestClaimAuthorControl_TestOnlyNameIsOutOfArea(t *testing.T) {
	t.Parallel()
	files := map[string]ClaimFileScan{
		"synthetic/service/mint.go":  claimInjScan(t, "synthetic/service/mint.go", claimInjMintByConst),
		"synthetic/domain/claims.go": claimInjScan(t, "synthetic/domain/claims.go", claimInjConstHome),
		// ТОТ ЖЕ файл-читатель, изменён ровно один факт: имя пути.
		"synthetic/middleware/read_test.go": claimInjScan(t,
			"synthetic/middleware/read_test.go", claimInjOwnReaderOnly),
	}
	v := DeriveClaimVocabulary(files)
	if _, known := v.Names[claimInjName("device_id")]; !known {
		t.Fatal("имя из пробы обязано войти в СЛОВАРЬ — иначе молчание гейта " +
			"объясняется слепотой разбора, а не разрезом области")
	}
	a := AuditClaimAuthors(v, claimInjArea(files), map[string]string{})
	if len(a.Missing) != 0 {
		t.Fatalf("имя, названное только пробой, потребовало чеканщика: %v", a.Missing)
	}
}

// TestClaimAuthorControl_EmptyVocabularyIsNotGreen — пустой обход НЕ есть
// чистота: область пуста, и порог гейта обязан это поймать.
func TestClaimAuthorControl_EmptyVocabularyIsNotGreen(t *testing.T) {
	t.Parallel()
	a := AuditClaimAuthors(DeriveClaimVocabulary(map[string]ClaimFileScan{}),
		func(string) bool { return true }, map[string]string{})
	if len(a.Area) != 0 {
		t.Fatalf("на пустом дереве область обязана быть пуста, получено %v", a.Area)
	}
	if len(a.Area) >= claimAuthorFloor {
		t.Fatal("порог гейта не поймал бы пустой обход")
	}
}
