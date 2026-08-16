// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package loadbalancer

import (
	"context"
	"fmt"
	"log/slog"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/emptypb"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	vpcclient "github.com/PRO-Robotech/kacho/services/nlb/internal/clients/vpc"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// lbUnregisterIntent builds the FGA-unregister-intent (project-hierarchy)
// for a deleted LoadBalancer. The creator tuple is left for IAM-side GC
// (unregister project-hierarchy/parent-link).
func lbUnregisterIntent(id, projectID string) domain.FGARegisterIntent {
	return domain.FGARegisterIntent{
		Kind:       "NetworkLoadBalancer",
		ResourceID: id,
		Tuples: []domain.FGATuple{
			domain.FGAProjectTuple(domain.FGAObjectTypeLoadBalancer, id, projectID),
		},
	}
}

// DeleteLoadBalancerUseCase — sync precheck + async delete.
//
// Sync prechecks:
//   - lb.DeletionProtection=true → FailedPrecondition (фиксированный текст);
//   - HasListeners > 0           → FailedPrecondition "has listener(s); delete first".
//     (target groups wire in THROUGH listeners — no separate TG precheck.)
//
// Worker: Writer-TX → Delete (FK 23503 backstop → ErrFailedPrecondition) +
// outbox-emit DELETED → Commit. Response = google.protobuf.Empty.
type DeleteLoadBalancerUseCase struct {
	repo          Repo
	opsRepo       operations.Repo
	addressClient InternalAddressClient
	logger        *slog.Logger
}

// NewDeleteLoadBalancerUseCase конструктор.
func NewDeleteLoadBalancerUseCase(repo Repo, opsRepo operations.Repo, ac InternalAddressClient, logger *slog.Logger) *DeleteLoadBalancerUseCase {
	if logger == nil {
		logger = slog.Default()
	}
	return &DeleteLoadBalancerUseCase{repo: repo, opsRepo: opsRepo, addressClient: ac, logger: logger}
}

// Execute — sync prechecks + ops insert + spawn worker.
func (u *DeleteLoadBalancerUseCase) Execute(
	ctx context.Context, req *lbv1.DeleteNetworkLoadBalancerRequest,
) (*operations.Operation, error) {
	id := req.GetNetworkLoadBalancerId()
	if id == "" {
		return nil, errInvalidArg("network_load_balancer_id", "required")
	}
	if err := validateLoadBalancerID(id); err != nil {
		return nil, err
	}

	// Sync prechecks (read reader-TX).
	rd, err := u.repo.Reader(ctx)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	defer func() { _ = rd.Close() }()

	cur, err := rd.LoadBalancers().Get(ctx, id)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	if cur.DeletionProtection {
		return nil, status.Error(codes.FailedPrecondition,
			"load balancer has deletion protection enabled")
	}
	hasListeners, err := rd.LoadBalancers().HasListeners(ctx, id)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	if hasListeners {
		// HasListeners (EXISTS) уже подтвердил наличие детей — не тянем полный
		// список ради счётчика (лишний fetch + silent-cap на >1000). Текст
		// ДОСЛОВНО совпадает с обоими производителями того же отказа на стороне
		// БД: guard'ом MarkDeleting (`markDeletingBlockReason`) и отображением
		// 23503 у `listeners_load_balancer_id_fkey` (repo/kacho/pg/restrict_fk.go).
		// Прежняя редакция этого комментария заявляла совпадение, которого не
		// было: путь FK отдавал безымянное «has dependent resources». Target groups
		// wire in THROUGH listeners (NLB CONTRACT — no M:N pivot), so "no listeners"
		// already implies "no wired target group": no separate TG precheck needed.
		return nil, status.Errorf(codes.FailedPrecondition,
			"NetworkLoadBalancer %s has listener(s); delete first", id)
	}

	// Operation row.
	op, err := operations.NewFromContext(ctx,
		ids.PrefixOperationNLB,
		fmt.Sprintf("Delete NetworkLoadBalancer %s", id),
		&lbv1.DeleteNetworkLoadBalancerMetadata{NetworkLoadBalancerId: id},
	)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	principal := operations.PrincipalFromContext(ctx)
	if err := u.opsRepo.CreateWithPrincipal(ctx, op, principal); err != nil {
		return nil, mapDomainErr(err)
	}

	projectID := string(cur.ProjectID)
	operations.Run(ctx, u.opsRepo, op.ID, func(workerCtx context.Context) (*anypb.Any, error) {
		// Worker-ctx детачнут — восстанавливаем principal, чтобы release-вызовы в vpc
		// (ClearReference/FreeIP) несли identity тенанта (иначе authz_no_principal).
		return u.doDelete(operations.WithPrincipal(workerCtx, principal), id, projectID)
	})

	return &op, nil
}

// vipBinding — per-family VIP-привязка LB (release-вход для Delete).
//
// Адрес несётся рядом с идентификатором аренды намеренно: без него освобождение
// не отличает «у этого семейства аренды нет» от «аренда есть, а её
// идентификатор потерян». Оба состояния выглядят как пустой идентификатор, а
// исходы у них противоположные — см. releaseVIP.
type vipBinding struct {
	addressV4   string
	addressV6   string
	addressIDV4 string
	addressIDV6 string
	originV4    domain.VipOrigin
	originV6    domain.VipOrigin
}

// doDelete — durable-handle Delete-сага (mark→release→delete):
//
//  1. MarkDeleting — атомарный guarded-переход в status=DELETING (unprotected +
//     no children) ДО необратимого release VIP. Provал guard'а → fail БЕЗ release
//     (строка цела, VIP не тронут). Пометка DELETING также делает строку видимой
//     free_ip_runner'у.
//  2. Per-family release VIP (auto → ClearReference→FreeIP, byo → ClearReference;
//     идемпотентно по address_id, NotFound=успех).
//  3. Writer-TX: DELETE строки + outbox DELETED + fga-unregister → Commit.
//
// Краш/release-сбой между 1 и 3 оставляет строку в DELETING → free_ip_runner
// доводит release+DELETE (durable-handle self-heal). FK 23503 backstop на
// финальном DELETE недостижим (mark-guard требует отсутствия детей, а
// child-INSERT'ы отвергают DELETING-родителя), но `Delete` его сохраняет как
// defense-in-depth.
func (u *DeleteLoadBalancerUseCase) doDelete(ctx context.Context, id, projectID string) (*anypb.Any, error) {
	// Шаг 1: атомарно пометить DELETING ДО release VIP.
	marked, err := u.markDeleting(ctx, id)
	if err != nil {
		return nil, err
	}
	// VIP-binding — из строки под mark-lock (устойчиво к гонке с AttachVIP).
	vip := vipBinding{
		addressV4:   string(marked.AddressV4),
		addressV6:   string(marked.AddressV6),
		addressIDV4: string(marked.AddressIDV4),
		addressIDV6: string(marked.AddressIDV6),
		originV4:    marked.VipOriginV4,
		originV6:    marked.VipOriginV6,
	}

	// Шаг 2: per-family release VIP (раздельно v4/v6).
	if err := u.releaseVIP(ctx, "IPV4", vip.addressV4, vip.addressIDV4, projectID, id); err != nil {
		return nil, mapDomainErr(err)
	}
	if err := u.releaseVIP(ctx, "IPV6", vip.addressV6, vip.addressIDV6, projectID, id); err != nil {
		return nil, mapDomainErr(err)
	}

	// Шаг 3: финальный DELETE + outbox DELETED + fga-unregister (одна TX).
	w, err := u.repo.Writer(ctx)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	defer w.Abort()

	if err := w.LoadBalancers().Delete(ctx, id); err != nil {
		return nil, mapDomainErr(err)
	}
	if err := w.Outbox().Emit(ctx,
		kachorepo.OutboxResourceLoadBalancer, id, projectID,
		kachorepo.OutboxActionDeleted, map[string]any{"id": id, "project_id": projectID},
	); err != nil {
		return nil, mapDomainErr(err)
	}
	// FGA-unregister-intent (project-hierarchy) in the SAME tx as the
	// Delete — drainer applies UnregisterResource to remove the owner-tuple.
	if _, err := w.FGARegisterOutbox().Emit(ctx, domain.FGAEventUnregister,
		lbUnregisterIntent(id, projectID)); err != nil {
		return nil, mapDomainErr(err)
	}
	if err := w.Commit(); err != nil {
		return nil, mapDomainErr(err)
	}

	out, err := anypb.New(&emptypb.Empty{})
	if err != nil {
		return nil, mapDomainErr(err)
	}
	return out, nil
}

// markDeleting — отдельная закоммиченная Writer-TX с атомарным MarkDeleting-guard'ом
// (первый шаг Delete). DELETING обязан быть durable ДО release VIP: (а) при провале
// guard'а VIP не трогается, (б) при краше после release строка в DELETING
// самозалечивается free_ip_runner'ом. Конкурентный Update(deletion_protection=true)
// или появившийся ребёнок между sync-precheck и этим guard'ом пресекается на
// DB-уровне (0 rows → FailedPrecondition).
func (u *DeleteLoadBalancerUseCase) markDeleting(ctx context.Context, id string) (*kachorepo.LoadBalancerRecord, error) {
	w, err := u.repo.Writer(ctx)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	defer w.Abort()
	rec, err := w.LoadBalancers().MarkDeleting(ctx, id)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	if err := w.Commit(); err != nil {
		return nil, mapDomainErr(err)
	}
	return rec, nil
}

// releaseVIP — снимает аренду VIP одного семейства по address_id ОДНИМ глаголом
// владельца, который называет исход полем ответа.
//
// ЧТО ЗДЕСЬ БЫЛО И ПОЧЕМУ ЭТО БЫЛ ДЕФЕКТ (#439). Прежде освобождение было парой
// вызовов, спрашивавших владельца ПООБЪЕКТНО — про сам адрес. На пообъектном
// вопросе ответ «не найдено» не означает «аренды нет»: тем же ответом владелец
// намеренно отвечает на промах чужого проекта, а опрос операции схлопывает в
// него же три разных положения дел, включая «у тебя нет ключа владельца». Пара
// принимала этот ответ за доказательство выполненной работы и возвращала успех,
// после чего шаг 3 сносил строку — и с этого мгновения аренду не видел никто:
// реконсайлер ищет от строки потребителя, а обратного поиска у владельца нет.
//
// Теперь вопрос задан так, что такого ответа не бывает: право анкорится на
// проекте, исход приезжает полем. Решение «удалить адрес или оставить адрес
// арендатора» принимает ВЛАДЕЛЕЦ по своей колонке — потребитель его больше не
// выводит и не может ошибиться направлением сравнения.
//
// ПУСТОЙ addressID — ДВА РАЗНЫХ СОСТОЯНИЯ, и различает их адрес (#467):
//
//   - адреса нет и идентификатора нет — у балансировщика нет этого семейства,
//     освобождать нечего, это законный вход;
//   - адрес ЕСТЬ, а идентификатора нет — аренда выдана, а ключ к ней потерян.
//
// Прежняя редакция принимала оба за первое и возвращала успех. Дальше шаг 3
// удалял строку, и с этого мгновения аренду не видел НИКТО: реконсайлер выбирает
// только строки в DELETING/CREATING, а обратного поиска «что принадлежит этому
// балансировщику» на стороне vpc не существует. Подсеть после этого не удалить
// никогда — `Subnet has allocated internal addresses`, — и починить некому.
//
// Поэтому второе состояние — ОТКАЗ. Он оставляет строку в DELETING, то есть
// оставляет зацепку: её видит реконсайлер и видит человек. Отказ шумен и
// поправим; успех тих и необратим. Из двух ошибок выбираем ту, которую ещё можно
// заметить.
func (u *DeleteLoadBalancerUseCase) releaseVIP(
	ctx context.Context, family, address, addressID, projectID, lbID string,
) error {
	if addressID == "" {
		if address == "" {
			return nil // семейства у балансировщика нет — освобождать нечего
		}
		return status.Errorf(codes.Internal,
			"load balancer %s lease is not releasable: address is set but its address id is empty", family)
	}
	if u.addressClient == nil {
		return status.Error(codes.Unavailable, "vpc internal address client not configured")
	}
	// Исход читается, а не выводится. Все четыре названных исхода означают
	// «этот потребитель аренды на этом адресе не держит» — то есть работу,
	// доведённую до постусловия; любой ОТКАЗ означает, что она не доведена, и
	// тогда строка потребителя обязана уцелеть (шаг 3 не выполняется).
	if _, err := u.addressClient.ReleaseLease(ctx, vpcclient.ReleaseLeaseRequest{
		ProjectID: projectID,
		AddressID: addressID,
		Owner:     vpcclient.AddressOwner{Kind: lbAddressOwnerKind, ID: lbID},
	}); err != nil {
		return err
	}
	return nil
}
