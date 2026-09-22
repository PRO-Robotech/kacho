// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// tokenissuers.go — разбор ОБЪЯВЛЕНИЯ приёма токена на крае: перечня
// принимаемых издателей и привязки «издатель → адрес его набора проверочных
// ключей».
//
// # Почему разбор живёт здесь, а не у стража старта
//
// Страж обязан считать ЭЛЕМЕНТЫ, а не длину строки, и ровно то же обязан делать
// всякий, кто эти значения ЧИТАЕТ. Два разбора одного объявления разошлись бы
// молча — и разошлись бы там, где расхождение не видно: на вырожденном
// значении, где один говорит «непусто», а другой «пусто».
//
// Второе следствие единственности читателя: его вправе позвать проба
// РАЗВЁРТЫВАНИЯ и спросить у объявленного профилем ровно то, что спросит
// процесс при старте.
//
// # Состояний два, и одно из них — отказ старта
//
//   - не задано ЛИБО задано и даёт НОЛЬ элементов ⇒ отказ в старте,
//     безусловный: краю некого принимать, а пустой перечень означал бы
//     «принимаем любого издателя»;
//   - задано и даёт элементы ⇒ принимаются ровно они, у каждого своя запись.
//
// Третьего состояния — «не задано, и край строит запись сам» — нет. Прежде оно
// было: издатель выводился из домена установки, адрес набора из издателя, и
// отказа старта на этом пути не существовало. Выведенное имя вело на хост,
// которого нет ни на одном стенде, и край, поднявшись готовым, отвергал каждый
// токен при первом же запросе — оператор видел зелёный выкат, арендатор
// сплошное 401.
//
// Отказы двух путей названы РАЗНЫМИ текстами: «не объявлено» и «объявлено, но
// элементов ноль» чинятся по-разному, и отказ, не различающий их, отправлял бы
// оператора искать опечатку там, где строки нет вовсе.
package config

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/PRO-Robotech/corelib/tokenpolicy"
)

const (
	// knobIssuers / knobDeclaredKeySets — настройки объявления приёма.
	// Названы константами, потому что их называет ОТКАЗ, а отказ, указывающий
	// не на ту настройку, хуже отсутствующего.
	knobIssuers         = "KACHO_API_GATEWAY_TOKEN_ISSUERS"
	knobDeclaredKeySets = "KACHO_API_GATEWAY_TOKEN_ISSUER_KEYSETS"

	// TokenTypePlatform — тип токена доступа НАШЕЙ чеканки (RFC 9068).
	// Значение НЕ объявляется здесь второй раз: оно живёт в `corelib/tokenpolicy`,
	// и второе объявление одного значения расходится с первым при первой же
	// правке одного из двух — молча.
	TokenTypePlatform = tokenpolicy.TokenTypeAccess
	// TokenTypeLegacy — тип, которым помечает свои токены прежний издатель.
	TokenTypeLegacy = tokenpolicy.TokenTypeLegacy
)

// TokenIssuerBinding — ОБЪЯВЛЕННАЯ запись «издатель → источник его набора».
type TokenIssuerBinding struct {
	// Issuer — точное значение `iss`. Служит ТОЛЬКО ключом поиска.
	Issuer string
	// KeySetURL — объявленный адрес набора проверочных ключей этого издателя.
	KeySetURL string
	// TokenTypes — закрытый набор принимаемых значений заголовка `typ`.
	TokenTypes []string
	// TolerateAbsentTokenType — принимать токен этой полосы без заголовка
	// типа. Несовпадающий тип отвергается всё равно.
	TolerateAbsentTokenType bool
	// ReadRevocation — читать НАШ авторитет отзыва на предъявлении токена
	// этого издателя.
	ReadRevocation bool
}

// isProductionPosture — режимы, в которых послаблений нет. Тот же разделитель
// классов, что у соседних стражей старта края: только явные метки разработки
// терпят послабление, а пустая или ошибочная метка — производственная.
func (c Config) isProductionPosture() bool {
	switch strings.ToLower(strings.TrimSpace(c.AppEnv)) {
	case "dev", "local", "test":
		return false
	}
	return true
}

// AcceptedTokenIssuers возвращает ЭЛЕМЕНТЫ перечня принимаемых издателей.
//
// Пустые элементы отбрасываются намеренно: значение «,» непусто как строка и
// пусто как перечень, и именно это различие обязан видеть страж старта.
func (c Config) AcceptedTokenIssuers() ([]string, error) {
	out := make([]string, 0, 2)
	seen := map[string]bool{}
	for _, raw := range strings.Split(c.TokenIssuers, ",") {
		iss := strings.TrimSpace(raw)
		if iss == "" {
			continue
		}
		if seen[iss] {
			return nil, fmt.Errorf("KACHO_API_GATEWAY_TOKEN_ISSUERS names issuer %q twice — "+
				"one issuer, one record", iss)
		}
		seen[iss] = true
		out = append(out, iss)
	}
	return out, nil
}

// TokenIssuerKeySetMap разбирает привязку «издатель → адрес набора».
func (c Config) TokenIssuerKeySetMap() (map[string]string, error) {
	out := map[string]string{}
	for _, raw := range strings.Split(c.TokenIssuerKeySets, ",") {
		pair := strings.TrimSpace(raw)
		if pair == "" {
			continue
		}
		// Разделяем по ПЕРВОМУ знаку равенства: он встречается и внутри адреса
		// (строка запроса), и там частью разделителя не является.
		issuer, keySetURL, ok := strings.Cut(pair, "=")
		if !ok {
			return nil, fmt.Errorf("KACHO_API_GATEWAY_TOKEN_ISSUER_KEYSETS entry %q is not «issuer=url»", pair)
		}
		issuer, keySetURL = strings.TrimSpace(issuer), strings.TrimSpace(keySetURL)
		if issuer == "" {
			return nil, fmt.Errorf("KACHO_API_GATEWAY_TOKEN_ISSUER_KEYSETS entry %q names no issuer", pair)
		}
		if _, dup := out[issuer]; dup {
			return nil, fmt.Errorf("KACHO_API_GATEWAY_TOKEN_ISSUER_KEYSETS names issuer %q twice — "+
				"one issuer, one key-set record", issuer)
		}
		if err := absoluteKeySetURL(keySetURL); err != nil {
			return nil, fmt.Errorf("KACHO_API_GATEWAY_TOKEN_ISSUER_KEYSETS record for issuer %q: %w",
				issuer, err)
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
		// «не разбирается» на ОБЪЯВЛЕННОМ пути и на ЗАПАСНОМ — два разных стража,
		// и тексты у них разные намеренно: иначе отказ не сказал бы, какую из
		// двух настроек править, а различитель пробы стал бы общим для обоих.
		return fmt.Errorf("key-set URL %q is not a parseable URL: %w", raw, err)
	}
	if u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("key-set URL %q is not absolute (scheme and host are required; "+
			"a relative path cannot be a declared source)", raw)
	}
	return nil
}

// declaresIssuerSet отвечает, объявил ли профиль перечень издателей вообще.
//
// Различие «не задано» / «задано и вырождено» решается ДО отбрасывания пустых
// элементов, и исход у обоих один — отказ. Различаются ТЕКСТЫ: значение из
// одних пробелов или запятых оператор набрал, и отказ обязан назвать, сколько в
// нём символов и сколько элементов, а незаданное набрать забыли.
func (c Config) declaresIssuerSet() bool { return c.TokenIssuers != "" }

// TokenAcceptance возвращает записи приёма и отвергает объявление, с которым
// край не поднимется.
//
// Порядок — от того, без чего не построить ничего, к тому, что уточняет
// посадку: перечень издателей объявлен → непуст → привязка «издатель →
// источник» → защищённость адресов → авторитет отзыва.
//
// Место, пройденное не полностью, даёт отказ проверки при ПЕРВОМ ЖЕ ЗАПРОСЕ
// вместо отказа при СТАРТЕ. Разница не косметическая: первый виден арендатору и
// не виден оператору, второй виден оператору и не доходит до арендатора.
func (c Config) TokenAcceptance() ([]TokenIssuerBinding, error) {
	if !c.declaresIssuerSet() {
		// Отказ не зависит от режима: край, который не знает, чьи токены он
		// проверяет, не становится законным оттого, что стенд назвали
		// разработческим.
		return nil, fmt.Errorf("%s is not declared: the edge has no accepted token issuer, and "+
			"there is nothing to derive one from — declare the issuers whose tokens this "+
			"installation accepts, each with its key-set record in %s", knobIssuers, knobDeclaredKeySets)
	}

	issuers, err := c.AcceptedTokenIssuers()
	if err != nil {
		return nil, err
	}
	if len(issuers) == 0 {
		// Считаются ЭЛЕМЕНТЫ, а не длина строки, и сообщение называет обе
		// величины: у «,» длина 1 и элементов ноль, и именно на таком входе
		// предикат по длине молчит. Отказ не зависит от режима — пустой
		// перечень означает «принимаем любого издателя», а тогда проходит токен
		// любой третьей стороны, разделяющей с нами набор ключей и адресата.
		return nil, fmt.Errorf("KACHO_API_GATEWAY_TOKEN_ISSUERS declares no issuer element "+
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
			return nil, fmt.Errorf("KACHO_API_GATEWAY_PLATFORM_TOKEN_ISSUER names %q, which "+
				"KACHO_API_GATEWAY_TOKEN_ISSUERS does not accept — the platform would mint tokens "+
				"this edge rejects on the first request", platform)
		}
	}

	out := make([]TokenIssuerBinding, 0, len(issuers))
	for _, iss := range issuers {
		keySetURL, ok := keySets[iss]
		if !ok {
			return nil, fmt.Errorf("issuer %q is accepted (KACHO_API_GATEWAY_TOKEN_ISSUERS) but has "+
				"no declared key-set record (KACHO_API_GATEWAY_TOKEN_ISSUER_KEYSETS); an issuer "+
				"without a record resolves to nothing, and deriving its address from the issuer "+
				"string is forbidden (the issuer comes from the presenter)", iss)
		}
		b := legacyBinding(iss, keySetURL)
		if iss == platform {
			// НАША полоса: производитель типа — мы сами, отсутствие типа
			// означало бы, что мы не выпускаем того, что требуем; и отзыв
			// нашего токена знает только наш авторитет.
			b.TokenTypes = []string{TokenTypePlatform}
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
			return nil, fmt.Errorf("KACHO_API_GATEWAY_TOKEN_ISSUER_KEYSETS declares a key-set record "+
				"for issuer %q, which KACHO_API_GATEWAY_TOKEN_ISSUERS does not accept — a record "+
				"nobody reads outlives its subject silently", iss)
		}
	}

	readsRevocation := false
	for _, b := range out {
		if err := c.requireSecureKeySetURL(b); err != nil {
			return nil, err
		}
		readsRevocation = readsRevocation || b.ReadRevocation
	}
	if readsRevocation {
		if err := c.requirePlatformRevocationAuthority(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// legacyBinding — запись полосы издателя, чья чеканка НЕ наша.
//
// Тип сверяется, если объявлен, и не требуется, если не объявлен. Его токены
// чеканим не мы, форму заголовка диктует он, и требовать от неё того, чего мы у
// него не проверяли, значило бы поставить работу живого контура на
// непроверенное допущение о третьей стороне. Защиты строгость здесь не
// добавляет: подпись, издатель, адресат и привязка ключа уже отвергли бы чужой
// токен.
//
// Принимаемых значений ДВА, и вот чем это обосновано — ровно настолько, насколько
// обосновано, и ни словом больше. Дерево НАЗЫВАЕТ оба: одно закреплено предикатом
// приёма у первой конфигурации, другое названо давним наблюдением в разборе
// заголовка на самом крае. Который из них провайдер ставит на самом деле —
// вопрос НАБЛЮДЕНИЯ на поднятом стенде, а не чтения дерева, и он заведён
// предметом (задача продукта #953).
//
// Отсюда и выбор набора вместо единственного значения: пока наблюдения нет,
// единственное значение — это ставка, и проигранная ставка означает отказ
// КАЖДОМУ запросу той полосы, на которой форму заголовка выбираем не мы. Набор
// же не ослабляет ничего: издатель уже выбрал полосу, а подпись, адресат и
// привязка ключа отвергли бы чужой токен независимо от типа.
//
// ПРЕДИКАТ СНЯТИЯ: послабление и второе значение уходят вместе с самой записью
// прежнего издателя.
func legacyBinding(issuer, keySetURL string) TokenIssuerBinding {
	return TokenIssuerBinding{
		Issuer:                  issuer,
		KeySetURL:               keySetURL,
		TokenTypes:              []string{TokenTypeLegacy, TokenTypePlatform},
		TolerateAbsentTokenType: true,
	}
}

// requireSecureKeySetURL — источник набора проверочных ключей есть единственный
// якорь доверия проверки подписи. По открытому HTTP его документ подменяется на
// пути, и тогда подделывается токен под любого субъекта — то есть проверка
// подлинности обходится целиком.
//
// В режиме разработки открытый HTTP допустим — симметрично незашифрованному
// соединению к базе.
func (c Config) requireSecureKeySetURL(b TokenIssuerBinding) error {
	if !c.isProductionPosture() {
		return nil
	}
	// Ошибка разбора здесь не спрашивается, и это не пропуск: адрес каждой
	// записи уже разобран и признан абсолютным при чтении привязки
	// (TokenIssuerKeySetMap → absoluteKeySetURL), и запись, не прошедшая разбор,
	// до этой строки не доходит. Второй отказ о том же входе был бы веткой,
	// которую не достигает ни одно объявление.
	u, _ := url.Parse(b.KeySetURL)
	if !strings.EqualFold(u.Scheme, "https") {
		return fmt.Errorf("KACHO_APP_ENV=%q requires an https:// key-set URL in %s "+
			"for issuer %q (the key set is the trust anchor of signature verification and must "+
			"not be fetched over plaintext; got scheme %q)",
			c.AppEnv, knobDeclaredKeySets, b.Issuer, u.Scheme)
	}
	return nil
}

// requirePlatformRevocationAuthority — наш издатель принимается, значит отзыв
// обязан иметь читателя на пути запроса.
//
// Адрес задаётся ЯВНО. Умолчание вида «взять базовый адрес соседа и приклеить
// путь» запрещено: оно всегда непусто, поэтому контроль выглядит включённым,
// ведя в никуда, и ни один профиль развёртывания не обязан ничего задавать,
// чтобы это заметить.
func (c Config) requirePlatformRevocationAuthority() error {
	trimmed := strings.TrimSpace(c.PlatformTokenRevocationURL)
	if trimmed == "" {
		return fmt.Errorf("KACHO_API_GATEWAY_PLATFORM_TOKEN_ISSUER is accepted, so "+
			"KACHO_API_GATEWAY_PLATFORM_TOKEN_REVOCATION_URL is required: a control that acts only "+
			"where the credential is ISSUED is not revocation — it merely declines to issue a new "+
			"one (KACHO_APP_ENV=%q)", c.AppEnv)
	}
	u, err := url.Parse(trimmed)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("KACHO_API_GATEWAY_PLATFORM_TOKEN_REVOCATION_URL must be absolute "+
			"(got %q); it is declared explicitly and never derived from a neighbour's address",
			c.PlatformTokenRevocationURL)
	}
	if !c.isProductionPosture() {
		return nil
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return fmt.Errorf("KACHO_APP_ENV=%q requires an https:// "+
			"KACHO_API_GATEWAY_PLATFORM_TOKEN_REVOCATION_URL (the answer decides access and must "+
			"not transit plaintext; got scheme %q)", c.AppEnv, u.Scheme)
	}
	return nil
}
