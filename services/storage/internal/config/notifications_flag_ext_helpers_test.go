// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config_test

import "github.com/PRO-Robotech/corelib/notify/feed"

// probeNotificationsOff — флаг ленты модуля в конфигурациях проб этого пакета,
// собранных литералом: разобран (feed.ParseEnabled) значением false. Флаг без
// умолчания (NTF3-64), и конфигурация без него отвергается стражем старта;
// пробы, чей предмет не флаг, получают его разобранным, как от загрузчика.
func probeNotificationsOff() feed.Enabled {
	en, err := feed.ParseEnabled("KACHO_STORAGE_NOTIFICATIONS_ENABLED", func(string) (string, bool) { return "false", true })
	if err != nil {
		panic("разбор флага ленты пробы: " + err.Error())
	}
	return en
}
