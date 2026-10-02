// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// f1b_token_acceptance_test.go — Ф1б-02…05: страж старта края на ОБЪЯВЛЕНИИ
// приёма токена.
//
// Каждое отрицание здесь стоит рядом со своим положительным контролем: без него
// проба зеленеет на разборе, отвергающем всё, и «страж работает» становится
// неотличимо от «страж отвергает любую посадку».
package config_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

const (
	f1bOurs   = "https://kaname.kacho.local"
	f1bLegacy = "https://legacy.api.kacho.test"
	f1bOursKS = "https://kaname-internal.kacho.svc:9097/.well-known/kaname/jwks.json"
	f1bLegKS  = "https://kaname-internal.kacho.svc:9097/.well-known/jwks.json"
	f1bRevURL = "https://kaname-internal.kacho.svc:9097/internal/tokens/introspect"
)

// f1bFullDeclaration — законная посадка с ДВУМЯ издателями и нашей чеканкой.
func f1bFullDeclaration() config.Config {
	return config.Config{
		AppEnv:                     "production",
		APIDomain:                  "api.kacho.test",
		TokenIssuers:               f1bOurs + "," + f1bLegacy,
		TokenIssuerKeySets:         f1bOurs + "=" + f1bOursKS + "," + f1bLegacy + "=" + f1bLegKS,
		PlatformTokenIssuer:        f1bOurs,
		PlatformTokenRevocationURL: f1bRevURL,
	}
}

// f1bOnlyTheIssuerSetGuardCanFire — объявление, в котором вырожденный перечень
// издателей отвергает СТРАЖ ПЕРЕЧНЯ и никто другой.
//
// Фикстура нарочно бедна. Полное объявление (`f1bFullDeclaration`) для этой
// пробы негодно, и это измерено, а не предположено: на нём вырожденный перечень
// роняет старт СОСЕДНИМ стражем — «наш издатель вне перечня принимаемых», — чьё
// сообщение содержит ту же подстроку, что искала прежняя редакция пробы.
// Обезвредив страж перечня, я получал `err == nil` и НОЛЬ записей приёма, то
// есть буквально посадку «принимаем любого издателя», а проба оставалась
// зелёной. Здесь названо и то, откуда фикстура бедна: убрать из неё нашего
// издателя и авторитет отзыва — значит убрать всех, кто способен отказать
// вместо предмета пробы.
func f1bOnlyTheIssuerSetGuardCanFire(env, issuers string) config.Config {
	return config.Config{
		AppEnv:       env,
		APIDomain:    "api.kacho.test",
		TokenIssuers: issuers,
	}
}

// TestF1b02_DegenerateIssuerSetRefusesTheStart — страж считает ЭЛЕМЕНТЫ, а не
// длину строки, и говорит ОБЕ величины.
func TestF1b02_DegenerateIssuerSetRefusesTheStart(t *testing.T) {
	for _, degenerate := range []string{",", " ", " , , ", ",,,", "\t"} {
		cfg := f1bOnlyTheIssuerSetGuardCanFire("production", degenerate)
		bindings, err := cfg.TokenAcceptance()
		if err == nil {
			t.Fatalf("вырожденный перечень издателей %q принят (записей приёма %d) — при длине "+
				"%d он содержит НОЛЬ элементов, а пустой перечень означает «принимаем любого "+
				"издателя»", degenerate, len(bindings), len(degenerate))
		}
		// Сообщение обязано назвать ОБЕ величины — иначе отличить этот страж от
		// соседнего нечем, и проба зеленеет на любом отказе.
		//
		// Именно эти две подстроки и есть различающая сила пробы: предикат по
		// длине строки, поставленный вместо предиката по элементам, напечатал бы
		// не ноль элементов, а другое число либо не напечатал бы ничего.
		wantElements := "0 elements"
		wantChars := fmt.Sprintf("%d characters", len(degenerate))
		if !strings.Contains(err.Error(), wantElements) || !strings.Contains(err.Error(), wantChars) {
			t.Fatalf("отказ на входе %q не называет обе величины (ждали %q и %q): %v\n"+
				"Страж, не назвавший их, неотличим от соседнего: у «,» длина 1 и элементов "+
				"ноль, и именно на таком входе предикат по длине молчит.",
				degenerate, wantElements, wantChars, err)
		}
		if !strings.Contains(err.Error(), "KACHO_API_GATEWAY_TOKEN_ISSUERS") {
			t.Fatalf("отказ не называет настройку, которую правит оператор: %v", err)
		}
	}

	// Отказ НЕ освобождается режимом: «принимаем любого» не становится законным
	// оттого, что стенд назвали разработческим.
	for _, env := range []string{"dev", "local", "test", "", "production", "production-strict"} {
		cfg := f1bOnlyTheIssuerSetGuardCanFire(env, ",")
		_, err := cfg.TokenAcceptance()
		if err == nil {
			t.Fatalf("вырожденный перечень принят в режиме %q — освобождение по режиму здесь "+
				"означало бы объявленную посадку «принимаем любого издателя»", env)
		}
		if !strings.Contains(err.Error(), "0 elements") {
			t.Fatalf("в режиме %q отказал не страж перечня, а кто-то другой: %v", env, err)
		}
	}

	// ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ на той же бедной фикстуре: непустой перечень с
	// объявленной записью источника проходит. Без него отрицания выше зеленели
	// бы на разборе, отвергающем всё.
	ok := f1bOnlyTheIssuerSetGuardCanFire("production", f1bLegacy)
	ok.TokenIssuerKeySets = f1bLegacy + "=" + f1bLegKS
	if _, err := ok.TokenAcceptance(); err != nil {
		t.Fatalf("непустой перечень на той же фикстуре отвергнут: %v", err)
	}
}

// TestF1b02_BothCardinalitiesStart — положительный контроль на ОБЕИХ мощностях.
func TestF1b02_BothCardinalitiesStart(t *testing.T) {
	one := f1bFullDeclaration()
	one.TokenIssuers = f1bLegacy
	one.TokenIssuerKeySets = f1bLegacy + "=" + f1bLegKS
	one.PlatformTokenIssuer = ""
	one.PlatformTokenRevocationURL = ""
	got, err := one.TokenAcceptance()
	if err != nil {
		t.Fatalf("перечень из ОДНОГО издателя отвергнут: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("записей приёма %d, ожидалась 1", len(got))
	}

	two, err := f1bFullDeclaration().TokenAcceptance()
	if err != nil {
		t.Fatalf("перечень из ДВУХ издателей отвергнут: %v", err)
	}
	if len(two) != 2 {
		t.Fatalf("записей приёма %d, ожидалось 2", len(two))
	}
	// Полосы РАЗНЫЕ: наша строгая по типу и читает отзыв, прежняя — нет.
	var ours, legacy *config.TokenIssuerBinding
	for i := range two {
		switch two[i].Issuer {
		case f1bOurs:
			ours = &two[i]
		case f1bLegacy:
			legacy = &two[i]
		}
	}
	if ours == nil || legacy == nil {
		t.Fatalf("в записях нет обеих полос: %+v", two)
	}
	if !ours.ReadRevocation {
		t.Fatalf("НАША полоса не читает отзыв — контроль, действующий на выдаче и не " +
			"действующий на предъявлении, отзывом не является")
	}
	if ours.TolerateAbsentTokenType {
		t.Fatalf("НАША полоса терпит отсутствие типа — производитель типа мы сами")
	}
	if legacy.ReadRevocation {
		t.Fatalf("полоса ПРЕЖНЕГО издателя объявила чтение НАШЕГО авторитета отзыва — " +
			"он о чужих токенах не знает by construction")
	}
	if !legacy.TolerateAbsentTokenType {
		t.Fatalf("полоса прежнего издателя перестала терпеть отсутствие типа — форму " +
			"заголовка на ней диктуем не мы")
	}
}

// TestF1b03_IssuerWithoutRecordAndRecordWithoutIssuer — обе стороны привязки.
func TestF1b03_IssuerWithoutRecordAndRecordWithoutIssuer(t *testing.T) {
	noRecord := f1bFullDeclaration()
	noRecord.TokenIssuerKeySets = f1bLegacy + "=" + f1bLegKS
	if _, err := noRecord.TokenAcceptance(); err == nil {
		t.Fatalf("издатель принимается, не имея объявленной записи источника — он " +
			"резолвится в ничто, а вывести адрес из самого издателя запрещено")
	}

	orphanRecord := f1bFullDeclaration()
	orphanRecord.TokenIssuerKeySets += ",https://third.example.test=" + f1bLegKS
	if _, err := orphanRecord.TokenAcceptance(); err == nil {
		t.Fatalf("запись источника без принимающего её издателя принята — она объявляет " +
			"источник, к которому никогда не обратятся, и переживает свой предмет молча")
	}

	if _, err := f1bFullDeclaration().TokenAcceptance(); err != nil {
		t.Fatalf("полная привязка отвергнута: %v", err)
	}
}

// TestF1b04_DegenerateKeySetURLRefusesTheStart — вырожденный адрес записи.
func TestF1b04_DegenerateKeySetURLRefusesTheStart(t *testing.T) {
	for _, bad := range []string{"", "/", "//", "///", "   ", "/.well-known/jwks.json", "not a url"} {
		cfg := f1bFullDeclaration()
		cfg.TokenIssuerKeySets = f1bOurs + "=" + bad + "," + f1bLegacy + "=" + f1bLegKS
		if _, err := cfg.TokenAcceptance(); err == nil {
			t.Fatalf("вырожденный адрес записи %q принят — «источника нет», выданное за "+
				"«источник объявлен»", bad)
		}
	}

	// В производственной посадке незащищённая схема отвергается: источник
	// набора есть единственный якорь доверия проверки подписи.
	plain := f1bFullDeclaration()
	plain.TokenIssuerKeySets = f1bOurs + "=http://kaname-internal.kacho.svc:9097/x," +
		f1bLegacy + "=" + f1bLegKS
	if _, err := plain.TokenAcceptance(); err == nil {
		t.Fatalf("незащищённая схема адреса набора принята в производственной посадке")
	}
	plain.AppEnv = "dev"
	if _, err := plain.TokenAcceptance(); err != nil {
		t.Fatalf("незащищённая схема отвергнута в режиме разработки: %v — там она "+
			"допустима симметрично незашифрованному соединению к базе", err)
	}
}

// TestF1b05_IncompleteDeclarationsRefuseTheStart — наш издатель объявлен вне
// перечня, принимается без авторитета отзыва либо с негодным его адресом.
func TestF1b05_IncompleteDeclarationsRefuseTheStart(t *testing.T) {
	// Наш издатель вне перечня принимаемых.
	outside := f1bFullDeclaration()
	outside.PlatformTokenIssuer = "https://iam.other.test"
	if _, err := outside.TokenAcceptance(); err == nil {
		t.Fatalf("наш издатель объявлен вне перечня принимаемых — платформа чеканила бы то, " +
			"что край отвергнет на первом же запросе")
	}

	// Наш издатель принимается, а авторитет отзыва не объявлен.
	noAuthority := f1bFullDeclaration()
	noAuthority.PlatformTokenRevocationURL = "   "
	if _, err := noAuthority.TokenAcceptance(); err == nil {
		t.Fatalf("наш издатель принимается без объявленного авторитета отзыва")
	}

	// Адрес авторитета отзыва не выводится и в производственной посадке обязан
	// быть абсолютным и защищённым.
	weakAuthority := f1bFullDeclaration()
	weakAuthority.PlatformTokenRevocationURL = "http://kaname-internal.kacho.svc:9097/x"
	if _, err := weakAuthority.TokenAcceptance(); err == nil {
		t.Fatalf("незащищённый адрес авторитета отзыва принят в производственной посадке — " +
			"ответ решает доступ и не вправе ехать открытым текстом")
	}

	// Положительный контроль на каждый: убрать нарушение — объявление принимается.
	if _, err := f1bFullDeclaration().TokenAcceptance(); err != nil {
		t.Fatalf("полное объявление отвергнуто: %v", err)
	}
}

// TestF1b01_AnUndeclaredIssuerSetRefusesTheStart — перечня принимаемых издателей
// НЕ ОБЪЯВЛЕНО, и край НЕ ПОДНИМАЕТСЯ.
//
// Прежде «не объявлено» было третьим состоянием: край строил одну запись сам —
// издателя выводил из домена установки, а адрес его набора ключей из издателя.
// Отказа старта на этом пути не было, и выведенное имя вело на хост, которого
// нет ни на одном стенде: под становился готовым и отвергал каждый токен при
// первом же запросе. Теперь «не объявлено» и «объявлено пустым» — один исход:
// принимать некого, и процесс, которому некого принимать, не стартует.
//
// Отказ не зависит от режима. Режим разработки не делает законным край, который
// не знает, чьи токены он проверяет.
func TestF1b01_AnUndeclaredIssuerSetRefusesTheStart(t *testing.T) {
	for _, env := range []string{"production", "dev", "local", "test", ""} {
		cfg := config.Config{AppEnv: env, APIDomain: "api.kacho.test"}
		bindings, err := cfg.TokenAcceptance()
		if err == nil {
			t.Fatalf("режим %q: перечень издателей НЕ ОБЪЯВЛЕН, а край поднимается с записями "+
				"приёма %d — %v. Эти записи никто не объявлял: край вывел их сам",
				env, len(bindings), bindings)
		}
		if !strings.Contains(err.Error(), "KACHO_API_GATEWAY_TOKEN_ISSUERS") ||
			!strings.Contains(err.Error(), "is not declared") {
			t.Fatalf("режим %q: отказ обязан назвать настройку и сказать, что она НЕ ОБЪЯВЛЕНА, "+
				"а не что она пуста: %v", env, err)
		}
	}

	// ЗАКОННЫЙ БЛИЗНЕЦ меняет ОДИН факт — перечень объявлен одним издателем с
	// его записью источника, — и край поднимается. Без него отказ выше
	// зеленел бы на разборе, отвергающем всё.
	twin := config.Config{AppEnv: "production", APIDomain: "api.kacho.test",
		TokenIssuers: f1bLegacy, TokenIssuerKeySets: f1bLegacy + "=" + f1bLegKS}
	got, err := twin.TokenAcceptance()
	if err != nil {
		t.Fatalf("объявленный перечень из одного издателя отвергнут: %v", err)
	}
	if len(got) != 1 || got[0].Issuer != f1bLegacy || got[0].KeySetURL != f1bLegKS {
		t.Fatalf("записи приёма %v, ожидалась одна: %s ← %s", got, f1bLegacy, f1bLegKS)
	}
	if got[0].ReadRevocation {
		t.Fatalf("полоса издателя, не названного нашим, объявила чтение НАШЕГО авторитета отзыва")
	}
}

// TestF1b04_AnInsecureKeySetURLIsRefusedInProductionAndToleratedInDev —
// источник набора есть единственный якорь доверия проверки подписи, и в
// производственной посадке он обязан ехать защищённым транспортом.
func TestF1b04_AnInsecureKeySetURLIsRefusedInProductionAndToleratedInDev(t *testing.T) {
	declared := f1bFullDeclaration()
	declared.TokenIssuerKeySets = f1bOurs + "=" + f1bOursKS + "," +
		f1bLegacy + "=http://kaname-internal.kacho.svc:9097/.well-known/jwks.json"
	if _, err := declared.TokenAcceptance(); err == nil {
		t.Fatalf("незащищённый адрес набора принят в производственной посадке")
	}

	// Положительный контроль: защищённый адрес принимается.
	if _, err := f1bFullDeclaration().TokenAcceptance(); err != nil {
		t.Fatalf("защищённый адрес отвергнут: %v", err)
	}

	// Законный близнец меняет ОДИН факт — метку окружения: в режиме разработки
	// открытый HTTP допустим, симметрично незашифрованному соединению к базе.
	declared.AppEnv = "dev"
	if _, err := declared.TokenAcceptance(); err != nil {
		t.Fatalf("незащищённый адрес отвергнут в режиме разработки: %v", err)
	}
}
