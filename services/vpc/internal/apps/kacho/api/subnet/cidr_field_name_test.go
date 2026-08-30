// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subnet

// cidr_field_name_test.go — отказ подсети называет поле, КОТОРОЕ КЛИЕНТ НАПИСАЛ.
//
// ПРЕДМЕТ. Имя поля в отказе — часть контракта: по нему вызывающий понимает, что
// править. Отказы подсети называли `v4_cidr_blocks` — имя ДОМЕННОГО поля
// (`domain.Subnet.V4CidrBlocks`), которого в контракте подсети нет ни одним
// сообщением: `Create` принимает `ipv4_cidr_primary` (скаляр), `:addCidrBlocks` —
// `ipv4_cidr_blocks` (массив). Клиент искал в своём теле ключ, которого туда не
// клал, и правил наугад.
//
// ХУЖЕ ТОГО, НА `Create` ВРАЛО И ЧИСЛО. Скаляр `ipv4CidrPrimary` получал отказ с
// индексом — `v4_cidr_blocks[0]`, — то есть отправителя отсылали к нулевому
// элементу массива, которого он не присылал.
//
// ИМЯ БЕРЁТСЯ ИЗ ДЕСКРИПТОРА, А НЕ ВЫПИСЫВАЕТСЯ. Ожидание этой пробы выводится
// из контракта (`(&vpcv1.CreateSubnetRequest{}).ProtoReflect()`), поэтому оно не
// может разойтись с ним молча: снимут поле — проба покраснеет на своей же
// предпосылке, а не продолжит утверждать снятое имя.
//
// ФОРМА ИМЕНИ — ТА, В КОТОРОЙ ЕГО ПИШЕТ КЛИЕНТ. Единственный транспорт быстрых
// стартов — REST, и каждая страница `api/overview.mdx` обещает тела с
// camelCase-ключами; `json_name` дескриптора даёт ровно эту форму и берётся
// оттуда же, а не собирается своим преобразователем.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/reflect/protoreflect"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/repomock"
)

// jsonNameOf — json-имя поля контракта. Именно оно приезжает в теле REST-запроса
// и потому обязано стоять в отказе.
//
// Проба падает, если поля в сообщении НЕТ: это её предпосылка, а не деталь.
// Ожидание, выведенное из несуществующего поля, было бы утверждением ни о чём.
func jsonNameOf(t *testing.T, m protoreflect.ProtoMessage, protoName string) string {
	t.Helper()
	fd := m.ProtoReflect().Descriptor().Fields().ByName(protoreflect.Name(protoName))
	require.NotNilf(t, fd, "контракт %s не несёт поля %q — предпосылка пробы не выполнена",
		m.ProtoReflect().Descriptor().FullName(), protoName)
	return fd.JSONName()
}

// violationFields — имена полей из машинной части отказа. Проза и `details`
// правятся ВМЕСТЕ: клиент, ветвящийся по `FieldViolations[].field`, читает
// именно её, и разойдись они — одно из двух назвало бы не то поле.
func violationFields(t *testing.T, err error) []string {
	t.Helper()
	var out []string
	for _, d := range status.Convert(err).Details() {
		br, ok := d.(*errdetails.BadRequest)
		if !ok {
			continue
		}
		for _, v := range br.GetFieldViolations() {
			out = append(out, v.GetField())
		}
	}
	return out
}

// TestSubnetCreateRefusalNamesTheFieldTheClientWrote — `Create`.
//
// Клиент присылает `ipv4CidrPrimary` (скаляр). Отказ обязан назвать его — без
// индекса и без доменного имени.
func TestSubnetCreateRefusalNamesTheFieldTheClientWrote(t *testing.T) {
	want := jsonNameOf(t, &vpcv1.CreateSubnetRequest{}, "ipv4_cidr_primary")

	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	netID := ids.NewID(ids.PrefixNetwork)
	seedNetworkForReserved(t, kr, "f-fn", netID)

	uc := NewCreateSubnetUseCase(kr, &repomock.ProjectClient{OK: true},
		repomock.NewZoneRegistry(testZone), repomock.NewRegionRegistry(testRegion), or)

	// Host-биты не нулевые — отказ по формату, синхронный, до любой записи.
	_, err := uc.Execute(context.Background(), domain.Subnet{
		ProjectID:    "f-fn",
		NetworkID:    netID,
		Name:         domain.RcNameVPC("s-badcidr"),
		ZoneID:       testZone,
		V4CidrBlocks: []string{"10.0.0.5/24"},
	})
	require.Error(t, err)
	st := status.Convert(err)
	require.Equal(t, codes.InvalidArgument, st.Code())

	assert.Contains(t, st.Message(), want,
		"отказ обязан назвать поле, которое клиент написал в теле")
	assert.NotContains(t, st.Message(), "v4_cidr_blocks",
		"доменное имя поля не выходит на провод: в контракте подсети его нет")
	assert.NotContains(t, st.Message(), want+"[",
		"ipv4CidrPrimary — скаляр: индекса у него не бывает")
	assert.Contains(t, violationFields(t, err), want,
		"машинная часть отказа обязана называть то же поле, что и проза")
}

// TestSubnetAddCidrBlocksRefusalNamesTheFieldTheClientWrote — `:addCidrBlocks`.
//
// Здесь клиент присылает МАССИВ `ipv4CidrBlocks`, поэтому индекс слота законен и
// обязан остаться: он говорит, КОТОРЫЙ из присланных блоков негоден.
func TestSubnetAddCidrBlocksRefusalNamesTheFieldTheClientWrote(t *testing.T) {
	want := jsonNameOf(t, &vpcv1.AddSubnetCidrBlocksRequest{}, "ipv4_cidr_blocks")

	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()

	uc := NewAddCidrBlocksUseCase(kr, or)
	_, err := uc.Execute(context.Background(), ids.NewID(ids.PrefixSubnet),
		[]string{"10.0.0.5/24"}, nil)
	require.Error(t, err)
	st := status.Convert(err)
	require.Equal(t, codes.InvalidArgument, st.Code())

	assert.Contains(t, st.Message(), want+"[0]",
		"массив: отказ называет поле контракта И слот")
	assert.NotContains(t, st.Message(), "v4_cidr_blocks",
		"доменное имя поля не выходит на провод")
	assert.Contains(t, violationFields(t, err), want+"[0]")
}

// TestSubnetCidrFamilyRequiredRefusalNamesContractFields — «хотя бы одно
// семейство» на `:addCidrBlocks`.
//
// Отказ перечисляет ОБА поля, между которыми выбирает клиент, — значит оба
// обязаны быть контрактными именами.
func TestSubnetCidrFamilyRequiredRefusalNamesContractFields(t *testing.T) {
	v4 := jsonNameOf(t, &vpcv1.AddSubnetCidrBlocksRequest{}, "ipv4_cidr_blocks")
	v6 := jsonNameOf(t, &vpcv1.AddSubnetCidrBlocksRequest{}, "ipv6_cidr_blocks")

	uc := NewAddCidrBlocksUseCase(kachomock.NewRepository(), repomock.NewOpsRepo())
	_, err := uc.Execute(context.Background(), ids.NewID(ids.PrefixSubnet), nil, nil)
	require.Error(t, err)
	st := status.Convert(err)
	require.Equal(t, codes.InvalidArgument, st.Code())

	assert.Contains(t, st.Message(), v4)
	assert.Contains(t, st.Message(), v6)
	assert.NotContains(t, st.Message(), "v4_cidr_blocks")
	assert.Contains(t, violationFields(t, err), v4)
}
