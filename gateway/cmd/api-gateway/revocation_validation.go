// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package main — startup-validation for the revocation path.
//
// Verifying a bearer's signature proves who minted it and when it expires. It
// does NOT prove the token is still good: a sign-out, a revoked machine key or a
// back-channel logout all leave a perfectly valid signature behind. The only
// authority on that is the identity provider, reached over its ADMIN API — and
// the gateway therefore needs two addresses it cannot work out for itself:
//
//   - the token-introspection endpoint, asked per request (through a short-TTL
//     cache) before the request is let through;
//   - the admin base, used by the logout handler to kill the provider-side
//     session so a sign-out actually ends the session everywhere.
//
// Neither can be derived. Both live on a Service and port distinct from the
// public issuer, and an address derived from the issuer names a server that does
// not serve them. That failure is invisible from the outside: the check runs, is
// answered by something that is not the endpoint, and — unless the process is
// told to treat that as a misconfiguration — the request proceeds.
//
// So an unset address is not a neutral default, it is the control switched off.
// This guard refuses to start a production-class gateway in that state, and it
// refuses an address whose shape shows it points at the public API instead of
// the admin one, because that is the exact mistake the derived default made.
package main

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
	"github.com/PRO-Robotech/kacho/pkg/identityposture"
)

// introspectionAdminPath — the path the identity provider's admin API serves
// token introspection on. Pinned here so an address aimed at the public OAuth2
// API (which serves no introspection at all) is refused at startup rather than
// failing on every request afterwards.
const introspectionAdminPath = "/admin/oauth2/introspect"

// RevocationConfig is the cross-section of the configuration this guard reads:
// the two admin-API addresses the revocation path depends on.
//
// Kept as a small value-type at the composition root — like AuthzMiddlewareConfig
// — so the validator is testable without pulling the middleware/clients graph
// into a `package main` test binary.
type RevocationConfig struct {
	// IntrospectionURL — resolved token-introspection endpoint (empty ⇒ unset).
	IntrospectionURL string
	// AdminURL — resolved admin API base for the logout session-kill (empty ⇒ unset).
	AdminURL string
	// IdentityProvider — ПОСАДКА ЛИЧНОСТИ, объявленная профилем (задача #1125).
	//
	// Разводит требование АДМИНИСТРАТИВНОГО адреса, и только его: этот адрес
	// нужен ровно затем, чтобы выход человека снял сессию НА СТОРОНЕ ВНЕШНЕГО
	// ПОСТАВЩИКА. Под `own` такой сессии не существует вовсе, и требовать адрес
	// значило бы не пускать в старт край, которому он не нужен ни для чего.
	//
	// Полоса ИНТРОСПЕКЦИИ этим полем НЕ разводится и остаётся обязательной на
	// обеих полосах — её предмет (перестал ли край принимать издателя-
	// поставщика) принадлежит соседней задаче. Снять требование заодно значило
	// бы убрать защиту побочным эффектом смены посадки.
	IdentityProvider identityposture.Provider
	// AdminCAFile — the trust anchor the admin hop is verified against
	// (KACHO_HYDRA_ADMIN_CA_FILE; empty ⇒ none pinned). Read here because on
	// this hop TLS without a pinned anchor is not a partial improvement — see
	// validateAdminHopTransport.
	AdminCAFile string

	// ─── НАШ авторитет отзыва: ось, которой полоса `own` ЗАМЕЩАЕТ поставщика ──
	//
	// Полоса не СНИМАЕТ требование читать отзыв на предъявлении — она меняет
	// того, у кого спрашивают. Под `own` токены чеканим мы, поставщик о них не
	// знает by construction, и его ответ был бы утверждением о предмете,
	// которого у него нет.

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

// adminHopCAKnob — the setting naming the admin hop's trust anchor. Named in the
// refusal so an operator can act on it without reading this file.
const adminHopCAKnob = "KACHO_HYDRA_ADMIN_CA_FILE"

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
// # Почему это ЗАМЕЩЕНИЕ, а не послабление
//
// Отзыв, действующий на выдаче и не действующий на предъявлении, отзывом не
// является: предъявленное продолжает проходить до истечения срока, и это
// состояние не сходится само (`security.md` §«Контроль, действующий на ВЫДАЧЕ,
// но не на ПРЕДЪЯВЛЕНИИ»). Если бы полоса `own` только СНИМАЛА требование
// поставщика, смена посадки стала бы способом выключить чтение отзыва, ничего
// об этом не объявляя.
//
// # Требование НАЛИЧИЯ разведено полосой, требования ТРАНСПОРТА — нет
//
// Наличие адреса требуется под `own`. Заданный адрес судится теми же правилами
// на ЛЮБОЙ полосе: край, объявивший наш авторитет под `external`, тоже его
// спрашивает, и негодный хоп там так же нерабочий.
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
			// Под `external` предмета у этой оси нет: отзыв читает поставщик.
			return nil
		}
		return []string{
			platformRevocationURLKnob + " is empty — on this posture we mint the tokens and " +
				"nobody else can be asked whether one was revoked, so a revoked token would " +
				"keep working until it expires on its own [required because " +
				config.IdentityProviderKnob + "=own; on external the provider's introspection " +
				"endpoint answers instead]",
		}
	}

	var problems []string
	if err := validateAdminEndpoint(addr, ""); err != nil {
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
// WHY PLAINTEXT IS REFUSED HERE AND NOT MERELY WARNED. Since the revocation check
// moved onto the authN layer, this hop is taken on every authenticated request
// that misses the short-TTL cache, and introspection asks about a bearer by
// SENDING it. So the wire carries a live end-user credential, not an
// administrative call — and a bearer read off the wire is usable by whoever read
// it, for as long as it lives. That is a tenant-data question, which is why it
// fails the start rather than logging.
//
// WHY TLS WITHOUT AN ANCHOR IS REFUSED TOO. The provider's certificate on an
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

	var problems []string
	// Посадка личности обязана быть ОБЪЯВЛЕНА прежде, чем по ней что-то
	// требовать: пока она неизвестна, неизвестно и то, нужен ли краю
	// административный адрес поставщика. Отказ производится первым и в
	// одиночку.
	if !cfg.IdentityProvider.IsSet() {
		return fmt.Errorf(
			"revocation path invalid in %q env: %v (refuse to start)",
			env, identityposture.NotDeclared(config.IdentityProviderKnob))
	}
	// ─── ОСЬ ПОСТАВЩИКА ───────────────────────────────────────────────────────
	//
	// Требуется под `external` и НЕ требуется под `own`: под `own` токены
	// чеканим мы, поставщик о них не знает by construction, и его ответ был бы
	// утверждением о предмете, которого у него нет. Требование не снимается, а
	// ЗАМЕЩАЕТСЯ осью ниже.
	if strings.TrimSpace(cfg.IntrospectionURL) == "" {
		if cfg.IdentityProvider != identityposture.Own {
			problems = append(problems,
				"KACHO_HYDRA_INTROSPECTION_URL is empty — with no introspection endpoint the "+
					"gateway never asks whether an access token was revoked, so a revoked token "+
					"keeps working until it expires on its own [required because "+
					config.IdentityProviderKnob+"=external; declare "+config.IdentityProviderKnob+
					"=own and this requirement moves to "+platformRevocationURLKnob+"]")
		}
	} else if err := validateAdminEndpoint(cfg.IntrospectionURL, introspectionAdminPath); err != nil {
		// Заданный адрес судится ОДИНАКОВО на обеих полосах: полосность снимает
		// требование НАЛИЧИЯ, а не правила формы и транспорта.
		problems = append(problems, "KACHO_HYDRA_INTROSPECTION_URL "+err.Error())
	} else if p := validateAdminHopTransport(
		"KACHO_HYDRA_INTROSPECTION_URL", adminHopCAKnob, cfg.IntrospectionURL, cfg.AdminCAFile); p != "" {
		problems = append(problems, p)
	}

	problems = append(problems, judgeOurRevocationAuthority(cfg)...)

	if strings.TrimSpace(cfg.AdminURL) == "" && cfg.IdentityProvider != identityposture.Own {
		problems = append(problems,
			"KACHO_HYDRA_ADMIN_URL is empty — the logout handler's provider-side session "+
				"kill is disabled when it is unset, so signing out leaves the session alive "+
				"at the identity provider [required because "+config.IdentityProviderKnob+"="+
				"external; declare "+config.IdentityProviderKnob+"=own and this requirement is lifted]")
	} else if strings.TrimSpace(cfg.AdminURL) == "" {
		// Посадка `own`: сессии у внешнего поставщика не существует, снимать
		// нечего. Адрес не требуется — и это НЕ послабление транспорта: заданный
		// адрес проверяется теми же правилами ниже на любой полосе.
	} else if err := validateAdminEndpoint(cfg.AdminURL, ""); err != nil {
		problems = append(problems, "KACHO_HYDRA_ADMIN_URL "+err.Error())
	} else if p := validateAdminHopTransport(
		"KACHO_HYDRA_ADMIN_URL", adminHopCAKnob, cfg.AdminURL, cfg.AdminCAFile); p != "" {
		problems = append(problems, p)
	}

	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf(
		"revocation path invalid in %q env: %s (refuse to start)",
		env, strings.Join(problems, "; "),
	)
}

// validateAdminEndpoint checks that an address is a usable absolute URL and,
// when wantPath is non-empty, that it addresses that exact path. The path check
// is what separates the admin API from the public one: the public OAuth2 API
// serves `/oauth2/introspect` to nobody, and an address ending there is the
// derived default this guard exists to catch.
func validateAdminEndpoint(raw, wantPath string) error {
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
	if wantPath != "" && strings.TrimRight(u.Path, "/") != wantPath {
		return fmt.Errorf(
			"must address the admin API path %q, got %q — the public OAuth2 API does not "+
				"serve introspection, and an address pointing there answers every check with "+
				"a non-answer", wantPath, u.Path)
	}
	return nil
}
