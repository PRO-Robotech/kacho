// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

import "time"

// ListFilter — величины сужателя затронутых ресурсов notify-api (приёмка NTF-5
// Р16, Р17; замысел issue-2924 З14): окно положительных вердиктов — ручка
// KACHO_NOTIFY_LIST_FILTER_CACHE_TTL, срок одного пакетного вопроса — срок
// вопроса о правах того же развёртывания.
type ListFilter struct {
	// CacheTTL — окно положительных вердиктов сужателя (окно отзыва).
	CacheTTL time.Duration
	// CheckTimeout — срок одного пакетного вопроса владельцу модели.
	CheckTimeout time.Duration
}
