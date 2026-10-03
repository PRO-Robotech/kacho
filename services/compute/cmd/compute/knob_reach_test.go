// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// knob_reach_test.go — каждое имя ручки, которое задают пробы этого пакета,
// ДОЕЗЖАЕТ до загрузчика службы.
//
// Имя, которого разбор конфигурации не знает, не отказывает и не
// предупреждает: величина остаётся умолчанием, и проба утверждает про
// конфигурацию, которой процесс не получал. Гейт спрашивает об этом САМ
// загрузчик — тем же вызовом, что композиционный корень, — а не образец имени:
// правила подстановки у загрузчиков разные. Устройство вопроса и перепись —
// `internal/knobreach`.

import (
	"testing"

	"github.com/PRO-Robotech/kacho/internal/knobreach"
	"github.com/PRO-Robotech/kacho/services/compute/internal/config"
)

func TestEveryKnobTheProbesSetReachesTheLoader(t *testing.T) {
	knobreach.Gate(t, knobreach.Package{
		Service: "compute",
		Dir:     ".",
		// Тексты процесса и его конфигурации: имя, которое они называют
		// оператору, обязано читаться тем же загрузчиком.
		ProseDirs: []string{".", "../../internal/config"},
		Base:      map[string]string{"KACHO_COMPUTE_DB_PASSWORD": "x"},
		Load:      func() (any, error) { return config.Load() },
	})
}
