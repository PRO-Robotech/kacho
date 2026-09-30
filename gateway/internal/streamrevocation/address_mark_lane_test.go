// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package streamrevocation_test

// address_mark_lane_test.go — отметка адреса почты на ОТКРЫТОМ соединении
// (kacho#2900; приёмка F6b, Р4 и Р16; условие 7 (а) чек-листа поверхности).
//
// # Предмет
//
// Путь запроса судит отметку на каждом обращении: полоса сессии — ответом службы
// о сессии по носителю, полоса нашего токена — сверкой по самому токену. Перепрос
// открытых потоков спрашивал только по идентификаторам (отсечка субъекта, запись
// отзыва), и отметки эти вопросы не называют. Поток, открытый подтверждённым
// человеком, переживал снятие отметки до конца своего срока, хотя путь запроса
// отказал бы ему на следующем же обращении.
//
// Утверждение здесь — «не позже окна перепроса»: поток закрыт ПЕРВЫМ перепросом
// после того, как служба сменила ответ. Перепрос зовётся вручную: предмет — число
// перепросов, а не расписание, и на тикере проба утверждала бы о занятости машины.
//
// Каждое отрицание стоит в паре с близнецом, отличающимся ровно одним фактом, и
// рядом утверждается, что службу спросили ТЕМ ЖЕ вопросом, что путь запроса:
// без этого «поток жив» неотличимо от «про отметку не спрашивали вовсе».

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
	"github.com/PRO-Robotech/kacho/gateway/internal/streamrevocation"
)

// sessionHeaders — заголовки, какие ставит полоса нашей сессии: человек и момент
// аутентификации; `jti` у сессии нет.
func sessionHeaders(userID string) map[string]string {
	return map[string]string{
		principalmeta.HeaderPrincipalType: "user",
		principalmeta.HeaderPrincipalID:   userID,
		principalmeta.HeaderTokenMfaAt:    itoa(time.Now().Add(-time.Hour).Unix()),
	}
}

// tokenHeaders — заголовки, какие ставит полоса предъявителя токену человека.
func tokenHeaders(userID, jti string) map[string]string {
	return map[string]string{
		principalmeta.HeaderPrincipalType: "user",
		principalmeta.HeaderPrincipalID:   userID,
		principalmeta.HeaderTokenJti:      jti,
	}
}

// ourTokensStub — сверка токенов НАШЕЙ чеканки: тот порт, которым путь запроса
// спрашивает о них (middleware.TokenRevocationChecker). Отвечает «не действует»
// ровно на те токены, про которые проба это объявила, — так служба отвечает о
// токене человека, чей адрес не подтверждён.
type ourTokensStub struct {
	mu       sync.Mutex
	inactive map[string]bool
	asked    []string
}

func newOurTokensStub() *ourTokensStub { return &ourTokensStub{inactive: map[string]bool{}} }

func (o *ourTokensStub) deactivate(raw string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.inactive[raw] = true
}

func (o *ourTokensStub) askedPairs() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.asked...)
}

func (o *ourTokensStub) Introspect(
	_ context.Context, jti, raw string,
) (middleware.IntrospectionResult, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.asked = append(o.asked, jti+"|"+raw)
	if o.inactive[raw] {
		return middleware.IntrospectionResult{}, middleware.ErrTokenInactive
	}
	return middleware.IntrospectionResult{Active: true}, nil
}

func containsAll(t *testing.T, got []string, want ...string) {
	t.Helper()
	have := map[string]bool{}
	for _, g := range got {
		have[g] = true
	}
	for _, w := range want {
		if !have[w] {
			t.Fatalf("службу не спросили %q (спрошено: %v) — «поток жив» здесь означало бы "+
				"«отметку не спрашивали», а не «адрес подтверждён»", w, got)
		}
	}
}

// TestUnverifiedAddressClosesTheSessionStreamAtTheNextRecheck — НЕСУЩЕЕ
// утверждение полосы сессии. Близнец — сессия другого человека с подтверждённым
// адресом: различие ровно в отметке.
func TestUnverifiedAddressClosesTheSessionStreamAtTheNextRecheck(t *testing.T) {
	sink := &logSink{}
	s := newStand(t, func(c *streamrevocation.Config) { c.Logger = sink.logger() })
	s.human.put("brw-mark", "usr00000000000000021", true)
	s.human.put("brw-twin", "usr00000000000000022", true)

	marked := s.openStream(t, sessionHeaders("usr00000000000000021"), principalmeta.PresentedSession("brw-mark"))
	twin := s.openStream(t, sessionHeaders("usr00000000000000022"), principalmeta.PresentedSession("brw-twin"))
	defer func() { s.projection.CloseAll(); <-twin }()

	ctx := context.Background()
	s.sweeper.Sweep(ctx)
	aliveFor(t, marked, 300*time.Millisecond,
		"поток закрыт при подтверждённом адресе — перепрос закрывает не по отметке")
	containsAll(t, s.human.askedBearers(), "brw-mark", "brw-twin")

	s.human.unverify("brw-mark")
	s.sweeper.Sweep(ctx)

	closedWithin(t, marked, 2*time.Second,
		"поток сессии пережил перепрос после снятия отметки адреса: путь запроса отказал бы "+
			"этой сессии на следующем обращении, а открытое соединение живёт до своего срока")
	aliveFor(t, twin, 300*time.Millisecond,
		"закрыт поток сессии с ПОДТВЕРЖДЁННЫМ адресом — перепрос закрывает не по отметке")
	if got := sink.text(); !strings.Contains(got, "streams_closed_address_unverified=1") {
		t.Fatalf("закрытие по отметке адреса не названо числом; журнал:\n%s", got)
	}
}

// TestEndedSessionClosesItsStreamAtTheNextRecheck — носитель больше ни на что не
// указывает. Путь запроса отвечает такому носителю отказом F4d-22; «адрес
// неизвестен» проходом не бывает (F6b-09), поэтому поток закрывается.
func TestEndedSessionClosesItsStreamAtTheNextRecheck(t *testing.T) {
	s := newStand(t, nil)
	s.human.put("brw-ended", "usr00000000000000023", true)
	s.human.put("brw-alive", "usr00000000000000024", true)

	ended := s.openStream(t, sessionHeaders("usr00000000000000023"), principalmeta.PresentedSession("brw-ended"))
	alive := s.openStream(t, sessionHeaders("usr00000000000000024"), principalmeta.PresentedSession("brw-alive"))
	defer func() { s.projection.CloseAll(); <-alive }()

	s.human.end("brw-ended")
	s.sweeper.Sweep(context.Background())

	closedWithin(t, ended, 2*time.Second,
		"поток пережил конец своей сессии: служба ответила «сессии нет», а перепрос оставил поток")
	aliveFor(t, alive, 300*time.Millisecond, "закрыт поток живой сессии с подтверждённым адресом")
}

// TestSessionStreamWithoutPresentedBearerIsClosed — полоса сессии не записала
// носитель. Спросить отметку нечем, а «адрес неизвестен» проходом не бывает:
// поток закрывается, громко. Близнец отличается ровно записанным носителем.
func TestSessionStreamWithoutPresentedBearerIsClosed(t *testing.T) {
	sink := &logSink{}
	s := newStand(t, func(c *streamrevocation.Config) { c.Logger = sink.logger() })
	s.human.put("brw-recorded", "usr00000000000000026", true)

	bare := s.openStream(t, sessionHeaders("usr00000000000000025"))
	recorded := s.openStream(t, sessionHeaders("usr00000000000000026"), principalmeta.PresentedSession("brw-recorded"))
	defer func() { s.projection.CloseAll(); <-recorded }()

	s.sweeper.Sweep(context.Background())

	closedWithin(t, bare, 2*time.Second,
		"поток сессии без записанного носителя остался открыт — отметку спросить нечем, "+
			"и «не спросили» прошло бы как «подтверждён»")
	aliveFor(t, recorded, 300*time.Millisecond, "закрыт поток с записанным носителем и подтверждённым адресом")
	if got := sink.text(); !strings.Contains(got, "streams_closed_presented_missing=1") {
		t.Fatalf("закрытие потока без записанного предъявленного не названо числом; журнал:\n%s", got)
	}
}

// TestInactiveOurTokenClosesTheStreamAtTheNextRecheck — НЕСУЩЕЕ утверждение
// полосы нашего токена: сверка ответила «не действует» (так служба отвечает о
// токене человека с неподтверждённым адресом, Р5а службы). Близнец — наш токен,
// о котором сверка отвечает «действует».
//
// Сверх того: о токене нашей чеканки спрашивается сверка, а не запись отзыва по
// идентификатору, — так же, как на пути запроса, где полосу выбирает пометка
// записи издателя.
func TestInactiveOurTokenClosesTheStreamAtTheNextRecheck(t *testing.T) {
	ours := newOurTokensStub()
	s := newStand(t, func(c *streamrevocation.Config) { c.OurTokens = ours })

	marked := s.openStream(t, tokenHeaders("usr00000000000000031", "jti-ours-a"),
		principalmeta.PresentedToken("tok-a", true))
	twin := s.openStream(t, tokenHeaders("usr00000000000000032", "jti-ours-b"),
		principalmeta.PresentedToken("tok-b", true))
	defer func() { s.projection.CloseAll(); <-twin }()

	ctx := context.Background()
	s.sweeper.Sweep(ctx)
	aliveFor(t, marked, 300*time.Millisecond, "поток нашего токена закрыт при действующем токене")
	containsAll(t, ours.askedPairs(), "jti-ours-a|tok-a", "jti-ours-b|tok-b")
	if jti, _ := s.authority.asked(); len(jti) != 0 {
		t.Fatalf("о токене нашей чеканки спрошена запись отзыва (%v) — путь запроса этот вопрос "+
			"ему не задаёт, и отметку адреса запись не называет", jti)
	}

	ours.deactivate("tok-a")
	s.sweeper.Sweep(ctx)

	closedWithin(t, marked, 2*time.Second,
		"поток нашего токена пережил ответ сверки «не действует»: путь запроса отказал бы "+
			"этому токену на следующем обращении")
	aliveFor(t, twin, 300*time.Millisecond, "закрыт поток нашего действующего токена")
}

// TestOurTokenStreamWithoutItsReaderIsClosed — токен нашей чеканки, а сверки у
// перепроса нет. Путь запроса в этом состоянии отказывает такому токену всегда;
// перепрос обязан не держать его поток. Близнец — токен иной записи издателя,
// о котором спрашивается запись отзыва.
func TestOurTokenStreamWithoutItsReaderIsClosed(t *testing.T) {
	s := newStand(t, nil)

	ours := s.openStream(t, tokenHeaders("usr00000000000000033", "jti-unwired"),
		principalmeta.PresentedToken("tok-unwired", true))
	record := s.openStream(t, tokenHeaders("usr00000000000000034", "jti-record"), recordLaneToken("jti-record"))
	defer func() { s.projection.CloseAll(); <-record }()

	s.sweeper.Sweep(context.Background())

	closedWithin(t, ours, 2*time.Second,
		"поток нашего токена открыт, хотя спросить о нём сверку нечем — путь запроса такому "+
			"токену отказывает всегда")
	aliveFor(t, record, 300*time.Millisecond, "закрыт поток токена иной записи издателя при живой записи отзыва")
}

// TestTokenStreamWithoutRecordedTokenIsClosed — полоса предъявителя не записала
// токен. Какой вопрос ему задаёт путь запроса, перепрос знать не может: полосу
// выбирает пометка записи издателя, и её нет. Близнец — записанный токен той же
// формы.
func TestTokenStreamWithoutRecordedTokenIsClosed(t *testing.T) {
	s := newStand(t, nil)

	bare := s.openStream(t, tokenHeaders("usr00000000000000035", "jti-bare"))
	recorded := s.openStream(t, tokenHeaders("usr00000000000000036", "jti-recorded"), recordLaneToken("jti-recorded"))
	defer func() { s.projection.CloseAll(); <-recorded }()

	s.sweeper.Sweep(context.Background())

	closedWithin(t, bare, 2*time.Second,
		"поток токена без записанного предъявленного остался открыт — вопрос пути запроса "+
			"ему задать нечем")
	aliveFor(t, recorded, 300*time.Millisecond, "закрыт поток записанного токена при живой записи отзыва")
}
