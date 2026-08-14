// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package network

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/emptypb"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	corevalidate "github.com/PRO-Robotech/kacho/pkg/validate"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/fgaregister"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// DeleteNetworkUseCase — sync FAILED_PRECONDITION если в Network есть subnets /
// tenant route tables / non-default SG. Async-часть (worker): cleanup
// system-provisioned default-SG И default-RouteTable + Network.Delete + все
// outbox-emit'ы — в одной writer-TX (atomic). FK RESTRICT — DB-уровневый backstop.
//
// Собственные system-provisioned ресурсы сети (default-SG, default-RT) НЕ делают
// её «непустой»: их создал сам сервис при Create, и он же снимает их здесь.
// Непустой сеть делают только tenant-ресурсы.
type DeleteNetworkUseCase struct {
	repo           Repo
	subnetReader   SubnetReader      // may be nil → skip child class
	routeTableRead RouteTableReader  // may be nil
	sgRepo         SecurityGroupRepo // may be nil → skip default-SG cleanup
	opsRepo        operations.Repo
}

// NewDeleteNetworkUseCase создает DeleteNetworkUseCase. Все child-reader'ы
// необязательны: nil → пропускаем соответствующий child-класс (для unit-тестов
// со scoped wiring).
func NewDeleteNetworkUseCase(r Repo, subnetReader SubnetReader, routeTableRead RouteTableReader, sgRepo SecurityGroupRepo, opsRepo operations.Repo) *DeleteNetworkUseCase {
	return &DeleteNetworkUseCase{
		repo:           r,
		subnetReader:   subnetReader,
		routeTableRead: routeTableRead,
		sgRepo:         sgRepo,
		opsRepo:        opsRepo,
	}
}

// Execute инициирует Delete: sync-проверки → Operation → worker.
func (u *DeleteNetworkUseCase) Execute(ctx context.Context, id string) (*operations.Operation, error) {
	if err := corevalidate.ResourceID("network", ids.PrefixNetwork, id); err != nil {
		return nil, err
	}
	if id == "" {
		return nil, status.Error(codes.InvalidArgument, "network_id required")
	}
	if err := u.checkNetworkEmpty(ctx, id); err != nil {
		return nil, err
	}

	op, err := operations.NewFromContext(
		ctx,
		ids.PrefixOperationVPC,
		fmt.Sprintf("Delete network %s", id),
		&vpcv1.DeleteNetworkMetadata{NetworkId: id},
	)
	if err != nil {
		return nil, err
	}
	if err := u.opsRepo.Create(ctx, op); err != nil {
		return nil, err
	}

	if err := operations.RunSync(ctx, u.opsRepo, &op, func(ctx context.Context) (*anypb.Any, error) {
		return u.doDelete(ctx, id)
	}); err != nil {
		return nil, err
	}

	return &op, nil
}

// doDelete — async-часть Delete. Default-SG cleanup + Network.Delete + оба
// outbox-emit'а идут в ОДНОЙ writer-TX: либо все применяется, либо ничего
// (atomic, нет orphan-window сети без default-SG).
func (u *DeleteNetworkUseCase) doDelete(ctx context.Context, id string) (*anypb.Any, error) {
	w, err := u.repo.Writer(ctx)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	defer w.Abort()

	// Собираем FGA owner-tuples для unregister в ТОЙ ЖЕ writer-TX, что и Delete:
	// ресурс исчезает — его место в hierarchy тоже. projectID нужен как subject
	// tuple'а; читаем его из строки до удаления.
	var unregTuples []fgaregister.Tuple

	// Системная default-RouteTable снимается в той же writer-TX (симметрия
	// default-SG): её создал Network.Create, поэтому Delete сети обязан её
	// забрать — иначе orphan-RT и FK RESTRICT, который сеть больше никогда не
	// даст удалить. Tenant-RT не трогаем: их наличие уже отвергнуто
	// checkNetworkEmpty (а если проскочит — FK RESTRICT остаётся backstop'ом).
	if n, gerr := w.Networks().Get(ctx, id); gerr == nil && n.DefaultRouteTableID != "" {
		if derr := w.RouteTables().Delete(ctx, n.DefaultRouteTableID); derr != nil && !errors.Is(derr, repo.ErrNotFound) {
			return nil, serviceerr.MapRepoErr(derr)
		}
		if oerr := w.Outbox().Emit(ctx, "RouteTable", n.DefaultRouteTableID, "DELETED", map[string]any{"id": n.DefaultRouteTableID}); oerr != nil {
			return nil, serviceerr.MapRepoErr(fmt.Errorf("%w: outbox emit: %v", repo.ErrInternal, oerr))
		}
		unregTuples = append(unregTuples,
			fgaregister.ProjectHierarchy(n.ProjectID, "vpc_route_table", n.DefaultRouteTableID))
	}

	// Default-SG cleanup в той же writer-TX. Не-default SG — preserve, FK
	// RESTRICT не даст удалить Network ⇒ FAILED_PRECONDITION с перечнем мешающего
	// (см. checkNetworkEmpty; sync-путь отвергает раньше, FK — backstop).
	// sgRepo == nil → default-SG-inline выключен, чистить нечего.
	if u.sgRepo != nil {
		n, gerr := w.Networks().Get(ctx, id)
		switch {
		case gerr == nil && n.DefaultSecurityGroupID != "":
			if derr := w.SecurityGroups().Delete(ctx, n.DefaultSecurityGroupID); derr != nil && !errors.Is(derr, repo.ErrNotFound) {
				return nil, serviceerr.MapRepoErr(derr)
			}
			if oerr := w.Outbox().Emit(ctx, "SecurityGroup", n.DefaultSecurityGroupID, "DELETED", map[string]any{"id": n.DefaultSecurityGroupID}); oerr != nil {
				return nil, serviceerr.MapRepoErr(fmt.Errorf("%w: outbox emit: %v", repo.ErrInternal, oerr))
			}
			unregTuples = append(unregTuples,
				fgaregister.ProjectHierarchy(n.ProjectID, "vpc_security_group", n.DefaultSecurityGroupID))
		case errors.Is(gerr, repo.ErrNotFound):
			// Сеть уже исчезла — пусть Networks().Delete ниже вернет каноничный NotFound.
		case gerr != nil:
			return nil, serviceerr.MapRepoErr(gerr)
		}
	}

	// Читаем projectID для network-unregister-tuple (best-effort: если строка уже
	// исчезла, Networks().Delete ниже вернет каноничный NotFound).
	if n, gerr := w.Networks().Get(ctx, id); gerr == nil {
		unregTuples = append(unregTuples,
			fgaregister.ProjectHierarchy(n.ProjectID, "vpc_network", id))
	}

	if err := w.Networks().Delete(ctx, id); err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	if err := w.Outbox().Emit(ctx, "Network", id, "DELETED", map[string]any{"id": id}); err != nil {
		return nil, serviceerr.MapRepoErr(fmt.Errorf("%w: outbox emit: %v", repo.ErrInternal, err))
	}
	if len(unregTuples) > 0 {
		if err := w.FGARegister().EmitUnregister(ctx, fgaregister.RegisterIntent(unregTuples...)); err != nil {
			return nil, serviceerr.MapRepoErr(fmt.Errorf("%w: fga unregister intent: %v", repo.ErrInternal, err))
		}
	}
	if err := w.Commit(); err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	return anypb.New(&emptypb.Empty{})
}

// checkNetworkEmpty — sync FAILED_PRECONDITION, если в сети еще есть subnets /
// tenant route tables / non-default security groups. Reader'ы могут быть nil —
// тогда соответствующий child-класс не проверяется.
//
// Текст контракта: `"Network <id> is not empty (subnets: 2, route tables: 1)"` —
// отказ ПЕРЕЧИСЛЯЕТ мешающее по видам и числам. Прежняя редакция называла только
// факт непустоты, и арендатор выяснял радиус ПЕРЕБОРОМ: снял подсети, повторил,
// получил тот же текст из-за группы правил, повторил снова. Отсюда два свойства
// перечисления, каждое закреплено пробой:
//
//   - **опрашиваются ВСЕ классы, а не до первого мешающего** — иначе перечень
//     называл бы один вид из трёх, то есть снова не радиус. Стоимость: на пустой
//     сети три чтения, как и прежде (там короткого замыкания не было by
//     construction); на непустой — те же три вместо одного;
//   - **печатаются ВИДЫ И ЧИСЛА, никогда идентификаторы дочерних.** Число
//     координатой не является; перечень идентификаторов чужих объектов ею
//     становится. Класс с нулём мешающих в перечень не попадает — «subnets: 0»
//     не сообщает ничего, а перечень с нулями нельзя прочитать как радиус.
//
// Из RT-проверки исключается СОБСТВЕННАЯ system-provisioned default-RT сети
// (`network.defaultRouteTableId°`) — ровно так же, как из SG-проверки исключается
// default-SG (`DefaultForNetwork`). Иначе сеть, которой сервис сам провижнит RT
// на Create, стала бы неудаляемой навсегда. Оба системных ребёнка снимает
// doDelete в той же writer-TX, поэтому непустой сеть делают только tenant-строки.
func (u *DeleteNetworkUseCase) checkNetworkEmpty(ctx context.Context, networkID string) error {
	// id системной RT (пусто, если сеть не найдена — Delete ниже вернёт NotFound).
	var systemRT string
	if rd, err := u.repo.Reader(ctx); err == nil {
		if n, gerr := rd.Networks().Get(ctx, networkID); gerr == nil {
			systemRT = n.DefaultRouteTableID
		}
		_ = rd.Close()
	}

	var blockers []string
	countKind := func(kind string, count int) {
		if count > 0 {
			blockers = append(blockers, fmt.Sprintf("%s: %d", kind, count))
		}
	}

	if u.subnetReader != nil {
		n, err := countPagedChildren(ctx,
			func(ctx context.Context, token string) ([]*kacho.SubnetRecord, string, error) {
				return u.subnetReader.List(ctx, SubnetFilter{NetworkID: networkID}, childCountPage(token))
			},
			func(*kacho.SubnetRecord) bool { return true })
		if err != nil {
			return serviceerr.MapRepoErr(err)
		}
		countKind("subnets", n)
	}
	if u.routeTableRead != nil {
		n, err := countPagedChildren(ctx,
			func(ctx context.Context, token string) ([]*kacho.RouteTableRecord, string, error) {
				return u.routeTableRead.List(ctx, RouteTableFilter{NetworkID: networkID}, childCountPage(token))
			},
			func(rt *kacho.RouteTableRecord) bool { return rt.ID != systemRT })
		if err != nil {
			return serviceerr.MapRepoErr(err)
		}
		countKind("route tables", n)
	}
	if u.sgRepo != nil {
		n, err := countPagedChildren(ctx,
			func(ctx context.Context, token string) ([]*kacho.SecurityGroupRecord, string, error) {
				return u.sgRepo.List(ctx, SecurityGroupFilter{NetworkID: networkID}, childCountPage(token))
			},
			func(sg *kacho.SecurityGroupRecord) bool { return !sg.DefaultForNetwork })
		if err != nil {
			return serviceerr.MapRepoErr(err)
		}
		countKind("security groups", n)
	}

	if len(blockers) == 0 {
		return nil
	}
	return status.Errorf(codes.FailedPrecondition,
		"Network %s is not empty (%s)", networkID, strings.Join(blockers, ", "))
}

// childCountPage — страница курсора для подсчёта дочерних. Размер берётся у
// верхней границы контракта (`corevalidate.MaxPageSize`), а не выписывается
// числом: чем крупнее страница, тем меньше обходов на один отказ, а границу всё
// равно валидирует репозиторий — двух копий предела не заводится.
func childCountPage(token string) Pagination {
	return Pagination{PageToken: token, PageSize: corevalidate.MaxPageSize}
}

// countPagedChildren считает дочерние, проходя ВСЕ страницы курсора.
//
// Одна страница лгала бы числом: List отдаёт не больше page_size строк, поэтому
// счёт по первой странице занижал бы радиус ровно там, где он велик, — арендатор
// чинил бы «subnets: 1000» при трёх тысячах. Прежней проверке «пусто?» одной
// страницы хватало (пустая первая страница ⟹ строк нет вовсе); у счёта этого
// свойства нет.
//
// Курсор непрозрачен: токен приходит от репозитория и уезжает обратно КАК ЕСТЬ,
// без разбора. Токен, не сдвинувший курсор, — нарушение контракта чтения, а не
// «страниц больше нет»: вернуть на нём недосчёт значило бы выдать заниженное
// число за измеренное, поэтому это ErrInternal (наружу — фиксированный
// непрозрачный текст). Проверяется именно НЕсдвиг, наблюдаемый за один шаг;
// цикл длиннее одного шага ни один репозиторий дерева не производит, а вечное
// вращение здесь держало бы горутину запроса.
func countPagedChildren[T any](
	ctx context.Context,
	page func(ctx context.Context, token string) ([]T, string, error),
	blocks func(T) bool,
) (int, error) {
	total := 0
	token := ""
	for {
		rows, next, err := page(ctx, token)
		if err != nil {
			return 0, err
		}
		for _, row := range rows {
			if blocks(row) {
				total++
			}
		}
		if next == "" {
			return total, nil
		}
		if next == token {
			return 0, fmt.Errorf("%w: child count pagination cursor did not advance", repo.ErrInternal)
		}
		token = next
	}
}
