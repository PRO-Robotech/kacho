// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package serviceerr

import (
	"testing"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// fieldsOf — пути полей так, как их увидит клиент-автомат.
func fieldsOf(t *testing.T, err error) []string {
	t.Helper()
	var got []string
	for _, d := range status.Convert(err).Details() {
		if br, ok := d.(*errdetails.BadRequest); ok {
			for _, v := range br.GetFieldViolations() {
				got = append(got, v.GetField())
			}
		}
	}
	return got
}

// Один нарушитель — тон совпадает с InvalidArg дословно: текст отказа часть
// контракта, и появление второго конструктора не вправе его сдвинуть.
func TestInvalidArgFields_SingleViolationKeepsTheToneOfInvalidArg(t *testing.T) {
	a := InvalidArgFields(FieldViolation{Field: "boot_source.image_kind", Desc: "bootSource.imageKind is output-only"})
	b := InvalidArg("boot_source.image_kind", "bootSource.imageKind is output-only")

	if status.Code(a) != codes.InvalidArgument {
		t.Fatalf("код не InvalidArgument: %v", status.Code(a))
	}
	if got, want := status.Convert(a).Message(), status.Convert(b).Message(); got != want {
		t.Errorf("тон разошёлся с InvalidArg: %q против %q", got, want)
	}
	if got := fieldsOf(t, a); len(got) != 1 || got[0] != "boot_source.image_kind" {
		t.Errorf("путь поля не тот: %v", got)
	}
}

// Несколько нарушителей — каждый своим путём, в порядке, заданном вызывающим.
// Порядок утверждается намеренно: клиент сверяет перечень, и перестановка
// читалась бы как другой ответ.
func TestInvalidArgFields_EveryViolationCarriesItsOwnPath(t *testing.T) {
	err := InvalidArgFields(
		FieldViolation{Field: "boot_source.name", Desc: "bootSource.name is output-only"},
		FieldViolation{Field: "boot_source.image_kind", Desc: "bootSource.imageKind is output-only"},
	)
	if got := fieldsOf(t, err); len(got) != 2 || got[0] != "boot_source.name" || got[1] != "boot_source.image_kind" {
		t.Errorf("пути полей не те либо порядок не сохранён: %v", got)
	}
	msg := status.Convert(err).Message()
	if msg != "bootSource.name is output-only; bootSource.imageKind is output-only" {
		t.Errorf("сообщение не соединило правила: %q", msg)
	}
}

// Ноль нарушителей — отказа нет. Иначе конструктор произвёл бы отказ без
// предмета, и вызывающий, забывший проверить непустоту, отверг бы законный вход.
func TestInvalidArgFields_NoViolationsIsNoRefusal(t *testing.T) {
	if err := InvalidArgFields(); err != nil {
		t.Fatalf("отказ без нарушителя: %v", err)
	}
}
