// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package authzfilter

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// SubjectFromCtx — извлекает FGA subject ("user:<id>" / "service_account:<id>")
// из ctx через operations.PrincipalFromContext (nlb-конвенция: principal кладётся
// grpcsrv.UnaryPrincipalExtract на обоих listener'ах). system / unauthenticated
// principal → "". Пустой subject НЕ трактуется как bypass: FilterPage прокидывает
// его в filter, который fail-close'ит (enabled FGAFilter → Unauthenticated).
//
// Зеркало internal/domain.FGASubjectFromPrincipal — единый формат subject-строки.
func SubjectFromCtx(ctx context.Context) string {
	p := operations.PrincipalFromContext(ctx)
	return domain.FGASubjectFromPrincipal(p.Type, p.ID)
}

// FilterPage — единый entry-point для List use-case'ов всех ресурсов nlb: берёт
// СТРАНИЦУ, уже прочитанную из БД курсором, и оставляет из неё только видимые
// вызывающему subject'у id (порядок входа сохраняется — страница упорядочена
// курсором, переупорядочивание сломало бы пагинацию).
//
//   - filter == nil               → passthrough (list-filter disabled / dev — явно
//     не сконфигурирован filter в composition root).
//   - пустая страница             → nil без обращения к iam.
//   - subject == "" (system)      → subject прокидывается в filter КАК ЕСТЬ
//     (fail-closed). Реальный FGAFilter вернёт Unauthenticated (enabled) либо
//     passthrough (disabled — enabled=false проверяется ДО subject); никакого
//     short-circuit-bypass на пустом subject'е здесь НЕТ — это была cross-tenant
//     enumeration дыра (CWE-862): запрос, потерявший forwarded-principal, не
//     должен перечислять чужой проект.
//   - filter error                → возвращается как есть (fail-closed
//     Unauthenticated/Unavailable), НИКОГДА не нефильтрованная страница.
//
// Use-case'ы НЕ дублируют subject-extraction/passthrough-логику — зовут FilterPage
// (или FilterVisiblePage, если фильтруют записи, а не голые id).
func FilterPage(ctx context.Context, filter Filter, resourceType, action string, ids []string) ([]string, error) {
	if filter == nil {
		return ids, nil
	}
	if len(ids) == 0 {
		return nil, nil
	}
	subject := SubjectFromCtx(ctx)
	visible, err := filter.FilterVisibleIDs(ctx, subject, resourceType, action, ids)
	if err != nil {
		// Fail-closed: любая ошибка фильтра — Unavailable. FGAFilter уже маппит на
		// gRPC-status; defensive guard на случай, если реализация Filter вернула
		// не-status ошибку (иначе она утекла бы как codes.Unknown, не fail-closed).
		// НЕ возвращаем нефильтрованную страницу — no-leak.
		if _, ok := status.FromError(err); !ok {
			return nil, status.Errorf(codes.Unavailable, "list filter: %v", err)
		}
		return nil, err
	}
	return visible, nil
}

// FilterVisiblePage — FilterPage над страницей ЗАПИСЕЙ: оставляет только видимые
// строки, сохраняя порядок курсора. Дженерик, потому что три List-пути nlb
// (LoadBalancer / Listener / TargetGroup) отличаются ТОЛЬКО типом записи и
// извлечением id — альтернатива это троекратно скопированный одинаковый цикл в
// use-case'ах (LEAN). Порт остаётся id-based: authzfilter не знает repo-типов,
// caller передаёт idOf.
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
	ids := make([]string, 0, len(page))
	for _, rec := range page {
		ids = append(ids, idOf(rec))
	}
	visibleIDs, err := FilterPage(ctx, filter, resourceType, action, ids)
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
