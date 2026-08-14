// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package address

// Стражи самих конструкторов причин полосы выделения адреса. Пробы в
// `refusal_contract_test.go` утверждают, что видит вызывающий на конкретном
// пути; здесь утверждаются свойства НАБОРА причин, которые ни один отдельный
// путь не проверяет и которые ломаются молча:
//
//   - источник отказа один на сервис (иначе клиент получает от одного сервиса
//     два разных `ErrorInfo.domain` и ключуется не на то);
//   - код полосы приходит вместе со своим признаком (разъехавшись, они дают
//     худший исход: ответ машинно заявляет одну полосу, а кодом — другую);
//   - деталь НЕ говорит больше сообщения (иначе вырезанное из текста
//     возвращается через метаданные, и в диффе это не видно);
//   - в тексте нет координат админского ресурса, и текст при этом НЕ пуст —
//     без второго условия отрицание зеленело бы на пустой строке.

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
)

// probeSubnetID — подсеть в пробах. Она принадлежит вызывающему, поэтому в
// тексте отказа стоять ВПРАВЕ; координата, которая стоять не вправе, — админский
// пул (`adminPoolID`).
const probeSubnetID = "sub7q71g63fwmzmnx64r"

// refusalProducers — все производители причин этой оси. Перечень задан один раз:
// проба, перечисляющая их у себя, промолчала бы ровно о том, который забыли
// дописать.
func refusalProducers() []struct {
	name   string
	err    error
	code   codes.Code
	reason string
} {
	return []struct {
		name   string
		err    error
		code   codes.Code
		reason string
	}{
		{"external/v4", noExternalAddressAvailable(domain.IpVersionIPv4),
			codes.FailedPrecondition, reasonExternalUnavailable},
		{"external/v6", noExternalAddressAvailable(domain.IpVersionIPv6),
			codes.FailedPrecondition, reasonExternalUnavailable},
		{"subnet/no-free/v4", noFreeSubnetAddress(probeSubnetID, domain.IpVersionIPv4),
			codes.ResourceExhausted, reasonNoFreeSubnetAddress},
		{"subnet/no-free/v6", noFreeSubnetAddress(probeSubnetID, domain.IpVersionIPv6),
			codes.ResourceExhausted, reasonNoFreeSubnetAddress},
		{"subnet/contended/v4", allocationContended(probeSubnetID, domain.IpVersionIPv4),
			codes.Aborted, reasonAllocationContended},
		{"subnet/contended/v6", allocationContended(probeSubnetID, domain.IpVersionIPv6),
			codes.Aborted, reasonAllocationContended},
	}
}

// errorInfoOf — `ErrorInfo` готовой ошибки; nil, если детали нет.
func errorInfoOf(t *testing.T, err error) *errdetails.ErrorInfo {
	t.Helper()
	st, ok := status.FromError(err)
	require.True(t, ok, "ожидался gRPC-status, получено %v", err)
	for _, d := range st.Details() {
		if info, isInfo := d.(*errdetails.ErrorInfo); isInfo {
			return info
		}
	}
	return nil
}

// Ось ёмкости — не полоса резолва идентификатора, и словарь у неё свой. Общее у
// двух осей ровно одно: ИСТОЧНИК отказа. Поэтому домен здесь не сверяется с
// литералом (сверка литерала с собой доказывает только то, что он себе равен), а
// БЕРЁТСЯ у существующего производителя полосы резолва.
func TestCapacityReasonSharesTheResolveLaneDomain(t *testing.T) {
	laneInfo := errorInfoOf(t, serviceerr.UnknownZone("zone-x"))
	require.NotNil(t, laneInfo,
		"предпосылка стража не выполнена: у производителя полосы резолва нет ErrorInfo — сверять домен не с чем")
	lane := laneInfo.GetDomain()
	require.NotEmpty(t, lane, "предпосылка стража не выполнена: домен полосы резолва пуст")

	producers := refusalProducers()
	for _, p := range producers {
		t.Run(p.name, func(t *testing.T) {
			info := errorInfoOf(t, p.err)
			require.NotNil(t, info, "причина обязана нести машинный признак")
			require.Equal(t, lane, info.GetDomain(),
				"источник отказа обязан быть один на сервис: полоса резолва отвечает %q", lane)
		})
	}
	t.Logf("осмотрено производителей причины: %d при домене полосы резолва %q", len(producers), lane)
}

// Код и признак — две половины одного утверждения о полосе. Проба держит их
// вместе, чтобы расхождение было красным, а не наблюдаемым у клиента.
func TestCapacityReasonCarriesItsOwnCode(t *testing.T) {
	producers := refusalProducers()
	for _, p := range producers {
		t.Run(p.name, func(t *testing.T) {
			require.Equal(t, p.code, status.Code(p.err))
			info := errorInfoOf(t, p.err)
			require.NotNil(t, info)
			require.Equal(t, p.reason, info.GetReason())
		})
	}
	t.Logf("осмотрено пар «код ↔ признак»: %d", len(producers))
}

// Деталь не говорит больше сообщения. У внешнего отказа метаданных нет вовсе —
// он намеренно не называет ни пула, ни зоны; у подсети метаданные несут ровно
// тот идентификатор, который уже стоит в тексте.
func TestCapacityReasonMetadataSaysNoMoreThanTheMessage(t *testing.T) {
	t.Run("external/no-metadata", func(t *testing.T) {
		for _, v := range []domain.IpVersion{domain.IpVersionIPv4, domain.IpVersionIPv6} {
			info := errorInfoOf(t, noExternalAddressAvailable(v))
			require.NotNil(t, info)
			require.Empty(t, info.GetMetadata(),
				"внешний отказ ничего не называет в тексте — не вправе называть и в деталях")
		}
	})

	t.Run("subnet/metadata-mirrors-the-message", func(t *testing.T) {
		for _, err := range []error{
			noFreeSubnetAddress(probeSubnetID, domain.IpVersionIPv4),
			allocationContended(probeSubnetID, domain.IpVersionIPv6),
		} {
			info := errorInfoOf(t, err)
			require.NotNil(t, info)
			msg := status.Convert(err).Message()
			for key, val := range info.GetMetadata() {
				if key != "resource_id" {
					continue
				}
				require.Contains(t, msg, val,
					"метаданные назвали идентификатор, которого нет в тексте")
			}
		}
	})
}

// Перепись по обеим осям (семейство × исход): ни идентификатора админского пула,
// ни признаков сырого текста хранилища в сообщении нет — и сообщение при этом
// непустое. Второе условие обязательно: без него все четыре отрицания зеленели бы
// на конструкторе, который вернул пустую строку.
func TestRefusalTextsCarryNoAdminCoordinate(t *testing.T) {
	producers := refusalProducers()
	for _, p := range producers {
		t.Run(p.name, func(t *testing.T) {
			msg := status.Convert(p.err).Message()
			require.NotEmpty(t, msg, "положительный контроль: сообщение обязано быть непустым")
			require.NotContains(t, msg, adminPoolID, "идентификатор админского пула в ответе вызывающему")
			require.NotContains(t, msg, "apl-", "префикс идентификатора пула в ответе вызывающему")
			require.NotContains(t, msg, "SQLSTATE", "сырой признак хранилища в ответе вызывающему")
			require.NotContains(t, msg, "address pool", "имя админского ресурса в ответе вызывающему")
		})
	}
	t.Logf("осмотрено текстов отказа: %d", len(producers))
}
