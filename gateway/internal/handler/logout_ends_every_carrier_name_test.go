// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// logout_ends_every_carrier_name_test.go — ОТВЕТ ОБРАБОТЧИКА ВЫХОДА КРАЯ НЕСЁТ
// ГАШЕНИЕ ПО ВСЕМУ ПЕРЕЧНЮ ИМЁН носителя, что бы ни принёс браузер.
//
// # Чего проба НЕ утверждает
//
// Она судит ОТВЕТ — выдано ли гашение каждому имени, — а не хранилище печений
// браузера. Совпадёт ли выданное гашение с печеньем, решает `Domain` выдачи:
// гашение края идёт без него, и с чужим печеньем, выданным с `Domain`, не
// совпадает. Что из этого следует, названо в шапке
// `middleware/session_carrier_names.go`.
//
// # Как проба не даёт себя обмануть
//
// Перечень гасимых имён берётся У ПРОИЗВОДИТЕЛЯ (`middleware.SessionCarrierNames`),
// а не выписывается здесь: выписанный разошёлся бы с продуктом молча при
// добавлении нового имени — проба осталась бы зелёной, проверяя часть перечня.
package handler_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/gateway/internal/handler"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

func TestLogout_EndsEveryCarrierNameWhateverTheBrowserPresents(t *testing.T) {
	names := middleware.SessionCarrierNames()
	require.NotEmpty(t, names, "перечень гасимых имён пуст — гасить нечего, и проба судила бы о непрочитанном")

	h, err := handler.NewLogoutHandler(handler.LogoutHandlerConfig{Logger: newLogger()})
	require.NoError(t, err)

	// Обработчик выхода посадки не читает, поэтому единственное, чем браузер
	// может на него повлиять, — состав предъявленных печений.
	presentations := map[string][]string{
		"все имена перечня": names,
		"ни одного":         nil,
	}
	for _, n := range names {
		presentations["только "+n] = []string{n}
	}
	for label, presented := range presentations {
		t.Run(label, func(t *testing.T) {
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
					"предъявлено %v: имя %q не погашено — выход, гасящий не все имена, оставляет "+
						"браузеру вход по непогашенному", presented, n)
			}
			t.Logf("предъявлено имён %d · погашено %d из %d", len(presented), len(ended), len(names))
		})
	}
	t.Logf("перепись: составов предъявления %d · имён в перечне %d", len(presentations), len(names))
}
