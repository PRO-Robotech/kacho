// Правила валидации полей — единый источник для компонента <Restrictions />.
// Дом контракта — `proto/kacho/cloud/compute/v1`; дом общего валидатора —
// `pkg/validate` (единая форма имени ресурса объявлена в `pkg/validate/nameform`).
// Правя тексты ниже, сверяй их с этими двумя, а не с прежними полирепо.
export const RESTRICTIONS = {
  name: [
    'regex ^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$ — DNS label по RFC 1123',
    'строчные латинские буквы, цифры и дефис; подчёркивание НЕ допускается',
    'длина 1..63; цифра первым знаком допустима (`9lives` — валидное имя)',
    'пустая строка при Create означает «назови сам»: сервер подставит имя, производное от id',
    'форма ОДНА на всю платформу (`pkg/validate/nameform`) — не «строже, чем в других доменах»',
  ],
  projectId: [
    'обязателен при Create',
    'ссылка на Project домена kaname (существование проверяется вызовом ProjectService.Get)',
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
