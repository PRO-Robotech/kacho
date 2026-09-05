// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package loadbalancer

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/apps/kacho/api/shared"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// MoveLoadBalancerUseCase — change project_id (cross-project) keeping region.
// Sync prechecks:
//   - same-project — InvalidArgument "destination project is the same as source";
//   - destination project exists (peer ProjectClient.Get);
//   - no child listener wired to a target group (cross-project ref guard: a
//     listener in the destination project must not reference a TG in the source
//     project — Move заблокирован если есть).
//
// Worker: Writer-TX → repo.MoveProject (UPDATE LB + cascade UPDATE listeners) +
// outbox MOVED/UPDATED балансировщика + outbox MOVED НА КАЖДЫЙ переехавший
// слушатель + FGA-register(dst project) + FGA-unregister(src project) → Commit
// (Вариант A: project-rewrite = register new-project tuple + unregister
// old-project tuple, both in the same writer-tx as MoveProject — no dual-write).
//
// Acceptance:.
type MoveLoadBalancerUseCase struct {
	repo          Repo
	opsRepo       operations.Repo
	projectClient ProjectClient
	checkClient   CheckClient
	registrar     Registrar
	logger        *slog.Logger
}

// WithRegistrar подключает sync-primary owner-tuple registrar. Возвращает self
// для chaining. nil-безопасно (sync-путь пропускается).
//
// ПОЧЕМУ MOVE НУЖДАЕТСЯ В ЭТОМ БОЛЬШЕ, ЧЕМ CREATE. Create только ДОБАВЛЯЕТ
// проекцию: пока она не материализовалась, терять нечего, а ресурс всё равно
// только что создан. Move СНАЧАЛА СНОСИТ действующую проекцию (unregister
// источника) и лишь потом ставит новую (register назначения) — то есть это
// единственная мутация, после которой у ресурса в окне материализации нет
// проекции ВООБЩЕ. Всё это время край не резолвит цель проверки прав в проект и
// отвечает вызывающему hide-existence `NotFound` — побайтово тем же текстом, что
// и настоящее «не найдено» (`security.md` §6). Владелец видит собственный
// ресурс исчезнувшим; ждать приходится дренаж.
//
// Ускоритель тут безопасен ПО ПОСТРОЕНИЮ, а не по счастливой случайности:
// register(dst) эмитится ПОСЛЕ unregister(src) и потому несёт строго БОЛЬШИЙ
// `source_version` (ординал эмиттера, см. fga_register_outbox_emitter.go). Отзыв
// в IAM — удаление, гейтованное `source_version <= tombstone`, поэтому
// unregister(src), доехавший дренажем ПОЗЖЕ, снять раньше применённый register
// не может: его версия меньше. Ровно это свойство ординал и вводил.
func (u *MoveLoadBalancerUseCase) WithRegistrar(r Registrar) *MoveLoadBalancerUseCase {
	u.registrar = r
	return u
}

// syncRegister — BEST-EFFORT sync-регистрация проекции назначения после durable
// commit. Ошибка ЛОГИРУЕТСЯ и ГЛОТАЕТСЯ: durable intent в `fga_register_outbox`
// + register-drainer остаются at-least-once backstop'ом, а `Operation.done` не
// гейтится на видимость (ban #9 — иначе phantom-ресурс).
func (u *MoveLoadBalancerUseCase) syncRegister(
	ctx context.Context, intent domain.FGARegisterIntent, intentVersion time.Time,
) {
	if u.registrar == nil {
		return
	}
	if err := u.registrar.Register(ctx, intent, intentVersion); err != nil {
		u.logger.Warn("LoadBalancer.Move sync owner-tuple registration incomplete; register-drainer will reconcile",
			"err", err, "load_balancer_id", intent.ResourceID)
	}
}

// NewMoveLoadBalancerUseCase конструктор. checkClient авторизует caller'а на
// destination project (`editor on project:<dst>`). nil НЕ означает «пропустить»:
// отсутствие решателя — отказ (`Unavailable`), см. shared.AuthorizeObject.
func NewMoveLoadBalancerUseCase(
	repo Repo, opsRepo operations.Repo,
	pc ProjectClient, checkClient CheckClient, logger *slog.Logger,
) *MoveLoadBalancerUseCase {
	if logger == nil {
		logger = slog.Default()
	}
	return &MoveLoadBalancerUseCase{
		repo: repo, opsRepo: opsRepo,
		projectClient: pc, checkClient: checkClient, logger: logger,
	}
}

// Execute — sync prechecks + ops insert + spawn worker.
func (u *MoveLoadBalancerUseCase) Execute(
	ctx context.Context, req *lbv1.MoveNetworkLoadBalancerRequest,
) (*operations.Operation, error) {
	id := req.GetNetworkLoadBalancerId()
	if id == "" {
		return nil, errInvalidArg("network_load_balancer_id", "required")
	}
	if err := validateLoadBalancerID(id); err != nil {
		return nil, err
	}
	dst := req.GetDestinationProjectId()
	if dst == "" {
		return nil, errInvalidArg("destination_project_id", "required")
	}

	rd, err := u.repo.Reader(ctx)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	cur, err := rd.LoadBalancers().Get(ctx, id)
	if err != nil {
		_ = rd.Close()
		return nil, mapDomainErr(err)
	}
	if string(cur.ProjectID) == dst {
		_ = rd.Close()
		return nil, status.Error(codes.InvalidArgument,
			"destination project is the same as source")
	}
	hasTG, err := rd.LoadBalancers().HasWiredTargetGroup(ctx, id)
	_ = rd.Close()
	if err != nil {
		return nil, mapDomainErr(err)
	}
	if hasTG {
		return nil, status.Error(codes.FailedPrecondition,
			"NetworkLoadBalancer has a listener wired to a target group; repoint before Move")
	}

	// Peer-check destination project.
	if u.projectClient != nil {
		if _, err := u.projectClient.Get(ctx, dst); err != nil {
			return nil, peerErrToStatus(err, "project", dst)
		}
	}

	// Destination-project authorization (CWE-862/863): the
	// per-RPC interceptor authorizes the caller on the SOURCE LB only; the caller
	// must ALSO hold `editor` on the destination project, else it could inject
	// the LB into a victim's project. This is a handler-side Check by design (an
	// RPCEntry has a single object extractor and cannot check the destination).
	if err := u.authorizeDestination(ctx, dst); err != nil {
		return nil, err
	}

	op, err := operations.NewFromContext(ctx,
		ids.PrefixOperationNLB,
		fmt.Sprintf("Move NetworkLoadBalancer %s → %s", id, dst),
		&lbv1.MoveNetworkLoadBalancerMetadata{
			NetworkLoadBalancerId: id,
			DestinationProjectId:  dst,
		},
	)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	principal := operations.PrincipalFromContext(ctx)
	if err := u.opsRepo.CreateWithPrincipal(ctx, op, principal); err != nil {
		return nil, mapDomainErr(err)
	}
	srcProject := string(cur.ProjectID)
	operations.Run(ctx, u.opsRepo, op.ID, func(workerCtx context.Context) (*anypb.Any, error) {
		return u.doMove(workerCtx, id, srcProject, dst)
	})
	return &op, nil
}

// authorizeDestination авторизует caller'а на destination project
// (`editor on project:<dst>`). Fail-closed посадка (решателя нет / вызывающего
// нельзя назвать → отказ, никогда не пропуск) живёт в `shared.AuthorizeObject` —
// одно правило на все пообъектные решения сервиса.
func (u *MoveLoadBalancerUseCase) authorizeDestination(ctx context.Context, dst string) error {
	return shared.AuthorizeObject(ctx, u.checkClient,
		domain.FGARelationEditor,
		domain.FGAObjectRef(domain.FGAObjectTypeProject, dst),
		fmt.Sprintf("caller is not authorized (editor) on destination project %s", dst))
}

// doMove — worker: Writer-TX → MoveProject + outbox MOVED → Commit → FGA rewrite.
func (u *MoveLoadBalancerUseCase) doMove(ctx context.Context, id, srcProject, dstProject string) (*anypb.Any, error) {
	w, err := u.repo.Writer(ctx)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	defer w.Abort()

	moved, movedListeners, err := w.LoadBalancers().MoveProject(ctx, id, dstProject)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	// ОДИН ПЕРЕЕЗД — ОДНА СТРОКА О ПЕРЕЕХАВШЕМ ПРЕДМЕТЕ.
	//
	// Здесь стояла ПАРА: следом за этой строкой шла вторая, рода `UPDATED`, «для
	// downstream watchers, не подписанных на MOVED». Довод неисполним by
	// construction: подписки ПО РОДУ ИЗМЕНЕНИЯ не бывает — фильтр единой формы
	// сужается по видам, проекту и идентификаторам, и только по ним. Значит
	// второй род не добавлял ни одного получателя.
	//
	// Что он добавлял: словарь журнала отдаёт `MOVED` и `UPDATED` ОДНИМ родом
	// контракта, а нагрузку обе строки собирали ОДНИМ строителем на ОДНОЙ записи,
	// — подписчик получал два события, различимых только позицией, и обязан был
	// сам догадаться, что второе не несёт новости. Плюс объём в журнале, который
	// НЕ ЧИСТИТСЯ, и который поток с начала доносит целиком.
	//
	// Ровно этот вывод уже сделан НИЖЕ, на слушателях того же переезда, и теми же
	// словами. Продукт противоречил себе в пределах одной функции (#1565).
	//
	// Оставлено `MOVED`: слово хранилища обязано называть сделанное, а форма на
	// проводе от этого не зависит — словарь отдаёт его правкой, как и прежде.
	if err := w.Outbox().Emit(ctx,
		kachorepo.OutboxResourceLoadBalancer, string(moved.ID), string(moved.ProjectID),
		kachorepo.OutboxActionMoved, kachorepo.LoadBalancerStatePayload(moved),
	); err != nil {
		return nil, mapDomainErr(err)
	}
	// ПЕРЕЕЗД ОБЪЯВЛЯЕТ ТО, ЧТО СДЕЛАЛ — по строке на каждый переехавший слушатель.
	//
	// MoveProject каскадом переписывает `project_id` у ВСЕХ слушателей этого
	// балансировщика. Прежде в журнал уходили только строки своего вида — и якорь
	// проекта у чужого вида менялся МОЛЧА. Поток при этом не замолкал и не
	// отказывал, поэтому отличить «событие не пришло» от «изменений не было»
	// подписчику было нечем, и слушатель оставался у него в СТАРОМ проекте
	// бессрочно (#1549).
	//
	// Строка несёт ПОЛНОЕ состояние: вид `nlb_listener` объявлен несущим его, и
	// одна частичная строка сделала бы ложным ВЕСЬ вид. Записи пришли `RETURNING`
	// того же UPDATE — состояние на момент события, без второго запроса.
	//
	// Род — MOVED, и парного UPDATED здесь не шлётся: подписка сужается по видам,
	// проекту и идентификаторам, но НЕ по роду изменения, а общий сервер отдаёт
	// MOVED тем же UPDATED. То же правило теперь и у самого балансировщика выше —
	// прежде пара там жила исторически и никем не читалась (#1565).
	for _, l := range movedListeners {
		if err := w.Outbox().Emit(ctx,
			kachorepo.OutboxResourceListener, string(l.ID), string(l.ProjectID),
			kachorepo.OutboxActionMoved, kachorepo.ListenerStatePayload(l),
		); err != nil {
			return nil, mapDomainErr(err)
		}
	}
	// project-rewrite as unregister(src) THEN register(dst) in the SAME tx.
	//
	// ORDER IS PART OF THE CONTRACT, not style. Both intents are about the SAME
	// object, so they land in one drainer partition (resource_id) and are applied
	// in emission order, and the emitter stamps each with a strictly newer
	// source_version than the previous intent of this tx. The surviving state must
	// therefore be emitted LAST: unregister takes down the source scope, register
	// then puts the destination scope in place. Swap them and the unregister —
	// a hard DELETE in IAM, gated `source_version <= tombstone` — carries the newer
	// version and removes the projection the register has just written, leaving the
	// moved balancer with no access at all while the Move itself reports success.
	//
	// unregister(src) stays bare (IAM uses only object + source_version on
	// unregister). register(dst) must carry full lbMirrorIntent semantics (Labels
	// from the moved record + ParentProjectID=dst) so the kaname resource_mirror
	// feeding the γ label/parent selector is re-created intact.
	if _, err := w.FGARegisterOutbox().Emit(ctx, domain.FGAEventUnregister,
		lbUnregisterIntent(id, srcProject)); err != nil {
		return nil, mapDomainErr(err)
	}
	registerIntent := lbMirrorIntent(moved)
	registerVersion, err := w.FGARegisterOutbox().Emit(ctx, domain.FGAEventRegister, registerIntent)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	if err := w.Commit(); err != nil {
		return nil, mapDomainErr(err)
	}
	// Проекция назначения ставится синхронно СРАЗУ ПОСЛЕ commit'а — тем же
	// `source_version`, что уехал в outbox, поэтому повторное применение
	// дренажем идемпотентно, а не «вторая запись». Без этого окно между сносом
	// проекции источника и её восстановлением дренажем вызывающий видит как
	// исчезновение собственного ресурса (см. WithRegistrar выше).
	u.syncRegister(ctx, registerIntent, registerVersion)

	pb, err := lbRecordToProto(moved)
	if err != nil {
		return nil, err
	}
	out, err := anypb.New(pb)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	return out, nil
}
