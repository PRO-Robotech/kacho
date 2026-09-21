// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// Распознаватель эфемерного порта знает ВСЕ законные написания своего предмета.
// Разбор — в соседе (services/nlb/cmd/kacho-loadbalancer/ephemeralport_test.go):
// ядру порт отдаёт ЧИСЛО ноль, а не строка «0», и `net.Listen` поднимает
// эфемерный слушатель на `:0`, `:00` и `:0000` (замерено прямым вызовом).

import "testing"

func TestPortKernelAssignedKnowsEveryLawfulSpelling(t *testing.T) {
	for _, tc := range []struct {
		port string
		want bool
		why  string
	}{
		{"0", true, "каноническое написание"},
		{"00", true, "ядро смотрит на ЧИСЛО: net.Listen поднимает эфемерный слушатель"},
		{"0000", true, "то же числом, другой записью"},
		{"+0", true, "net.Listen принимает и поднимает эфемерный слушатель — замерено"},
		{"-0", true, "то же: область приёма продукта шире канонической записи"},
		{" 0", false, "с пробелом net.Listen ОТКАЗЫВАЕТ — распознаватель не вправе быть шире"},
		{"0x0", false, "шестнадцатеричную запись net.Listen тоже отвергает"},
		{"9090", false, "фиксированный порт — предмет запрета"},
		{"9091", false, "фиксированный порт — предмет запрета"},
		{"10", false, "ненулевой порт, начинающийся с цифры предмета"},
		{"", false, "порта нет вовсе: отсутствие не считается за разрешение"},
	} {
		if got := portIsKernelAssigned(tc.port); got != tc.want {
			t.Errorf("portIsKernelAssigned(%q) = %v, ждали %v — %s", tc.port, got, tc.want, tc.why)
		}
	}
}
