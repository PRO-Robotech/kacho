// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package anonmail

import (
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

// PassRetention — срок хранения моментов пропуска (З26, CX2-29). Функция
// живёт рядом с таблицей границ (`config.AnonMailPassRetention`): её же читает
// уборщик хранилища однократности, и второго определения нет.
func PassRetention() time.Duration { return config.AnonMailPassRetention() }
