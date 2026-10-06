// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/PRO-Robotech/corelib/notify/feed"
)

// registerNotificationsGauge ставит серию kacho_notifications_enabled{module="registry"}
// (приёмка NTF-3, NTF3-65 и NTF3-67; NTF1-N09) путём фундамента
// `feed.RegisterEnabledGauge`: 1 — лента модуля включена, 0 — выключена; серия
// заводится и при 0, поэтому «выключено» отличимо от «серии нет». Значение —
// флаг, разобранный загрузчиком один раз (cfg.Notifications, замысел З11), а
// не сырое окружение; своего написания метрики у корня нет.
func registerNotificationsGauge(reg prometheus.Registerer, en feed.Enabled) error {
	_, err := feed.RegisterEnabledGauge(reg, "registry", en)
	return err
}
