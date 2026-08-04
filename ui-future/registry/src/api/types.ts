// TS-типы плоского API реестра образов (контракт proto ствола).
// Ресурсы плоские (нет envelope metadata/spec/status); grpc-gateway
// сериализует proto snake_case → JSON snake_case (прямой маппинг).
//
// Здесь лежит ТОЛЬКО то, что это приложение действительно принимает с провода:
// конверт операции и три ресурса реестра. Прежде файл нёс ещё описания vpc-сетей
// и блочного хранения compute — ни одно из них не импортировалось, и все они
// описывали форму, которой в контракте больше нет (блочное хранение уехало в
// домен storage и переписано: у образа сменились даже номера полей, то есть это
// другое сообщение, а не эволюция прежнего; у подсети переименованы диапазоны).
// Мёртвое описание рядом с живым читается как действующий контракт, поэтому они
// удалены, а не поправлены: своего потребителя у них не было.

// ====== Operation ======

export interface Operation {
  id: string;
  description?: string;
  created_at?: string;
  created_by?: string;
  modified_at?: string;
  done: boolean;
  metadata?: { "@type": string; [key: string]: unknown };
  error?: { code: number; message: string; details?: unknown[] };
  response?: { "@type": string; [key: string]: unknown };
}

export interface OperationList {
  operations: Operation[];
  next_page_token?: string;
}

// ====== registry (Container Registry) ======
// proto: kacho.cloud.registry.v1. Ресурсы плоские; мутации async → Operation.
//
// REG-1 REDESIGN: Registry становится REGIONAL-anycast (region_id + placement_type
// const REGIONAL, оба immutable; peer-validate geo.Region). default_repository_visibility
// (rename default_visibility) сидит visibility новых Repository (any-path-to-PUBLIC
// admin-gated). id — единственная идентичность/URL-адресация (pull-путь
// $domain/$registryId/$repo:$tag); name — mutable косметический label (НЕ в URL).
// Явно НЕТ: globalSlug / displayName / top-level visibility / :rename.

// Registry — реестр контейнерных образов (project-scoped, tenant-facing).
export interface Registry {
  id: string;
  project_id: string;
  created_at?: string;
  name?: string;
  description?: string;
  labels?: Record<string, string>;
  // Endpoint для docker login / push / pull (output-only; derived по id: <host>/<id>).
  endpoint?: string;
  // Число репозиториев в реестре (output-only; растёт с docker push).
  repository_count?: number;
  status?: string;
  // --- REG-1 (NET-NEW) ---
  // Регион размещения (REG-1 F4): required на Create, immutable, peer-validate geo.Region.
  region_id?: string;
  // Тип размещения — всегда REGIONAL (REG-1 F4, regional-anycast const, immutable).
  // zone_id отсутствует by construction (зоне-независимый ресурс).
  placement_type?: "PLACEMENT_TYPE_UNSPECIFIED" | "REGIONAL" | string;
  // Видимость по умолчанию для новых Repository (REG-1 F5, rename default_visibility).
  // any-path-to-PUBLIC admin-gated (enforced server-side).
  default_repository_visibility?: "PRIVATE" | "PUBLIC" | string;
}

export interface RegistryList {
  registries: Registry[];
  next_page_token?: string;
}

// Repository — read-only: материализуется при первом docker push, через API не
// создаётся. Идентифицируется полным именем (OCI-путь внутри реестра).
export interface Repository {
  name: string;
  registry_id?: string;
  // Класс исчезаемости репозитория (REG-1 F7): output-only enum. DURABLE —
  // survives-empty (явный CreateRepository / установленный overlay); EPHEMERAL —
  // register-on-first-push, unregister-on-last-tag. Установка overlay AUTO-PROMOTE'ит
  // EPHEMERAL→DURABLE. Понижение через API не выразимо (снимается DeleteRepository).
  lifecycle?: "REPOSITORY_LIFECYCLE_UNSPECIFIED" | "DURABLE" | "EPHEMERAL" | string;
  // Видимость репозитория (авторитетный per-repo гейт). Сидится из
  // Registry.default_repository_visibility на create; any-path-to-PUBLIC admin-gated.
  visibility?: "PRIVATE" | "PUBLIC" | string;
  // Число тегов образов в репозитории (output-only).
  tag_count?: number;
  // Агрегатный размер репозитория; proto3 int64 сериализуется как СТРОКА.
  size_bytes?: string;
  // Время последнего push (last pushed).
  updated_at?: string;
  // Время последнего pull; zero/пусто = ни разу не скачивался.
  last_pulled_at?: string;
  // Основной тип артефакта (enum-имя ARTIFACT_TYPE_*).
  artifact_type?: string;
  // Все типы артефактов репозитория (смешанный: docker + helm) — enum-имена.
  artifact_types?: string[];
  // Суммарное число pull'ов; proto3 int64 сериализуется как СТРОКА.
  download_count?: string;
}

export interface RepositoryList {
  repositories: Repository[];
  next_page_token?: string;
}

// Tag — тег образа в репозитории. Единственная мутация — DeleteTag (async).
export interface Tag {
  tag: string;
  registry_id?: string;
  repository?: string;
  digest?: string;
  // proto3 int64 сериализуется в JSON как СТРОКА.
  size_bytes?: string;
  media_type?: string;
  // Время push этого тега.
  created_at?: string;
  architecture?: string;
  // Время последнего pull; zero/пусто = ни разу не скачивался.
  last_pulled_at?: string;
  // Кем запушен (identity/subject).
  pushed_by?: string;
  // Число pull'ов тега; proto3 int64 сериализуется как СТРОКА.
  download_count?: string;
}

export interface TagList {
  tags: Tag[];
  next_page_token?: string;
}
