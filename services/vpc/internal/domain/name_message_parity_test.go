// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain

import (
	"testing"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/status"

	corevalidate "github.com/PRO-Robotech/kacho/pkg/validate"
)

// Текст отказа по форме имени существует в ДВУХ местах, и это признанный форк.
//
// Домен не вправе звать `validate.Name`: та возвращает транспортную ошибку и
// тянет за собой gRPC, а домен обязан остаться stdlib-чистым (`architecture.md`).
// Форма при этом у обоих одна — `nameform.Form`, — а поясняющий хвост выписан
// дважды.
//
// Тон сообщения — часть контракта (`api-conventions.md`), поэтому расхождение
// было бы НАБЛЮДАЕМО арендатором: один и тот же негодный ввод отвечал бы разным
// текстом на создании (полоса домена) и на правке (полоса `validate`). Форк
// держится этой пробой, а не вниманием: она краснеет в день, когда любая из
// сторон поправится одна.
//
// Проба ИМПОРТИРУЕТ `validate` — но только она: это тестовый файл, в граф
// импортов прод-кода он не входит, и чистота домена от него не страдает
// (предикат: `go list -deps ./services/vpc/internal/domain | grep grpc` → пусто).
//
// Настоящее снятие форка — за владельцем `nameform`: текст обязан жить там же,
// где форма. Пока его там нет, живёт эта проба.
func TestNameFormViolationMessage_ByteIdenticalToTheUpdateLane(t *testing.T) {
	const malformed = "Bad_Name" // заглавные и подчёркивание формой не приняты

	err := corevalidate.Name("name", malformed)
	if err == nil {
		t.Fatalf("предпосылка пробы не выполнена: %q обязано отвергаться формой", malformed)
	}
	got := fieldViolationDesc(t, err, "name")

	if got != nameFormViolationMsg {
		t.Fatalf("тексты отказа разошлись.\n  полоса правки: %q\n  полоса домена: %q\n"+
			"Один и тот же ввод обязан получать один и тот же текст: тон сообщения — часть контракта.",
			got, nameFormViolationMsg)
	}
}

// TestNameFormViolationMessage_CarriesTheCanonForm — вторая половина: текст
// обязан НЕСТИ саму форму. Без неё проба выше зеленела бы и на двух одинаково
// пустых сообщениях.
func TestNameFormViolationMessage_CarriesTheCanonForm(t *testing.T) {
	if !contains(nameFormViolationMsg, corevalidate.NameForm) {
		t.Fatalf("текст отказа обязан называть форму %q, got %q",
			corevalidate.NameForm, nameFormViolationMsg)
	}
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) &&
		indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

// fieldViolationDesc — описание нарушения по имени поля из BadRequest-деталей.
func fieldViolationDesc(t *testing.T, err error, field string) string {
	t.Helper()
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("ошибка обязана быть gRPC-статусом, got %T", err)
	}
	for _, d := range st.Details() {
		br, isBR := d.(*errdetails.BadRequest)
		if !isBR {
			continue
		}
		for _, v := range br.GetFieldViolations() {
			if v.GetField() == field {
				return v.GetDescription()
			}
		}
	}
	t.Fatalf("отказ не назвал поле %q: %v", field, err)
	return ""
}
