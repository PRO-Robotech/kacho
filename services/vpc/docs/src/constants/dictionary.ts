// Словарь описаний полей — единый источник для таблиц запроса/ответа во всех
// API-страницах (DRY: одно описание поля переиспользуется на всех ресурсах).
export const DICTIONARY = {
  id: { short: 'Идентификатор ресурса — TEXT: 3-символьный префикс + 17-символьный crockford-base32 (output-only, генерируется сервером)' },
  projectId: { short: 'Идентификатор проекта kacho-iam, которому принадлежит ресурс (обязателен при создании)' },
  name: { short: 'Имя ресурса: DNS label по RFC 1123 — ^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$ (строчные латинские буквы, цифры и дефис; 1..63; цифра первым знаком допустима). Уникально в паре (projectId, name). Пустая строка — законный вход Create: сервер проставит имя, производное от id; в Update отвергается' },
  description: { short: 'Описание ресурса (UTF-8, ≤256)' },
  labels: { short: 'Пользовательские метки key→value (≤64 пар) для поиска ресурса' },
  createdAt: { short: 'Время создания (output-only; truncate до секунд)' },
  updateMask: { short: 'FieldMask: список изменяемых полей. Неизвестное поле / immutable → InvalidArgument; пустой mask = full-PATCH' },
  status: { short: 'Грубый статус ресурса (output-only enum)' },
  filter: { short: 'Строка фильтра (конвенция Kachō; поддерживается name="<value>")' },
  pageSize: { short: 'Размер страницы (0 → 50, max 1000)' },
  pageToken: { short: 'Opaque cursor (base64 от {created_at, id}); невалидный → InvalidArgument' },
  networkId: { short: 'Идентификатор Network, которому принадлежит ресурс' },
  subnetId: { short: 'Идентификатор Subnet' },
  zoneId: { short: 'Идентификатор зоны (region-1-a); существование валидируется через kacho-geo' },
  regionId: { short: 'Идентификатор региона (region-1); существование валидируется через kacho-geo' },
  placementType: { short: 'Дискриминатор размещения подсети: ZONAL (zoneId) или REGIONAL (regionId). OUTPUT-ONLY — выводится сервером, непустое значение в теле отвергается; immutable' },
  ipv4CidrPrimary: { short: 'Первичный IPv4-префикс подсети — адресный якорь, immutable после создания; host-биты = 0, размер в диапазоне /16../28' },
  ipv6CidrPrimary: { short: 'Первичный IPv6-префикс подсети — адресный якорь, immutable после создания; host-биты = 0' },
  ipv4CidrBlocks: { short: 'Текущий набор IPv4-префиксов подсети (якорь плюс добавленные через :add-cidr-blocks); host-биты = 0' },
  ipv6CidrBlocks: { short: 'Текущий набор IPv6-префиксов подсети' },
  v4CidrBlocks: { short: 'СНЯТОЕ имя: у подсети действуют ipv4CidrPrimary (вход) и ipv4CidrBlocks (проекция). Ключ оставлен, потому что имя ещё живо у ДРУГИХ ресурсов — пула адресов и правила группы' },
  v6CidrBlocks: { short: 'СНЯТОЕ имя у подсети; у пула адресов и правила группы имя действует' },
  deletionProtection: { short: 'Защита от удаления — при true Delete отклоняется (FailedPrecondition)' },
  securityGroupIds: { short: 'Список id SecurityGroup, привязанных к NIC' },
  v4AddressIds: { short: 'Список id IPv4-Address (≤1 на NIC)' },
  v6AddressIds: { short: 'Список id IPv6-Address (≤1 на NIC)' },
  macAddress: { short: 'MAC-адрес NIC (output-only, аллоцируется сервером, уникален в облаке)' },
  usedBy: { short: 'Ссылка «кто использует ресурс» (output-only denormalised mirror)' },
} as const

export type DictKey = keyof typeof DICTIONARY
