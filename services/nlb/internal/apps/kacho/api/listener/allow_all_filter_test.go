// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package listener

import (
	"context"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/authzfilter"
)

// allowAllFilter — модель прав, отвечающая «видно» на всё.
//
// Нужна там, где тест проверяет НЕ авторизацию (порядок, фильтр по имени, маршрутизацию
// RPC), но обязан назвать посадку явно. Прежде такие тесты передавали nil, и это
// означало «модели нет»; с тех пор как отсутствие модели стало отказом (её отсутствие —
// состояние посадки, а не ответ «да»), nil в них перестал выражать намерение: они
// проверяли бы конфигурацию, которую продукт отвергает.
//
// Разрешать всё — именно то, что этим тестам нужно: предмет у них другой, а сужение
// проверяется отдельными тестами того же пакета (list_filter_test.go).
type allowAllFilter struct{}

func (allowAllFilter) FilterVisibleIDs(_ context.Context, _, _, _ string, ids []string) ([]string, error) {
	return ids, nil
}

// allowAll — читаемая форма для конструкторов.
func allowAll() authzfilter.Filter { return allowAllFilter{} }
