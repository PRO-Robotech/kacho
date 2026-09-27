// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// stack_start_guards_test.go — каждый развёртываемый стенд объявляет то, чего
// требуют стражи старта vpc (kacho#941): окружение контейнера, отрендеренное
// настоящим helm из цепочки профилей, подаётся тому же загрузчику и тем же
// стражам, что исполняет процесс. Устройство вопроса — internal/stackenv.
//
// Конструктор дескриптора (отказы носителя) здесь НЕ зовётся, и это осознанное
// исключение: в боевой посадке он читает смонтированные файлы сертификатов, а
// их объявление не выражает; его величины судят пробы носителя на фикстуре.

import (
	"os"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/config"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/stackenv"
)

func TestEveryDeployedStackDeclaresWhatTheStartGuardsRequire(t *testing.T) {
	stackenv.Probe(t, stackenv.Service{
		Name:       "vpc",
		Root:       "../../../..",
		Chart:      "services/vpc/deploy",
		ValuesKey:  "vpc",
		EnvPrefix:  "KACHO_VPC_",
		GuardNames: []string{"config.Load", "config.LoadMTLS", "Config.ValidateBoot"},
		Boot: func(env stackenv.Env) error {
			cfg, err := config.Load(os.Getenv(configPathEnv))
			if err != nil {
				return err
			}
			m, err := config.LoadMTLS()
			if err != nil {
				return err
			}
			return cfg.ValidateBoot(m)
		},
	})
}
