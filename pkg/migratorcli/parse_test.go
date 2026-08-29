// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package migratorcli_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/migratorcli"
)

// TestFlagIsAcceptedInBothPositions — несущая проба задачи #1461.
//
// Прямая форма мигратора разбирала аргументы стандартным flag: разбор
// останавливается на первом не-флаге, поэтому флаг ПОСЛЕ подкоманды
// отбрасывался БЕЗ ЕДИНОГО СЛОВА. Опыт, воспроизводящий прежнее поведение
// дословно (flag.FlagSet + Parse + Args):
//
//	--dsn XXX up  -> dsn="XXX" direction="up"
//	up --dsn XXX  -> dsn=""    direction="up"
//
// Второй исход означает накат не на ту базу: пустой DSN уезжает в запасной
// путь (окружение, потом конфигурация сервиса), и оператор получает УСПЕХ на
// чужой схеме. Делегирующая тройка (cobra) принимает оба порядка — проверено
// прогоном настоящих команд vpc.
//
// Утверждается РАВЕНСТВО двух порядков, а не «флаг разобран»: именно равенство
// и есть предмет задачи — оператор, знающий один сервис, применяет знание к
// соседнему.
func TestFlagIsAcceptedInBothPositions(t *testing.T) {
	const dsn = "postgres://u:p@h:5432/db?sslmode=require"

	before, err := migratorcli.Parse("kacho-migrator", []string{"--dsn", dsn, "up"})
	if err != nil {
		t.Fatalf("флаг ПЕРЕД подкомандой отвергнут: %v", err)
	}
	after, err := migratorcli.Parse("kacho-migrator", []string{"up", "--dsn", dsn})
	if err != nil {
		t.Fatalf("флаг ПОСЛЕ подкоманды отвергнут: %v", err)
	}
	if before != after {
		t.Fatalf("порядок флага изменил разбор:\n  до  подкоманды: %+v\n  после подкоманды: %+v", before, after)
	}
	if after.DSN != dsn {
		t.Fatalf("DSN потерян при разборе: %q", after.DSN)
	}
}

// TestFlagAfterCommandIsNeverDroppedSilently — форма `--flag=value` тоже.
// Именно её пишет services/nlb/Makefile (`up --dialect=postgres --dsn=…`).
func TestFlagAfterCommandIsNeverDroppedSilently(t *testing.T) {
	const dsn = "postgres://u:p@h:5432/db"
	got, err := migratorcli.Parse("kacho-migrator", []string{"up", "--dialect=postgres", "--dsn=" + dsn})
	if err != nil {
		t.Fatalf("склеенная форма флага отвергнута: %v", err)
	}
	if got.DSN != dsn {
		t.Fatalf("DSN потерян: %q", got.DSN)
	}
	if got.Dialect != "postgres" {
		t.Fatalf("диалект потерян: %q", got.Dialect)
	}
}

// TestTargetIsAcceptedOnUpAndDown — возможность, которой у прямой формы не было
// вовсе (#1461, различие 1). Решение о том, что она обязана быть у всех семи,
// записано в docs/architecture/migrator-cli.md.
func TestTargetIsAcceptedOnUpAndDown(t *testing.T) {
	for _, cmd := range []string{"up", "down"} {
		got, err := migratorcli.Parse("kacho-migrator", []string{cmd, "--target", "800001"})
		if err != nil {
			t.Fatalf("%s --target отвергнут: %v", cmd, err)
		}
		if got.Target != "800001" {
			t.Fatalf("%s: цель потеряна: %q", cmd, got.Target)
		}
		// Тот же флаг перед подкомандой — cobra его на корне НЕ знает, значит и
		// здесь он обязан быть отвергнут: равенство семи важнее удобства.
		if _, err := migratorcli.Parse("kacho-migrator", []string{"--target", "800001", cmd}); err == nil {
			t.Fatalf("%s: --target перед подкомандой принят, а cobra его отвергает", cmd)
		}
	}
}

// TestTargetIsRejectedOnStatus — у status цели нет ни в одной из семи точек.
func TestTargetIsRejectedOnStatus(t *testing.T) {
	_, err := migratorcli.Parse("kacho-migrator", []string{"status", "--target", "1"})
	if err == nil {
		t.Fatal("status --target принят, хотя у status цели не существует")
	}
	if !strings.Contains(err.Error(), "target") {
		t.Fatalf("отказ не называет флаг: %v", err)
	}
}

// TestUnknownFlagIsRejectedExplicitly — в ОБЕИХ позициях. Тихо принятый
// неизвестный флаг — тот же класс, ради которого задача заведена.
func TestUnknownFlagIsRejectedExplicitly(t *testing.T) {
	for _, args := range [][]string{
		{"--nosuchflag", "up"},
		{"up", "--nosuchflag"},
	} {
		_, err := migratorcli.Parse("kacho-migrator", args)
		if err == nil {
			t.Fatalf("%v: неизвестный флаг принят молча", args)
		}
		if !strings.Contains(err.Error(), "nosuchflag") {
			t.Fatalf("%v: отказ не называет флаг: %v", args, err)
		}
	}
}

// TestExtraPositionalIsRejected — `up 800001` (догадка оператора о том, как
// задать цель) обязана получить ответ, а не молчаливый накат до головы.
func TestExtraPositionalIsRejected(t *testing.T) {
	_, err := migratorcli.Parse("kacho-migrator", []string{"up", "800001"})
	if err == nil {
		t.Fatal("лишний позиционный аргумент принят молча")
	}
	if !strings.Contains(err.Error(), "800001") {
		t.Fatalf("отказ не называет аргумент: %v", err)
	}
}

// TestUnknownCommandNamesTheKnownOnes — отказ обязан восстанавливать
// следующий шаг оператора.
func TestUnknownCommandNamesTheKnownOnes(t *testing.T) {
	_, err := migratorcli.Parse("kacho-migrator", []string{"upp"})
	if err == nil {
		t.Fatal("неизвестная подкоманда принята")
	}
	for _, want := range []string{"upp", "up", "down", "status"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("отказ не называет %q: %v", want, err)
		}
	}
}

// TestNoCommandIsRejectedWithUsage — положительный контроль отрицаний выше:
// без него все они зеленели бы на разборе, отвергающем вообще всё.
func TestNoCommandIsRejectedWithUsage(t *testing.T) {
	_, err := migratorcli.Parse("kacho-migrator", nil)
	if err == nil {
		t.Fatal("пустая командная строка принята")
	}
	if !strings.Contains(err.Error(), "usage") {
		t.Fatalf("отказ не показывает форму вызова: %v", err)
	}
}

// TestHelpIsRequestable — у делегирующей тройки `--help` есть; равенство семи
// означает, что он есть и здесь, и это НЕ ошибка разбора.
func TestHelpIsRequestable(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}, {"up", "--help"}} {
		if _, err := migratorcli.Parse("kacho-migrator", args); !errors.Is(err, migratorcli.ErrHelpRequested) {
			t.Fatalf("%v: помощь не запрошена, а получено: %v", args, err)
		}
	}
	if !strings.Contains(migratorcli.Usage("kacho-migrator"), "kacho-migrator") {
		t.Fatal("форма вызова не называет бинарь")
	}
}

// TestUnknownDialectIsRejected — единственное законное значение одно, и
// неверное обязано быть названо, а не проигнорировано.
func TestUnknownDialectIsRejected(t *testing.T) {
	if _, err := migratorcli.Parse("kacho-migrator", []string{"--dialect", "mysql", "up"}); err == nil {
		t.Fatal("чужой диалект принят")
	}
	got, err := migratorcli.Parse("kacho-migrator", []string{"up"})
	if err != nil {
		t.Fatalf("умолчание диалекта отвергнуто: %v", err)
	}
	if got.Dialect != "postgres" {
		t.Fatalf("умолчание диалекта не postgres: %q", got.Dialect)
	}
}

// TestResolveDSNPriority — --dsn > KACHO_MIGRATOR_DSN > конфигурация сервиса.
// Порядок был у трёх прямых из четырёх; у compute не было ни флага, ни
// переменной (#1461, различие 2).
func TestResolveDSNPriority(t *testing.T) {
	fromConfig := func() (string, error) { return "dsn-from-config", nil }

	t.Setenv("KACHO_MIGRATOR_DSN", "dsn-from-env")
	if got, err := migratorcli.ResolveDSN("dsn-from-flag", fromConfig); err != nil || got != "dsn-from-flag" {
		t.Fatalf("флаг не перекрыл окружение: %q, %v", got, err)
	}
	if got, err := migratorcli.ResolveDSN("", fromConfig); err != nil || got != "dsn-from-env" {
		t.Fatalf("окружение не перекрыло конфигурацию: %q, %v", got, err)
	}

	t.Setenv("KACHO_MIGRATOR_DSN", "")
	if got, err := migratorcli.ResolveDSN("", fromConfig); err != nil || got != "dsn-from-config" {
		t.Fatalf("запасной путь к конфигурации не сработал: %q, %v", got, err)
	}

	// Отказ конфигурации доезжает до оператора, а не превращается в пустой DSN.
	boom := func() (string, error) { return "", errors.New("config boom") }
	if _, err := migratorcli.ResolveDSN("", boom); err == nil || !strings.Contains(err.Error(), "config boom") {
		t.Fatalf("отказ конфигурации проглочен: %v", err)
	}
}
