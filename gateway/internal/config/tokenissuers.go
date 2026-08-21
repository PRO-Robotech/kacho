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
// # Почему у края «не объявлено» отличается от «объявлено пустым»
//
// Край ВЫВОДИТ издателя из домена API по умолчанию, поэтому «ручка не задана» —
// сегодняшнее, работающее и повсеместное состояние, а не забытая настройка.
// Отсюда три состояния, а не два:
//
//   - не задано ⇒ строится ОДНА запись из сегодняшнего пина и сегодняшнего
//     адреса набора. Множество мощности 1 остаётся сужением;
//   - задано и даёт НОЛЬ элементов ⇒ отказ в старте, безусловный: пустой
//     перечень означает «принимаем любого издателя»;
//   - задано и даёт элементы ⇒ принимаются ровно они, у каждого своя запись.
//
// Двусмысленность (задано и новое объявление, и прежний скалярный пин)
// закрывается ОТКАЗОМ, а не старшинством: молчаливое старшинство означало бы,
// что оператор задаёт значение, оно принимается и не действует.
package config

import (
	"fmt"
	"net/url"
	"strings"
)

const (
	// TokenTypePlatform — тип токена доступа НАШЕЙ чеканки (RFC 9068).
	TokenTypePlatform = "at+jwt"
	// TokenTypeLegacy — тип, которым помечает свои токены прежний издатель.
	TokenTypeLegacy = "JWT"
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
		return fmt.Errorf("key-set URL %q is not a URL: %w", raw, err)
	}
	if u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("key-set URL %q is not absolute (scheme and host are required; "+
			"a relative path cannot be a declared source)", raw)
	}
	return nil
}

// declaresIssuerSet отвечает, объявил ли профиль перечень издателей ЯВНО.
//
// Различие «не задано» / «задано и вырождено» — предмет этой функции, и оно
// намеренно решается ДО отбрасывания пустых элементов: именно на нём предикат
// по длине строки молчит, а предикат по элементам говорит.
func (c Config) declaresIssuerSet() bool { return c.TokenIssuers != "" }

// TokenAcceptance возвращает записи приёма и отвергает объявление, с которым
// край не поднимется.
//
// Порядок — от того, без чего не построить ничего, к тому, что уточняет
// посадку: двусмысленность → перечень издателей → привязка «издатель →
// источник» → защищённость адресов → авторитет отзыва.
//
// Место, пройденное не полностью, даёт отказ проверки при ПЕРВОМ ЖЕ ЗАПРОСЕ
// вместо отказа при СТАРТЕ. Разница не косметическая: первый виден арендатору и
// не виден оператору, второй виден оператору и не доходит до арендатора.
func (c Config) TokenAcceptance() ([]TokenIssuerBinding, error) {
	if !c.declaresIssuerSet() {
		// Перечень не объявлен — сегодняшняя посадка. Одна запись из
		// сегодняшнего пина и сегодняшнего адреса набора; наша чеканка на ней
		// не принимается, потому что не объявлена.
		if strings.TrimSpace(c.PlatformTokenIssuer) != "" {
			return nil, fmt.Errorf("KACHO_API_GATEWAY_PLATFORM_TOKEN_ISSUER names %q, but "+
				"KACHO_API_GATEWAY_TOKEN_ISSUERS declares no issuer set — the platform would mint "+
				"tokens this edge rejects on the first request", c.PlatformTokenIssuer)
		}
		b := legacyBinding(c.ResolvedHydraIssuer(), c.ResolvedHydraJWKSURL())
		// Требование к транспорту источника набора действует и здесь.
		//
		// Асимметрия была бы хуже строгости: объявивший перечень оператор
		// получал бы проверку, а не объявивший — нет, и правильный поступок
		// оказывался бы наказуем. Правило одно, потому что предмет один —
		// источник набора есть единственный якорь доверия проверки подписи, и
		// он не становится безопаснее оттого, что адрес приехал прежней ручкой.
		if err := c.requireSecureKeySetURL(b.Issuer, b.KeySetURL); err != nil {
			return nil, err
		}
		return []TokenIssuerBinding{b}, nil
	}

	// ДВА объявления об одном предмете. Отказ, а не старшинство: значение,
	// принятое и не действующее, хуже отвергнутого — отказ виден сразу, а
	// несделанное только по последствиям.
	if c.HydraIssuer != "" || c.HydraJWKSURL != "" {
		return nil, fmt.Errorf("both KACHO_API_GATEWAY_TOKEN_ISSUERS and the retired scalar pin "+
			"(KACHO_HYDRA_ISSUER=%q / KACHO_HYDRA_JWKS_URL=%q) are set — two declarations of one "+
			"subject. Precedence is not assigned silently: a value that is accepted and does not "+
			"take effect is worse than a refused one. Keep the issuer set and clear the scalar pin",
			c.HydraIssuer, c.HydraJWKSURL)
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
		if err := c.requireSecureKeySetURL(b.Issuer, b.KeySetURL); err != nil {
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

// legacyBinding — запись полосы ПРЕЖНЕГО издателя.
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
func (c Config) requireSecureKeySetURL(issuer, keySetURL string) error {
	if !c.isProductionPosture() {
		return nil
	}
	u, err := url.Parse(keySetURL)
	if err != nil {
		return fmt.Errorf("KACHO_API_GATEWAY_TOKEN_ISSUER_KEYSETS record for issuer %q: "+
			"invalid URL %q: %w", issuer, keySetURL, err)
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return fmt.Errorf("KACHO_APP_ENV=%q requires an https:// key-set URL in "+
			"KACHO_API_GATEWAY_TOKEN_ISSUER_KEYSETS for issuer %q (the key set is the trust anchor "+
			"of signature verification and must not be fetched over plaintext; got scheme %q)",
			c.AppEnv, issuer, u.Scheme)
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
