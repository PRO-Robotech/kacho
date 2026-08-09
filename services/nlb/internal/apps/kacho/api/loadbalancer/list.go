// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package loadbalancer

import (
	"context"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/apps/kacho/api/shared"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/authzfilter"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"

	"github.com/PRO-Robotech/kacho/pkg/listnarrow"
)

// ListLoadBalancersUseCase — sync list с фильтром `project_id` (required) +
// optional `name="<value>"` (от proto request.Filter, через общий
// shared.ParseNameFilter — kacho-corelib/filter.Parse, whitelist {"name"}) +
// cursor-based pagination.
//
// Порядок принципиален: страница берётся из БД ПЕРВОЙ, права проверяются на её
// идентификаторах (per-object BatchCheck). Обратный порядок («перечисли все
// разрешённые id → сузь ими SQL») упирался в жёсткий предел OpenFGA ListObjects
// (1000) и делал собственные ресурсы тенанта невидимыми — см. package-doc
// `internal/authzfilter`. Побочный эффект нового порядка: страница может вернуться
// НЕПОЛНОЙ (часть строк отфильтрована) — это нормально для cursor-пагинации,
// next_page_token берётся от последней ПРОСМОТРЕННОЙ строки, поэтому обхода без
// пропусков это не ломает.
type ListLoadBalancersUseCase struct {
	repo  Repo
	authz *listnarrow.Narrower
}

// NewListLoadBalancersUseCase конструктор. authz может быть nil
// (list-filter disabled / dev) → нефильтрованный project-scoped passthrough.
func NewListLoadBalancersUseCase(repo Repo, authz *listnarrow.Narrower) *ListLoadBalancersUseCase {
	return &ListLoadBalancersUseCase{repo: repo, authz: authz}
}

// Execute — читает страницу (read-TX закрывается сразу после SELECT'а, см.
// readPage), сужает её per-object фильтром видимости, отдаёт DTO.
//
// RBAC: per-object FGA filter. subject из ctx → страница из БД → iam BatchCheck
// (viewer ∪ v_list) на id этой страницы. Ничего не видно → пустой ответ (no-leak).
// iam недоступен → Unavailable (fail-closed, НЕ нефильтрованная страница).
func (u *ListLoadBalancersUseCase) Execute(
	ctx context.Context, req *lbv1.ListNetworkLoadBalancersRequest,
) (*lbv1.ListNetworkLoadBalancersResponse, error) {
	projectID := req.GetProjectId()
	if projectID == "" {
		return nil, errInvalidArg("project_id", "required")
	}

	name, err := shared.ParseNameFilter(req.GetFilter())
	if err != nil {
		return nil, err
	}
	filter := kachorepo.LoadBalancerFilter{
		ProjectID: projectID,
		Filter:    req.GetFilter(),
		Name:      name,
	}

	// Validate pagination BEFORE reading the page, so a malformed page_token /
	// out-of-range page_size is 400 InvalidArgument regardless of grant state
	// (api-convention parity with compute + vpc; repo decodePageToken/
	// pageSizeOrDefault remain backstop).
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
	recs, next, err := u.readPage(ctx, filter, kachorepo.Pagination{
		PageToken: req.GetPageToken(),
		PageSize:  req.GetPageSize(),
	})
	if err != nil {
		return nil, mapDomainErr(err)
	}

	// RBAC: оставить из страницы только видимые subject'у строки (per-object).
	recs, err = listnarrow.Page(ctx, u.authz,
		authzfilter.ResourceTypeLoadBalancer, authzfilter.ActionLoadBalancerList,
		recs, func(rec *kachorepo.LoadBalancerRecord) string { return string(rec.ID) })
	if err != nil {
		return nil, err
	}

	resp := &lbv1.ListNetworkLoadBalancersResponse{NextPageToken: next}
	resp.NetworkLoadBalancers = make([]*lbv1.NetworkLoadBalancer, 0, len(recs))
	for _, rec := range recs {
		pb, err := lbRecordToProto(rec)
		if err != nil {
			return nil, err
		}
		resp.NetworkLoadBalancers = append(resp.NetworkLoadBalancers, pb)
	}
	return resp, nil
}

// readPage читает страницу и ОТДАЁТ соединение пула обратно до возврата: read-TX
// живёт ровно на время SELECT'а, а не до конца RPC.
//
// Reader — это занятая ссуда пула (read-only TX на pgxpool, см. repo/kacho/pg),
// и следом за чтением идёт FilterVisiblePage — сетевое ожидание ответа iam
// (BatchCheck волнами, per-call дедлайны). Держать соединение через это ожидание
// значит вычитать его из пула на всё время round-trip'а соседа: пул один
// (slave == master), поэтому как только одновременных List'ов становится столько
// же, сколько соединений в пуле, следующий Reader ИЛИ Writer — то есть Get и любая
// мутация — ждёт iam, чтобы дотянуться до здоровой БД, и отвечает
// DEADLINE_EXCEEDED/UNAVAILABLE.
//
// Закрывать безопасно: List вычитывает строки в срез целиком (rows.Close внутри),
// маппинг в proto к БД не обращается, второго запроса в этой TX нет. Порядок
// зафиксирован тестами list_reader_release_test.go (порядок) и
// list_pool_release_integration_test.go (реальный пул на одно соединение).
func (u *ListLoadBalancersUseCase) readPage(
	ctx context.Context, filter kachorepo.LoadBalancerFilter, page kachorepo.Pagination,
) ([]*kachorepo.LoadBalancerRecord, string, error) {
	rd, err := u.repo.Reader(ctx)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = rd.Close() }()
	return rd.LoadBalancers().List(ctx, filter, page)
}
