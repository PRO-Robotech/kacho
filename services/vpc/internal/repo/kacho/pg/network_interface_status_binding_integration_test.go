// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/services/nicinternal"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/handler"
)

// network_interface_status_binding_integration_test.go — сужение перечисления не
// сломало живой переход состояния привязки (APPLY-04b).
//
// # Предмет
//
// Три значения статуса интерфейса сняты с контракта и из ограничения столбца.
// Снятие лишнего опасно ровно тем, что вместе с ним легко снять нужное: `ACTIVE`
// и `AVAILABLE` производит запись, и если бы сужение задело их, дефект был бы
// виден не сразу — интерфейс продолжал бы создаваться, а состояние привязки
// перестало бы что-либо означать.
//
// # Почему через глагол, а не через репозиторий напрямую
//
// Публичных глаголов привязки у интерфейса нет: привязку делает `compute`
// вызовом `InternalNetworkInterfaceService.Attach` на внутреннем слушателе.
// Проба идёт через ТОТ ЖЕ глагол — тонкий handler поверх настоящего use-case и
// настоящей базы, — потому что предмет утверждения именно «переход, который
// исполняет продукт», а не «UPDATE, который умеет репозиторий».
//
// Цепочка интерсепторов слушателя (mTLS, проверка прав) здесь не поднимается:
// её предмет — кто вправе звать глагол, и он проверяется своими пробами. Здесь
// проверяется ЧТО глагол делает с состоянием.
//
// # Почему утверждается ещё и «не принимает снятых значений»
//
// Без этой половины «переход работает» осталось бы верным и в мире, где статус
// принимает произвольную строку: положительное утверждение о двух значениях
// ничего не говорит о том, что третьего не появится.

// TestIntegration_NICStatus_BindingCycleSurvivesTheNarrowing — цикл
// AVAILABLE → ACTIVE → AVAILABLE на настоящей базе через внутренний глагол.
func TestIntegration_NICStatus_BindingCycleSurvivesTheNarrowing(t *testing.T) {
	e := newNICAttachEnv(t)
	const projectID = "prj-nic-status"
	netID := e.makeNetwork(t, projectID)
	subnetID := e.makeZonalSubnet(t, projectID, netID, "zone-a", "10.39.7.0/24")
	nicID := e.makeFreeNIC(t, projectID, subnetID, "0e:39:07:00:00:01")
	const instanceID = "epdinst0000039001"

	h := handler.NewInternalNetworkInterfaceHandler(nicinternal.NewService(e.repo))

	// Живые значения перечисления контракта. Множество выписано здесь, а не взято
	// из дескриптора: предмет утверждения — что глагол производит РОВНО их, и
	// вывести ожидание из того же источника, который проверяется, значило бы
	// сверить дескриптор с самим собой.
	alive := map[vpcv1.NetworkInterface_Status]bool{
		vpcv1.NetworkInterface_ACTIVE:    true,
		vpcv1.NetworkInterface_AVAILABLE: true,
	}
	seen := map[vpcv1.NetworkInterface_Status]bool{}

	// Given: интерфейс создан и не привязан. Читается СТОЛБЕЦ, а не проекция:
	// на этом шаге глагол ещё не вызывался, и утверждение относится к строке.
	var raw string
	require.NoError(t, e.pool.QueryRow(e.ctx,
		`SELECT status FROM kacho_vpc.network_interfaces WHERE id = $1`, nicID).Scan(&raw))
	assert.Equal(t, domain.NIStatusStrAvailable, raw, "свободный интерфейс не привязан")

	// When: привязка.
	att, err := h.Attach(e.ctx, &vpcv1.AttachNetworkInterfaceRequest{
		NicId:          nicID,
		InstanceId:     instanceID,
		InstanceName:   "vm-39",
		InstanceZoneId: "zone-a",
		ProjectId:      projectID,
	})
	require.NoError(t, err)
	assert.Equal(t, vpcv1.NetworkInterface_ACTIVE, att.GetNetworkInterface().GetStatus(),
		"привязанный интерфейс занят потребителем")
	seen[att.GetNetworkInterface().GetStatus()] = true

	// And: отвязка возвращает к исходному.
	det, err := h.Detach(e.ctx, &vpcv1.DetachNetworkInterfaceRequest{
		NicId:      nicID,
		InstanceId: instanceID,
	})
	require.NoError(t, err)
	assert.Equal(t, vpcv1.NetworkInterface_AVAILABLE, det.GetNetworkInterface().GetStatus(),
		"отвязанный интерфейс снова свободен")
	seen[det.GetNetworkInterface().GetStatus()] = true

	// And: ни одно наблюдённое значение не выходит за состав контракта.
	for st := range seen {
		assert.True(t, alive[st], "глагол произвёл значение %s вне живого состава перечисления", st)
	}
	assert.Len(t, seen, 2, "наблюдены оба живых значения — иначе утверждение о цикле беспредметно")
}
