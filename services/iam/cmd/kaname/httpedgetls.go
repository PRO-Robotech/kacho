// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: AGPL-3.0-or-later

// httpedgetls.go — HTTP-рёбра службы в боевой посадке обязаны нести TLS.
//
// # Предмет, и он появился ВМЕСТЕ С ВЫНОСОМ
//
// Ручки транспорта у трёх HTTP-рёбер — вебхуки провайдера, скрейп и зеркало
// ключей проверки — объявлены кодом давно, но задавал их ЗОНТИЧНЫЙ чарт
// монорепо. Профиль ЭТОЙ службы их не объявляет, а адреса всех трёх приходят
// умолчанием процесса и потому непусты всегда: отдельно поставленная служба
// поднимала три слушателя ОТКРЫТЫМ ТЕКСТОМ, и заметить это было неоткуда —
// умолчание `Enable=false` означает «открытый текст», а не «выключено».
//
// Цена каждого ребра названа отдельно, потому что она разная:
//
//   - ВЕБХУКИ. По проводу идёт общий секрет провайдера — тот самый, которым
//     обработчик отличает провайдера от постороннего. Снятый с провода, он
//     позволяет звать заведение пользователя и обогащение токена от чужого
//     имени;
//   - ЗЕРКАЛО КЛЮЧЕЙ. Снятие аутентификации с этой поверхности —
//     задокументированное исключение, и обосновано оно ТРЕМЯ вещами разом:
//     внутренний Service, односторонняя TLS, только публичный материал. Без
//     TLS предпосылка собственного исключения ложна: остаётся
//     неаутентифицированный слушатель открытым текстом, чьи ключи плоскость
//     данных реестра принимает как решающие, чью подпись верить;
//   - СКРЕЙП. Счётчики процесса — внутренняя кардинальность, которую
//     `security.md` держит вне публичной поверхности; открытый текст выносит её
//     всякому, кто слушает сеть пода.
//
// # Почему у докерной полосы страж СВОЙ, а не эта функция
//
// `requireRegistryTokenTLS` отказывает своим текстом, и текст этот несущий: по
// той ноге едет ПРИВАТНЫЙ КЛЮЧ ключа служебной учётки, у которого нет ни срока,
// ни ротации, — ущерб не ограничен ничем, в отличие от короткоживущего
// предъявителя. Свести обе функции значило бы либо потерять этот довод, либо
// повторить его здесь вторым местом об одном предмете. Отдельный страж с
// отдельной причиной — решение, а не дрейф.
//
// # Отказ называет ВСЕ рёбра сразу
//
// Оператор чинит профиль один раз, а не по одному ребру за перезапуск: страж,
// останавливающийся на первом, продаёт три круга подъёма вместо одного.
package main

import (
	"errors"
	"fmt"
	"strings"
)

// httpEdgeTLS — HTTP-ребро, чей транспорт судится стражем старта.
type httpEdgeTLS struct {
	// name — как ребро зовётся в журнале и в тексте отказа.
	name string
	// knob — ручка, которой ребро включается. Отказ обязан её назвать: иначе
	// оператор знает, что не так, и не знает, где это чинить.
	knob string
	// addr — адрес слушателя. Пустой ⇒ слушатель не поднимается, судить нечего.
	addr string
	// enabled — объявлен ли транспорт.
	enabled bool
	// why — ЧТО едет по этому проводу. Текст отказа без этого не отличим от
	// придирки, и первый же оператор снимет ручку, а не заведёт материал.
	why string
}

// requireHTTPEdgeTLS — в боевой посадке ни одно поднимаемое HTTP-ребро не
// работает открытым текстом.
//
// В не-боевой посадке — no-op: тот же порядок, что у прочих рёбер (умолчание
// выключено, стенд байт-идентичен). Пустой адрес — no-op: слушателя нет.
//
// Пустой перечень рёбер — ОТКАЗ, а не тишина: «стражу не на что жаловаться»
// обязано быть отличимо от «стражу не дали ничего».
func requireHTTPEdgeTLS(productionMode bool, edges []httpEdgeTLS) error {
	if len(edges) == 0 {
		return errors.New("страж транспорта HTTP-рёбер позван с пустым перечнем: " +
			"вердикт беспредметен — «нарушений нет» неотличимо от «рёбер не передано ни одного»")
	}
	if !productionMode {
		return nil
	}
	var refusals []error
	for _, e := range edges {
		if strings.TrimSpace(e.addr) == "" || e.enabled {
			continue
		}
		refusals = append(refusals, fmt.Errorf(
			"production mode requires TLS on the %s listener %s (set %s=true with its cert/key): %s; "+
				"refusing to start with it in the clear",
			e.name, e.addr, e.knob, e.why))
	}
	return errors.Join(refusals...)
}

// iamHTTPEdges — перечень рёбер, чей транспорт судится. ВЫВОДИТСЯ из той же
// настройки, из которой поднимаются слушатели, и живёт в ОДНОМ месте: подъём и
// проба профиля зовут его оба, поэтому разойтись им нечем.
//
// Докерной полосы здесь НЕТ намеренно — у неё свой страж со своей причиной
// (см. шапку файла).
func iamHTTPEdges(hooksAddr, metricsAddr, jwksProxyAddr, restAddr, internalRESTAddr string,
	mtlsCfg mtlsEnableReader) []httpEdgeTLS {
	return []httpEdgeTLS{
		{
			name: "identity-provider hooks", knob: "KANAME_HOOKS_SERVER_MTLS_ENABLE",
			addr: hooksAddr, enabled: mtlsCfg.HooksTLSEnabled(),
			why: "the identity provider's shared secret travels this hop — the very value the " +
				"handler uses to tell the provider from a stranger",
		},
		{
			name: "verification-key mirror (/.well-known/jwks.json)", knob: "KANAME_JWKSPROXY_SERVER_MTLS_ENABLE",
			addr: jwksProxyAddr, enabled: mtlsCfg.JWKSProxyTLSEnabled(),
			why: "this surface carries NO authentication by documented exception, and that " +
				"exception rests on one-way TLS: without it an unauthenticated plaintext " +
				"listener decides whose signatures the registry data-plane trusts",
		},
		{
			name: "собственный публичный REST-фронт", knob: "KANAME_REST_SERVER_MTLS_ENABLE",
			addr: restAddr, enabled: mtlsCfg.RESTTLSEnabled(),
			why: "по этому проводу идёт ПРЕДЪЯВЛЕННОЕ УДОСТОВЕРЕНИЕ арендатора — то самое, " +
				"которым он называет себя платформе. Снятое с провода, оно предъявляется " +
				"повторно кем угодно до истечения срока, и отличить такой вызов от " +
				"настоящего нечем: подпись верна",
		},
		{
			name: "собственный внутренний REST-фронт", knob: "KANAME_INTERNALREST_SERVER_MTLS_ENABLE",
			addr: internalRESTAddr, enabled: mtlsCfg.InternalRESTTLSEnabled(),
			why: "внутренний периметр доверенным не считается (defense-in-depth против " +
				"бокового движения): по этому проводу идут служебные поверхности, " +
				"а открытый текст выносит их всякому, кто слушает сеть пода",
		},
		{
			name: "metrics scrape", knob: "KANAME_METRICS_SERVER_MTLS_ENABLE",
			addr: metricsAddr, enabled: mtlsCfg.MetricsTLSEnabled(),
			why: "process counters are internal cardinality, kept off any surface a stranger " +
				"can read (security.md)",
		},
	}
}

// mtlsEnableReader — ровно то, что стражу нужно от посадки транспорта: три
// ответа «объявлен ли транспорт». Узкий интерфейс здесь не украшение — он
// позволяет пробе подать посадку, не собирая её из окружения.
type mtlsEnableReader interface {
	HooksTLSEnabled() bool
	MetricsTLSEnabled() bool
	JWKSProxyTLSEnabled() bool
	RESTTLSEnabled() bool
	InternalRESTTLSEnabled() bool
}
