// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package authzfilter

import (
	"context"

	"github.com/PRO-Robotech/kacho/pkg/operations"
)

// SubjectFromPrincipal извлекает FGA-subject ("user:usr_x" / "service_account:sva_x")
// из Principal запроса. Principal кладёт в ctx principal-extract-интерсептор обоих
// листенеров (grpcsrv.UnaryCertIdentityExtract → UnaryTrustedPrincipalExtract),
// читающий заголовки api-gateway с уже проверенным доверенным форвардером.
//
// Это ЕДИНЫЙ источник caller-identity и для per-RPC Check, и для list-filter.
// system-principal (control-plane bootstrap / no-auth) либо неполный principal → ""
// (caller трактует пустой subject как fail-closed: пустая страница, НЕ bypass-all —
// именно bypass на пустом subject'е и есть over-show-утечка).
func SubjectFromPrincipal(ctx context.Context) string {
	p := operations.PrincipalFromContext(ctx)
	// Зарезервированное слово анонимности отсекается ОТДЕЛЬНО от типа:
	// `{user, anonymous}` — та же «неизвестно кто», но тип объявляет отправитель
	// заголовков, поэтому проверять надо само слово. Предикат общий
	// (operations.Principal.IsAnonymous), чтобы производитель метки и её
	// распознаватели не разъехались; `system` остаётся отдельным, более
	// строгим отказом.
	if p.IsAnonymous() || p.Type == "system" {
		return ""
	}
	return p.Type + ":" + p.ID
}

// FilterVisiblePage — единый entry-point для List use-case'ов всех ресурсов storage:
// берёт СТРАНИЦУ, уже прочитанную из БД курсором, и оставляет из неё только видимые
// вызывающему subject'у строки. Порядок входа сохраняется — страница упорядочена
// курсором, переупорядочивание сломало бы пагинацию.
//
//   - filter == nil  → passthrough (list-filter disabled / dev; production boot-guard
//     запрещает такую посадку на развёрнутом стенде).
//   - пустая страница → как есть, без round-trip'а в iam.
//   - subject == ""   → fail-closed ПУСТАЯ страница: запрос без caller-identity
//     (system / потерянный forwarded-principal) не перечисляет чужие объекты. Пустой
//     ответ, а не ошибка — существование чужих ресурсов остаётся непознаваемым.
//   - ошибка фильтра  → возвращается как есть (fail-closed Unavailable), НИКОГДА не
//     нефильтрованная страница.
//
// Дженерик, потому что три List-пути storage (Volume / Snapshot / Image) отличаются
// ТОЛЬКО типом записи и извлечением id — альтернатива это троекратно скопированный
// одинаковый цикл в use-case'ах (LEAN). Порт остаётся id-based: authzfilter не знает
// domain-типов, caller передаёт idOf.
func FilterVisiblePage[T any](
	ctx context.Context,
	filter Filter,
	resourceType, action string,
	page []T,
	idOf func(T) string,
) ([]T, error) {
	if filter == nil || len(page) == 0 {
		return page, nil
	}
	subject := SubjectFromPrincipal(ctx)
	if subject == "" {
		return nil, nil
	}

	ids := make([]string, 0, len(page))
	for _, rec := range page {
		ids = append(ids, idOf(rec))
	}
	visibleIDs, err := filter.FilterVisibleIDs(ctx, subject, resourceType, action, ids)
	if err != nil {
		return nil, err
	}
	visible := make(map[string]struct{}, len(visibleIDs))
	for _, id := range visibleIDs {
		visible[id] = struct{}{}
	}
	out := make([]T, 0, len(visibleIDs))
	for _, rec := range page {
		if _, ok := visible[idOf(rec)]; ok {
			out = append(out, rec)
		}
	}
	return out, nil
}
