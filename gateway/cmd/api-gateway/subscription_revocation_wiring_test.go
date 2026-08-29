// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// subscription_revocation_wiring_test.go — ПРОДОВАЯ точка сборки читателя отзыва
// (kacho#1022) и величины, которыми она распоряжается.
//
// # Почему проба здесь, а не только у механизма
//
// Две независимые инъекции — снять закрыватель у слушателя и передать ноль в
// точке сборки — оставляли ВЕСЬ корпус проб края зелёным и код собирающимся:
// сквозные пробы зовут конструкторы напрямую и продовую точку сборки минуют.
// То есть несделанная провязка была НЕОТЛИЧИМА от сделанной — ровно тот класс,
// ради которого задача заведена.
//
// Ни срок неподтверждённого чтения, ни обе его константы, ни страж старта не
// были покрыты ничем.

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
	"github.com/PRO-Robotech/kacho/gateway/internal/subscriptionstream"
	"github.com/PRO-Robotech/kacho/pkg/subjectchange"
)

func quietLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(&strings.Builder{}, &slog.HandlerOptions{Level: slog.LevelError + 1}))
}

type idlePoller struct{}

func (idlePoller) PollSubjectChanges(_ context.Context, _ int64, _ int32) ([]subjectchange.SubjectChange, int64, error) {
	return nil, 0, nil
}

// probeProjection — настоящая проекция потока, собранная теми же величинами, что
// в бою. Подделкой здесь пользоваться нельзя: предмет пробы — что край передаёт
// читателю отзыва ИМЕННО СВОЙ реестр открытых потоков.
func probeProjection(t *testing.T) *subscriptionstream.Handler {
	t.Helper()
	h, err := subscriptionstream.NewHandler(subscriptionstream.Config{
		StreamBudget: 90 * time.Second, Heartbeat: 20 * time.Second,
		MaxStreams: 64, MaxStreamsPerSubject: 8, Logger: quietLog(),
	})
	if err != nil {
		t.Fatalf("сборка проекции: %v", err)
	}
	return h
}

func probeConfig() config.Config {
	return config.Config{
		SubjectChangePollInterval: 2 * time.Second,
		SubscriptionStreamBudget:  90 * time.Second,
	}
}

// TestCompositionRootWiresTheStreamRegistryIntoTheRevocationReader — несущее
// утверждение: читатель отзыва, собранный ПРОДОВОЙ функцией, закрывает потоки.
func TestCompositionRootWiresTheStreamRegistryIntoTheRevocationReader(t *testing.T) {
	w, err := buildSubjectChangeWatcher(probeConfig(), idlePoller{}, func() {}, probeProjection(t), quietLog())
	if err != nil {
		t.Fatalf("сборка читателя отзыва: %v", err)
	}
	if !w.ClosesStreams() {
		t.Fatal("продовая точка сборки не связала читателя отзыва с реестром открытых потоков — " +
			"отзыв не доезжал бы до длинных соединений вовсе")
	}
	if got := w.StaleAfter(); got != revocationStaleAfter(2*time.Second) {
		t.Fatalf("срок неподтверждённого чтения %v — точка сборки объявила не ту величину", got)
	}
}

// TestMissingProjectionIsRefusedAtStartup — ноль проекции есть ошибка ПОРЯДКА
// сборки, а не посадки: отвергается на сборке, а не первым отзывом в бою.
func TestMissingProjectionIsRefusedAtStartup(t *testing.T) {
	if _, err := buildSubjectChangeWatcher(probeConfig(), idlePoller{}, func() {}, nil, quietLog()); err == nil {
		t.Fatal("читатель отзыва собрался без реестра открытых потоков")
	}
}

// TestStaleBudgetMustBeReachableWithinTheStreamsLifetime — СТРАЖ СТАРТА.
//
// Срок, не меньший срока жизни потока, означает, что fail-closed не наступит ни
// разу: поток закроется собственным бюджетом, и это выглядело бы закрытием по
// отзыву. Предел, который не может быть достигнут, предела не ставит.
func TestStaleBudgetMustBeReachableWithinTheStreamsLifetime(t *testing.T) {
	cfg := probeConfig()
	// Срок при интервале 2s — десять секунд; бюджет потока делаем меньше.
	cfg.SubscriptionStreamBudget = 5 * time.Second
	_, err := buildSubjectChangeWatcher(cfg, idlePoller{}, func() {}, probeProjection(t), quietLog())
	if err == nil {
		t.Fatal("посадка, в которой fail-closed не наступает никогда, принята стражем старта")
	}
	if !strings.Contains(err.Error(), "fail-closed") {
		t.Errorf("отказ старта не называет предмет: %q", err)
	}
}

// TestStaleBudgetFollowsThePollInterval — обе константы вывода.
//
// Величина ВЫВОДИТСЯ из периода перепроса, потому что измеряет отказ именно
// этого механизма. Проба закрепляет ОБЕ стороны вывода: множитель и пол, — иначе
// зелёной осталась бы формула, потерявшая одну из них.
func TestStaleBudgetFollowsThePollInterval(t *testing.T) {
	for _, tc := range []struct {
		name     string
		interval time.Duration
		want     time.Duration
	}{
		{name: "частый перепрос упирается в пол", interval: 500 * time.Millisecond, want: 10 * time.Second},
		{name: "умолчание — ровно на полу", interval: 2 * time.Second, want: 10 * time.Second},
		{name: "редкий перепрос считается множителем", interval: 4 * time.Second, want: 20 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := revocationStaleAfter(tc.interval); got != tc.want {
				t.Fatalf("срок %v, ожидался %v", got, tc.want)
			}
		})
	}
}
