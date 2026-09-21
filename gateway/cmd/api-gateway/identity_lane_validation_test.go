// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_lane_validation_test.go — РУЧКА ПОСАДКИ ЛИЧНОСТИ НА КРАЕ РАЗБИРАЕТСЯ
// ОБЩИМ СЛОВАРЁМ, И ОТКАЗ НАЗЫВАЕТ ЕЁ СОБСТВЕННУЮ РУЧКУ.
//
// ЧТО ЗДЕСЬ БЫЛО. Сценарий F4d-06 приёмки Ф4д: страж старта требовал адресов
// внешнего поставщика ПО ПОЛОСЕ, а не всегда, и случаи утверждали ТЕКСТ отказа
// на каждой полосе — тон отказа при старте есть часть контракта оператора.
//
// ЧЕГО НЕТ СЕЙЧАС. Адресов внешнего поставщика у края не осталось ни одного, и
// страж их больше не судит: требование нашего авторитета отзыва стало
// БЕЗУСЛОВНЫМ (`revocation_validation_test.go`). Случаи «под external требуется
// — под own не требуется» сняты вместе со своим предметом: вход, на котором они
// краснели, непредставим, а оставленные — они замолкают МОЛЧА и переживают то,
// чем обозначались.
//
// ЧТО ОСТАЛОСЬ ЖИВЫМ. Сама ручка посадки. Её читает композиционный корень
// (`main.go`: полоса личности, ретрансляция полосы формы, самоотчёт посадки), и
// разбор значения по-прежнему обязан называть РУЧКУ КРАЯ, а не ручку службы
// прав: оператор, получивший отказ, идёт править профиль, и назвать ему чужой
// значит послать его не туда.
package main

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/identityposture"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

// Разбор ручки края идёт ОБЩИМ словарём, и отказ называет ЕЁ ручку, а не чужую.
func TestF4d06_TheEdgeRefusalNamesTheEdgeKnob(t *testing.T) {
	cfg := config.Config{IdentityProvider: "Own"} // соседняя раскладка регистра
	_, err := cfg.ResolvedIdentityProvider()
	if err == nil {
		t.Fatal("значение вне словаря обязано быть отвергнуто")
	}
	if !strings.Contains(err.Error(), config.IdentityProviderKnob) {
		t.Fatalf("отказ края обязан называть ЕГО ручку, получено: %q", err.Error())
	}
	if strings.Contains(err.Error(), "authn.identity-provider") {
		t.Fatalf("отказ края назвал ручку службы прав — оператор пойдёт править не тот профиль: %q", err.Error())
	}

	// Положительный контроль: каноническое значение принимается.
	ok := config.Config{IdentityProvider: "own"}
	got, err := ok.ResolvedIdentityProvider()
	if err != nil || got != identityposture.Own {
		t.Fatalf("каноническое значение обязано приниматься, получено %v / %v", got, err)
	}
}
