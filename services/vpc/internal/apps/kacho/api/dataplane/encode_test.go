// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package dataplane

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/reflect/protoreflect"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// liveRowOfKind собирает живую строку намерения названного вида.
//
// Перечисление здесь ОДНО и полное: вид, забытый в нём, роняет пробу ниже на
// «нет тела», а не проходит незамеченным.
func liveRowOfKind(t *testing.T, kind Kind, rev int64, id string) IntentRow {
	t.Helper()
	at := time.Unix(1700000000, 0).UTC()
	row := IntentRow{Revision: rev, ResourceID: id, Kind: kind}
	switch kind {
	case KindNetwork:
		row.Network = &kachorepo.NetworkRecord{Network: domain.Network{ID: id}, CreatedAt: at}
	case KindSubnet:
		row.Subnet = &kachorepo.SubnetRecord{Subnet: domain.Subnet{ID: id}, CreatedAt: at}
	case KindNetworkInterface:
		row.NetworkInterface = &kachorepo.NetworkInterfaceRecord{
			NetworkInterface: domain.NetworkInterface{ID: id}, CreatedAt: at}
	case KindSecurityGroup:
		row.SecurityGroup = &kachorepo.SecurityGroupRecord{
			SecurityGroup: domain.SecurityGroup{ID: id}, CreatedAt: at}
	case KindRouteTable:
		row.RouteTable = &kachorepo.RouteTableRecord{RouteTable: domain.RouteTable{ID: id}, CreatedAt: at}
	case KindGateway:
		row.Gateway = &kachorepo.GatewayRecord{Gateway: domain.Gateway{ID: id}, CreatedAt: at}
	case KindAddress:
		row.Address = &kachorepo.AddressRecord{Address: domain.Address{ID: id}, CreatedAt: at}
	default:
		t.Fatalf("у вида %q нет тела в пробе — перечень видов разошёлся с KnownKinds", kind)
	}
	return row
}

// objectBranch возвращает имя выбранной ветви oneof `object`.
//
// Читается по дескриптору, а не по типу Go: предмет утверждения — КОНТРАКТ, и
// сверять его надо тем же способом, каким его читает получатель.
func objectBranch(t *testing.T, in *vpcv1.DataplaneIntent) protoreflect.Name {
	t.Helper()
	m := in.ProtoReflect()
	od := m.Descriptor().Oneofs().ByName("object")
	require.NotNil(t, od, "в контракте нет ветвления по объекту")
	fd := m.WhichOneof(od)
	require.NotNil(t, fd, "ветвь объекта не выбрана: вид объекта не назван ничем")
	return fd.Name()
}

// У КАЖДОГО вида есть ветвь контракта — и у объявленного намерения, и у снятого.
//
// Вид объекта выражен только ветвью oneof, поэтому вид без ветви доехал бы до
// исполнителя ничем: сообщение без выбранной ветви не сообщает, о чём оно.
// Ветвь выбирается в двух местах (живое и снятое), и проба обходит оба.
func TestEveryKindHasAContractBranch(t *testing.T) {
	require.NotEmpty(t, KnownKinds, "перечень видов пуст — проверять нечего")

	for _, kind := range KnownKinds {
		t.Run(string(kind), func(t *testing.T) {
			live, err := intentMessage(liveRowOfKind(t, kind, 5, "id-"+string(kind)))
			require.NoError(t, err, "живое намерение вида %q не собралось", kind)
			liveBranch := objectBranch(t, live)
			assert.False(t, live.GetWithdrawn())

			gone, err := intentMessage(IntentRow{
				Revision: 6, ResourceID: "id-" + string(kind), Kind: kind, Withdrawn: true})
			require.NoError(t, err, "снятое намерение вида %q не собралось", kind)
			goneBranch := objectBranch(t, gone)
			assert.True(t, gone.GetWithdrawn())

			assert.Equal(t, liveBranch, goneBranch,
				"живое и снятое намерение одного вида выбирают РАЗНЫЕ ветви: получатель "+
					"не сможет связать снятие с тем, что ему отдавали")
		})
	}
}

// Вид вне словаря — отказ, а не сообщение без ветви.
//
// Это парный отрицательный контроль к пробе выше: без него она зеленела бы на
// реализации, которая выбирает какую-нибудь ветвь для чего угодно.
func TestUnknownKindIsRefusedInsteadOfEncodedEmpty(t *testing.T) {
	_, err := intentMessage(IntentRow{
		Revision: 1, ResourceID: "id-x", Kind: Kind("neutrino"), Withdrawn: true})
	require.ErrorIs(t, err, ErrRowShape)
}

// Снятое намерение несёт ТОЛЬКО идентификатор.
//
// Утверждение прямое: у удалённого ресурса полей нет, и любое доставленное
// значение поля было бы утверждением о состоянии, которого не существует.
func TestWithdrawnIntentCarriesIdentityAndNothingElse(t *testing.T) {
	gone, err := intentMessage(IntentRow{
		Revision: 6, ResourceID: "sub-99", Kind: KindSubnet, Withdrawn: true})
	require.NoError(t, err)

	sub := gone.GetSubnet()
	require.NotNil(t, sub)
	assert.Equal(t, "sub-99", sub.GetId())

	var populated []string
	sub.ProtoReflect().Range(func(fd protoreflect.FieldDescriptor, _ protoreflect.Value) bool {
		if fd.Name() != "id" {
			populated = append(populated, string(fd.Name()))
		}
		return true
	})
	assert.Empty(t, populated, "снятое намерение несёт поля удалённого ресурса: %v", populated)
}

// Координата изоляции сети едет в намерении — без неё исполнителю нечем
// отличить одну изоляцию от другой.
func TestNetworkIntentCarriesTheIsolationCoordinate(t *testing.T) {
	row := IntentRow{
		Revision: 3, ResourceID: "net-1", Kind: KindNetwork,
		Network: &kachorepo.NetworkRecord{
			Network:   domain.Network{ID: "net-1", VRFID: 4242},
			CreatedAt: time.Unix(1700000000, 0).UTC(),
		},
	}
	msg, err := intentMessage(row)
	require.NoError(t, err)
	assert.Equal(t, uint32(4242), msg.GetNetwork().GetVrfId())
}

// Строка, чьё тело не того вида, который назван, до контракта не доезжает.
func TestRowWhoseBodyContradictsItsKindIsRefused(t *testing.T) {
	row := liveRowOfKind(t, KindSubnet, 5, "sub-1")
	row.Kind = KindGateway // тело подсети, вид — шлюз

	_, err := intentMessage(row)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "не того вида")
}
