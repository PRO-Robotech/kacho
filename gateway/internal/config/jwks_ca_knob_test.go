// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// jwks_ca_knob_test.go — ручка якоря доверия хопа за наборами ключей
// принимаемых издателей называется в терминах НАШЕЙ поверхности и доезжает до
// поля (kacho#2842).
//
// Проба фиксирует ИСХОД, а не тег: имя, которое оператор задаёт окружением,
// обязано менять загруженную настройку. Прежнее имя называло снимаемого
// поставщика, хотя хоп ходит за ключами издателей, объявленных краю ручкой
// `KACHO_API_GATEWAY_TOKEN_ISSUER_KEYSETS`, — в том числе нашего. Что прежнее имя
// больше не читает никто, держит предикат задачи по дереву (ноль вхождений), а
// не эта проба: читателя без литерала имени не бывает.
package config_test

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

func TestJWKSCAFile_KnobReachesTheField(t *testing.T) {
	const want = "/etc/api-gateway/keysets-ca/ca.crt"
	t.Setenv(config.JWKSCAFileKnob, want)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.JWKSCAFile != want {
		t.Fatalf("%s не доехала до поля: получено %q, задано %q",
			config.JWKSCAFileKnob, cfg.JWKSCAFile, want)
	}
}

// Законный близнец: незаданная ручка оставляет якорь пустым — транспорт по
// умолчанию, как и прежде. Значение здесь означало бы, что у поля завелось
// умолчание, и хоп стал бы требовать связку там, где её не объявляли.
func TestJWKSCAFile_UnsetStaysEmpty(t *testing.T) {
	t.Setenv(config.JWKSCAFileKnob, "")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.JWKSCAFile != "" {
		t.Fatalf("незаданная ручка дала якорь %q", cfg.JWKSCAFile)
	}
}

// Форма имени — `KACHO_<DOMAIN>_<NAME>` с доменом края, и имя называет хоп,
// которому ручка принадлежит, а не поставщика по ту его сторону.
func TestJWKSCAFile_KnobNamesTheEdgeAndItsHop(t *testing.T) {
	const want = "KACHO_API_GATEWAY_JWKS_"
	if !strings.HasPrefix(config.JWKSCAFileKnob, want) {
		t.Fatalf("имя ручки %q не в форме %s…: якорь обязан называть домен края и хоп",
			config.JWKSCAFileKnob, want)
	}
}
