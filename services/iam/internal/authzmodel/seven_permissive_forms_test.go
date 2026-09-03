// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// seven_permissive_forms_test.go — семь форм, которые РАЗБОР принимает молча, а
// ДОПУСК обязан ловить (#1978).
//
// # Что здесь утверждается и почему именно КОНЪЮНКЦИЯ
//
// Каждое из двух свойств по отдельности в дереве уже держится: допуск ловит
// каждую форму своей пробой (Д1(а), Д1(б), Д4(б), Д7(б)), а разбор отвергает
// то, что до допуска не доезжает (`TestParserRejectsWhatNeverReachesAdmission`).
// Не утверждает никто ровно ШВА между ними — что разбор эти семь форм
// ПРИНИМАЕТ, и что именно поэтому у допуска есть предмет.
//
// Шов несущий, а не декоративный. «Починка» разбора, которая напрашивается
// первой — отвергать дубль имени, — СНЯЛА БЫ у допуска его вход: Д1 сообщает о
// дубле типа и о дубле отношения ИМЕНОВАННОЙ находкой, называя, какой блок
// действует и почему это решал бы порядок сборки. Начни разбор отвергать такой
// текст, находка превратилась бы в непрозрачный отказ «разбор не состоялся», а
// оператор, приславший манифест, потерял бы единственное объяснение.
//
// Разбор терпим ОСОЗНАННО, и терпимость его — часть контракта допуска. Проба
// краснеет с обеих сторон: и если разбор станет строже, и если допуск перестанет
// ловить.
package authzmodel

import (
	"testing"

	"github.com/PRO-Robotech/kacho/internal/authzplan"
)

// sevenForms — семь суффиксов канона, каждый несёт РОВНО ОДНУ форму.
//
// Суффикс, а не отдельная модель: допуск судит собранный текст, который обязан
// начинаться каноном дословно, и форма, поданная вне этого вида, не дошла бы до
// правил вовсе.
var sevenForms = []struct {
	name   string
	suffix string
	rule   Rule
}{
	{"1 дубль канонического типа", "\ntype group\n  relations\n    define member: [user, service_account, user:*]\n", RuleD1},
	{"2 дубль отношения в типе", "\ntype acme_widget\n  relations\n    define v_get: [user]\n    define v_get: [user, user:*]\n", RuleD1},
	{"3 дубль условия с другим телом", "\ncondition mfa_fresh(x: int) {\n  x > 1\n}\n", RuleD7Suffix},
	{"4 необъявленное условие", "\ntype acme_widget\n  relations\n    define v_get: [user with nosuch_condition]\n", RuleD4Condition},
	{"5 имя типа с пробелом", "\ntype acme widget\n  relations\n    define v_get: [user]\n", RuleD7Suffix},
	{"6 мусор перед типом", "\nсовершенно посторонняя строка\ntype acme_widget\n  relations\n    define v_get: [user]\n", RuleD7Suffix},
	{"7 мусор после условия", "\nсовершенно посторонняя строка в хвосте\n", RuleD7Suffix},
}

// TestSevenFormsParseYetAreCaughtByAdmission — по каждой форме утверждается ПАРА.
func TestSevenFormsParseYetAreCaughtByAdmission(t *testing.T) {
	for _, c := range sevenForms {
		t.Run(c.name, func(t *testing.T) {
			composed := DSL + c.suffix

			// Половина 1: РАЗБОР ПРИНИМАЕТ. Это предпосылка допуска, а не
			// снисходительность: перестань он принимать — правило ниже потеряет
			// вход, и находка станет непрозрачным отказом.
			if _, err := authzplan.ParseModel(composed); err != nil {
				t.Fatalf("разбор ОТВЕРГ форму, которую принимал: %v.\n"+
					"Это не улучшение: у правила %s пропадает вход, и именованная находка "+
					"допуска превращается в отказ «разбор не состоялся». Решать, кто ловит "+
					"эту форму, надо ОДИН раз — здесь или там, но не молча", err, c.rule)
			}

			// Половина 2: ДОПУСК ЛОВИТ, и ловит НАЗВАННЫМ правилом.
			rep, err := Admit(composed)
			if err != nil {
				t.Fatalf("допуск обязан вернуть отчёт, а не ошибку: %v", err)
			}
			if len(rep.Findings) == 0 {
				t.Fatalf("форма прошла молча: находок 0 (TypesNew=%d NothingToJudge=%v)",
					rep.TypesNew, rep.NothingToJudge)
			}
			if got := findingsOf(rep, c.rule); len(got) == 0 {
				t.Fatalf("ждали находку %s, получено %v", c.rule, rules(rep))
			}
			if rep.Admitted() {
				t.Fatal("допуск обязан отказать")
			}
		})
	}
	t.Logf("осмотрено: форм %d; по каждой — разбор принимает, допуск отвергает", len(sevenForms))
}

// TestLawfulSuffixParsesAndIsAdmitted — положительный близнец ко всем семи.
//
// Без него утверждение «эти семь ловятся» зеленело бы и у допуска, который
// отвергает ВСЯКИЙ суффикс: семь отказов не отличались бы от отказа всему.
func TestLawfulSuffixParsesAndIsAdmitted(t *testing.T) {
	composed := compose(twin(t))

	if _, err := authzplan.ParseModel(composed); err != nil {
		t.Fatalf("законный близнец обязан разбираться: %v", err)
	}
	rep, err := Admit(composed)
	if err != nil {
		t.Fatalf("допуск обязан вернуть отчёт: %v", err)
	}
	if !rep.Admitted() {
		t.Fatalf("законная композиция обязана допускаться, находки: %v", rep.Findings)
	}
	t.Logf("осмотрено: законный суффикс — разбор принимает, допуск допускает, находок %d",
		len(rep.Findings))
}
