// Правила валидации полей — единый источник для компонента <Restrictions />.
// Источник истины — CHECK-констрейнты миграций `internal/migrations/*.sql` и
// самовалидирующийся domain-слой сервиса.
export const RESTRICTIONS = {
  name: [
    'regex ^[a-z]([-a-z0-9]{0,61}[a-z0-9])?$ (строгий lowercase, без uppercase и underscore)',
    'длина 0..63; пустая строка допустима',
    'уникально в паре (projectId, name), когда непусто — partial UNIQUE',
  ],
  description: ['UTF-8 длина ≤ 256'],
  labels: [
    '≤ 64 пар',
    'key: ^[a-z][-_./\\@a-z0-9]{0,62}$ (1..63 байт)',
    'value: ≤ 63 байт (пустое значение допустимо)',
  ],
  projectId: [
    'обязателен при Create; immutable после Create',
    'существование проверяется через kacho-iam ProjectService.Get',
    'обязателен и в List — пустое значение отвергается синхронно («projectId is required»)',
  ],
  zoneId: [
    'обязателен при Volume.Create; immutable после Create',
    'существование валидируется через kacho-geo ZoneService.Get (отказ закрытый)',
  ],
  regionId: [
    'обязателен при Image.Create; immutable после Create',
    'существование валидируется через kacho-geo RegionService.Get (отказ закрытый)',
  ],
  diskTypeId: [
    'обязателен при Volume.Create; immutable после Create',
    'FK внутри своей БД на каталог типов, ON DELETE RESTRICT — тип с томами не удаляется',
  ],
  sizeBytes: [
    'у тома > 0 (CHECK на уровне БД)',
    'Update — только увеличение («Volume size can only be increased»)',
    'при засеве из образа — не меньше minDiskBytes образа; граница включающая',
  ],
  blockSize: ['> 0; по умолчанию 4096', 'immutable после Create'],
  volumeSource: [
    'источник тома — снимок ЛИБО образ, не оба сразу',
    'источник обязан принадлежать тому же проекту',
    'источник обязан быть размещён когерентно (см. «Когерентность размещения»)',
    'immutable после Create',
  ],
  imageSource: [
    'источник образа — снимок ЛИБО том, ровно один («an image source must be either a snapshot or a volume, not both»)',
    'источник обязан принадлежать тому же проекту',
    'immutable после Create',
  ],
  deviceName: [
    'уникально в пределах машины (UNIQUE на уровне БД)',
    'пустое значение — сервер подбирает первое свободное из диапазона sdb..sdz',
    'явно указанное занятое имя → «device <name> is already in use on Instance <id>»',
  ],
  isBoot: ['не более одного загрузочного тома на машину (EXCLUDE-констрейнт на уровне БД)'],
  updateMask: [
    'неизвестное поле → InvalidArgument',
    'hard-immutable поле в mask → InvalidArgument («<field> is immutable after <Resource>.Create»)',
    'пустой mask → full-PATCH (immutable из тела молча игнорируются)',
  ],
  pagination: [
    'page_size: 0 → 50, max 1000; значение вне диапазона отвергается, а не обрезается',
    'page_token: opaque base64 от {created_at, id}; невалидный → InvalidArgument',
    'валидация выполняется ДО сужения по правам — отсутствие грантов не превращает 400 в пустую страницу',
  ],
  resourceId: ['нераспознанный префикс → InvalidArgument «invalid <res> id \'<X>\'» первым стейтментом RPC'],
} as const

export type RestrictionKey = keyof typeof RESTRICTIONS
