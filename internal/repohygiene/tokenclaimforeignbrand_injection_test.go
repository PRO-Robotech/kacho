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
	keys, err := ScanClaimMint(path, []byte(src), claimNamespaces, claimMinKeys)
	if err != nil {
		t.Fatalf("разбор составов %s: %v", path, err)
	}
	return ClaimFileScan{Uses: uses, Assembled: keys}
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
