// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: AGPL-3.0-or-later

package role

// Системная роль, посеянная миграцией, обязана ЧИТАТЬСЯ ПО СВОЕМУ id.
//
// Задача #1808. Идентификатор роли viewer имел длину 21 при требуемых 20, а
// строгая проверка формы стоит ПЕРВЫМ стейтментом каждого глагола роли, — то
// есть роль отвергалась `INVALID_ARGUMENT` ещё до чтения. Арендатор получал её
// в ответе `List` и не мог прочитать ни одним путём.
//
// # ПОЧЕМУ ПРОБА ПАРНАЯ
//
// Полосы две — admin и viewer, — и различие между ними НИКЕМ НЕ РЕШАЛОСЬ: у
// admin длина 20 и своя константа в `domain`, у viewer не было ни того, ни
// другого. Утверждение об одной полосе зеленело бы на проверке, не отвергающей
// ничего; проба спрашивает ОБЕ и печатает, сколько полос осмотрела.
//
// # ЧТО ПРОБА СУДИТ
//
// Она судит ПОЛОСУ ОТКАЗА, а не хранилище: репозиторий здесь поддельный, и его
// строка заведена под тем же идентификатором. Значит красное может прийти
// только от проверки формы — от того самого стейтмента, который стоит первым.

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/iam/internal/domain"
	"github.com/PRO-Robotech/kacho/services/iam/internal/testsupport/catalogfixture"
)

// seededSystemRole — одна полоса: посеянный идентификатор и его имя.
type seededSystemRole struct {
	lane string
	id   string
	name string
}

// seededSystemRoles — обе системные роли кластера, каждая по СВОЕЙ константе
// домена. Литералы здесь не пишутся намеренно: расхождение константы с посевом
// ловит гейт `internal/check`, а не эта проба.
func seededSystemRoles() []seededSystemRole {
	return []seededSystemRole{
		{"admin", domain.SystemAdminRoleID, "kacho-system.admin"},
		{"viewer", domain.SystemViewerRoleID, "kacho-system.viewer"},
	}
}

func TestGetRole_SeededSystemRolesAreReadableByTheirOwnID(t *testing.T) {
	lanes := 0
	for _, lane := range seededSystemRoles() {
		lane := lane
		t.Run(lane.lane, func(t *testing.T) {
			repo := newRoleListFakeRepo()
			repo.roles[lane.id] = domain.Role{
				ID:        domain.RoleID(lane.id),
				Name:      domain.RoleName(lane.name),
				IsSystem:  true,
				Rules:     domain.Rules{{Module: "*", Resources: []string{"*"}, Verbs: []string{"get"}}},
				CreatedAt: time.Now().UTC(),
			}

			uc := NewGetRoleUseCase(repo, catalogfixture.Source()).WithRelationStore(newRoleFGAStub())
			got, err := uc.Execute(ctxUser("usr-u1"), domain.RoleID(lane.id))

			require.NoErrorf(t, err,
				"полоса %s: посеянная системная роль %q не читается по своему id — "+
					"код %s; проверка формы стоит первым стейтментом глагола, поэтому "+
					"негодный посев отвергает роль ДО чтения",
				lane.lane, lane.id, status.Code(err))
			require.NotEqual(t, codes.InvalidArgument, status.Code(err))
			require.Equal(t, lane.id, string(got.ID))
		})
		lanes++
	}
	require.Equal(t, 2, lanes, "осмотрено полос %d — обе системные роли обязаны быть спрошены", lanes)
	t.Logf("перепись: полос системных ролей осмотрено %d", lanes)
}
