// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Административная поверхность пределов обязана быть достижима снаружи — ADM-1 S1
// на предмете, ради которого стадия и заведена (#878).
//
// ПОЧЕМУ ЭТО ПРОБА КРАЯ, А НЕ СЕРВИСА. Величины назначает администратор облака, и
// назначает он их через край: страница пределов консоли — единственный
// пользователь этого глагола. Пока глагол объявлен только `Internal*`, край его
// наружу не отдаёт, и страница получает 404 — отказ, неотличимый от «такого
// раздела нет вовсе». Сервис при этом исправен, поэтому ни одна проба сервиса
// класса не видит by construction.
//
// ЗАПРЕТ 6 НЕ СМЯГЧЁН, и проба это утверждает отдельным пунктом: наружу выставлен
// публичный `LimitService`, а не `InternalLimitService`. Переезжает ГЛАГОЛ, а не
// разрешение для внутреннего сервиса.
func TestLimits_AdminSurfaceIsReachableFromOutside(t *testing.T) {
	c, err := LoadEmbeddedPermissionCatalog("")
	require.NoError(t, err)

	const (
		publicList   = "kaname.cloud.iam.v1.LimitService/List"
		internalList = "kaname.cloud.iam.v1.InternalLimitService/List"
	)

	pub, ok := c.Lookup(publicList)
	require.Truef(t, ok, "публичного глагола пределов в каталоге прав нет: %s. "+
		"Пока его нет, край отдаёт странице администратора 404, и отказ в доступе "+
		"неотличим от отсутствия раздела", publicList)

	// Решение о доступе не меняется переездом. Сравнение — ТОЙ ЖЕ функцией, что
	// у гейта общей пары: своя копия предиката доказывала бы лишь работу копии.
	intr, ok := c.Lookup(internalList)
	require.Truef(t, ok, "внутренний глагол пределов исчез из каталога: %s", internalList)
	if diff, same := accessDecisionDiffers(pub, intr); !same {
		t.Errorf("переезд пределов изменил требование к вызывающему (%s). "+
			"Публичный адрес обязан спрашивать ровно то же, что внутренний, "+
			"иначе стадия S1 не переносит поверхность, а расширяет доступ", diff)
	}

	// Отношение названо дословно: `system_admin` @ `cluster` — то, которое
	// подстановочный кортеж `user:*` НЕ выполняет. Не назови мы его здесь,
	// проба осталась бы зелёной и на паре, согласованно открытой всем.
	require.Equalf(t, "system_admin", pub.RequiredRelation,
		"публичный глагол пределов гейтится не тем отношением")

	// Маршрут разрешается на публичный FQN, а не на внутренний.
	r := NewRestRouter()
	fqn, ok := r.Resolve("GET", "/iam/v1/limits")
	require.Truef(t, ok, "GET /iam/v1/limits не разрешается ни во что — "+
		"именно этот адрес консоль получает как 404")
	require.Equalf(t, publicList, fqn,
		"адрес пределов ведёт во внутренний глагол — запрет 6 был бы смягчён")
}
