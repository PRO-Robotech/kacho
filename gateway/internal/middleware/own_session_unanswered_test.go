// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// own_session_unanswered_test.go — служба не ответила краю, а запрос пришёл
// на глагол формы: приёмка Ф12-38 (половина полосы) и закрытый перечень
// глаголов, на которых недоступность ретранслируется (Ф3 Р7, Ф3-17).
//
// # Почему перечень ПРОПУСКА, а не перечень отказа
//
// Отказ F4d-23 полагается глаголу, который читает носитель: он меняет либо
// читает состояние ПОД сессией, чью отсечку установить не удалось (Ф3 Р7 о
// смене пароля; Ф12 Р4 о шести глаголах второго фактора: «чтение состояния
// сюда же — оно читает эту сессию»). Ретрансляция полагается глаголу, который
// носителя не читает (вход, признак формы — Ф3 Р7 «вход и признак носителя не
// читают»; регистрация и оба глагола восстановления ключуются адресом и кодом),
// и выходу, который сессию оканчивает (Ф3-17). Прежняя форма была перечнем
// исключений — «отказ только на смене пароля», — и шесть глаголов Ф12, дописанные
// в объявление, молча попали в «ретранслировать». Отказ — умолчание: глагол,
// дописанный без решения, получает F4d-23 (синтетика ниже).
//
// Все пробы идут ЧЕРЕЗ ЦЕПОЧКУ `AuthInterceptor.HTTP(mux)` с маршрутами,
// зарегистрированными по объявлению, — как у соседних проб полосы.
package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
)

// unansweredPassVerbs — записи объявления, на которых недоступность службы
// РЕТРАНСЛИРУЕТСЯ. Выписаны дословно из приёмок, а не прочитаны из объявления:
// проба обязана краснеть на объявлении, которое решило иначе.
//
//   - login, csrf, logout — Ф3 Р7: «На выходе, входе и признаке недоступность
//     ретранслируется: ни один из трёх под сессией носителя ничего не меняет»;
//     Ф3-17 — «через край», ретранслировано 1 на выходе, вход и признак — так же;
//   - register, recovery, recovery-complete — носителя не читают: регистрация
//     заводит новую личность, запрос кода и его предъявление ключуются адресом
//     и кодом (Ф5 Р5 — «одно обращение … с email, code, newPassword»); критерий
//     Ф3 Р7 «вход и признак носителя не читают» применён к ним без изменений;
//   - authorize, token — две координаты церемонии авторизации (замысел LINE-A-1
//     §5.1б п. 4, решение по КАЖДОЙ координате): навигация носителя не читает —
//     вопрос о сессии решает сама церемония своим швом; обмен кода носителя не
//     читает вовсе — решает по коду и удостоверению клиента. Отказ края на их
//     месте был бы вторым решением о том же предмете.
//
// Смена пароля и шесть глаголов второго фактора в перечне отсутствуют: Ф3 Р7
// (смена пароля — F4d-23) и Ф12 Р4 («недоступность службы и отсечка на всех
// шести — как на смене пароля»).
var unansweredPassVerbs = map[string]bool{
	"login":             true,
	"csrf":              true,
	"logout":            true,
	"register":          true,
	"recovery":          true,
	"recovery-complete": true,
	"authorize":         true,
	"token":             true,
}

// secondFactorPaths — шесть глаголов Ф12 Р4.
var secondFactorPaths = []string{
	LoginLanePathSecondFactor,
	LoginLanePathSecondFactorEnroll,
	LoginLanePathSecondFactorConfirm,
	LoginLanePathSecondFactorRemove,
	LoginLanePathSecondFactorBackupCodes,
	LoginLanePathStepUp,
}

// carrierBook — дублёр `Resolve`, различающий носители по значению печенья:
// у Ф12-38 их три, и один общий ответ на все их бы не различил. Недоступность —
// один ответ на всякий носитель, как у настоящей службы.
type carrierBook struct {
	sessions map[string]HumanSession
	err      error
	asked    int
}

func (b *carrierBook) ResolveHumanSession(_ context.Context, bearer string) (HumanSession, bool, error) {
	b.asked++
	if b.err != nil {
		return HumanSession{}, false, b.err
	}
	s, ok := b.sessions[bearer]
	return s, ok, nil
}

// cutoffBook — дублёр `SessionCutoffOf` по субъекту.
type cutoffBook struct {
	cutoffs map[string]time.Time
	err     error
	asked   int
}

func (c *cutoffBook) SessionCutoffOf(_ context.Context, userID string) (time.Time, bool, error) {
	c.asked++
	if c.err != nil {
		return time.Time{}, false, c.err
	}
	at, ok := c.cutoffs[userID]
	return at, ok, nil
}

// Носители Ф12-38: `A` — живая сессия; `B` — сессия, покрытая отсечкой
// принудительного выхода; `C` — снятая выходом (служба её не знает).
const (
	carrierA = "f12-a-live"
	carrierB = "f12-b-cut"
	carrierC = "f12-c-gone"

	userA = "usr-f12-a"
	userB = "usr-f12-b"

	// foreignUser — чужой субъект, которого клиент называет заголовками
	// пространства `x-kacho-`.
	foreignUser = "usr-foreign-9"
)

func f12Books() (*carrierBook, *cutoffBook) {
	a := liveOwnSession()
	a.UserID = userA
	b := liveOwnSession()
	b.UserID = userB
	return &carrierBook{sessions: map[string]HumanSession{carrierA: a, carrierB: b}},
		&cutoffBook{cutoffs: map[string]time.Time{userB: b.AuthenticatedAt}} // отсечка НЕ ниже момента
}

// unansweredMode — режим службы: отвечает; `Resolve` недоступен; отсечка не
// установлена (вопрос 2 без ответа).
type unansweredMode struct {
	name string
	set  func(*carrierBook, *cutoffBook)
}

var serviceAnswers = unansweredMode{"служба отвечает", func(*carrierBook, *cutoffBook) {}}

var unansweredModes = []unansweredMode{
	{"Resolve недоступен", func(b *carrierBook, _ *cutoffBook) {
		b.err = errors.New("rpc error: code = Unavailable desc = down")
	}},
	{"отсечка не установлена", func(_ *carrierBook, c *cutoffBook) {
		c.err = errors.New("rpc error: code = DeadlineExceeded desc = unanswered")
	}},
}

// formChain — край под `own`: полоса личности и следующее звено, считающее
// ретрансляции, на каждом пути объявления.
func formChain(t *testing.T, book *carrierBook, cut *cutoffBook) (http.Handler, *countingNext) {
	t.Helper()
	relay := &countingNext{}
	mux := http.NewServeMux()
	for _, rt := range LoginLaneRoutes() {
		mux.Handle(rt.Path, relay)
	}
	return ownLane(t, book, cut).HTTP(mux), relay
}

func formVerbRequest(path, carrier string, foreign bool) *http.Request {
	method := http.MethodPost
	if path == LoginLanePathSecondFactor || path == LoginLanePathCSRF || path == CeremonyPathAuthorize {
		method = http.MethodGet
	}
	req := withOurCarrier(httptest.NewRequest(method, path, strings.NewReader(`{}`)), carrier)
	if foreign {
		// Обе формы написания: голая и приставкой моста.
		for _, name := range []string{principalmeta.HeaderPrincipalType, principalmeta.HeaderPrincipalID, principalmeta.HeaderPrincipalDisplay} {
			value := foreignUser
			if name == principalmeta.HeaderPrincipalType {
				value = "user"
			}
			req.Header.Set(name, value)
			req.Header.Set(principalmeta.BridgePrefix+name, value)
		}
	}
	return req
}

// laneOutcome — наблюдаемое: что получил клиент и что получило следующее звено.
type laneOutcome struct {
	code      int
	body      string
	setCookie string
	relayed   int
	principal string
	foreign   string // заголовки ретранслированного запроса, несущие чужого субъекта
}

func runFormVerb(chain http.Handler, relay *countingNext, req *http.Request) laneOutcome {
	before := relay.served
	relay.lastReq = nil
	rec := serve(chain, req)
	out := laneOutcome{
		code:      rec.Code,
		body:      rec.Body.String(),
		setCookie: strings.Join(rec.Result().Header["Set-Cookie"], "\n"),
		relayed:   relay.served - before,
	}
	if relay.lastReq != nil {
		out.principal = relay.lastReq.Header.Get(principalmeta.HeaderPrincipalID)
		var carrying []string
		for name, values := range relay.lastReq.Header {
			for _, v := range values {
				if strings.Contains(v, foreignUser) {
					carrying = append(carrying, name)
				}
			}
		}
		sort.Strings(carrying)
		out.foreign = strings.Join(carrying, ",")
	}
	return out
}

// denyBody — тело F4d-22, произведённое цепочкой на носителе `B`: эталон, с
// которым F4d-23 обязан совпасть побайтово (тот же код и тот же текст).
func denyBody(t *testing.T) string {
	t.Helper()
	book, cut := f12Books()
	chain, _ := formChain(t, book, cut)
	rec := serve(chain, withOurCarrier(httptest.NewRequest(http.MethodGet, platformPath, nil), carrierB))
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), sessionCutoffDenyDescription) {
		t.Fatalf("эталон F4d-22 не получен: %d %s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

func expectRefusedKept(t *testing.T, where string, got laneOutcome, deny string) {
	t.Helper()
	if got.code != http.StatusUnauthorized || got.body != deny {
		t.Errorf("%s: ожидался F4d-23 — 401 текстом отказа по отсечке; получено %d %q", where, got.code, got.body)
	}
	if got.setCookie != "" {
		t.Errorf("%s: на F4d-23 носитель обязан остаться целым, Set-Cookie: %q", where, got.setCookie)
	}
	if got.relayed != 0 {
		t.Errorf("%s: ретранслировано %d, ожидалось 0 — запрос с носителем, чью отсечку установить не удалось, до службы не доходит", where, got.relayed)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Ф12-38 — край ретранслирует шесть глаголов через полосу сессии;
// недоступность и отсечка — как на смене пароля.

func TestOwnSessionLane_F12_38_SecondFactorVerbsThroughTheSessionLane(t *testing.T) {
	deny := denyBody(t)

	type want int
	const (
		relayedAsA want = iota // ретранслировано, личность `A` выставлена
		relayedAnonymous
		refusedEnded // F4d-22, носитель гасится
		refusedKept  // F4d-23, носитель цел
	)
	// cutoffAsked — задан ли вопрос об отсечке. Порядок вопросов несущий:
	// отсечку спрашивают только о сессии, которую `Resolve` назвал, — не
	// ответил он либо ответил «сессии нет», второго вопроса нет.
	cases := []struct {
		mode        unansweredMode
		carrier     string
		want        want
		cutoffAsked bool
	}{
		{serviceAnswers, carrierA, relayedAsA, true},
		{serviceAnswers, carrierB, refusedEnded, true},
		{serviceAnswers, carrierC, relayedAnonymous, false},
		{unansweredModes[0], carrierA, refusedKept, false},
		{unansweredModes[0], carrierB, refusedKept, false},
		{unansweredModes[0], carrierC, refusedKept, false},
		{unansweredModes[1], carrierA, refusedKept, true},
		{unansweredModes[1], carrierB, refusedKept, true},
		// «Сессии нет» отвечено службой до вопроса об отсечке — судит служба.
		{unansweredModes[1], carrierC, relayedAnonymous, false},
	}

	relayedTotal := map[string]int{}
	for _, tc := range cases {
		book, cut := f12Books()
		tc.mode.set(book, cut)
		chain, relay := formChain(t, book, cut)
		for _, path := range secondFactorPaths {
			where := tc.mode.name + " · носитель " + tc.carrier + " · " + path
			got := runFormVerb(chain, relay, formVerbRequest(path, tc.carrier, false))
			relayedTotal[tc.mode.name+" · "+tc.carrier] += got.relayed

			switch tc.want {
			case relayedAsA, relayedAnonymous:
				if got.relayed != 1 || got.code != http.StatusOK || got.setCookie != "" {
					t.Errorf("%s: ожидалась ретрансляция без гашения носителя; получено %d, ретранслировано %d, Set-Cookie %q", where, got.code, got.relayed, got.setCookie)
				}
				wantPrincipal := ""
				if tc.want == relayedAsA {
					wantPrincipal = userA
				}
				if got.principal != wantPrincipal {
					t.Errorf("%s: личность на ретранслированном запросе %q, ожидалась %q", where, got.principal, wantPrincipal)
				}
			case refusedEnded:
				if got.code != http.StatusUnauthorized || got.body != deny || !strings.Contains(got.setCookie, OurSessionCarrierName+"=") || got.relayed != 0 {
					t.Errorf("%s: ожидался F4d-22 с гашением носителя и ретранслировано 0; получено %d %q, Set-Cookie %q, ретранслировано %d", where, got.code, got.body, got.setCookie, got.relayed)
				}
			case refusedKept:
				expectRefusedKept(t, where, got, deny)
			}

			// Чужие заголовки: исход побайтово равен исходу без них, и до
			// следующего звена чужой субъект не доезжает ни в одной форме.
			withForeign := runFormVerb(chain, relay, formVerbRequest(path, tc.carrier, true))
			if withForeign.foreign != "" {
				t.Errorf("%s: ретранслированный запрос несёт чужого субъекта в заголовках %s", where, withForeign.foreign)
			}
			withForeign.foreign = ""
			if withForeign != got {
				t.Errorf("%s: чужие заголовки x-kacho- изменили исход:\n без них %+v\n с ними  %+v", where, got, withForeign)
			}
		}

		// Два запроса на путь — без чужих заголовков и с ними. `Resolve`
		// спрашивается на каждом ровно раз; отсечка — только там, где строка
		// таблицы это обещает.
		requests := 2 * len(secondFactorPaths)
		if book.asked != requests {
			t.Errorf("%s · носитель %s: Resolve спрошен %d раз, ожидалось %d — по одному на запрос", tc.mode.name, tc.carrier, book.asked, requests)
		}
		wantCutoffAsked := 0
		if tc.cutoffAsked {
			wantCutoffAsked = requests
		}
		if cut.asked != wantCutoffAsked {
			t.Errorf("%s · носитель %s: вопрос об отсечке задан %d раз, ожидалось %d", tc.mode.name, tc.carrier, cut.asked, wantCutoffAsked)
		}
	}

	// Положительный контроль различимости (Ф12-38 «And», Ф3-17): при тех же
	// дублёрах выход ретранслируется, смена пароля получает F4d-23.
	for _, mode := range unansweredModes {
		book, cut := f12Books()
		mode.set(book, cut)
		chain, relay := formChain(t, book, cut)
		logout := runFormVerb(chain, relay, formVerbRequest(LoginLanePathLogout, carrierA, false))
		if logout.relayed != 1 {
			t.Errorf("%s: выход обязан ретранслироваться (Ф3-17), ретранслировано %d, получено %d %q", mode.name, logout.relayed, logout.code, logout.body)
		}
		expectRefusedKept(t, mode.name+" · смена пароля", runFormVerb(chain, relay, formVerbRequest(LoginLanePathPassword, carrierA, false)), deny)
	}

	keys := make([]string, 0, len(relayedTotal))
	for k := range relayedTotal {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		t.Logf("перепись: %s — глаголов Ф12 %d · ретранслировано %d", k, len(secondFactorPaths), relayedTotal[k])
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Отказ — умолчание: на КАЖДОМ глаголе объявления при недоступности службы
// F4d-23, кроме закрытого перечня пропуска.

func TestOwnSessionLane_UnansweredServiceRefusesEveryFormVerbOutsideThePassList(t *testing.T) {
	deny := denyBody(t)
	routes := LoginLaneRoutes()

	// Перечень пропуска не несёт мёртвых записей: каждое имя — глагол объявления.
	declared := map[string]bool{}
	for _, rt := range routes {
		declared[rt.Verb] = true
	}
	for verb := range unansweredPassVerbs {
		if !declared[verb] {
			t.Errorf("глагол пропуска %q в объявлении отсутствует — запись пережила свой предмет", verb)
		}
	}

	for _, mode := range unansweredModes {
		book, cut := f12Books()
		mode.set(book, cut)
		chain, relay := formChain(t, book, cut)
		relayed, refused := 0, 0
		for _, rt := range routes {
			got := runFormVerb(chain, relay, formVerbRequest(rt.Path, carrierA, false))
			relayed += got.relayed
			if got.code == http.StatusUnauthorized && got.body == deny && got.setCookie == "" && got.relayed == 0 {
				refused++
			}
			where := mode.name + " · " + rt.Verb
			if unansweredPassVerbs[rt.Verb] {
				if got.relayed != 1 {
					t.Errorf("%s: глагол перечня пропуска обязан ретранслироваться, ретранслировано %d, получено %d %q", where, got.relayed, got.code, got.body)
				}
				continue
			}
			expectRefusedKept(t, where, got, deny)
		}
		t.Logf("перепись: %s — глаголов формы %d · ретранслировано %d (по перечню %d) · отказов F4d-23 %d (по перечню %d)",
			mode.name, len(routes), relayed, len(unansweredPassVerbs), refused, len(routes)-len(unansweredPassVerbs))
	}
}

// Глагол, дописанный в объявление без решения о недоступности, получает
// F4d-23. Законный близнец — тот же путь при ответившей службе и носителе без
// сессии: ретранслируется, то есть путь действительно стоит в объявлении и
// отказ на недоступности — не отказ пути платформы.
func TestOwnSessionLane_UnansweredServiceRefusesAFormVerbAddedWithoutADecision(t *testing.T) {
	const futurePath = "/iam/v1/auth/future-verb"
	// Проба подменяет переменную пакета и потому НЕ может идти `t.Parallel`:
	// параллельная ей проба читала бы перечень во время подмены — гонка по
	// `loginLaneRoutes`, а не вердикт.
	saved := loginLaneRoutes
	loginLaneRoutes = append(append([]LoginLaneRoute(nil), saved...), LoginLaneRoute{Verb: "future-verb", Path: futurePath})
	t.Cleanup(func() { loginLaneRoutes = saved })
	deny := denyBody(t)

	book, cut := f12Books()
	chain, relay := formChain(t, book, cut)
	twin := runFormVerb(chain, relay, formVerbRequest(futurePath, carrierC, false))
	if twin.relayed != 1 || twin.code != http.StatusOK {
		t.Fatalf("близнец: синтетический путь при «сессии нет» обязан ретранслироваться как глагол формы; получено %d, ретранслировано %d", twin.code, twin.relayed)
	}

	for _, mode := range unansweredModes {
		book, cut := f12Books()
		mode.set(book, cut)
		chain, relay := formChain(t, book, cut)
		expectRefusedKept(t, mode.name+" · глагол без решения", runFormVerb(chain, relay, formVerbRequest(futurePath, carrierA, false)), deny)
	}
}
