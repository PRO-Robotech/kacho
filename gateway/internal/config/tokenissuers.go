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
// # Почему у края «не объявлено» и «объявлено пустым» дают ОДИН исход
//
// Прежде их было три, и различал их вывод адреса: край СТРОИЛ издателя из
// домена API, когда перечень не объявлен, поэтому «ручка не задана» было
// работающим состоянием, а не забытой настройкой. Работающим оно только
// выглядело: выведенный хост `https://hydra.<домен>` на стенде не существует,
// набор ключей по нему не приезжает никогда, и край отвергает каждый
// предъявленный токен при первом же запросе. Отказа старта на этом пути не было
// НИ ПОД ОДНОЙ меткой окружения, включая производственную, — то есть оператор
// видел зелёный выкат и готовый под, а арендатор получал 401, неотличимый от
// неверных учётных данных.
//
// Ветвь снята вместе с обеими ручками пина, и состояний осталось два:
//
//   - не задано ЛИБО задано и даёт НОЛЬ элементов ⇒ отказ в старте,
//     безусловный. Основание у двух половин разное — у первой «адрес неоткуда
//     взять, а выводить его запрещено», у второй «пустой перечень означает
//     принимаем любого издателя», — но исход один, и он не зависит от метки
//     окружения: послабление означало бы «на стенде разработки адрес выводим»,
//     то есть ровно снятую ветвь;
//   - задано и даёт элементы ⇒ принимаются ровно они, у каждого своя запись.
//
// Снятие это разрушило ПАРУ: утверждение «перечень объявлен каждым стендом»
// держалось профилями и проверялось стражем развёртывания
// (gateway/deploy/f1c_issuer_set_reaches_every_stand_test.go), а код на него
// только опирался. Теперь оно держится самим кодом и стало безусловным.
package config

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/PRO-Robotech/corelib/tokenpolicy"
)

const (
	// knobDeclaredKeySets / knobLegacyKeySet — настройки, из которых может
	// приехать адрес набора. Названы константами, потому что их называет ОТКАЗ,
	// а отказ, указывающий не на ту настройку, хуже отсутствующего: он
	// отправляет оператора править то, что в этой посадке пусто.
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
	// SourceKnob — настройка, ИЗ КОТОРОЙ приехал адрес набора.
	//
	// Нужна отказу, а не логике: отказ, называющий не ту настройку, отправляет
	// оператора править то, что в этой посадке пусто. Источник сегодня ОДИН —
	// объявленный перечень, — и поле оставлено именно потому, что источников
	// снова может стать больше: значение, вписанное в текст отказа литералом,
	// разошлось бы с правдой молча в тот день, когда второй источник появится.
	SourceKnob string
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
		// Перечень не объявлен ⇒ ОТКАЗ В СТАРТЕ, и он БЕЗУСЛОВЕН.
		//
		// Прежде здесь строилась одна запись приёма: издатель и адрес набора
		// брались из скалярного пина, а при пустом пине ВЫВОДИЛИСЬ из домена
		// установки. Обе ручки сняты вместе с этой ветвью, поэтому адрес брать
		// неоткуда — и выводить его запрещено по той же причине, по какой он не
		// выводится из строки издателя ниже: производный адрес непуст ВСЕГДА,
		// поэтому состояние «адреса нет» не наступает никогда, а страж старта
		// остаётся в тексте, не имея возможности упасть.
		//
		// Отказ называет ту ручку, которую оператору править, и не называет
		// снятых: отказ, отправляющий править несуществующую настройку, хуже
		// отсутствующего.
		return nil, fmt.Errorf("KACHO_API_GATEWAY_TOKEN_ISSUERS declares no issuer set "+
			"(KACHO_APP_ENV=%q): the edge accepts an issuer only by a declared record, and the "+
			"address of a key set is never derived — a derived address is non-empty for EVERY "+
			"issuer, so «no record» would never occur and this guard could never fire. Declare "+
			"the issuer set together with KACHO_API_GATEWAY_TOKEN_ISSUER_KEYSETS", c.AppEnv)
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
		b := legacyBinding(iss, keySetURL, knobDeclaredKeySets)
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
func legacyBinding(issuer, keySetURL, sourceKnob string) TokenIssuerBinding {
	return TokenIssuerBinding{
		Issuer:                  issuer,
		KeySetURL:               keySetURL,
		SourceKnob:              sourceKnob,
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
	// ВТОРОГО РАЗБОРА АДРЕСА ЗДЕСЬ НЕТ, И ЭТО НЕ ЭКОНОМИЯ.
	//
	// Он тут был и нёс СВОЙ отказ «адрес не разбирается». Дойти до этого отказа
	// умел ровно один вход — запасной путь, где адрес приезжал скалярным пином
	// и формы не проверял никто. Путь снят, и вход стал непредставим: каждая
	// запись, доезжающая сюда, уже прошла `absoluteKeySetURL`, который разбирает
	// адрес и требует схемы с хостом.
	//
	// Ветвь, вход которой непредставим, снимается вместе со своим предметом.
	// Оставленная «на всякий случай», она замолкает МОЛЧА: отрицательный кейс к
	// ней зеленеет на отказе соседа, снятие самого стража не роняет ничего, а
	// перепись отказов в разборе начинает считать то, чего нет
	// (f1b_guard_discrimination_test.go).
	//
	// Схема берётся отсечением по «://» и разбора не требует: у адреса, который
	// `absoluteKeySetURL` принял, схема и хост непусты оба, а значит разделитель
	// в строке ЕСТЬ.
	scheme, _, _ := strings.Cut(b.KeySetURL, "://")
	if !strings.EqualFold(scheme, "https") {
		return fmt.Errorf("KACHO_APP_ENV=%q requires an https:// key-set URL in %s "+
			"for issuer %q (the key set is the trust anchor of signature verification and must "+
			"not be fetched over plaintext; got scheme %q)",
			c.AppEnv, b.SourceKnob, b.Issuer, scheme)
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
