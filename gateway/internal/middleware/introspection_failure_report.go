// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// introspection_failure_report.go — making a check that cannot currently answer
// visible, without drowning the log that is supposed to show it.
//
// When a revocation source cannot be reached, or a token carries nothing to ask
// by, the request is refused (auth_revocation.go: «could not establish» is not
// «live»). The refusal is per-request, and so is its cause, so a line per
// occurrence buries everything else in the log during an outage — and a single
// line at the start of a ten-minute outage says nothing about how long it
// lasted. Hence: one line per window, carrying the running total and how many
// occurrences it stands for.
//
// The line is the narrative of a window — when it started, how many it stands
// for. The DURATION of a state is read on the diagnostic surface: the bearer
// lane's outcomes are cells there (bearer_lane.go → kacho_api_gateway_bearer_lane_
// revocation_total), as the session lane's are (human_session.go), and every
// window's own running total is a cell too (Total → kacho_api_gateway_log_window_
// events_total{window}, observability/metrics/log_window.go). The note that
// stood here — «this process exposes no metrics endpoint» — outlived its subject:
// the edge serves a diagnostic surface (KACHO_API_GATEWAY_METRICS_ADDR).
package middleware

import (
	"sync"
	"time"
)

// defaultIntrospectionFailureLogInterval — how often a continuing failure is
// re-stated. Short enough that an operator watching a stand sees it promptly,
// long enough that a sustained outage does not crowd out other lines.
const defaultIntrospectionFailureLogInterval = 30 * time.Second

// introspectionFailureReporter rate-limits repeated reports while keeping an
// exact count, so consecutive lines describe the whole outage rather than two
// unrelated moments.
type introspectionFailureReporter struct {
	interval time.Duration
	now      func() time.Time

	mu         sync.Mutex
	total      int64     // every failure since start
	suppressed int64     // failures since the last emitted line
	last       time.Time // when a line was last emitted
	started    bool
}

func newIntrospectionFailureReporter(interval time.Duration, now func() time.Time) *introspectionFailureReporter {
	if interval <= 0 {
		interval = defaultIntrospectionFailureLogInterval
	}
	if now == nil {
		now = time.Now
	}
	return &introspectionFailureReporter{interval: interval, now: now}
}

// observe records one failure and reports whether this one should be logged,
// along with the running total and the number of failures the line stands for
// (itself included). The first failure always reports — an operator must not
// wait a window to learn that a check stopped answering.
func (r *introspectionFailureReporter) observe() (report bool, total, represents int64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.total++
	r.suppressed++

	now := r.now()
	if r.started && now.Sub(r.last) < r.interval {
		return false, r.total, 0
	}
	r.started = true
	r.last = now
	represents = r.suppressed
	r.suppressed = 0
	return true, r.total, represents
}

// Total — итог окна с запуска: величина его клетки на диагностической
// поверхности (kacho#2740). Строка журнала называет первое событие окна; рост
// клетки показывает, держится ли состояние. nil — окно непровязанной полосы:
// ноль.
func (r *introspectionFailureReporter) Total() uint64 {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return uint64(r.total) // #nosec G115 -- счёт событий с запуска, отрицательным не бывает
}

// LogWindowTotal — итог одного окна доклада, как его читает поверхность сбора.
type LogWindowTotal interface {
	Total() uint64
}

// logWindow отдаёт окно поверхности; непровязанное окно — nil интерфейса, а не
// интерфейс с nil внутри, чтобы «окна нет» читалось сравнением с nil.
func logWindow(r *introspectionFailureReporter) LogWindowTotal {
	if r == nil {
		return nil
	}
	return r
}

// LogWindows — окна доклада слоя аутентификации по предмету: у каждого — клетка
// на приборе. Поле на КАЖДОЕ окно AuthInterceptor; полноту держит проба
// TestLogWindowsCarryEveryReporterOfTheAuthLayer.
type LogWindows struct {
	// RecordRevocationFailure — запись отзыва не ответила либо собрана без
	// источника (revocationFailures).
	RecordRevocationFailure LogWindowTotal
	// RevocationNoIdentifier — у токена нет jti, спросить об отзыве нечем
	// (revocationSkips).
	RevocationNoIdentifier LogWindowTotal
	// AuthorityRevocationFailure — авторитет отзыва нашей чеканки не ответил
	// либо адресован не туда (platformRevocationFailures).
	AuthorityRevocationFailure LogWindowTotal
	// SessionServiceFailure — служба доступа не ответила о сессии либо об
	// отсечке на полосе браузерной сессии (sessionCutoffFailures).
	SessionServiceFailure LogWindowTotal
	// OwnAssuranceOffAxis — ответ о нашей сессии с уровнем вне оси сессии.
	OwnAssuranceOffAxis LogWindowTotal
	// BasicAssuranceUnknown — базовое удостоверение с уровнем вне оси каталога.
	BasicAssuranceUnknown LogWindowTotal
	// AuthMethodsUnusable — способы подтверждения, не довезённые до модели прав.
	AuthMethodsUnusable LogWindowTotal
}

// LogWindows — окна доклада слоя для коллектора диагностической поверхности.
func (a *AuthInterceptor) LogWindows() LogWindows {
	return LogWindows{
		RecordRevocationFailure:    logWindow(a.revocationFailures),
		RevocationNoIdentifier:     logWindow(a.revocationSkips),
		AuthorityRevocationFailure: logWindow(a.platformRevocationFailures),
		SessionServiceFailure:      logWindow(a.sessionCutoffFailures),
		OwnAssuranceOffAxis:        logWindow(a.ownAssuranceOffAxis),
		BasicAssuranceUnknown:      logWindow(a.basicAssuranceUnknown),
		AuthMethodsUnusable:        logWindow(a.authMethodsUnusable),
	}
}

// AuthMethodsUnusableWindow — окно поверхности DPoP о способах подтверждения,
// не довезённых до модели прав: своё, отдельное от окна слоя аутентификации.
func (m *DPoPMiddleware) AuthMethodsUnusableWindow() LogWindowTotal {
	if m == nil {
		return nil
	}
	return logWindow(m.authMethodsUnusable)
}
