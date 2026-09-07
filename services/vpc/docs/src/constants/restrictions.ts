// Правила валидации полей — единый источник для компонента <Restrictions />.

// NAME_RULES — форма имени ресурса, ОДНА на всю платформу (`pkg/validate/nameform`).
// Вынесена отдельной константой, потому что её читают два ключа ниже: копия
// разошлась бы с оригиналом молча — ровно так и разошлись прежние четыре
// объявления формы имени (#715).
const NAME_RULES = [
  'regex ^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$ — DNS label по RFC 1123',
  'строчные латинские буквы, цифры и дефис; первый и последний знак — буква или цифра',
  'длина 1..63; цифра первым знаком допустима (`9lives` — валидное имя)',
  'пустая строка при Create означает «назови сам»: сервер подставит имя, производное от id',
  'пустая строка при Update отвергается — ресурса без имени не бывает',
];

export const RESTRICTIONS = {
  name: NAME_RULES,
  // nameGateway — тот же набор, а не второе правило. Отдельной формы у имени шлюза
  // не существует с #715, но ключ остаётся: его читают две таблицы полей на
  // странице Gateway, и отсылкой их не заполнить.
  nameGateway: NAME_RULES,
  description: ['UTF-8 длина ≤ 256'],
  labels: [
    '≤ 64 пар',
    "key: ^[a-z][-_./\\@a-z0-9]{0,62}$ (1..63 байт)",
    'value: ≤ 63 байт (пустое значение допустимо)',
  ],
  projectId: ['обязателен при Create', 'существование проверяется через kaname ProjectService.Get'],
  cidr: [
    'валидный CIDR-префикс',
    'host-биты = 0 (10.0.0.0/24 — OK; 10.0.0.5/24 → InvalidArgument)',
    'CIDR не должен пересекаться с соседними Subnet (EXCLUDE-constraint, FailedPrecondition)',
  ],
  zoneId: ['для Subnet — обязателен при ZONAL-placement (region_id должен быть пуст); для external Address — при неявном адресе', 'immutable после Create', 'существование валидируется через kacho-geo ZoneService.Get'],
  regionId: ['для Subnet — обязателен при REGIONAL-placement (zone_id должен быть пуст)', 'immutable после Create', 'существование валидируется через kacho-geo RegionService.Get'],
  placementType: ['обязателен при Create: ZONAL | REGIONAL', 'UNSPECIFIED → InvalidArgument (не дефолтит в ZONAL)', 'immutable после Create'],
  updateMask: [
    'неизвестное поле → InvalidArgument',
    'hard-immutable поле в mask → InvalidArgument («<field> is immutable after <Resource>.Create»)',
    'пустой mask → full-PATCH (immutable из тела silently игнорируются)',
  ],
  pagination: ['page_size: 0 → 50, max 1000', 'page_token: opaque base64; невалидный → InvalidArgument'],
  resourceId: ["нераспознанный 3-char префикс → InvalidArgument «invalid <res> id '<X>'»"],
  // CidrGroup адресуется hyphen-формой (`cdg-` + 17 символов), а не слитным
  // 3-символьным префиксом — отдельный ключ заведён потому, что общий текст
  // выше называл бы форму, которой у этого ресурса нет.
  resourceIdHyphen: [
    'форма id: cdg- + 17 символов crockford-base32',
    "нераспознанный префикс → InvalidArgument «invalid <res> id '<X>'»",
    'корректный по форме, но отсутствующий id → NotFound',
  ],
  cidrGroupBlocks: [
    'валидный CIDR-префикс, host-биты = 0',
    'семейство блока обязано совпадать с полем (v4CidrBlocks / v6CidrBlocks)',
    '≤ 64 префиксов НА СЕМЕЙСТВО (и в одном запросе, и накопленно)',
    'пересечения между членами набора допустимы',
  ],
  nicCardinality: ['≤ 1 IPv4 и ≤ 1 IPv6 на NIC (DB-level CHECK + sync-валидация)'],
} as const

export type RestrictionKey = keyof typeof RESTRICTIONS
