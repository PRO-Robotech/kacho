// Единые формулировки описаний полей — источник для таблиц полей на страницах ресурсов.
// Меняешь смысл поля → правишь здесь, а не в N страницах.
export const DICTIONARY = {
  id: { short: 'Идентификатор ресурса (output-only, генерируется сервером)' },
  projectId: { short: 'Идентификатор проекта (домен kaname); ресурс — project-level' },
  name: { short: 'Имя ресурса: DNS label по RFC 1123 — ^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$ (строчные латинские буквы, цифры и дефис; 1..63; цифра первым знаком допустима). Уникально в паре (projectId, name). Пустая строка — законный вход Create: сервер проставит имя, производное от id; в Update отвергается' },
  description: { short: 'Описание (0..256 символов)' },
  labels: { short: 'Метки key:value (до 64 пар)' },
  createdAt: { short: 'Время создания (RFC 3339, усечено до секунд)' },
  status: { short: 'Текущий статус ресурса (enum)' },
  zoneId: { short: 'Зона доступности (домен kacho-geo); immutable' },
  updateMask: { short: 'Список изменяемых полей (FieldMask)' },
  filter: { short: 'Фильтр списка (name="<value>")' },
  pageSize: { short: 'Размер страницы (0 → 50, максимум 1000)' },
  pageToken: { short: 'Курсор следующей страницы (opaque base64)' },
  // Instance
  instanceKind: { short: 'Род инстанса: VM либо CONTAINER (required, immutable после Create)' },
  machineTypeId: { short: 'Тип машины — единственный канал размера (слаг "mt-…" либо устойчивое имя типа)' },
  effectiveResources: { short: 'Действующие ресурсы, выведенные из типа машины: vCPU, память, GPU (output-only)' },
  bootSource: { short: 'Источник загрузки: {type, id} — образ, снимок или том домена kacho-storage' },
  cpuGuaranteePercent: { short: 'Гарантированная доля vCPU в процентах (0 — burstable); применяется к семействам STANDARD / COMPUTE / MEMORY' },
  vmSpec: { short: 'Конфигурация виртуальной машины (при instanceKind = VM)' },
  containerSpec: { short: 'Конфигурация контейнера (при instanceKind = CONTAINER)' },
  statusReason: { short: 'Человекочитаемая причина текущего статуса или отложенного изменения (output-only)' },
  placementGroupId: { short: 'Группа размещения инстанса' },
  bootDisk: { short: 'Зеркало загрузочного тома (output-only; том принадлежит kacho-storage)' },
  secondaryDisks: { short: 'Зеркало дополнительных присоединённых томов (output-only; тома принадлежат kacho-storage)' },
  networkInterfaces: { short: 'Сетевые интерфейсы инстанса (denormalised-зеркало NIC из kacho-vpc)' },
  serviceAccountId: { short: 'Сервисный аккаунт для аутентификации внутри инстанса (kaname)' },
  fqdn: { short: 'Доменное имя инстанса (output-only, назначается сервером)' },
  // MachineType
  machineTypeFamily: { short: 'Семейство типа машины: STANDARD | COMPUTE | MEMORY | GPU' },
  machineTypeStatus: { short: 'Доступность в каталоге: AVAILABLE | DEPRECATED | RETIRED' },
} as const

export type DictionaryKey = keyof typeof DICTIONARY
