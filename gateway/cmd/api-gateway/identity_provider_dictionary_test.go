// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_provider_dictionary_test.go — страж старта судит ЗАКОННОСТЬ посадки
// личности, а не только то, что поле объявлено (#2862).
//
// Объявлено не значит законно. Тип посадки — целое, и число вне словаря
// записывает в поле всякий, кто идёт мимо разбора: преобразование типа или
// декодер, кладущий число прямо в поле. IsSet такое значение называет
// объявленным — профиль поле действительно объявил. Страж, судивший одно IsSet,
// пропускал это число к требованиям, разведённым посадкой, где оно читалось
// как «не own» и снимало требование нашего авторитета отзыва. Проверку старта
// несёт фундамент — Provider.Validate, у неё три исхода: не объявлено, вне
// словаря, законно. Страж края зовёт её вместо IsSet.
//
// Числа вне словаря проба не выписывает, а выводит из словаря фундамента: она
// обходит окрестность законных значений. Поэтому число снятой посадки попадает
// в неё без имени устаревшей константы, а новое законное значение, появись оно
// в словаре, само выпадет из отрицательных случаев и перейдёт в близнеца.
package main

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/identityposture"
	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

// numbersOutsideTheDictionary — объявленные значения поля, которых нет в
// словаре: отрицательное число и каждое число от первого после «не задано» до
// двух за наибольшим законным, не стоящее в словаре.
//
// ПРЕДПОСЫЛКА проверяется здесь же: словарь непуст, и среди выведенных чисел
// есть число выше наибольшего законного. Без первого обходить нечего, без
// второго проба не видит значения, которого фундамент ещё не завёл.
func numbersOutsideTheDictionary(t *testing.T) []identityposture.Provider {
	t.Helper()
	legal := identityposture.Values()
	if len(legal) == 0 {
		t.Fatal("словарь посадок фундамента пуст — законного близнеца нет, и проба судить не может")
	}
	highest := identityposture.Unset
	for _, p := range legal {
		if p > highest {
			highest = p
		}
	}
	outside := []identityposture.Provider{-1}
	above := false
	for p := identityposture.Unset + 1; p <= highest+2; p++ {
		if p.IsLegal() {
			continue
		}
		outside = append(outside, p)
		above = above || p > highest
	}
	if !above {
		t.Fatalf("среди чисел вне словаря нет ни одного выше наибольшего законного (%d): обход не дошёл до "+
			"значения, которого фундамент не заводил", int(highest))
	}
	return outside
}

// Число вне словаря отвергает старт текстом проверки старта фундамента. От
// законного близнеца случай отличается ровно одним: посадкой. Наш авторитет
// отзыва провязан полностью, поэтому пропустить старт стражу больше нечем,
// кроме как прочесть посадку объявленной.
func TestRevocationGuardRefusesAnIdentityProviderOutsideTheDictionary(t *testing.T) {
	outside := numbersOutsideTheDictionary(t)
	for _, p := range outside {
		t.Run(p.String(), func(t *testing.T) {
			want := p.Validate(config.IdentityProviderKnob)
			if want == nil {
				t.Fatalf("фундамент называет %v законным — проба судит не то значение", p)
			}
			err := validateProductionRevocationConfig("production", ownLaneOn(p))
			if err == nil {
				t.Fatalf("посадка %v вне словаря прошла страж старта: объявленное поле прочитано законным", p)
			}
			if !strings.Contains(err.Error(), want.Error()) {
				t.Fatalf("отказ обязан нести текст проверки старта фундамента\n  ждали: %q\nполучено: %q",
					want.Error(), err.Error())
			}
		})
	}
	t.Logf("ОСМОТРЕНО: чисел вне словаря %d · законных значений %d", len(outside), len(identityposture.Values()))
}

// Отказ по числу вне словаря производится ПЕРВЫМ и в одиночку, как отказ по
// незаданной посадке: пока посадка незаконна, неизвестно, что по ней требовать,
// и требования, разведённые посадкой, в тексте отказа не появляются.
func TestRevocationGuardRefusesAnOutsideValueBeforeAnyLaneScopedDemand(t *testing.T) {
	outside := numbersOutsideTheDictionary(t)
	for _, p := range outside {
		t.Run(p.String(), func(t *testing.T) {
			err := validateProductionRevocationConfig("production", RevocationConfig{IdentityProvider: p})
			if err == nil {
				t.Fatalf("посадка %v вне словаря без нашего авторитета прошла страж старта", p)
			}
			msg := err.Error()
			if !strings.Contains(msg, p.Validate(config.IdentityProviderKnob).Error()) {
				t.Fatalf("отказ обязан нести текст проверки старта фундамента, получено: %q", msg)
			}
			if strings.Contains(msg, platformRevocationURLKnob) || strings.Contains(msg, platformIssuerKnob) {
				t.Fatalf("при посадке вне словаря посадочное требование предъявляться не должно: %q", msg)
			}
		})
	}
	t.Logf("ОСМОТРЕНО: чисел вне словаря %d", len(outside))
}

// ЗАКОННЫЙ БЛИЗНЕЦ: каждое значение словаря с провязанным нашим авторитетом
// проходит страж. Без него отказ выше неотличим от стража, отвергающего всякую
// посадку.
func TestRevocationGuardPassesEveryValueOfTheDictionary(t *testing.T) {
	legal := identityposture.Values()
	if len(legal) == 0 {
		t.Fatal("словарь посадок фундамента пуст — близнецу нечего утверждать")
	}
	for _, p := range legal {
		if err := validateProductionRevocationConfig("production", ownLaneOn(p)); err != nil {
			t.Fatalf("законная посадка %v с провязанным нашим авторитетом обязана проходить страж: %v", p, err)
		}
	}
	t.Logf("ОСМОТРЕНО: законных значений %d", len(legal))
}

// Незаданная посадка отвергается текстом фундамента для этого исхода, с именем
// ручки края.
func TestRevocationGuardNamesAnUndeclaredPostureInTheFoundationText(t *testing.T) {
	err := validateProductionRevocationConfig("production", ownLaneOn(identityposture.Unset))
	if err == nil {
		t.Fatal("незаданная посадка обязана отвергать старт края")
	}
	want := identityposture.NotDeclared(config.IdentityProviderKnob).Error()
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("отказ обязан нести текст фундамента для незаданной посадки\n  ждали: %q\nполучено: %q",
			want, err.Error())
	}
}

// ownLaneOn — провязанная полоса нашего авторитета с посадкой p вместо own:
// отрицательные случаи и близнец этого файла отличаются от ownLane ровно ею.
func ownLaneOn(p identityposture.Provider) RevocationConfig {
	c := ownLane()
	c.IdentityProvider = p
	return c
}
