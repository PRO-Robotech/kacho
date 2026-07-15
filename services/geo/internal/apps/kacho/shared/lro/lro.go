// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package lro — общие константы Long-Running Operations каталога kacho-geo.
package lro

// OperationPrefix — 3-символьный префикс operation-id каталога geo. По нему
// api-gateway opsproxy маршрутизирует OperationService.Get/Cancel в backend
// kacho-geo (parity с per-domain operation-префиксами прочих сервисов). Region и
// Zone делят единый geo-префикс (один backend, один пул операций).
const OperationPrefix = "geo"
