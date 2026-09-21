// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package main — страж старта полосы отзыва.
//
// Проверка подписи доказывает, КТО удостоверение отчеканил и когда оно истечёт.
// Она НЕ доказывает, что удостоверение ещё годно: выход, отозванный машинный
// ключ и обратный вызов выхода оставляют подпись целой. Авторитет по этому
// вопросу один — тот, кто удостоверение чеканил, — а чеканим их МЫ.
//
// ЗДЕСЬ СУДИЛАСЬ ЕЩЁ И ОСЬ ЧУЖОГО ПОСТАВЩИКА: адрес его интроспекции,
// спрашиваемый на каждом запросе, и административный адрес, по которому выход
// снимал сессию входа на его стороне. Ось снята вместе с самим поставщиком —
// требовать адресов там, где спрашивать некого, страж не вправе, а послабление
// оси стало бы способом выключить чтение отзыва, ничего об этом не объявляя.
//
// Незаданный адрес нашего авторитета — не нейтральное умолчание, а выключенный
// контроль. Этот страж отказывает в старте производственному краю в таком
// состоянии и отвергает адрес, чья форма или транспорт показывают, что
// ответ на нём решать о доступе не может.
package main

import (
	"fmt"
	"net/url"
	"strings"
)

// RevocationConfig is the cross-section of the configuration this guard reads:
// the two admin-API addresses the revocation path depends on.
//
// Kept as a small value-type at the composition root — like AuthzMiddlewareConfig
// — so the validator is testable without pulling the middleware/clients graph
// into a `package main` test binary.
type RevocationConfig struct {
	// ЗДЕСЬ БЫЛИ ЧЕТЫРЕ ВЕЛИЧИНЫ ЧУЖОЙ ОСИ — адрес интроспекции, адрес его
	// административного API, якорь доверия хопа к нему и объявленная профилем
	// посадка личности, разводившая требование второго из них. Сняты вместе с
	// самой осью.

	// ─── НАШ авторитет отзыва: единственная ось этого стража ────────────────
	//
	// Полоса не СНИМАЕТ требование читать отзыв на предъявлении — она называет
	// того, у кого спрашивают. Токены чеканим мы, и никто другой о них не знает
	// by construction.

	// PlatformRevocationURL — адрес НАШЕГО авторитета отзыва
	// (KACHO_API_GATEWAY_PLATFORM_TOKEN_REVOCATION_URL; пусто ⇒ не задан).
	PlatformRevocationURL string
	// PlatformRevocationCAFile — якорь доверия хопа к нашему авторитету.
	PlatformRevocationCAFile string
	// PlatformRevocationCertFile / PlatformRevocationKeyFile — ЧЕМ край
	// представляется нашему авторитету. Авторитет спрашивает клиентский
	// сертификат, поэтому хоп без пары — контроль, отказывающий ВСЕГДА и по
	// одной и той же причине.
	PlatformRevocationCertFile string
	PlatformRevocationKeyFile  string
}

// Ручки НАШЕГО авторитета отзыва. Объявлены здесь ИМЕНАМИ, потому что их
// называет текст отказа: оператор обязан узнать, что править, не открывая
// исходник (одно из трёх мест, выведенных из-под запрета на публичный разбор,
// `security.md` §«Публичные артефакты»).
const (
	platformRevocationURLKnob  = "KACHO_API_GATEWAY_PLATFORM_TOKEN_REVOCATION_URL"
	platformRevocationCAKnob   = "KACHO_API_GATEWAY_PLATFORM_TOKEN_REVOCATION_CA_FILE"
	platformRevocationCertKnob = "KACHO_API_GATEWAY_PLATFORM_TOKEN_REVOCATION_CERT_FILE"
	platformRevocationKeyKnob  = "KACHO_API_GATEWAY_PLATFORM_TOKEN_REVOCATION_KEY_FILE"
)

// judgeOurRevocationAuthority — ось НАШЕГО авторитета отзыва.
//
// # Почему требование не снимается вместе с поставщиком
//
// Отзыв, действующий на выдаче и не действующий на предъявлении, отзывом не
// является: предъявленное продолжает проходить до истечения срока, и это
// состояние не сходится само (`security.md` §«Контроль, действующий на ВЫДАЧЕ,
// но не на ПРЕДЪЯВЛЕНИИ»). Снятие чужого поставщика поэтому ЗАМЕЩАЕТ авторитет,
// а не отменяет вопрос.
//
// # Требование БЕЗУСЛОВНО
//
// Прежде наличие адреса требовалось только под посадкой `own`, а под `external`
// на вопрос отвечал чужой поставщик. Поставщика нет — второго ответа тоже, и
// разводить требование нечем.
//
// # Пара предъявления обязательна вместе с адресом
//
// Авторитет спрашивает клиентский сертификат. Хоп, которому нечего предъявить,
// получает отказ на КАЖДОМ запросе и по одной и той же причине — то есть
// контроль, объявленный, провязанный и не отказавший ни разу по существу
// (`security.md` §«Контроль, у которого нет МЕХАНИЗМА исполниться»). Половина
// пары отвергается отдельно: она хуже отсутствия обеих, потому что выглядит
// настроенной.
func judgeOurRevocationAuthority(cfg RevocationConfig) []string {
	addr := strings.TrimSpace(cfg.PlatformRevocationURL)
	if addr == "" {
		// Развилки по посадке здесь больше нет: посадка была одна из двух, и
		// вторая — «отзыв читает чужой поставщик» — снята вместе с ним. Требование
		// стало БЕЗУСЛОВНЫМ, и это не ужесточение, а исчезновение второго ответа.
		return []string{
			platformRevocationURLKnob + " is empty — we mint the tokens and nobody else can " +
				"be asked whether one was revoked, so a revoked token would keep working " +
				"until it expires on its own",
		}
	}

	var problems []string
	if err := validateAdminEndpoint(addr); err != nil {
		problems = append(problems, platformRevocationURLKnob+" "+err.Error())
	} else if p := validateAdminHopTransport(
		platformRevocationURLKnob, platformRevocationCAKnob, addr, cfg.PlatformRevocationCAFile); p != "" {
		// ТОТ ЖЕ предикат, что у административного хопа, а не его копия: ручка
		// подаётся параметром. Второй экземпляр правила разъехался бы молча — и
		// разъехался бы тот, где дефект ещё не нашли.
		problems = append(problems, p)
	}

	cert := strings.TrimSpace(cfg.PlatformRevocationCertFile)
	key := strings.TrimSpace(cfg.PlatformRevocationKeyFile)
	switch {
	case cert == "" && key == "":
		problems = append(problems,
			platformRevocationCertKnob+" and "+platformRevocationKeyKnob+" are both empty — "+
				"our revocation authority asks the caller for a client certificate, so a hop "+
				"with nothing to present is answered the same way every time and the check "+
				"refuses every presenter of our own minting; declare the pair together with "+
				platformRevocationURLKnob)
	case key == "":
		problems = append(problems,
			platformRevocationCertKnob+" is set without "+platformRevocationKeyKnob+
				" — half a pair presents nothing")
	case cert == "":
		problems = append(problems,
			platformRevocationKeyKnob+" is set without "+platformRevocationCertKnob+
				" — a key with no certificate has nothing to present")
	}
	return problems
}

// validateAdminHopTransport refuses a production-class admin hop that carries a
// credential readable on the wire, and a TLS one that verifies nothing.
//
// WHY PLAINTEXT IS REFUSED HERE AND NOT MERELY WARNED. This hop is taken on
// every authenticated request that misses the short-TTL cache, and the authority
// is asked about a bearer by SENDING it. So the wire carries a live end-user
// credential — and a bearer read off the wire is usable by whoever read it, for
// as long as it lives. That is a tenant-data question, which is why it fails the
// start rather than logging.
//
// WHY TLS WITHOUT AN ANCHOR IS REFUSED TOO. The authority's certificate on an
// in-cluster address is issued by the internal CA, and this process trusts the
// system roots by default. An operator who moves the address to https and stops
// there gets an unknown-authority handshake, which the introspection layer
// classifies as a PERMANENT misconfiguration and answers by refusing every
// request (see permanentTransportFailure in the middleware). Refusing at boot
// turns a fleet-wide outage discovered in production into a message at the one
// moment the operator is looking.
//
// The scheme is read from the address itself rather than from a separate switch:
// a switch could disagree with the address, and then the two would have to be
// kept in step by hand.
func validateAdminHopTransport(knob, caKnob, raw, caFile string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" {
		return "" // shape is reported by validateAdminEndpoint
	}
	if u.Scheme == "http" {
		return knob + " is plaintext (" + raw + ") — the revocation check asks about a " +
			"bearer by SENDING it, on every authenticated request that misses the cache, so " +
			"this hop carries a live end-user credential in the clear and anything on the path " +
			"can read and reuse it; address the provider's admin API over https"
	}
	if u.Scheme == "https" && strings.TrimSpace(caFile) == "" {
		return knob + " is https (" + raw + ") but " + caKnob + " is empty — the " +
			"peer's in-cluster certificate is issued by the internal CA and this process " +
			"trusts the system roots, so every handshake fails with an unknown authority; the " +
			"introspection layer treats that as a permanent misconfiguration and then refuses " +
			"EVERY request. Pin the bundle together with the address"
	}
	return ""
}

// validateProductionRevocationConfig refuses to start when the deploy
// environment is production-class AND the revocation path is unconfigured or
// misaddressed.
//
// Only the explicit dev-class labels ("dev" / "local" / "test") tolerate an
// unconfigured path — a local stand may run with no reachable admin API at all.
// Every OTHER value, including an empty/unset label, is production-class and is
// validated: a deploy that forgets KACHO_APP_ENV still fails closed rather than
// silently skipping the guard, exactly as the sibling authz and internal-listener
// guards do.
func validateProductionRevocationConfig(env string, cfg RevocationConfig) error {
	switch strings.ToLower(strings.TrimSpace(env)) {
	case "dev", "local", "test":
		return nil
	}

	// ЗДЕСЬ СУДИЛИСЬ ТРИ ЧУЖИЕ ОСИ — объявленность посадки личности, адрес
	// интроспекции поставщика и его административный адрес. Они сняты вместе со
	// своим предметом: посадка разводила требование административного адреса, а
	// оба адреса вели к поставщику, которого край больше не спрашивает ни о чём.
	problems := judgeOurRevocationAuthority(cfg)

	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf(
		"revocation path invalid in %q env: %s (refuse to start)",
		env, strings.Join(problems, "; "),
	)
}

// validateAdminEndpoint checks that an address is a usable absolute URL.
//
// ЗДЕСЬ БЫЛА ВТОРАЯ ПОЛОВИНА — сверка ТОЧНОГО пути. Она отделяла
// административный API чужого поставщика от его же публичного: публичный не
// отдаёт интроспекции никому, и адрес, целящий туда, отвечал бы не-ответом на
// каждую проверку. Единственный вход этой половины — адрес интроспекции
// поставщика — снят, и ветвь, вход которой непредставим, снимается вместе со
// своим предметом: оставленная, она замолкает МОЛЧА, а её отрицательный кейс
// зеленеет на отказе соседа.
func validateAdminEndpoint(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("is not a valid URL: %v", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("must be an absolute http(s) URL, got %q", raw)
	}
	if u.Host == "" {
		return fmt.Errorf("has no host: %q", raw)
	}
	return nil
}
