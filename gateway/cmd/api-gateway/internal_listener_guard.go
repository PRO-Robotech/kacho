// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"crypto/tls"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

// validateProductionInternalListener — страж старта внутреннего REST-слушателя
// края и сборщик его транспорта (kacho#3131). Отказывает на ЛЮБОЙ метке
// KACHO_APP_ENV, а не только на боевой: ветки «слушатель без mTLS» нет нигде.
// Правило — в config.InternalRESTListenerTLS (одно место на корень и пробы);
// здесь — серверная часть края (edgeTLSConfig, ALPN h2 и http/1.1).
func validateProductionInternalListener(cfg config.Config) (*tls.Config, error) {
	return cfg.InternalRESTListenerTLS(edgeTLSConfig)
}
