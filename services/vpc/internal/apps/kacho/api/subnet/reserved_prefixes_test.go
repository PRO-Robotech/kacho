// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subnet

// reserved_prefixes_test.go — подсеть поверх диапазона, который платформа держит
// за собой, отвергается синхронно.
//
// ПРЕДМЕТ. Часть адресного пространства обслуживает саму платформу: служебные
// адреса узлов, адреса служб внутри подсети, точка получения метаданных
// экземпляра. Подсеть арендатора поверх такого диапазона проходит все проверки
// контура и НЕ РАБОТАЕТ, причём симптом выглядит сетевым, а причина лежит в
// перекрытии.
//
// ДВА МЕСТА, А НЕ ОДНО. Диапазон подсети объявляется ровно двумя глаголами —
// `Create` и `:addCidrBlocks` (остальные его не расширяют: `Update` держит блоки
// неизменяемыми, `:removeCidrBlocks` только сужает). Закрыть один и оставить
// второй значило бы починить громкий подслучай: арендатор создал бы подсеть
// законным блоком и добавил бы к ней служебный вторым запросом.
//
// ЧТО СООБЩАЕТСЯ АРЕНДАТОРУ. Имя поля и ЕГО СОБСТВЕННОЕ значение — то, что он
// прислал. Перечень служебных диапазонов не раскрывается: карта служебного
// пространства не выставляется на публичной поверхности, и способ напечатать её
// через отказ стал бы первым путём, которым она туда попадёт.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/repomock"
)

// countingZoneRegistry — реестр зон, считающий обращения.
//
// Поведение НЕ переписывается: оно делегируется настоящему дублёру пакета
// (`repomock.ZoneRegistry`), а здесь добавлен только счётчик. Дублёр,
// снисходительнее настоящего, сделал бы невидимым как раз то, ради чего его
// подставляют.
type countingZoneRegistry struct {
	inner *repomock.ZoneRegistry
	calls int
}

func newCountingZoneRegistry(known ...string) *countingZoneRegistry {
	return &countingZoneRegistry{inner: repomock.NewZoneRegistry(known...)}
}

func (z *countingZoneRegistry) Get(ctx context.Context, id string) (*domain.Zone, error) {
	z.calls++
	return z.inner.Get(ctx, id)
}

// seedNetworkForReserved — сеть с супернетом, широким настолько, чтобы служебный
// диапазон в него укладывался: иначе отказ пришёл бы от проверки супернета, и
// случай проверял бы не то, о чём заявлен.
func seedNetworkForReserved(t *testing.T, kr *kachomock.Repository, projectID, networkID string) {
	t.Helper()
	ctx := context.Background()
	w, err := kr.Writer(ctx)
	require.NoError(t, err)
	_, err = w.Networks().Insert(ctx, &domain.Network{
		ID:             networkID,
		ProjectID:      projectID,
		Name:           domain.RcNameVPC("net-reserved"),
		IPv4CidrBlocks: []string{"10.0.0.0/8"},
		IPv6CidrBlocks: []string{"fd00::/16"},
	})
	require.NoError(t, err)
	require.NoError(t, w.Commit())
}

// reservedFixture — служебные диапазоны стенда для случаев ниже.
func reservedFixture() domain.ReservedPrefixes {
	return domain.NewReservedPrefixes("10.11.12.0/24", "fd00:dead::/32")
}

// RS-01: v4-блок поверх служебного диапазона отвергается синхронно.
//
// Отказ проверяется ПАРОЙ: код и текст. Текст называет поле и присланное
// значение — по нему вызывающий понимает, что править.
func TestSubnetCreate_ReservedV4Overlap_RefusedSynchronously(t *testing.T) {
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	netID := ids.NewID(ids.PrefixNetwork)
	seedNetworkForReserved(t, kr, "f-rs", netID)
	zones := newCountingZoneRegistry(testZone)

	uc := NewCreateSubnetUseCase(kr, &repomock.ProjectClient{OK: true},
		zones, repomock.NewRegionRegistry(testRegion), or).
		WithReservedPrefixes(reservedFixture())

	op, err := uc.Execute(context.Background(), domain.Subnet{
		ProjectID:    "f-rs",
		NetworkID:    netID,
		Name:         domain.RcNameVPC("s-over-reserved"),
		ZoneID:       testZone,
		V4CidrBlocks: []string{"10.11.12.0/24"},
	})
	require.Error(t, err, "подсеть поверх служебного диапазона обязана отвергаться")
	require.Nil(t, op, "отказ синхронный: операция не создаётся вовсе")

	st := status.Convert(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), createCidrFields.v4, "отказ обязан назвать поле контракта, которое клиент написал")
	assert.Contains(t, st.Message(), "10.11.12.0/24", "и присланное значение")
	assert.Contains(t, st.Message(), "reserved")

	// Отказ не оплачивается вызовом к владельцу Geography: ввод не станет законным
	// ни при каком его ответе. Порядок «локальные проверки → вызов соседа» здесь и
	// утверждается — исходом, а не комментарием.
	assert.Zero(t, zones.calls,
		"локальная проверка обязана стоять ДО вызова к geo: иначе безнадёжный ввод "+
			"оплачивается сетевым вызовом")
}

// RS-02: v6-блок поверх служебного диапазона отвергается так же.
//
// Второе семейство — не копия случая: у него своя ветка проверки, и семейство,
// оставленное без неё, выглядело бы защищённым по соседству с защищённым.
func TestSubnetCreate_ReservedV6Overlap_Refused(t *testing.T) {
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	netID := ids.NewID(ids.PrefixNetwork)
	seedNetworkForReserved(t, kr, "f-rs6", netID)

	uc := NewCreateSubnetUseCase(kr, &repomock.ProjectClient{OK: true},
		repomock.NewZoneRegistry(testZone), repomock.NewRegionRegistry(testRegion), or).
		WithReservedPrefixes(reservedFixture())

	op, err := uc.Execute(context.Background(), domain.Subnet{
		ProjectID:    "f-rs6",
		NetworkID:    netID,
		Name:         domain.RcNameVPC("s-over-reserved6"),
		ZoneID:       testZone,
		V6CidrBlocks: []string{"fd00:dead:beef::/48"},
	})
	require.Error(t, err)
	require.Nil(t, op)
	st := status.Convert(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), createCidrFields.v6)
	assert.Contains(t, st.Message(), "fd00:dead:beef::/48")
}

// RS-03 — ЯДРО: служебный диапазон ВНУТРИ блока арендатора тоже отвергается.
//
// Это та сторона вложенности, которую пропускают: проверку пишут как «содержится
// ли ввод в перечне», и арендатор забирает служебные адреса, попросив диапазон
// пошире. Здесь просят /16, внутри которого лежит служебная /24.
func TestSubnetCreate_TenantBlockSwallowsReservedRange_Refused(t *testing.T) {
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	netID := ids.NewID(ids.PrefixNetwork)
	seedNetworkForReserved(t, kr, "f-swallow", netID)

	uc := NewCreateSubnetUseCase(kr, &repomock.ProjectClient{OK: true},
		repomock.NewZoneRegistry(testZone), repomock.NewRegionRegistry(testRegion), or).
		WithReservedPrefixes(reservedFixture())

	op, err := uc.Execute(context.Background(), domain.Subnet{
		ProjectID:    "f-swallow",
		NetworkID:    netID,
		Name:         domain.RcNameVPC("s-swallow"),
		ZoneID:       testZone,
		V4CidrBlocks: []string{"10.11.0.0/16"}, // служебная 10.11.12.0/24 внутри
	})
	require.Error(t, err, "блок арендатора, поглощающий служебный диапазон, обязан отвергаться")
	require.Nil(t, op)
	assert.Equal(t, codes.InvalidArgument, status.Convert(err).Code())
	assert.Contains(t, status.Convert(err).Message(), "10.11.0.0/16")
}

// RS-04 (ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ): законный блок проходит.
//
// Без него все отказы выше зеленели бы и на реализации, отвергающей всё подряд.
// Соседний вплотную диапазон взят намеренно: касание границ пересечением не
// является, и проверка, отвергающая его, отвергала бы каждую подсеть рядом со
// служебной.
func TestSubnetCreate_AdjacentAndUnrelatedBlocksPass(t *testing.T) {
	for _, tc := range []struct {
		name  string
		block string
	}{
		{"вплотную ниже служебного диапазона", "10.11.11.0/24"},
		{"вплотную выше служебного диапазона", "10.11.13.0/24"},
		{"в другой части адресного плана сети", "10.200.0.0/16"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			kr := kachomock.NewRepository()
			or := repomock.NewOpsRepo()
			netID := ids.NewID(ids.PrefixNetwork)
			seedNetworkForReserved(t, kr, "f-ok", netID)

			uc := NewCreateSubnetUseCase(kr, &repomock.ProjectClient{OK: true},
				repomock.NewZoneRegistry(testZone), repomock.NewRegionRegistry(testRegion), or).
				WithReservedPrefixes(reservedFixture())

			op, err := uc.Execute(context.Background(), domain.Subnet{
				ProjectID:    "f-ok",
				NetworkID:    netID,
				Name:         domain.RcNameVPC("s-ok"),
				ZoneID:       testZone,
				V4CidrBlocks: []string{tc.block},
			})
			require.NoError(t, err)
			require.NotNil(t, op)
			require.True(t, op.Done)
			require.Nil(t, op.Error)
		})
	}
}

// RS-05: НЕобъявленный перечень не запрещает ничего — и это закреплено намеренно.
//
// Внутрипроцессные фикстуры посадки не несут, и «пустой перечень отвергает всё»
// превратило бы отсутствие настройки в отказ обслуживания. Вакуумность пустого
// перечня закрывает страж старта (config.ValidateReservedPrefixes: боевой режим с
// необъявленным перечнем не поднимается), а не этот путь.
func TestSubnetCreate_UndeclaredReservedSetForbidsNothing(t *testing.T) {
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	netID := ids.NewID(ids.PrefixNetwork)
	seedNetworkForReserved(t, kr, "f-none", netID)

	// Перечень не подключён вовсе — ровно как в фикстуре без посадки.
	uc := NewCreateSubnetUseCase(kr, &repomock.ProjectClient{OK: true},
		repomock.NewZoneRegistry(testZone), repomock.NewRegionRegistry(testRegion), or)

	op, err := uc.Execute(context.Background(), domain.Subnet{
		ProjectID:    "f-none",
		NetworkID:    netID,
		Name:         domain.RcNameVPC("s-none"),
		ZoneID:       testZone,
		V4CidrBlocks: []string{"10.11.12.0/24"},
	})
	require.NoError(t, err)
	require.NotNil(t, op)
	require.Nil(t, op.Error)
}

// RS-06: `:addCidrBlocks` — второй и последний глагол, которым диапазон подсети
// объявляется. Отказ синхронный, до создания операции.
func TestSubnetAddCidrBlocks_ReservedOverlap_RefusedSynchronously(t *testing.T) {
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	netID := ids.NewID(ids.PrefixNetwork)
	seedNetworkForReserved(t, kr, "f-add", netID)

	create := NewCreateSubnetUseCase(kr, &repomock.ProjectClient{OK: true},
		repomock.NewZoneRegistry(testZone), repomock.NewRegionRegistry(testRegion), or).
		WithReservedPrefixes(reservedFixture())
	cOp, err := create.Execute(context.Background(), domain.Subnet{
		ProjectID: "f-add", NetworkID: netID, Name: domain.RcNameVPC("s-add"),
		ZoneID: testZone, V4CidrBlocks: []string{"10.200.0.0/16"},
	})
	require.NoError(t, err)
	require.Nil(t, cOp.Error)
	var created vpcv1.Subnet
	require.NoError(t, cOp.Response.UnmarshalTo(&created))

	add := NewAddCidrBlocksUseCase(kr, or).WithReservedPrefixes(reservedFixture())

	// Служебный диапазон вторым запросом — тот самый обход, который остался бы,
	// если закрыть только Create.
	op, aerr := add.Execute(context.Background(), created.Id, []string{"10.11.12.0/24"}, nil)
	require.Error(t, aerr, "добавление служебного диапазона обязано отвергаться")
	require.Nil(t, op, "отказ синхронный: операция не создаётся вовсе")
	st := status.Convert(aerr)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), blocksCidrFields.V4Slot(0))
	assert.Contains(t, st.Message(), "10.11.12.0/24")

	// ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ рядом: законный блок тем же глаголом проходит.
	okOp, okErr := add.Execute(context.Background(), created.Id, []string{"10.201.0.0/16"}, nil)
	require.NoError(t, okErr)
	require.NotNil(t, okOp)
	require.Nil(t, okOp.Error)
}

// RS-07: отказ не раскрывает перечень служебных диапазонов.
//
// Арендатору сообщается про ЕГО ввод. Служебные диапазоны, которых он не
// присылал, в ответе не появляются — иначе отказ становится способом получить
// карту служебного адресного пространства.
func TestSubnetCreate_RefusalDoesNotRevealOtherReservedRanges(t *testing.T) {
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	netID := ids.NewID(ids.PrefixNetwork)
	seedNetworkForReserved(t, kr, "f-leak", netID)

	uc := NewCreateSubnetUseCase(kr, &repomock.ProjectClient{OK: true},
		repomock.NewZoneRegistry(testZone), repomock.NewRegionRegistry(testRegion), or).
		WithReservedPrefixes(domain.NewReservedPrefixes("10.11.12.0/24", "10.99.99.0/24", "fd00:dead::/32"))

	_, err := uc.Execute(context.Background(), domain.Subnet{
		ProjectID: "f-leak", NetworkID: netID, Name: domain.RcNameVPC("s-leak"),
		ZoneID: testZone, V4CidrBlocks: []string{"10.11.12.0/24"},
	})
	require.Error(t, err)
	msg := status.Convert(err).Message()
	assert.NotContains(t, msg, "10.99.99.0/24",
		"отказ не вправе называть служебные диапазоны, которых вызывающий не присылал")
	assert.NotContains(t, msg, "fd00:dead::/32",
		"и тем более диапазоны другого семейства")
}

// RS-05: отказ несёт МАШИННЫЙ признак полосы — и несёт его на ПУТИ ЗАПРОСА,
// а не только у своего производителя.
//
// Проба производителя (`serviceerr.TestReservedCIDROverlap_*`) утверждает, что
// признак собирается. О том, ДОХОДИТ ли он до вызывающего, она не говорит
// ничего: use-case мог бы собрать отказ сам, соседней копией текста, и обе
// пробы остались бы зелёными при неразличимом отказе.
//
// Что признак даёт клиенту: планировщик адресации ветвится машинно —
// `SUBNET_CIDR_RESERVED` означает «префикс служебный, бери следующий кандидат»,
// прочий `INVALID_ARGUMENT` — «ввод негоден по форме, чинить надо код».
// Разбирать прозу контракт запрещает.
func TestSubnetCreate_ReservedOverlap_CarriesMachineReadableReason(t *testing.T) {
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	netID := ids.NewID(ids.PrefixNetwork)
	seedNetworkForReserved(t, kr, "f-rs-reason", netID)

	uc := NewCreateSubnetUseCase(kr, &repomock.ProjectClient{OK: true},
		repomock.NewZoneRegistry(testZone), repomock.NewRegionRegistry(testRegion), or).
		WithReservedPrefixes(reservedFixture())

	_, err := uc.Execute(context.Background(), domain.Subnet{
		ProjectID:    "f-rs-reason",
		NetworkID:    netID,
		Name:         domain.RcNameVPC("s-reason"),
		ZoneID:       testZone,
		V4CidrBlocks: []string{"10.11.12.0/24"},
	})
	require.Error(t, err)

	var info *errdetails.ErrorInfo
	for _, d := range status.Convert(err).Details() {
		if ei, ok := d.(*errdetails.ErrorInfo); ok {
			info = ei
		}
	}
	require.NotNil(t, info,
		"отказ обязан нести google.rpc.ErrorInfo — иначе полоса различима только прозой")
	assert.Equal(t, serviceerr.ReasonSubnetCIDRReserved, info.GetReason())

	// Служебный диапазон, с которым вышло пересечение, не раскрывается ни текстом,
	// ни метаданными: отказ, печатающий его, стал бы способом получить карту
	// служебного пространства по одному пробному запросу.
	for _, v := range info.GetMetadata() {
		assert.NotEqual(t, "fd00:dead::/32", v)
	}
}

// RS-06 (ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ к RS-05): отказ ДРУГОЙ полосы этого признака НЕ
// несёт.
//
// Без него утверждение выше зеленело бы и на реализации, приклеивающей
// `SUBNET_CIDR_RESERVED` к каждому отказу подряд, — то есть признак перестал бы
// что-либо различать, оставшись на вид рабочим.
func TestSubnetCreate_UnrelatedRefusal_HasNoReservedReason(t *testing.T) {
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	netID := ids.NewID(ids.PrefixNetwork)
	seedNetworkForReserved(t, kr, "f-rs-ctrl", netID)

	uc := NewCreateSubnetUseCase(kr, &repomock.ProjectClient{OK: true},
		repomock.NewZoneRegistry(testZone), repomock.NewRegionRegistry(testRegion), or).
		WithReservedPrefixes(reservedFixture())

	// Ни одного адресного якоря — отказ той же полосы кода (InvalidArgument),
	// но другого предмета.
	_, err := uc.Execute(context.Background(), domain.Subnet{
		ProjectID: "f-rs-ctrl",
		NetworkID: netID,
		Name:      domain.RcNameVPC("s-ctrl"),
		ZoneID:    testZone,
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Convert(err).Code())

	for _, d := range status.Convert(err).Details() {
		if ei, ok := d.(*errdetails.ErrorInfo); ok {
			assert.NotEqual(t, serviceerr.ReasonSubnetCIDRReserved, ei.GetReason(),
				"признак служебного диапазона приклеен к чужому отказу — он перестал различать")
		}
	}
}
