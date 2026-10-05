// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

// notifications_knob_main_test.go — пробы этого пакета грузят конфигурацию так,
// как её получает процесс модуля в посадке: ручка флага ленты
// KACHO_REGISTRY_NOTIFICATIONS_ENABLED задана (умолчания у неё нет, NTF3-64).
// Ставим её один раз на пакет, значением выключенной ленты: иначе каждая проба,
// чей предмет не флаг (адрес соседа, mTLS-ручка, круг отправителей), падала бы
// по чужой причине — отказом стража флага.
//
// Фикстура не снисходительнее продукта: пробы самого флага (ntf3_module_flag_test.go)
// снимают и портят ручку сами (t.Setenv, os.Unsetenv) и судят отказ.

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	if err := os.Setenv("KACHO_REGISTRY_NOTIFICATIONS_ENABLED", "false"); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}
