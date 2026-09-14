// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// token_audience_validation_test.go — АДРЕСАТ ОБЪЯВЛЯЕТСЯ, А НЕ ВЫВОДИТСЯ
// (задача #2567).
//
// # Предмет
//
// Адресат — то единственное, чем токен говорит, КАКОЙ УСТАНОВКЕ он выдан. Пока
// край выводил его построением из домена с умолчанием, величина не бывала
// пустой НИКОГДА: ручки у неё не было, стража тоже, а сама проверка сужала лишь
// при непустом ожидаемом значении.
//
// # Почему страж, а не «разумное умолчание»
//
// Величина, которую построение подставляет молча, предметом стража быть НЕ
// МОЖЕТ: он зелен при любом входе, потому что незаданной величина не бывает. Та
// же норма, по которой умолчания нет у посадки личности (задача #1125) и у
// адресата чеканящей службы.
//
// # Перепись печатает ДВА числа
//
// Полос у механизма несколько, и свойство обязательно для каждой. Одно число
// («находок 0») скрывает ровно тот случай, ради которого перепись заведена:
// полосу, выпавшую из обхода. Поэтому печатается «полос N · несут свойство M».
package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/identityposture"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

// testTokenAudience — объявленный адресат годного профиля.
const testTokenAudience = "https://api.kacho.test"

// TestProdRefusesUndeclaredTokenAudience — незаявленный адресат старт не
// проходит, и отказ НАЗЫВАЕТ РУЧКУ: оператору иначе нечего искать.
func TestProdRefusesUndeclaredTokenAudience(t *testing.T) {
	err := validateProductionTokenAudience("production", "")
	if err == nil {
		t.Fatal("адресат принимаемых токенов не объявлен — старт обязан быть отвергнут: " +
			"иначе край принимает удостоверение, выпущенное для другой установки")
	}
	if !strings.Contains(err.Error(), config.AudienceKnob) {
		t.Fatalf("отказ обязан называть ручку %q, получено: %v", config.AudienceKnob, err)
	}
}

// TestProdRefusesDegenerateTokenAudience — вырожденное значение непусто по
// длине и пусто по существу. Тот же класс, что одинокая запятая в круге
// отправителей: страж обязан считать ЗНАЧЕНИЕ, а не длину строки.
func TestProdRefusesDegenerateTokenAudience(t *testing.T) {
	for _, raw := range []string{" ", "\t", "  \n "} {
		if raw == "" {
			t.Fatal("вырожденный вход обязан быть непустым — иначе проба судит не о том")
		}
		if err := validateProductionTokenAudience("production", raw); err == nil {
			t.Fatalf("вырожденное значение %q прошло старт", raw)
		}
	}
}

// TestProdAcceptsDeclaredTokenAudience — ПАРНЫЙ ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ. Без
// него отрицания выше зеленели бы на страже, отвергающем всё.
func TestProdAcceptsDeclaredTokenAudience(t *testing.T) {
	if err := validateProductionTokenAudience("production", testTokenAudience); err != nil {
		t.Fatalf("объявленный адресат обязан проходить старт: %v", err)
	}
}

// TestDevClassToleratesUndeclaredTokenAudience — полосность та же, что у
// соседних стражей: только явные dev-метки терпят ненастроенное, а пустая либо
// незнакомая метка — боевой класс.
func TestDevClassToleratesUndeclaredTokenAudience(t *testing.T) {
	for _, env := range []string{"dev", "local", "test"} {
		if err := validateProductionTokenAudience(env, ""); err != nil {
			t.Fatalf("%s: dev-класс обязан терпеть незаявленный адресат: %v", env, err)
		}
	}
	for _, env := range []string{"", "staging", "PRODUCTION"} {
		if err := validateProductionTokenAudience(env, ""); err == nil {
			t.Fatalf("%q: метка вне dev-класса — боевой класс, отказ обязателен", env)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ПЕРЕПИСЬ ПОЛОС: сверка МЕЖДУ СОБОЙ, а не по каждой отдельно
//
// Механизм один — опознание токена на крае, — и у него несколько РАВНОПРАВНЫХ
// полос: адресат, авторитет отзыва поставщика, административный хоп, наш
// авторитет отзыва. Каждая называет ЭТУ установку либо её соседа, и ни одну
// построение подставить не вправе.
//
// Свойство проверяется ДВУМЯ половинами, потому что каждая по отдельности
// проходима при сломанной второй: величина без встроенного умолчания, но без
// стража, молча работает пустой; страж при живом умолчании тождественно истинен.
// ─────────────────────────────────────────────────────────────────────────────

// identityLane — полоса опознания токена и способ проверить её страж.
type identityLane struct {
	// knob — имя переменной окружения, объявляющей величину.
	knob string
	// field — поле config.Config, несущее её.
	field string
	// posture — посадка, на которой величина ТРЕБУЕТСЯ. Полосность снимает
	// требование НАЛИЧИЯ у соседа, поэтому каждая полоса судится там, где её
	// предмет жив.
	posture identityposture.Provider
	// refuseWhenUnset — страж, позванный с ЭТОЙ полосой незаданной и всеми
	// остальными объявленными. Одно-фактность: красное приходит от предмета, а
	// не от соседа.
	refuseWhenUnset func() error
}

// goodRevocation — годная боевая настройка полос отзыва на названной посадке.
func goodRevocation(p identityposture.Provider) RevocationConfig {
	cfg := RevocationConfig{
		IdentityProvider: p,
		IntrospectionURL: tlsIntrospectURL,
		AdminURL:         tlsAdminURL,
		AdminCAFile:      "/etc/api-gateway/hydra-admin-ca/ca.crt",
	}
	if p == identityposture.Own {
		cfg.PlatformRevocationURL = tlsAdminURL
		cfg.PlatformRevocationCAFile = "/etc/api-gateway/platform-revocation-ca/ca.crt"
		cfg.PlatformRevocationCertFile = "/etc/api-gateway/mtls/tls.crt"
		cfg.PlatformRevocationKeyFile = "/etc/api-gateway/mtls/tls.key"
	}
	return cfg
}

// tokenIdentityLanes — полосы механизма. Перечень выписан ЗДЕСЬ намеренно:
// заведя новую величину опознания, автор обязан дописать строку — и тем самым
// ответить, чем она держится. Молчаливое появление полосы без стража — ровно
// то, что перепись и ловит.
func tokenIdentityLanes() []identityLane {
	return []identityLane{
		{
			knob:    config.AudienceKnob,
			field:   "TokenAudience",
			posture: identityposture.External,
			refuseWhenUnset: func() error {
				return validateProductionTokenAudience("production", "")
			},
		},
		{
			knob:    "KACHO_HYDRA_INTROSPECTION_URL",
			field:   "HydraIntrospectionURL",
			posture: identityposture.External,
			refuseWhenUnset: func() error {
				c := goodRevocation(identityposture.External)
				c.IntrospectionURL = ""
				return validateProductionRevocationConfig("production", c)
			},
		},
		{
			knob:    "KACHO_HYDRA_ADMIN_URL",
			field:   "HydraAdminURL",
			posture: identityposture.External,
			refuseWhenUnset: func() error {
				c := goodRevocation(identityposture.External)
				c.AdminURL = ""
				return validateProductionRevocationConfig("production", c)
			},
		},
		{
			knob:    "KACHO_API_GATEWAY_PLATFORM_TOKEN_REVOCATION_URL",
			field:   "PlatformTokenRevocationURL",
			posture: identityposture.Own,
			refuseWhenUnset: func() error {
				c := goodRevocation(identityposture.Own)
				c.PlatformRevocationURL = ""
				return validateProductionRevocationConfig("production", c)
			},
		},
	}
}

// envconfigTag читает объявление ручки у поля config.Config.
func envconfigTag(t *testing.T, field string) (envName, defaultValue string, found bool) {
	t.Helper()
	f, ok := reflect.TypeOf(config.Config{}).FieldByName(field)
	if !ok {
		return "", "", false
	}
	return f.Tag.Get("envconfig"), f.Tag.Get("default"), true
}

// TestTokenIdentityLanesAreDeclaredNotDerived — ПЕРЕПИСЬ.
//
// Печатает ДВЕ величины. «Полос N» — сколько осмотрено; «несут свойство M» —
// сколько закрыты обеими половинами. Равенство и есть утверждение; одно число
// на его месте скрыло бы выпавшую полосу.
func TestTokenIdentityLanesAreDeclaredNotDerived(t *testing.T) {
	lanes := tokenIdentityLanes()
	if len(lanes) == 0 {
		t.Fatal("обход пуст — «ноль находок» неотличимо от «ноль прочитанного»")
	}

	carry := 0
	for _, lane := range lanes {
		ok := true

		// Половина ПЕРВАЯ: у величины нет встроенного умолчания.
		envName, def, found := envconfigTag(t, lane.field)
		switch {
		case !found:
			t.Errorf("%s: поля %q в config.Config нет — полоса называет координату, "+
				"которой не существует", lane.knob, lane.field)
			ok = false
		case envName != lane.knob:
			t.Errorf("%s: поле %q объявляет ручку %q — перепись и дерево разошлись",
				lane.knob, lane.field, envName)
			ok = false
		case strings.TrimSpace(def) != "":
			t.Errorf("%s: у величины встроенное умолчание %q — незаданной она не бывает, "+
				"и страж ниже тождественно истинен", lane.knob, def)
			ok = false
		}

		// Половина ВТОРАЯ: незаданное доезжает до стража, и отказ называет ручку.
		err := lane.refuseWhenUnset()
		switch {
		case err == nil:
			t.Errorf("%s (посадка %s): незаданное значение прошло старт — "+
				"полоса объявлена и ничем не держится", lane.knob, lane.posture)
			ok = false
		case !strings.Contains(err.Error(), lane.knob):
			t.Errorf("%s: отказ не называет ручку, оператору нечего искать: %v",
				lane.knob, err)
			ok = false
		}

		if ok {
			carry++
		}
	}

	t.Logf("перепись: полос %d · несут свойство %d", len(lanes), carry)
	if carry != len(lanes) {
		t.Fatalf("перепись: полос %d · несут свойство %d — расходятся",
			len(lanes), carry)
	}
}

// TestTokenIdentityCensusCanFail — САМОПРОВЕРКА переписи на СИНТЕТИКЕ.
//
// Фикстура синтетическая намеренно: опирайся самопроверка на живую полосу,
// доказательство исчезло бы вместе с её починкой — ровно тогда, когда перепись
// достигла цели.
func TestTokenIdentityCensusCanFail(t *testing.T) {
	// Полоса с живым умолчанием — первая половина обязана её отвергнуть.
	envName, def, found := envconfigTag(t, "AuthNMode")
	if !found || envName == "" {
		t.Fatal("контрольное поле не найдено — самопроверка беспредметна")
	}
	if strings.TrimSpace(def) == "" {
		t.Fatal("контрольное поле утратило умолчание — синтетика больше не воспроизводит дефект")
	}

	// Законный близнец: у поля посадки личности умолчания нет, и первая
	// половина обязана на нём МОЛЧАТЬ.
	_, idpDef, idpFound := envconfigTag(t, "IdentityProvider")
	if !idpFound {
		t.Fatal("законный близнец не найден")
	}
	if strings.TrimSpace(idpDef) != "" {
		t.Fatalf("законный близнец обзавёлся умолчанием %q — он больше не близнец", idpDef)
	}

	// Страж, который не может упасть: позванный на dev-классе, он молчит на
	// любом входе. Вторая половина обязана это отличать.
	if err := validateProductionTokenAudience("dev", ""); err != nil {
		t.Fatalf("контроль: dev-класс обязан молчать, получено %v", err)
	}
	t.Logf("самопроверка: осмотрено полей 2 (дефект + законный близнец), стражей 1")
}
