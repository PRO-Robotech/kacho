// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// stack_start_guards_test.go — каждый развёртываемый стенд объявляет то, чего
// требуют стражи старта nlb (kacho#941): окружение контейнера, отрендеренное
// настоящим helm из цепочки профилей, подаётся тому же загрузчику и тем же
// стражам, что исполняет процесс. Устройство вопроса — internal/stackenv.
//
// Конструктор дескриптора (отказы носителя) здесь НЕ зовётся, и это осознанное
// исключение: в боевой посадке он читает смонтированные файлы сертификатов, а
// их объявление не выражает; его величины судят пробы носителя на фикстуре.

import (
	"github.com/PRO-Robotech/kacho/services/nlb/internal/apps/kacho/config"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/stackenv"
)

func TestEveryDeployedStackDeclaresWhatTheStartGuardsRequire(t *testing.T) {
	stackenv.Probe(t, stackenv.Service{
		Name:       "nlb",
		Root:       "../../../..",
		Chart:      "services/nlb/deploy",
		ValuesKey:  "kacho-nlb",
		EnvPrefix:  "KACHO_NLB_",
		GuardNames: []string{"config.Load (файл из --config: разбор и Config.Validate)"},
		Boot: func(env stackenv.Env) error {
			// Конфигурация nlb приезжает ФАЙЛОМ из ConfigMap, путь — аргументом
			// `--config`, ровно как его разбирает корень (main.go).
			_, err := config.Load(env.Flag("--config"))
			return err
		},
	})
}
