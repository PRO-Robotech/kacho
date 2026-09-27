// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// knob_reach_test.go — каждое имя ручки, которое задают пробы точки наката,
// ДОЕЗЖАЕТ до того, что эта точка читает (kacho#2737).
//
// Читатель здесь — не загрузчик службы целиком, а СБОРКА НАКАТА
// (`buildRunner`): точка наката спрашивает у конфигурации только адрес своей
// базы, и имя, которое читает служба, но не накат, для этого двоичного файла
// не значит ничего. Спрашивается ровно тот путь, которым процесс строит накат.
// Устройство вопроса и перепись — `internal/knobreach`.

import (
	"testing"
	"testing/fstest"

	"github.com/PRO-Robotech/kacho/internal/knobreach"
)

func TestEveryKnobTheProbesSetReachesTheLoader(t *testing.T) {
	knobreach.Gate(t, knobreach.Package{
		Service: "nlb (точка наката)",
		Dir:     ".",
		// Тексты процесса и его конфигурации: имя, которое они называют
		// оператору, обязано читаться тем же загрузчиком (kacho#2739).
		ProseDirs: []string{"."},
		// Без адреса базы и без посадки разработки (адрес фикстуры идёт без
		// шифрования) сборка наката отказывает при ЛЮБОМ значении прочих имён —
		// вопрос о них тогда не задан вовсе. Оба значения — те же, что задают
		// пробы запасного пути (main_test.go).
		Base: map[string]string{
			"KACHO_NLB_MODE":                      "dev",
			"KACHO_NLB_REPOSITORY__POSTGRES__URL": "postgres://envuser:envpass@h/db",
		},
		Load: func() (any, error) {
			return buildRunner(&rootOptions{dialect: "postgres"}, fstest.MapFS{})
		},
	})
}
