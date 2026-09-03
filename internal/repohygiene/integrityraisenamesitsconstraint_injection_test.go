// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// integrityraisenamesitsconstraint_injection_test.go — ДОКАЗАТЕЛЬСТВО, что
// гейт способен упасть и способен смолчать.
//
// Инъекция подаётся настоящим входом — текстом миграции той же формы, что в
// дереве, — и правит РОВНО ОДИН факт против положительного близнеца: клаузу
// `CONSTRAINT`, версию миграции либо границу `-- +goose Down`. Остальное в паре
// побайтово одинаково, поэтому красное не могло прийти от соседа.
//
// Судят инъекцию ТЕ ЖЕ функции, что исполняются обходом дерева
// (`IntegrityRaiseSitesIn` → `LiveIntegrityRaiseSites`).
package repohygiene

import (
	"fmt"
	"strings"
	"testing"
)

// integrityInjectionMigration — миграция той же формы, что в дереве.
//
// `%[1]s` — клауза, объявляющая связь (пусто либо `,\n CONSTRAINT = '…'`).
// Больше подставлять нечего: это и есть тот один факт, которым инъекция
// отличается от близнеца.
const integrityInjectionMigration = `-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION kacho_probe.membership_is_kept() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    IF EXISTS (SELECT 1 FROM kacho_probe.bindings) THEN
        RAISE EXCEPTION
            'Membership of user %% still carries active access bindings', OLD.user_id
            USING ERRCODE = 'integrity_constraint_violation'%[1]s;
    END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd
`

const integrityNamedClause = `,
                  CONSTRAINT = 'membership_is_kept'`

// integrityUnnamedLiveSites — сколько живых производителей НЕ называют связь.
// Тот же предикат, что у гейта; вынесен, чтобы обе стороны каждой оси считались
// одинаково.
func integrityUnnamedLiveSites(files map[string]string) []IntegrityRaiseSite {
	var all []IntegrityRaiseSite
	for path, body := range files {
		all = append(all, IntegrityRaiseSitesIn(path, body)...)
	}
	var out []IntegrityRaiseSite
	for _, s := range LiveIntegrityRaiseSites(all) {
		if !s.NamesConstraint {
			out = append(out, s)
		}
	}
	return out
}

func TestIntegrityRaiseGateFallsAndStaysSilentOnItsTwin(t *testing.T) {
	named := fmt.Sprintf(integrityInjectionMigration, integrityNamedClause)
	unnamed := fmt.Sprintf(integrityInjectionMigration, "")

	cases := []struct {
		name  string
		files map[string]string
		// wantUnnamed — сколько живых безымянных производителей ожидается.
		wantUnnamed int
		why         string
	}{
		{
			name:        "живой производитель называет связь",
			files:       map[string]string{"services/probe/internal/migrations/20260101000000_a.sql": named},
			wantUnnamed: 0,
			why:         "ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: гейт обязан молчать на верной форме",
		},
		{
			name:        "у живого производителя снята клауза CONSTRAINT",
			files:       map[string]string{"services/probe/internal/migrations/20260101000000_a.sql": unnamed},
			wantUnnamed: 1,
			why:         "ОДИН факт против близнеца — клауза",
		},
		{
			name: "безымянное определение ЗАМЕЩЕНО поздним именованным",
			files: map[string]string{
				// Версии подобраны так, что СТРОКОЙ они упорядочиваются наоборот:
				// без числового сравнения гейт счёл бы живым раннее определение
				// и покраснел бы на истории, которую правкой не изменить (ban #5).
				"services/probe/internal/migrations/472002_early.sql":         unnamed,
				"services/probe/internal/migrations/20260824010000_later.sql": named,
			},
			wantUnnamed: 0,
			why:         "ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ разбора «живого»: применённую миграцию не правят",
		},
		{
			name: "позднее определение снимает клаузу",
			files: map[string]string{
				"services/probe/internal/migrations/472002_early.sql":         named,
				"services/probe/internal/migrations/20260824010000_later.sql": unnamed,
			},
			wantUnnamed: 1,
			why:         "ОДИН факт против предыдущего: местами поменялись клаузы, а не версии",
		},
		{
			name: "безымянный RAISE стоит в ветви отката",
			files: map[string]string{
				"services/probe/internal/migrations/20260101000000_a.sql": named +
					"\n-- +goose Down\n" + unnamed,
			},
			wantUnnamed: 0,
			why:         "ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: откат возвращает прежнее состояние by construction",
		},
		{
			name: "класс НАЗВАН ПРОЗОЙ, производителя нет",
			files: map[string]string{
				"services/probe/internal/migrations/20260101000000_a.sql": "-- +goose Up\n" +
					"-- Страж поднимает `integrity_constraint_violation` без имени связи,\n" +
					"-- и отображение отказов ключуется именем.\nSELECT 1;\n",
			},
			wantUnnamed: 0,
			why: "ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: счёт по слову считал бы объяснение производителем — " +
				"ровно тот дефект предиката, из-за которого гейт и заведён",
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			got := integrityUnnamedLiveSites(c.files)
			if len(got) != c.wantUnnamed {
				t.Fatalf("живых безымянных производителей %d, ожидалось %d (%s)\n%v",
					len(got), c.wantUnnamed, c.why, got)
			}
			if c.wantUnnamed == 0 {
				return
			}
			// Находка обязана называть КООРДИНАТУ и функцию: без них читатель
			// ищет не там, и гейт снимают как непонятный.
			if got[0].Function == "" || got[0].File == "" || got[0].Line == 0 {
				t.Fatalf("находка не называет координату: %+v", got[0])
			}
		})
	}
	t.Logf("осей 6; сторон проверено %d", len(cases))
}

// TestIntegrityRaiseParserSeesTheWholeStatement — контроль предпосылки разбора:
// клауза `CONSTRAINT` стоит НИЖЕ клаузы `ERRCODE`, и разбор по одной строке
// объявил бы названную связь безымянной.
//
// Заведено отдельно, потому что это единственная ось, где ошибка разбора даёт
// красное на ВЕРНОМ дереве: гейт с ложными находками отключают первым.
func TestIntegrityRaiseParserSeesTheWholeStatement(t *testing.T) {
	named := fmt.Sprintf(integrityInjectionMigration, integrityNamedClause)
	if !strings.Contains(named, "ERRCODE") || !strings.Contains(named, "CONSTRAINT") {
		t.Fatal("фикстура не несёт обеих клауз — контроль беспредметен")
	}
	// Между клаузами есть перевод строки: иначе проба доказывала бы свойство
	// однострочного оператора, которого в дереве нет.
	errcodeLine := strings.Count(named[:strings.Index(named, "ERRCODE")], "\n")
	constraintLine := strings.Count(named[:strings.Index(named, "CONSTRAINT =")], "\n")
	if constraintLine <= errcodeLine {
		t.Fatalf("клаузы на одной строке (ERRCODE %d, CONSTRAINT %d) — фикстура не той формы",
			errcodeLine, constraintLine)
	}

	sites := IntegrityRaiseSitesIn("services/probe/internal/migrations/20260101000000_a.sql", named)
	if len(sites) != 1 {
		t.Fatalf("разобрано %d операторов вместо одного", len(sites))
	}
	if !sites[0].NamesConstraint {
		t.Fatal("разбор не увидел клаузу CONSTRAINT, стоящую ниже ERRCODE: " +
			"гейт краснел бы на верном дереве")
	}
	t.Logf("операторов 1; ERRCODE на строке %d, CONSTRAINT на строке %d", errcodeLine, constraintLine)
}
