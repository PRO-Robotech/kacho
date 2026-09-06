// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

import (
	"time"

	"fmt"

	"google.golang.org/grpc"

	corecfg "github.com/PRO-Robotech/kacho/pkg/config"
	"github.com/PRO-Robotech/kacho/pkg/grpcclient"
	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
)

// EnvPrefix — корневой сегмент имён env для kacho-compute (KACHO_<DOMAIN>).
// LoadPrefixed выводит env-имя каждого поля из иерархии: EnvPrefix + tag/field.
//
// Экспортирован, чтобы гейт «чарт не выставляет переменных, которых процесс не
// читает» перечислял имена ТЕМ ЖЕ префиксом, каким их читает Load: собственная
// копия префикса в тесте разъехалась бы с кодом ровно так же, как разъехался
// сам чарт.
const EnvPrefix = "KACHO_COMPUTE"

// Config — конфигурация kacho-compute.
type Config struct {
	DBHost     string `envconfig:"KACHO_COMPUTE_DB_HOST" default:"localhost"`
	DBPort     string `envconfig:"KACHO_COMPUTE_DB_PORT" default:"5432"`
	DBUser     string `envconfig:"KACHO_COMPUTE_DB_USER" default:"compute"`
	DBPassword string `envconfig:"KACHO_COMPUTE_DB_PASSWORD" required:"true"`
	DBName     string `envconfig:"KACHO_COMPUTE_DB_NAME" default:"kacho_compute"`
	// DBSSLMode — sslmode для DSN. По умолчанию `disable` для dev-стенда;
	// production обязан выставить `verify-full` (security P0).
	DBSSLMode string `envconfig:"KACHO_COMPUTE_DB_SSLMODE" default:"disable"`
	// DBMaxConns — лимит pgx pool (0 = pgx default max(4, NumCPU)).
	DBMaxConns int `envconfig:"KACHO_COMPUTE_DB_MAX_CONNS" default:"0"`

	GrpcPort string `envconfig:"KACHO_COMPUTE_GRPC_PORT" default:"9090"`

	// InternalGrpcPort — порт для cluster-internal RPC (админ-каталог, реализация,
	// владение узлом).
	// НЕ выставляется через api-gateway external endpoint.
	InternalGrpcPort string `envconfig:"KACHO_COMPUTE_INTERNAL_PORT" default:"9091"`

	// MetricsAddr — адрес cluster-internal diagnostic HTTP-listener'а
	// (/metrics + /healthz + /readyz). Default ":9095" — отдельный internal-порт
	// (НЕ маршрутизируется на external endpoint; cluster-internal scrape +
	// kubelet-проба /readyz). Пустое значение явно отключает listener (back-compat).
	MetricsAddr string `envconfig:"KACHO_COMPUTE_METRICS_ADDR" default:":9095"`

	// IAMGRPCAddr — адрес kaname (ProjectService.Get — project-existence-check).
	IAMGRPCAddr string `envconfig:"KACHO_COMPUTE_IAM_GRPC_ADDR" default:"kaname.kacho.svc:9090"`

	// GeoGRPCAddr — адрес kacho-geo (geo.v1.ZoneService.Get, public :9090) для
	// валидации Instance/Disk.zone_id. Geography (Region/Zone) — leaf-сервис
	// kacho-geo; compute больше не валидирует zone_id по своей таблице `zones` и не
	// обслуживает Region/Zone.
	GeoGRPCAddr string `envconfig:"KACHO_COMPUTE_GEO_GRPC_ADDR" default:"kacho-geo.kacho.svc:9090"`

	// VPCInternalGRPCAddr — адрес kacho-vpc internal listener (:9091) для
	// InternalNetworkInterfaceService.Attach/Detach/ListByInstance (NIC↔Instance
	// attach-saga, S4). Internal-only (не external endpoint). Пустое значение
	// → NIC-ребро не сконфигурировано (NoopNicClient: attach fail-closed Unavailable,
	// зеркало опускается).
	VPCInternalGRPCAddr string `envconfig:"KACHO_COMPUTE_VPC_INTERNAL_GRPC_ADDR" default:"kacho-vpc.kacho.svc:9091"`

	// VPCGRPCAddr — адрес kacho-vpc PUBLIC listener (:9090) для
	// vpc.v1.SubnetService.Get: placement-валидация подсети NIC-спеки на
	// Instance.Create (зона инстанса == зона подсети; REGIONAL/anycast — только
	// региональная когерентность). Читается под идентичностью вызывающего.
	// Пустое значение → ребро не сконфигурировано: Create с NIC-спеками
	// fail-closed Unavailable (coherence неверифицируема).
	VPCGRPCAddr string `envconfig:"KACHO_COMPUTE_VPC_GRPC_ADDR" default:"kacho-vpc.kacho.svc:9090"`

	// StorageInternalGRPCAddr — адрес kacho-storage internal listener (:9091) для
	// InternalVolumeService.Attach/Detach/ListAttachments (volume↔Instance attach-saga).
	// Internal-only (не external endpoint). Пустое значение → storage-ребро не
	// сконфигурировано (NoopStorageClient: attach fail-closed Unavailable, зеркало
	// опускается).
	StorageInternalGRPCAddr string `envconfig:"KACHO_COMPUTE_STORAGE_INTERNAL_GRPC_ADDR" default:"kacho-storage.kacho.svc:9091"`

	// SkipPeerValidation — отключить cross-service existence-check (project в
	// kaname, zone_id в kacho-geo) → no-op. Для unit/newman/load-тестов без
	// поднятых peer-сервисов.
	SkipPeerValidation bool `envconfig:"KACHO_COMPUTE_SKIP_PEER_VALIDATION" default:"false"`

	// AuthMode — fail-closed гейт перед IAM merge: `dev` | `production` | `production-strict`.
	//
	// Дефолт — production (secure-by-default, kacho core rule «production-mode обязателен ВЕЗДЕ»): незаданный env НЕ должен
	// поднимать сервис в insecure-posture. dev — явный opt-in локальных фикстур и
	// dev-профиля стенда (values.dev.yaml выставляет его явно).
	AuthMode string `envconfig:"KACHO_COMPUTE_AUTH_MODE" default:"production"`

	// AuthZIAMGRPCAddr — gRPC адрес kaname internal-port'а для Check.
	//
	// ОБЯЗАТЕЛЕН в любом режиме, включая dev: пустое значение — отказ старта в
	// конструкторе дескриптора (ребро решения о доступе обязано быть объявлено
	// ЯВНО, адрес и транспорт вместе). Прежняя редакция обещала обратное —
	// «interceptor не навешивается, graceful start без kaname в dev», — и это
	// было верно, пока звено собирал сам композиционный корень: процесс поднимался
	// и обслуживал запросы, не спрашивая ни о чьих правах. Такой ветки у носителя
	// контура нет.
	//
	// Обычно адрес совпадает с IAMGRPCAddr (тот же сервис), но порт другой: 9091
	// (internal) против 9090 (публичный ProjectService.Get).
	AuthZIAMGRPCAddr string `envconfig:"KACHO_COMPUTE_AUTHZ_IAM_GRPC_ADDR" default:""`

	// QuotaAuthority — ОБЪЯВЛЕНИЕ домена величин. Ровно два законных значения:
	// адрес в форме host:port либо слово `not-deployed`. Незаданное значение —
	// отказ старта: умолчание означало бы выбор за оператора между «потолки
	// действуют» и «потолков нет», и выбор этот был бы невидим.
	//
	// Объявление ОДНО на обе полосы ребра — разрешение величины на пути запроса
	// и фоновую дельту. Приёмка ухода модуля квотирования из службы доступа,
	// решение Д1 (имя документа приёмки здесь не цитируется: страж комментариев
	// этой службы отвергает процессный лексикон, а он входит в имя файла).
	QuotaAuthority string `envconfig:"KACHO_COMPUTE_QUOTA_AUTHORITY" default:""`
	// AuthZBreakglass — аварийная ручка, чей ПРЕДМЕТ УЖЕ СНЯТ переездом контура на
	// общий носитель, и это записано здесь, а не оставлено в имени.
	//
	// Прежде она означала «пропускать все RPC без проверки прав»: звено решения
	// собирал сам композиционный корень, и ручка выбирала его аварийную ветку.
	// Носитель такой ветки не имеет вовсе — «цепочка без звена решения» им не
	// выражается, — поэтому сегодня взведённая ручка НЕ снимает ни одной проверки
	// прав ни на одном слушателе.
	//
	// Что она делает сейчас, целиком: освобождает от стражи круга отправителей в
	// `Validate` и участвует в двух отказах старта боевой посадки
	// (`cmd/compute/validateAuthMode`), где взведённость остаётся запретом. Круг при
	// этом всё равно судится конструктором дескриптора, поэтому освобождение в
	// `Validate` наблюдаемого послабления не даёт.
	//
	// Оставлена, а не снята, потому что снятие — отдельное изменение поведения со
	// своим обзором: её взведённость сегодня РОНЯЕТ боевой старт, и убрать ручку
	// значит убрать этот отказ вместе с ней.
	AuthZBreakglass bool `envconfig:"KACHO_COMPUTE_AUTHZ_BREAKGLASS" default:"false"`

	// AuthZCacheTTL — окно кеша положительных вердиктов авторизации, оно же ОКНО
	// ОТЗЫВА: столько субъект, у которого право уже отобрали, продолжает
	// проходить. Отрицательные вердикты не кешируются никогда, поэтому свежая
	// выдача видна сразу, а ждёт один лишь отзыв.
	//
	// Ручка заведена не ради нового поведения: значение то же, что было
	// умолчанием платформы. Изменилось то, что число стало ВЫБРАННЫМ — параметр
	// безопасности, которого никто не выбирал, нельзя ни обсудить, ни отозвать,
	// ни сузить на конкретной посадке. Потолок объявлен политикой
	// (pkg/authz.RevocationPolicy.Ceiling), перепись — там же; смена значения без
	// правки политики роняет гейт и называет оба числа.
	//
	// Ноль означает «беру объявленную политику», а не «кеша нет».
	AuthZCacheTTL time.Duration `envconfig:"KACHO_COMPUTE_AUTHZ_CACHE_TTL" default:"5s"`

	// AuthZCheckTimeout — срок ОДНОГО вопроса о правах владельцу модели.
	//
	// Ручка заведена не ради нового поведения: значение то же, что было
	// умолчанием платформы (прежняя фабрика звена срок не прокидывала вовсе, и
	// его выбирала библиотека). Изменилось то, что оно стало ВЫБРАННЫМ.
	// Неотвечающий сосед без срока вешает горутину навсегда, и звено не доходит
	// до своей ветки fail-closed — горутины копятся до исчерпания процесса.
	AuthZCheckTimeout time.Duration `envconfig:"KACHO_COMPUTE_AUTHZ_CHECK_TIMEOUT" default:"2s"`

	// AuthZDenyBudgetPerSec — устойчивый темп (в секунду на принципала) проверок,
	// чей исход кэш НЕ поглощает: отказ, сокрытие существования, промах «нет
	// пути», недоступность модели. По исчерпании звено отвечает
	// `ResourceExhausted`, не обращаясь к kaname, — то есть сбрасывает шторм с
	// него.
	//
	// Величина 100 не выдумана: это то же число, которое платформа уже выбрала
	// для того же механизма у vpc, geo, storage и nlb. Умолчания у самого
	// механизма НЕТ (неположительное значение он читает как «ограничения нет»),
	// поэтому число обязано быть названо здесь.
	//
	// Изъятия («ронять некого») у compute быть не может: решение о доступе он
	// принимает не у себя, а вопросом к kaname — сетевой сосед, которого шторм
	// отказов уронит, у него ЕСТЬ, и на том же соединении живут пообъектный
	// сужатель видимости и регистрация владельца.
	AuthZDenyBudgetPerSec float64 `envconfig:"KACHO_COMPUTE_AUTHZ_DENY_BUDGET_PER_SEC" default:"100"`

	// AdmissionPublic / AdmissionInternal — ПОТОЛОК ТЕМПА и ОДНОВРЕМЕННОСТИ на
	// вызывающего, по одному набору на слушатель. Не путать с бюджетом отказов
	// выше: тот сбрасывает шторм ОТКАЗОВ с владельца модели, этот ограничивает
	// ПОТОК запросов к нам самим, и стоимость запроса здесь высокая по
	// построению — мутация есть три строки в базе, чтение есть до тысячи
	// объектов на страницу с проверкой прав партиями.
	//
	// # Почему у ручек НЕТ умолчаний в тегах
	//
	// Полы у слушателей РАЗНЫЕ, поэтому умолчание пришлось бы написать дважды —
	// то есть завести седьмую пропись чисел, которые уже названы в фундаменте
	// (grpcsrv.PlatformPublicAdmission / PlatformInternalAdmission). Молчание
	// посадки разрешается там же, где живут числа: пустой набор означает ПОЛ
	// ПЛАТФОРМЫ, а не ноль. Ноль механизм читает как «не ограничиваем», и
	// слушатель выглядел бы защищённым, ни разу не отказав.
	//
	// # Что посадка вправе сказать
	//
	// Ничего (берётся пол) либо ВЕСЬ набор из четырёх осей. Частичное
	// объявление отвергается стартом с именем слушателя: оператор, задавший темп
	// и забывший одновременность, считает предел выставленным, а незаполненная
	// ось не ограничивает ничего. Своя величина осмысленна потому, что вёдра
	// живут В ПРОЦЕССЕ: при N репликах эффективный предел равен N × объявленного,
	// и запас у стенда из одной реплики и из двадцати разный.
	//
	// Имена: KACHO_COMPUTE_ADMISSION_{PUBLIC,INTERNAL}_{READ_PER_SEC,
	// MUTATION_PER_SEC,BURST_FACTOR,IN_FLIGHT}.
	AdmissionPublic   grpcsrv.AdmissionKnobs `envconfig:"ADMISSION_PUBLIC"`
	AdmissionInternal grpcsrv.AdmissionKnobs `envconfig:"ADMISSION_INTERNAL"`

	// HandlingBudget — верхняя граница обработки ОДНОГО вызова (серверный срок).
	// Более строгий срок вызывающего уважается; окно не расширяется никогда.
	//
	// 30s — то же число, что платформа выбрала для той же величины у vpc, geo,
	// storage и nlb. Это ПОТОЛОК, а не цель: он обязан с запасом накрывать вопрос
	// о правах (KACHO_COMPUTE_AUTHZ_CHECK_TIMEOUT, 2s), пообъектное сужение
	// страницы (KACHO_COMPUTE_LIST_FILTER_TIMEOUT_MS × волны) и запрос к своей БД.
	// Предмет его — не задержка, а вызов БЕЗ срока: он держит соединение из
	// ограниченного пула столько, сколько выполняется его запрос, и MaxConns таких
	// вызовов отказывают весь сервис (CWE-770).
	//
	// К серверному стриму эта величина НЕ применяется — у подписки своя
	// (KACHO_COMPUTE_WATCH_STREAM_BUDGET).
	HandlingBudget time.Duration `envconfig:"KACHO_COMPUTE_HANDLING_BUDGET" default:"30s"`

	// SubscriptionStreamBudget — СРОК ЖИЗНИ одного потока подписки.
	//
	// По истечении поток закрывается ЧИСТО, и клиент возобновляется со своей
	// позиции: обрыв — штатное событие, а не отказ. Величина обязана заметно
	// превосходить границу обработки одиночного вызова, иначе поток закрывался бы
	// раньше, чем доезжает первое событие догона, и подписчик читал бы штатное
	// закрытие как «изменений нет». Носитель судит это отношение сам и роняет
	// старт на негодной величине.
	SubscriptionStreamBudget time.Duration `envconfig:"KACHO_COMPUTE_SUBSCRIPTION_STREAM_BUDGET" default:"1h"`

	// SubscriptionMaxStreams — потолок ОДНОВРЕМЕННЫХ потоков этого процесса.
	//
	// Это АРИФМЕТИКА СОЕДИНЕНИЙ, а не вкус: каждый поток держит выделенное
	// соединение ВНЕ ПУЛА всё время своей жизни, поэтому
	//
	//	число реплик × этот потолок + пулы всех реплик ≤ max_connections базы
	//
	// Величина выбрана от ПРЕДЕЛА БАЗЫ, а не от ожидаемого спроса: в боевом
	// профиле база этой службы принимает 100 соединений, пул ширины своей не
	// объявляет (берёт умолчание драйвера), и запас обязан остаться и ему, и
	// миграциям, и опросу готовности.
	//
	// Асимметрия, которая и решает выбор: упереться в СВОЙ потолок — чистый отказ
	// ОДНОМУ вызывающему (`RESOURCE_EXHAUSTED`, повтор осмыслен), упереться в
	// предел БАЗЫ — отказ всему процессу и всем арендаторам сразу, включая тех,
	// кто подписки не открывал. Поэтому свой потолок держится заведомо ниже.
	//
	// Превышение отвечает ОТКАЗОМ, а не молчаливой очередью: очередь превратила бы
	// исчерпание в неограниченное ожидание, неотличимое для клиента от
	// «событий нет».
	//
	// Поднимая величину, подними и предел базы — вместе, а не по одному: сегодня
	// их произведение не сверяет никто (гейт посадки считает только пул).
	SubscriptionMaxStreams int `envconfig:"KACHO_COMPUTE_SUBSCRIPTION_MAX_STREAMS" default:"16"`

	// SubscriptionIdlePoll — холостой перепрос журнала.
	//
	// Он не «на всякий случай»: ОТКАТИВШИЙСЯ писатель уведомления не шлёт, и
	// подтверждение границы устоявшегося приезжает именно этим перепросом. Ноль
	// означал бы, что горизонт не подтверждается никогда, и общий сервер такую
	// величину отвергает на подъёме.
	SubscriptionIdlePoll time.Duration `envconfig:"KACHO_COMPUTE_SUBSCRIPTION_IDLE_POLL" default:"2s"`

	// AuthZTrustedForwarderSANs — allow-list cert-identity SAN'ов, которым разрешено
	// форвардить end-user principal в x-kacho-principal-* metadata (обычно
	// единственный — api-gateway SA, SAN spiffe://kacho.cloud/ns/<ns>/sa/kacho-api-gateway).
	// Принимает comma-separated список. Пусто (default) → любой mTLS-verified peer
	// доверен как форвардер (паритет с insecure dev back-compat и kaname) — допустимо
	// ТОЛЬКО в dev: validateAuthMode() fail-closed отвергает пустой список в любом
	// production-режиме (Config.Validate). Задаётся
	// в production для defense-in-depth против confused-deputy: внутренний сервис со
	// своим валидным client-cert'ом не сможет выдать себя за пользователя. На обоих
	// листенерах principal trust-gated через grpcsrv.UnaryCertIdentityExtract +
	// UnaryTrustedPrincipalExtract(WithTrustedForwarders(...)) — без verified cert'а
	// (или вне allow-list) forwarded principal снимается.
	AuthZTrustedForwarderSANs []string `envconfig:"KACHO_COMPUTE_AUTHZ_TRUSTED_FORWARDER_SANS"`

	// AuthZTrustAnyForwarder — ЯВНЫЙ опт-ин «круг не сужаем», действующий ТОЛЬКО
	// вне боевого режима. Нужен для локальных in-process фикстур, где ни
	// сертификатов, ни шлюза нет.
	//
	// Он существует потому, что стража круга срабатывает на ЛЮБОМ старте, а не
	// только в боевом режиме: контроль, чья ветка на локальном стенде не
	// исполняется ни разу, обнаруживает «забыл выставить круг» только на боевом
	// профиле, где цена ошибки максимальна. Оставленный незаданным (false) =
	// отказ старта на пустом круге. В боевом режиме НЕ действует — иначе это была
	// бы ручка, снимающая защиту на развёрнутом стенде.
	AuthZTrustAnyForwarder bool `envconfig:"KACHO_COMPUTE_AUTHZ_TRUST_ANY_FORWARDER" default:"false"`

	// AuthZTrustDomain — ДОМЕН ДОВЕРИЯ установки: то, чьи сертификаты она признаёт своими.
	//
	// Круг отправителей выше называет, КОМУ позволено говорить за пользователя;
	// домен отвечает на предыдущий вопрос — чьи вообще предъявители наши. Пока он
	// был скомпилирован, установка меняла его только пересборкой: сертификаты
	// выпускаются под доменом из величины профиля, а принимающая сторона читала
	// литерал, и расходились они МОЛЧА — законный отправитель переставал
	// опознаваться, а отказ выглядел как вызов без личности.
	//
	// Умолчания нет намеренно: непустое умолчание сделало бы контроль на вид
	// включённым и увело бы установку, забывшую назвать свой домен, в чужой.
	// Пустая величина — отказ старта (Validate), а не «принимаем любой».
	//
	// ENV `KACHO_COMPUTE_AUTHZ_TRUST_DOMAIN`.
	AuthZTrustDomain string `envconfig:"KACHO_COMPUTE_AUTHZ_TRUST_DOMAIN"`

	// ===== per-object filtered List =====
	//
	// Все ListFilter* — production-edition: configurable, no hardcoded.
	// Reuses AuthZIAMGRPCAddr (+ per-edge IAMAuthzMTLS creds) as the iam-authorize
	// endpoint (kaname internal :9091 — AuthorizeService.BatchCheck).
	//
	// NB: результирующего «размера allow-list» здесь БОЛЬШЕ НЕТ (прежний
	// MaxResults): видимость спрашивается per-object по прочитанной СТРАНИЦЕ, так
	// что усечь её нечем — стоимость и полнота задаются page_size, а не knob'ом.

	// ListFilterEnabled — master-switch. true → handler спрашивает iam.BatchCheck
	// про id прочитанной страницы. false → no filter (handler bypass).
	ListFilterEnabled bool `envconfig:"KACHO_COMPUTE_LIST_FILTER_ENABLED" default:"true"`

	// ListFilterTimeoutMs — per-call deadline ОДНОГО iam.BatchCheck (страница >100
	// id режется на батчи ≤100, у каждого свой deadline), НЕ бюджет всей
	// фильтрации: бюджет операции выводится в authzfilter.NewFGAFilter.
	//
	// Default 1000ms, а не прежние 500ms: батчи страницы идут ограниченным
	// fan-out'ом (authzfilter.defaultParallelism = 5), поэтому worst-case глубина
	// на предельной странице (page_size=1000) — 4 волны, а не 20 последовательных
	// хопов, и 4×1s помещается в выведенный бюджет (6s) с запасом 33%. Прежние
	// 500ms были подогнаны не под латентность здорового пира, а под число
	// последовательных хопов — загруженный iam ронял позитивный List в 503.
	ListFilterTimeoutMs int `envconfig:"KACHO_COMPUTE_LIST_FILTER_TIMEOUT_MS" default:"1000"`

	// ListFilterCacheTTLMs — TTL ПОЛОЖИТЕЛЬНОГО per-object вердикта. Short (5s)
	// so что access-binding revoke виден ≤5s; lower → больше RTT к iam.
	// Отрицательные вердикты не кешируются вовсе (иначе свежий грант / только что
	// созданный свой ресурс были бы невидимы весь TTL).
	ListFilterCacheTTLMs int `envconfig:"KACHO_COMPUTE_LIST_FILTER_CACHE_TTL_MS" default:"5000"`

	// ListFilterCacheMaxEntries — bound для cache, вытеснение LRU (см.
	// authzfilter.putCache). Кеш НИКАК не ограничивает видимость: страница
	// проверяется целиком независимо от его размера.
	// 10000 enough для ~1000 concurrent users × 10 hot (subject, type, id) verdicts.
	ListFilterCacheMaxEntries int `envconfig:"KACHO_COMPUTE_LIST_FILTER_CACHE_MAX_ENTRIES" default:"10000"`

	// ListFilterFailOpen — degraded mode. true → на ошибке iam: handler возвращает
	// страницу НЕотфильтрованной (+ audit-WARN); false → Unavailable. **Default
	// false** (fail-closed = secure). Set to true только в break-glass.
	ListFilterFailOpen bool `envconfig:"KACHO_COMPUTE_LIST_FILTER_FAIL_OPEN" default:"false"`

	// ListFilterBreakglass — аварийный режим: когда модели прав на этой посадке нет
	// вовсе (фильтр выключен либо эндпоинт не задан), списки отдаются НЕсуженными, а
	// поток журнала изменений стартует, вместо отказа.
	//
	// Он остаётся явным исключением, а не умолчанием: прежде «фильтр выключен» само
	// по себе означало сквозной проход, и вся защита держалась на загрузочном страже
	// — то есть существовала ровно до первой конфигурации, которая его не взвела.
	// Теперь пропуск требуется ОБЪЯВИТЬ, и каждое срабатывание считается и
	// называется (`listnarrow.Counts`).
	ListFilterBreakglass bool `envconfig:"KACHO_COMPUTE_LIST_FILTER_BREAKGLASS" default:"false"`

	// ===== register-drainer (FGA owner-tuple через kaname) =====
	//
	// FGARegisterDrainerEnabled — включает register-drainer (corelib outbox/drainer):
	// дренит compute_fga_register_outbox, применяя intent через
	// InternalIAMService.RegisterResource/UnregisterResource. Default-on
	// в dev (без него созданные ресурсы не получат owner-tuple → per-resource Check
	// DENY). Это in-process goroutine, не cross-cluster rollout-flag.
	FGARegisterDrainerEnabled bool `envconfig:"KACHO_COMPUTE_FGA_REGISTER_DRAINER_ENABLED" default:"true"`

	// FGARegisterApplyConcurrency — сколько owner-tuple register-intent'ов одного
	// claim-батча drainer применяет ПАРАЛЛЕЛЬНО через kaname RegisterResource
	// (corelib drainer.Config.ApplyConcurrency). Последовательный drainer упирается
	// в ~1/apply_latency: под write-burst RegisterResource таймаутит (~5s) →
	// ceiling ~0.2–0.5 tuple/s, что на порядок НИЖЕ create-throughput воркера
	// (~6.7/s) → outbox-backlog расходится без границы, v_list не материализуется в
	// окне ретрая (list-read-your-writes fail). Параллельный apply (N вызовов —
	// внешний gRPC, БЕЗ доп. conn'ов пула, exactly-once не меняется: claim-tx держит
	// per-row lock) поднимает ceiling до ~N/apply_latency, закрывая инверсию
	// producer/consumer. Default 16 — headroom при высокой apply-latency; тюнится
	// без ребилда. 1 = историческое последовательное поведение.
	FGARegisterApplyConcurrency int `envconfig:"KACHO_COMPUTE_FGA_REGISTER_APPLY_CONCURRENCY" default:"16"`

	// RequireIAM — fail-closed boot-gate. When true,
	// mutating Create is refused (UNAVAILABLE) and readiness is NotReady until the
	// register-drainer is IAM-connected, so no resource is ever created without a
	// deliverable owner-tuple intent. Default false (dev back-compat: old Warn
	// behaviour, Create allowed). In production: true (single canonical mode, N5).
	RequireIAM bool `envconfig:"KACHO_COMPUTE_REQUIRE_IAM" default:"false"`

	// ===== opt-in mTLS (per-edge) =====
	//
	// Каждое ребро — независимый grpcclient.TLSClient / grpcsrv.TLSServer value-struct.
	// envconfig.Process(EnvPrefix, &cfg) выводит env-имена из тега родительского поля:
	// `IAM_REGISTER_MTLS` → KACHO_COMPUTE_IAM_REGISTER_MTLS_{ENABLE,CERTFILE,KEYFILE,
	// CAFILES,SERVERNAME}. Enable=false (default) → insecure (dev backward-compat).
	// Per-edge enable → независимый rollback/rollout.

	// IAMRegisterMTLS — client-creds для ребра compute→iam (register-drainer →
	// InternalIAMService.RegisterResource/UnregisterResource). FGA-proxy edge.
	IAMRegisterMTLS grpcclient.TLSClient `envconfig:"IAM_REGISTER_MTLS"`

	// ===== CLIENT mTLS на read/authz рёбрах compute→iam =====
	//
	// register-drainer ребро закрыто отдельно (IAMRegisterMTLS). Это — зеркало того
	// же паттерна на ОСТАВШИХСЯ read/authz iam-conn'ах, которые ранее диалились
	// server-auth-only bool-флагами БЕЗ client-cert (флаги удалены). Когда iam
	// требует RequireAndVerifyClientCert на обоих listener'ах, такой dial падает на
	// TLS-handshake — оба ребра ОБЯЗАНЫ предъявлять kacho-compute-client-tls cert
	// (completeness-инвариант). Два отдельных поля, т.к. ServerName различается
	// per-listener: ProjectService.Get → :9090 (kaname), Check/list-filter →
	// :9091 (kaname-internal); один общий TLSClient не несёт оба ServerName.

	// IAMProjectMTLS — client-creds для ребра compute→iam ProjectService.Get
	// (existence + leaf-owner, public :9090). ServerName = kaname.*.
	IAMProjectMTLS grpcclient.TLSClient `envconfig:"IAM_PROJECT_MTLS"`

	// IAMAuthzMTLS — client-creds для ребра compute→iam per-RPC
	// InternalIAMService.Check + FGA-filtered List (один conn → AuthZIAMGRPCAddr,
	// internal :9091). ServerName = kaname-internal.*.
	IAMAuthzMTLS grpcclient.TLSClient `envconfig:"IAM_AUTHZ_MTLS"`

	// QuotaAuthorityMTLS — client-creds для ребра compute→домен величин
	// (InternalLimitService.Resolve на пути запроса И ListChangedSince фоновой
	// дельтой — обе полосы ОДНОГО ребра, поэтому удостоверение одно).
	//
	// Своё, а не заимствованное у authz-ребра: адрес домена величин объявляется
	// отдельно (KACHO_COMPUTE_QUOTA_AUTHORITY), и удостоверение обязано следовать
	// за адресом. Половина пары — адрес есть, удостоверения нет — отвергается
	// стражем старта.
	QuotaAuthorityMTLS grpcclient.TLSClient `envconfig:"QUOTA_AUTHORITY_MTLS"`

	// GeoMTLS — client-creds для ребра compute→geo (geo.v1.ZoneService.Get,
	// zone_id-валидация Instance). Enable=false (default) → insecure
	// (dev backward-compat); enable=true без валидного cert-trio → startup error
	// (fail-closed, без silent insecure-fallback) — паритет с IAM*MTLS.
	GeoMTLS grpcclient.TLSClient `envconfig:"GEO_MTLS"`

	// VPCNicMTLS — client-creds для ребра compute→vpc InternalNetworkInterfaceService
	// (:9091 internal). Enable=false (default) → insecure (dev backward-compat);
	// enable=true без валидного cert-trio → startup error (fail-closed) — паритет с
	// Geo/IAM*MTLS.
	VPCNicMTLS grpcclient.TLSClient `envconfig:"VPC_NIC_MTLS"`

	// VPCMTLS — client-creds для ребра compute→vpc SubnetService.Get (:9090
	// public, placement-валидация подсети NIC-спеки). Имя envconfig-группы
	// (`KACHO_COMPUTE_VPC_MTLS_*`) уже рендерится чартом под блоком
	// `mtls.edges.vpc` («compute→vpc NIC-spec validation») — до появления самого
	// ребра эта группа была объявлена в чарте, но не читалась ни одним полем
	// конфига. Enable=false (default) → insecure (dev backward-compat);
	// enable=true без валидного cert-trio → startup error (fail-closed).
	VPCMTLS grpcclient.TLSClient `envconfig:"VPC_MTLS"`

	// StorageMTLS — client-creds для ребра compute→storage InternalVolumeService
	// (:9091 internal). Enable=false (default) → insecure (dev backward-compat);
	// enable=true без валидного cert-trio → startup error (fail-closed) — паритет с
	// Geo/IAM/VPCNic*MTLS.
	StorageMTLS grpcclient.TLSClient `envconfig:"STORAGE_MTLS"`

	// PublicServerMTLS — server-creds для публичного listener (:9090, GrpcPort).
	PublicServerMTLS grpcsrv.TLSServer `envconfig:"PUBLIC_SERVER_MTLS"`

	// InternalServerMTLS — server-creds для cluster-internal listener (:9091,
	// InternalGrpcPort).
	InternalServerMTLS grpcsrv.TLSServer `envconfig:"INTERNAL_SERVER_MTLS"`
}

// IAMRegisterClientCreds возвращает grpc.DialOption для ребра compute→iam
// (register-drainer). Enable=false → insecure (dev backward-compat); enable=true
// без валидного cert-trio → error (fail-closed, без silent insecure-fallback).
func (c Config) IAMRegisterClientCreds() (grpc.DialOption, error) {
	return grpcclient.TLSClientCreds(c.IAMRegisterMTLS)
}

// IAMProjectClientCreds возвращает grpc.DialOption для ребра compute→iam
// ProjectService.Get (existence/leaf-owner, :9090). Enable=false → insecure (dev);
// enable=true без валидного cert-trio → error (fail-closed).
func (c Config) IAMProjectClientCreds() (grpc.DialOption, error) {
	return grpcclient.TLSClientCreds(c.IAMProjectMTLS)
}

// IAMAuthzClientCreds возвращает grpc.DialOption для ребра compute→iam
// InternalIAMService.Check + FGA-filtered List (:9091). Enable=false → insecure
// (dev); enable=true без валидного cert-trio → error (fail-closed).
func (c Config) IAMAuthzClientCreds() (grpc.DialOption, error) {
	return grpcclient.TLSClientCreds(c.IAMAuthzMTLS)
}

// GeoClientCreds возвращает grpc.DialOption для ребра compute→geo
// (geo.v1.ZoneService.Get, zone_id-валидация Instance, S4). Enable=false →
// insecure (dev); enable=true без валидного cert-trio → error (fail-closed).
func (c Config) GeoClientCreds() (grpc.DialOption, error) {
	return grpcclient.TLSClientCreds(c.GeoMTLS)
}

// VPCNicClientCreds возвращает grpc.DialOption для ребра compute→vpc
// InternalNetworkInterfaceService (NIC-attach saga, :9091 internal). Enable=false →
// insecure (dev); enable=true без валидного cert-trio → error (fail-closed).
func (c Config) VPCNicClientCreds() (grpc.DialOption, error) {
	return grpcclient.TLSClientCreds(c.VPCNicMTLS)
}

// StorageClientCreds возвращает grpc.DialOption для ребра compute→storage
// InternalVolumeService (volume-attach saga, :9091 internal). Enable=false →
// insecure (dev); enable=true без валидного cert-trio → error (fail-closed).
func (c Config) StorageClientCreds() (grpc.DialOption, error) {
	return grpcclient.TLSClientCreds(c.StorageMTLS)
}

// PublicServerCreds возвращает grpc.ServerOption для публичного listener (:9090).
func (c Config) PublicServerCreds() (grpc.ServerOption, error) {
	return grpcsrv.TLSServerCreds(c.PublicServerMTLS)
}

// InternalServerCreds возвращает grpc.ServerOption для internal listener (:9091).
func (c Config) InternalServerCreds() (grpc.ServerOption, error) {
	return grpcsrv.TLSServerCreds(c.InternalServerMTLS)
}

// baseDSN — стандартный postgres DSN без pgxpool-специфичных параметров
// (пригоден и для pgxpool, и для database/sql.Open("pgx")).
func (c Config) baseDSN() string {
	mode := c.DBSSLMode
	if mode == "" {
		mode = "disable"
	}
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=%s",
		c.DBUser, c.DBPassword, c.DBHost, c.DBPort, c.DBName, mode,
	)
}

// SingleConnDSN — строка подключения для ОДИНОЧНОГО соединения (вне пула).
//
// Отличается от [Config.DSN] ровно отсутствием пуловых параметров, и различие
// несущее: `pool_max_conns` понимает pgxpool, а `pgx.Connect` — нет. Разбор
// строки на этом не спотыкается: неизвестный ключ уезжает серверу
// runtime-параметром в стартовом пакете, и Postgres отвечает FATAL уже на
// ПОДКЛЮЧЕНИИ. Отказ поэтому наступает не на сборке, а у каждого потребителя в
// бою, и выглядит тихо — источник «недоступен» и никогда ничем иным.
//
// Нужна тем, кто держит собственную сессию: `LISTEN` подписки не может жить в
// пуле — сессия вернулась бы в него вместе с подпиской.
func (c Config) SingleConnDSN() string {
	return c.baseDSN()
}

// DSN — connection string для pgxpool (поддерживает pool_max_conns).
// НЕ использовать для database/sql.Open("pgx") (pool_max_conns → unknown PG-param → FATAL).
func (c Config) DSN() string {
	dsn := c.baseDSN()
	if c.DBMaxConns > 0 {
		dsn += fmt.Sprintf("&pool_max_conns=%d", c.DBMaxConns)
	}
	return dsn
}

// MigrateDSN — connection string для goose/database/sql и dedicated Watch-conn
// (без pgxpool-параметров).
func (c Config) MigrateDSN() string {
	return c.baseDSN()
}

// Load загружает конфигурацию из переменных окружения.
//
// Использует LoadPrefixed(EnvPrefix): абсолютно-тегированные поля
// (`envconfig:"KACHO_COMPUTE_..."`) резолвятся как есть, а вложенные
// per-edge TLS value-структуры (grpcclient.TLSClient / grpcsrv.TLSServer) с
// относительным тегом (`IAM_REGISTER_MTLS`) получают независимые
// KACHO_COMPUTE_<EDGE>_<NAME> имена (per-edge prefixing).
func Load() (Config, error) {
	var c Config
	err := corecfg.LoadPrefixed(EnvPrefix, &c)
	return c, err
}

// TrustDomain — домен доверия, который РЕАЛЬНО уезжает в пару звеньев извлечения
// личности на обоих слушателях.
//
// Единственный источник этой величины на процесс: проводка, стража старта и
// самоотчёт о посадке читают ОДИН объект и спрашивают его ОДИН предикат. Значит
// «страж пропустил» ⟺ «домен реально объявлен» — по построению, а не потому, что
// три автора написали одинаковые тела.
//
// Приведение написанного оператором (пробелы, схема, косые черты) живёт в
// конструкторе типа и здесь не повторяется: два места об одном предмете
// расходятся молча. См. grpcsrv.NewTrustDomain.
func (c Config) TrustDomain() grpcsrv.TrustDomain {
	return grpcsrv.NewTrustDomain(c.AuthZTrustDomain)
}

// TrustedForwarders — круг отправителей, который РЕАЛЬНО уезжает в
// grpcsrv.WithTrustedForwarders на обоих листенерах.
//
// Единственный источник этого значения на процесс: его читает и проводка
// (cmd/compute/main.go), и стража старта (Validate), и самоотчёт о посадке
// (cmd/compute/bootposture.go). Все трое спрашивают ОДИН объект и ОДИН его
// предикат, поэтому «стража пропустила» ⟺ «круг реально сужен» — по построению,
// а не по совпадению трёх одинаково написанных тел. До ввода типа их было
// именно три, и каждое считало по-своему.
//
// Нормализация круга (пустые записи, пробелы по краям, повторы) живёт в
// конструкторе типа и здесь не пересказывается: два места об одном предмете
// разъезжаются молча. См. grpcsrv.NewTrustedForwarders.
func (c Config) TrustedForwarders() grpcsrv.TrustedForwarders {
	return grpcsrv.NewTrustedForwarders(c.AuthZTrustedForwarderSANs...)
}
