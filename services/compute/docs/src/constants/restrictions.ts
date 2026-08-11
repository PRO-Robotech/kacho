// Правила валидации полей — единый источник для компонента <Restrictions />.
// Тексты и границы сверены с proto (kacho-proto/.../compute/v1) и
// internal/service/validate.go kacho-compute.
export const RESTRICTIONS = {
  name: [
    'regex ^([a-z]([-_a-z0-9]{0,61}[a-z0-9])?)?$',
    'длина 0..63; только lowercase (строчные буквы, цифры, дефис, подчёркивание)',
    'uppercase запрещён (конвенция имени compute — строже, чем в некоторых других доменах Kachō)',
  ],
  projectId: [
    'обязателен при Create',
    'ссылка на Project домена kacho-iam (существование проверяется вызовом ProjectService.Get)',
    'immutable после Create',
  ],
  zoneId: [
    'обязателен при Create Instance / Disk',
    'ссылка на Zone домена kacho-geo (существование проверяется вызовом ZoneService.Get)',
    'immutable после Create (меняется только через Relocate)',
  ],
  labels: [
    'до 64 пар key:value',
    'ключ: regex [a-z][-_./\\@0-9a-z]*, длина 1..63',
    'значение: regex [-_./\\@0-9a-z]*, длина 0..63',
  ],
  description: ['длина 0..256'],
  instanceResources: [
    'размер задаётся ссылкой на каталог: machineTypeId — слаг «mt-…» либо устойчивое имя типа',
    'effectiveResources (vCpu / memoryMib / gpus / gpuType) выводится сервером и на вход не принимается',
    'cpuGuaranteePercent ∈ 0..100 (0 — burstable); применяется к семействам STANDARD / COMPUTE / MEMORY',
    'изменение machineTypeId / cpuGuaranteePercent / placementGroupId — только когда Instance в статусе STOPPED',
    'тип в статусе RETIRED отвергается на Create',
  ],
  metadata: [
    'карта key:value; суммарный размер всех ключей и значений < 512 KB, каждое значение ≤ 256 KB',
    'меняется отдельным RPC UpdateMetadata (delete[] + upsert{}), не через Update',
  ],
  updateMask: [
    'google.protobuf.FieldMask — список изменяемых полей',
    'неизвестное поле в mask → INVALID_ARGUMENT',
    'hard-immutable поле в mask → INVALID_ARGUMENT «<field> is immutable after <Resource>.Create»',
    'пустой mask → full-PATCH: применяются все mutable-поля, immutable из тела игнорируются',
  ],
  pagination: [
    'pageSize: 0 → default 50; максимум 1000',
    'pageToken: opaque base64 от (createdAt, id); передавать как есть',
    'garbage-token → INVALID_ARGUMENT',
    'nextPageToken пуст → последняя страница',
  ],
  filter: ['синтаксис фильтрации Kachō; в текущей фазе поддержан предикат name="<value>"'],
  resourceId: [
    'TEXT: префикс, дефис и 17 символов crockford-base32 (ins-…, mt-…)',
    'id операции сохраняет слитную форму epd + 17 символов',
    'генерируется сервером (output-only), неизменяем на всю жизнь ресурса',
    'malformed → INVALID_ARGUMENT синхронно; well-formed, но несуществующий → NOT_FOUND',
  ],
} as const

export type RestrictionKey = keyof typeof RESTRICTIONS
