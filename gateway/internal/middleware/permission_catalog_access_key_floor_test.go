// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

// permission_catalog_access_key_floor_test.go — пол глаголов ключа доступа на
// КОПИИ КРАЯ не ниже пола выпуска личного токена (приёмка службы
// PRO-Robotech/kaname#747, инвариант §7.1; kacho#3193).
//
// Ключ доступа даёт «2» одним предъявлением. Будь пол его заведения ниже пола
// выпуска личного токена, способ, дающий «2», заводился бы через более дешёвую
// дверь, чем токен. Служба держит это у себя; край судит пол по своей
// встроенной копии каталога, и копия, отставшая от службы, открыла бы ту же
// дешёвую дверь на публичном пути. Проба сравнивает ОТНОШЕНИЕ двух записей
// копии края, а не литерал: смена шкалы уровней не делает её ложной.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/corelib/acrlevel"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// accessKeyFloorAnchor — запись, ниже пола которой глаголы ключа не опускаются.
const accessKeyFloorAnchor = "kaname.cloud.iam.v1.UserTokenService/Issue"

// accessKeyFloorVerbs — глаголы, заводящие и снимающие ключ доступа.
func accessKeyFloorVerbs() []string {
	return []string{
		"kaname.cloud.iam.v1.AccessKeyService/BeginRegistration",
		"kaname.cloud.iam.v1.AccessKeyService/FinishRegistration",
		"kaname.cloud.iam.v1.AccessKeyService/Revoke",
	}
}

// floorBelowAnchor — РЕШЕНИЕ чистой функцией: пол глагола ниже пола якоря.
// Пустой пол глагола — «пола нет», то есть ниже любого объявленного якоря.
func floorBelowAnchor(verbFloor, anchorFloor string) bool {
	return acrlevel.Rank(verbFloor) < acrlevel.Rank(anchorFloor)
}

func TestPermissionCatalog_AccessKeyFloorNotBelowUserTokenIssue(t *testing.T) {
	c, err := middleware.LoadEmbeddedPermissionCatalog("")
	require.NoError(t, err)

	anchor, ok := c.Lookup(accessKeyFloorAnchor)
	require.True(t, ok, "якорь отсутствует в каталоге края: %s", accessKeyFloorAnchor)
	// Предпосылка: якорь несёт объявленный пол. Без него сравнение беспредметно.
	require.NotEmpty(t, anchor.RequiredACRMin, "якорь %s не несёт пола", accessKeyFloorAnchor)

	verbs := accessKeyFloorVerbs()
	judged := 0
	for _, fqn := range verbs {
		e, ok := c.Lookup(fqn)
		require.True(t, ok, "глагол ключа отсутствует в каталоге края: %s", fqn)
		judged++
		assert.False(t, floorBelowAnchor(e.RequiredACRMin, anchor.RequiredACRMin),
			"пол %s (%q) ниже пола %s (%q) — способ, дающий «2», заводится дешевле токена",
			fqn, e.RequiredACRMin, accessKeyFloorAnchor, anchor.RequiredACRMin)
	}
	t.Logf("осмотрено: глаголов ключа %d из %d, пол якоря %q", judged, len(verbs), anchor.RequiredACRMin)
}

// TestAccessKeyFloorJudgeInjection — решение краснеет на настоящем дефекте
// (прежний пол «1» при якоре «2») и молчит на законном близнеце («2» при «2»).
func TestAccessKeyFloorJudgeInjection(t *testing.T) {
	assert.True(t, floorBelowAnchor("1", "2"), "пол «1» при якоре «2» обязан быть находкой")
	assert.True(t, floorBelowAnchor("", "2"), "отсутствие пола при якоре «2» обязано быть находкой")
	assert.False(t, floorBelowAnchor("2", "2"), "равный пол — законный близнец, находкой не является")
	assert.False(t, floorBelowAnchor("3", "2"), "пол выше якоря находкой не является")
}
