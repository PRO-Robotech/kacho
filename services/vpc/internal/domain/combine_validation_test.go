// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain

import (
	"errors"
	"testing"
)

// Совокупная Validate() не имеет права ПОТЕРЯТЬ отказ.
//
// Слияние нарушений собирает только ошибки своего типа, а всё прочее прежняя
// редакция молча отбрасывала и возвращала nil. Дефект был найден так: проверка
// имени на несколько часов стала возвращать готовый транспортный статус (#715),
// и совокупная `Validate()` ВОСЬМИ ресурсов начала принимать негодное имя,
// оставаясь зелёной по всем прежним пробам.
//
// Сегодня путь имени эту ветку НЕ исполняет: форма приезжает из
// `pkg/validate/nameform` — пакета без транспорта, — и имя снова возвращает
// доменное нарушение. Поэтому у ветки не осталось производителя в проде, и
// проба обязана дать его сама: без неё запрет был бы формой без содержания —
// ровно тем классом, который он и ловит.

// TestCombineValidation_ForeignErrorIsNotSwallowed — ошибка чужого типа доходит
// до вызывающего, а не превращается в nil. Производитель — синтетический: в
// проде его сейчас нет, и это названо вслух выше.
func TestCombineValidation_ForeignErrorIsNotSwallowed(t *testing.T) {
	foreign := errors.New("отказ, пришедший не из домена")

	got := combineValidation(foreign)
	if got == nil {
		t.Fatalf("чужая ошибка проглочена: слияние вернуло nil вместо %v", foreign)
	}
	if !errors.Is(got, foreign) {
		t.Fatalf("вернулась не та ошибка: got %v, want %v", got, foreign)
	}
}

// TestCombineValidation_OwnViolationsStillMerge — положительный контроль рядом:
// свои нарушения по-прежнему СЛИВАЮТСЯ в одно, а не обрываются на первом. Без
// этой половины предыдущая проба зеленела бы и на слиянии, которое возвращает
// первую попавшуюся ошибку и больше ничего не делает.
func TestCombineValidation_OwnViolationsStillMerge(t *testing.T) {
	got := combineValidation(
		newValidationError("a", "первое"),
		newValidationError("b", "второе"),
	)
	var ve *ValidationError
	if !errors.As(got, &ve) {
		t.Fatalf("ожидался *ValidationError, got %T (%v)", got, got)
	}
	if len(ve.Violations) != 2 {
		t.Fatalf("нарушений %d, want 2 — свои нарушения обязаны сливаться", len(ve.Violations))
	}
}

// TestCombineValidation_NoViolationsIsUntypedNil — вырожденный случай: без
// нарушений возвращается НЕТИПИЗИРОВАННЫЙ nil, иначе `err != nil` у вызывающих
// стало бы истиной на пустом результате.
func TestCombineValidation_NoViolationsIsUntypedNil(t *testing.T) {
	if err := combineValidation(nil, nil); err != nil {
		t.Fatalf("без нарушений ожидался nil, got %T (%v)", err, err)
	}
}

// TestAggregateValidate_DoesNotSwallowNameRefusal — то же свойство на уровне,
// который видит вызывающий: негодное имя обязано быть отвергнуто совокупной
// проверкой КАЖДОГО ресурса, а не только своим newtype'ом.
func TestAggregateValidate_DoesNotSwallowNameRefusal(t *testing.T) {
	const badName = "Bad_Name" // заглавные и подчёркивание формой не приняты

	cases := []struct {
		name string
		err  error
	}{
		{"Network", Network{Name: badName}.Validate()},
		{"Subnet", Subnet{Name: badName}.Validate()},
		{"NetworkInterface", NetworkInterface{Name: badName}.Validate()},
		{"SecurityGroup", SecurityGroup{Name: badName}.Validate()},
		{"RouteTable", RouteTable{Name: badName}.Validate()},
		{"Address", Address{Name: badName}.Validate()},
		{"CidrGroup", CidrGroup{Name: badName}.Validate()},
		{"Gateway", Gateway{Name: badName}.Validate()},
	}
	for _, c := range cases {
		if c.err == nil {
			t.Errorf("%s: негодное имя обязано быть отвергнуто совокупной проверкой", c.name)
		}
	}
}

// TestAggregateValidate_LawfulNamePasses — положительный контроль рядом с
// отрицанием: без него «отвергнуто» было бы неотличимо от «отвергается всё».
func TestAggregateValidate_LawfulNamePasses(t *testing.T) {
	const okName = "net-1"

	cases := []struct {
		name string
		err  error
	}{
		{"Network", Network{Name: okName}.Validate()},
		{"Subnet", Subnet{Name: okName}.Validate()},
		{"NetworkInterface", NetworkInterface{Name: okName}.Validate()},
		{"SecurityGroup", SecurityGroup{Name: okName}.Validate()},
		{"RouteTable", RouteTable{Name: okName}.Validate()},
		{"Address", Address{Name: okName}.Validate()},
		{"CidrGroup", CidrGroup{Name: okName}.Validate()},
		{"Gateway", Gateway{Name: okName}.Validate()},
	}
	for _, c := range cases {
		if c.err != nil {
			t.Errorf("%s: законное имя обязано проходить, got %v", c.name, c.err)
		}
	}
}

// TestAggregateValidate_EmptyNamePasses — пустое имя совокупной проверкой НЕ
// отвергается: на создании она исполняется раньше, чем существует
// идентификатор, и отказ здесь сделал бы создание без имени невозможным.
func TestAggregateValidate_EmptyNamePasses(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"Network", Network{}.Validate()},
		{"Subnet", Subnet{}.Validate()},
		{"NetworkInterface", NetworkInterface{}.Validate()},
		{"SecurityGroup", SecurityGroup{}.Validate()},
		{"RouteTable", RouteTable{}.Validate()},
		{"Address", Address{}.Validate()},
		{"CidrGroup", CidrGroup{}.Validate()},
		{"Gateway", Gateway{}.Validate()},
	}
	for _, c := range cases {
		if c.err != nil {
			t.Errorf("%s: пустое имя обязано проходить (подстановка умолчания — позже), got %v", c.name, c.err)
		}
	}
}
