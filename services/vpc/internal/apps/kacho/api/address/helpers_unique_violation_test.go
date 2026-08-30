// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// helpers_unique_violation_test.go — конфликт уникальности опознаётся СИГНАЛЬНОЙ
// ошибкой слоя repo, а не словами сервера.
//
// # Предмет
//
// Петля аллокатора отличает «этот адрес уже занят, возьми следующий» от всякого
// иного отказа. Пока различие принималось по подстроке в тексте СУБД, оно
// держалось настройкой сервера, о которой никто не решал: `lc_messages` на
// русской локали не производит «duplicate key value» ВОВСЕ, и петля перестала бы
// отличать конфликт от сбоя — молча, потому что подстрока не краснеет, а
// перестаёт совпадать.
//
// # Что здесь утверждается — ОБСЕРВАБЛ, а не форма кода
//
// Пара: сигнальная ошибка узнаётся, слова — нет. Одностороннее утверждение
// зеленело бы на функции, отвечающей «да» всему подряд (задача #1455).
package address

import (
	"errors"
	"fmt"
	"testing"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo"
)

// TestUniqueViolationIsDecidedBySentinelNotByWords — обе стороны различия.
func TestUniqueViolationIsDecidedBySentinelNotByWords(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
		why  string
	}{
		// ── ПОЛОЖИТЕЛЬНАЯ сторона: конфликт обязан узнаваться ────────────────
		{
			name: "сигнальная ошибка слоя repo",
			err:  repo.ErrAlreadyExists,
			want: true,
			why:  "repo уже разобрал отказ по КОДУ через pkg/db/pgfault и назвал род сигналом",
		},
		{
			name: "сигнальная ошибка под обёрткой %w",
			err:  fmt.Errorf("SetInternalIPv4 %s: %w", "adr-1", repo.ErrAlreadyExists),
			want: true,
			why:  "обёртка контекстом род не теряет — цепочка читается errors.Is",
		},

		// ── ОТРИЦАТЕЛЬНАЯ сторона: слова решением не являются ────────────────
		{
			name: "слова сервера БЕЗ сигнальной ошибки",
			err:  errors.New(`ERROR: duplicate key value violates unique constraint "addresses_ip_key"`),
			want: false,
			why: "текст сервера не контракт: он зависит от lc_messages и выпуска сервера. " +
				"Отказ, не разобранный слоем repo, конфликтом не объявляется — иначе петля " +
				"крутилась бы на отказе, который повтором не лечится",
		},
		{
			name: "код, отрендеренный драйвером в текст",
			err:  errors.New(`ERROR (SQLSTATE 23505)`),
			want: false,
			why: "код настоящий, но добыт разбором ФОРМАТИРОВАНИЯ: формат вывода драйвер " +
				"менять вправе, поле PgError.Code — нет",
		},
		{
			name: "русская локаль сервера: тот же конфликт другими словами",
			err:  errors.New(`ОШИБКА: повторяющееся значение ключа нарушает ограничение уникальности`),
			want: false,
			why: "ровно тот случай, ради которого правило: прежний предикат молча " +
				"перестал бы совпадать, и конфликт уехал бы в ветку внутреннего сбоя",
		},
		{
			name: "чужой род отказа",
			err:  repo.ErrNotFound,
			want: false,
			why:  "конфликтом является только конфликт",
		},
		{
			name: "отказа нет",
			err:  nil,
			want: false,
			why:  "nil родом не обладает",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isUniqueViolation(tc.err); got != tc.want {
				t.Fatalf("isUniqueViolation(%v) = %v, ожидалось %v.\nПочему: %s", tc.err, got, tc.want, tc.why)
			}
		})
	}
}
