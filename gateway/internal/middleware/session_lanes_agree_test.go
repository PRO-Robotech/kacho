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
	"strings"
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
//
// ОБЕ ПОЛОСЫ СПРАШИВАЮТСЯ ЧЕРЕЗ ЦЕПОЧКУ `AuthInterceptor.HTTP(mux)` с
// зарегистрированным маршрутом, а не обработчик в изоляции (Д13 приёмки Ф3,
// `kacho#2688`): в боевой провязке «кто я» стоит ЗА полосой личности, и на
// отвергнутой сессии отвечает она — 401 с гашением носителя, — а не обработчик.
// Прежняя редакция звала `Me` напрямую и утверждала о ветке, которая через
// цепочку недостижима. Вердикт «назвал ли человека» читается из тела: 401 тела
// с `user` не несёт.
//
// Читателей носителя ДВА (сессия поставщика и наша, Ф3), и полосы обязаны
// сходиться на каждом: у нашего носителя читатель другой, а свойство то же.
// Заводит читателей множество носителей, а не посадка: при `own,external`
// заведены оба, и чужая сессия читается с моментом открытия окна — эту ось
// судит TestBrowserSessionLanesAgree_OnTheTransitionalWindowAxis.

// cookieSafeName — имя пробы, приведённое к байтам, КОТОРЫЕ БРАУЗЕР МОЖЕТ
// ПРОВЕСТИ в значении печенья (RFC 6265).
//
// Прежде значение бралось из `t.Name()` как есть, а имена здесь русские.
// Такого печенья не существует: разбор отвергает его, клиент Go при отправке
// печатает «dropping invalid bytes», и ни один браузер этих байтов не пошлёт.
// Фикстура была СНИСХОДИТЕЛЬНЕЕ настоящего входа — и держалась на том, что
// предикат присутствия смотрел на подстроку заголовка, а не на разобранное
// печенье. Как только край стал решать по имени, которое провёл браузер,
// фикстура перестала представлять что-либо реальное.
func cookieSafeName(t *testing.T) string {
	var b strings.Builder
	for _, c := range []byte(t.Name()) {
		// Октеты значения печенья по RFC 6265, без кавычки, точки с запятой и
		// обратной косой: ровно то, что примет разбор на принимающей стороне.
		if c > 0x20 && c < 0x7f && c != '"' && c != ';' && c != '\\' && c != ',' {
			b.WriteByte(c)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

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
	req.Header.Set("Cookie", "ory_kratos_session="+cookieSafeName(t)+"-identity")
	rec := httptest.NewRecorder()
	a.HTTP(next).ServeHTTP(rec, req)
	return laneVerdict{name: "полоса личности на пути запроса", signed: served}
}

// askWhoAmILane — вердикт маршрута «кто я» ЧЕРЕЗ ЦЕПОЧКУ: назвал ли ответ
// человека. Полоса личности стоит перед маршрутом, как в боевой провязке.
func askWhoAmILane(t *testing.T, kratosURL string, cut SessionCutoffReader) laneVerdict {
	t.Helper()
	lookup := cutoffLookup{subj: Subject{Type: "user", ID: "usr-1", DisplayName: "A"}}
	a := NewAuthInterceptor(AuthModeDev, "", lookup,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	).WithKratos(NewKratosClient(kratosURL))
	h := NewSessionIdentityHandler(slog.New(slog.NewTextHandler(io.Discard, nil))).
		WithKratos(NewKratosClient(kratosURL), lookup)
	if cut != nil {
		a = a.WithSessionCutoffCheck(cut, time.Hour)
		h = h.WithSessionCutoff(cut)
	}
	mux := http.NewServeMux()
	h.Register(mux)
	req := httptest.NewRequest(http.MethodGet, "/iam/v1/auth/me", nil)
	req.Header.Set("Cookie", "ory_kratos_session="+cookieSafeName(t)+"-whoami")
	rec := httptest.NewRecorder()
	a.HTTP(mux).ServeHTTP(rec, req)
	return laneVerdict{name: "маршрут «кто я»", signed: whoAmINamedAPerson(rec)}
}

// whoAmINamedAPerson читает вердикт «кто я» из ответа цепочки: только 200 с
// непустым `user` называет человека; 401 полосы — нет.
func whoAmINamedAPerson(rec *httptest.ResponseRecorder) bool {
	if rec.Code != http.StatusOK {
		return false
	}
	var body struct {
		User map[string]any `json:"user"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return body.User != nil
}

// askOwnIdentityLane / askOwnWhoAmILane — те же две полосы под посадкой `own`:
// читатель — НАША сессия (Ф3 Р7), носитель — наше печенье.
func askOwnIdentityLane(t *testing.T, sess HumanSession, cut SessionCutoffReader) laneVerdict {
	t.Helper()
	served := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { served = true })
	a := ownLane(t, &fakeHumanSession{found: true, sess: sess}, cut)
	req := httptest.NewRequest(http.MethodGet, "/vpc/v1/networks", nil)
	rec := httptest.NewRecorder()
	a.HTTP(next).ServeHTTP(rec, withOurCarrier(req, t.Name()+"-own-identity"))
	return laneVerdict{name: "полоса личности на пути запроса (own)", signed: served}
}

func askOwnWhoAmILane(t *testing.T, sess HumanSession, cut SessionCutoffReader) laneVerdict {
	t.Helper()
	reader := &fakeHumanSession{found: true, sess: sess}
	a := ownLane(t, reader, cut)
	mux := http.NewServeMux()
	ownWhoAmI(t, mux, reader, cut, nil)
	req := httptest.NewRequest(http.MethodGet, "/iam/v1/auth/me", nil)
	rec := httptest.NewRecorder()
	a.HTTP(mux).ServeHTTP(rec, withOurCarrier(req, t.Name()+"-own-whoami"))
	return laneVerdict{name: "маршрут «кто я» (own)", signed: whoAmINamedAPerson(rec)}
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
			own := liveOwnSession()
			own.AuthenticatedAt = at

			verdicts := []laneVerdict{
				askIdentityLane(t, url, tc.cut),
				askWhoAmILane(t, url, tc.cut),
				askOwnIdentityLane(t, own, tc.cut),
				askOwnWhoAmILane(t, own, tc.cut),
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
	own := liveOwnSession()
	for _, v := range []laneVerdict{
		askIdentityLane(t, url, nil), askWhoAmILane(t, url, nil),
		askOwnIdentityLane(t, own, nil), askOwnWhoAmILane(t, own, nil),
	} {
		if !v.signed {
			t.Fatalf("без читателя отсечки полоса обязана работать как прежде: %s=%v", v.name, v.signed)
		}
	}
	t.Log("перепись: полос осмотрено 4 · сошлись с ожидаемым 4 (читатель не провязан)")
}

// ctxUnused держит импорт context значимым для читателя: порт объявлен на нём, и
// подставные читатели выше его исполняют.
var _ = context.Background

// ─────────────────────────────────────────────────────────────────────────────
// ОСЬ ОКНА: полосы сравниваются и по нему, а не только по отсечке.
//
// Сравнение выше разбирает ШЕСТЬ состояний отсечки и НИ ОДНОГО состояния окна.
// Для отсечки прецедент разобран явно — маршрут «кто я» держит СВОЙ читатель,
// вложенную точку предъявления, и снимать его ради одного вызова запрещено.
// Для окна того же сделано не было: читателей момента два, оба на полосе
// личности, а обработчик «кто я» об окне не знал вовсе.
//
// Это остаток ровно того класса, который аудит нашёл в этой дельте трижды:
// свойство доказано на одной полосе из двух. Через боевую цепочку маршрут
// сегодня закрыт стоящей впереди полосой — находка не в достижимости, а в том,
// что РАВЕНСТВО ПОЛОС ПО ОКНУ никто не проверял.

// windowState — одно состояние оси окна.
type windowState struct {
	name       string
	openedAt   time.Time
	sessionAt  time.Time
	wantSigned bool
	why        string
}

func windowAxisStates() []windowState {
	// Свой момент: соседнее сравнение держит его локальной величиной, и брать
	// его оттуда значило бы связать две таблицы, которые о разном.
	base := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	opened := base.Add(time.Hour)
	return []windowState{
		{
			name: "окна нет — момент не ограничен", openedAt: time.Time{},
			sessionAt: opened.Add(24 * time.Hour), wantSigned: true,
			why: "состояние «только чужой» — сегодняшний стенд, вход заново единственный способ войти",
		},
		{
			name: "сессия СТАРШЕ открытия окна", openedAt: opened,
			sessionAt: opened.Add(-time.Minute), wantSigned: true,
			why: "живые чужие сессии дочитываются — ради этого окно и заводится",
		},
		{
			name: "сессия ЗАВЕДЕНА ПОСЛЕ открытия окна", openedAt: opened,
			sessionAt: opened.Add(time.Minute), wantSigned: false,
			why: "новых не заводим: иначе отзыв снимается входом заново на чужой стороне",
		},
		{
			name: "сессия РОВНО в момент открытия", openedAt: opened,
			sessionAt: opened, wantSigned: false,
			why: "граница закрыта включающе",
		},
		{
			name: "момента аутентификации нет вовсе", openedAt: opened,
			sessionAt: time.Time{}, wantSigned: false,
			why: "доказать старшинство окна нечем, и «неизвестно» означает «не доказано»",
		},
	}
}

// askProviderIdentityLaneWindowed / askProviderWhoAmILaneWindowed — те же две
// полосы, читающие ЧУЖУЮ сессию, но с объявленным окном.
func askProviderIdentityLaneWindowed(t *testing.T, kratosURL string, openedAt time.Time) laneVerdict {
	t.Helper()
	served := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { served = true })
	a := NewAuthInterceptor(AuthModeDev, "",
		cutoffLookup{subj: Subject{Type: "user", ID: "usr-1", DisplayName: "A"}},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	).WithKratos(NewKratosClient(kratosURL)).
		WithSessionCutoffCheck(&fakeCutoff{}, time.Hour).
		WithTransitionalCarrierWindow(openedAt)
	req := httptest.NewRequest(http.MethodGet, "/vpc/v1/networks", nil)
	req.Header.Set("Cookie", "ory_kratos_session="+cookieSafeName(t)+"-identity")
	rec := httptest.NewRecorder()
	a.HTTP(next).ServeHTTP(rec, req)
	return laneVerdict{name: "полоса личности на пути запроса", signed: served}
}

// Маршрут «кто я» спрашивается В ИЗОЛЯЦИИ намеренно, и это не отступление от
// Д13, а его предмет: вопрос здесь не «что увидит человек через боевую цепочку»
// — впереди стоит полоса и отвергнет запрос раньше, — а «держит ли обработчик
// СВОЙ читатель окна». Ровно так разобран прецедент отсечки: вложенная точка
// предъявления, снимать которую ради одного вызова запрещено.
func askProviderWhoAmILaneWindowed(t *testing.T, kratosURL string, openedAt time.Time) laneVerdict {
	t.Helper()
	lookup := cutoffLookup{subj: Subject{Type: "user", ID: "usr-1", DisplayName: "A"}}
	h := NewSessionIdentityHandler(slog.New(slog.NewTextHandler(io.Discard, nil))).
		WithKratos(NewKratosClient(kratosURL), lookup).
		WithSessionCutoff(&fakeCutoff{}).
		WithTransitionalCarrierWindow(openedAt)
	req := httptest.NewRequest(http.MethodGet, "/iam/v1/auth/me", nil)
	req.Header.Set("Cookie", "ory_kratos_session="+cookieSafeName(t)+"-whoami")
	rec := httptest.NewRecorder()
	h.Me(rec, req)
	return laneVerdict{name: "маршрут «кто я» (свой читатель окна)", signed: whoAmINamedAPerson(rec)}
}

func TestBrowserSessionLanesAgree_OnTheTransitionalWindowAxis(t *testing.T) {
	states := windowAxisStates()
	lanesSeen, lanesAgreed := 0, 0
	for _, st := range states {
		t.Run(st.name, func(t *testing.T) {
			url := kratosStub(t, st.sessionAt).URL
			for _, v := range []laneVerdict{
				askProviderIdentityLaneWindowed(t, url, st.openedAt),
				askProviderWhoAmILaneWindowed(t, url, st.openedAt),
			} {
				lanesSeen++
				if v.signed != st.wantSigned {
					t.Errorf("%s: считает вошедшим=%v, обе полосы обязаны отвечать %v — %s",
						v.name, v.signed, st.wantSigned, st.why)
					continue
				}
				lanesAgreed++
			}
		})
	}
	if lanesSeen == 0 {
		t.Fatal("осмотрено ноль полос — проба ничего не сравнивала")
	}
	t.Logf("перепись: состояний оси окна %d · полос, читающих чужую сессию, 2 · осмотрено %d · "+
		"сошлись с ожидаемым %d", len(states), lanesSeen, lanesAgreed)
}
