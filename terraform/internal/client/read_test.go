// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Сценарий 12 приёмки — четыре случая, и каждый обязан быть отличим.
//
// Три из них дают ОДИН И ТОТ ЖЕ первый ответ («не найдено» по идентификатору), поэтому
// набор проверяется целиком: тест на один случай зеленел бы на реализации, которая всегда
// возвращает именно его.
func TestConfirmAbsenceDistinguishesFourCases(t *testing.T) {
	cases := []struct {
		name string
		// byName — ответ на список с фильтром по имени; control — на контрольный список.
		byName, control string
		byNameCode      int
		controlCode     int
		want            Verdict
		why             string
	}{
		{
			name: "а: настоящее отсутствие",
			// по имени пусто, но в проекте есть другие сети ⇒ выдача работает
			byName: `{"networks":[]}`, control: `{"networks":[{"id":"netother"}]}`,
			byNameCode: 200, controlCode: 200,
			want: VerdictGone,
			why:  "контрольная страница непуста — значит пообъектная выдача жива",
		},
		{
			name:   "б: ресурс на месте, первый ответ был окном прав",
			byName: `{"networks":[{"id":"netmine"}]}`, control: `{"networks":[]}`,
			byNameCode: 200, controlCode: 200,
			want: VerdictPresent,
			why:  "ресурс найден по имени — расхождения нет",
		},
		{
			name:   "в: доступ к проекту утрачен",
			byName: `{"code":7,"message":"no authorization path"}`, control: `{"networks":[]}`,
			byNameCode: 403, controlCode: 200,
			want: VerdictDenied,
			why:  "список отвечает отказом — это событие прав, а не удаление",
		},
		{
			name:   "г: различить нечем — контрольная страница пуста целиком",
			byName: `{"networks":[]}`, control: `{"networks":[]}`,
			byNameCode: 200, controlCode: 200,
			want: VerdictAmbiguous,
			why:  "пусто и по имени, и в проекте: удаление и отзыв пообъектных прав неразличимы",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var nth int
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				nth++
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Query().Get("filter") != "" {
					w.WriteHeader(tc.byNameCode)
					_, _ = w.Write([]byte(tc.byName))
					return
				}
				w.WriteHeader(tc.controlCode)
				_, _ = w.Write([]byte(tc.control))
			}))
			defer srv.Close()

			c := mustClient(t, srv.URL)
			got, err := c.ConfirmAbsence(context.Background(), "/vpc/v1/networks", "prj1", "my-net")
			if err != nil && tc.want != VerdictAmbiguous {
				t.Fatalf("неожиданная ошибка: %v", err)
			}
			if got != tc.want {
				t.Errorf("получено %v, ожидалось %v — %s", got, tc.want, tc.why)
			}
		})
	}
}

// Ответ, не являющийся ответом края, НЕ приводит к выводу об отсутствии (сценарий 35).
// Это тот случай, где опечатка в адресе иначе читается как «инфраструктуры больше нет».
func TestConfirmAbsenceRefusesToConcludeOnNonEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("<html>404</html>"))
	}))
	defer srv.Close()

	c := mustClient(t, srv.URL)
	got, _ := c.ConfirmAbsence(context.Background(), "/vpc/v1/networks", "prj1", "my-net")
	if got == VerdictGone {
		t.Error("ответ чужого прокси принят за удаление ресурса")
	}
	if got != VerdictAmbiguous {
		t.Errorf("получено %v, ожидалось Ambiguous", got)
	}
}
