// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Полос, читающих ОДНУ И ТУ ЖЕ браузерную сессию, в этом крае две:
//
//	1. полоса личности на пути запроса — AuthInterceptor.HTTP;
//	2. маршрут «кто я» — SessionIdentityHandler.Me, по которому консоль решает,
//	   вошёл человек или нет.
//
// Свойство, обязательное для одной, обязано проверяться СРАВНЕНИЕМ полос, а не
// по каждой отдельно. Проба по каждой в отдельности требует знать, каким
// свойство ДОЛЖНО быть, — а это и есть спорный вопрос; сравнение спрашивает
// другое: «решал ли кто-нибудь, что они различаются». На это ответ есть всегда.
//
// Именно эта разница и была предметом подфазы: полоса предъявителя про отзыв
// спрашивала, полоса cookie — нет, и различие возникло как побочный эффект, а не
// как чьё-то решение.

// laneVerdict — что полоса сказала про сессию: считает ли она человека вошедшим.
type laneVerdict struct {
	name   string
	signed bool
}

// askIdentityLane — вердикт полосы личности: дошёл ли запрос до backend.
func askIdentityLane(t *testing.T, kratosURL string, cut SessionCutoffReader) laneVerdict {
	t.Helper()
	served := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { served = true })
	a := NewAuthInterceptor(AuthModeDev, "",
		cutoffLookup{subj: Subject{Type: "user", ID: "usr-1", DisplayName: "A"}},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	).WithKratos(NewKratosClient(kratosURL))
	if cut != nil {
		a = a.WithSessionCutoffCheck(cut, time.Hour)
	}
	req := httptest.NewRequest(http.MethodGet, "/vpc/v1/networks", nil)
	req.Header.Set("Cookie", "ory_kratos_session="+t.Name()+"-identity")
	rec := httptest.NewRecorder()
	a.HTTP(next).ServeHTTP(rec, req)
	return laneVerdict{name: "полоса личности на пути запроса", signed: served}
}

// askWhoAmILane — вердикт маршрута «кто я»: назвал ли он человека.
func askWhoAmILane(t *testing.T, kratosURL string, cut SessionCutoffReader) laneVerdict {
	t.Helper()
	h := NewSessionIdentityHandler(slog.New(slog.NewTextHandler(io.Discard, nil))).
		WithKratos(NewKratosClient(kratosURL),
			cutoffLookup{subj: Subject{Type: "user", ID: "usr-1", DisplayName: "A"}})
	if cut != nil {
		h = h.WithSessionCutoff(cut)
	}
	req := httptest.NewRequest(http.MethodGet, "/iam/v1/auth/me", nil)
	req.Header.Set("Cookie", "ory_kratos_session="+t.Name()+"-whoami")
	rec := httptest.NewRecorder()
	h.Me(rec, req)

	var body struct {
		User map[string]any `json:"user"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return laneVerdict{name: "маршрут «кто я»", signed: body.User != nil}
}

// TestBrowserSessionLanesAgree — обе полосы обязаны отвечать про одну сессию
// ОДИНАКОВО, на каждом из состояний отсечки.
//
// Перепись печатает ДВЕ величины — сколько полос осмотрено и сколько из них
// несут свойство. Одно число скрыло бы ровно тот случай, ради которого проба
// заведена: «полос 2» без «сошлись 2» не отличает согласие от того, что вторую
// полосу просто не спросили.
func TestBrowserSessionLanesAgree(t *testing.T) {
	authAt := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name       string
		cut        SessionCutoffReader
		wantSigned bool
		why        string
	}{
		{
			name:       "сессия старше отсечки",
			cut:        &fakeCutoff{cutoff: authAt.Add(time.Hour), found: true},
			wantSigned: false,
			why:        "выведен нашим глаголом — вошедшим не считается ни одной полосой",
		},
		{
			name:       "сессия моложе отсечки",
			cut:        &fakeCutoff{cutoff: authAt.Add(-time.Hour), found: true},
			wantSigned: true,
			why:        "вошёл заново — отсечка действует вперёд",
		},
		{
			name:       "отсечки нет",
			cut:        &fakeCutoff{found: false},
			wantSigned: true,
			why:        "человека никто не отзывал",
		},
		{
			name:       "авторитет не ответил",
			cut:        &fakeCutoff{err: errors.New("unreachable")},
			wantSigned: false,
			why:        "авторитет наш; мягкий проход означал бы «отзываем и свой же отзыв не исполняем»",
		},
		{
			name:       "авторитет не предлагает такого вопроса (окно раската)",
			cut:        &fakeCutoff{err: ErrSessionCutoffUnsupported},
			wantSigned: true,
			why:        "раскат не атомарен; отказ здесь уронил бы консоль на всё окно, а состояние сходится само",
		},
		{
			name:       "сессия без момента аутентификации при живой отсечке",
			cut:        &fakeCutoff{cutoff: authAt, found: true},
			wantSigned: false,
			why:        "доказать непревышение отсечки нечем",
		},
	}

	lanesSeen, lanesAgreed := 0, 0
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			at := authAt
			if tc.name == "сессия без момента аутентификации при живой отсечке" {
				at = time.Time{}
			}
			url := kratosStub(t, at).URL

			verdicts := []laneVerdict{
				askIdentityLane(t, url, tc.cut),
				askWhoAmILane(t, url, tc.cut),
			}
			for _, v := range verdicts {
				lanesSeen++
				if v.signed != tc.wantSigned {
					t.Errorf("%s: считает вошедшим=%v, обе полосы обязаны отвечать %v — %s",
						v.name, v.signed, tc.wantSigned, tc.why)
					continue
				}
				lanesAgreed++
			}
		})
	}

	t.Logf("перепись: полос осмотрено %d · сошлись с ожидаемым %d", lanesSeen, lanesAgreed)
	// Предпосылка пробы: она обязана ОТКАЗЫВАТЬ на беспредметности. Ноль
	// осмотренных полос снаружи неотличим от «расхождений нет».
	if lanesSeen == 0 {
		t.Fatal("осмотрено ноль полос — проба ничего не сравнивала, и её молчание ничего не значит")
	}
}

// TestBrowserSessionLanesAgree_UnmountedReaderIsAlsoSymmetric — граница названа
// вслух: полоса без провязанного читателя работает как прежде, и это тоже
// одинаково на обеих. Иначе «одна полоса спрашивает, вторая нет» вернулось бы
// через непровязку.
func TestBrowserSessionLanesAgree_UnmountedReaderIsAlsoSymmetric(t *testing.T) {
	url := kratosStub(t, time.Now()).URL
	a := askIdentityLane(t, url, nil)
	m := askWhoAmILane(t, url, nil)
	if !a.signed || !m.signed {
		t.Fatalf("без читателя обе полосы обязаны работать как прежде: %s=%v, %s=%v",
			a.name, a.signed, m.name, m.signed)
	}
	t.Log("перепись: полос осмотрено 2 · сошлись с ожидаемым 2 (читатель не провязан)")
}

// ctxUnused держит импорт context значимым для читателя: порт объявлен на нём, и
// подставные читатели выше его исполняют.
var _ = context.Background
