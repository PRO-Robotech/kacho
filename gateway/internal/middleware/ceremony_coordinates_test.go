// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// ceremony_coordinates_test.go — краевая половина разводки координаты
// `/iam/v1/authorize` (замысел LINE-A-1 §5.1–§5.2, полоса L13; kacho#2817) и
// публикация обнаружения (полоса L8, kacho#2721).
//
// Три координаты церемонии авторизации объявляются на крае ТЕМ ЖЕ родом, что
// глаголы полосы формы (род I, §5.1а): запись в объявлении путей, точным
// совпадением, — и ни одной записи в сгенерированной таблице маршрутов прав и в
// `rest_route_edge.go`. Сосед по имени — четыре глагола проверки доступа
// `/iam/v1/authorize:*` — остаётся за транскодером и не наследует ничего.
package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ceremonyWant — координаты церемонии на крае, выписанные ДОСЛОВНО: навигация на
// эндпоинт авторизации и обмен кода (замысел §5.1, полоса L13) и метаданные
// обнаружения (RFC 8414 §3). Обнаружение замысел оставил полосе L8 «своей
// записью того же рода и своим решением» (§5.1б п. 5); решение принято
// kacho#2721 — запись объявления называет его довод.
var ceremonyWant = map[string]string{
	"authorize": "/iam/v1/authorize",
	"token":     "/iam/v1/token",
	"discovery": "/.well-known/oauth-authorization-server",
}

// authorizeNeighbours — четыре глагола проверки доступа, делящие с церемонией
// корень имени: они обслуживаются транскодером и обязаны остаться за ним.
var authorizeNeighbours = map[string]string{
	"/iam/v1/authorize:check":           "kaname.cloud.iam.v1.AuthorizeService/Check",
	"/iam/v1/authorize:batchCheck":      "kaname.cloud.iam.v1.AuthorizeService/BatchCheck",
	"/iam/v1/authorize:listSubjects":    "kaname.cloud.iam.v1.AuthorizeService/ListSubjects",
	"/iam/v1/authorize:expandRelations": "kaname.cloud.iam.v1.AuthorizeService/ExpandRelations",
}

// TestCeremonyCoordinates_L13_EachIsDeclaredOnceExactlyAndRelayedToTheIssuanceListener —
// §7 инв. 9: записей о каждой координате РОВНО одна, она в объявлении рода I, её
// цель — слушатель выдачи; решение о ретрансляции при неответе принято по КАЖДОЙ
// координате (§5.1б п. 4: обе носителя не читают — ретранслировать).
func TestCeremonyCoordinates_L13_EachIsDeclaredOnceExactlyAndRelayedToTheIssuanceListener(t *testing.T) {
	for verb, path := range ceremonyWant {
		n := 0
		for _, rt := range LoginLaneRoutes() {
			if rt.Path == path {
				n++
				if rt.Verb != verb {
					t.Errorf("%s объявлена под именем %q, ожидалось %q", path, rt.Verb, verb)
				}
				if rt.Target != RelayTargetIssuance {
					t.Errorf("%s ретранслируется на цель %q — церемония живёт целиком на слушателе выдачи (§5.1б п. 2)", path, rt.Target)
				}
			}
		}
		if n != 1 {
			t.Errorf("записей о %s в объявлении %d, ожидалась ровно одна: две записи об одном внешнем имени расходятся молча", path, n)
		}
		rt, ok := LoginLaneRouteFor(path)
		if !ok || rt.Path != path {
			t.Errorf("поиск записи по пути %s не находит её", path)
		}
		if !IsLoginLanePath(path) {
			t.Errorf("%s не узнаётся веткой полосы сессии", path)
		}
		// Состав освобождения — ПОЛНЫЙ, и он решён (§5.1б п. 3): каталог прав,
		// пол уверенности, полоса привязки предъявителя и вопрос об отзыве читают
		// ОДИН предикат.
		if !isPublicHTTPPath(path) {
			t.Errorf("%s не освобождена от решения по каталогу — ретранслируемая без освобождения отвергалась бы каталогом до службы", path)
		}
		if !loginLaneRelaysWhenUnanswered(path) {
			t.Errorf("%s: при неответе службы край отказывал бы сам — решение §5.1б п. 4 «ретранслировать» не исполнено", path)
		}
	}
	t.Logf("перепись: координат церемонии объявлено %d · на слушатель выдачи %d", len(ceremonyWant), len(routesOf(RelayTargetIssuance)))
}

// TestCeremonyCoordinates_L13_NoSecondRecordInTheRouteTables — §5.2, §5.3 п. 2:
// ни в сгенерированной таблице (её источник — аннотации контракта, а контракта
// церемония не заводит), ни в таблице собственных ручек края записи о координатах
// церемонии нет. Сосед `:check` и его три брата остаются за транскодером.
func TestCeremonyCoordinates_L13_NoSecondRecordInTheRouteTables(t *testing.T) {
	tables := map[string][]restRoute{"generatedRestRoutes": generatedRestRoutes, "edgeRestRoutes": edgeRestRoutes}
	examined := 0
	for name, table := range tables {
		if len(table) == 0 {
			t.Fatalf("таблица %s пуста — судить нечего, и это не зелёный", name)
		}
		for _, r := range table {
			examined++
			for _, path := range ceremonyWant {
				if matchTemplate(r.Template, path) {
					t.Errorf("%s несёт запись %s %s → %s о координате церемонии %s — второе место об одном внешнем имени",
						name, r.Method, r.Template, r.FQN, path)
				}
			}
		}
	}
	router := NewRestRouter()
	for _, m := range []string{http.MethodGet, http.MethodPost} {
		for _, path := range ceremonyWant {
			if fqn, ok := router.Resolve(m, path); ok {
				t.Errorf("%s %s резолвится в метод %s — координата церемонии обязана уходить в ретранслятор, а не в AuthorizeService", m, path, fqn)
			}
		}
	}
	// Положительный контроль: соседи остались за транскодером — различение по
	// суффиксу `:verb` держит (ось 1, §5.2), и ни один не стал путём церемонии.
	for path, fqn := range authorizeNeighbours {
		got, ok := router.Resolve(http.MethodPost, path)
		if !ok || got != fqn {
			t.Errorf("POST %s резолвится в %q (ok=%v), ожидалось %s", path, got, ok, fqn)
		}
		if IsLoginLanePath(path) || isPublicHTTPPath(path) {
			t.Errorf("%s признан координатой церемонии либо освобождён — освобождение протекло бы на проверку доступа", path)
		}
	}
	t.Logf("перепись: строк таблиц маршрутов осмотрено %d · записей о координатах церемонии 0 · соседей `:verb` за транскодером %d",
		examined, len(authorizeNeighbours))
}

// TestCeremonyCoordinates_L13_ExactMatchNotPrefix — отрицательные близнецы оси
// 1 (§5.2): запись объявлена ТОЧНЫМ совпадением. Путь, отличающийся одним
// символом, не освобождён и не ретранслируется. У обнаружения соседи — его
// подпути (RFC 8414 §3 вставляет путь издателя ПОСЛЕ имени документа; у нашего
// издателя пути нет, и подпуть — чужой документ) и соседние документы
// `/.well-known/`: освобождение на них не протекает.
func TestCeremonyCoordinates_L13_ExactMatchNotPrefix(t *testing.T) {
	near := []string{
		"/iam/v1/authorize/", "/iam/v1/authorizex", "/iam/v1/authorize/x", "/iam/v1/authoriz",
		"/iam/v1/token/", "/iam/v1/tokens", "/iam/v1/token:introspect", "/iam/v1/token/x",
		"/.well-known/oauth-authorization-server/", "/.well-known/oauth-authorization-serverx",
		"/.well-known/oauth-authorization-server/iam", "/.well-known/oauth-authorization-serve",
		"/.well-known/", "/.well-known/openid-configuration", "/.well-known/jwks.json",
	}
	for _, p := range near {
		if IsLoginLanePath(p) || isPublicHTTPPath(p) {
			t.Errorf("путь %q признан координатой церемонии либо освобождён — совпадение обязано быть точным", p)
		}
		if _, ok := LoginLaneRouteFor(p); ok {
			t.Errorf("поиск записи по пути %q нашёл запись", p)
		}
	}
	t.Logf("перепись: соседних путей %d · признанных координатой 0", len(near))
}

// TestRelayTargets_L13_ClosedSetWithoutDeadTargets — цели ретрансляции — закрытый
// перечень: у каждой записи объявления цель из него, у каждой цели есть записи.
// Цель без записей была бы ретранслятором, которому нечего ретранслировать;
// запись с целью вне перечня — путём, для которого композиционный корень
// ретранслятора не заводит.
func TestRelayTargets_L13_ClosedSetWithoutDeadTargets(t *testing.T) {
	targets := RelayTargets()
	if len(targets) != 2 {
		t.Fatalf("целей ретрансляции %d, ожидалось 2 (слушатель формы · слушатель выдачи, §5.1б п. 2а)", len(targets))
	}
	known := map[RelayTarget]bool{}
	for _, tg := range targets {
		if tg == "" {
			t.Fatal("пустая цель в закрытом перечне")
		}
		if known[tg] {
			t.Fatalf("цель %q объявлена дважды", tg)
		}
		known[tg] = true
		if !tg.Valid() {
			t.Errorf("цель %q перечня не признаёт себя годной", tg)
		}
		if len(routesOf(tg)) == 0 {
			t.Errorf("у цели %q нет ни одной записи объявления — мёртвая цель", tg)
		}
	}
	for _, rt := range LoginLaneRoutes() {
		if !known[rt.Target] {
			t.Errorf("запись %q несёт цель %q вне закрытого перечня", rt.Verb, rt.Target)
		}
	}
	// Отрицательный контроль: нулевое значение и чужое слово годной целью не
	// являются — запись, дописанная без решения о цели, не ретранслируется никуда.
	for _, tg := range []RelayTarget{"", "foreign", "Form"} {
		if tg.Valid() {
			t.Errorf("цель %q признана годной", tg)
		}
	}
	t.Logf("перепись: целей %d · записей формы %d · записей выдачи %d", len(targets), len(routesOf(RelayTargetForm)), len(routesOf(RelayTargetIssuance)))
}

// TestCeremonyCoordinates_L13_SessionLaneRelaysNoSessionAndStillRefusesTheCutOff —
// §5.1б п. 4: членство координаты в объявлении несущее. Вне объявления исход
// «сессии нет» гасил бы печенья человека и отвечал 401 — браузер, пришедший на
// церемонию с просроченной сессией, выходил бы из системы вместо входа. Отсечка
// сессии отвергается КАК ВСЮДУ: ретрансляцией она не снимается.
func TestCeremonyCoordinates_L13_SessionLaneRelaysNoSessionAndStillRefusesTheCutOff(t *testing.T) {
	requests := map[string]func(carrier string) *http.Request{
		"authorize": func(c string) *http.Request {
			return withOurCarrier(httptest.NewRequest(http.MethodGet,
				"/iam/v1/authorize?response_type=code&client_id=console&state=s-0123456789abcdef&code_challenge=x&code_challenge_method=S256", nil), c)
		},
		"token": func(c string) *http.Request {
			req := withOurCarrier(httptest.NewRequest(http.MethodPost, "/iam/v1/token",
				strings.NewReader("grant_type=authorization_code&code=c&code_verifier=v")), c)
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			return req
		},
		"discovery": func(c string) *http.Request {
			return withOurCarrier(httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil), c)
		},
	}
	if len(requests) != len(ceremonyWant) {
		t.Fatalf("запросов пробы %d, координат церемонии %d — координата без пробы полосы сессии", len(requests), len(ceremonyWant))
	}
	for verb, build := range requests {
		// Сессии нет — ретранслируется, носитель не гасится.
		gone := ownLane(t, &fakeHumanSession{found: false}, &fakeCutoff{})
		next := &countingNext{}
		rec := serve(gone.HTTP(muxOverDeclaration(next)), build("s-gone"))
		if next.served != 1 || rec.Code != http.StatusOK {
			t.Errorf("%s, сессии нет: обязан ретранслироваться (исход судит церемония), дошло %d, ответ %d %s", verb, next.served, rec.Code, rec.Body.String())
		}
		if ourCarrierEnded(rec.Result()) {
			t.Errorf("%s, сессии нет: край погасил носитель — человек вышел бы из системы вместо входа", verb)
		}

		// Отсечка — отказ края как всюду, до службы не доходит.
		cut := ownLane(t, &fakeHumanSession{found: true, sess: liveOwnSession()}, &fakeCutoff{found: true, cutoff: ownAuthAt})
		next = &countingNext{}
		rec = serve(cut.HTTP(muxOverDeclaration(next)), build("s-cut"))
		if rec.Code != http.StatusUnauthorized || next.served != 0 || !ourCarrierEnded(rec.Result()) {
			t.Errorf("%s, отсечённая сессия: обязан быть отказ края с гашением носителя, получено %d, дошло %d", verb, rec.Code, next.served)
		}
	}
}

// routesOf — записи объявления с данной целью.
func routesOf(target RelayTarget) []LoginLaneRoute {
	var out []LoginLaneRoute
	for _, rt := range LoginLaneRoutes() {
		if rt.Target == target {
			out = append(out, rt)
		}
	}
	return out
}

// muxOverDeclaration — мультиплексор, крепящий следующее звено на каждую запись
// объявления (форма композиционного корня, без ретранслятора).
func muxOverDeclaration(next http.Handler) *http.ServeMux {
	mux := http.NewServeMux()
	for _, rt := range LoginLaneRoutes() {
		mux.Handle(rt.Path, next)
	}
	return mux
}
