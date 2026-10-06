// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

import (
	"fmt"

	"github.com/PRO-Robotech/corelib/notify/feed"
)

// NotificationsKnob — ручка флага ленты извещений модуля (приёмка NTF-3,
// NTF3-64…67; замысел issue-2918 З11). Значение — ровно `true` либо `false`;
// умолчания нет: `false` из незаданного неотличим от выключенного модуля.
const NotificationsKnob = "KACHO_VPC_NOTIFICATIONS_ENABLED"

// parseNotifications — единственное чтение ручки флага в процессе. lookup —
// источник переменных окружения (`os.LookupEnv`).
func (c *Config) parseNotifications(lookup func(string) (string, bool)) {
	c.Notifications, c.notificationsErr = feed.ParseEnabled(NotificationsKnob, lookup)
}

// validateNotifications — страж старта по флагу ленты (NTF3-64): ручка не
// задана либо не разбирается — отказ с именем ручки и допустимыми значениями,
// до подъёма слушателей. Судится на ЛЮБОЙ посадке: незаданный флаг — не
// свойство посадки, а отсутствие решения оператора.
func (c Config) validateNotifications() error {
	if c.Notifications.Set() {
		return nil
	}
	if c.notificationsErr != nil {
		return c.notificationsErr
	}
	return fmt.Errorf("%s не разобрана загрузчиком конфигурации — ожидается true или false", NotificationsKnob)
}
