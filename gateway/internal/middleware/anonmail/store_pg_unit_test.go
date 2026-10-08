// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package anonmail

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// TestDecisionLimitsSQL_UK53_IsDerivedFromTheConstants — строка пределов
// (УК53): statement_timeout и idle_in_transaction_session_timeout —
// anonMailDecisionBudget в миллисекундах, lock_timeout — anonMailStoreWait;
// параметров у строки нет; подмена значения меняет строку.
func TestDecisionLimitsSQL_UK53_IsDerivedFromTheConstants(t *testing.T) {
	want := fmt.Sprintf("SET LOCAL statement_timeout = %d; SET LOCAL idle_in_transaction_session_timeout = %d; SET LOCAL lock_timeout = %d",
		anonMailDecisionBudget.Milliseconds(), anonMailDecisionBudget.Milliseconds(), anonMailStoreWait.Milliseconds())
	if decisionLimitsSQL != want {
		t.Errorf("строка пределов:\n  %q\nожидалась\n  %q", decisionLimitsSQL, want)
	}
	if strings.Contains(decisionLimitsSQL, "$") {
		t.Errorf("у строки пределов есть параметры: %q", decisionLimitsSQL)
	}
	other := decisionLimitsSQLFor(2*time.Second, 300*time.Millisecond)
	if other == decisionLimitsSQL || !strings.Contains(other, "= 2000;") || !strings.HasSuffix(other, "= 300") {
		t.Errorf("подмена значений не меняет строку: %q", other)
	}
	t.Logf("строка пределов: %s", decisionLimitsSQL)
}

// TestLimiterPoolConfig_UK52_UK54 — модульная проба конфигурации пула
// ограничителя: MaxConns = MinConns = anonMailPoolConns, MaxConnLifetime задан,
// MaxConnLifetimeJitter > 0, обработчик отмены — CancelRequest с нулевой
// задержкой запроса и DeadlineDelay = anonMailCancelGrace. Инъекция разброса 0
// — отказ с именем поля.
func TestLimiterPoolConfig_UK52_UK54(t *testing.T) {
	cfg, err := limiterPoolConfig("postgres://u:p@127.0.0.1:5432/db?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	h := cfg.ConnConfig.BuildContextWatcherHandler(nil)
	cr, ok := h.(*pgconn.CancelRequestContextWatcherHandler)
	if !ok {
		t.Fatalf("обработчик отмены %T, ожидался *pgconn.CancelRequestContextWatcherHandler", h)
	}
	t.Logf("пул: MaxConns %d · MinConns %d · MaxConnLifetime %s · MaxConnLifetimeJitter %s · обработчик %T · CancelRequestDelay %s · DeadlineDelay %s",
		cfg.MaxConns, cfg.MinConns, cfg.MaxConnLifetime, cfg.MaxConnLifetimeJitter, h, cr.CancelRequestDelay, cr.DeadlineDelay)
	if cfg.MaxConns != anonMailPoolConns || cfg.MinConns != anonMailPoolConns {
		t.Errorf("MaxConns %d, MinConns %d — ожидалось %d", cfg.MaxConns, cfg.MinConns, anonMailPoolConns)
	}
	if cfg.MaxConnLifetime != anonMailConnLifetime || cfg.MaxConnLifetimeJitter != anonMailConnLifetimeJitter {
		t.Errorf("срок жизни %s, разброс %s", cfg.MaxConnLifetime, cfg.MaxConnLifetimeJitter)
	}
	if cr.CancelRequestDelay != 0 || cr.DeadlineDelay != anonMailCancelGrace {
		t.Errorf("CancelRequestDelay %s, DeadlineDelay %s", cr.CancelRequestDelay, cr.DeadlineDelay)
	}
	if err := validateLimiterPoolConfig(cfg); err != nil {
		t.Errorf("исправная конфигурация отвергнута: %v", err)
	}
	cfg.MaxConnLifetimeJitter = 0
	if err := validateLimiterPoolConfig(cfg); err == nil || !strings.Contains(err.Error(), "MaxConnLifetimeJitter") {
		t.Errorf("инъекция разброса 0: %v — ожидался отказ с именем поля", err)
	}
}

// stubProbe — заглушка опроса для resolveCommit: ответ задан последовательно.
type stubProbe struct {
	answers []func() (*string, error)
	polls   int
	reads   int
	row     bool
}

func (s *stubProbe) xactStatus(context.Context, string) (*string, error) {
	i := s.polls
	s.polls++
	if i >= len(s.answers) {
		i = len(s.answers) - 1
	}
	return s.answers[i]()
}

func (s *stubProbe) passRecorded(context.Context, pendingCommit) (bool, error) {
	s.reads++
	return s.row, nil
}

func str(v string) func() (*string, error) { return func() (*string, error) { return &v, nil } }
func null() (*string, error)               { return nil, nil }
func pgErr(code string) func() (*string, error) {
	return func() (*string, error) { return nil, &pgconn.PgError{Code: code} }
}

// TestResolveCommit_UK57_EveryAnswerClassOfThePoll — resolveCommit на заглушке
// опроса (УК57): по варианту на каждый класс ответа pg_xact_status; утверждаются
// исход, число опросов, число чтений строки и признак WARN. Близнец-инъекция
// «22023 сведён к повтору» — опросов больше одного и WARN записан.
func TestResolveCommit_UK57_EveryAnswerClassOfThePoll(t *testing.T) {
	type tc struct {
		name    string
		answers []func() (*string, error)
		row     bool
		want    Outcome
		polls   string // "1", ">1"
		reads   int
		warn    bool
	}
	cases := []tc{
		{"in progress, затем committed — строка есть", []func() (*string, error){str("in progress"), str("committed")}, true, Pass, ">1", 1, false},
		{"in progress до конца срока", []func() (*string, error){str("in progress")}, true, StoreUnavailable, ">1", 0, true},
		{"committed — строка есть", []func() (*string, error){str("committed")}, true, Pass, "1", 1, false},
		{"aborted — строки нет", []func() (*string, error){str("aborted")}, false, StoreUnavailable, "1", 1, false},
		{"NULL — старше усечения, строка есть", []func() (*string, error){null}, true, Pass, "1", 1, false},
		{"22023 — база после переключения, строки нет", []func() (*string, error){pgErr("22023")}, false, StoreUnavailable, "1", 1, false},
		{"ошибка соединения, затем committed", []func() (*string, error){func() (*string, error) { return nil, errors.New("dial tcp: connection refused") }, str("committed")}, true, Pass, ">1", 1, false},
		{"отмена, затем committed", []func() (*string, error){func() (*string, error) { return nil, context.DeadlineExceeded }, str("committed")}, true, Pass, ">1", 1, false},
		{"57014, затем committed", []func() (*string, error){pgErr("57014"), str("committed")}, true, Pass, ">1", 1, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			logs := &syncBuffer{}
			b := &pgBackend{log: slog.New(slog.NewTextHandler(logs, nil)), classify: classifyPoll}
			p := &stubProbe{answers: c.answers, row: c.row}
			start := time.Now()
			got := b.resolveCommit(context.Background(), p, pendingCommit{xid: "1", decisionID: [16]byte{1}}, start.Add(anonMailResolveBudget))
			warn := strings.Contains(logs.String(), "commit outcome unresolved")
			pollsOK := (c.polls == "1" && p.polls == 1) || (c.polls == ">1" && p.polls > 1)
			if got != c.want || !pollsOK || p.reads != c.reads || warn != c.warn {
				t.Errorf("исход %s (ожидался %s) · опросов %d (%s) · чтений %d (%d) · WARN %v (%v)",
					got, c.want, p.polls, c.polls, p.reads, c.reads, warn, c.warn)
			}
		})
	}
	t.Run("близнец-инъекция: 22023 сведён к повтору", func(t *testing.T) {
		logs := &syncBuffer{}
		b := &pgBackend{log: slog.New(slog.NewTextHandler(logs, nil)), classify: func(s *string, err error) pollClass {
			var pe *pgconn.PgError
			if errors.As(err, &pe) && pe.Code == "22023" {
				return pollTransient
			}
			return classifyPoll(s, err)
		}}
		p := &stubProbe{answers: []func() (*string, error){pgErr("22023")}}
		_ = b.resolveCommit(context.Background(), p, pendingCommit{xid: "1"}, time.Now().Add(anonMailResolveBudget))
		if p.polls <= 1 || !strings.Contains(logs.String(), "commit outcome unresolved") {
			t.Fatalf("инъекция: опросов %d, WARN %v — проба не отличила бы класс 22023", p.polls, strings.Contains(logs.String(), "commit outcome unresolved"))
		}
	})
}
