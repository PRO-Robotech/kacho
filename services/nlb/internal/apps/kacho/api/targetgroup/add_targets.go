// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package targetgroup

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	computeclient "github.com/PRO-Robotech/kacho/services/nlb/internal/clients/compute"
	vpcclient "github.com/PRO-Robotech/kacho/services/nlb/internal/clients/vpc"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// targetPeerFanout — верхняя граница ОДНОВРЕМЕННЫХ peer-запросов при валидации
// одной пачки targets.
//
// Бюджет времени принадлежит ЗАПРОСУ, а не элементу: контракт принимает до
// domain.MaxTargetsPerGroup целей за вызов, каждая instance-цель стоит двух
// обращений к соседям (compute + geo) со своим 5s-дедлайном, а потолок
// исполнения операции — 4 минуты. Строго последовательный обход укладывается в
// него только пока соседи отвечают быстрее 1.2s; медленнее — операция падает
// терминально по дедлайну и не добавляет НИ ОДНОЙ цели (writer-TX открывается
// после всех проверок). Ограничение сверху обязательно в другую сторону: без
// него пачка на 100 целей превратилась бы в 200 одновременных соединений к
// соседям (CWE-770 — усиление нагрузки на чужой сервис). 8 — тот же предел, что
// у bounded-fan-out'а per-object authz-Check в registry (listauthz.go).
const targetPeerFanout = 8

// AddTargetsUseCase — добавляет targets в TG.
//
// Sync (handler-thread):
//   - target_group_id required, len(targets) >= 1;
//   - per-target domain.Target.Validate (4-way oneof, weight bounds,
//     external_ip bogon-check).
//
// Async worker:
//   - TG.Get + status guard (DELETING → FailedPrecondition);
//   - per-target peer-validate — ограниченным фан-аутом (targetPeerFanout) и с
//     дедупликацией повторяющегося вопроса в пределах ОДНОЙ операции
//     (см. peerAnswers): пачка целей в одной зоне спрашивает geo про эту зону
//     один раз, а не по разу на цель;
//   - instance: compute.InstanceService.Get + region match (006);
//   - nic: vpc.NetworkInterfaceService.Get + region match via subnet;
//   - ip_ref: vpc.SubnetService.Get + region match + IP-in-CIDR (008);
//   - external_ip: bogon-check уже в Validate, peer-validate нет;
//   - Writer-TX → AddTargets (reactivate same-identity DRAINING row via CAS,
//     иначе INSERT ON CONFLICT DO NOTHING per partial UNIQUE) + outbox UPDATED
//     только если >0 строк вставлено/реактивировано (idempotent no-op) → Commit.
type AddTargetsUseCase struct {
	repo           Repo
	opsRepo        OpsRepo
	instanceClient InstanceClient
	nicClient      NetworkInterfaceClient
	subnetClient   SubnetClient
	zoneRegion     ZoneRegionClient
	logger         *slog.Logger
}

// NewAddTargetsUseCase конструктор. `zoneRegion` — авторитетный zone→region
// резолвер (geo, владелец Geography); обязателен для instance-таргетов, у которых
// region есть только у зоны. nil → region-coherence неверифицируема → мутация
// fail-closed UNAVAILABLE (регион НИКОГДА не выводится из имени зоны).
func NewAddTargetsUseCase(
	repo Repo, opsRepo OpsRepo,
	inst InstanceClient, nic NetworkInterfaceClient, sub SubnetClient,
	zoneRegion ZoneRegionClient,
	logger *slog.Logger,
) *AddTargetsUseCase {
	if logger == nil {
		logger = slog.Default()
	}
	return &AddTargetsUseCase{
		repo: repo, opsRepo: opsRepo,
		instanceClient: inst, nicClient: nic, subnetClient: sub,
		zoneRegion: zoneRegion,
		logger:     logger,
	}
}

// Execute — sync validate + ops insert + spawn worker.
func (u *AddTargetsUseCase) Execute(
	ctx context.Context, req *lbv1.AddTargetsRequest,
) (*operations.Operation, error) {
	tgID := req.GetTargetGroupId()
	if tgID == "" {
		return nil, errInvalidArg("target_group_id", "required")
	}
	if err := validateTargetGroupID(tgID); err != nil {
		return nil, err
	}
	if len(req.GetTargets()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "at least one target is required")
	}
	if len(req.GetTargets()) > domain.MaxTargetsPerGroup {
		return nil, status.Errorf(codes.InvalidArgument,
			"too many targets in a single AddTargets call (max %d)", domain.MaxTargetsPerGroup)
	}

	targets := targetsFromPb(req.GetTargets())
	for i := range targets {
		if err := targets[i].Validate(); err != nil {
			return nil, mapDomainErr(err)
		}
	}

	op, err := operations.NewFromContext(ctx,
		ids.PrefixOperationNLB,
		fmt.Sprintf("AddTargets to TargetGroup %s (n=%d)", tgID, len(targets)),
		&lbv1.AddTargetsMetadata{TargetGroupId: tgID},
	)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	principal := operations.PrincipalFromContext(ctx)
	if err := u.opsRepo.CreateWithPrincipal(ctx, op, principal); err != nil {
		return nil, mapDomainErr(err)
	}
	operations.Run(ctx, u.opsRepo, op.ID, func(workerCtx context.Context) (*anypb.Any, error) {
		return u.doAdd(workerCtx, tgID, targets)
	})
	return &op, nil
}

// doAdd — async worker. См. описание use-case'а.
func (u *AddTargetsUseCase) doAdd(ctx context.Context, tgID string, targets []domain.Target) (*anypb.Any, error) {
	// 1. TG.Get + status guard.
	rd, err := u.repo.Reader(ctx)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	tg, err := rd.TargetGroups().Get(ctx, tgID)
	_ = rd.Close()
	if err != nil {
		return nil, mapDomainErr(err)
	}
	if tg.Status == domain.TargetGroupStatusDeleting {
		return nil, status.Error(codes.FailedPrecondition, "target group is being deleted")
	}

	// 2. Per-target peer-validate (bounded fan-out + дедуп вопросов).
	if err := u.validateTargetsPeer(ctx, tg.RegionID, targets); err != nil {
		return nil, err
	}

	// 3. Writer-TX → AddTargets + (conditional) outbox UPDATED → Commit.
	w, err := u.repo.Writer(ctx)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	defer w.Abort()

	inserted, err := w.TargetGroups().AddTargets(ctx, tgID, targets)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	if inserted > 0 {
		if err := w.Outbox().Emit(ctx,
			kachorepo.OutboxResourceTargetGroup, tgID, string(tg.ProjectID),
			kachorepo.OutboxActionUpdated, map[string]any{
				"id":             tgID,
				"project_id":     string(tg.ProjectID),
				"region_id":      string(tg.RegionID),
				"trigger":        "add_targets",
				"inserted_count": inserted,
			},
		); err != nil {
			return nil, mapDomainErr(err)
		}
	}
	if err := w.Commit(); err != nil {
		return nil, mapDomainErr(err)
	}

	// Re-read TG (с inline targets) для response.
	rd2, err := u.repo.Reader(ctx)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	updated, err := rd2.TargetGroups().Get(ctx, tgID)
	_ = rd2.Close()
	if err != nil {
		return nil, mapDomainErr(err)
	}
	return marshalTargetGroup(updated)
}

// validateTargetsPeer — peer-validate ВСЕЙ пачки: ограниченный фан-аут
// (targetPeerFanout) поверх общей на операцию памяти ответов (peerAnswers).
//
// Исход собирается ПО ИНДЕКСУ, а не берётся из errgroup: текст ошибки называет
// конкретную цель (`target[N]…`), поэтому при двух негодных целях ответ обязан
// быть детерминированным — ошибка цели с наименьшим индексом, ровно как у
// прежнего последовательного обхода. Поэтому же фан-аут идёт БЕЗ отмены по
// первой ошибке: отменённые соседние вызовы вернули бы «context canceled» вместо
// своего настоящего исхода, и выбор наименьшего индекса стал бы ложью.
// Отказ от ранней остановки не удорожает вызов для соседей: число вопросов
// сверху ограничено потолком пачки (domain.MaxTargetsPerGroup) и дедупликацией,
// а сами вопросы — чтения.
//
// Паника пере-возбуждается в ЭТОЙ горутине: recover воркера операций стоит
// вокруг вызова operation-fn, и без пере-возбуждения фан-аут вынес бы работу
// из-под него — паника в дочерней горутине роняла бы весь процесс.
func (u *AddTargetsUseCase) validateTargetsPeer(
	ctx context.Context, tgRegion domain.RegionID, targets []domain.Target,
) error {
	answers := &peerAnswers{}
	errs := make([]error, len(targets))
	panicked := make([]any, len(targets))

	g := new(errgroup.Group)
	g.SetLimit(targetPeerFanout)
	for i := range targets {
		g.Go(func() error {
			defer func() {
				if p := recover(); p != nil {
					panicked[i] = p
				}
			}()
			errs[i] = u.validateTargetPeer(ctx, answers, tgRegion, i, &targets[i])
			return nil // исход — в errs[i]/panicked[i], см. godoc выше
		})
	}
	_ = g.Wait() // ошибок горутины не возвращают: Wait здесь — барьер, не исход

	for _, p := range panicked {
		if p != nil {
			panic(p)
		}
	}
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// peerAnswers — ответы соседей, полученные в пределах ОДНОЙ операции AddTargets.
//
// Один и тот же вопрос (та же зона, та же подсеть, тот же инстанс/NIC) задаётся
// за операцию ровно один раз: пачка из 100 целей одной зоны стоит одного вопроса
// к geo, а не ста одинаковых. Второй спрашивающий ЖДЁТ первого (sync.Once), а не
// идёт к соседу сам.
//
// Область жизни — намеренно одна операция, а не долгоживущий кэш в адаптере:
// зона/подсеть/инстанс проверяются как ПРЕДУСЛОВИЕ мутации на пути запроса, и
// кэш с TTL означал бы решение по устаревшему ответу владельца (adapter'ы
// остаются stateless pass-through, см. их godoc). В пределах одной операции
// повторный вопрос ничего нового дать не может — за это окно чужая строка
// считается неизменной, ровно как и при прежнем последовательном обходе.
//
// Ошибка кэшируется вместе с ответом: недоступность соседа fail-closed валит всю
// операцию, повторять тот же вопрос за неё нечем.
type peerAnswers struct {
	instances onceMemo[*computeclient.Instance]
	nics      onceMemo[*vpcclient.NetworkInterface]
	subnets   onceMemo[*vpcclient.Subnet]
	zones     onceMemo[string]
}

// onceMemo — «один вопрос на ключ» под конкурентными спрашивающими.
// Обобщён по типу ответа: семантика дедупликации одна на все четыре вопроса
// (инстанс, NIC, подсеть, регион зоны), различаются только типы ответов.
type onceMemo[T any] struct {
	mu    sync.Mutex
	cells map[string]*memoCell[T]
}

type memoCell[T any] struct {
	once sync.Once
	val  T
	err  error
}

// do возвращает ответ на вопрос key, задав его ask ровно один раз.
//
// Возвращаемое значение РАЗДЕЛЯЕТСЯ всеми спрашивающими и потому read-only:
// валидация целей чужую строку не мутирует. Все спрашивающие идут под одним и
// тем же worker-ctx, поэтому неважно, чья горутина задаст вопрос первой.
//
// Ожидание совместимо с ограниченным фан-аутом: ответ производит одна из УЖЕ
// занявших слот горутин, поэтому ожидающие никогда не зависят от появления
// свободного слота (взаимной блокировки нет by construction).
func (m *onceMemo[T]) do(key string, ask func() (T, error)) (T, error) {
	m.mu.Lock()
	if m.cells == nil {
		m.cells = make(map[string]*memoCell[T])
	}
	c := m.cells[key]
	if c == nil {
		c = &memoCell[T]{}
		m.cells[key] = c
	}
	m.mu.Unlock()

	c.once.Do(func() { c.val, c.err = ask() })
	return c.val, c.err
}

// validateTargetPeer — per-target peer-validate. idx — индекс target'а в request
// массиве (для с фиксированным текстом error text `"target[N]..."`). Errors → gRPC InvalidArgument.
func (u *AddTargetsUseCase) validateTargetPeer(
	ctx context.Context, an *peerAnswers, tgRegion domain.RegionID, idx int, t *domain.Target,
) error {
	switch {
	case isInstanceTarget(t):
		return u.validateInstanceTarget(ctx, an, tgRegion, idx, t)
	case isNicTarget(t):
		return u.validateNicTarget(ctx, an, tgRegion, idx, t)
	case t.IPRef != nil:
		return u.validateIPRefTarget(ctx, an, tgRegion, idx, t)
	case t.ExternalIP != nil:
		// bogon-check уже в domain.Target.Validate; peer-validate нет.
		return nil
	}
	return nil
}

func (u *AddTargetsUseCase) validateInstanceTarget(
	ctx context.Context, an *peerAnswers, tgRegion domain.RegionID, idx int, t *domain.Target,
) error {
	instID, _ := t.InstanceID.Maybe()
	if u.instanceClient == nil {
		return status.Error(codes.Unavailable, "compute instance client not configured")
	}
	inst, err := an.instances.do(string(instID), func() (*computeclient.Instance, error) {
		return u.instanceClient.Get(ctx, string(instID))
	})
	if err != nil {
		return mapPeerTargetErr(idx, "instance_id", string(instID), err)
	}
	// Instance — всегда ZONAL; его регион знает ТОЛЬКО владелец Geography.
	r, err := u.regionOfZone(ctx, an, inst.ZoneID)
	if err != nil {
		return regionUnverifiableErr(idx, "instance_id", string(instID))
	}
	if r != string(tgRegion) {
		return status.Errorf(codes.InvalidArgument,
			"target[%d].instance_id '%s' region '%s' does not match target_group region '%s'",
			idx, instID, r, tgRegion)
	}
	return nil
}

// regionOfZone — авторитетный zone→region резолв через geo (владелец Geography).
// Регион НИКОГДА не выводится из имени зоны: имена региона и зоны — произвольные
// строки, между ними нет выводимой связи (data-integrity.md §Placement-coherence
// «валидировать peer-вызовом geo.v1.ZoneService.Get, не локально»). Пустая зона /
// нет резолвера / geo недоступен → ошибка (fail-closed на мутации).
//
// Вопрос про одну зону задаётся один раз на операцию (an.zones) — цели пачки,
// как правило, лежат в одной зоне.
func (u *AddTargetsUseCase) regionOfZone(ctx context.Context, an *peerAnswers, zoneID string) (string, error) {
	if u.zoneRegion == nil {
		return "", fmt.Errorf("%w: zone→region resolver not configured", domain.ErrUnavailable)
	}
	if zoneID == "" {
		return "", fmt.Errorf("%w: peer resource carries no zone", domain.ErrUnavailable)
	}
	r, err := an.zones.do(zoneID, func() (string, error) {
		return u.zoneRegion.RegionOfZone(ctx, zoneID)
	})
	if err != nil {
		return "", err
	}
	if r == "" {
		return "", fmt.Errorf("%w: zone→region resolved empty", domain.ErrUnavailable)
	}
	return r, nil
}

// subnetRegion — авторитетный регион подсети: `vpc.Subnet.region_id` (REGIONAL
// несёт его напрямую; ZONAL — zone→region резолв через geo внутри adapter'а).
// Пустое зеркало = регион неизвестен → coherence неверифицируема, fail-closed.
func subnetRegion(sn *vpcclient.Subnet) (string, bool) {
	if sn == nil || sn.RegionID == "" {
		return "", false
	}
	return sn.RegionID, true
}

// regionUnverifiableErr — fail-closed на мутации, когда регион peer-ресурса
// неустановим (geo недоступен / зеркало не заполнено). Никакой инфра-детали
// наружу — только факт недоступности резолва.
func regionUnverifiableErr(idx int, field, id string) error {
	return status.Errorf(codes.Unavailable,
		"target[%d].%s '%s': region lookup unavailable", idx, field, id)
}

func (u *AddTargetsUseCase) validateNicTarget(
	ctx context.Context, an *peerAnswers, tgRegion domain.RegionID, idx int, t *domain.Target,
) error {
	nicID, _ := t.NicID.Maybe()
	if u.nicClient == nil || u.subnetClient == nil {
		return status.Error(codes.Unavailable, "vpc nic/subnet client not configured")
	}
	nic, err := an.nics.do(string(nicID), func() (*vpcclient.NetworkInterface, error) {
		return u.nicClient.Get(ctx, string(nicID))
	})
	if err != nil {
		return mapPeerTargetErr(idx, "nic_id", string(nicID), err)
	}
	// NIC region resolved via parent subnet zone. Подсеть у интерфейсов пачки, как
	// правило, общая — спрашиваем vpc о ней один раз на операцию.
	sub, err := an.subnets.do(nic.SubnetID, func() (*vpcclient.Subnet, error) {
		return u.subnetClient.Get(ctx, nic.SubnetID)
	})
	if err != nil {
		return mapPeerTargetErr(idx, "nic_id", string(nicID), err)
	}
	r, ok := subnetRegion(sub)
	if !ok {
		return regionUnverifiableErr(idx, "nic_id", string(nicID))
	}
	if r != string(tgRegion) {
		return status.Errorf(codes.InvalidArgument,
			"target[%d].nic_id '%s' region '%s' does not match target_group region '%s'",
			idx, nicID, r, tgRegion)
	}
	return nil
}

func (u *AddTargetsUseCase) validateIPRefTarget(
	ctx context.Context, an *peerAnswers, tgRegion domain.RegionID, idx int, t *domain.Target,
) error {
	if u.subnetClient == nil {
		return status.Error(codes.Unavailable, "vpc subnet client not configured")
	}
	subID := string(t.IPRef.SubnetID)
	// Адреса пачки, как правило, из одной подсети — спрашиваем vpc о ней один раз.
	sub, err := an.subnets.do(subID, func() (*vpcclient.Subnet, error) {
		return u.subnetClient.Get(ctx, subID)
	})
	if err != nil {
		return mapPeerTargetErr(idx, "ip_ref.subnet_id", subID, err)
	}
	r, ok := subnetRegion(sub)
	if !ok {
		return regionUnverifiableErr(idx, "ip_ref.subnet_id", subID)
	}
	if r != string(tgRegion) {
		return status.Errorf(codes.InvalidArgument,
			"target[%d].ip_ref.subnet_id '%s' region '%s' does not match target_group region '%s'",
			idx, subID, r, tgRegion)
	}
	// IP ∈ CIDR check.
	addr, err := netip.ParseAddr(string(t.IPRef.Address))
	if err != nil {
		return status.Errorf(codes.InvalidArgument,
			"target[%d].ip_ref.address %s is not a valid IP", idx, t.IPRef.Address)
	}
	cidrs := sub.V4CIDRBlocks
	if addr.Is6() {
		cidrs = sub.V6CIDRBlocks
	}
	if !addressInAnyCIDR(addr, cidrs) {
		return status.Errorf(codes.InvalidArgument,
			"target[%d].ip_ref.address %s is not in subnet %s CIDR %s",
			idx, t.IPRef.Address, subID, strings.Join(cidrs, ","))
	}
	return nil
}

// mapPeerTargetErr — peer-error → InvalidArgument с с фиксированным текстом per-target context.
// NotFound от peer → "target[N].<field> '<id>' not found".
// Unavailable → Unavailable. Прочие → InvalidArgument.
func mapPeerTargetErr(idx int, field, id string, err error) error {
	if err == nil {
		return nil
	}
	// Sentinel-strip: peer-clients оборачивают `domain.ErrInvalidArg: <Resource> <id> not found`.
	switch {
	case errIsKind(err, domain.ErrNotFound):
		return status.Errorf(codes.InvalidArgument,
			"target[%d].%s '%s' not found", idx, field, id)
	case errIsKind(err, domain.ErrInvalidArg):
		// compute/vpc peer-clients мапят NotFound → InvalidArgument внутри
		// сентинела — это нормальный путь (см. instance_client.mapInstanceErr).
		return status.Errorf(codes.InvalidArgument,
			"target[%d].%s '%s' not found", idx, field, id)
	case errIsKind(err, domain.ErrFailedPrecondition):
		return status.Errorf(codes.FailedPrecondition,
			"target[%d].%s '%s': %v", idx, field, id, err)
	case errIsKind(err, domain.ErrUnavailable):
		return status.Errorf(codes.Unavailable,
			"target[%d].%s '%s': peer lookup unavailable", idx, field, id)
	}
	return status.Errorf(codes.Internal,
		"target[%d].%s '%s': peer lookup failed", idx, field, id)
}

// errIsKind — errors.Is с nil-guard для удобства switch'а.
func errIsKind(err error, sentinel error) bool {
	if err == nil || sentinel == nil {
		return false
	}
	return errors.Is(err, sentinel)
}

// addressInAnyCIDR — true если addr ∈ хотя бы одного prefix'а из cidrs.
// Невалидный CIDR в slice пропускается без ошибки (тест выше).
func addressInAnyCIDR(addr netip.Addr, cidrs []string) bool {
	for _, c := range cidrs {
		p, err := netip.ParsePrefix(c)
		if err != nil {
			continue
		}
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// isInstanceTarget / isNicTarget — small predicates для switch (option Maybe
// возвращает (val, ok); хотим только ok).
func isInstanceTarget(t *domain.Target) bool {
	_, ok := t.InstanceID.Maybe()
	return ok
}

func isNicTarget(t *domain.Target) bool {
	_, ok := t.NicID.Maybe()
	return ok
}
