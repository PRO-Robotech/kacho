// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain_test

import (
	"strings"
	"testing"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// violation — текст нарушения по названному полю. Сообщение отказа общее по
// контракту («invalid argument»), а имя поля и причина живут в
// google.rpc.BadRequest-деталях, поэтому судить надо их, а не err.Error().
func violation(err error, field string) string {
	st, ok := status.FromError(err)
	if !ok {
		return ""
	}
	for _, d := range st.Details() {
		br, ok := d.(*errdetails.BadRequest)
		if !ok {
			continue
		}
		for _, v := range br.GetFieldViolations() {
			if v.GetField() == field {
				return v.GetDescription()
			}
		}
	}
	return ""
}

// ── Одна форма имени на дерево (#715) ────────────────────────────────────────
//
// Здесь стояла ВТОРАЯ форма — `^[a-z][-a-z0-9]{1,61}[a-z0-9]$`, — расходившаяся
// с канонической по двум осям: минимальная длина 3 (имя из одного-двух символов
// выразить было нельзя) и запрет ведущей цифры. Ни та, ни другая ось нигде не
// обоснована; расхождение накопилось молча, как и три остальных.
//
// Требование имени nlb НЕ ослабляется: пустая строка по-прежнему отвергается
// отдельным сообщением, и это утверждается ниже.

// TestLbNameAcceptsWhatTheCanonAccepts — короткое имя и ведущая цифра законны.
func TestLbNameAcceptsWhatTheCanonAccepts(t *testing.T) {
	t.Parallel()
	for _, n := range []domain.LbName{"a", "ab", "1edge", "9", "edge-public",
		domain.LbName("a" + strings.Repeat("b", 61) + "c")} {
		if err := n.Validate(); err != nil {
			t.Errorf("LbName(%q) отвергнуто: %v", n, err)
		}
	}
}

// TestLbNameStillRejectsNonNames — отрицание в паре с положительным выше:
// расширение формы не открыло её ни для регистра, ни для подчёркивания, ни для
// дефиса по краям, ни для длины сверх 63.
func TestLbNameStillRejectsNonNames(t *testing.T) {
	t.Parallel()
	for _, n := range []domain.LbName{"Edge", "edge_public", "edge!", "edge-", "-edge",
		domain.LbName("a" + strings.Repeat("b", 62) + "c")} {
		if err := n.Validate(); err == nil {
			t.Errorf("LbName(%q) принято, want отказ", n)
		}
	}
}

// TestLbNameStillRequiresAName — nlb требует имя на создании, и сведение к
// канону этого НЕ ослабляет: пустая строка отвергается своим сообщением
// («name is required»), а не общим отказом по форме — вызывающий, забывший
// поле, и вызывающий, приславший `My_Name`, ошиблись по-разному.
func TestLbNameStillRequiresAName(t *testing.T) {
	t.Parallel()
	err := domain.LbName("").Validate()
	if err == nil {
		t.Fatal("пустое имя принято — nlb требует имя на создании")
	}
	if got := violation(err, "name"); !strings.Contains(got, "required") {
		t.Fatalf("нарушение по полю name = %q, want отдельное сообщение о требуемом поле", got)
	}
}
