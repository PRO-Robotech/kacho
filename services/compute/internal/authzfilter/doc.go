// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package authzfilter реализует per-object фильтрацию видимости для kacho-compute.
//
// Каждый публичный List-handler (Instance / Disk / Image / Snapshot) читает
// СТРАНИЦУ строк из своей БД курсором и затем спрашивает kacho-iam, какие id этой
// страницы видимы вызывающему subject'у (`AuthorizeService.BatchCheck`, батчи
// ≤100). Это даёт настоящую per-object видимость: видимый набор равен Check-allow
// набору (read==enforce), а стоимость пропорциональна СТРАНИЦЕ, а не популяции
// типа. Тем же прямым вопросом авторизуется `Image.GetLatestByFamily` — RPC,
// у которого целевой id неизвестен до резолва family, поэтому per-object gate на
// interceptor'е невозможен.
//
// # Почему не ListObjects (важно — это был баг)
//
// Раньше фильтр спрашивал `AuthorizeService.ListObjects` — «перечисли ВСЕ объекты
// этого типа, которые subject'у можно» — и сужал SQL до полученного набора
// (`WHERE id = ANY(...)`). У OpenFGA ListObjects есть ЖЁСТКИЙ server-side предел
// (`OPENFGA_LIST_OBJECTS_MAX_RESULTS`, default 1000) и НЕТ continuation-token'а:
// перечисление молча возвращает произвольный 1000-id префикс. Предел действует на
// ТИП В СТОРЕ (cluster-wide), а не на тенанта, поэтому на долгоживущем сторе
// (>1000 объектов типа) собственный ресурс тенанта выпадал за префикс и становился
// НЕВИДИМЫМ навсегда: List → нет в выдаче, GetLatestByFamily → NotFound, при том
// что строка есть, грант есть, а Update/Delete (они задают ПРЯМОЙ per-object
// вопрос через per-RPC interceptor) продолжали работать. Просьба
// `max_results=10000` предел не поднимает — это лишь client-side trim уже
// усечённого ответа.
//
// Лечится не поднятием предела (он внешний и всё равно конечен), а формой
// вопроса: вместо «перечисли вселенную и найди в ней» — «можно ли этому subject'у
// ЭТОТ объект», батчем на страницу.
//
// # Предикат видимости не изменился
//
// `ListObjects` по определению возвращал объекты, на которых `Check` сказал бы
// «да», а kacho-iam объединял два отношения: `viewer ∪ v_list` (см.
// AuthorizeService.ListObjects). Здесь вычисляется РОВНО тот же предикат, только
// per-object: сначала батч на `viewer`, затем батч на `v_list` для тех, кому
// `viewer` отказал. Никакого ослабления авторизации — тот же вопрос, другая форма
// запроса.
//
// # Дисциплина фильтра
//
// Fail-closed по умолчанию: iam недоступен → Unavailable (страница НЕ отдаётся
// нефильтрованной). KACHO_COMPUTE_LIST_FILTER_FAIL_OPEN=true переключает в
// degraded-mode — страница отдаётся как есть + audit-WARN на каждый fail-open.
// Публичный каталог (DiskType / MachineType) через фильтр не проходит вовсе —
// это ambient cluster-scoped read (handler передаёт nil-фильтр).
//
// Configurable env vars (все KACHO_COMPUTE_LIST_FILTER_*):
//   - URL/grpc address — переиспользует KACHO_COMPUTE_AUTHZ_IAM_GRPC_ADDR.
//   - ENABLED           — master switch.
//   - TIMEOUT_MS        — per-call deadline ОДНОГО BatchCheck (default 500).
//   - CACHE_TTL_MS      — TTL положительного per-object вердикта (default 5000).
//   - CACHE_MAX_ENTRIES — bound кеша, LRU-вытеснение (default 10000).
//   - FAIL_OPEN         — на ошибке iam отдать нефильтрованную страницу; иначе fail-closed.
//
// Clean Architecture: пакет — outbound adapter (ходит в iam по gRPC) и port
// (потребляется List-handler'ами). Живёт в internal/, т.к. это compute-specific
// glue; если тот же паттерн вырастет во втором сервисе — поднять в
// kacho-corelib/authz.
package authzfilter
