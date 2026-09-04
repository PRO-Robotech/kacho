// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: Apache-2.0

package authz

// hide_existence_parity_internal_test.go — the two places that refuse a read on
// a foreign object must speak with one voice.
//
// The api-gateway refuses such a read one hop before the service does, and both
// answer NOT_FOUND with the owning service's own miss text. If the two tables
// drift apart, one of them stops matching the owner and becomes the tell that
// distinguishes "not yours" from "not there" — the very oracle both are written
// to close. Neither copy can import the other (the gateway's table lives under
// its own internal/ tree), so the agreement is checked at the source level: the
// gateway's map literal is parsed and compared entry by entry.
//
// A failure here is not a formality: it means one of the two answers no longer
// matches the backend.

import (
	"testing"
)

// parseGatewayFormats — записи таблицы края.
//
// Разрешение идёт ПО ПАКЕТУ (`parseFormatsInPackage`), а не по имени файла:
// прежняя редакция пинила координату `permission_denied_response.go`, и перенос
// объявления в соседний файл того же пакета — законная правка — давал `t.Fatalf`,
// то есть «не выполнилось», поданное как красное. Собственный текст того отказа
// это признавал («move the guard with it»), и стража, отвечающего так, снимают
// как вечно красный — вместе с единственным, что байт-идентичность двух текстов
// держит (задача #1946).
//
// Второго разбора здесь НЕ заводится: разбор один, и он же доказан инъекцией
// (`hide_existence_parity_scope_test.go`).
func parseGatewayFormats(t *testing.T) map[string]string {
	t.Helper()
	formats, read, err := parseFormatsInPackage(gatewayTableDir)
	if err != nil {
		t.Fatalf("таблица скрытия существования у края не разрешена: %v", err)
	}
	t.Logf("перепись: файлов пакета края прочитано %d · записей таблицы %d", read, len(formats))
	return formats
}

// TestHideExistenceFormats_MatchTheGateway — same keys, same texts.
func TestHideExistenceFormats_MatchTheGateway(t *testing.T) {
	gateway := parseGatewayFormats(t)

	for objectType, gatewayText := range gateway {
		ours, ok := hideExistenceNotFoundFormats[objectType]
		if !ok {
			t.Errorf("object type %q is refused with the owner's text at the gateway but falls back to the neutral text here — the two answers differ", objectType)
			continue
		}
		if ours != gatewayText {
			t.Errorf("object type %q: gateway answers %q, this interceptor answers %q — one of them no longer matches the owning service", objectType, gatewayText, ours)
		}
	}
	for objectType, ourText := range hideExistenceNotFoundFormats {
		if gatewayText, ok := gateway[objectType]; !ok {
			t.Errorf("object type %q is known here (%q) but not at the gateway — the gateway's refusal for it is the neutral text and is therefore distinguishable", objectType, ourText)
		} else if gatewayText != ourText {
			t.Errorf("object type %q: texts differ (here %q, gateway %q)", objectType, ourText, gatewayText)
		}
	}
}

// TestHideExistenceMessage_ShapeOfTheFallback — the two cases where byte-identity
// is impossible must yield the least-informative answer, never the internal type.
func TestHideExistenceMessage_ShapeOfTheFallback(t *testing.T) {
	for _, tc := range []struct {
		name       string
		objectType string
		objectID   string
		want       string
	}{
		{"known type and id", "vpc_subnet", "sub00000000000000abc", "Subnet sub00000000000000abc not found"},
		{"unknown type", "something_new", "xyz00000000000000abc", "not found"},
		{"wildcard id", "vpc_subnet", "*", "not found"},
		{"absent id", "vpc_subnet", "", "not found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := hideExistenceMessage(tc.objectType, tc.objectID); got != tc.want {
				t.Fatalf("hideExistenceMessage(%q,%q) = %q; want %q", tc.objectType, tc.objectID, got, tc.want)
			}
		})
	}
}
