// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config_test

import (
	"bufio"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/services/notify/internal/config"
)

// fixturePath — единственная базовая конфигурация проб старта (CX1-79).
const fixturePath = "testdata/boot.env"

// readFixture разбирает файл фикстуры в упорядоченный перечень пар.
func readFixture(t *testing.T) [][2]string {
	t.Helper()
	f, err := os.Open(fixturePath)
	if err != nil {
		t.Fatalf("фикстура проб старта %s не открыта: %v", fixturePath, err)
	}
	defer func() { _ = f.Close() }()

	var pairs [][2]string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("строка фикстуры без «=»: %q", line)
		}
		pairs = append(pairs, [2]string{k, v})
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("фикстура не дочитана: %v", err)
	}
	if len(pairs) == 0 {
		t.Fatal("фикстура пуста: пробе старта не на чем стоять")
	}
	return pairs
}

// clearKnobs снимает из окружения процесса пробы КАЖДУЮ ручку переписи —
// унаследованная из окружения прогона переменная иначе подменила бы фикстуру.
func clearKnobs(t *testing.T) {
	t.Helper()
	for _, k := range config.Knobs() {
		t.Setenv(k.Env, "")
		if err := os.Unsetenv(k.Env); err != nil {
			t.Fatalf("снять %s: %v", k.Env, err)
		}
	}
}

// useFixture ставит фикстуру в окружение пробы, применив правки: значение
// заменяет строку, nil снимает её вовсе.
func useFixture(t *testing.T, edits map[string]*string) {
	t.Helper()
	clearKnobs(t)
	seen := map[string]bool{}
	for _, p := range readFixture(t) {
		k, v := p[0], p[1]
		seen[k] = true
		if e, ok := edits[k]; ok {
			if e == nil {
				continue
			}
			v = *e
		}
		t.Setenv(k, v)
	}
	for k, e := range edits {
		if !seen[k] && e != nil {
			t.Setenv(k, *e)
		}
	}
}

func str(s string) *string { return &s }

// start — старт с точки зрения стража: загрузка и проверка, ровно то, что
// композиционный корень делает до подъёма чего бы то ни было.
func start(t *testing.T) error {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	return cfg.Validate()
}

// requireOnlyRefusal утверждает: старт отвергнут, находка РОВНО одна, и она
// называет ожидаемую ручку (CX1-79: отрицание не зеленеет по чужой причине).
func requireOnlyRefusal(t *testing.T, err error, knob string, why ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("старт принят, ожидался отказ по %s", knob)
	}
	var r *config.RefusalError
	if !errors.As(err, &r) {
		t.Fatalf("отказ не в форме RefusalError (%T): %v", err, err)
	}
	if len(r.Findings) != 1 {
		t.Fatalf("ожидалась ровно одна находка по %s, получено %d:\n%v", knob, len(r.Findings), err)
	}
	f := r.Findings[0]
	if f.Knob.Name != knob {
		t.Fatalf("находка называет ручку %q, ожидалась %q: %v", f.Knob.Name, knob, err)
	}
	msg := err.Error()
	if !strings.Contains(msg, knob) || !strings.Contains(msg, f.Knob.Env) {
		t.Fatalf("текст отказа не называет ручку %s (%s): %s", knob, f.Knob.Env, msg)
	}
	for _, w := range why {
		if !strings.Contains(msg, w) {
			t.Fatalf("текст отказа не несёт %q: %s", w, msg)
		}
	}
}

// TestBootFixtureStarts — близнец всех отрицаний пакета: фикстура целиком
// проходит стража. Без него каждое отрицание могло бы краснеть от фикстуры.
func TestBootFixtureStarts(t *testing.T) {
	useFixture(t, nil)
	if err := start(t); err != nil {
		t.Fatalf("фикстура проб старта не проходит стража: %v", err)
	}
}
