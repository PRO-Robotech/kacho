// Базовый клиент: REST JSON на api-gateway endpoints.
// В dev: vite.config.ts проксирует /<domain>/v1/* на http://localhost:8080.
// В prod: same-origin, ingress рулит на api-gateway:8080.
//
// Эндпоинты, которые адресует исходник ЭТОГО приложения. Перечень утверждается
// против src (client.endpoints.test.ts): лишняя строка здесь — находка ровно так
// же, как недостающая. URL-ы verbatim из proto google.api.http annotations:
//   compute:    /compute/v1/instances, /compute/v1/machineTypes,
//               /compute/v1/guestAccessKeys, /compute/v1/placementGroups
//   geo:        /geo/v1/zones, /geo/v1/regions
//   storage:    /storage/v1/volumes, /storage/v1/images, /storage/v1/snapshots,
//               /storage/v1/diskTypes
//   vpc:        /vpc/v1/networkInterfaces, /vpc/v1/networks,
//               /vpc/v1/networks/{id}:internal, /vpc/v1/subnets,
//               /vpc/v1/addresses, /vpc/v1/addressPools, /vpc/v1/routeTables,
//               /vpc/v1/securityGroups, /vpc/v1/cidrGroups, /vpc/v1/gateways
//   nlb:        /nlb/v1/networkLoadBalancers, /nlb/v1/listeners,
//               /nlb/v1/targetGroups
//   registry:   /registry/v1/registries,
//               /registry/v1/registries/{id}/repositories,
//               /registry/v1/registries/{id}/repositories/{id}/tags
//   iam:        /iam/v1/accounts, /iam/v1/projects, /iam/v1/users,
//               /iam/v1/serviceAccounts, /iam/v1/groups, /iam/v1/roles,
//               /iam/v1/accessBindings, /iam/v1/me, /iam/v1/permissionCatalog
//   operations: /operations/{id}
//
// Вне proto: /iam/v1/auth/me — HTTP-роут самого api-gateway (личность за сессией
// развёрнутого провайдера), google.api.http annotation у него нет.
//
// ПОЧЕМУ ПЕРЕЧЕНЬ РАСТЁТ — не потому, что приложение стало звать больше. Реестр ресурсов сведён к единственному на всю консоль (#406), и
// прослойка этого приложения ведёт теперь в него: обход, читающий адресацию ЗА
// ШИМОМ, видит адреса ВСЕХ записей реестра. Раздел по-прежнему рисует свои
// маршруты; выросло не поведение, а то, что предикату видно. Тот же рост случался в этой
// шапке дважды до того: когда за шим уехали клиент, разбор отказа и справочник
// прав, и когда в общий реестр переехали спеки этого домена. Три строки реестра
// образов добавлены той же причиной — реестр registry стал проекцией общего
// (#409), и его записи стали досягаемы отсюда, хотя раздел его маршрутов рисует
// другой модуль.
//
// Отсюда следствие, которое стоит сказать вслух: перечень описывает ДОСЯГАЕМОЕ,
// а не вызываемое. Строка исчезнет отсюда только вместе с записью реестра либо
// вместе с прослойкой, а не оттого, что раздел перестал показывать ресурс.
//
// Три ПОД-перечисления сети (её подсети, таблицы маршрутов и группы
// безопасности отдельными вложенными списками) сюда не входят и не входили:
// они сняты с контракта как вторые пути к одному ответу, а общая реализация
// спрашивает то же самое плоскими списками с сужением по родителю — их адреса и
// стоят выше. Пути в этой оговорке намеренно НЕ записаны дословно: перечень
// разбирается из шапки по литералам, поэтому упоминание снятого адреса в прозе
// вернуло бы его в объявленное множество, и объяснение снятия опровергло бы
// само себя.
//
// API mapping:
//   GET    /<domain>/v1/<plural>          → List
//   GET    /<domain>/v1/<plural>/{id}     → Get
//   POST   /<domain>/v1/<plural>          → Create  → Operation
//   PATCH  /<domain>/v1/<plural>/{id}     → Update  → Operation
//   DELETE /<domain>/v1/<plural>/{id}     → Delete  → Operation
//   POST   /<domain>/v1/<plural>/{id}:verb → Custom verb → Operation

// Единственная реализация живёт в `shared/`. Копия в модуле была форком —
// и форк отставал: в нём не было сохранения НЕ-JSON тела отказа (страница
// 5xx от края/nginx выбрасывалась молча, а вызывающий видел только statusText)
// и код отказа был объявлен строкой, тогда как край присылает ЧИСЛО (#405).
export * from "@shared/api/client";
