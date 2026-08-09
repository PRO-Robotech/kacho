// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package check

import (
	// Дескрипторы обслуживаемых RPC обязаны быть в бинаре ЭТОГО пакета: карта
	// строится из их аннотаций, и пустой реестр дал бы карту без единой записи —
	// то есть отказ на каждом вызове. Импорт делает предпосылку вывода
	// принадлежностью пакета, а не удачей чужого графа импортов.
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/api"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/registry/v1"
	"github.com/PRO-Robotech/kacho/pkg/authz"
	"github.com/PRO-Robotech/kacho/pkg/authz/catalogderive"
)

// protoPackages — proto-пакеты, чьи gRPC-сервисы поднимает registry.
//
// Это ЕДИНСТВЕННОЕ, что сервис заявляет здесь о правах, и заявляет он не права,
// а «какие RPC я обслуживаю». Требуемое отношение, тип объекта, поле с
// идентификатором и форма отказа приезжают из аннотаций тех же методов — из того
// же источника, из которого генерируется каталог края. Поэтому «сервис
// спрашивает не то, что объявлено оператору» перестало быть выразимым: двух
// объявлений больше нет.
var protoPackages = []string{
	"kacho.cloud.registry.v1",
	// LRO-конверт: Operation.Get/Cancel поднимает каждый сервис.
	"kacho.cloud.operation",
}

// PermissionMap — карта `<gRPC FullMethod>` → требуемое право, выведенная из
// аннотаций.
//
// Отказывает в старте (а не на первом запросе) при аннотации, называющей
// несуществующее поле запроса, при несвязанном пакете и при методе без
// аннотации вовсе: всё это свойства собранного бинаря, одинаковые на каждом
// старте, и каждое иначе выглядело бы как отказ по правам.
func PermissionMap() authz.RPCMap {
	return catalogderive.MustDerive(protoPackages...)
}
