// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"fmt"
	"log/slog"
	"maps"

	"github.com/PRO-Robotech/kacho/gateway/internal/clients"
	"github.com/PRO-Robotech/kacho/gateway/internal/config"
	"github.com/PRO-Robotech/kacho/gateway/internal/proxy"
)

// readinessAuthorizeKey — имя, под которым `/readyz` отвечает о соединении
// решения о праве. В теле ответа — только имя и состояние, адреса нет.
const readinessAuthorizeKey = "iamAuthorize"

// readinessTargets — что опрашивает `/readyz` края и что из этого критично.
//
// Критичны оба адреса службы доступа (`iam`, `iamInternal`): она фронтит
// authN на каждом запросе. Решение о праве край спрашивает ТРЕТЬИМ, своим
// соединением (`clients.NewIAMAuthorizeClient`), которого в карте бэкендов нет.
// Пока его не опрашивали, после переката службы прав край отвечал «готов», а на
// первый вопрос о праве — 503 «решение не принято» (kacho#3200). Теперь это
// соединение — цель готовности, опрашиваемая тем же `Health/Check`, что и
// остальные.
//
// Критично оно ровно тогда, когда неотвеченный вопрос о праве оборачивается
// отказом, — на закрытом исходе. На открытом (`failOpen`) запрос проходит и без
// ответа, готовность от соединения не зависит, но его состояние видно в теле.
// Клиента нет (проверка прав выключена) — цели нет.
//
// Карта бэкендов не меняется: её же читают маршрутизатор и прочие звенья.
func readinessTargets(backends proxy.Backends, authorize *clients.IAMAuthorizeClient, failOpen bool) (proxy.Backends, map[string]bool) {
	critical := map[string]bool{"iam": true, "iamInternal": true}
	if authorize == nil || authorize.Conn() == nil {
		return backends, critical
	}
	targets := make(proxy.Backends, len(backends)+1)
	maps.Copy(targets, backends)
	targets[readinessAuthorizeKey] = authorize.Conn()
	critical[readinessAuthorizeKey] = !failOpen
	return targets, critical
}

// subjectClientOf — клиент субъекта (вход по токену, базовая полоса, проверка
// администратора системы) над соединением `iamInternal` карты бэкендов — тем,
// что опрашивает `/readyz` (kacho#3284). Соединение, ведущее не по адресу
// внутреннего слушателя службы доступа, — отказ старта: иначе вход молча шёл
// бы одним адресом, а готовность спрашивала бы о другом.
func subjectClientOf(cfg config.Config, backends proxy.Backends, logger *slog.Logger) (*clients.IAMSubjectClient, error) {
	conn := backends["iamInternal"]
	if conn == nil {
		return nil, fmt.Errorf("backend %q has no connection", "iamInternal")
	}
	if conn.Target() != cfg.IAMInternalAddr {
		return nil, fmt.Errorf("connection for backend %q targets %q, address map says %q",
			"iamInternal", conn.Target(), cfg.IAMInternalAddr)
	}
	return clients.NewIAMSubjectClient(conn, logger)
}
