# Классификация типа артефакта образа (artifact_type)

`Repository.artifact_types` — output-only проекция, отличающая **контейнерный образ**
(docker/OCI image) от **Helm-чарта** (и прочих OCI-артефактов). Нужна UI-фильтру
«docker-образы vs helm-чарты». Значение классифицируется на request-path в
`services/registry/internal/clients/zot/graphql.go` (source of truth — zot; в БД не
хранится). Форма поля наружу и что видит арендатор — на странице сайта
`docs/content/api/repository.mdx`; здесь только механика.

## Дискриминатор

Тип определяется **`config.mediaType`** манифеста, НЕ top-level manifest media-type:
контейнерный образ и helm-чарт несут одинаковый top-level
`application/vnd.oci.image.manifest.v1+json` — различие только в конфиге.

| config.mediaType | artifact_type |
|---|---|
| `application/vnd.cncf.helm.config.v1+json` | HELM_CHART |
| `application/vnd.docker.container.image.v1+json` | CONTAINER_IMAGE |
| `application/vnd.oci.image.config.v1+json` | CONTAINER_IMAGE |
| (config пуст) + top-level index/manifest-list | CONTAINER_IMAGE (multi-arch) |
| иной непустой config.mediaType | OTHER |
| нет тегов / манифест непрочитан | UNSPECIFIED |

Алгоритм — чистая доменная функция `domain.ClassifyArtifact(configMediaType,
manifestMediaType)` (config-приоритет над top-level, first-match); zot-адаптер лишь
извлекает два media-type-поля из тела манифеста и делегирует.

## By-design tradeoffs

**Классифицируется КАЖДЫЙ тег, наружу идёт набор.** `repositoryFromSummaries` прогоняет
`classifyTag` по всем тегам, пришедшим в `ImageList`, и собирает **упорядоченно-уникальный**
набор без `UNSPECIFIED` в `ArtifactTypes`; `ArtifactType` (единственное число) — первый
элемент того же набора, оставленный ради обратной совместимости фильтра. Mixed-repo несёт
**оба** значения.

> [!note] Здесь был описан ровно противоположный компромисс — исправлено по коду 2026-08-11
> Прежняя редакция утверждала «best-effort по ОДНОМУ репрезентативному тегу (`latest`, иначе
> последний в отсортированном списке)», объявляла классификацию mixed-repo по репрезентанту
> «осознанным компромиссом, НЕ латентным багом» и отдельно фиксировала, что per-tag
> классификация «намеренно НЕ вводится (GET-манифест на каждый тег, N×M)». В дереве
> реализовано именно то, что объявлено невведённым, и цена, которой это обосновывалось, не
> платится: теги приходят **одним** GraphQL-запросом на репозиторий, а не манифестом на тег.
>
> Страница сайта была неверна **иначе**: поля она называла правильно (`artifactTypes`
> присутствует и в proto — `proto/kacho/cloud/registry/v1/registry.proto`), а прозу механики
> унаследовала от той же старой редакции — и получилось внутреннее противоречие, которое
> нельзя было заметить, не сверив с кодом: «читаем один тег» и «mixed-repo отражается в
> наборе» одновременно верными быть не могут. Обе стороны приведены к дереву одним заходом,
> иначе следующая правка починила бы одну и оставила вторую.
>
> Предикат: `grep -n 'classifyTag\|ArtifactTypes' services/registry/internal/clients/zot/graphql.go`.

**Стоимость принадлежит окну страницы, а не пространству имён.** Имена сортируются и режутся
окном (`page_size`/`page_token`) **до** запроса тегов, и `ImageList` спрашивается только для
окна, параллельно. Обход всего namespace развернул бы один вызов в N обращений к zot, где N
не контролируется вызывающим, — шапка `ListRepositories` называет это прямо.

**Fail-closed сохранён.** Полный отказ zot → `ErrUnavailable` ДО классификации. Репозиторий
без тегов остаётся `UNSPECIFIED` и на этом шаге **не скрывается**: видим ли он арендатору —
решает объединение наложения и проекции выше по стеку, единственный слой, которому видны обе
стороны. Классификатор никогда не роняет `ListRepositories` и не маскирует недоступность
частичной проекцией.

**Фильтр — client-side.** UI грузит все страницы `ListRepositories` (follow `next_page_token`)
и фильтрует по типу на клиенте. Server-side filter-параметр не введён (namespace ограничен).
Если появится deep-пагинация большого масштаба — facet потребует server-side support.
