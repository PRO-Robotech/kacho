// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package main — startup-validation for the revocation path.
//
// Verifying a bearer's signature proves who minted it and when it expires. It
// does NOT prove the token is still good: a sign-out or a revoked machine key
// leaves a perfectly valid signature behind. The authority on that is OURS —
// the revocation authority on the identity service's cluster-internal listener
// for tokens of our own minting, and our revocation record for any other
// accepted record (auth_revocation.go).
//
// THE PREVIOUS PROVIDER'S AXIS IS GONE (#2734). This guard used to demand two
// addresses on that provider's ADMIN API — its introspection endpoint and its
// session-kill base — under the `external` posture. The edge no longer talks to
// that provider at all, so there is nothing left to demand: the guard now
// judges the one hop the edge still makes for revocation, to our authority.
//
// So an unset address is not a neutral default, it is the control switched off.
// This guard refuses to start a production-class gateway in that state, and it
// refuses an address that would carry a live bearer in the clear or over TLS it
// cannot verify.
package main

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/PRO-Robotech/corelib/identityposture"
	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

// RevocationConfig is the cross-section of the configuration this guard reads.
//
// Kept as a small value-type at the composition root — like AuthzMiddlewareConfig
// — so the validator is testable without pulling the middleware/clients graph
// into a `package main` test binary.
type RevocationConfig struct {
	// IdentityProvider — ПОСАДКА ЛИЧНОСТИ, объявленная профилем (задача #1125).
	//
	// Разводит требование НАШЕГО авторитета отзыва: под `own` токены чеканим мы,
	// и спросить, отозван ли токен, можно только у нас. Незаданная посадка —
	// отказ старта первым и в одиночку: пока она неизвестна, неизвестно и то, что
	// требовать.
	IdentityProvider identityposture.Provider

	// ─── НАШ авторитет отзыва ────────────────────────────────────────────────

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
// # Почему требуется под `own`
//
// Отзыв, действующий на выдаче и не действующий на предъявлении, отзывом не
// является: предъявленное продолжает проходить до истечения срока, и это
// состояние не сходится само (`security.md` §«Контроль, действующий на ВЫДАЧЕ,
// но не на ПРЕДЪЯВЛЕНИИ»). Под `own` токены чеканим мы, и кроме нас спросить,
// отозван ли токен, некого.
//
// # Требование НАЛИЧИЯ разведено посадкой, требования ТРАНСПОРТА — нет
//
// Наличие адреса требуется под `own`. Заданный адрес судится теми же правилами
// на ЛЮБОЙ посадке: край, объявивший наш авторитет, его и спрашивает, и
// негодный хоп нерабочий везде одинаково.
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
		if cfg.IdentityProvider != identityposture.Own {
			// Вне `own` наша чеканка краем не принимается, пока её не объявит
			// перечень издателей, — а объявленный наш издатель без авторитета
			// отзыва отвергается раньше, разбором приёма (`TokenAcceptance`).
			return nil
		}
		return []string{
			platformRevocationURLKnob + " is empty — on this posture we mint the tokens and " +
				"nobody else can be asked whether one was revoked, so a revoked token would " +
				"keep working until it expires on its own [required because " +
				config.IdentityProviderKnob + "=own]",
		}
	}

	var problems []string
	if err := validateEndpoint(addr); err != nil {
		problems = append(problems, platformRevocationURLKnob+" "+err.Error())
	} else if p := validateHopTransport(
		platformRevocationURLKnob, platformRevocationCAKnob, addr, cfg.PlatformRevocationCAFile); p != "" {
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

// validateHopTransport refuses a production-class revocation hop that carries a
// credential readable on the wire, and a TLS one that verifies nothing.
//
// WHY PLAINTEXT IS REFUSED HERE AND NOT MERELY WARNED. This hop is taken on every
// authenticated request that misses the short-TTL cache, and introspection asks
// about a bearer by SENDING it. So the wire carries a live end-user credential —
// and a bearer read off the wire is usable by whoever read it, for as long as it
// lives. That is a tenant-data question, which is why it fails the start rather
// than logging.
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
func validateHopTransport(knob, caKnob, raw, caFile string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" {
		return "" // shape is reported by validateEndpoint
	}
	if u.Scheme == "http" {
		return knob + " is plaintext (" + raw + ") — the revocation check asks about a " +
			"bearer by SENDING it, on every authenticated request that misses the cache, so " +
			"this hop carries a live end-user credential in the clear and anything on the path " +
			"can read and reuse it; address the authority over https"
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
// unconfigured path — a local stand may run with no reachable authority at all.
// Every OTHER value, including an empty/unset label, is production-class and is
// validated: a deploy that forgets KACHO_APP_ENV still fails closed rather than
// silently skipping the guard, exactly as the sibling authz and internal-listener
// guards do.
func validateProductionRevocationConfig(env string, cfg RevocationConfig) error {
	switch strings.ToLower(strings.TrimSpace(env)) {
	case "dev", "local", "test":
		return nil
	}

	var problems []string
	// Посадка личности обязана быть ОБЪЯВЛЕНА прежде, чем по ней что-то
	// требовать: пока она неизвестна, неизвестно и то, нужен ли краю наш
	// авторитет отзыва. Отказ производится первым и в одиночку.
	if !cfg.IdentityProvider.IsSet() {
		return fmt.Errorf(
			"revocation path invalid in %q env: %v (refuse to start)",
			env, identityposture.NotDeclared(config.IdentityProviderKnob))
	}
	problems = append(problems, judgeOurRevocationAuthority(cfg)...)

	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf(
		"revocation path invalid in %q env: %s (refuse to start)",
		env, strings.Join(problems, "; "),
	)
}

// validateEndpoint checks that an address is a usable absolute http(s) URL.
func validateEndpoint(raw string) error {
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
