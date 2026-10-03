// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// stack_start_guards_test.go — каждый развёртываемый стенд объявляет то, чего
// требуют стражи старта storage (kacho#941): окружение контейнера, отрендеренное
// настоящим helm из цепочки профилей, подаётся тому же загрузчику и тем же
// стражам, что исполняет процесс. Устройство вопроса — internal/stackenv.
//
// Конструктор дескриптора (отказы носителя) здесь НЕ зовётся, и это осознанное
// исключение: в боевой посадке он читает смонтированные файлы сертификатов, а
// их объявление не выражает; его величины судят пробы носителя на фикстуре.

import (
	"testing"

	"github.com/PRO-Robotech/kacho/internal/stackenv"
	"github.com/PRO-Robotech/kacho/services/storage/internal/config"
)

func TestEveryDeployedStackDeclaresWhatTheStartGuardsRequire(t *testing.T) {
	stackenv.Probe(t, stackenv.Service{
		Name:       "storage",
		Root:       "../../../..",
		Chart:      "services/storage/deploy",
		ValuesKey:  "storage",
		EnvPrefix:  "KACHO_STORAGE_",
		GuardNames: []string{"config.Load", "Config.Validate"},
		Boot: func(env stackenv.Env) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			return cfg.Validate()
		},
	})
}
