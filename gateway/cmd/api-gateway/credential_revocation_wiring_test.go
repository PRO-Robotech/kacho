// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// credential_revocation_wiring_test.go — ПРОДОВАЯ точка сборки сметателя отзыва
// удостоверения (kacho#1410) и величины, которыми она распоряжается.
//
// # Почему проба здесь, а не только у механизма
//
// Та же причина, что у соседней (kacho#1022): инъекция «передать ноль в точке
// сборки» оставляет весь корпус проб края зелёным и код собирающимся, потому что
// сквозные пробы зовут конструктор напрямую и продовую точку сборки минуют.
// Несделанная провязка была бы НЕОТЛИЧИМА от сделанной.

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

// idleAuthority — авторитет, у которого никого не отзывали.
//
// Подделка здесь законна и её граница названа: предмет пробы — ЧТО край передал
// в точку сборки, а не что ответил сосед. Ответы соседа проверяет сквозная проба
// `gateway/internal/credentialrevocation`, где авторитет настоящий.
type idleAuthority struct{}

func (idleAuthority) IsSessionRevoked(_ context.Context, _ string) (bool, error) { return false, nil }

func (idleAuthority) SessionCutoffOf(_ context.Context, _ string) (time.Time, bool, error) {
	return time.Time{}, false, nil
}

func credentialProbeConfig() config.Config {
	return config.Config{
		CredentialRevocationSweepInterval: 2 * time.Second,
		SubscriptionStreamBudget:          90 * time.Second,
	}
}

// TestCompositionRootWiresTheStreamRegistryIntoTheCredentialSweeper — несущее
// утверждение: сметатель, собранный ПРОДОВОЙ функцией, закрывает потоки.
func TestCompositionRootWiresTheStreamRegistryIntoTheCredentialSweeper(t *testing.T) {
	s, err := buildCredentialRevocationSweeper(
		credentialProbeConfig(), idleAuthority{}, probeProjection(t), quietLog())
	if err != nil {
		t.Fatalf("сборка сметателя: %v", err)
	}
	if !s.ClosesStreams() {
		t.Fatal("продовая точка сборки не связала сметателя с реестром открытых потоков — " +
			"отзыв удостоверения не доезжал бы до длинных соединений вовсе")
	}
	if got := s.StaleAfter(); got != revocationStaleAfter(2*time.Second) {
		t.Fatalf("срок неподтверждённого чтения %v — точка сборки объявила не ту величину", got)
	}
}

// TestCredentialRevocationWindowIsNamedAndIsNotTheStreamsLifetime — ОКНО названо
// числом, и это число не есть срок жизни соединения.
//
// Ровно то, чего требует предикат задачи. Без второй половины утверждение
// зеленело бы на устройстве, объявляющем окном бюджет потока, — то есть на том
// самом состоянии, которое здесь и чинится.
func TestCredentialRevocationWindowIsNamedAndIsNotTheStreamsLifetime(t *testing.T) {
	cfg := credentialProbeConfig()
	s, err := buildCredentialRevocationSweeper(cfg, idleAuthority{}, probeProjection(t), quietLog())
	if err != nil {
		t.Fatalf("сборка сметателя: %v", err)
	}
	// Период обхода плюс бюджет одного вопроса.
	if want := 3 * time.Second; s.Window() != want {
		t.Fatalf("окно отзыва %v, ожидалось %v", s.Window(), want)
	}
	if s.Window() >= cfg.SubscriptionStreamBudget {
		t.Fatalf("окно отзыва %v не меньше срока жизни потока %v — граница отзыва осталась "+
			"сроком жизни соединения", s.Window(), cfg.SubscriptionStreamBudget)
	}
	// И оно не шире объявленной границы отзыва удостоверения на пути запроса:
	// иначе одно и то же удостоверение отвергалось бы на запросе и продолжало
	// держать поток.
	if declared := 5 * time.Second; s.Window() > declared {
		t.Fatalf("окно отзыва на соединении %v шире объявленной границы на запросе %v",
			s.Window(), declared)
	}
}

// TestMissingProjectionIsRefusedAtStartupForTheCredentialSweeper — ноль проекции
// есть ошибка ПОРЯДКА сборки, а не посадки.
func TestMissingProjectionIsRefusedAtStartupForTheCredentialSweeper(t *testing.T) {
	if _, err := buildCredentialRevocationSweeper(
		credentialProbeConfig(), idleAuthority{}, nil, quietLog()); err == nil {
		t.Fatal("сметатель собрался без реестра открытых потоков")
	}
}

// TestCredentialSweepMustFitInsideTheStreamsLifetime — СТРАЖ СТАРТА, обе его
// стороны.
//
// Величины две и они про разное: срок неподтверждённого чтения измеряет АВАРИЮ
// читателя, окно — ОБЫЧНУЮ работу. Посадка, в которой окно не меньше бюджета
// потока, означает, что отозванное удостоверение не будет спрошено на этом потоке
// ни разу, а закрытие по сроку читалось бы как исполненный отзыв.
func TestCredentialSweepMustFitInsideTheStreamsLifetime(t *testing.T) {
	t.Run("срок неподтверждённого чтения недостижим", func(t *testing.T) {
		cfg := credentialProbeConfig()
		cfg.SubscriptionStreamBudget = 5 * time.Second // при интервале 2s срок — десять
		_, err := buildCredentialRevocationSweeper(cfg, idleAuthority{}, probeProjection(t), quietLog())
		if err == nil {
			t.Fatal("посадка, в которой fail-closed не наступает никогда, принята стражем старта")
		}
		if !strings.Contains(err.Error(), "fail-closed") {
			t.Errorf("отказ старта не называет предмет: %q", err)
		}
	})

	t.Run("окно шире жизни потока", func(t *testing.T) {
		cfg := credentialProbeConfig()
		// Редкий обход: срок неподтверждённого чтения проходит (60s < 100s), а
		// окно (12s + 1s) — нет. Обе половины стража нужны порознь.
		cfg.CredentialRevocationSweepInterval = 12 * time.Second
		cfg.SubscriptionStreamBudget = 13 * time.Second
		_, err := buildCredentialRevocationSweeper(cfg, idleAuthority{}, probeProjection(t), quietLog())
		if err == nil {
			t.Fatal("посадка, в которой удостоверение не спрашивается на потоке ни разу, принята")
		}
	})

	t.Run("объявленная посадка проходит", func(t *testing.T) {
		// Положительный контроль: без него отрицания выше зеленели бы на страже,
		// отвергающем всякую посадку.
		if _, err := buildCredentialRevocationSweeper(
			credentialProbeConfig(), idleAuthority{}, probeProjection(t), quietLog()); err != nil {
			t.Fatalf("объявленная посадка отвергнута: %v", err)
		}
	})
}

// TestCredentialSweeperIsWiredAndRunInTheCompositionRoot — ПОСЛЕДНЕЕ звено, до
// которого сквозная проба не достаёт: собранный сметатель обязан быть ЗАПУЩЕН.
//
// Сметатель, собранный и не запущенный, компилируется, не роняет ни одной пробы
// и не спрашивает авторитет НИ РАЗУ — то есть отзыв удостоверения не доезжает до
// потоков, а выглядит это ровно как исправная работа.
//
// Судится текст композиционного корня, а не поведение: поднять край целиком ради
// двух строк дороже, чем их прочитать, и предмет здесь — именно строки.
func TestCredentialSweeperIsWiredAndRunInTheCompositionRoot(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("чтение композиционного корня: %v", err)
	}
	flat := collapse(string(src))

	// Реестр — ИМЕННО тот, что обслуживает потоки этого края; авторитет — по
	// УЖЕ ОБЪЯВЛЕННОМУ адресу владельца. Судятся одним вызовом: проверь порознь,
	// и вызов с верным авторитетом и чужим реестром остался бы зелёным.
	const wiring = "buildCredentialRevocationSweeper(" +
		`cfg,clients.NewSessionRevocationsAdapter(iamConn),subscriptionStream,logger)`
	if !strings.Contains(flat, wiring) {
		t.Fatalf("сметатель отзыва удостоверения не провязан к реестру открытых потоков "+
			"этого края.\nОжидалось (пробелы не в счёт): %s", wiring)
	}

	// И он ЗАПУЩЕН. Без этой половины провязка была бы объявлением: собранный и
	// не запущенный сметатель не задаёт авторитету ни одного вопроса.
	if !strings.Contains(flat, "gosweeper.Run(ctx)") {
		t.Fatal("сметатель собран, но не запущен — он не спросит авторитет ни разу, " +
			"и отзыв удостоверения не доедет до открытых потоков; выглядеть это будет " +
			"как исправная работа")
	}
}
