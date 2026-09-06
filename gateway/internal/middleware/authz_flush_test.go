// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestAuthzMiddleware_MaybeFlushOnMutation — a successful AccessBinding
// mutation flushes the whole decision cache so a just-revoked grant cannot be
// served stale from cache.
func TestAuthzMiddleware_MaybeFlushOnMutation(t *testing.T) {
	m, err := NewAuthzMiddleware(AuthzMiddlewareConfig{Enabled: false})
	require.NoError(t, err)
	m.cache = newDecisionCache(100, 5*time.Second, time.Now)
	m.cache.put("user:nob|iam.projects.get|project|prj1|")

	// Non-mutation / non-2xx — cache untouched.
	m.MaybeFlushOnMutation("kaname.cloud.iam.v1.ProjectService/Get", 200)
	require.Equal(t, 1, m.cache.Size())
	m.MaybeFlushOnMutation("kaname.cloud.iam.v1.AccessBindingService/Delete", 500)
	require.Equal(t, 1, m.cache.Size())

	// Successful AccessBinding mutation — full flush.
	m.MaybeFlushOnMutation("kaname.cloud.iam.v1.AccessBindingService/Delete", 200)
	require.Equal(t, 0, m.cache.Size())
}

// TestAuthzMiddleware_SelfFlushCoversEveryGrantChangingVerb — самосброс срабатывает
// на КАЖДОМ глаголе, меняющем права, а не только на двух из пяти.
//
// # Почему это отдельная проба, а не строка к предыдущей
//
// Предыдущая утверждает МЕХАНИЗМ («успешная мутация гасит кэш»), эта — ОХВАТ
// («каких мутаций это касается»). Механизм был исправен всё время; неполон был
// перечень, и потому проба на одном глаголе оставалась зелёной ровно тогда, когда
// три остальных не гасили ничего.
//
// # Что было красным до правки
//
// `Revoke`, `AddMember`, `RemoveMember` кэш не гасили: реплика, обслужившая отзыв,
// продолжала отвечать по закешированному вердикту до следующего чтения журнала.
// Дольше всего это длилось там, где пользователь только что нажал «отозвать», —
// то есть ровно в том случае, ради которого сброс и заведён.
//
// # Отрицательный контроль стоит рядом намеренно
//
// Без него проба зеленела бы на наборе, в который свалили ВСЕ методы: тогда
// «гасит на этих» перестало бы что-либо утверждать.
func TestAuthzMiddleware_SelfFlushCoversEveryGrantChangingVerb(t *testing.T) {
	const key = "user:nob|iam.projects.get|project|prj1|"

	// Полоса, обязанная гасить. Перечень зеркалит производителей журнала смены
	// субъекта; сходимость обоих перечней держит гейт дерева
	// TestSelfFlushCoversEveryProducerOfTheSubjectChangeQueue.
	for _, fqn := range []string{
		"kaname.cloud.iam.v1.AccessBindingService/Create",
		"kaname.cloud.iam.v1.AccessBindingService/Delete",
		"kaname.cloud.iam.v1.AccessBindingService/Revoke",
		"kaname.cloud.iam.v1.GroupService/AddMember",
		"kaname.cloud.iam.v1.GroupService/RemoveMember",
	} {
		t.Run(fqn, func(t *testing.T) {
			m, err := NewAuthzMiddleware(AuthzMiddlewareConfig{Enabled: false})
			require.NoError(t, err)
			m.cache = newDecisionCache(100, 5*time.Second, time.Now)
			m.cache.put(key)
			require.Equal(t, 1, m.cache.Size(), "предпосылка: вердикт закеширован")

			m.MaybeFlushOnMutation(fqn, 200)

			require.Equal(t, 0, m.cache.Size(),
				"глагол меняет права и пишет строку в журнал смены субъекта, но кэш "+
					"обслужившей реплики не погас: она продолжит отвечать по отозванному "+
					"праву до следующего чтения журнала")
		})
	}

	// Отрицательный контроль: глагол, прав НЕ меняющий, кэш не трогает. Без него
	// утверждение выше выполнялось бы набором, в который свалили всё подряд.
	t.Run("правка меток привязки прав не меняет и кэш не гасит", func(t *testing.T) {
		m, err := NewAuthzMiddleware(AuthzMiddlewareConfig{Enabled: false})
		require.NoError(t, err)
		m.cache = newDecisionCache(100, 5*time.Second, time.Now)
		m.cache.put(key)

		m.MaybeFlushOnMutation("kaname.cloud.iam.v1.AccessBindingService/Update", 200)

		require.Equal(t, 1, m.cache.Size(),
			"Update правит метки и защиту от удаления, строки в журнал не пишет — "+
				"гасить кэш ему не на чем")
	})
}
