// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

// authz_id_shape_test.go — полоса 5b судит id типа, который чеканит сама
// платформа, по ЕГО КАНОНИЧЕСКОЙ ФОРМЕ, а не по одному префиксу (kacho#2976).
//
// ПРЕДМЕТ. Корпоративный валидатор `validate.ResourceID` по своему контракту
// смотрит только на первые три символа: «Длину/алфавит тела внутри здесь не
// проверяем». Полоса 5b на нём одном пропускала `snpe` и `snp`+16 к модели прав —
// id известного семейства, но формы, которой ни одна служба не выдаёт. Хуже того,
// исход зависел от СЛОВАРЯ префиксов: появление префикса `nop` в фундаменте
// превратило негодный `nope` в «годный» и сняло отказ края на сквозной пробе
// storage (SNP-GET-NEG-MALFORMED-ID).
//
// ПРАВИЛО. Тип, чьи id чеканит служба kacho (`ids.NewID` → префикс + 17 знаков
// Crockford; `ids.NewHyphenID` → префикс-17 знаков), судится строгой формой. Тип
// домена kaname судит его владелец: у kaname есть законные id вне канона
// (системные роли `rol000000000sysadmin`, `rol000000000sysviewer`), и край их не
// отвергает.
//
// Каталог берётся ВСТРОЕННЫЙ и целиком: принадлежность типа выводится из всего
// каталога, и выписанная выборка проверяла бы не тот предмет.

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	iamv1 "github.com/PRO-Robotech/kaname/pkg/api/kaname/cloud/iam/v1"

	computev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"
	storagev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/storage/v1"
)

const (
	fqnSnapshotGet = "kacho.cloud.storage.v1.SnapshotService/Get"
	fqnInstanceGet = "kacho.cloud.compute.v1.InstanceService/Get"
	fqnRoleDelete  = "kaname.cloud.iam.v1.RoleService/Delete"
)

// callThroughAuthz проводит запрос через страж края с разрешающей моделью прав и
// возвращает ошибку стража и число обращений к модели прав.
func callThroughAuthz(t *testing.T, fqn string, req proto.Message) (error, int64) {
	t.Helper()
	checker := &fakeChecker{allowed: true}
	mw := buildAuthzMiddleware(t, embeddedCatalog(t), checker)
	_, err := mw.Unary()(withTokenMD("usr_x", "user"), req,
		&grpc.UnaryServerInfo{FullMethod: "/" + fqn},
		func(ctx context.Context, req any) (any, error) { return "ok", nil })
	return err, checker.calls.Load()
}

// requireEdgeShapeRefusal — отказ полосы формы края: InvalidArgument, текст края
// дословно, модель прав не спрошена.
func requireEdgeShapeRefusal(t *testing.T, id string, err error, checks int64) {
	t.Helper()
	require.Error(t, err, "id %q неканонической формы обязан быть отвергнут краем", id)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code(), "id %q", id)
	assert.Equal(t, "invalid resource id '"+id+"'", st.Message(), "id %q", id)
	assert.Zero(t, checks, "модель прав не спрашивается о негодном id %q", id)
}

// requirePassedToCheck — id прошёл полосу формы и дошёл до модели прав.
func requirePassedToCheck(t *testing.T, id string, err error, checks int64) {
	t.Helper()
	require.NoError(t, err, "id %q законной формы обязан пройти полосу формы края", id)
	assert.Equal(t, int64(1), checks, "id %q обязан дойти до модели прав", id)
}

// TestAuthz_KachoTypeIdOfKnownPrefixButWrongShapeIsRefusedByTheEdge — известный
// префикс kacho-типа при неканонической длине: край отвечает 400 сам.
func TestAuthz_KachoTypeIdOfKnownPrefixButWrongShapeIsRefusedByTheEdge(t *testing.T) {
	for _, id := range []string{
		"snpe",                                // префикс + один знак
		"snp" + strings.Repeat("0", 16),       // на знак короче канона
		"snp" + strings.Repeat("0", 18),       // на знак длиннее канона
		"snp" + strings.Repeat("0", 16) + "u", // длина канона, знак вне Crockford
		"nope",                                // вход сквозной пробы SNP-GET-NEG-MALFORMED-ID
	} {
		t.Run(id, func(t *testing.T) {
			err, checks := callThroughAuthz(t, fqnSnapshotGet, &storagev1.GetSnapshotRequest{SnapshotId: id})
			requireEdgeShapeRefusal(t, id, err, checks)
		})
	}
}

// TestAuthz_KachoTypeCanonicalIdPassesTheShapeLane — близнец: id ровно той
// формы, какую выдаёт ids.NewID(snp), проходит к модели прав.
func TestAuthz_KachoTypeCanonicalIdPassesTheShapeLane(t *testing.T) {
	id := "snp" + strings.Repeat("0", 17)
	err, checks := callThroughAuthz(t, fqnSnapshotGet, &storagev1.GetSnapshotRequest{SnapshotId: id})
	requirePassedToCheck(t, id, err, checks)
}

// TestAuthz_KachoTypeHyphenFormIsJudgedByItsShape — форма с дефисом kacho-типа
// (ids.NewHyphenID(ins)): тело ровно 17 знаков Crockford.
func TestAuthz_KachoTypeHyphenFormIsJudgedByItsShape(t *testing.T) {
	for _, id := range []string{
		"ins-" + strings.Repeat("0", 16),       // на знак короче
		"ins-" + strings.Repeat("0", 18),       // на знак длиннее
		"ins-" + strings.Repeat("0", 16) + "i", // знак вне Crockford
		"ins-",                                 // пустое тело
	} {
		t.Run(id, func(t *testing.T) {
			err, checks := callThroughAuthz(t, fqnInstanceGet, &computev1.GetInstanceRequest{InstanceId: id})
			requireEdgeShapeRefusal(t, id, err, checks)
		})
	}
	t.Run("canonical", func(t *testing.T) {
		id := "ins-" + strings.Repeat("0", 17)
		err, checks := callThroughAuthz(t, fqnInstanceGet, &computev1.GetInstanceRequest{InstanceId: id})
		requirePassedToCheck(t, id, err, checks)
	})
}

// TestAuthz_KanameTypeIdIsLeftToItsOwner — близнец по оси владельца: системная
// роль kaname вне канона формы проходит полосу формы края, как и прежде; её форму
// судит kaname.
func TestAuthz_KanameTypeIdIsLeftToItsOwner(t *testing.T) {
	for _, id := range []string{"rol000000000sysadmin", "rol000000000sysviewer"} {
		t.Run(id, func(t *testing.T) {
			err, checks := callThroughAuthz(t, fqnRoleDelete, &iamv1.DeleteRoleRequest{RoleId: id})
			requirePassedToCheck(t, id, err, checks)
		})
	}
}

// TestAuthz_ForeignTypeNamedByAKachoMethodIsLeftToItsOwner — близнец по второму
// признаку принадлежности: метод kacho, чья область — тип службы доступа
// (`project`), судит id прежним судом одним префиксом. Строгой форме подлежит
// только тип, который kacho чеканит сам; `project` называют своей областью и
// методы `kaname.cloud.iam`.
func TestAuthz_ForeignTypeNamedByAKachoMethodIsLeftToItsOwner(t *testing.T) {
	id := "prj0000000000wellfmx" // длина канона, `l` вне Crockford
	err, checks := callThroughAuthz(t, "kacho.cloud.storage.v1.SnapshotService/List",
		&storagev1.ListSnapshotsRequest{ProjectId: id})
	requirePassedToCheck(t, id, err, checks)
}
