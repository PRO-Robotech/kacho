// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware

// gRPC-звенья восстановления паники здесь больше не живут: они сведены в
// pkg/grpcsrv.UnaryPanicRecovery / StreamPanicRecovery — одно звено на
// платформу. Здешняя редакция отдавала клиенту «internal server error», тогда
// как четыре сервисных отдавали «internal error»: два разных ответа на одно и
// то же условие. Общим стал «internal error».
//
// HTTP-звено остаётся здесь: у него другая поверхность (REST-мультиплексор
// края) и нет общего двойника, поэтому перенос его в grpcsrv был бы неверным
// адресом, а не унификацией.

import (
	"log/slog"
	"net/http"
	"runtime/debug"
)

// HTTPRecovery — HTTP middleware: перехватывает panic, возвращает 500.
func HTTPRecovery(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("recovered from panic",
						"panic", rec,
						"stack", string(debug.Stack()),
					)
					http.Error(w, "internal server error", http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
