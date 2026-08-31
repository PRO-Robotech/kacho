// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// tokenissuers.go — разбор объявлений приёма токена: перечня принимаемых
// издателей и привязки «издатель → адрес его набора проверочных ключей».
//
// Разбор живёт здесь, а не у стража старта, по одной причине: страж обязан
// считать ЭЛЕМЕНТЫ, а не длину строки, и то же самое обязан делать всякий, кто
// эти значения читает. Два разбора одного объявления разошлись бы молча — и
// разошлись бы там, где расхождение не видно: на вырожденном значении, где один
// говорит «непусто», а другой «пусто».
package config

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/PRO-Robotech/kacho/pkg/tokenpolicy"

	"github.com/PRO-Robotech/kacho/pkg/servicecontract"
)

const (
	// TokenTypePlatform — тип токена доступа НАШЕЙ чеканки (RFC 9068).
	TokenTypePlatform = tokenpolicy.TokenTypeAccess
	// TokenTypeLegacy — тип токена прежнего издателя. Полоса прежнего издателя
	// вне области этой под-фазы и своего поведения не меняет.
	TokenTypeLegacy = tokenpolicy.TokenTypeLegacy
)

// TokenIssuerBinding — ОБЪЯВЛЕННАЯ запись «издатель → источник его набора».
type TokenIssuerBinding struct {
	// Issuer — точное значение `iss`. Служит ТОЛЬКО ключом поиска: ни частью
	// адреса, ни частью имени, ни частью ключа кэша.
	Issuer string
	// KeySetURL — объявленный адрес набора проверочных ключей этого издателя.
	KeySetURL string
	// TokenType — ожидаемое значение `typ` для этой полосы.
	TokenType string
	// TolerateAbsentTokenType — принимать токен этой полосы без заголовка
	// типа. Несовпадающий тип отвергается всё равно. Выдано только полосе
	// прежнего издателя и уходит вместе с ней.
	TolerateAbsentTokenType bool
	// ReadRevocation — читать авторитет отзыва на предъявлении токена этого
	// издателя.
	ReadRevocation bool
}

// AcceptedTokenIssuers возвращает ЭЛЕМЕНТЫ перечня принимаемых издателей.
//
// Пустые элементы отбрасываются намеренно: значение «,» непусто как строка и
// пусто как перечень, и именно это различие обязан видеть страж старта. Ошибка
// — только на повторе: один издатель, одна запись.
func (c Config) AcceptedTokenIssuers() ([]string, error) {
	out := make([]string, 0, 2)
	seen := map[string]bool{}
	for _, raw := range strings.Split(c.TokenIssuers, ",") {
		iss := strings.TrimSpace(raw)
		if iss == "" {
			continue
		}
		if seen[iss] {
			return nil, fmt.Errorf("KACHO_REGISTRY_TOKEN_ISSUERS names issuer %q twice — one issuer, one record", iss)
		}
		seen[iss] = true
		out = append(out, iss)
	}
	return out, nil
}

// TokenIssuerKeySetMap разбирает привязку «издатель → адрес набора».
//
// Вырожденная запись отвергается здесь, а не в рантайме: издатель без адреса
// (пусто, одни разделители, относительный путь) — это «источника нет», выданное
// за «источник объявлен».
func (c Config) TokenIssuerKeySetMap() (map[string]string, error) {
	out := map[string]string{}
	for _, raw := range strings.Split(c.TokenIssuerKeySets, ",") {
		pair := strings.TrimSpace(raw)
		if pair == "" {
			continue
		}
		// Разделяем по ПЕРВОМУ знаку равенства: он встречается и внутри адреса
		// (строка запроса), и там он частью разделителя не является.
		issuer, keySetURL, ok := strings.Cut(pair, "=")
		if !ok {
			return nil, fmt.Errorf("KACHO_REGISTRY_TOKEN_ISSUER_KEYSETS entry %q is not «issuer=url»", pair)
		}
		issuer, keySetURL = strings.TrimSpace(issuer), strings.TrimSpace(keySetURL)
		if issuer == "" {
			return nil, fmt.Errorf("KACHO_REGISTRY_TOKEN_ISSUER_KEYSETS entry %q names no issuer", pair)
		}
		if _, dup := out[issuer]; dup {
			return nil, fmt.Errorf("KACHO_REGISTRY_TOKEN_ISSUER_KEYSETS names issuer %q twice — "+
				"one issuer, one key-set record", issuer)
		}
		if err := absoluteKeySetURL(keySetURL); err != nil {
			return nil, fmt.Errorf("KACHO_REGISTRY_TOKEN_ISSUER_KEYSETS record for issuer %q: %w", issuer, err)
		}
		out[issuer] = keySetURL
	}
	return out, nil
}

// absoluteKeySetURL отвергает адрес, который источником не является.
//
// «Только разделители» (`/`, `//`, `///`) — самый коварный вид вырожденного
// значения: строка непуста, глазом читается как путь, а адресом не является.
func absoluteKeySetURL(raw string) error {
	if raw == "" {
		return fmt.Errorf("key-set URL is empty (an unset source is not a declared source)")
	}
	if strings.Trim(raw, "/ \t") == "" {
		return fmt.Errorf("key-set URL %q consists of separators only", raw)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("key-set URL %q is not a URL: %w", raw, err)
	}
	if u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("key-set URL %q is not absolute (scheme and host are required; "+
			"a relative path cannot be a declared source)", raw)
	}
	return nil
}

// TokenIssuerBindings собирает записи приёма: по одной на КАЖДОГО принимаемого
// издателя, с его объявленным адресом, полосой типа токена и признаком чтения
// отзыва.
//
// Издатель без записи источника здесь становится ошибкой — это и есть то
// состояние, ради проверяемости которого адрес объявляется, а не выводится.
func (c Config) TokenIssuerBindings() ([]TokenIssuerBinding, error) {
	issuers, err := c.AcceptedTokenIssuers()
	if err != nil {
		return nil, err
	}
	if len(issuers) == 0 {
		// Считаются ЭЛЕМЕНТЫ, а не длина строки, и сообщение называет обе
		// величины: у «,» длина 1 и элементов ноль, и именно на таком входе
		// предикат по длине молчит. Отказ не зависит от режима — пустой
		// перечень означает «принимаем любого издателя», а тогда проходит
		// токен любой третьей стороны, разделяющей с нами набор ключей и
		// адресата.
		return nil, fmt.Errorf("KACHO_REGISTRY_TOKEN_ISSUERS declares no issuer element "+
			"(value %q has %d characters and %d elements); an empty issuer set means "+
			"«accept any issuer»", c.TokenIssuers, len(c.TokenIssuers), len(issuers))
	}
	keySets, err := c.TokenIssuerKeySetMap()
	if err != nil {
		return nil, err
	}

	platform := strings.TrimSpace(c.PlatformTokenIssuer)
	if platform != "" {
		known := false
		for _, iss := range issuers {
			if iss == platform {
				known = true
				break
			}
		}
		if !known {
			return nil, fmt.Errorf("KACHO_REGISTRY_PLATFORM_TOKEN_ISSUER names %q, which "+
				"KACHO_REGISTRY_TOKEN_ISSUERS does not accept — the platform would mint tokens "+
				"this surface rejects on the first request", platform)
		}
	}

	out := make([]TokenIssuerBinding, 0, len(issuers))
	for _, iss := range issuers {
		keySetURL, ok := keySets[iss]
		if !ok {
			return nil, fmt.Errorf("issuer %q is accepted (KACHO_REGISTRY_TOKEN_ISSUERS) but has no "+
				"declared key-set record (KACHO_REGISTRY_TOKEN_ISSUER_KEYSETS); an issuer without a "+
				"record resolves to nothing, and deriving its address from the issuer string is "+
				"forbidden (the issuer comes from the presenter)", iss)
		}
		// Полоса ПРЕЖНЕГО издателя: тип сверяется, если объявлен, и не
		// требуется, если не объявлен. Его токены чеканим не мы, форму
		// заголовка диктует он, и требовать от неё того, чего мы у него не
		// проверяли, значило бы поставить работу живого контура на
		// непроверенное допущение о третьей стороне. Защиты строгость здесь
		// не добавляет: подпись, издатель, адресат и привязка ключа уже
		// отвергли бы чужой токен. Послабление уходит вместе с этой записью.
		b := TokenIssuerBinding{
			Issuer: iss, KeySetURL: keySetURL,
			TokenType:               TokenTypeLegacy,
			TolerateAbsentTokenType: true,
		}
		if iss == platform {
			// НАША полоса: производитель типа — мы сами, и отсутствие типа
			// означало бы, что мы не выпускаем того, что требуем.
			b.TokenType = TokenTypePlatform
			b.TolerateAbsentTokenType = false
			b.ReadRevocation = true
		}
		out = append(out, b)
	}

	// Запись источника без принимающего её издателя — тоже находка: она
	// объявляет источник, к которому никогда не обратятся, и переживает свой
	// предмет молча.
	for iss := range keySets {
		accepted := false
		for _, b := range out {
			if b.Issuer == iss {
				accepted = true
				break
			}
		}
		if !accepted {
			return nil, fmt.Errorf("KACHO_REGISTRY_TOKEN_ISSUER_KEYSETS declares a key-set record for "+
				"issuer %q, which KACHO_REGISTRY_TOKEN_ISSUERS does not accept — a record nobody reads "+
				"outlives its subject silently", iss)
		}
	}
	return out, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// ОБЪЯВЛЕНИЕ ПРИЁМА ЦЕЛИКОМ — один читатель на два вопроса
//
// «Что принимаем» и «поднимется ли эта посадка» — вопросы об ОДНОМ объявлении,
// и отвечать на них обязан один предикат. Пока их было два, они разошлись:
// страж старта освобождал режим разработки от требования непустого перечня, а
// читатель отвергал пустой перечень в любом режиме. Освобождение было
// недостижимым — отказ приходил из читателя, — но объявляло посадку, которой у
// кода нет: «в разработке принимаем любого издателя». Правило, которое нельзя
// исполнить, хуже отсутствующего (`api-conventions.md` §«ДВА ПРАВИЛА ОБ ОДНОМ
// ПОЛЕ»), а «пустой перечень допустим» — ещё и прямо запрещённая посадка
// (`security.md` §«Пустой список — это „не сужаем“»).
//
// Второе следствие единственности читателя: его вправе позвать проба
// РАЗВЁРТЫВАНИЯ и спросить у объявленного профилем ровно то, что спросит
// процесс при старте. Пока предикат жил в `main`, такой пробе оставалось
// сформулировать его заново — и разойтись с ним молча.

// Posture — посадка процесса, разобранная ОБЩИМ словарём.
//
// Словарь допустимых значений объявлен в дереве один раз
// (`servicecontract.Modes`): пока каждый сервис сравнивал строку на месте, у
// одного предмета было пять объявлений, и одно из них расходилось с остальными в
// обе стороны — принимало алиасы, которых не принимал никто, и отвергало
// значение, в котором работали шестеро (задача продукта #1656).
//
// Неразобранное значение читается как БОЕВАЯ посадка. Это не «умолчание»: старт
// на нём всё равно отвергает страж, который называет ручку и перечисляет словарь;
// а до отказа послаблений быть не должно — иначе опечатка в профиле тихо
// открывала бы то, что закрывает боевой режим.
//
// Разбор точный: ни регистр, ни обрамляющие пробелы дом не прощает, и здесь они
// не прощаются тоже — иначе у одной ручки завелось бы два правила чтения.
func (c Config) Posture() servicecontract.Mode {
	mode, err := servicecontract.ParseMode(c.AuthMode)
	if err != nil {
		return servicecontract.ModeProduction
	}
	return mode
}

// isProductionPosture — режимы, в которых послаблений нет.
func (c Config) isProductionPosture() bool { return c.Posture().IsProduction() }

// TokenAcceptance возвращает записи приёма и отвергает объявление, с которым
// плоскость данных не поднимется.
//
// Порядок — от того, без чего не построить ничего, к тому, что уточняет
// посадку: перечень издателей → привязка «издатель → источник» →
// защищённость адресов → авторитет отзыва.
//
// Место, пройденное не полностью, даёт отказ проверки при ПЕРВОМ ЖЕ ЗАПРОСЕ
// вместо отказа при СТАРТЕ. Разница не косметическая: первый виден арендатору и
// не виден оператору, второй виден оператору и не доходит до арендатора.
func (c Config) TokenAcceptance() ([]TokenIssuerBinding, error) {
	bindings, err := c.TokenIssuerBindings()
	if err != nil {
		return nil, err
	}
	readsRevocation := false
	for _, b := range bindings {
		if err := c.requireSecureKeySetURL(b.Issuer, b.KeySetURL); err != nil {
			return nil, err
		}
		readsRevocation = readsRevocation || b.ReadRevocation
	}
	if readsRevocation {
		if err := c.requireRevocationAuthority(); err != nil {
			return nil, err
		}
	}
	return bindings, nil
}

// requireSecureKeySetURL — источник набора проверочных ключей есть единственный
// якорь доверия проверки подписи. По открытому HTTP его документ подменяется на
// пути, и тогда подделывается токен под любого субъекта — то есть проверка
// подлинности обходится целиком.
//
// В dev открытый HTTP допустим — симметрично незашифрованному соединению к базе.
func (c Config) requireSecureKeySetURL(issuer, keySetURL string) error {
	if !c.isProductionPosture() {
		return nil
	}
	u, err := url.Parse(keySetURL)
	if err != nil {
		return fmt.Errorf("KACHO_REGISTRY_TOKEN_ISSUER_KEYSETS record for issuer %q: invalid URL %q: %w",
			issuer, keySetURL, err)
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return fmt.Errorf("AuthMode=%s requires an https:// key-set URL in "+
			"KACHO_REGISTRY_TOKEN_ISSUER_KEYSETS for issuer %q (the key set is the trust anchor of "+
			"signature verification and must not be fetched over plaintext; got scheme %q)",
			c.AuthMode, issuer, u.Scheme)
	}
	return nil
}

// requireRevocationAuthority — наш издатель принимается, значит отзыв обязан
// иметь читателя на пути запроса.
//
// Адрес задаётся ЯВНО. Умолчание вида «взять базовый адрес соседа и приклеить
// путь» запрещено: оно всегда непусто, поэтому контроль выглядит включённым,
// ведя в никуда, и ни один профиль развёртывания не обязан ничего задавать,
// чтобы это заметить.
func (c Config) requireRevocationAuthority() error {
	trimmed := strings.TrimSpace(c.TokenRevocationURL)
	if trimmed == "" {
		return fmt.Errorf("KACHO_REGISTRY_PLATFORM_TOKEN_ISSUER is accepted, so "+
			"KACHO_REGISTRY_TOKEN_REVOCATION_URL is required: a control that acts only where the "+
			"credential is ISSUED is not revocation — it merely declines to issue a new one "+
			"(AuthMode=%s)", c.AuthMode)
	}
	if !c.isProductionPosture() {
		return nil
	}
	u, err := url.Parse(trimmed)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("AuthMode=%s requires an absolute KACHO_REGISTRY_TOKEN_REVOCATION_URL "+
			"(got %q); it is declared explicitly and never derived from a neighbour's address",
			c.AuthMode, c.TokenRevocationURL)
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return fmt.Errorf("AuthMode=%s requires an https:// KACHO_REGISTRY_TOKEN_REVOCATION_URL "+
			"(the answer decides access and must not transit plaintext; got scheme %q)",
			c.AuthMode, u.Scheme)
	}
	return nil
}
