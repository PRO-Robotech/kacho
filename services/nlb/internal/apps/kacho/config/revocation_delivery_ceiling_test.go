// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// revocation_delivery_ceiling_test.go — первая ступень цепочки отзыва гранта.
//
// Строка очереди регистрации пишется той же транзакцией, что и смена носителя;
// дренаж будится уведомлением, а откат — периодический перепрос. Величина отката
// и есть ступень: когда уведомление потеряно, отозванная выдача действует ровно
// столько. До этой пробы величину не судил никто, и посадка была вправе назвать
// любую.
package config

import (
	"strings"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/pkg/authz"
)

func TestValidate_PollFallbackAboveTheCeiling(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.FGA.RegisterDrainer.PollFallback = authz.RevocationPolicy.DeliveryCeiling + time.Second

	err := cfg.Validate()
	if err == nil {
		t.Fatalf("откат %v шире потолка %v принят: первая ступень цепочки отзыва "+
			"назначается посадкой и не судится",
			cfg.FGA.RegisterDrainer.PollFallback, authz.RevocationPolicy.DeliveryCeiling)
	}
	for _, want := range []string{
		"fga.register-drainer.poll-fallback",
		authz.RevocationPolicy.DeliveryCeiling.String(),
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("текст отказа не называет %q: %v", want, err)
		}
	}
}

// Положительный контроль в двух точках: умолчание (его ставит RegisterDefaults)
// и ровно потолок. Без него отрицание выше зеленело бы на страже, отвергающем
// любую величину, — в том числе ту, на которой сегодня работает каждая посадка.
func TestValidate_PollFallbackWithinTheCeiling(t *testing.T) {
	for _, v := range []time.Duration{
		30 * time.Second, // умолчание дерева
		authz.RevocationPolicy.DeliveryCeiling,
		time.Second,
	} {
		cfg := minimalValidConfig()
		cfg.FGA.RegisterDrainer.PollFallback = v
		if err := cfg.Validate(); err != nil {
			t.Errorf("законный откат %v отвергнут: %v", v, err)
		}
	}
}

// Незаданное значение — законный вход: библиотека дренажа подставляет своё
// умолчание, и оно объявлено потолком ступени. Судить ноль как нарушение
// значило бы отвергать посадку, ручку которой никто не трогал.
func TestValidate_PollFallbackUnset(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.FGA.RegisterDrainer.PollFallback = 0
	if err := cfg.Validate(); err != nil {
		t.Fatalf("незаданный откат отвергнут: %v", err)
	}
}
