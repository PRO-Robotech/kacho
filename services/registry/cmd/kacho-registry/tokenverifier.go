// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// tokenverifier.go — сборка проверяющего identity-JWT плоскости данных.
//
// Само ОБЪЯВЛЕНИЕ приёма — кого принимаем, откуда берём его набор проверочных
// ключей, кто читает отзыв — разбирает и отвергает `config.TokenAcceptance`.
// Здесь остаётся только то, что объявлением не является: транспорт ребра к
// авторитету отзыва и сам проверяющий.
//
// Почему предикат живёт не здесь. Он нужен ДВУМ читателям: процессу при старте
// и пробе развёртывания, которая спрашивает у профиля ровно то, что спросит
// процесс. Второй читатель до `main` не дотягивается by construction, поэтому
// предикат, оставленный здесь, пришлось бы сформулировать заново — и он
// разошёлся бы молча. Это уже происходило: страж перечня издателей освобождал
// режим разработки, а читатель того же перечня не освобождал.
package main

import (
	"fmt"

	"github.com/PRO-Robotech/kacho/services/registry/internal/apps/kacho/config"
	"github.com/PRO-Robotech/kacho/services/registry/internal/clients/jwks"
)

// buildTokenVerifier собирает проверяющего по ОБЪЯВЛЕННЫМ записям приёма.
func buildTokenVerifier(cfg config.Config) (*jwks.Verifier, error) {
	bindings, err := cfg.TokenAcceptance()
	if err != nil {
		return nil, err
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
