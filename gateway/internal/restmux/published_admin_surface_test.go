// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package restmux

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/listenerorigin"
)

// publishedAdminSurface — все одиннадцать пар (метод, путь) административной
// поверхности пула адресов, опубликованной ADM-1 S1.
//
// Пути НЕ новые: публичный глагол встаёт на тот же канонический адрес, что нёс
// внутренний (решение владельца, приёмка ADM-1 Р11) — второго адреса у ресурса
// быть не должно. Поэтому здесь перечислены ровно те пары, что стоят в
// `internal_address_pool_service.proto`.
var publishedAdminSurface = []struct{ method, path string }{
	{"GET", "/vpc/v1/addressPools"},
	{"POST", "/vpc/v1/addressPools"},
	{"GET", "/vpc/v1/addressPools/apl0000000000000001"},
	{"PATCH", "/vpc/v1/addressPools/apl0000000000000001"},
	{"DELETE", "/vpc/v1/addressPools/apl0000000000000001"},
	{"POST", "/vpc/v1/addressPools/apl0000000000000001:addCidrBlocks"},
	{"POST", "/vpc/v1/addressPools/apl0000000000000001:removeCidrBlocks"},
	{"GET", "/vpc/v1/addressPools/apl0000000000000001/addresses"},
	{"GET", "/vpc/v1/addressPools/apl0000000000000001/utilization"},
	{"POST", "/vpc/v1/networks/net-1/addressPoolBinding"},
	{"DELETE", "/vpc/v1/networks/net-1/addressPoolBinding"},
}

// serveProbe гоняет одну пару через диспетчер и возвращает код и тело.
// Тело grpc-gateway при недозвоне цитирует АДРЕС бэкенда — на нём и строится
// различение публичного бэкенда от административного.
func serveProbe(t *testing.T, h http.Handler, method, path string, internal bool) (int, string) {
	t.Helper()
	var body *strings.Reader
	switch method {
	case http.MethodPost, http.MethodPatch, http.MethodPut:
		body = strings.NewReader("{}")
	default:
		body = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, body)
	req.Header.Set("Content-Type", "application/json")
	if internal {
		req = req.WithContext(listenerorigin.WithInternal(req.Context()))
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// TestExternalListener_PublishedAdminPathReachesPublicBackendOnly — ADM-1 S1,
// сценарии 01/03 на уровне края: наблюдение владельца («раздел администрирования
// отвечает 404») закрывается ЗДЕСЬ.
//
// # Что именно утверждается — и почему не «код 200»
//
// Достижимость снаружи равна ОДНОМУ факту: зарегистрирована ли пара (метод,
// путь) на `publicMux`. Классификатор пути снаружи не решает ничего — обе ветки
// диспетчера на внешнем слушателе ведут в один и тот же `publicMux` (приёмка
// ADM-1 Р11; проверено чтением тела диспетчера, а не только решающей функции).
// Поэтому утверждение строится по АДРЕСУ бэкенда, который цитирует тело ответа:
// адрес подделать нечем, а код — можно (недозвон и отсутствие маршрута дают
// разные коды по причинам, к предмету не относящимся).
//
// Три половины, и все три обязательны:
//
//	(1) снаружи запрос ДОХОДИТ до публичного бэкенда vpc — это и есть «раздел
//	    администрирования перестал отвечать 404»;
//	(2) снаружи он НИКОГДА не доходит до административного бэкенда — расщепление
//	    чистое, публикация не открыла внутреннюю поверхность;
//	(3) изнутри он по-прежнему доходит до АДМИНИСТРАТИВНОГО бэкенда — внутренний
//	    глагол жив, окно расширения открыто (S1 его не снимает).
//
// Без (3) «снаружи публичный» удовлетворялось бы снятием внутреннего маршрута
// вовсе, то есть стадией S3, которую эта проба обязана отличать от S1.
func TestExternalListener_PublishedAdminPathReachesPublicBackendOnly(t *testing.T) {
	addrs, adminLiteral := splitAddrs(t)
	const publicLiteral = "127.0.0.1:1"
	h, err := NewMux(context.Background(), addrs, nil, nil)
	if err != nil {
		t.Fatalf("NewMux: %v", err)
	}

	// Положительный контроль предиката: заведомо публичный путь снаружи
	// действительно цитирует публичный литерал. Без него «доходит до публичного
	// бэкенда» было бы утверждением, годность которого ничем не показана, — и
	// проба зеленела бы на любом теле, случайно содержащем этот адрес.
	if _, body := serveProbe(t, h, "GET", "/vpc/v1/networks", false); !strings.Contains(body, publicLiteral) {
		t.Fatalf("положительный контроль провален: заведомо публичный GET /vpc/v1/networks не цитирует "+
			"публичный литерал %q — предикат пробы негоден, тело: %s", publicLiteral, body)
	}

	for _, tc := range publishedAdminSurface {
		t.Run("EXT "+tc.method+" "+tc.path, func(t *testing.T) {
			code, body := serveProbe(t, h, tc.method, tc.path, false)
			if code == http.StatusNotFound {
				t.Fatalf("опубликованный административный путь %s %s на ВНЕШНЕМ слушателе: 404 — "+
					"раздел администрирования консоли остаётся недостижимым (наблюдение владельца, issue #447)",
					tc.method, tc.path)
			}
			if strings.Contains(body, adminLiteral) {
				t.Fatalf("опубликованный путь %s %s на ВНЕШНЕМ слушателе дошёл до АДМИНИСТРАТИВНОГО бэкенда "+
					"(КРИТИЧНО: публикация открыла внутреннюю поверхность вместо публичной): %s",
					tc.method, tc.path, body)
			}
			if !strings.Contains(body, publicLiteral) {
				t.Fatalf("опубликованный путь %s %s на ВНЕШНЕМ слушателе не дошёл до ПУБЛИЧНОГО бэкенда vpc "+
					"(код %d): %s", tc.method, tc.path, code, body)
			}
		})

		t.Run("INT "+tc.method+" "+tc.path, func(t *testing.T) {
			code, body := serveProbe(t, h, tc.method, tc.path, true)
			if code == http.StatusNotFound {
				t.Fatalf("%s %s на ВНУТРЕННЕМ слушателе: 404 — внутренний глагол снят вместе с публикацией; "+
					"S1 его не снимает (это стадия S3), и окно расширения обязано быть открыто",
					tc.method, tc.path)
			}
			if !strings.Contains(body, adminLiteral) {
				t.Fatalf("%s %s на ВНУТРЕННЕМ слушателе не дошёл до АДМИНИСТРАТИВНОГО бэкенда — "+
					"публичный обработчик перехватил внутренний путь на внутреннем mux'е "+
					"(код %d): %s", tc.method, tc.path, code, body)
			}
		})
	}
}
