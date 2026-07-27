// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package authzfilter реализует per-object фильтрацию видимости для kacho-storage.
//
// Каждый публичный List use-case (Volume / Snapshot / Image) читает СТРАНИЦУ строк
// из своей БД курсором и затем спрашивает kacho-iam, какие id этой страницы видимы
// вызывающему subject'у (`AuthorizeService.BatchCheck`, батчи ≤100). Это даёт
// настоящую per-object видимость: видимый набор равен Check-allow набору
// (read==enforce), а стоимость пропорциональна СТРАНИЦЕ, а не популяции типа.
//
// # Что здесь было сломано (дыра видимости)
//
// storage единственным из сервисов НЕ фильтровал списки по объектам вообще: гейт
// заканчивался на project-tier `viewer` Check в api-gateway плюс сужении SQL по
// `project_id`. Следствие: ЛЮБОЙ член проекта видел КАЖДЫЙ том, снимок и образ
// проекта — независимо от того, выдан ли ему per-object грант (over-show,
// CWE-862 / OWASP A01). Per-object грант при этом энфорсился на Get/Update/Delete,
// то есть List противоречил остальным путям того же ресурса.
//
// # Почему не ListObjects (это был баг в соседях, не повторяем)
//
// Соседние сервисы когда-то спрашивали `AuthorizeService.ListObjects` —
// «перечисли ВСЕ объекты этого типа, которые subject'у можно» — и сужали SQL до
// полученного набора. У OpenFGA ListObjects ЖЁСТКИЙ server-side предел
// (`OPENFGA_LIST_OBJECTS_MAX_RESULTS`, default 1000) и НЕТ continuation-token'а:
// перечисление молча возвращает произвольный 1000-id префикс. Предел действует на
// ТИП В СТОРЕ (cluster-wide), а не на тенанта, поэтому на долгоживущем сторе
// собственный ресурс тенанта выпадал за префикс и становился НЕВИДИМЫМ навсегда,
// хотя строка есть и грант есть. Просьба `max_results` предел не поднимает — это
// лишь client-side trim уже усечённого ответа.
//
// Лечится не поднятием предела (он внешний и всё равно конечен), а формой вопроса:
// вместо «перечисли вселенную и найди в ней» — «можно ли этому subject'у ЭТОТ
// объект», батчем на страницу. Поэтому здесь такого вопроса нет ни на одном пути.
//
// # Предикат видимости
//
// Видимость — РОВНО `viewer` (read-tier: direct usersets ∪ editor ∪ admin): то же
// отношение, на котором permission-catalog энфорсит per-RPC Check на `Get` (и
// которое зеркалит internal/check/permission_map.go), поэтому «видно в списке» ⟺
// «Get разрешён» (read==enforce).
//
// Предикат НЕЛЬЗЯ расширять: список отдаёт полные строки, поэтому любое отношение
// сверх Get-овского кладёт на страницу объект, который Get затем не отдаст, —
// вызывающий узнаёт идентификатор недоступного ему ресурса. Ровно это и делал
// прежний союз `viewer ∪ v_list`; подробности и замок — filter.go
// (visibilityRelations) и filter_get_parity_test.go.
//
// # Дисциплина фильтра
//
// Fail-closed по умолчанию: iam недоступен → Unavailable (страница НЕ отдаётся
// нефильтрованной). KACHO_STORAGE_LIST_FILTER_FAIL_OPEN=true переключает в
// degraded-mode — страница отдаётся как есть + audit-WARN на каждый fail-open.
// Публичный каталог DiskType через фильтр не проходит вовсе — это ambient
// cluster-scoped read (`viewer` на cluster-синглтоне, per-object грантов нет).
//
// Configurable env vars (все KACHO_STORAGE_LIST_FILTER_*):
//   - grpc address      — переиспользует KACHO_STORAGE_AUTHZ_IAM_GRPC_ADDR.
//   - ENABLED           — master switch (production boot-guard требует true).
//   - TIMEOUT_MS        — per-call deadline ОДНОГО BatchCheck (default 1000).
//   - CACHE_TTL_MS      — TTL положительного per-object вердикта (default 5000).
//   - CACHE_MAX_ENTRIES — bound кеша, LRU-вытеснение (default 10000).
//   - FAIL_OPEN         — на ошибке iam отдать нефильтрованную страницу; иначе fail-closed.
//
// Clean Architecture: пакет — outbound adapter (ходит в iam по gRPC) и port
// (потребляется List use-case'ами). Живёт в internal/, как одноимённые пакеты
// vpc/compute/nlb.
package authzfilter
