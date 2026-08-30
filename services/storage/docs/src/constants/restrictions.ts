// Правила валидации полей — единый источник для компонента <Restrictions />.
// Источник истины — CHECK-констрейнты миграций `internal/migrations/*.sql`,
// объявления полей в `proto/kacho/cloud/storage/v1/*.proto` и самовалидирующийся
// domain-слой сервиса.
export const RESTRICTIONS = {
  name: [
    'regex ^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$ — DNS label по RFC 1123',
    'строчные латинские буквы, цифры и дефис; без uppercase и underscore',
    'длина 1..63; цифра первым знаком допустима (`9lives` — валидное имя)',
    'пустая строка при Create означает «назови сам»: сервер подставит имя, производное от id',
    'уникально в паре (projectId, name) — полный UNIQUE: имени без значения не бывает',
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
  snapshotZoneId: [
    'output-only: снимается с зоны исходного тома на Create, вызывающим не назначается',
    'immutable — собственный якорь снимка, а не ссылка на зону тома',
    'пустая зона (том удалён до появления якоря) запрещает засев: доказать когерентность нечем',
  ],
  diskTypeId: [
    'обязателен при Volume.Create; класс обязан быть ACTIVE и предлагаться в зоне тома',
    'в updateMask не входит НИКОГДА → «disk_type_id is immutable after Volume.Create»',
    'меняется отдельным глаголом :changeDiskType — это перемещение данных, а не правка поля',
    'FK внутри своей БД на каталог классов, ON DELETE RESTRICT — класс с томами не удаляется',
  ],
  sizeBytes: [
    'у тома > 0 (CHECK на уровне БД)',
    'в границах класса: ≥ limits.minSizeBytes, ≤ limits.maxSizeBytes, кратен limits.sizeStepBytes (ноль в границе = класс не сужает)',
    'Update — только увеличение («Volume size can only be increased»)',
    'при засеве из образа — не меньше minDiskBytes образа; граница включающая',
  ],
  volumeSource: [
    'источник тома — снимок ЛИБО образ, не оба сразу',
    'источник обязан принадлежать тому же проекту',
    'источник обязан быть размещён когерентно (см. «Когерентность размещения»)',
    'источник обязан быть READY («Snapshot <id> is not ready» / «Image <id> is not ready»)',
    'immutable после Create',
  ],
  imageSource: [
    'источник образа — снимок ЛИБО том, ровно один («an image source must be either a snapshot or a volume, not both»)',
    'источник обязан принадлежать тому же проекту',
    'immutable после Create',
  ],
  diskTypeCatalog: [
    'id — slug, назначает администратор; первичный ключ, immutable',
    'tier — только из закрытого словаря CAPACITY|BALANCED|FAST|SINGLE|IO_MAX; омит законен, значение вне словаря → InvalidArgument',
    'lifecycle — ACTIVE|DEPRECATED|RETIRED; в updateMask не входит, меняется глаголом :setLifecycle',
    'capabilities — output-only, выводятся пересечением действующих ревизий привязки; на вход не принимаются',
    'limits — границы кратны шагу, min ≤ max (CHECK на уровне БД)',
  ],
  storageBackend: [
    'kind — из закрытого перечня; immutable после Create («kind is immutable after StorageBackend.Create»)',
    'endpoint — форма <схема>://<путь>: голый host:port отвергается, иначе адрес узла просочился бы в контракт',
    'credentialsRef — форма <схема>://<путь>; значение, не похожее на ссылку, отвергается InvalidArgument',
    'name уникально в реестре; адресация идёт по id, имя косметическое',
    'бэкенд с ревизиями привязки не удаляется (FK RESTRICT) — вывод из обращения это DRAINING',
  ],
  diskTypeBinding: [
    'ревизии append-only и НЕИЗМЕНЯЕМЫ: Update у ресурса нет и быть не должно',
    'ровно одна ACTIVE-ревизия на пару (класс, зона) — частичный UNIQUE, не «прочитал → записал»',
    'revision присваивает сервис; зона обязана входить в список обслуживаемых зон бэкенда',
    'бэкенд в DRAINING/DISABLED новых ревизий не принимает',
    'методов ровно три — Create, Get, List: ни Update, ни Delete, ни отдельного глагола снятия с действия у ревизии нет',
    'вытеснение — следствие Create, а не операция: SUPERSEDED в ACTIVE не возвращается, сменить политику пары можно только НОВОЙ ревизией',
  ],
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
