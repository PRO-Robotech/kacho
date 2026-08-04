// TS-типы плоского API домена балансировщика (контракт proto ствола).
// Ресурсы плоские (нет envelope metadata/spec/status).
//
// Имена полей: край отдаёт camelCase — оба mux'а api-gateway собираются с
// `UseProtoNames: false` (`gateway/internal/restmux/strict_enum.go`). Объявления
// ниже — в snake_case, как в proto, и мост между ними один: `api/client.ts`
// переводит тело запроса snake_case → camelCase, а ответ обратно (`lib/case.ts`,
// пользовательские map'ы вроде labels не трогаются). То есть snake_case здесь —
// не то, что приходит с провода, а то, во что уже переведено. Убрать перевод,
// поверив, что имена совпадают, значит начать читать поля, которых в ответе нет.
//
// Здесь лежит ТОЛЬКО то, что это приложение действительно принимает с провода:
// конверт операции и три ресурса балансировщика. Прежде файл нёс ещё описания
// vpc-сетей и блочного хранения compute — ни одно из них не импортировалось, и
// все они описывали форму, которой в контракте больше нет (блочное хранение
// уехало в домен storage и переписано; у подсети переименованы диапазоны).
// Мёртвое описание рядом с живым читается как действующий контракт, поэтому
// они удалены, а не поправлены: своего потребителя у них не было.
//
// Замечание об именах домена: proto-пакет называется
// `kacho.cloud.loadbalancer.v1`, а REST-путь — `/nlb/v1/...`. Это два имени
// одного домена, сосуществующие в контракте намеренно; ни одно не является
// опечаткой другого.

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

// ====== nlb (Network Load Balancer) ======
// proto: kacho.cloud.loadbalancer.v1 (REST — /nlb/v1/...). Мутации async → Operation.

// VIP address-spec (per-family oneof v4_source/v6_source). Ровно один кейс на
// семейство: аллокация из подсети, привязка существующего Address, либо
// платформенный публичный пул (для EXTERNAL).
export interface NlbVipSource {
  subnet_id?: string;
  address_id?: string;
  public?: Record<string, never>;
}

// NetworkLoadBalancer — плоский ресурс (публичная проекция, tenant-facing).
export interface NetworkLoadBalancer {
  id: string;
  project_id: string;
  created_at?: string;
  name?: string;
  description?: string;
  labels?: Record<string, string>;
  region_id: string;
  // Единственный авторитетный режим размещения и обязательный вход Create.
  // Пара «external + zonal» невыразима by construction — её в наборе нет.
  placement?: "EXTERNAL_REGIONAL" | "INTERNAL_REGIONAL" | "INTERNAL_ZONAL";
  // Схема VIP: производная проекция `placement`, ТОЛЬКО на чтение
  // (на вход не принимается).
  type?: "INTERNAL" | "EXTERNAL";
  // Размещение INTERNAL-VIP: тоже производная проекция `placement`, только на
  // чтение. Для EXTERNAL пуста.
  placement_type?: "ZONAL" | "REGIONAL";
  // Желаемое административное состояние — замена снятым глаголам :start/:stop.
  // Выключенный балансировщик сохраняет конфигурацию, его таргеты сообщаются
  // как INACTIVE.
  admin_state?: "ADMIN_STATE_UNSPECIFIED" | "ADMIN_STATE_ENABLED" | "ADMIN_STATE_DISABLED";
  // Балансировка между зонами — только для REGIONAL (у ZONAL зона одна).
  cross_zone_enabled?: boolean;
  // vpc SecurityGroup, ограничивающие доступ к VIP. Только для INTERNAL
  // (группы сетевые); набор заменяется целиком.
  security_group_ids?: string[];
  // Зоны, из которых anycast-VIP не анонсируется (drain, только REGIONAL).
  disabled_announce_zones?: string[];
  session_affinity?: "FIVE_TUPLE" | "CLIENT_IP_ONLY";
  deletion_protection?: boolean;
  // Аллоцированные VIP Address-ресурсы (output-only, резолв id → vpc Address).
  v4_address_id?: string;
  v6_address_id?: string;
  // INACTIVE — ни один листенер не связан с целевой группой: проб нет,
  // трафик не пересылается. STARTING/STOPPING/STOPPED сняты вместе с глаголами.
  status?: string;
}

export interface NetworkLoadBalancerList {
  network_load_balancers: NetworkLoadBalancer[];
  next_page_token?: string;
}

// Listener — L4-обработчик балансировщика.
export interface Listener {
  id: string;
  project_id: string;
  created_at?: string;
  name?: string;
  description?: string;
  labels?: Record<string, string>;
  load_balancer_id: string;
  protocol?: "TCP" | "UDP";
  port?: number;
  target_port?: number;
  proxy_protocol_v2?: boolean;
  // Целевая группа листенера. Привязка перешла СЮДА со снятых глаголов
  // балансировщика (:attachTargetGroup / :detachTargetGroup) вместе с M:N-снимком.
  target_group_id?: string;
  default_target_group_id?: string;
  // Backend-порт, переотражённый из `TargetGroup.port` (output-only).
  resolved_backend_port?: number;
  // MISCONFIGURED — листенер объявлен, но обслуживать некому.
  substatus?: "SUBSTATUS_UNSPECIFIED" | "OK" | "MISCONFIGURED" | string;
  status?: string;
}

export interface ListenerList {
  listeners: Listener[];
  next_page_token?: string;
}

// ====== TargetGroup + встроенная проба ======

/** Общая часть всех ветвей пробы: необязательное переопределение порта. */
interface NlbProbePort {
  port?: number;
}

/** HTTP/HTTPS-проба: GET по `path`, здоровым считается код из `expected_codes`. */
interface NlbHttpProbe extends NlbProbePort {
  path?: string;
  expected_codes?: string;
  host?: string;
  headers?: Record<string, string>;
}

// NlbHealthCheck — встроенный объект-значение целевой группы, НЕ ресурс: имени
// у него нет (снято с контракта). Задана ровно одна ветвь из четырёх.
export interface NlbHealthCheck {
  tcp?: NlbProbePort;
  http?: NlbHttpProbe;
  https?: NlbHttpProbe;
  grpc?: NlbProbePort & { service_name?: string };
  // Порт, на котором проба фактически идёт: переопределение ветви, иначе
  // `TargetGroup.port` (output-only).
  effective_port?: number;
  // Duration — на проводе строка секунд с хвостовым «s» («2s»).
  interval?: string;
  timeout?: string;
  unhealthy_threshold?: number;
  healthy_threshold?: number;
}

// TargetGroup — набор target'ов + одна встроенная проба.
export interface TargetGroup {
  id: string;
  project_id: string;
  created_at?: string;
  name?: string;
  description?: string;
  labels?: Record<string, string>;
  region_id: string;
  // Единственный backend-порт группы; его переотражает Listener.resolved_backend_port.
  port?: number;
  // Duration (строка секунд, «300s»). Прежние целочисленные имена
  // deregistration_delay_seconds / slow_start_seconds зарезервированы: тело,
  // ключёванное на них, край отбрасывает молча.
  deregistration_delay?: string;
  slow_start?: string;
  health_check?: NlbHealthCheck;
  status?: string;
}

export interface TargetGroupList {
  target_groups: TargetGroup[];
  next_page_token?: string;
}
