// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// migration_0039_nic_status_binding_only_integration_test.go — снятое с контракта
// значение статуса интерфейса не выразимо и в базе (APPLY-02, APPLY-03).
//
// # Предмет
//
// Перечисление `NetworkInterface.Status` объявляло шесть значений, а
// производилось два. Три значения — те, что заявляли программирование
// датаплейна, — сняты с контракта. Ограничение столбца при этом продолжало бы
// их принимать, то есть путь записи в обход use-case (посев, ручная правка,
// будущий репозиторий) мог бы вписать значение, которого контракт не знает, и
// путь чтения отдал бы его как `STATUS_UNSPECIFIED`. Снятие с контракта без
// сужения ограничения оставляет состояние записываемым — и невыразимым.
//
// # Почему обратное заполнение выводится из привязки
//
// Направление объявлено в самой миграции и берётся из АВТОРИТЕТА, а не из
// удобства: `used_by_id` и есть то, что это поле выражает. Строка со снятым
// значением получает `ACTIVE`, если потребитель у неё есть, и `AVAILABLE`
// иначе. Придумать здесь «наиболее вероятное» значение было бы утверждением о
// состоянии, которого никто не наблюдал.
//
// # Почему число переписанных строк ПЕЧАТАЕТСЯ
//
// Ожидаемое число на чистом дереве — ноль, и «ноль переписано» обязано быть
// отличимо от «не смотрели»: миграция, молча ничего не сделавшая, и миграция,
// не нашедшая работы, выглядят одинаково ровно до того дня, когда разница
// понадобится.
package migrations_test

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
)

// preNICStatusBindingVersion — последняя миграция ДО 0039.
const preNICStatusBindingVersion int64 = 38

// nicStatusBindingVersion — сама 0039.
const nicStatusBindingVersion int64 = 39

// seedNICParentSubnet — сеть и подсеть, без которых интерфейс не вписать (FK).
func seedNICParentSubnet(t *testing.T, db *sql.DB, tag string) string {
	t.Helper()
	netID := ids.NewID(ids.PrefixNetwork)
	subID := ids.NewID(ids.PrefixSubnet)
	_, err := db.Exec(
		`INSERT INTO networks (id, project_id, name) VALUES ($1, 'prj-39', $2)`, netID, "n39"+tag)
	require.NoError(t, err)
	_, err = db.Exec(`
		INSERT INTO subnets (id, project_id, name, network_id, zone_id, placement_type, v4_cidr_blocks)
		VALUES ($1, 'prj-39', $2, $3, 'zone-39', 'ZONAL', ARRAY['10.39.0.0/24'])`,
		subID, "s39"+tag, netID)
	require.NoError(t, err)
	return subID
}

// insertNICWithStatus вписывает интерфейс с заданным статусом и привязкой.
// Возвращает ошибку, а не роняет пробу: часть утверждений ниже ждёт отказа базы.
func insertNICWithStatus(db *sql.DB, subnetID, status, usedByID string, macTail int) (string, error) {
	id := ids.NewID(ids.PrefixNetworkInterface)
	_, err := db.Exec(`
		INSERT INTO network_interfaces (id, name, project_id, subnet_id, mac_address, status, used_by_id, used_by_type)
		VALUES ($1, $1, 'prj-39', $2, $3, $4, $5, $6)`,
		id, subnetID, fmt.Sprintf("0e:39:00:00:00:%02x", macTail), status, usedByID,
		map[bool]string{true: "compute_instance", false: ""}[usedByID != ""])
	return id, err
}

// TestIntegration_Migration0039_RetiredStatusIsNotExpressibleInTheDatabase —
// APPLY-02: ограничение столбца сужено до состава контракта.
//
// Отрицания стоят В ПАРЕ с положительными: ограничение, отвергающее всё, прошло
// бы каждую отрицательную половину и было бы полностью сломанным.
func TestIntegration_Migration0039_RetiredStatusIsNotExpressibleInTheDatabase(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	db, _ := openChainAt(t, nicStatusBindingVersion)
	sub := seedNICParentSubnet(t, db, "a")

	for i, tc := range []struct {
		status  string
		accepts bool
		why     string
	}{
		{domain.NIStatusStrAvailable, true,
			"живое значение обязано проходить, иначе отрицания ниже беспредметны"},
		{domain.NIStatusStrActive, true,
			"второе живое значение — тот же положительный контроль с другой стороны"},
		{domain.NIStatusStrUnspecified, true,
			"нулевое значение перечисления остаётся выразимым: его отдаёт чтение legacy-строки"},
		{"PROVISIONING", false, "снято с контракта — производителя не было ни одного"},
		{"FAILED", false, "снято с контракта — производителя не было ни одного"},
		{"DELETING", false, "снято с контракта — производителя не было ни одного"},
	} {
		t.Run(tc.status, func(t *testing.T) {
			_, err := insertNICWithStatus(db, sub, tc.status, "", i+1)
			if tc.accepts {
				assert.NoError(t, err, tc.why)
				return
			}
			require.Error(t, err, tc.why)
			assert.Contains(t, err.Error(), "network_interfaces_status_check",
				"отказ обязан прийти от ИМЕНОВАННОГО ограничения, а не от чего попало: "+
					"иначе проба зеленела бы на любой посторонней ошибке вставки")
		})
	}
}

// TestIntegration_Migration0039_BackfillDerivesTheValueFromTheBinding —
// APPLY-03: обратное заполнение выводит значение из авторитета.
func TestIntegration_Migration0039_BackfillDerivesTheValueFromTheBinding(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	db, sink := openChainAt(t, preNICStatusBindingVersion)
	sub := seedNICParentSubnet(t, db, "b")

	boundID, err := insertNICWithStatus(db, sub, "PROVISIONING", "ci-39-bound", 11)
	require.NoError(t, err, "до 0039 снятое значение выразимо — иначе пробе нечего было бы переписывать")
	freeID, err := insertNICWithStatus(db, sub, "FAILED", "", 12)
	require.NoError(t, err)
	// Живая строка того же прогона: обратное заполнение обязано пройти МИМО неё.
	// Без этого «все строки получили ACTIVE» было бы неотличимо от правильного
	// исхода на выборке из одних только снятых значений.
	untouchedID, err := insertNICWithStatus(db, sub, domain.NIStatusStrAvailable, "ci-39-x", 13)
	require.NoError(t, err)

	require.NoError(t, goose.UpTo(db, ".", nicStatusBindingVersion))

	assert.Equal(t, domain.NIStatusStrActive, nicStatusOf(t, db, boundID),
		"строка с потребителем выражает привязку — значит ACTIVE")
	assert.Equal(t, domain.NIStatusStrAvailable, nicStatusOf(t, db, freeID),
		"строка без потребителя свободна — значит AVAILABLE")
	assert.Equal(t, domain.NIStatusStrAvailable, nicStatusOf(t, db, untouchedID),
		"живое значение переписывать нечем и незачем")

	notices := sink.text()
	assert.Contains(t, notices, "network_interfaces status narrow",
		"миграция обязана назвать себя в NOTICE — иначе прочитанное нечем атрибутировать")
	assert.Contains(t, notices, "rewritten 2 row(s)",
		"число переписанных строк печатается: «ноль переписано» обязано быть отличимо от «не смотрели»")
}

// TestIntegration_Migration0039_ZeroRewrittenIsPrinted — APPLY-03, вторая
// половина: на дереве без снятых значений число равно нулю И ПЕЧАТАЕТСЯ.
//
// Отдельная проба, а не ветка предыдущей: предмет здесь — наблюдаемость нуля, и
// он выполняется ровно тогда, когда переписывать нечего.
func TestIntegration_Migration0039_ZeroRewrittenIsPrinted(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	db, sink := openChainAt(t, preNICStatusBindingVersion)
	sub := seedNICParentSubnet(t, db, "c")
	_, err := insertNICWithStatus(db, sub, domain.NIStatusStrAvailable, "", 21)
	require.NoError(t, err)

	require.NoError(t, goose.UpTo(db, ".", nicStatusBindingVersion))

	notices := sink.text()
	require.Contains(t, notices, "network_interfaces status narrow")
	assert.Contains(t, notices, "rewritten 0 row(s)")
	assert.True(t, strings.Contains(notices, "examined 1 row(s)"),
		"объём осмотренного печатается вместе с нулём находок, иначе они неразличимы; напечатано:\n"+notices)
}

// nicStatusOf — статус строки по идентификатору.
func nicStatusOf(t *testing.T, db *sql.DB, id string) string {
	t.Helper()
	var s string
	require.NoError(t, db.QueryRow(
		`SELECT status FROM kacho_vpc.network_interfaces WHERE id = $1`, id).Scan(&s))
	return s
}
