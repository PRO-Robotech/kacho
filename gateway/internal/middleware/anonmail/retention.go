// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package anonmail

import (
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

// retentionMargin — запас срока хранения над верхней границей окон: покрывает
// шаг уборки (замысел З26).
const retentionMargin = time.Hour

// PassRetention — срок хранения моментов пропуска (З26, CX2-29): функция, а не
// литерал, — верхняя граница каждого окна, читающего моменты (окна источника и
// подсети), из ТОЙ ЖЕ таблицы границ, что судит страж старта
// (`config.AnonMailWindowUpperBound`), плюс запас. Поднятие ручки окна в
// пределах границы не делает прежние моменты невидимыми.
func PassRetention() time.Duration {
	return config.AnonMailWindowUpperBound() + retentionMargin
}
