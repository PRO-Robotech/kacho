// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// stack_start_guards_test.go — каждый развёртываемый стенд объявляет то, чего
// требуют стражи старта geo (kacho#941): окружение контейнера, отрендеренное
// настоящим helm из цепочки профилей, подаётся тому же загрузчику и тем же
// стражам, что исполняет процесс. Устройство вопроса — internal/stackenv.
//
// Конструктор дескриптора (отказы носителя) здесь НЕ зовётся, и это осознанное
// исключение: в боевой посадке он читает смонтированные файлы сертификатов, а
// их объявление не выражает; его величины судят пробы носителя на фикстуре.

import (
	"testing"

	"github.com/PRO-Robotech/kacho/internal/stackenv"
	"github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/config"
)

func TestEveryDeployedStackDeclaresWhatTheStartGuardsRequire(t *testing.T) {
	stackenv.Probe(t, stackenv.Service{
		Name: "geo",
		Root: "../../../..",
		// Чарт geo живёт под умбреллой, а не рядом со службой.
		Chart:      "deploy/helm/umbrella/charts/kacho-geo",
		ValuesKey:  "kacho-geo",
		EnvPrefix:  "KACHO_GEO_",
		GuardNames: []string{"config.Load"},
		Boot: func(env stackenv.Env) error {
			_, err := config.Load()
			return err
		},
	})
}
