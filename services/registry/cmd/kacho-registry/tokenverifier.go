// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// tokenverifier.go — сборка проверяющего identity-JWT плоскости данных и стражи
// старта, которые её охраняют.
//
// Каждый страж закрывает состояние, при котором ПУСТОЕ значение означает «не
// сужаем»: незаданный перечень издателей — «принимаем любого»; издатель без
// записи источника — «резолвим как получится»; наш издатель без авторитета
// отзыва — «отзыв не читается ни разу за всю жизнь контроля».
//
// Место, пройденное не полностью, даёт отказ проверки при ПЕРВОМ ЖЕ ЗАПРОСЕ
// вместо отказа при СТАРТЕ. Разница не косметическая: первый виден арендатору и
// не виден оператору, второй виден оператору и не доходит до арендатора.
package main

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/PRO-Robotech/kacho/services/registry/internal/apps/kacho/config"
	"github.com/PRO-Robotech/kacho/services/registry/internal/clients/jwks"
)

// buildTokenVerifier собирает проверяющего по ОБЪЯВЛЕННЫМ записям приёма.
//
// Порядок стражей — от того, без чего не построить ничего, к тому, что
// уточняет посадку: перечень издателей → привязка «издатель → источник» →
// защищённость адресов → авторитет отзыва.
func buildTokenVerifier(cfg config.Config) (*jwks.Verifier, error) {
	if err := requireTokenIssuersDeclared(cfg.AuthMode, cfg.TokenIssuers); err != nil {
		return nil, err
	}
	bindings, err := cfg.TokenIssuerBindings()
	if err != nil {
		return nil, err
	}
	for _, b := range bindings {
		if err := requireSecureKeySetURL(cfg.AuthMode, b.Issuer, b.KeySetURL); err != nil {
			return nil, err
		}
	}

	sources := make([]jwks.KeySetSource, 0, len(bindings))
	readsRevocation := false
	for _, b := range bindings {
		sources = append(sources, jwks.KeySetSource{
			Issuer:                  b.Issuer,
			URL:                     b.KeySetURL,
			TokenType:               b.TokenType,
			TolerateAbsentTokenType: b.TolerateAbsentTokenType,
			ReadRevocation:          b.ReadRevocation,
		})
		readsRevocation = readsRevocation || b.ReadRevocation
	}

	var opts []jwks.Option
	if readsRevocation {
		if err := requireRevocationAuthority(cfg.AuthMode, cfg.TokenRevocationURL); err != nil {
			return nil, err
		}
		reader, rerr := jwks.NewIntrospectionReader(cfg.TokenRevocationURL, jwks.RevocationTransport{
			Enable:     cfg.TokenRevocationMTLS.Enable,
			CAFiles:    cfg.TokenRevocationMTLS.CAFiles,
			CertFile:   cfg.TokenRevocationMTLS.CertFile,
			KeyFile:    cfg.TokenRevocationMTLS.KeyFile,
			ServerName: cfg.TokenRevocationMTLS.ServerName,
		})
		if rerr != nil {
			return nil, fmt.Errorf("KACHO_REGISTRY_TOKEN_REVOCATION_URL / KACHO_REGISTRY_TOKEN_REVOCATION_MTLS_*: %w", rerr)
		}
		opts = append(opts, jwks.WithRevocationReader(reader))
	}

	return jwks.New(sources, cfg.ServiceAud, opts...)
}

// requireTokenIssuersDeclared — перечень принимаемых издателей обязан содержать
// ЭЛЕМЕНТЫ, а не быть непустой строкой.
//
// Разделитель без элементов («,»), пробелы, пустые элементы подряд дают строку,
// у которой длина непуста, а перечень пуст. Страж, меряющий длину, на таком
// значении молчит, а приём — принимает любого издателя: два разных предиката об
// одном предмете, из которых верен один.
//
// В dev пустой перечень допустим — там проверяющий вообще не поднимается без
// объявленных записей, и отказ придёт из `TokenIssuerBindings`; сообщение о нём
// называет ту же настройку.
func requireTokenIssuersDeclared(authMode, raw string) error {
	switch authMode {
	case "production", "production-strict":
	default:
		return nil
	}
	elements := 0
	for _, part := range strings.Split(raw, ",") {
		if strings.TrimSpace(part) != "" {
			elements++
		}
	}
	if elements == 0 {
		return fmt.Errorf("AuthMode=%s requires KACHO_REGISTRY_TOKEN_ISSUERS to name at least one issuer "+
			"(value %q has %d characters and %d elements; the guard counts ELEMENTS — an empty issuer set "+
			"means «accept any issuer», and then a token from any relying party sharing the key set and the "+
			"audience authenticates)", authMode, raw, len(raw), elements)
	}
	return nil
}

// requireSecureKeySetURL — источник набора проверочных ключей есть единственный
// якорь доверия проверки подписи. По открытому HTTP его документ подменяется на
// пути, и тогда подделывается токен под любого субъекта — то есть проверка
// подлинности обходится целиком.
//
// В dev открытый HTTP допустим — симметрично незашифрованному соединению к базе.
func requireSecureKeySetURL(authMode, issuer, keySetURL string) error {
	switch authMode {
	case "production", "production-strict":
	default:
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
			authMode, issuer, u.Scheme)
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
func requireRevocationAuthority(authMode, authorityURL string) error {
	trimmed := strings.TrimSpace(authorityURL)
	if trimmed == "" {
		return fmt.Errorf("KACHO_REGISTRY_PLATFORM_TOKEN_ISSUER is accepted, so "+
			"KACHO_REGISTRY_TOKEN_REVOCATION_URL is required: a control that acts only where the "+
			"credential is ISSUED is not revocation — it merely declines to issue a new one "+
			"(AuthMode=%s)", authMode)
	}
	switch authMode {
	case "production", "production-strict":
	default:
		return nil
	}
	u, err := url.Parse(trimmed)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("AuthMode=%s requires an absolute KACHO_REGISTRY_TOKEN_REVOCATION_URL "+
			"(got %q); it is declared explicitly and never derived from a neighbour's address",
			authMode, authorityURL)
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return fmt.Errorf("AuthMode=%s requires an https:// KACHO_REGISTRY_TOKEN_REVOCATION_URL "+
			"(the answer decides access and must not transit plaintext; got scheme %q)",
			authMode, u.Scheme)
	}
	return nil
}
