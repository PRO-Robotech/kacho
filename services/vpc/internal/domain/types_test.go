// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain_test

import (
	stderrors "errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
)

// isValidationError — true, если err — доменная *ValidationError (gRPC-агностично).
func isValidationError(err error) bool {
	var ve *domain.ValidationError
	return stderrors.As(err, &ve)
}

// TestValidate_ReturnsDomainValidationError — проверки домена возвращают
// stdlib-ошибку `*domain.ValidationError` (без зависимости от gRPC/corelib),
// несущую FieldViolation'ы. gRPC-трансляция — отдельным слоем (serviceerr).
//
// Имя входит в это утверждение НАРАВНЕ с остальными полями, хотя его форму домен
// больше не объявляет: форма приезжает из `pkg/validate/nameform` — пакета без
// транспорта, — а решение и его носитель остаются доменными.
func TestValidate_ReturnsDomainValidationError(t *testing.T) {
	// bad name → *domain.ValidationError с violation на поле "name".
	nerr := domain.RcNameVPC("Bad_Name").Validate()
	var nve *domain.ValidationError
	require.True(t, stderrors.As(nerr, &nve), "Validate() must return *domain.ValidationError")
	require.Len(t, nve.Violations, 1)
	assert.Equal(t, "name", nve.Violations[0].Field)

	// description over-limit → violation на "description".
	derr := domain.RcDescription(strings.Repeat("a", 257)).Validate()
	var ve *domain.ValidationError
	require.True(t, stderrors.As(derr, &ve), "Validate() must return *domain.ValidationError")
	require.Len(t, ve.Violations, 1)
	assert.Equal(t, "description", ve.Violations[0].Field)
	assert.NotEmpty(t, ve.Violations[0].Msg)

	// composite Network с bad description → тоже *domain.ValidationError.
	cerr := domain.Network{Description: domain.RcDescription(strings.Repeat("a", 257))}.Validate()
	require.True(t, stderrors.As(cerr, &ve))
	assert.Equal(t, "description", ve.Violations[0].Field)
}

// Unit-тесты Validate() у domain-newtypes: валидация живет в domain, а не в
// corevalidate.*; проверяем, что regex/length-контракт сохранен.

// TestRcNameVPC_Validate — форма имени взята из ЕДИНСТВЕННОГО объявления дерева
// (RFC 1123 DNS label). Три строки таблицы перевернулись относительно прежней
// редакции, и каждая — потому что своя, более широкая форма сервиса снята (#715):
// заглавные и подчёркивание больше не принимаются, ведущая цифра — принимается.
// Именно расхождение по этим трём осям и было предметом задачи.
//
// Тип ошибки — снова доменный `*domain.ValidationError`: форма приезжает из
// `pkg/validate/nameform`, пакета БЕЗ транспорта, поэтому домен судит имя сам и
// остаётся stdlib-чистым. Утверждается и исход, и носитель: транспортная ошибка
// отсюда означала бы, что слой снова протёк.
func TestRcNameVPC_Validate(t *testing.T) {
	cases := []struct {
		name    string
		v       string
		wantErr bool
	}{
		{"empty allowed", "", false},
		{"simple", "net-a", false},
		{"uppercase forbidden", "BadCAPS", true},
		{"underscore forbidden", "abc_def", true},
		{"starts with digit allowed", "1abc", false},
		{"starts with hyphen forbidden", "-abc", true},
		{"ends with hyphen forbidden", "abc-", true},
		{"63 chars OK", "a" + strings.Repeat("b", 62), false},
		{"64 chars forbidden", "a" + strings.Repeat("b", 63), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := domain.RcNameVPC(tc.v).Validate()
			if tc.wantErr {
				require.Error(t, err)
				assert.True(t, isValidationError(err), "want *domain.ValidationError")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestRcDescription_Validate(t *testing.T) {
	assert.NoError(t, domain.RcDescription("").Validate())
	assert.NoError(t, domain.RcDescription(strings.Repeat("a", 256)).Validate())
	err := domain.RcDescription(strings.Repeat("a", 257)).Validate()
	require.Error(t, err)
	assert.True(t, isValidationError(err), "want *domain.ValidationError")

	// UTF-8 rune count (не bytes): 256 ru-rune (по 2 байта) — ok.
	assert.NoError(t, domain.RcDescription(strings.Repeat("я", 256)).Validate())
	assert.Error(t, domain.RcDescription(strings.Repeat("я", 257)).Validate())
}

func TestLabelKey_Validate(t *testing.T) {
	assert.Error(t, domain.LabelKey("").Validate(), "empty key forbidden")
	assert.NoError(t, domain.LabelKey("env").Validate())
	assert.NoError(t, domain.LabelKey("tier-1").Validate())
	assert.Error(t, domain.LabelKey("Env").Validate(), "uppercase forbidden")
	assert.Error(t, domain.LabelKey("1env").Validate(), "starts with digit forbidden")
	assert.NoError(t, domain.LabelKey("env_v1").Validate())
	assert.NoError(t, domain.LabelKey("env.v1").Validate())
	assert.Error(t, domain.LabelKey(strings.Repeat("a", 64)).Validate(), ">63 chars forbidden")
}

func TestLabelVal_Validate(t *testing.T) {
	assert.NoError(t, domain.LabelVal("").Validate())
	assert.NoError(t, domain.LabelVal(strings.Repeat("a", 63)).Validate())
	assert.Error(t, domain.LabelVal(strings.Repeat("a", 64)).Validate(), ">63 chars forbidden")
}

func TestValidateLabels_Cardinality(t *testing.T) {
	// up to 64 — OK. Ключи фикс. 4 char: "k" + 3-char zero-padded index.
	good := map[string]string{}
	for i := 0; i < 64; i++ {
		key := "k"
		idx := i
		// 3-digit zero-pad
		for d := 100; d >= 1; d /= 10 {
			key += string(rune('0' + (idx/d)%10))
		}
		good[key] = "v"
	}
	require.Len(t, good, 64)
	require.NoError(t, domain.ValidateLabels(domain.LabelsFromMap(good)))

	// 65 — forbidden
	good["overflow"] = "v"
	assert.Error(t, domain.ValidateLabels(domain.LabelsFromMap(good)))
}

func TestNetwork_Validate_Composes(t *testing.T) {
	// happy
	n := domain.Network{
		Name:        domain.RcNameVPC("net1"),
		Description: domain.RcDescription("ok"),
		Labels:      domain.LabelsFromMap(map[string]string{"env": "prod"}),
	}
	assert.NoError(t, n.Validate())

	// bad name → *domain.ValidationError (заглавные и подчёркивание формой не приняты)
	bad := domain.Network{Name: domain.RcNameVPC("Bad_Name")}
	err := bad.Validate()
	require.Error(t, err)
	assert.True(t, isValidationError(err), "want *domain.ValidationError")
}

// NIC.Validate композирует Name/Description/Labels + MAC-формат (regex). MAC
// может быть пустым на Create (do-create аллоцирует его позже) — empty MAC
// проходит Validate.
func TestNetworkInterface_Validate_Composes(t *testing.T) {
	// happy — full
	n := domain.NetworkInterface{
		Name:        domain.RcNameVPC("nic1"),
		Description: domain.RcDescription("ok"),
		Labels:      domain.LabelsFromMap(map[string]string{"env": "prod"}),
		MAC:         "0e:11:22:33:44:55",
	}
	assert.NoError(t, n.Validate())

	// happy — empty MAC (allocate в do-create)
	n2 := domain.NetworkInterface{
		Name: domain.RcNameVPC("nic2"),
	}
	assert.NoError(t, n2.Validate())

	// bad name → *domain.ValidationError (заглавные и подчёркивание формой не приняты)
	bad := domain.NetworkInterface{Name: domain.RcNameVPC("Bad_Name")}
	err := bad.Validate()
	require.Error(t, err)
	assert.True(t, isValidationError(err), "want *domain.ValidationError")

	// bad MAC (uppercase) → *domain.ValidationError
	badMAC := domain.NetworkInterface{
		Name: domain.RcNameVPC("nic3"),
		MAC:  "0E:11:22:33:44:55",
	}
	err = badMAC.Validate()
	require.Error(t, err)
	assert.True(t, isValidationError(err), "want *domain.ValidationError")

	// bad MAC (wrong separator) → *domain.ValidationError
	badMAC2 := domain.NetworkInterface{
		Name: domain.RcNameVPC("nic4"),
		MAC:  "0e-11-22-33-44-55",
	}
	err = badMAC2.Validate()
	require.Error(t, err)

	// bad MAC (too short) → InvalidArgument
	badMAC3 := domain.NetworkInterface{
		Name: domain.RcNameVPC("nic5"),
		MAC:  "0e:11:22:33:44",
	}
	err = badMAC3.Validate()
	require.Error(t, err)
}
