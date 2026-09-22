// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// logout_carrier_states_test.go — ОТВЕТ ВЫХОДА НЕСЁТ ГАШЕНИЕ ПО ОБОИМ ИМЕНАМ
// во всех трёх состояниях.
//
// # Почему это отдельный предмет переходного состояния
//
// Множество читателей носителя (`config.SessionCarrierSet`) выражает три
// состояния: только чужой · оба · только наш. Перечень гашения к нему НЕ
// привязан, и это решение, а не недосмотр: привязанный к множеству, он в
// состоянии «только наш» не выдавал бы гашения чужому имени вовсе.
//
// # Чего проба НЕ утверждает
//
// Она судит ОТВЕТ — выдано ли гашение каждому имени, — а не хранилище печений
// браузера. Совпадёт ли выданное гашение с печеньем, решает `Domain` выдачи:
// гашение края идёт без него, и с чужим печеньем, выданным с `Domain`, не
// совпадает. Какое предусловие объявления из этого следует, названо в шапке
// `middleware/session_carrier_names.go`.
//
// # Что проба утверждает и как она не даёт себя обмануть
//
// Перечень гасимых имён берётся У ПРОИЗВОДИТЕЛЯ (`middleware.SessionCarrierNames`),
// а не выписывается здесь: выписанный разошёлся бы с продуктом молча при
// добавлении третьего имени — проба осталась бы зелёной, проверяя два из трёх.
package handler_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/gateway/internal/handler"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

func TestLogout_EndsEveryCarrierNameInEveryCarrierState(t *testing.T) {
	names := middleware.SessionCarrierNames()
	require.NotEmpty(t, names, "перечень гасимых имён пуст — гасить нечего, и проба судила бы о непрочитанном")

	h, err := handler.NewLogoutHandler(handler.LogoutHandlerConfig{Logger: newLogger()})
	require.NoError(t, err)

	// Обработчик выхода множества читателей НЕ ЧИТАЕТ, поэтому «три состояния»
	// здесь представлены тем, что приносит БРАУЗЕР: состав предъявленных
	// печений — единственное, чем состояние может на него повлиять.
	states := map[string][]string{
		"только чужой": {middleware.SessionCarrierNames()[1]},
		"оба":          middleware.SessionCarrierNames(),
		"только наш":   {middleware.SessionCarrierNames()[0]},
		"ни одного":    nil,
	}
	for state, presented := range states {
		t.Run(state, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/oauth/logout", nil)
			for _, n := range presented {
				req.AddCookie(&http.Cookie{Name: n, Value: "v-" + n})
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			require.Equal(t, http.StatusOK, rec.Code)

			ended := map[string]bool{}
			for _, c := range rec.Result().Cookies() {
				if c.MaxAge < 0 {
					ended[c.Name] = true
				}
			}
			for _, n := range names {
				require.Truef(t, ended[n],
					"состояние %q: имя %q не погашено — выход, гасящий не все имена, оставляет "+
						"браузеру вход, который вернётся откатом профиля на одно состояние назад",
					state, n)
			}
			t.Logf("состояние %q: предъявлено имён %d · погашено %d из %d",
				state, len(presented), len(ended), len(names))
		})
	}
	t.Logf("перепись: состояний проверено %d · имён в перечне %d", len(states), len(names))
}

// Законный близнец: гашение идёт по ВСЕМУ перечню производителя, а не по двум
// выписанным здесь. Перечень вырос — проба обязана это увидеть, а не молчать.
func TestLogout_TheEndedNamesComeFromTheProducerNotFromThisFile(t *testing.T) {
	names := middleware.SessionCarrierNames()
	require.Len(t, names, 2,
		"перечень гасимых имён изменился (%v): состояния выше перечисляют имена по позиции, и их "+
			"обязано пересмотреть то же изменение, что расширило перечень", names)
}
