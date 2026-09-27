// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// knob_reach_test.go — каждое имя ручки, которое задают пробы этого пакета,
// ДОЕЗЖАЕТ до загрузчика службы (kacho#2737).
//
// Имя, которого разбор конфигурации не знает, не отказывает и не
// предупреждает: величина остаётся умолчанием, и проба утверждает про
// конфигурацию, которой процесс не получал. Гейт спрашивает об этом САМ
// загрузчик — тем же вызовом, что композиционный корень, — а не образец имени:
// правила подстановки у загрузчиков разные. Устройство вопроса и перепись —
// `internal/knobreach`.

import (
	"os"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/knobreach"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/config"
)

func TestEveryKnobTheProbesSetReachesTheLoader(t *testing.T) {
	knobreach.Gate(t, knobreach.Package{
		Service: "vpc",
		Dir:     ".",
		// Тексты процесса и его конфигурации: имя, которое они называют
		// оператору, обязано читаться тем же загрузчиком (kacho#2739).
		ProseDirs: []string{".", "../../internal/apps/kacho/config"},
		Base:      nil,
		Load:      func() (any, error) { return loadAsMainDoes() },
	})
}

// loadAsMainDoes — оба загрузчика, которые зовёт композиционный корень, в том же
// порядке и с тем же источником пути к файлу (main.go: `config.Load` по пути из
// окружения, затем `config.LoadMTLS`). Имя, которое читает один из них, законно.
func loadAsMainDoes() (any, error) {
	cfg, err := config.Load(os.Getenv(configPathEnv))
	if err != nil {
		return nil, err
	}
	mtls, err := config.LoadMTLS()
	if err != nil {
		return nil, err
	}
	return struct {
		Config config.Config
		MTLS   config.MTLSConfig
	}{cfg, mtls}, nil
}
