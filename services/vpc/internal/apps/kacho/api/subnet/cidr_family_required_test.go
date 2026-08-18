// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subnet

// cidr_family_required_test.go — подсеть без НИ ОДНОГО адресного якоря отвергается
// синхронно; подсеть одного семейства остаётся законной.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Реестр намеренных решений сервиса называет границу прямо: «Подсеть может быть
// одной семьи — но не „без CIDR вообще как норма“» — пустым может быть ОДИН из
// двух якорей (`services/vpc/docs/engineering/architecture/07-known-divergences.md`
// §2). Код же проверял каждое семейство ПО ОТДЕЛЬНОСТИ — потолок числа диапазонов
// и формат каждого переданного значения, — и оба предиката на пустом наборе
// молчат by construction. Подсеть, у которой пусты ОБА семейства, проходила: у
// неё нет ни одного адреса, из неё нельзя выделить ни один интерфейс, и
// единственное, что она делает, — занимает `UNIQUE(project,name)`.
//
// Отдельно поучительно, что проверять было НЕЧЕГО ровно по той же причине, по
// которой проверка выглядела исполненной: два предиката на пустом входе
// возвращают «годится», и по каждому в отдельности это верно. Свойство «хотя бы
// одно из двух» не выводится из двух независимых проверок и обязано быть
// названо своим.
//
// ПОЧЕМУ ДВА ПОЛОЖИТЕЛЬНЫХ КОНТРОЛЯ ОБЯЗАТЕЛЬНЫ
//
// Отрицание «оба пусты → отказ» зеленеет на реализации, отвергающей ЛЮБУЮ
// подсеть, и зеленеет ещё увереннее на реализации, требующей ОБА семейства.
// Поэтому рядом стоят обе односемейные формы: только v4 и только v6 — каждая
// доезжает до коммита.
//
// ГДЕ ЭТОГО ПУТИ НЕТ: `Update` CIDR-полей не принимает вовсе (все четыре стоят в
// immutable-switch `update.go`), а `:add-cidr-blocks` свою пару якорей уже
// требует (`add_cidr_blocks.go`) — то есть предмет был ровно у `Create`.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
)

// fieldViolationsOf — имена полей из BadRequest-деталей отказа. Отказ обязан
// НАЗЫВАТЬ поле: без имени вызывающий читает «что-то не так с запросом» и
// правит наугад.
func fieldViolationsOf(t *testing.T, err error) []string {
	t.Helper()
	var fields []string
	for _, d := range status.Convert(err).Details() {
		if br, ok := d.(*errdetails.BadRequest); ok {
			for _, fv := range br.GetFieldViolations() {
				fields = append(fields, fv.GetField())
			}
		}
	}
	return fields
}

// TestSubnetRefusedWhenNeitherFamilyHasAnAnchor — оба якоря пусты → синхронный
// InvalidArgument с именем поля; каждая односемейная форма проходит до коммита.
func TestSubnetRefusedWhenNeitherFamilyHasAnAnchor(t *testing.T) {
	for _, tc := range []struct {
		имя   string
		v4    []string
		v6    []string
		отказ bool
	}{
		{
			имя:   "ни v4, ни v6 якоря — адресов у подсети нет вовсе",
			отказ: true,
		},
		{
			// Положительный контроль №1: v4-only подсеть законна (реестр решений §2).
			имя: "только v4",
			v4:  []string{"10.20.0.0/24"},
		},
		{
			// Положительный контроль №2: v6-only подсеть законна. Без него отказ
			// зеленел бы и на реализации, ТРЕБУЮЩЕЙ оба семейства.
			имя: "только v6",
			// Внутри объявленного сетью v6-супернета `fd00::/48` (seedNetwork):
			// фикстура не снисходительнее продукта, иначе положительный контроль
			// краснел бы по чужой причине и обесценивал отрицание рядом.
			v6: []string{"fd00::/64"},
		},
	} {
		t.Run(tc.имя, func(t *testing.T) {
			uc, netID := placementFixture(t)
			op, err := uc.Execute(context.Background(), domain.Subnet{
				ProjectID: "f1", NetworkID: netID, Name: domain.RcNameVPC("anchor-case"),
				ZoneID:       testZone,
				V4CidrBlocks: tc.v4,
				V6CidrBlocks: tc.v6,
			})
			if !tc.отказ {
				require.NoError(t, err, "односемейная подсеть законна — реестр решений §2")
				require.True(t, op.Done)
				require.Nil(t, op.Error, "односемейная подсеть обязана доехать до коммита")
				var got vpcv1.Subnet
				require.NoError(t, op.Response.UnmarshalTo(&got))
				return
			}
			require.Error(t, err, "подсеть без ни одного адресного якоря принята: "+
				"выделить из неё нельзя ничего, а имя в проекте она заняла. Оба "+
				"существовавших предиката — потолок и формат — на пустом наборе "+
				"молчат by construction, поэтому свойство «хотя бы одно из двух» "+
				"обязано быть названо своим")
			assert.Equal(t, codes.InvalidArgument, status.Code(err),
				"это неверный ввод, а не состояние ресурса")
			assert.Contains(t, fieldViolationsOf(t, err), "ipv4_cidr_primary",
				"отказ обязан называть поле, иначе вызывающий правит наугад")
		})
	}
}

// TestSubnetAnchorRefusalPrecedesPeerResolution — отказ по отсутствию якоря
// выносится ДО обращения к владельцу Geography.
//
// Стоимость запроса не назначается вызывающим: запрос, который не может стать
// законным ни при каком ответе соседа, не оплачивается вызовом к соседу. Проба
// смотрит на ИСХОД, а не на порядок строк: у подсети без якоря вымышленная зона,
// и если бы резолв шёл первым, вызывающий получил бы полосу peer-validate
// (`FAILED_PRECONDITION` + `PEER_RESOURCE_MISSING`) вместо своей, синтаксической.
func TestSubnetAnchorRefusalPrecedesPeerResolution(t *testing.T) {
	uc, netID := placementFixture(t)
	_, err := uc.Execute(context.Background(), domain.Subnet{
		ProjectID: "f1", NetworkID: netID, Name: domain.RcNameVPC("no-anchor-fake-zone"),
		ZoneID: "zone-does-not-exist",
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err),
		"своя, синтаксическая полоса — не peer-validate")
	assert.Empty(t, reasonTokenOf(t, err),
		"машинный признак полосы peer-validate означал бы, что за приговором ходили к соседу")
}

// TestSubnetPerFieldRefusalsPrecedeTheAnchorRequirement — локальные проверки
// ОТДЕЛЬНЫХ ПОЛЕЙ выносятся ДО перекрёстного требования «хотя бы одно из двух».
//
// ─────────────────────────────────────────────────────────────────────────────
// ЗАЧЕМ ЭТА ПОЛОВИНА СУЩЕСТВУЕТ ОТДЕЛЬНО ОТ СОСЕДНЕЙ
//
// Соседняя проба (`…PrecedesPeerResolution`) закрепляет, что отказ по якорю идёт
// РАНЬШЕ вызова к соседу. Сама по себе она не мешает поставить требование якоря
// ПЕРВЫМ стейтментом — а именно так оно и стояло, и это перекрывало собственные
// отказы полей.
//
// Цена измерена, а не предположена. Перепись набора e2e (129 кейсов, 97 шагов
// создания подсети, из них 47 без якоря) показала: ДВЕНАДЦАТЬ шагов ждут отказа
// по СВОЕМУ предмету — имя, описание, метки, зона — и при раннем требовании якоря
// получили бы 400 по ЧУЖОЙ причине. Кейс остался бы ЗЕЛЁНЫМ, перестав проверять
// то, ради чего написан; это хуже красного, потому что незаметно.
//
// Правило, которое здесь закрепляется: локальные проверки бесплатны, поэтому
// «не платить за безнадёжный ввод» относится к ВЫЗОВУ к соседу, а не к ним.
// Итоговый порядок: обязательные скаляры → поля по одному → перекрёстное
// требование → вызов к соседу.
//
// ВХОД ВЫБРАН ТАК, ЧТОБЫ ПРИ НЕВЕРНОМ ПОРЯДКЕ ТЕКСТ БЫЛ ДРУГИМ — иначе проба
// закрепляла бы ответ, а не место: у подсети НЕТ ни одного якоря И неверное имя,
// поэтому при раннем требовании якоря отказ назвал бы `ipv4_cidr_primary`, а при
// верном порядке — `name`.
func TestSubnetPerFieldRefusalsPrecedeTheAnchorRequirement(t *testing.T) {
	uc, netID := placementFixture(t)

	// (1) Оба якоря пусты И имя неверно → отказ обязан назвать ИМЯ.
	_, err := uc.Execute(context.Background(), domain.Subnet{
		ProjectID: "f1", NetworkID: netID,
		// Заведомо неверное имя: заглавные и подчёркивание единственной формой
		// дерева не приняты (RFC 1123 DNS label, #715).
		//
		// Здесь дважды подряд ошибались ВЫБОРОМ ВХОДА, и оба раза одинаково: брали
		// то, что «выглядит неверным», вместо того, что форма действительно
		// отвергает. Первая редакция взяла заглавные при форме, их допускавшей;
		// вторая — ведущую цифру, и та стала законной вместе со сменой формы.
		// Проба про ПОРЯДОК зеленеет при любом порядке, если её вход законен, —
		// поэтому вход сверяется с действующей формой, а не с памятью о ней.
		Name:   domain.RcNameVPC("Bad_Name"),
		ZoneID: testZone,
	})
	require.Error(t, err, "неверное имя обязано быть отвергнуто")
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	fields := fieldViolationsOf(t, err)
	assert.Contains(t, fields, "name",
		"требование якоря перекрыло собственный отказ поля: кейс про имя позеленел бы, "+
			"перестав проверять имя")
	assert.NotContains(t, fields, "ipv4_cidr_primary",
		"перекрёстное требование выносится ПОСЛЕ проверок отдельных полей")

	// (2) Зеркально и обязательно: при ЗАКОННОМ имени и тех же пустых якорях отказ
	// приходит именно по якорю. Без этой половины утверждение (1) зеленело бы на
	// реализации, где требования якоря нет вовсе.
	_, err = uc.Execute(context.Background(), domain.Subnet{
		ProjectID: "f1", NetworkID: netID,
		Name:   domain.RcNameVPC("good-name"),
		ZoneID: testZone,
	})
	require.Error(t, err, "подсеть без ни одного якоря обязана быть отвергнута")
	assert.Contains(t, fieldViolationsOf(t, err), "ipv4_cidr_primary",
		"при законном имени отказ приходит по перекрёстному требованию")
}
