// Базовый клиент: REST JSON на api-gateway endpoints.
// В dev: vite.config.ts проксирует /<domain>/v1/* на http://localhost:8080.
// В prod: same-origin, ingress рулит на api-gateway:8080.
//
// Эндпоинты, которые адресует исходник ЭТОГО приложения. Перечень утверждается
// против src (client.endpoints.test.ts): лишняя строка здесь — находка ровно так
// же, как недостающая. URL-ы verbatim из proto google.api.http annotations:
//   compute:    /compute/v1/instances, /compute/v1/machineTypes,
//               /compute/v1/guestAccessKeys
//   geo:        /geo/v1/zones
//   storage:    /storage/v1/volumes, /storage/v1/images
//   vpc:        /vpc/v1/addresses, /vpc/v1/networkInterfaces,
//               /vpc/v1/networks/{id}/subnets, /vpc/v1/networks/{id}/route_tables,
//               /vpc/v1/networks/{id}/security_groups
//   iam:        /iam/v1/accounts, /iam/v1/projects, /iam/v1/users,
//               /iam/v1/serviceAccounts, /iam/v1/groups, /iam/v1/roles,
//               /iam/v1/accessBindings, /iam/v1/me, /iam/v1/permissionCatalog
//   operations: /operations/{id}
//
// Вне proto: /iam/v1/auth/me — HTTP-роут самого api-gateway (личность за сессией
// развёрнутого провайдера), google.api.http annotation у него нет.
//
// Перечень iam вырос на /iam/v1/permissionCatalog не потому, что приложение стало
// звать больше: клиент, разбор отказа и справочник прав сведены к единственной
// реализации в `shared/` (#405), и обход теперь читает адресацию ЗА ШИМОМ. Прежде
// она была не видна предикату — не потому, что её не было.
//
// Оговорка о достижимости: /vpc/v1/addresses и три подколлекции сети (subnets,
// route_tables, security_groups) адресует только dependency-graph, а его ветки
// networks/subnets/addresses спеки в реестре ЭТОГО приложения не имеют — маршрута,
// который бы их позвал, здесь нет; из vpc-ветвей достижима network-interfaces.
// Оговорка держится тем же тестом и сама истечёт: заведут спеку — он покраснеет.
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
