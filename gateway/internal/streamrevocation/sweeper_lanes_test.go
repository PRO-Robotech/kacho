// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package streamrevocation_test

// sweeper_lanes_test.go — ПОЛОСЫ перепроса и его отказ.
//
// Здесь каждое отрицание стоит В ПАРЕ со своим положительным контролем: без
// пары «поток закрыт» зеленело бы на устройстве, закрывающем всё подряд, а
// «поток жив» — на устройстве, не закрывающем ничего.

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
	"github.com/PRO-Robotech/kacho/gateway/internal/streamrevocation"
)

// clock — управляемые часы. Срок обязан быть свойством решения, а не занятости
// машины: на настоящих часах проба либо ждёт минуты, либо утверждает о них
// наугад.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func newClock() *clock { return &clock{now: time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)} }

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// logSink — журнал, который можно прочитать.
type logSink struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *logSink) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *logSink) text() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func (l *logSink) logger() *slog.Logger {
	return slog.New(slog.NewTextHandler(l, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

// closedWithin / aliveFor — два наблюдения об одном канале, и они НЕ
// симметричны: «закрылся» доказывается ожиданием, «жив» — только истечением
// срока, за который он был бы закрыт.
func closedWithin(t *testing.T, done <-chan struct{}, d time.Duration, msg string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatal(msg)
	}
}

func aliveFor(t *testing.T, done <-chan struct{}, d time.Duration, msg string) {
	t.Helper()
	select {
	case <-done:
		t.Fatal(msg)
	case <-time.After(d):
	}
}

// TestLiveCredentialLeavesTheStreamOpen — ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ несущего
// утверждения. Ни одного отзыва нет; перепрос идёт и обязан не закрыть ничего.
func TestLiveCredentialLeavesTheStreamOpen(t *testing.T) {
	s := newStand(t, nil)
	done := s.openStream(t, map[string]string{
		principalmeta.HeaderPrincipalType: "user",
		principalmeta.HeaderPrincipalID:   "usr00000000000000002",
		principalmeta.HeaderTokenJti:      "jti-live",
	})

	runCtx, stop := context.WithCancel(context.Background())
	defer stop()
	go s.sweeper.Run(runCtx)

	aliveFor(t, done, time.Second,
		"поток закрыт при живом удостоверении — перепрос закрывает не по отзыву, "+
			"и тогда «закрыт по отзыву» ничего не означает")

	jti, _ := s.authority.asked()
	if len(jti) == 0 {
		t.Fatal("у авторитета не спросили НИ РАЗУ — «поток жив» здесь означает «контроль не исполнялся», " +
			"а не «удостоверение действительно»")
	}
}

// TestSessionCutoffClosesTheBrowserStream — БРАУЗЕРНАЯ полоса: удостоверения с
// идентификатором нет, спрашивается пара (человек, момент аутентификации).
//
// Полоса отдельная не по вкусу: у браузерной сессии `jti` нет вовсе, и полоса,
// закрывающая только по нему, оставила бы консоль — единственного клиента этой
// проекции — без отзыва целиком.
func TestSessionCutoffClosesTheBrowserStream(t *testing.T) {
	s := newStand(t, nil)

	authAt := time.Now().Add(-time.Hour).Truncate(time.Second)
	done := s.openStream(t, map[string]string{
		principalmeta.HeaderPrincipalType: "user",
		principalmeta.HeaderPrincipalID:   "usr00000000000000003",
		// `jti` НЕ ставится: браузерная сессия его не несёт.
		principalmeta.HeaderTokenMfaAt: itoa(authAt.Unix()),
	})

	runCtx, stop := context.WithCancel(context.Background())
	defer stop()
	go s.sweeper.Run(runCtx)

	// Отсечка РАНЬШЕ момента входа: сессия законна, поток обязан жить. Это
	// положительный контроль ВНУТРИ пробы — без него «закрыт» зеленело бы на
	// любой отсечке, включая ту, которая эту сессию не касается.
	s.authority.setCutoff("usr00000000000000003", authAt.Add(-time.Minute))
	aliveFor(t, done, 700*time.Millisecond,
		"поток закрыт отсечкой, которая РАНЬШЕ его входа — отсечка обязана действовать вперёд, "+
			"иначе принудительный выход блокировал бы человека навсегда")

	// Отсечка ПОЗЖЕ момента входа: сессия отозвана.
	s.authority.setCutoff("usr00000000000000003", authAt.Add(time.Minute))
	closedWithin(t, done, 10*time.Second,
		"браузерный поток пережил принудительный выход: отсечка субъекта до открытых соединений "+
			"не доезжает, и консоль — единственный клиент этой проекции — остаётся без отзыва вовсе")
}

// TestUnansweredAuthorityClosesEveryStreamOnlyAfterTheDeclaredWindow —
// FAIL-CLOSED и его законный близнец в одной пробе.
//
// Неполученный ответ авторитета не есть «удостоверений никто не отзывал». Но
// одна заминка аварией не является: закрывать на первом же молчании значило бы
// рвать потоки всего флота на каждом чихе соседа.
func TestUnansweredAuthorityClosesEveryStreamOnlyAfterTheDeclaredWindow(t *testing.T) {
	clk := newClock()
	s := newStand(t, func(c *streamrevocation.Config) {
		// Перепрос вручную: предмет — СРОК, а не расписание. На тикере проба
		// утверждала бы о занятости машины.
		c.Interval = time.Second
		c.StaleAfter = 25 * time.Second
		c.Now = clk.Now
	})
	done := s.openStream(t, map[string]string{
		principalmeta.HeaderPrincipalType: "user",
		principalmeta.HeaderPrincipalID:   "usr00000000000000004",
		principalmeta.HeaderTokenJti:      "jti-stale",
	})

	ctx := context.Background()
	s.authority.goSilent()

	// Молчание КОРОЧЕ объявленного срока — поток жив.
	clk.advance(20 * time.Second)
	s.sweeper.Sweep(ctx)
	aliveFor(t, done, 300*time.Millisecond,
		"поток закрыт молчанием короче объявленного срока — тогда срок не объявлен, "+
			"а любая заминка соседа рвёт потоки всего флота")

	// Молчание ДОЛЬШЕ объявленного срока — закрываем всё.
	clk.advance(10 * time.Second)
	s.sweeper.Sweep(ctx)
	closedWithin(t, done, 10*time.Second,
		"поток пережил молчание авторитета дольше объявленного срока — неполученный ответ "+
			"не есть «не отозван», и контроль отключался бы тем самым событием, ради которого заведён")
}

// TestAnsweringAuthorityNeverTripsFailClosed — законный близнец предыдущей: срок
// неподтверждённого чтения не копится, пока авторитет отвечает.
//
// Без неё fail-closed зеленел бы на устройстве, закрывающем потоки по
// истечении срока НЕЗАВИСИМО от ответов авторитета, — то есть по второму
// бюджету жизни соединения.
func TestAnsweringAuthorityNeverTripsFailClosed(t *testing.T) {
	clk := newClock()
	s := newStand(t, func(c *streamrevocation.Config) {
		c.Interval = time.Second
		c.StaleAfter = 25 * time.Second
		c.Now = clk.Now
	})
	done := s.openStream(t, map[string]string{
		principalmeta.HeaderPrincipalType: "user",
		principalmeta.HeaderPrincipalID:   "usr00000000000000005",
		principalmeta.HeaderTokenJti:      "jti-answered",
	})

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		clk.advance(time.Minute)
		s.sweeper.Sweep(ctx)
	}
	aliveFor(t, done, 300*time.Millisecond,
		"поток закрыт при отвечающем авторитете — срок неподтверждённого чтения копится "+
			"независимо от ответов, то есть стал вторым бюджетом жизни соединения")
}

// TestUnaskableCredentialIsNamedAndNotSilentlyPassed — поток, чьё удостоверение
// себя не назвало, отзывом закрыть НЕЛЬЗЯ. Остаток обязан быть назван числом.
//
// «Закрывать было нечего» и «закрыть было нечем» — разные состояния; слитые в
// одно, они дают величину, по которой нельзя принять ни одного решения.
func TestUnaskableCredentialIsNamedAndNotSilentlyPassed(t *testing.T) {
	sink := &logSink{}
	s := newStand(t, func(c *streamrevocation.Config) {
		c.Interval = time.Second
		c.StaleAfter = 25 * time.Second
		c.Logger = sink.logger()
	})
	// Служебная учётка без удостоверения с идентификатором: спросить авторитет
	// не о чем — отсечка ключуется людьми, а `jti` не предъявлен.
	done := s.openStream(t, map[string]string{
		principalmeta.HeaderPrincipalType: "service_account",
		principalmeta.HeaderPrincipalID:   "sva00000000000000001",
	})
	defer func() { s.projection.CloseAll(); <-done }()

	s.sweeper.Sweep(context.Background())

	jti, user := s.authority.asked()
	if len(jti) != 0 || len(user) != 0 {
		t.Fatalf("у авторитета спросили про %v / %v — про удостоверение, которое себя не назвало, "+
			"вопроса не существует, и заданный означал бы вопрос ни о ком", jti, user)
	}
	if got := sink.text(); !strings.Contains(got, "streams_unaskable=1") {
		t.Fatalf("перепрос не назвал остаток числом; журнал:\n%s", got)
	}
}

// TestRolloutSkewPassesLoudlyAndNeverTripsFailClosed — авторитет ЖИВ и вопроса
// не предлагает (край впереди службы прав).
//
// Считать это отказом значило бы закрывать потоки всего флота на всё окно
// раската — авария, а не ужесточение, при том что состояние сходится само. Та
// же посадка принята полосой ЗАПРОСА, и расхождение полос одного механизма
// здесь было бы расхождением его с самим собой.
func TestRolloutSkewPassesLoudlyAndNeverTripsFailClosed(t *testing.T) {
	clk := newClock()
	sink := &logSink{}
	s := newStand(t, func(c *streamrevocation.Config) {
		c.Interval = time.Second
		c.StaleAfter = 25 * time.Second
		c.Now = clk.Now
		c.Logger = sink.logger()
	})
	s.authority.mu.Lock()
	s.authority.unsupported = true
	s.authority.mu.Unlock()

	done := s.openStream(t, map[string]string{
		principalmeta.HeaderPrincipalType: "user",
		principalmeta.HeaderPrincipalID:   "usr00000000000000006",
		principalmeta.HeaderTokenMfaAt:    itoa(time.Now().Add(-time.Hour).Unix()),
	})
	defer func() { s.projection.CloseAll(); <-done }()

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		clk.advance(time.Minute)
		s.sweeper.Sweep(ctx)
	}
	aliveFor(t, done, 300*time.Millisecond,
		"поток закрыт в окне раската: «метода нет» прочитано как «авторитет не ответил», "+
			"и тогда всякая выкатка рвёт потоки всего флота, пока служба прав не догонит")

	if got := sink.text(); !strings.Contains(got, "streams_cutoff_unsupported=1") {
		t.Fatalf("окно раската прошло МОЛЧА — контроль не исполнился, и это неотличимо от того, "+
			"что он прошёл; журнал:\n%s", got)
	}
}
