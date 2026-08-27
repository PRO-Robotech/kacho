// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionstream_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	subscriptionv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"

	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
	"github.com/PRO-Robotech/kacho/gateway/internal/subscriptionstream"
)

// TestOpenedComesFirstAndCarriesItsPosition — служебное сообщение открытия
// приходит первым кадром и несёт позицию полем `id:`.
//
// Позиция в `id:` — это ВЕСЬ механизм возобновления: браузер запоминает
// последнее значение сам и возвращает его заголовком. Кадр без `id:` оставил бы
// клиента без того, с чего продолжить, и разрыв терял бы события молча.
func TestOpenedComesFirstAndCarriesItsPosition(t *testing.T) {
	owner := &ownerStub{script: []*subscriptionv1.SubscriptionMessage{
		openedMessage("pos-0", true),
		eventMessage("pos-1", "compute.placement_group", "pg-1"),
	}}
	h := newHandler(t, owner)

	rec := serve(t, h, request("owner=probe"))

	if rec.Code != http.StatusOK {
		t.Fatalf("код ответа %d, тело %q — поток обязан открыться", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("Content-Type %q — браузер разбирает поток только по этому типу", ct)
	}
	if buf := rec.Header().Get("X-Accel-Buffering"); buf != "no" {
		t.Errorf("X-Accel-Buffering %q — без него посредник копит ответ и отдаёт целиком в конце, "+
			"то есть поток перестаёт быть потоком, ничем себя не выдав", buf)
	}

	got := frames(t, rec.Body.String())
	if len(got) != 2 {
		t.Fatalf("кадров %d, ожидалось 2: %q", len(got), rec.Body.String())
	}
	if got[0].event != "opened" {
		t.Errorf("первый кадр — %q; служебное сообщение открытия приходит ПЕРВЫМ и ВСЕГДА", got[0].event)
	}
	if got[0].id != "pos-0" {
		t.Errorf("позиция открытия %q, ожидалась pos-0", got[0].id)
	}
	if got[1].event != "event" || got[1].id != "pos-1" {
		t.Errorf("второй кадр %+v — событие обязано нести СВОЮ позицию", got[1])
	}
	if !strings.Contains(got[1].data, "pg-1") {
		t.Errorf("тело события не несёт предмета: %q", got[1].data)
	}
}

// TestLastEventIdBecomesTheSubscriptionPosition — НЕСУЩЕЕ утверждение задачи:
// возобновление по курсору работает ЧЕРЕЗ край.
//
// Позиция, названная браузером стандартным заголовком, доезжает до владельца
// ветвью позиции — дословно, без разбора и без второго кодека курсора.
// Проверяется по ЗАПРОСУ, который владелец получил: это и есть предмет, а не
// то, что край его куда-то положил.
func TestLastEventIdBecomesTheSubscriptionPosition(t *testing.T) {
	owner := &ownerStub{script: []*subscriptionv1.SubscriptionMessage{openedMessage("pos-9", false)}}
	h := newHandler(t, owner)

	serve(t, h, request("owner=probe", "Last-Event-ID", "cursor-from-browser"))

	position, ok := owner.gotRequest.GetStart().(*subscriptionv1.SubscriptionRequest_Position)
	if !ok {
		t.Fatalf("владелец получил начало %T, ожидалась ВЕТВЬ ПОЗИЦИИ — иначе разрыв соединения "+
			"начинает поток заново и теряет всё, что произошло за время разрыва", owner.gotRequest.GetStart())
	}
	if position.Position != "cursor-from-browser" {
		t.Errorf("позиция доехала как %q, а браузер назвал \"cursor-from-browser\": "+
			"позиция непрозрачна и возвращается ДОСЛОВНО", position.Position)
	}
}

// TestLastEventIdOutranksStartAnchor — заголовок сильнее якоря, и это не
// «принято-и-проигнорировано».
//
// Браузер шлёт заголовок на ТОТ ЖЕ адрес, в котором стоит исходный якорь.
// Обратный порядок начинал бы поток заново при каждом переподключении, то есть
// возобновление не работало бы никогда. Клиент при этом не гадает: край отдаёт
// служебное сообщение открытия владельца, а оно несёт принятую позицию.
func TestLastEventIdOutranksStartAnchor(t *testing.T) {
	owner := &ownerStub{script: []*subscriptionv1.SubscriptionMessage{openedMessage("pos-9", false)}}
	h := newHandler(t, owner)

	serve(t, h, request("owner=probe&start=beginning", "Last-Event-ID", "cursor-2"))

	position, ok := owner.gotRequest.GetStart().(*subscriptionv1.SubscriptionRequest_Position)
	if !ok || position.Position != "cursor-2" {
		t.Fatalf("владелец получил %v — при названной позиции якорь не применяется", owner.gotRequest.GetStart())
	}
}

// TestStartAnchorReachesTheOwner — положительный контроль к предыдущей пробе:
// без заголовка якорь доезжает. Без него обе пробы зеленели бы на ручке,
// которая начало не передаёт вовсе.
func TestStartAnchorReachesTheOwner(t *testing.T) {
	for _, tc := range []struct {
		query string
		want  subscriptionv1.SubscriptionAnchor
	}{
		{"owner=probe&start=beginning", subscriptionv1.SubscriptionAnchor_BEGINNING},
		{"owner=probe&start=currentEnd", subscriptionv1.SubscriptionAnchor_CURRENT_END},
	} {
		owner := &ownerStub{script: []*subscriptionv1.SubscriptionMessage{openedMessage("p", false)}}
		serve(t, newHandler(t, owner), request(tc.query))
		anchor, ok := owner.gotRequest.GetStart().(*subscriptionv1.SubscriptionRequest_Anchor)
		if !ok || anchor.Anchor != tc.want {
			t.Errorf("%s: владелец получил %v, ожидался якорь %v", tc.query, owner.gotRequest.GetStart(), tc.want)
		}
	}
}

// TestUnsetStartLeavesTheBranchUnchosen — незаданное начало доезжает как
// НЕВЫБРАННАЯ ветвь, а не как выбранная с нулевым значением.
//
// Умолчание живёт у незаданной ветви, и край не вправе выбирать её за
// вызывающего: выбери он `CURRENT_END` сам, одно и то же намерение выражалось бы
// двумя способами, а владелец потерял бы возможность различить их.
func TestUnsetStartLeavesTheBranchUnchosen(t *testing.T) {
	owner := &ownerStub{script: []*subscriptionv1.SubscriptionMessage{openedMessage("p", false)}}
	serve(t, newHandler(t, owner), request("owner=probe"))
	if start := owner.gotRequest.GetStart(); start != nil {
		t.Errorf("владелец получил начало %v, ожидалась НЕВЫБРАННАЯ ветвь", start)
	}
}

// TestFilterAxesReachTheOwner — три оси доезжают до владельца конъюнкцией.
//
// Это и есть «одна точка проекции»: события разных модулей различает ФИЛЬТР, а
// не отдельная поверхность на каждый модуль.
func TestFilterAxesReachTheOwner(t *testing.T) {
	owner := &ownerStub{script: []*subscriptionv1.SubscriptionMessage{openedMessage("p", false)}}
	serve(t, newHandler(t, owner),
		request("owner=probe&kinds=a&kinds=b&projectId=prj-1&ids=res-1&ids=res-2"))

	got := owner.gotRequest
	if len(got.GetKinds()) != 2 || got.GetKinds()[0] != "a" || got.GetKinds()[1] != "b" {
		t.Errorf("виды доехали как %v", got.GetKinds())
	}
	if got.GetProjectId() != "prj-1" {
		t.Errorf("проект доехал как %q", got.GetProjectId())
	}
	if len(got.GetIds()) != 2 {
		t.Errorf("идентификаторы доехали как %v", got.GetIds())
	}
}

// TestCallerIdentityReachesTheOwner — владелец сужает поток по правам
// ВЫЗЫВАЮЩЕГО, а не края.
//
// Дозванивайся край под своей учёткой — владелец сузил бы поток по правам края,
// и арендатор увидел бы чужое. Это не «желательно», это единственное, что стоит
// между подпиской и утечкой: за этим методом нет пообъектной проверки на крае.
func TestCallerIdentityReachesTheOwner(t *testing.T) {
	owner := &ownerStub{script: []*subscriptionv1.SubscriptionMessage{openedMessage("p", false)}}
	serve(t, newHandler(t, owner), request("owner=probe"))

	if got := owner.gotMD.Get(principalmeta.MetaPrincipalID); len(got) != 1 || got[0] != "usr-probe" {
		t.Fatalf("владелец получил идентификатор вызывающего %v, ожидался usr-probe", got)
	}
	if got := owner.gotMD.Get(principalmeta.MetaPrincipalType); len(got) != 1 || got[0] != "user" {
		t.Errorf("владелец получил тип вызывающего %v", got)
	}
}

// TestAnonymousCallerIsRefused — безымянный вызывающий не открывает поток.
//
// Рядом — положительный контроль (названный вызывающий проходит): отрицание без
// него зеленело бы на ручке, отвергающей вообще всё.
func TestAnonymousCallerIsRefused(t *testing.T) {
	owner := &ownerStub{script: []*subscriptionv1.SubscriptionMessage{openedMessage("p", false)}}
	h := newHandler(t, owner)

	anonymous := request("owner=probe")
	anonymous.Header.Del(principalmeta.HeaderPrincipalID)
	if rec := serve(t, h, anonymous); rec.Code != http.StatusUnauthorized {
		t.Errorf("безымянный получил %d, ожидался 401", rec.Code)
	}
	if rec := serve(t, h, request("owner=probe")); rec.Code != http.StatusOK {
		t.Errorf("названный вызывающий получил %d — положительный контроль обязан проходить", rec.Code)
	}
}

// TestUnknownOwnerIsRefusedByName — словарь владельцев ЗАКРЫТ.
//
// Открытый параметр отдал бы выбор адреса дозвона вызывающему. Отказ называет
// поле и значение — вызывающий обязан понять, что править, — но НЕ перечисляет
// известных: перечень есть свойство посадки, и оглашать его отказом значит
// отвечать на вопрос, которого никто не задавал.
func TestUnknownOwnerIsRefusedByName(t *testing.T) {
	owner := &ownerStub{script: []*subscriptionv1.SubscriptionMessage{openedMessage("p", false)}}
	h := newHandler(t, owner)

	rec := serve(t, h, request("owner=elsewhere"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("неизвестный владелец получил %d, ожидался 400", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "owner") || !strings.Contains(body, "elsewhere") {
		t.Errorf("отказ %q не называет ни поля, ни значения", body)
	}
	if strings.Contains(body, "probe") {
		t.Errorf("отказ %q огласил перечень известных владельцев", body)
	}
	if rec := serve(t, h, request("owner=probe")); rec.Code != http.StatusOK {
		t.Errorf("известный владелец получил %d — положительный контроль обязан проходить", rec.Code)
	}
}

// TestOwnerIsRequired — владельца называет вызывающий: у владельцев независимые
// счётчики, поэтому одной позицией их не адресовать.
func TestOwnerIsRequired(t *testing.T) {
	h := newHandler(t, &ownerStub{script: []*subscriptionv1.SubscriptionMessage{openedMessage("p", false)}})
	rec := serve(t, h, request(""))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "owner") {
		t.Errorf("отсутствующий владелец: %d %q", rec.Code, rec.Body.String())
	}
}

// TestUnknownStartValueIsRefused — негодное начало отвергается с названным
// значением, а не принимается «как похожее».
func TestUnknownStartValueIsRefused(t *testing.T) {
	h := newHandler(t, &ownerStub{script: []*subscriptionv1.SubscriptionMessage{openedMessage("p", false)}})
	rec := serve(t, h, request("owner=probe&start=whenever"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("негодное начало получило %d, ожидался 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "whenever") {
		t.Errorf("отказ %q не называет отвергнутого значения", rec.Body.String())
	}
}

// TestEmptyOwnerDictionaryIsNotImplemented — пустой словарь означает «владелец
// не объявлен», а не «все домены».
//
// Отказ отличается от «неизвестный владелец» намеренно: вызывающий не виноват в
// том, что посадка владельца не объявила, и `400` послал бы его править запрос,
// который править не в чем.
func TestEmptyOwnerDictionaryIsNotImplemented(t *testing.T) {
	h, err := subscriptionstream.NewHandler(subscriptionstream.Config{
		StreamBudget: time.Second, Heartbeat: 100 * time.Millisecond, MaxStreams: 1,
	})
	if err != nil {
		t.Fatalf("сборка ручки без владельцев обязана проходить: %v", err)
	}
	rec := serve(t, h, request("owner=probe"))
	if rec.Code != http.StatusNotImplemented {
		t.Errorf("пустой словарь ответил %d, ожидался 501", rec.Code)
	}
}

// TestOwnerRefusalBecomesTheResponseCode — отказ владельца становится КОДОМ
// ОТВЕТА, а не кадром в уже открытом потоке.
//
// Ради этого заголовки и пишутся после первого сообщения: код после их отправки
// уже не изменить, и клиент получил бы `200` с ошибкой внутри — то есть успех,
// которого не было.
func TestOwnerRefusalBecomesTheResponseCode(t *testing.T) {
	for _, tc := range []struct {
		code codes.Code
		want int
		msg  string
	}{
		{codes.InvalidArgument, http.StatusBadRequest, "is not a kind of this owner"},
		{codes.NotFound, http.StatusNotFound, "Project prj-1 not found"},
		{codes.OutOfRange, http.StatusBadRequest, "subscription position is no longer resumable"},
		{codes.PermissionDenied, http.StatusForbidden, "subscription requires an authenticated caller"},
		{codes.ResourceExhausted, http.StatusTooManyRequests, "too many concurrent subscriptions"},
		{codes.Unimplemented, http.StatusNotImplemented, ""},
		{codes.Unavailable, http.StatusServiceUnavailable, ""},
	} {
		owner := &ownerStub{failFirst: true, failWith: status.Error(tc.code, tc.msg)}
		rec := serve(t, newHandler(t, owner), request("owner=probe"))
		if rec.Code != tc.want {
			t.Errorf("%v: код ответа %d, ожидался %d (тело %q)", tc.code, rec.Code, tc.want, rec.Body.String())
		}
		if ct := rec.Header().Get("Content-Type"); strings.HasPrefix(ct, "text/event-stream") {
			t.Errorf("%v: отказ отдан как поток — код уже не изменить", tc.code)
		}
		if tc.msg != "" && !strings.Contains(rec.Body.String(), tc.msg) {
			t.Errorf("%v: тон владельца потерян: %q", tc.code, rec.Body.String())
		}
	}
}

// TestInternalRefusalDoesNotEchoTheOwnerText — нутряной отказ не эхает текста
// владельца: он несёт имена схемы, драйвера и адреса.
func TestInternalRefusalDoesNotEchoTheOwnerText(t *testing.T) {
	const leak = "pgx: dial tcp 10.42.0.7:5432 user=kacho_compute"
	owner := &ownerStub{failFirst: true, failWith: status.Error(codes.Internal, leak)}
	rec := serve(t, newHandler(t, owner), request("owner=probe"))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("нутряной отказ получил %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "pgx") || strings.Contains(rec.Body.String(), "5432") {
		t.Fatalf("текст владельца утёк наружу: %q", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "internal error") {
		t.Errorf("нутряной отказ обязан нести фиксированный текст, а не %q", rec.Body.String())
	}
}

// TestPositionLostDetailsReachTheClient — машинные подробности отказа доезжают.
//
// Клиент ключуется на ПРИЗНАК, а не разбирает прозу: узнав, что позиция
// утрачена, он обязан получить и ту, с которой подписка ещё возможна.
func TestPositionLostDetailsReachTheClient(t *testing.T) {
	st := status.New(codes.OutOfRange, "subscription position is no longer resumable")
	withDetails, err := st.WithDetails(&errdetails.ErrorInfo{
		Reason:   "SUBSCRIPTION_POSITION_LOST",
		Domain:   "subscription.kacho.cloud",
		Metadata: map[string]string{"earliest_resumable_position": "pos-earliest"},
	})
	if err != nil {
		t.Fatalf("сборка отказа: %v", err)
	}
	owner := &ownerStub{failFirst: true, failWith: withDetails.Err()}
	rec := serve(t, newHandler(t, owner), request("owner=probe", "Last-Event-ID", "pos-ancient"))

	body := rec.Body.String()
	if !strings.Contains(body, "SUBSCRIPTION_POSITION_LOST") {
		t.Fatalf("признак отказа не доехал до клиента: %q", body)
	}
	if !strings.Contains(body, "pos-earliest") {
		t.Errorf("возобновимая позиция не доехала: %q", body)
	}
}

// TestConcurrentStreamLimitRefusesInsteadOfQueueing — исчерпание отвечает
// ОТКАЗОМ, а не молчаливой очередью.
//
// Очередь превратила бы исчерпание в неограниченное ожидание, неотличимое для
// клиента от «событий нет».
func TestConcurrentStreamLimitRefusesInsteadOfQueueing(t *testing.T) {
	held := &ownerStub{
		script:  []*subscriptionv1.SubscriptionMessage{openedMessage("p", false)},
		hold:    true,
		started: make(chan struct{}),
	}
	h := newHandler(t, held, func(c *subscriptionstream.Config) {
		c.MaxStreams = 1
		c.StreamBudget = 3 * time.Second
		c.Heartbeat = time.Second
	})

	first := make(chan struct{})
	go func() {
		defer close(first)
		serve(t, h, request("owner=probe"))
	}()
	select {
	case <-held.started:
	case <-time.After(10 * time.Second):
		t.Fatal("первый поток не открылся")
	}

	rec := serve(t, h, request("owner=probe"))
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("второй поток при потолке 1 получил %d, ожидался 429 (тело %q)", rec.Code, rec.Body.String())
	}
	<-first
}

// TestHeartbeatKeepsAMuteStreamAlive — молчащий поток шлёт служебные кадры.
//
// Молчание — обычный режим подписки, а посредник закрывает соединение, по
// которому дольше своего предела ничего не шло. Без кадра поток рвался бы
// ровно там, где событий не происходило.
func TestHeartbeatKeepsAMuteStreamAlive(t *testing.T) {
	mute := &ownerStub{script: []*subscriptionv1.SubscriptionMessage{openedMessage("p", true)}, hold: true}
	h := newHandler(t, mute, func(c *subscriptionstream.Config) {
		c.Heartbeat = 50 * time.Millisecond
		c.StreamBudget = 400 * time.Millisecond
	})

	rec := serve(t, h, request("owner=probe"))
	if n := heartbeats(rec.Body.String()); n < 2 {
		t.Errorf("служебных кадров %d за срок жизни потока — молчащий поток закроет посредник", n)
	}
}

// TestNonGetIsRefused — поток только читается.
func TestNonGetIsRefused(t *testing.T) {
	h := newHandler(t, &ownerStub{script: []*subscriptionv1.SubscriptionMessage{openedMessage("p", false)}})
	r := request("owner=probe")
	r.Method = http.MethodPost
	rec := serve(t, h, r)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST получил %d, ожидался 405", rec.Code)
	}
	if allow := rec.Header().Get("Allow"); allow != http.MethodGet {
		t.Errorf("Allow %q — отказ обязан назвать допустимое", allow)
	}
}

// TestCloseSubjectClosesOpenStreams — ШОВ, оставленный kacho#1022.
//
// Проба утверждает не отзыв прав (его в этой фазе нет), а то, что отзыв БУДЕТ
// ВЫРАЗИМ: закрыть потоки субъекта — обход одного ключа, а не поиск по
// горутинам. Устройство, при котором следующая фаза невозможна, обязано
// краснеть здесь, а не обнаруживаться ею.
func TestCloseSubjectClosesOpenStreams(t *testing.T) {
	held := &ownerStub{
		script:  []*subscriptionv1.SubscriptionMessage{openedMessage("p", false)},
		hold:    true,
		started: make(chan struct{}),
	}
	h := newHandler(t, held, func(c *subscriptionstream.Config) {
		c.StreamBudget = 30 * time.Second
		c.Heartbeat = 10 * time.Second
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		serve(t, h, request("owner=probe"))
	}()
	select {
	case <-held.started:
	case <-time.After(10 * time.Second):
		t.Fatal("поток не открылся")
	}

	// Ключ субъекта — пара «тип:идентификатор», та же, что едет владельцу.
	if n := h.CloseSubject("user:usr-probe"); n != 1 {
		t.Fatalf("закрыто потоков %d, ожидался 1", n)
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("поток не закрылся после закрытия субъекта — отзыв прав остался бы невыразим")
	}
	if n := h.CloseSubject("user:usr-probe"); n != 0 {
		t.Errorf("после закрытия на учёте осталось %d потоков", n)
	}
	if n := h.CloseSubject("user:someone-else"); n != 0 {
		t.Errorf("закрытие чужого субъекта тронуло %d потоков — радиус обязан быть один субъект", n)
	}
}

// TestRefusedConfigurationIsRefusedAtStartup — величины посадки судятся при
// сборке, а не первым запросом в бою.
func TestRefusedConfigurationIsRefusedAtStartup(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  subscriptionstream.Config
	}{
		{"без потолка потоков", subscriptionstream.Config{StreamBudget: time.Second, Heartbeat: time.Millisecond}},
		{"без кадра поддержания связи", subscriptionstream.Config{StreamBudget: time.Second, MaxStreams: 1}},
		{"срок короче кадра", subscriptionstream.Config{
			StreamBudget: time.Millisecond, Heartbeat: time.Second, MaxStreams: 1}},
	} {
		if _, err := subscriptionstream.NewHandler(tc.cfg); err == nil {
			t.Errorf("%s: сборка прошла — величина посадки, которую никто не выбирал, "+
				"обнаружилась бы первым отказом в бою", tc.name)
		}
	}
}

// TestStatsCountRefusalsAndStreams — «ноль отказов за всю жизнь контроля»
// обязано быть заметно.
//
// Потолок, который ни разу не сработал, и потолок, который не подключён,
// выглядят одинаково, если их не считать.
func TestStatsCountRefusalsAndStreams(t *testing.T) {
	owner := &ownerStub{script: []*subscriptionv1.SubscriptionMessage{
		openedMessage("p", false),
		eventMessage("p1", "k", "r1"),
	}}
	h := newHandler(t, owner)

	serve(t, h, request("owner=probe"))
	serve(t, h, request("owner=elsewhere"))

	got := h.Stats()
	if got.Opened != 1 || got.EventsSent != 1 || got.RefusedInput != 1 {
		t.Errorf("счётчики %+v — открытий 1, событий 1, отказов по входу 1", got)
	}
	if got.Open != 0 {
		t.Errorf("после закрытия потоков на учёте %d", got.Open)
	}
}
