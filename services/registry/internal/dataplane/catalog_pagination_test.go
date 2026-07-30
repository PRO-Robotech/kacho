// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package dataplane

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

// catalog_pagination_test.go — TEST-ONLY (ban #13): BVA/equivalence unit-тесты чистых
// catalog-pagination хелперов (parseCatalogPageSize / catalogWindow / decode-encode
// cursor). Локают клампинг page-size к [1..max] (CWE-770 bound Check-count), опаковость
// offset-курсора и fail-safe разбор битого/вне-диапазона курсора (без leak/паники).
// Прод-код не трогается.

// TestCatalog_parseCatalogPageSize_ClampsToBounds — BVA на `n=`: ОТСУТСТВУЮЩЕЕ значение →
// дефолтное окно catalogDefaultPageSize; битое/≤0/>max → потолок catalogMaxPageSize;
// валидное в [1..max] → как есть. Кламп не даёт клиенту снять границу числа per-repo
// authz-Check (self-amplifying DoS), а дефолт не даёт непараметризованному запросу
// заказывать эту границу целиком (см. TestCatalog_NoPageSize_UsesDefaultWindow_NotCeiling).
//
// Прежняя редакция ждала «empty → max» — она закрепляла схлопывание двух разных случаев
// («клиент не просил» и «клиент просил слишком много») в одно значение.
func TestCatalog_parseCatalogPageSize_ClampsToBounds(t *testing.T) {
	cases := []struct {
		name, in string
		want     int
	}{
		{"empty → default (не потолок)", "", catalogDefaultPageSize},
		{"one", "1", 1},
		{"mid", "2", 2},
		{"max exact", "1000", catalogMaxPageSize},
		{"over max → max", "1001", catalogMaxPageSize},
		{"far over → max", "1000000", catalogMaxPageSize},
		{"zero → max", "0", catalogMaxPageSize},
		{"negative → max", "-5", catalogMaxPageSize},
		{"garbage → max", "abc", catalogMaxPageSize},
		{"float garbage → max", "1.5", catalogMaxPageSize},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseCatalogPageSize(c.in)
			require.Equal(t, c.want, got)
			require.GreaterOrEqual(t, got, 1, "результат всегда ≥1")
			require.LessOrEqual(t, got, catalogMaxPageSize, "результат всегда ≤ потолка")
		})
	}
}

// TestCatalog_catalogWindow_Slicing — окно из отсортированных имён по offset-курсору:
// первая страница, средняя (nextOffset), и хвост (more=false).
func TestCatalog_catalogWindow_Slicing(t *testing.T) {
	names := []string{"a", "b", "c", "d", "e"}

	win, next, more := catalogWindow(names, "", 2)
	require.Equal(t, []string{"a", "b"}, win, "первая страница = первые n")
	require.Equal(t, 2, next, "nextOffset = позиция после окна")
	require.True(t, more, "за окном есть ещё имена")

	win, next, more = catalogWindow(names, encodeCatalogCursor(2), 2)
	require.Equal(t, []string{"c", "d"}, win, "следующая страница продолжает с offset")
	require.Equal(t, 4, next)
	require.True(t, more)

	win, _, more = catalogWindow(names, encodeCatalogCursor(4), 2)
	require.Equal(t, []string{"e"}, win, "хвост короче pageSize")
	require.False(t, more, "хвост каталога → more=false")
}

// TestCatalog_catalogWindow_CursorClampsFailSafe — вне-диапазона offset-курсор
// клампится в [0..len] (fail-safe рестарт, без паники/leak): отрицательный → с начала,
// сверх длины → пустое окно, more=false.
func TestCatalog_catalogWindow_CursorClampsFailSafe(t *testing.T) {
	names := []string{"a", "b", "c"}

	// отрицательный offset → clamp к 0 (start<0 ветка).
	win, _, more := catalogWindow(names, encodeCatalogCursor(-1), 2)
	require.Equal(t, []string{"a", "b"}, win, "отрицательный курсор → рестарт с начала")
	require.True(t, more)

	// offset за длиной → clamp к len → пустое окно (start>len ветка).
	win, next, more := catalogWindow(names, encodeCatalogCursor(999), 2)
	require.Empty(t, win, "offset сверх длины → пустое окно")
	require.Equal(t, len(names), next, "nextOffset клампится к len")
	require.False(t, more, "за концом каталога больше нет страниц")
}

// TestCatalog_decodeCursor_FailSafe — разбор опакового offset-курсора: валидный
// round-trip; пусто/битый-base64/не-число → 0 (безопасный рестарт с начала, без паники).
func TestCatalog_decodeCursor_FailSafe(t *testing.T) {
	// round-trip нескольких значений (encode→decode идемпотентен).
	for _, n := range []int{0, 1, 7, 42, 1000} {
		require.Equal(t, n, decodeCatalogCursor(encodeCatalogCursor(n)), "encode/decode round-trip offset=%d", n)
	}

	require.Equal(t, 0, decodeCatalogCursor(""), "пустой курсор → 0")
	require.Equal(t, 0, decodeCatalogCursor("!!!not-base64!!!"), "битый base64 → 0 (fail-safe)")
	// валидный base64, но декодированное содержимое — не число → 0.
	nonNumeric := base64.RawURLEncoding.EncodeToString([]byte("not-a-number"))
	require.Equal(t, 0, decodeCatalogCursor(nonNumeric), "base64 не-числа → 0 (fail-safe)")
}

// TestCatalog_encodeCursor_IsOpaqueOffset — курсор кодирует ТОЛЬКО позицию (offset),
// не несёт сырых имён репо (existence-oracle guard): для двух разных каталогов с одним
// offset курсор идентичен, и в нём нет ни одного repo-имени.
func TestCatalog_encodeCursor_IsOpaqueOffset(t *testing.T) {
	c := encodeCatalogCursor(2)
	require.NotContains(t, c, "reg-", "курсор не эхает registry-префикс")
	require.NotContains(t, c, "/", "курсор не несёт repo-путь")
	require.Equal(t, c, encodeCatalogCursor(2), "один offset → один курсор (зависит только от позиции)")
	require.NotEqual(t, encodeCatalogCursor(2), encodeCatalogCursor(3), "разные offset → разные курсоры")
}

// TestCatalog_NoPageSize_UsesDefaultWindow_NotCeiling — GET /v2/_catalog БЕЗ `n=`
// обязан обработать ДЕФОЛТНОЕ окно, а не потолок.
//
// Предмет — стоимость запроса, который клиент не параметризовал. Каталог движка
// ГЛОБАЛЬНЫЙ и межтенантный, а фильтр прав поштучный: один вопрос в хранилище прав на
// КАЖДОЕ имя окна. Пока отсутствующий `n=` означал потолок, любой аутентифицированный
// docker-клиент одним пустым GET заказывал до потолка обращений в общую зависимость,
// которая fail-closed гейтит каждый запрос всех сервисов; тенант, которому видны два
// репозитория, платил authz за чужие имена, а цикл таких запросов давал тысячи проверок
// в секунду. Само отношение «страница = N проверок» — принятый платформенный дизайн и
// здесь не меняется; меняется только N по умолчанию.
//
// RED до фикса: 1000 вопросов на пустой GET.
func TestCatalog_NoPageSize_UsesDefaultWindow_NotCeiling(t *testing.T) {
	names := make([]string, catalogMaxPageSize)
	for i := range names {
		names[i] = "reg-A/app" + strconv.Itoa(i)
	}
	az := &fakeAuthz{} // allow-all: считаем ЧИСЛО вопросов, а не исход
	be := &fakeBackend{catalog: names}
	h := newTestHandler(&fakeVerifier{subject: "sva-ci"}, az, be, &fakeForwarder{}, &fakeRepoReg{})

	rec := doReq(h, http.MethodGet, "/v2/_catalog", true)
	require.Equal(t, http.StatusOK, rec.Code)

	require.Equal(t, catalogDefaultPageSize, len(az.checkedObjects()),
		"число вопросов в хранилище прав на пустой GET = дефолтное окно, а не потолок")
	require.Equal(t, catalogDefaultPageSize, len(decodeCatalogRepositories(t, rec)),
		"в ответе — дефолтное окно")
	require.NotEmpty(t, rec.Header().Get("Link"),
		"за окном остались имена → клиент продолжает по Link (ничего не теряется)")
}

// TestCatalog_ExplicitPageSize_StillReachesCeiling — обратная сторона инъекции: клиент,
// которому нужно больше, просит явно и получает до ПОТОЛКА. Без этого кейса гейт
// запрещал бы форму (большое окно), а не существо (большое окно, которого никто не
// просил).
func TestCatalog_ExplicitPageSize_StillReachesCeiling(t *testing.T) {
	names := make([]string, catalogMaxPageSize)
	for i := range names {
		names[i] = "reg-A/app" + strconv.Itoa(i)
	}
	az := &fakeAuthz{}
	be := &fakeBackend{catalog: names}
	h := newTestHandler(&fakeVerifier{subject: "sva-ci"}, az, be, &fakeForwarder{}, &fakeRepoReg{})

	rec := doReq(h, http.MethodGet, "/v2/_catalog?n="+strconv.Itoa(catalogMaxPageSize), true)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, catalogMaxPageSize, len(az.checkedObjects()), "явный запрос доходит до потолка")
	require.Equal(t, catalogMaxPageSize, len(decodeCatalogRepositories(t, rec)))
}

// decodeCatalogRepositories — список имён из тела /v2/_catalog.
func decodeCatalogRepositories(t *testing.T, rec *httptest.ResponseRecorder) []string {
	t.Helper()
	var body struct {
		Repositories []string `json:"repositories"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body.Repositories
}
