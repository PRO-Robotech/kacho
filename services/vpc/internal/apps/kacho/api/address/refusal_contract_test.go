// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package address

// Что здесь утверждается: ЧТО ИМЕННО видит вызывающий, когда путь адреса
// отказывает, — дословный текст, код и машинный признак, — и что осталось
// ТОЛЬКО в журнале оператора.
//
// Три класса, все три наблюдались на этом пути:
//
//  1. ПОДСТАНОВКА ЧУЖОЙ ОШИБКИ. Отказ собирался как «своя присказка: %v» от
//     ошибки соседа или хранилища. Внутрь уезжали адрес и порт узла, текст
//     драйвера, внутренние имена — и уезжали они арендатору, через
//     `operation.error.message`. Утверждение здесь — РАВЕНСТВО сообщения
//     фиксированному тексту: `NotContains` зеленеет и на пустом сообщении, и на
//     пути, до которого проба не дошла, поэтому им это не проверяется. Рядом —
//     отдельное утверждение, что исходная ошибка ЕСТЬ в журнале: «не течёт
//     наружу» и «потеряна» различимы только так.
//
//  2. КООРДИНАТА АДМИНСКОГО РЕСУРСА В ОТКАЗЕ АРЕНДАТОРУ. Пул адресов — ресурс
//     администратора (`Internal*`, :9091). Его идентификатор, ёмкость и число
//     занятых смещений в ответе арендатору — инфра-данные на публичной
//     поверхности. Отсюда же следует, что «у пула нет блоков» и «пул исчерпан»
//     арендатору отвечают ОДНИМ текстом: различие этих состояний — свойство
//     конфигурации пула, а не его запроса, и адресовано оператору.
//
//  3. ИМЯ ИСХОДА, КОТОРОГО НЕ БЫЛО. Отказ подбора внутреннего адреса называл
//     исчерпание подсети всегда — и когда свободных адресов действительно нет,
//     и когда кончился ОГРАНИЧЕННЫЙ ПЕРЕБОР при живых свободных. Это разные
//     состояния и разные действия вызывающего (освободить адрес против
//     повторить), поэтому у них разные коды, разные тексты и разные признаки.
//     Проба на оба исхода стоит парой: заполненная целиком подсеть даёт один,
//     та же «всё занято» на подсети, которую перебор физически не покрывает, —
//     другой.
//
// Двойник хранилища здесь СТРОЖЕ kachomock, и это намеренно: `SetInternalIPv4` /
// `SetInternalIPv6` у kachomock пишут всегда, тогда как боевая схема несёт
// частичные UNIQUE `addresses_internal_subnet_ip_uniq` /
// `addresses_internal_subnet_ipv6_uniq` (0001_initial.sql). Дублёр, принимающий
// больше настоящего, сделал бы невидимым ровно тот дефект, ради которого его
// подставляют, — поэтому конфликт занятого адреса моделируется здесь.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"

	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/listnarrow/narrowtest"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/api/addresspool"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo"
	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/repomock"
)

// peerTransportText — заведомо ЧУЖАЯ строка: так выглядит транспортный отказ
// узла. Ни один её фрагмент не вправе оказаться на проводе.
const peerTransportText = "dial tcp 10.1.2.3:5432: connect: refused"

// adminPoolID — идентификатор админского пула. Именно он не вправе появиться в
// ответе арендатору, поэтому в пробах он один и узнаваемый.
const adminPoolID = "apl-adminpoolvisible1"

// ---- захват журнала ----------------------------------------------------------

// syncBuf — журнал, в который пишет worker-горутина, а читает проба. Без замка
// это гонка, а под `-race` — падение, не относящееся к предмету.
type syncBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// captureLog подменяет журнал по умолчанию на время пробы и возвращает его.
func captureLog(t *testing.T) *syncBuf {
	t.Helper()
	prev := slog.Default()
	buf := &syncBuf{}
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf
}

// ---- двойники соседей --------------------------------------------------------

// errProjectClient — сосед-владелец проектов, отвечающий транспортным отказом.
type errProjectClient struct {
	err    error
	exists bool
}

func (c errProjectClient) Exists(context.Context, string) (bool, error) {
	return c.exists, c.err
}

func okProject() ProjectClient { return errProjectClient{exists: true} }

// stubPools — резолвер пулов: по семейству отдаёт либо готовый пул, либо отказ.
type stubPools struct {
	v4, v6       *addresspool.ResolvedPool
	v4Err, v6Err error
}

func (p stubPools) ResolvePoolForAddressObjFamily(
	_ context.Context, _ *kachorepo.AddressRecord, family addresspool.AddressFamily,
) (*addresspool.ResolvedPool, error) {
	if family == addresspool.FamilyV6 {
		return p.v6, p.v6Err
	}
	return p.v4, p.v4Err
}

var _ PoolService = stubPools{}

func poolWith(v4, v6 []string) *addresspool.ResolvedPool {
	return &addresspool.ResolvedPool{
		Pool: &domain.AddressPool{
			ID:           adminPoolID,
			V4CIDRBlocks: v4,
			V6CIDRBlocks: v6,
		},
		MatchedVia: "zone_default",
	}
}

// ---- двойник хранилища, соблюдающий UNIQUE занятого адреса -------------------

// claimRepo — хранилище kachomock, у которого занятие внутреннего адреса
// подчиняется частичному UNIQUE боевой схемы: повторное занятие пары
// (подсеть, адрес) отвечает `repo.ErrAlreadyExists`, как SQLSTATE 23505.
//
// Плюс управляемые исходы внешних аллокаторов: у kachomock они безусловно
// отвечают «пул исчерпан», а пробам нужны и успех, и отказ предусловия.
type claimRepo struct {
	Repo
	claimed      map[string]struct{}
	freelistIP   string // непусто → внешний v4 выдаётся
	externalV6IP string // непусто → внешний v6 выдаётся
	externalV6Er error  // ненулево → внешний v6 отвечает этим
}

func newClaimRepo(inner Repo) *claimRepo {
	return &claimRepo{Repo: inner, claimed: map[string]struct{}{}}
}

func claimKey(subnetID, ip string) string { return subnetID + "|" + ip }

func (r *claimRepo) Writer(ctx context.Context) (Writer, error) {
	w, err := r.Repo.Writer(ctx)
	if err != nil {
		return nil, err
	}
	return &claimWriter{Writer: w, r: r}, nil
}

type claimWriter struct {
	Writer
	r *claimRepo
}

func (w *claimWriter) Addresses() AddressWriterIface {
	return &claimAddresses{AddressWriterIface: w.Writer.Addresses(), r: w.r}
}

type claimAddresses struct {
	AddressWriterIface
	r *claimRepo
}

func (a *claimAddresses) SetInternalIPv4(
	ctx context.Context, id string, spec *domain.InternalIpv4Spec,
) (*kachorepo.AddressRecord, error) {
	if spec != nil && spec.Address != "" {
		key := claimKey(spec.SubnetID, spec.Address)
		if _, taken := a.r.claimed[key]; taken {
			return nil, fmt.Errorf("%w: SQLSTATE 23505", repo.ErrAlreadyExists)
		}
		a.r.claimed[key] = struct{}{}
	}
	return a.AddressWriterIface.SetInternalIPv4(ctx, id, spec)
}

func (a *claimAddresses) SetInternalIPv6(
	ctx context.Context, id string, spec *domain.InternalIpv6Spec,
) (*kachorepo.AddressRecord, error) {
	if spec != nil && spec.Address != "" {
		key := claimKey(spec.SubnetID, spec.Address)
		if _, taken := a.r.claimed[key]; taken {
			return nil, fmt.Errorf("%w: SQLSTATE 23505", repo.ErrAlreadyExists)
		}
		a.r.claimed[key] = struct{}{}
	}
	return a.AddressWriterIface.SetInternalIPv6(ctx, id, spec)
}

func (a *claimAddresses) AllocateIPFromFreelist(ctx context.Context, poolID, addressID string) (string, error) {
	if a.r.freelistIP != "" {
		return a.r.freelistIP, nil
	}
	return a.AddressWriterIface.AllocateIPFromFreelist(ctx, poolID, addressID)
}

func (a *claimAddresses) AllocateExternalIPv6(ctx context.Context, poolID, addressID, zoneID string) (string, error) {
	if a.r.externalV6Er != nil {
		return "", a.r.externalV6Er
	}
	if a.r.externalV6IP != "" {
		return a.r.externalV6IP, nil
	}
	return a.AddressWriterIface.AllocateExternalIPv6(ctx, poolID, addressID, zoneID)
}

// ---- фикстура ----------------------------------------------------------------

type addrFixture struct {
	kr  *kachomock.Repository
	rp  *claimRepo
	sr  *repomock.SubnetRepo
	or  *repomock.OpsRepo
	log *syncBuf
}

// newAddrFixture — общая обвязка: хранилище с честным UNIQUE, журнал под
// захватом, реестр операций.
func newAddrFixture(t *testing.T) *addrFixture {
	t.Helper()
	kr := kachomock.NewRepository()
	return &addrFixture{
		kr:  kr,
		rp:  newClaimRepo(kr),
		sr:  repomock.NewSubnetRepo(),
		or:  repomock.NewOpsRepo(),
		log: captureLog(t),
	}
}

// seedSubnet кладёт подсеть в ОБА хранилища: `assertSubnetOwned` читает её через
// порт SubnetReader, а сам подбор адреса — через собственную транзакцию
// writer'а. Подсеть, положенная в одно из двух, дала бы отказ не про предмет
// пробы.
func (f *addrFixture) seedSubnet(t *testing.T, v4, v6 []string) string {
	t.Helper()
	sub := &domain.Subnet{
		ID:           ids.NewID(ids.PrefixSubnet),
		ProjectID:    "f1",
		NetworkID:    ids.NewID(ids.PrefixNetwork),
		Name:         domain.RcNameVPC("sn-" + ids.NewID(ids.PrefixSubnet)),
		V4CidrBlocks: v4,
		V6CidrBlocks: v6,
	}
	_, err := f.sr.Insert(context.Background(), sub)
	require.NoError(t, err)
	f.kr.SeedSubnet(&kachorepo.SubnetRecord{Subnet: *sub})
	return sub.ID
}

func (f *addrFixture) claim(subnetID string, ips ...string) {
	for _, ip := range ips {
		f.rp.claimed[claimKey(subnetID, ip)] = struct{}{}
	}
}

func (f *addrFixture) createUC(pools PoolService, pc ProjectClient) *CreateAddressUseCase {
	return NewCreateAddressUseCase(f.rp, f.sr, pc, f.or, pools)
}

// created — адреса, дошедшие до состояния (через тот же список, которым их видит
// вызывающий).
func (f *addrFixture) created(t *testing.T) []*kachorepo.AddressRecord {
	t.Helper()
	listUC := NewListAddressesUseCase(f.kr, narrowtest.AllowingAll())
	addrs, _, err := listUC.Execute(narrowtest.Caller(), AddressFilter{ProjectID: "f1"}, Pagination{})
	require.NoError(t, err)
	return addrs
}

// opFailure — терминальный отказ операции: код, сообщение, машинный признак.
type opFailure struct {
	code   codes.Code
	msg    string
	reason string
}

func awaitFailure(t *testing.T, or *repomock.OpsRepo, opID string) opFailure {
	t.Helper()
	final := repomock.AwaitOpDone(t, or, opID)
	require.NotNil(t, final.Error, "операция обязана завершиться отказом")
	out := opFailure{
		// `google.rpc.Status.code` объявлен int32 и несёт значение из ЗАКРЫТОГО
		// набора кодов gRPC (0..16), поэтому преобразование к codes.Code потери не
		// даёт. Директивы подавления здесь НЕТ намеренно: в этом репозитории
		// анализатор безопасности не гоняется ни одним инструментом, и подавление в
		// диалекте линтера не подавляло бы ничего — гейт
		// `TestNoInertGosecSuppressions` такую строку и ловит.
		//
		// Форма директивы в этом комментарии не воспроизводится, и это не
		// осторожность: директива, действительная и на ОТДЕЛЬНОЙ строке, в
		// комментарии неотличима от упоминания о ней, поэтому гейт обязан считать
		// находкой и её. Тот же принцип, по которому мёртвая координата не
		// цитируется в документе.
		code: codes.Code(final.Error.GetCode()),
		msg:  final.Error.GetMessage(),
	}
	for _, d := range final.Error.GetDetails() {
		info := &errdetails.ErrorInfo{}
		if err := d.UnmarshalTo(info); err == nil {
			out.reason = info.GetReason()
		}
	}
	return out
}

// ---- (6) подстановка чужой ошибки: проверка проекта --------------------------

// Отказ соседа-владельца проектов оставляет вызывающему фиксированный текст, а
// свою прозу — журналу. Код остаётся `UNAVAILABLE`: непроверяемое предусловие
// мутации не считается выполненным (fail-closed).
func TestDoCreate_ProjectPeerFailure_FixedTextRawErrorOnlyInLog(t *testing.T) {
	f := newAddrFixture(t)
	uc := f.createUC(nil, errProjectClient{err: errors.New(peerTransportText)})

	op, err := uc.Execute(context.Background(), CreateInput{
		ProjectID:    "f1",
		Name:         "addr-peer-down",
		ExternalSpec: &ExternalAddrSpec{Address: "203.0.113.10"},
	})
	require.NoError(t, err)

	got := awaitFailure(t, f.or, op.ID)
	require.Equal(t, codes.Unavailable, got.code,
		"сосед недоступен — мутация отказывает закрыто")
	require.Equal(t, "project check: upstream project service unavailable", got.msg)
	require.Contains(t, f.log.String(), "10.1.2.3",
		"исходная ошибка обязана остаться в журнале: «не течёт наружу» и «потеряна» иначе неразличимы")
}

// Положительный близнец: тот же путь с отвечающим соседом создаёт адрес. Без
// него утверждение выше зеленело бы на сломанном создании целиком.
func TestDoCreate_ProjectPeerHealthy_Succeeds(t *testing.T) {
	f := newAddrFixture(t)
	uc := f.createUC(nil, okProject())

	op, err := uc.Execute(context.Background(), CreateInput{
		ProjectID:    "f1",
		Name:         "addr-peer-up",
		ExternalSpec: &ExternalAddrSpec{Address: "203.0.113.11"},
	})
	require.NoError(t, err)
	require.Nil(t, repomock.AwaitOpDone(t, f.or, op.ID).Error)
	require.Len(t, f.created(t), 1)
}

// ---- (6+7) резолв пула: чужая ошибка и координата админского ресурса ---------

// Резолвер пулов не нашёл пула. Его собственный текст несёт идентификаторы
// адреса и сети и номер семейства — арендатору уезжает одна причина без единой
// координаты, разбор остаётся в журнале.
func TestDoCreate_PoolNotResolved_OneReasonWithoutCoordinates(t *testing.T) {
	f := newAddrFixture(t)
	resolveErr := fmt.Errorf("%w for address adr-x (network net-secret-77, family=0)", repo.ErrPoolNotResolved)
	uc := f.createUC(stubPools{v4Err: resolveErr}, okProject())

	op, err := uc.Execute(context.Background(), CreateInput{
		ProjectID:    "f1",
		Name:         "addr-no-pool",
		ExternalSpec: &ExternalAddrSpec{},
	})
	require.NoError(t, err)

	got := awaitFailure(t, f.or, op.ID)
	require.Equal(t, codes.FailedPrecondition, got.code)
	require.Equal(t, "no external IPv4 address available", got.msg)
	require.Equal(t, "EXTERNAL_ADDRESS_UNAVAILABLE", got.reason)
	require.Contains(t, f.log.String(), "net-secret-77",
		"разбор резолва обязан остаться в журнале оператора")
}

// Резолвер пулов отказал НЕ отсутствием пула, а сбоем хранилища. Это не
// предусловие запроса: код обязан сменить полосу, а текст драйвера — не уехать.
func TestDoCreate_PoolResolveStorageFailure_OpaqueInternal(t *testing.T) {
	f := newAddrFixture(t)
	uc := f.createUC(stubPools{v4Err: errors.New(peerTransportText)}, okProject())

	op, err := uc.Execute(context.Background(), CreateInput{
		ProjectID:    "f1",
		Name:         "addr-pool-broken",
		ExternalSpec: &ExternalAddrSpec{},
	})
	require.NoError(t, err)

	got := awaitFailure(t, f.or, op.ID)
	require.Equal(t, codes.Internal, got.code,
		"сбой хранилища — не «предусловие пула не выполнено»")
	require.Equal(t, "internal database error", got.msg)
	require.Contains(t, f.log.String(), "10.1.2.3")
}

// Пул резолвится, но не выдаёт запрошенного семейства. Арендатору — ТА ЖЕ
// причина, что и у «пула нет»: различие адресовано оператору, а идентификатор
// пула — админская координата.
func TestDoCreate_PoolWithoutFamilyBlocks_SameReasonNoPoolID(t *testing.T) {
	f := newAddrFixture(t)
	uc := f.createUC(stubPools{v4: poolWith(nil, []string{"2001:db8::/32"})}, okProject())

	op, err := uc.Execute(context.Background(), CreateInput{
		ProjectID:    "f1",
		Name:         "addr-pool-no-v4",
		ExternalSpec: &ExternalAddrSpec{},
	})
	require.NoError(t, err)

	got := awaitFailure(t, f.or, op.ID)
	require.Equal(t, codes.FailedPrecondition, got.code)
	require.Equal(t, "no external IPv4 address available", got.msg)
	require.Equal(t, "EXTERNAL_ADDRESS_UNAVAILABLE", got.reason)
	require.Contains(t, f.log.String(), adminPoolID,
		"идентификатор пула — оператору, в журнал")
}

// Freelist пула пуст. Та же единственная причина: «нет блоков» и «исчерпан»
// снаружи неразличимы by construction, поэтому ёмкость пула не выводима.
func TestDoCreate_PoolFreelistEmpty_SameReasonNoPoolID(t *testing.T) {
	f := newAddrFixture(t)
	uc := f.createUC(stubPools{v4: poolWith([]string{"203.0.113.0/24"}, nil)}, okProject())

	op, err := uc.Execute(context.Background(), CreateInput{
		ProjectID:    "f1",
		Name:         "addr-pool-empty",
		ExternalSpec: &ExternalAddrSpec{},
	})
	require.NoError(t, err)

	got := awaitFailure(t, f.or, op.ID)
	require.Equal(t, codes.FailedPrecondition, got.code)
	require.Equal(t, "no external IPv4 address available", got.msg)
	require.Equal(t, "EXTERNAL_ADDRESS_UNAVAILABLE", got.reason)
	require.Contains(t, f.log.String(), adminPoolID)
}

// Положительный близнец обеих проб выше: живой пул с непустым freelist выдаёт
// адрес. Без него «отказано» неотличимо от «эта ветвь не работает никогда».
func TestDoCreate_PoolHealthy_AllocatesExternalV4(t *testing.T) {
	f := newAddrFixture(t)
	f.rp.freelistIP = "203.0.113.42"
	uc := f.createUC(stubPools{v4: poolWith([]string{"203.0.113.0/24"}, nil)}, okProject())

	op, err := uc.Execute(context.Background(), CreateInput{
		ProjectID:    "f1",
		Name:         "addr-pool-ok",
		ExternalSpec: &ExternalAddrSpec{},
	})
	require.NoError(t, err)
	require.Nil(t, repomock.AwaitOpDone(t, f.or, op.ID).Error)

	addrs := f.created(t)
	require.Len(t, addrs, 1)
	require.Equal(t, "203.0.113.42", addrs[0].ExternalIpv4.Address)
}

// Хранилище отказало предусловием на ВНЕШНЕМ v6 и назвало в своём тексте пул и
// число занятых смещений. Пересказ такого текста наружу — раскрытие ёмкости
// админского ресурса; ответ тот же единственный, что и у остальных отказов
// внешнего адреса.
func TestDoCreate_ExternalV6RepoPrecondition_NoPoolCapacityOnTheWire(t *testing.T) {
	f := newAddrFixture(t)
	f.rp.externalV6Er = fmt.Errorf("%w: address pool %s: 32 consecutive ipv6 offsets already allocated",
		repo.ErrFailedPrecondition, adminPoolID)
	uc := f.createUC(stubPools{v6: poolWith(nil, []string{"2001:db8::/32"})}, okProject())

	op, err := uc.Execute(context.Background(), CreateInput{
		ProjectID:        "f1",
		Name:             "addr-ext-v6",
		ExternalIpv6Spec: &ExternalAddrSpec{},
	})
	require.NoError(t, err)

	got := awaitFailure(t, f.or, op.ID)
	require.Equal(t, codes.FailedPrecondition, got.code)
	require.Equal(t, "no external IPv6 address available", got.msg)
	require.Equal(t, "EXTERNAL_ADDRESS_UNAVAILABLE", got.reason)
	require.Contains(t, f.log.String(), "consecutive ipv6 offsets",
		"текст хранилища — оператору")
}

// Положительный близнец внешнего v6.
func TestDoCreate_ExternalV6Healthy_Allocates(t *testing.T) {
	f := newAddrFixture(t)
	f.rp.externalV6IP = "2001:db8::2a"
	uc := f.createUC(stubPools{v6: poolWith(nil, []string{"2001:db8::/32"})}, okProject())

	op, err := uc.Execute(context.Background(), CreateInput{
		ProjectID:        "f1",
		Name:             "addr-ext-v6-ok",
		ExternalIpv6Spec: &ExternalAddrSpec{},
	})
	require.NoError(t, err)
	require.Nil(t, repomock.AwaitOpDone(t, f.or, op.ID).Error)

	addrs := f.created(t)
	require.Len(t, addrs, 1)
	require.Equal(t, "2001:db8::2a", addrs[0].ExternalIpv6.Address)
}

// ---- (22) исчерпание подсети против исчерпания перебора ----------------------

// Подсеть /30 несёт ровно два пригодных адреса, и перебор покрывает их ЦЕЛИКОМ.
// Оба заняты — значит свободных нет, и это ПРОВЕРЕННОЕ утверждение: код
// исчерпания, свой признак, повтор не поможет.
func TestAllocateInternalV4_WholeSpaceExamined_ReportsNoFreeAddress(t *testing.T) {
	f := newAddrFixture(t)
	subnetID := f.seedSubnet(t, []string{"10.0.0.0/30"}, nil)
	f.claim(subnetID, "10.0.0.1", "10.0.0.2")
	uc := f.createUC(stubPools{}, okProject())

	op, err := uc.Execute(context.Background(), CreateInput{
		ProjectID:    "f1",
		Name:         "addr-v4-full",
		InternalSpec: &InternalAddrSpec{SubnetID: subnetID},
	})
	require.NoError(t, err)

	got := awaitFailure(t, f.or, op.ID)
	require.Equal(t, codes.ResourceExhausted, got.code)
	require.Equal(t, "subnet "+subnetID+" has no free IPv4 addresses", got.msg)
	require.Equal(t, "SUBNET_NO_FREE_ADDRESS", got.reason)
}

// Та же «всё занято» на подсети /24, чьё пространство ограниченный перебор
// покрыть не может. Утверждать исчерпание здесь НЕЛЬЗЯ — оно не проверено;
// верно «занять не удалось, повтори», и это другой код и другой признак.
func TestAllocateInternalV4_BudgetSpentSpaceUnexamined_ReportsRetryable(t *testing.T) {
	f := newAddrFixture(t)
	subnetID := f.seedSubnet(t, []string{"10.0.0.0/24"}, nil)
	for i := 1; i <= 254; i++ {
		f.claim(subnetID, fmt.Sprintf("10.0.0.%d", i))
	}
	uc := f.createUC(stubPools{}, okProject())

	op, err := uc.Execute(context.Background(), CreateInput{
		ProjectID:    "f1",
		Name:         "addr-v4-contended",
		InternalSpec: &InternalAddrSpec{SubnetID: subnetID},
	})
	require.NoError(t, err)

	got := awaitFailure(t, f.or, op.ID)
	require.Equal(t, codes.Aborted, got.code,
		"кончился перебор, а не адреса: вызывающему полагается повтор, а не «освободите адрес»")
	require.Equal(t, "subnet "+subnetID+": could not claim a free IPv4 address, retry", got.msg)
	require.Equal(t, "ALLOCATION_CONTENDED", got.reason)
}

// Положительный близнец обеих проб: свободная /24 выдаёт адрес.
func TestAllocateInternalV4_FreeSubnet_Allocates(t *testing.T) {
	f := newAddrFixture(t)
	subnetID := f.seedSubnet(t, []string{"10.0.0.0/24"}, nil)
	uc := f.createUC(stubPools{}, okProject())

	op, err := uc.Execute(context.Background(), CreateInput{
		ProjectID:    "f1",
		Name:         "addr-v4-free",
		InternalSpec: &InternalAddrSpec{SubnetID: subnetID},
	})
	require.NoError(t, err)
	require.Nil(t, repomock.AwaitOpDone(t, f.or, op.ID).Error)

	addrs := f.created(t)
	require.Len(t, addrs, 1)
	require.NotEmpty(t, addrs[0].InternalIpv4.Address)
}

// v6-зеркало «проверенного исчерпания»: /128 несёт ровно один адрес, подбор его
// и выдаёт, он занят — свободных нет.
func TestAllocateInternalV6_WholeSpaceExamined_ReportsNoFreeAddress(t *testing.T) {
	f := newAddrFixture(t)
	subnetID := f.seedSubnet(t, nil, []string{"2001:db8::1/128"})
	f.claim(subnetID, "2001:db8::1")
	uc := f.createUC(nil, okProject())

	op, err := uc.Execute(context.Background(), CreateInput{
		ProjectID:        "f1",
		Name:             "addr-v6-full",
		InternalIpv6Spec: &InternalAddrSpec{SubnetID: subnetID},
	})
	require.NoError(t, err)

	got := awaitFailure(t, f.or, op.ID)
	require.Equal(t, codes.ResourceExhausted, got.code)
	require.Equal(t, "subnet "+subnetID+" has no free IPv6 addresses", got.msg)
	require.Equal(t, "SUBNET_NO_FREE_ADDRESS", got.reason)
}

// v6-зеркало «кончился перебор»: /120 — 256 адресов при бюджете в 16 попыток,
// покрыть пространство физически нечем. Все 256 заняты, и всё равно верный
// ответ — «повтори», потому что исчерпание не проверено.
func TestAllocateInternalV6_BudgetSpentSpaceUnexamined_ReportsRetryable(t *testing.T) {
	f := newAddrFixture(t)
	subnetID := f.seedSubnet(t, nil, []string{"2001:db8::/120"})
	f.claim(subnetID, allV6HostsOfSlash120(t, "2001:db8::/120")...)
	uc := f.createUC(nil, okProject())

	op, err := uc.Execute(context.Background(), CreateInput{
		ProjectID:        "f1",
		Name:             "addr-v6-contended",
		InternalIpv6Spec: &InternalAddrSpec{SubnetID: subnetID},
	})
	require.NoError(t, err)

	got := awaitFailure(t, f.or, op.ID)
	require.Equal(t, codes.Aborted, got.code)
	require.Equal(t, "subnet "+subnetID+": could not claim a free IPv6 address, retry", got.msg)
	require.Equal(t, "ALLOCATION_CONTENDED", got.reason)
}

// Положительный близнец v6.
func TestAllocateInternalV6_FreeSubnet_Allocates(t *testing.T) {
	f := newAddrFixture(t)
	subnetID := f.seedSubnet(t, nil, []string{"2001:db8::/64"})
	uc := f.createUC(nil, okProject())

	op, err := uc.Execute(context.Background(), CreateInput{
		ProjectID:        "f1",
		Name:             "addr-v6-free",
		InternalIpv6Spec: &InternalAddrSpec{SubnetID: subnetID},
	})
	require.NoError(t, err)
	require.Nil(t, repomock.AwaitOpDone(t, f.or, op.ID).Error)

	addrs := f.created(t)
	require.Len(t, addrs, 1)
	require.NotEmpty(t, addrs[0].InternalIpv6.Address)
}

// allV6HostsOfSlash120 — все 256 адресов /120 в ТОЙ ЖЕ канонической записи,
// которую отдаёт подбор (`netip.Addr.String()`). Строки, собранные вручную,
// разошлись бы с ней на сжатии нулей, и проба заняла бы не те адреса.
func allV6HostsOfSlash120(t *testing.T, prefix string) []string {
	t.Helper()
	p, err := netip.ParsePrefix(prefix)
	require.NoError(t, err)
	require.Equal(t, 120, p.Bits())
	base := p.Masked().Addr().As16()
	out := make([]string, 0, 256)
	for i := 0; i < 256; i++ {
		cand := base
		cand[15] = byte(i)
		out = append(out, netip.AddrFrom16(cand).String())
	}
	return out
}
