// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package listener

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/apps/kacho/api/shared"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/authzfilter"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"

	"github.com/PRO-Robotech/kacho/pkg/listnarrow"
)

// ListUseCase — sync list listeners фильтрованный по `load_balancer_id`
// . Cursor-based pagination через repo'шный
// `(created_at, id)` token (см. listener_repo.go).
//
// Поддерживаемые фильтры (per proto + design):
//   - load_balancer_id   — required (пустой отвергается use-case синхронно)
//   - filter=`name="…"`  — optional name-equality filter через общий
//     shared.ParseNameFilter (kacho-corelib/filter.Parse, whitelist {"name"});
//     unknown-поле / unquoted / malformed → InvalidArgument.
type ListUseCase struct {
	repo  RepoFactory
	authz *listnarrow.Narrower
}

// NewListUseCase — конструктор. authz может быть nil (list-filter disabled / dev).
func NewListUseCase(repo RepoFactory, authz *listnarrow.Narrower) *ListUseCase {
	return &ListUseCase{repo: repo, authz: authz}
}

// Run выполняет List.
//
// Mapping:
//
//	req.LoadBalancerId == "" → InvalidArgument "load_balancer_id required"
//	repo error               → mapDomainErr (sentinel-aware)
func (u *ListUseCase) Run(ctx context.Context, req *lbv1.ListListenersRequest) (*lbv1.ListListenersResponse, error) {
	// project-scoped (parity with NLB/TG List). project_id is required;
	// load_balancer_id is an optional filter (restrict to one parent LB).
	projectID := req.GetProjectId()
	if projectID == "" {
		return nil, status.Error(codes.InvalidArgument, "project_id required")
	}

	name, err := shared.ParseNameFilter(req.GetFilter())
	if err != nil {
		return nil, err
	}

	filter := kachorepo.ListenerFilter{
		ProjectID:      projectID,
		LoadBalancerID: req.GetLoadBalancerId(),
		Name:           name,
	}

	// Validate pagination BEFORE reading the page (see loadbalancer/list.go).
	if err := shared.ValidatePagination(req.GetPageToken(), req.GetPageSize()); err != nil {
		return nil, err
	}

	// Предусловие сужения — ДО чтения страницы из БД: и «никого не назвали», и
	// «спросить негде» решаются одной функцией общего фундамента, поэтому ответ
	// совпадает у всех списков по построению, а не по внимательности. Проверка
	// формата остаётся ПЕРВОЙ и выше — ответ на некорректный ввод не должен
	// зависеть от того, что вызывающему выдано.
	if err := listnarrow.Precheck(ctx, u.authz); err != nil {
		return nil, err
	}
	// Страница читается и read-TX ЗАКРЫВАЕТСЯ до опроса прав (см. readPage).
	page, nextToken, err := u.readPage(ctx, filter, kachorepo.Pagination{
		PageSize:  req.GetPageSize(),
		PageToken: req.GetPageToken(),
	})
	if err != nil {
		return nil, mapDomainErr(err)
	}

	// RBAC: per-object FGA filter — страница из БД ПЕРВОЙ, права на её id
	// (см. loadbalancer/list.go и package-doc internal/authzfilter).
	page, err = listnarrow.Page(ctx, u.authz,
		authzfilter.ResourceTypeListener, authzfilter.ActionListenerList,
		page, func(rec *kachorepo.ListenerRecord) string { return string(rec.ID) })
	if err != nil {
		return nil, err
	}

	resp := &lbv1.ListListenersResponse{NextPageToken: nextToken}
	for _, rec := range page {
		pb, err := listenerRecordToPb(rec)
		if err != nil {
			return nil, err
		}
		resp.Listeners = append(resp.Listeners, pb)
	}
	return resp, nil
}

// readPage читает страницу и ОТДАЁТ соединение пула обратно до возврата — read-TX
// не удерживается через сетевое ожидание iam в FilterVisiblePage. Полное
// обоснование и цена удержания — loadbalancer/list.go readPage; закрывать
// безопасно, потому что List вычитывает строки в срез целиком, а маппинг в proto
// к БД не обращается.
func (u *ListUseCase) readPage(
	ctx context.Context, filter kachorepo.ListenerFilter, page kachorepo.Pagination,
) ([]*kachorepo.ListenerRecord, string, error) {
	rd, err := u.repo.Reader(ctx)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = rd.Close() }()
	return rd.Listeners().List(ctx, filter, page)
}
