// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package config — конфигурация kacho-storage из переменных окружения через corelib
// config.LoadPrefixed("KACHO_STORAGE"). Per-edge TLS-структуры (grpcclient.TLSClient
// / grpcsrv.TLSServer) получают независимые имена KACHO_STORAGE_<EDGE>_<NAME> —
// префикс на каждое ребро, без общего TLS-синглтона на процесс.
package config

import (
	"time"

	"fmt"

	corecfg "github.com/PRO-Robotech/kacho/pkg/config"
	"github.com/PRO-Robotech/kacho/pkg/grpcclient"
	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
)

// envPrefix — корневой сегмент env-имён kacho-storage (KACHO_<DOMAIN>).
const envPrefix = "KACHO_STORAGE"

// DBSchema — Postgres-схема (database-per-service): своя БД kacho_storage.
const DBSchema = "kacho_storage"

// Config — конфигурация kacho-storage.
type Config struct {
	DBHost     string `envconfig:"KACHO_STORAGE_DB_HOST" default:"localhost"`
	DBPort     string `envconfig:"KACHO_STORAGE_DB_PORT" default:"5432"`
	DBUser     string `envconfig:"KACHO_STORAGE_DB_USER" default:"storage"`
	DBPassword string `envconfig:"KACHO_STORAGE_DB_PASSWORD" required:"true"`
	DBName     string `envconfig:"KACHO_STORAGE_DB_NAME" default:"kacho_storage"`
	// DBSSLMode — sslmode для DSN. dev по умолчанию disable; в проде обязателен
	// require|verify-ca|verify-full.
	DBSSLMode string `envconfig:"KACHO_STORAGE_DB_SSLMODE" default:"disable"`
	// DBMaxConns — лимит pgx-пула (0 = дефолт pgx max(4, NumCPU)).
	DBMaxConns int `envconfig:"KACHO_STORAGE_DB_MAX_CONNS" default:"0"`

	// GrpcPort — публичный листенер (Volume/Snapshot/DiskType Service).
	GrpcPort string `envconfig:"KACHO_STORAGE_GRPC_PORT" default:"9090"`
	// InternalGrpcPort — cluster-internal листенер (InternalVolume/InternalDiskType).
	// Не выставляется на внешнем endpoint api-gateway — только cluster-internal.
	InternalGrpcPort string `envconfig:"KACHO_STORAGE_INTERNAL_PORT" default:"9091"`
	// MetricsAddr — адрес cluster-internal diagnostic HTTP-listener'а (/healthz,
	// /metrics). Пустое значение явно отключает listener (back-compat).
	MetricsAddr string `envconfig:"KACHO_STORAGE_METRICS_ADDR" default:":9095"`

	// AuthMode — fail-closed режим: dev | production | production-strict.
	AuthMode string `envconfig:"KACHO_STORAGE_AUTH_MODE" default:"production"`

	// ===== peer-адреса (runtime cross-domain edges) =====

	// GeoGRPCAddr — public endpoint kacho-geo для валидации zone_id
	// (ребро storage→geo, ZoneService.Get). Пусто → GeoClient fail-closed.
	GeoGRPCAddr string `envconfig:"KACHO_STORAGE_GEO_GRPC_ADDR" default:""`
	// IAMGRPCAddr — endpoint kaname для валидации project_id
	// (ребро storage→iam, ProjectService.Get). Пусто → IAMClient fail-closed.
	IAMGRPCAddr string `envconfig:"KACHO_STORAGE_IAM_GRPC_ADDR" default:""`
	// AuthZIAMGRPCAddr — internal endpoint kaname для per-RPC Check
	// (ребро storage→iam authz, InternalIAMService.Check). Пусто → authz-интерсептор
	// не подключается (грациозный dev-старт без kaname). Тот же endpoint несёт
	// InternalIAMService.RegisterResource/UnregisterResource (FGA-proxy, Internal-only
	// :9091) — его переиспользует register-drainer + sync-registrar.
	AuthZIAMGRPCAddr string `envconfig:"KACHO_STORAGE_AUTHZ_IAM_GRPC_ADDR" default:""`

	// AuthZTrustedForwarderSANs — allow-list личностей сертификата (SPIFFE-SAN),
	// которым разрешено ПЕРЕДАВАТЬ личность конечного пользователя в метаданных
	// x-kacho-principal-*. Пробрасывается в оба листенера через
	// grpcsrv.WithTrustedForwarders (см. cmd/storage/serve.go).
	//
	// Почему это ручка, а не константа, и почему её отсутствие было дырой: contract
	// corelib (pkg/grpcsrv principalIsTrusted) сужает круг отправителей ТОЛЬКО когда
	// список непуст; на пустом он отвечает «доверяем» любому пиру, прошедшему
	// проверку сертификата. Внутренний периметр у нас объявлен НЕдоверенным, поэтому
	// пустой список означает: любой сосед со своим законным клиентским сертификатом
	// присылает заголовки личности жертвы, и решение о правах принимается от её
	// имени. Раньше поля не было вовсе, а serve.go передавал литеральный пустой
	// список — то есть сузить круг было нечем.
	//
	// Формат — список через запятую. Законных отправителей ДВА, и оба обязаны быть
	// в списке (иначе ломается рабочий путь):
	//   - api-gateway — передаёт личность пользователя на публичный :9090;
	//   - compute — передаёт её же на внутренний :9091 (привязка/отвязка тома идёт
	//     под личностью того, кто её инициировал, см. compute
	//     internal/clients/storage_client.go → pkg/auth.PropagateOutgoing).
	// Канонические значения — в values.prod.
	//
	// Пусто допустимо ТОЛЬКО в dev (in-process фикстуры); в любом боевом режиме
	// Validate() отказывает в старте (fail-closed, зеркалит geo/compute/nlb).
	AuthZTrustedForwarderSANs []string `envconfig:"KACHO_STORAGE_AUTHZ_TRUSTED_FORWARDER_SANS"`

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
	AuthZTrustAnyForwarder bool `envconfig:"KACHO_STORAGE_AUTHZ_TRUST_ANY_FORWARDER" default:"false"`

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
	// ENV `KACHO_STORAGE_AUTHZ_TRUST_DOMAIN`.
	AuthZTrustDomain string `envconfig:"KACHO_STORAGE_AUTHZ_TRUST_DOMAIN"`

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
	AuthZCacheTTL time.Duration `envconfig:"KACHO_STORAGE_AUTHZ_CACHE_TTL" default:"5s"`

	// AuthZCheckTimeout — срок ОДНОГО вопроса о правах владельцу модели.
	//
	// Ручка заведена не ради нового поведения: значение то же, что было
	// умолчанием платформы (прежняя фабрика звена срок не прокидывала вовсе, и
	// его выбирала библиотека). Изменилось то, что оно стало ВЫБРАННЫМ.
	// Неотвечающий сосед без срока вешает горутину навсегда, и звено не доходит
	// до своей ветки fail-closed — горутины копятся до исчерпания процесса.
	AuthZCheckTimeout time.Duration `envconfig:"KACHO_STORAGE_AUTHZ_CHECK_TIMEOUT" default:"2s"`

	// AuthZDenyBudgetPerSec — устойчивый темп (в секунду на принципала) проверок,
	// чей исход кэш НЕ поглощает: отказ, сокрытие существования, промах «нет
	// пути», недоступность модели. По исчерпании звено отвечает
	// `ResourceExhausted`, не обращаясь к kaname, — то есть сбрасывает шторм с
	// него.
	//
	// Величина 100 не выдумана: это то же число, которое платформа уже выбрала
	// для того же механизма (литерал композиционного корня nlb, умолчание ручки
	// vpc, умолчание ручки geo). Умолчания у самого механизма НЕТ (неположительное
	// значение он читает как «ограничения нет»), поэтому число обязано быть
	// названо здесь.
	//
	// Изъятия («ронять некого») у storage быть не может: решение о доступе он
	// принимает не у себя, а вопросом к kaname — сетевой сосед, которого шторм
	// отказов уронит, у него ЕСТЬ, и на том же соединении живут пообъектный фильтр
	// видимости и регистрация владельца.
	AuthZDenyBudgetPerSec float64 `envconfig:"KACHO_STORAGE_AUTHZ_DENY_BUDGET_PER_SEC" default:"100"`

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
	// Имена: KACHO_STORAGE_ADMISSION_{PUBLIC,INTERNAL}_{READ_PER_SEC,
	// MUTATION_PER_SEC,BURST_FACTOR,IN_FLIGHT}.
	AdmissionPublic   grpcsrv.AdmissionKnobs `envconfig:"ADMISSION_PUBLIC"`
	AdmissionInternal grpcsrv.AdmissionKnobs `envconfig:"ADMISSION_INTERNAL"`

	// HandlingBudget — верхняя граница обработки ОДНОГО вызова (серверный срок).
	// Более строгий срок вызывающего уважается; окно не расширяется никогда.
	//
	// 30s — то же число, что платформа выбрала для той же величины у vpc и geo.
	// Это ПОТОЛОК, а не цель: он обязан с запасом накрывать вопрос о правах
	// (KACHO_STORAGE_AUTHZ_CHECK_TIMEOUT, 2s), пообъектную фильтрацию страницы
	// (KACHO_STORAGE_LIST_FILTER_TIMEOUT_MS × волны) и запрос к своей БД. Предмет
	// его — не задержка, а вызов БЕЗ срока: он держит соединение из ограниченного
	// пула столько, сколько выполняется его запрос, и MaxConns таких вызовов
	// отказывают весь сервис (CWE-770).
	//
	// «Не применимо» у величины нет и быть не может — сказать «границы не надо»
	// значит сказать «мой процесс вправе держать чужой ресурс сколько угодно».
	// Неположительное значение отвергает конструктор дескриптора.
	HandlingBudget time.Duration `envconfig:"KACHO_STORAGE_HANDLING_BUDGET" default:"30s"`

	// SubscriptionStreamBudget — СРОК ЖИЗНИ одного потока подписки
	// (`pkg/subscription`, общий сервер потока изменений).
	//
	// По истечении поток закрывается ЧИСТО, и клиент возобновляется со своей
	// позиции: обрыв — штатное событие, а не отказ. Величина обязана заметно
	// превосходить границу обработки одиночного вызова выше, иначе поток
	// закрывался бы раньше, чем доезжает первое событие догона, и подписчик читал
	// бы штатное закрытие как «изменений нет». Носитель судит это отношение сам и
	// роняет старт на негодной величине.
	SubscriptionStreamBudget time.Duration `envconfig:"KACHO_STORAGE_SUBSCRIPTION_STREAM_BUDGET" default:"1h"`

	// SubscriptionMaxStreams — потолок ОДНОВРЕМЕННЫХ потоков этого процесса.
	//
	// Это АРИФМЕТИКА СОЕДИНЕНИЙ, а не вкус: каждый поток держит выделенное
	// соединение ВНЕ ПУЛА всё время своей жизни, поэтому
	//
	//	число реплик × этот потолок + пулы всех реплик ≤ max_connections базы
	//
	// Величина выбрана от ПРЕДЕЛА БАЗЫ, а не от ожидаемого спроса, и запас обязан
	// остаться пулу, миграциям и опросу готовности.
	//
	// Асимметрия, которая и решает выбор: упереться в СВОЙ потолок — чистый отказ
	// ОДНОМУ вызывающему (`RESOURCE_EXHAUSTED`, повтор осмыслен), упереться в
	// предел БАЗЫ — отказ всему процессу и всем арендаторам сразу, включая тех,
	// кто подписки не открывал. Поэтому свой потолок держится заведомо ниже.
	//
	// Превышение отвечает ОТКАЗОМ, а не молчаливой очередью: очередь превратила бы
	// исчерпание в неограниченное ожидание, неотличимое для клиента от «событий
	// нет».
	//
	// Поднимая величину, подними и предел базы — вместе, а не по одному: сегодня
	// их произведение не сверяет никто (гейт посадки считает только пул).
	SubscriptionMaxStreams int `envconfig:"KACHO_STORAGE_SUBSCRIPTION_MAX_STREAMS" default:"16"`

	// SubscriptionIdlePoll — холостой перепрос журнала.
	//
	// Он не «на всякий случай»: ОТКАТИВШИЙСЯ писатель уведомления не шлёт, и
	// подтверждение границы устоявшегося приезжает именно этим перепросом. У
	// storage писатель журнала — триггер, то есть уведомление уходит в той же
	// транзакции, что и мутация, и откат снимает его вместе с ней.
	SubscriptionIdlePoll time.Duration `envconfig:"KACHO_STORAGE_SUBSCRIPTION_IDLE_POLL" default:"2s"`

	// FGARegisterDrainerEnabled — включает register-drainer owner-tuple'ов (SEC-D):
	// применяет fga_register_outbox-intents через kaname RegisterResource/
	// UnregisterResource по ребру storage→iam (AuthZIAMGRPCAddr, mTLS). Default true;
	// без него созданные Volume/Snapshot не получают owner-tuple → анти-BOLA
	// scope_extractor не резолвит target→project. false → intents копятся
	// неприменёнными (dev/degraded). Требует непустой AuthZIAMGRPCAddr.
	FGARegisterDrainerEnabled bool `envconfig:"KACHO_STORAGE_FGA_REGISTER_DRAINER_ENABLED" default:"true"`

	// ===== per-object filtered List =====
	//
	// Все ListFilter* — production-edition: configurable, без хардкода. Адрес
	// authorize-эндпоинта переиспользует AuthZIAMGRPCAddr (kaname internal :9091,
	// AuthorizeService.BatchCheck) и те же per-edge creds IAMClientMTLS.
	//
	// NB: knob'а «размер allow-list» здесь НЕТ и быть не может: видимость
	// спрашивается per-object по УЖЕ прочитанной странице, поэтому усечь её нечем —
	// полнота и стоимость задаются page_size, а не ручкой (перечисление всех
	// разрешённых объектов даёт усечение сверх жёсткого предела ListObjects).

	// ListFilterEnabled — master-switch. true → List спрашивает iam.BatchCheck про
	// id прочитанной страницы. false → фильтра нет (passthrough); production
	// boot-guard такую посадку ЗАПРЕЩАЕТ (см. Validate).
	ListFilterEnabled bool `envconfig:"KACHO_STORAGE_LIST_FILTER_ENABLED" default:"true"`

	// ListFilterTimeoutMs — per-call deadline ОДНОГО iam.BatchCheck (страница >100 id
	// режется на батчи ≤100, у каждого свой deadline), НЕ бюджет всей фильтрации:
	// бюджет операции выводится в authzfilter.NewFGAFilter из контракта (максимум
	// страницы ÷ предел батча ÷ параллелизм). 1000ms — реалистичный допуск на ОДИН
	// хоп: батчи идут ограниченным fan-out'ом, поэтому worst-case глубина на
	// предельной странице (page_size=1000) — 4 волны, а не 20 последовательных хопов.
	ListFilterTimeoutMs int `envconfig:"KACHO_STORAGE_LIST_FILTER_TIMEOUT_MS" default:"1000"`

	// ListFilterCacheTTLMs — TTL ПОЛОЖИТЕЛЬНОГО per-object вердикта. Короткий (5s),
	// чтобы revoke access-binding был виден ≤5s. Отрицательные вердикты не кешируются
	// вовсе (иначе свежий грант / только что созданный свой ресурс были бы невидимы
	// весь TTL).
	ListFilterCacheTTLMs int `envconfig:"KACHO_STORAGE_LIST_FILTER_CACHE_TTL_MS" default:"5000"`

	// ListFilterCacheMaxEntries — bound кеша, вытеснение LRU. Кеш НИКАК не
	// ограничивает видимость: страница проверяется целиком независимо от его размера.
	ListFilterCacheMaxEntries int `envconfig:"KACHO_STORAGE_LIST_FILTER_CACHE_MAX_ENTRIES" default:"10000"`

	// ListFilterFailOpen — degraded mode. true → на ошибке iam страница отдаётся
	// НЕотфильтрованной (+ audit-WARN); false → Unavailable. Default false
	// (fail-closed = secure); true — только break-glass.
	ListFilterFailOpen bool `envconfig:"KACHO_STORAGE_LIST_FILTER_FAIL_OPEN" default:"false"`

	// ListFilterBreakglass — аварийный режим: когда модели прав на этой посадке нет
	// вовсе (фильтр выключен либо эндпоинт не задан), списки и гейт объекта отдают
	// проход вместо отказа.
	//
	// Он остаётся явным исключением, а не умолчанием: прежде «фильтр выключен» само
	// по себе означало сквозной проход, и вся защита держалась на загрузочном страже
	// — то есть существовала ровно до первой конфигурации, которая его не взвела.
	// Теперь пропуск требуется ОБЪЯВИТЬ, и каждое срабатывание считается и
	// называется (`listnarrow.Counts`).
	ListFilterBreakglass bool `envconfig:"KACHO_STORAGE_LIST_FILTER_BREAKGLASS" default:"false"`

	// ===== плоскость данных блочного хранения =====
	//
	// Что живёт ЗДЕСЬ, а что в БД, и почему граница проходит именно так. Какие
	// бэкенды существуют, где у них пулы и какие классы на них отображены — это
	// ДАННЫЕ: они меняются администратором в рантайме и хранятся ресурсами
	// StorageBackend и DiskTypeBinding. В конфигурации процесса остаётся то, что
	// принадлежит РАЗВЁРТЫВАНИЮ и не может приехать из БД: чем этот экземпляр
	// облака отличается от соседнего и откуда он берёт учётный материал по
	// ссылке, записанной в ресурсе.

	// BlockBackendKind — вид плоскости данных, которую обслуживает ЭТОТ процесс.
	// Пусто — плоскости данных нет: ресурсы остаются control-plane-записями, и
	// сверщик не запускается.
	//
	// Значение по умолчанию пусто НАМЕРЕННО и не выводится ни из какого другого
	// адреса. Умолчание, собранное из чужой координаты, всегда непусто, поэтому
	// контроль выглядит включённым и ведёт в никуда, а ни один профиль
	// развёртывания не обязан ничего задавать, чтобы это заметить.
	BlockBackendKind string `envconfig:"KACHO_STORAGE_BLOCK_BACKEND_KIND" default:""`

	// BlockBackendInstallPrefix — префикс имени объектов ЭТОГО развёртывания у
	// бэкенда.
	//
	// Имя объекта выводится из неизменяемого идентификатора ресурса, поэтому два
	// облака, нацеленные на один кластер хранилища, выведут одинаковые имена и
	// «усыновят» объекты друг друга: сверщик каждого посчитает чужие объекты
	// своей утечкой, а удаление в одном снесёт данные в другом. Префикс —
	// единственное, что отличает наши объекты от чужих в общем пространстве.
	BlockBackendInstallPrefix string `envconfig:"KACHO_STORAGE_BLOCK_BACKEND_INSTALL_PREFIX" default:""`

	// BlockBackendCredentialsDir — каталог, в котором разрешаются ССЫЛКИ на
	// учётный материал из StorageBackend.credentials_ref.
	//
	// Сам секрет через API не проходит и в БД не ложится: строка таблицы
	// переживает ротацию и уезжает в резервные копии. Ресурс несёт ссылку,
	// процесс — способ её разрешить.
	BlockBackendCredentialsDir string `envconfig:"KACHO_STORAGE_BLOCK_BACKEND_CREDENTIALS_DIR" default:""`

	// BlockBackendCallTimeout — срок ОДНОГО обращения к бэкенду.
	//
	// Он обязан помещаться внутрь бюджета операции вместе со всеми повторами:
	// исполнитель операций убивает функцию по своему потолку, а разрешитель
	// осиротевших операций затем признаёт строку завершённой, читая нашу БД. Не
	// уместившись, длинное обращение превращается в ЛОЖНОЕ «готово» при
	// отсутствующем объекте. Соотношение проверяется стражем старта.
	BlockBackendCallTimeout time.Duration `envconfig:"KACHO_STORAGE_BLOCK_BACKEND_CALL_TIMEOUT" default:"30s"`

	// BlockBackendReconcileInterval — период обхода расхождений между желаемым и
	// наблюдённым.
	BlockBackendReconcileInterval time.Duration `envconfig:"KACHO_STORAGE_BLOCK_BACKEND_RECONCILE_INTERVAL" default:"15s"`

	// BlockBackendReconcileBatch — сколько ресурсов сверщик берёт за проход.
	BlockBackendReconcileBatch int `envconfig:"KACHO_STORAGE_BLOCK_BACKEND_RECONCILE_BATCH" default:"100"`

	// ProjectProvisionedBytesLimit — верхний предел ПРОВИЗИОНИРОВАННОГО объёма на
	// один проект. Ноль — предела нет.
	//
	// Величина наша, а не бэкенда: хранилище про наши проекты не знает и знать не
	// должно, поэтому «один арендатор выел ресурс, за который отвечает другая
	// команда» может остановить только облако. Предел проверяется ВНУТРИ вставки
	// тома, тем же стейтментом, — иначе две одновременные заявки прошли бы обе.
	//
	// Это заведомо грубый инструмент: предел один на все проекты. Он и не
	// претендует на большее — пределы как РЕСУРС принадлежат платформенному домену,
	// и заводить их здесь значило бы, что их заведёт у себя каждый сервис.
	ProjectProvisionedBytesLimit int64 `envconfig:"KACHO_STORAGE_PROJECT_PROVISIONED_BYTES_LIMIT" default:"0"`

	// ===== per-edge mTLS =====

	// GeoClientMTLS — client-creds ребра storage→geo (:9090).
	GeoClientMTLS grpcclient.TLSClient `envconfig:"GEO_CLIENT_MTLS"`
	// IAMClientMTLS — client-creds ребра storage→iam (:9090 / :9091 authz).
	IAMClientMTLS grpcclient.TLSClient `envconfig:"IAM_CLIENT_MTLS"`
	// PublicServerMTLS — server-creds публичного листенера (:9090).
	PublicServerMTLS grpcsrv.TLSServer `envconfig:"PUBLIC_SERVER_MTLS"`
	// InternalServerMTLS — server-creds cluster-internal листенера (:9091).
	InternalServerMTLS grpcsrv.TLSServer `envconfig:"INTERNAL_SERVER_MTLS"`
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
// (cmd/storage/serve.go), и стража старта (Validate), и самоотчёт о посадке
// (cmd/storage/bootposture.go). Все трое спрашивают ОДИН объект и ОДИН его
// предикат, поэтому «стража пропустила» ⟺ «круг реально сужен» — по построению,
// а не по совпадению трёх одинаково написанных тел.
//
// Нормализация круга (пустые записи, пробелы по краям, повторы) живёт в
// конструкторе типа и здесь не пересказывается: два места об одном предмете
// разъезжаются молча. См. grpcsrv.NewTrustedForwarders.
func (c Config) TrustedForwarders() grpcsrv.TrustedForwarders {
	return grpcsrv.NewTrustedForwarders(c.AuthZTrustedForwarderSANs...)
}

// Обёрток «creds как опция сервера» здесь БОЛЬШЕ НЕТ: композиционный корень несёт
// в дескриптор сами КРЕДЕНШЕЛЫ, а не опцию. Опция непрозрачна, а
// `TransportCredentials.Info()` — нет, и отказ старта читает именно её: на
// невзведённой ручке сборщик отдаёт незашифрованные креды БЕЗ ошибки, поэтому
// вопрос «что транспорт о себе говорит» — единственный, который стоит задавать.

// schemaOptionsParam — URL-encoded libpq options=-c search_path=kacho_storage,public.
// Каждое соединение (pgxpool + goose database/sql) видит схему без отдельного SET.
const schemaOptionsParam = "options=-c%20search_path%3Dkacho_storage%2Cpublic"

// baseDSN — стандартный postgres DSN (для pgxpool и database/sql), несёт search_path
// kacho_storage через libpq options.
func (c Config) baseDSN() string {
	mode := c.DBSSLMode
	if mode == "" {
		mode = "disable"
	}
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=%s&%s",
		c.DBUser, c.DBPassword, c.DBHost, c.DBPort, c.DBName, mode, schemaOptionsParam,
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

// DSN — строка подключения для pgxpool (поддерживает pool_max_conns). НЕ для
// database/sql (pool_max_conns → неизвестный PG-параметр → FATAL).
func (c Config) DSN() string {
	dsn := c.baseDSN()
	if c.DBMaxConns > 0 {
		dsn += fmt.Sprintf("&pool_max_conns=%d", c.DBMaxConns)
	}
	return dsn
}

// MigrateDSN — строка подключения для goose/database/sql (без pgxpool-параметров).
func (c Config) MigrateDSN() string {
	return c.baseDSN()
}

// Load загружает конфигурацию из переменных окружения.
func Load() (Config, error) {
	var c Config
	err := corecfg.LoadPrefixed(envPrefix, &c)
	return c, err
}
