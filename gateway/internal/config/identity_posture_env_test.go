// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_posture_env_test.go — документированное ИМЯ переменной посадки
// личности обязано иметь читателя, то есть доезжать до поля.
//
// Проба фиксирует ИСХОД, а не объявление тега: тег виден глазом и ничего не
// доказывает, а имя, которое документация обещает оператору, обязано менять
// загруженную настройку. Класс реален: у соседней половины (служба прав) ровно
// этот дефект нашёлся в этой же работе — переменная задавалась, а до поля не
// доезжала, и ручка выглядела настроенной, ничего не делая.
package config_test

import (
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
	"github.com/PRO-Robotech/kacho/pkg/identityposture"
)

func TestDocumentedEnvName_IdentityProviderReachesTheField(t *testing.T) {
	t.Setenv(config.IdentityProviderKnob, "own")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got, err := cfg.ResolvedIdentityProvider()
	if err != nil {
		t.Fatalf("ResolvedIdentityProvider: %v", err)
	}
	if got != identityposture.Own {
		t.Fatalf("документированная переменная не доехала до поля: получено %v", got)
	}
}

// Положительный контроль в другую сторону: незаданная переменная оставляет
// посадку НЕОБЪЯВЛЕННОЙ — умолчания у поля нет намеренно, и появление здесь
// какого-либо значения означало бы, что оно завелось.
func TestIdentityProviderStaysUnsetWhenNothingDeclaresIt(t *testing.T) {
	t.Setenv(config.IdentityProviderKnob, "")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got, err := cfg.ResolvedIdentityProvider()
	if err != nil {
		t.Fatalf("ResolvedIdentityProvider: %v", err)
	}
	if got.IsSet() {
		t.Fatalf("у поля завелось умолчание: %v", got)
	}
}
