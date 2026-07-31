// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package targetgroup_test

import (
	"context"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/authzfilter"
)

// allowAllFilterExt — та же модель прав «видно всё» для внешнего тестового пакета.
// См. allow_all_filter_test.go: отсутствие модели больше не значит passthrough, поэтому
// тест, чей предмет НЕ авторизация, обязан назвать посадку явно.
type allowAllFilterExt struct{}

func (allowAllFilterExt) FilterVisibleIDs(_ context.Context, _, _, _ string, ids []string) ([]string, error) {
	return ids, nil
}

func allowAllExt() authzfilter.Filter { return allowAllFilterExt{} }
