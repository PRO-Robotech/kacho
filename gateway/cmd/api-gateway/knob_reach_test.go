// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// knob_reach_test.go — каждое имя ручки, которое задают пробы края, ДОЕЗЖАЕТ
// до загрузчика края (kacho#2737). Устройство вопроса и перепись —
// `internal/knobreach`.

import (
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
	"github.com/PRO-Robotech/kacho/internal/knobreach"
)

func TestEveryKnobTheProbesSetReachesTheLoader(t *testing.T) {
	knobreach.Gate(t, knobreach.Package{
		Service: "api-gateway",
		Dir:     ".",
		// Тексты процесса и его конфигурации: имя, которое они называют
		// оператору, обязано читаться тем же загрузчиком (kacho#2739).
		ProseDirs: []string{".", "../../internal/config"},
		Load:      func() (any, error) { return config.Load() },
	})
}
