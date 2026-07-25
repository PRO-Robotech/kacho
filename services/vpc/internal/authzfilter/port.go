// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package authzfilter

import "context"

// UseCasePort — форма фильтра видимости, которую видит use-case-слой (clean-arch:
// use-case определяет узкий порт `ListFilter` с этой же сигнатурой и не зависит от
// transport-деталей AuthorizeClient).
//
// Контракт — см. Filter.FilterVisibleIDs: вернуть подмножество ids, видимое
// subject'у, сохранив порядок входа; err → fail-closed (use-case пробрасывает,
// НЕ отдает нефильтрованную страницу).
//
// Порт НЕ умеет «перечислить всё, что subject'у можно»: такого вопроса больше
// нет ни на одном пути (он был источником усечения на пределе OpenFGA
// ListObjects — см. package-doc). Единичное чтение по id (`Get`) авторизуется
// ПРЯМЫМ per-object Check в per-RPC interceptor'е — так же, как Update/Delete, —
// и в этот порт не заходит вовсе.
type UseCasePort interface {
	FilterVisibleIDs(ctx context.Context, subject, resourceType, action string, ids []string) ([]string, error)
}

// AsPort адаптирует Filter к UseCasePort. f == nil → nil (use-case трактует
// nil-порт как passthrough — dev / list-filter disabled).
func AsPort(f Filter) UseCasePort {
	if f == nil {
		return nil
	}
	// Typed-nil guard: *FGAFilter(nil) переданный как Filter — тоже passthrough.
	if ff, ok := f.(*FGAFilter); ok && ff == nil {
		return nil
	}
	return f
}
