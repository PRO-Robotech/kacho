// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// validate.go — Mode enum + Config.Validate.
//
//   - `Mode` enum заменяет `bool productionMode`  — `cfg.Mode`
//     (общий режим работы), а не `cfg.AuthMode`.
//   - Validate-логика — в config-пакете, не в main.
package config

import (
	"fmt"
	"net/url"
	"strings"

	"go.uber.org/multierr"

	"github.com/PRO-Robotech/kacho/pkg/authz"
	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
	"github.com/PRO-Robotech/kacho/pkg/servicecontract"
)

// ModeEnum — посадка процесса. ТИП ОБЩИЙ (`servicecontract.Mode`), и это не
// сокращение записи: пока у сервиса был свой enum, у него был и свой СЛОВАРЬ
// значений — а он разошёлся с остальными В ОБЕ СТОРОНЫ.
//
// Что было (замер задачи продукта #1656): принимались `development`, `prod` и
// ПУСТАЯ строка — алиасы, которых не принимал никто из шести соседей; и
// отвергался `production-strict`, в котором работали все шестеро. Оператор видел
// это как две противоположные поломки: выравнивание флота на строгой посадке
// роняло nlb отказом старта, а короткий алиас ронял шестерых и поднимал одного.
// Единой команды «ужесточить флот» не существовало.
//
// Псевдоним типа, а не свой enum: у одного предмета один дом. Имена ниже
// остаются, потому что читаются из этого пакета сотнями строк — но ЗНАЧЕНИЯ они
// берут у дома, а не объявляют заново.
type ModeEnum = servicecontract.Mode

const (
	// ModeDev — relaxed validation, TLS опционален. СТРОГО локальные фикстуры.
	ModeDev = servicecontract.ModeDev
	// ModeProduction — TLS обязателен для public listener / peer-вызовов,
	// sslmode строки подключения к Postgres — из безопасного перечня,
	// пообъектный фильтр списков включён и fail-closed.
	//
	// Адрес внешнего движка отношений здесь НЕ перечислен, и это тоже не
	// пропуск: прежняя редакция объявляла его обязательным («FGA endpoint
	// обязателен»), а поля под него в описателе настроек не было ни одного —
	// движок снят целиком (#747). Страж, на который ссылались обе редакции
	// комментария, проверять мог только то, что до него доезжает (kacho#1796).
	//
	// Аварийного обхода проверки прав здесь НЕ перечислено, и это не пропуск:
	// такой ручки у сервиса нет вовсе — звено решения о доступе ставится
	// безусловно. Прежняя редакция обоих комментариев называла её («в dev
	// допускается, в production запрещена»), и по этому описанию три профиля
	// развёртывания объявляли ключ выключенным. Запрет, которому нечего
	// запрещать, читается как действующий контроль.
	ModeProduction = servicecontract.ModeProduction
	// ModeProductionStrict — боевая посадка, в которой у КАЖДОГО ребра к соседу
	// требуется взаимная проверка сертификата, а не только шифрование канала.
	//
	// Ось названа своя, а не принята «за компанию»: в обычной боевой посадке
	// ребро считается защищённым и при одностороннем TLS — канал зашифрован, но
	// СОБЕСЕДНИК себя не доказывает, поэтому дозвонившийся в обход края
	// принимается как законный сосед. Строгая посадка эту разницу закрывает.
	// Значение, принятое и ничего не меняющее, было бы «принято-и-проигнорировано»
	// (`api-conventions.md`) — обещанием возможности, которой нет.
	ModeProductionStrict = servicecontract.ModeProductionStrict
)

// ParseMode разбирает строку из YAML / ENV. Разбор НЕ СВОЙ: словарь допустимых
// написаний объявлен в дереве один раз (`servicecontract.Modes`), и отказ
// перечисляет тот же набор, что у остальных шести стражей старта.
//
// Пустая строка режимом больше не является. Прежде она молча означала
// `production` — «fail-closed», и это выглядело осторожным; на деле посадка,
// которую никто не выбрал, есть решение о доступе, принятое никем, и отличить
// «оператор выбрал боевой режим» от «ключ потерялся при сборке профиля» было
// нечем. Умолчание при этом никуда не делось: его ставит `RegisterDefaults`
// (`mode: production`) — то есть выбор остаётся ЯВНЫМ и наблюдаемым в профиле.
func ParseMode(s string) (ModeEnum, error) { return servicecontract.ParseMode(s) }

// validLogLevels — допустимые значения logger.level.
var validLogLevels = map[string]struct{}{
	"FATAL": {}, "ERROR": {}, "WARN": {}, "INFO": {}, "DEBUG": {},
}

// Validate — проверяет required-поля и согласованность mode-specific
// требований через multierr.Combine. Применяется один раз сразу после
// `viper.Unmarshal` в `Load`.
func (c Config) Validate() error {
	var errs error

	// Объявление домена величин судится на ЛЮБОМ старте: незаданное объявление
	// означает, что оператор не выбрал между «потолки действуют» и «потолков
	// нет», и подставить за него разумное умолчание нельзя ни в каком режиме.
	errs = multierr.Append(errs, c.ValidateQuotaAuthority())

	// Круг отправителей чужой личности обязан быть сужен на ЛЮБОМ старте — не
	// только в боевом режиме, и не только когда включён server-mTLS.
	//
	// Освобождения по аварийному режиму здесь БОЛЬШЕ НЕТ, и это не ужесточение
	// «за компанию»: аварийного режима решения о доступе у процесса не осталось —
	// носитель контура (`pkg/servicehost`) ставит звено решения ВСЕГДА, и поля,
	// которым его можно отменить, в дескрипторе не существует. Оставленное
	// освобождение снимало бы стражу круга по ручке, которая больше ничего не
	// снимает, — то есть по причине, которой нет.
	//
	// Прежде страж стоял внутри боевой ветки И был обусловлен ЧУЖИМ полем
	// (mtls.server.enable). Обусловленность читалась как «без mTLS сужать нечего»,
	// но круг и mTLS — разные измерения: сертификат доказывает, ЧЕЙ это пир, и
	// ничего не говорит о праве представляться другим. Страж, молчащий вне боевого
	// режима, к тому же ни разу не исполняется на локальном стенде, поэтому «забыл
	// выставить круг» находится только на боевом профиле, где цена ошибки
	// максимальна.
	//
	// Стража общая на все семь сервисов (grpcsrv.TrustedForwarders.Require):
	// одинаковый исход и одинаковый текст отказа, различаются только имена ручек.
	errs = multierr.Append(errs, c.TrustedForwarders().Require(grpcsrv.ForwarderGate{
		Production:   c.Mode().IsProduction(),
		DevTrustAny:  c.Authz.TrustAnyForwarder,
		SANsKnob:     "authz.trusted-forwarder-sans (env KACHO_NLB_AUTHZ__TRUSTED_FORWARDER_SANS)",
		TrustAnyKnob: "authz.trust-any-forwarder (env KACHO_NLB_AUTHZ__TRUST_ANY_FORWARDER)",
	}))

	// Mode
	mode, err := ParseMode(c.ModeRaw)
	if err != nil {
		errs = multierr.Append(errs, fmt.Errorf("mode: %w", err))
	}

	// Logger
	if _, ok := validLogLevels[strings.ToUpper(strings.TrimSpace(c.Logger.Level))]; !ok {
		errs = multierr.Append(errs, fmt.Errorf("logger.level %q: want one of FATAL|ERROR|WARN|INFO|DEBUG", c.Logger.Level))
	}

	// API-server endpoints — must be `tcp://host:port` parseable.
	if err := validateEndpoint("api-server.endpoint", c.APIServer.Endpoint); err != nil {
		errs = multierr.Append(errs, err)
	}
	if err := validateEndpoint("api-server.internal-endpoint", c.APIServer.InternalEndpoint); err != nil {
		errs = multierr.Append(errs, err)
	}
	if c.APIServer.GracefulShutdown <= 0 {
		errs = multierr.Append(errs, fmt.Errorf("api-server.graceful-shutdown must be > 0, got %v", c.APIServer.GracefulShutdown))
	}
	// Верхняя граница обработки вызова. «Не применимо» у неё нет: вызов без срока
	// держит соединение из ограниченного пула столько, сколько выполняется его
	// запрос, и pool_max_conns таких вызовов отказывают весь сервис. Дескриптор
	// носителя отвергнет неположительное значение и сам; страж стоит здесь, чтобы
	// отказ назвал ИМЯ РУЧКИ, которую крутить оператору.
	if c.APIServer.HandlingBudget <= 0 {
		errs = multierr.Append(errs, fmt.Errorf(
			"api-server.handling-budget must be > 0, got %v (a call with no deadline holds a pooled "+
				"connection for as long as its query runs)", c.APIServer.HandlingBudget))
	}

	// Repository
	switch strings.ToUpper(strings.TrimSpace(c.Repository.Type)) {
	case "POSTGRES":
		// ok
	case "":
		errs = multierr.Append(errs, fmt.Errorf("repository.type: empty (want POSTGRES)"))
	default:
		errs = multierr.Append(errs, fmt.Errorf("repository.type %q: only POSTGRES supported", c.Repository.Type))
	}
	if strings.TrimSpace(c.Repository.Postgres.URL) == "" {
		errs = multierr.Append(errs, fmt.Errorf("repository.postgres.url: required"))
	}
	if c.Repository.Postgres.MaxConns < 0 {
		errs = multierr.Append(errs, fmt.Errorf("repository.postgres.max-conns must be >= 0, got %d", c.Repository.Postgres.MaxConns))
	}

	// Authn (TLS)
	switch strings.ToLower(strings.TrimSpace(c.Authn.Type)) {
	case "none", "":
		// ok
	case "tls":
		if c.Authn.TLS.KeyFile == "" || c.Authn.TLS.CertFile == "" {
			errs = multierr.Append(errs, fmt.Errorf("authn.tls: key-file and cert-file required when type=tls"))
		}
	default:
		errs = multierr.Append(errs, fmt.Errorf("authn.type %q: want none|tls", c.Authn.Type))
	}

	// Authz (FGA Check). Бюджет отказов — строго положительный НА ЛЮБОЙ посадке:
	// механизм читает неположительное значение как «ограничения нет», поэтому ноль
	// выключил бы отсечку МОЛЧА, ветка ResourceExhausted стала бы недостижимой, а
	// её счётчик — навсегда нулевым. Ровно так эта величина однажды и пропала.
	if c.Authz.DenyBudgetPerSec <= 0 {
		errs = multierr.Append(errs, fmt.Errorf(
			"authz.deny-budget-per-sec must be > 0, got %v (a non-positive value reads as "+
				"'no limit' to the decision link: the cut-off would switch itself off silently)",
			c.Authz.DenyBudgetPerSec))
	}

	// Первая ступень цепочки отзыва гранта — доставка намерения регистрации до
	// владельца прав. Дренаж будится уведомлением; когда уведомление потеряно,
	// ступенью становится ОТКАТ, и отозванная выдача действует ровно столько.
	//
	// Судится на ЛЮБОЙ посадке и одним потолком со всеми: потолок принадлежит
	// ЦЕПОЧКЕ, а не этой ручке, и объявлен там же, где два остальных
	// (`pkg/authz.RevocationPolicy`). Объявленный здесь, он стал бы вторым местом
	// об одном предмете — сумма ступеней разошлась бы со своими слагаемыми молча.
	//
	// Ноль — законный вход: библиотека дренажа подставит своё умолчание, и оно
	// равно потолку ступени. Отвергать его значило бы отвергать посадку, ручку
	// которой никто не трогал.
	if pf := c.FGA.RegisterDrainer.PollFallback; pf > authz.RevocationPolicy.DeliveryCeiling {
		errs = multierr.Append(errs, fmt.Errorf(
			"fga.register-drainer.poll-fallback %v exceeds the declared ceiling %v: this is the "+
				"FIRST step of the grant-revocation chain — with the notification lost, a withdrawn "+
				"binding keeps being honoured for exactly this long. The ceiling is declared once in "+
				"pkg/authz.RevocationPolicy, whose ChainCeiling is %v",
			pf, authz.RevocationPolicy.DeliveryCeiling, authz.RevocationPolicy.ChainCeiling()))
	}

	// Production transport fail-closed (security.md «AuthN+AuthZ ВЕЗДЕ»): plaintext
	// listener и insecure peer-вызовы в проде запрещены — boot отвергает insecure
	// prod-конфиг (не silent insecure-fallback).
	if mode.IsProduction() {
		// Server listener transport-security: ТОЛЬКО реальный server-cred
		// grpcsrv.TLSServerCreds(cfg.MTLS.Server) (composition root,
		// cmd/kacho-loadbalancer/main.go). authn.type=tls — МЁРТВОЕ значение: cfg.Authn
		// НЕ проброшен ни в один транспорт (grep: читается только здесь, в validate.go),
		// поэтому он НЕ настраивает TLS на listener'ах. Прежний gate принимал его как
		// «one-way TLS+JWT» и пропускал prod-конфиг с mtls.server.enable=false → boot
		// поднимал plaintext gRPC на public И internal :9091, доверяя client-asserted
		// principal без mTLS (CWE-319/CWE-290). Fail-closed, паритет с
		// kacho-vpc/kacho-compute (server-mTLS — обязателен, gate только на реальном
		// mTLS-настройке):
		//   1) dead authn.type=tls отвергается явно (оператор не примет его за transport-
		//      security и не оставит listener plaintext);
		//   2) mtls.server.enable=true обязателен (единственный источник server-creds).
		if strings.EqualFold(strings.TrimSpace(c.Authn.Type), "tls") {
			errs = multierr.Append(errs, fmt.Errorf(
				"production mode: authn.type=tls is a dead/unwired transport setting — it configures no server TLS "+
					"(listeners would run plaintext); remove it and set mtls.server.enable=true for real server transport security"))
		}
		if !c.MTLS.Server.Enable {
			errs = multierr.Append(errs, fmt.Errorf(
				"production mode: insecure server transport — set mtls.server.enable=true (plaintext listener forbidden)"))
		}
		// nlb→iam authz edge (per-RPC InternalIAMService.Check, internal :9091) обязан
		// (а) быть АДРЕСОВАН и (б) быть mTLS.
		//
		// (а) Адрес этого ребра берётся ТОЛЬКО из `extapi.iam.internal-addr`
		// (peerDialSpecs, соединение `iam-internal` — из него строятся
		// peers.Check и list-filter). Пока ключ пуст, ребро уезжает на fallback —
		// ПУБЛИЧНЫЙ листенер iam, где InternalIAMService не обслуживается: каждый
		// per-RPC Check упирается в Unimplemented, то есть авторизация в production
		// не работает, а сервис при этом стартует. Прежняя стража требовала здесь
		// `authz.iam.addr` — ключ, который НИ ОДИН путь кода не читает (из него не
		// строится ни одно соединение), поэтому она не могла поймать ни один
		// неверный конфиг и лишь заставляла оператора держать в values строку без
		// последствий. Ключ снят с контракта, стража требует настоящий.
		if strings.TrimSpace(c.ExtAPI.IAM.InternalAddr) == "" {
			errs = multierr.Append(errs, fmt.Errorf(
				"production mode: nlb→iam authz edge is not addressed — set extapi.iam.internal-addr "+
					"(InternalIAMService.Check lives on the iam INTERNAL listener; falling back to the public "+
					"address answers Unimplemented, so per-RPC authorization never runs)"))
		}
		// (б) Без mTLS Check идёт по plaintext и подделанная identity не отсекается.
		if !c.MTLS.IAMRegister.Enable {
			errs = multierr.Append(errs, fmt.Errorf(
				"production mode: nlb→iam authz edge must be mTLS — set mtls.iam-register.enable=true (insecure Check edge forbidden)"))
		}
		// Остальные cross-service рёбра (vpc / compute / geo / iam-project) обязаны
		// (а) БЫТЬ СКОНФИГУРИРОВАНЫ и (б) иметь transport-security: mTLS
		// (mtls.<edge>.enable) ЛИБО one-way TLS (<edge>.tls).
		//
		// (а) Незаданный addr — НЕ «peer выключён», а неверная конфигурация:
		// composition root оставляет клиент этого ребра nil, и use-case теряет
		// способность выполнить свои peer-проверки на request-path
		// (placement/region-когерентность подсети и связанного Address — vpc;
		// членство зон drain-набора — geo; existence проекта — iam; резолв
		// instance-таргетов — compute). Мутации обязаны быть fail-closed
		// (security.md), поэтому в рантайме такой вызов теперь отдаёт `Unavailable`;
		// guard превращает ту же ошибку в громкий refuse-to-start вместо 503 на
		// первом же тенантском Create. Прежде проверялась только
		// «secure-если-задано», из-за чего prod-конфиг без vpc/geo молча стартовал
		// с невыполнимыми coherence-проверками.
		//
		// (б) Без transport-security dialOne падает в insecure gRPC (buildCreds →
		// insecure.NewCredentials), и on-path attacker читает/подменяет
		// IPAM-аллокацию (VIP), instance-resolve, region-валидацию — integrity/
		// defense-in-depth (CWE-319).
		for _, e := range c.peerEdges() {
			if e.addr == "" {
				errs = multierr.Append(errs, fmt.Errorf(
					"production mode: %s peer edge is not configured — set extapi.%s.addr "+
						"(an unconfigured edge leaves its client nil, so the request-path checks it "+
						"guards cannot run and mutations fail closed at runtime)",
					e.name, e.tlsKey))
				continue
			}
			if !e.secure {
				errs = multierr.Append(errs, fmt.Errorf(
					"production mode: insecure peer transport on %s edge — set mtls.%s.enable=true or %s.tls=true (plaintext peer dial forbidden)",
					e.name, e.mtlsKey, e.tlsKey))
				continue
			}
			// Строгая посадка требует ВЗАИМНОЙ проверки сертификата, а не только
			// шифрования канала: односторонний TLS доказывает, кто СЕРВЕР, и
			// молчит о том, кто клиент, поэтому дозвонившийся в обход края
			// принимается ребром как законный сосед.
			if mode == ModeProductionStrict && !e.mtls {
				errs = multierr.Append(errs, fmt.Errorf(
					"production-strict mode: %s peer edge runs one-way TLS — set mtls.%s.enable=true "+
						"(one-way TLS encrypts the channel but does not prove the CLIENT, so a caller "+
						"reaching the peer around the edge is accepted as a legitimate neighbour)",
					e.name, e.mtlsKey))
			}
		}
		// Per-object List authorization fail-closed (security.md, defense-in-depth
		// parity с breakglass-gate). list-filter — единственный authz-слой для
		// ScopeFiltered List RPC (interceptor их пропускает); отключение или
		// fail-open превращает List в нефильтрованный passthrough → cross-tenant
		// enumeration. В проде: enabled обязателен, fail-open запрещён.
		if !c.Authz.ListFilter.Enabled {
			errs = multierr.Append(errs, fmt.Errorf(
				"production mode: authz.list-filter.enabled must be true (per-object List authorization required; disabling it enables cross-tenant enumeration)"))
		}
		if c.Authz.ListFilter.FailOpen {
			errs = multierr.Append(errs, fmt.Errorf(
				"production mode: authz.list-filter.fail-open forbidden (fail-closed only; fail-open returns unfiltered results during IAM outage)"))
		}
		// Postgres transport fail-closed (security.md «mTLS/TLS ВЕЗДЕ», CWE-319).
		// Peer-рёбра проверяются выше; DB-соединение — тот же
		// периметр: `sslmode=disable`/`allow`/`prefer` (или отсутствие sslmode,
		// libpq-default 'prefer') допускает plaintext-канал, по которому
		// DB-пароль (KACHO_NLB_DB_PASSWORD) и tenant-данные (VIP/listener/target)
		// идут в открытую. В проде допустимы только `require`/`verify-ca`/
		// `verify-full`. Проверяем лишь при непустом URL (пустой ловится выше).
		// Разбор строки подключения и перечень безопасных значений — НЕ свои:
		// оба приходят из дома семантики DSN (`pkg/db`), где объявлены один раз
		// на всё дерево (задача продукта #1464). Своя копия здесь была, и она
		// расходилась с домом на URL, который `url.Parse` не осиливает (пробел
		// или управляющий символ в пароле — законное содержимое секрета): её
		// запасной разбор бил строку по пробелам, а URL — один токен, поэтому
		// реальный `require` читался как «не задан» и исправная посадка
		// отвергалась.
		//
		// Судится строка, которая уходит В ПУЛ (`DSN()`), а не сырое поле URL:
		// у nlb они совпадают по `sslmode` (DSN дописывает только `pool_*`), но
		// спрашивать надо исход, иначе первое же изменение сборки строки
		// разведёт стража с пулом молча.
		//
		// Сам предикат и текст отказа живут в ОДНОМ месте на обе двери
		// конфигурации (`migrate_dsn.go`): точка наката судит ту же ось на своей
		// строке подключения, и две редакции одного текста разошлись бы молча.
		errs = multierr.Append(errs, postgresTransportRefusal(c.DSN()))
	}

	// Jobs.target-drain (фаза B drain runner). Interval должен быть > 0;
	// `0s` означало бы tight-loop, что нагрузит БД.
	if c.Jobs.TargetDrain.Interval <= 0 {
		errs = multierr.Append(errs, fmt.Errorf("jobs.target-drain.interval must be > 0, got %v", c.Jobs.TargetDrain.Interval))
	}

	// Jobs.free-ip (реконсиляция застрявших балансировщиков). Interval > 0 (иначе
	// tight-loop); age-threshold > 0 (иначе reconciler схватит свежий in-flight
	// create/delete и удалит легитимную in-progress строку — гонка).
	if c.Jobs.FreeIP.Interval <= 0 {
		errs = multierr.Append(errs, fmt.Errorf("jobs.free-ip.interval must be > 0, got %v", c.Jobs.FreeIP.Interval))
	}
	if c.Jobs.FreeIP.AgeThreshold <= 0 {
		errs = multierr.Append(errs, fmt.Errorf("jobs.free-ip.age-threshold must be > 0, got %v", c.Jobs.FreeIP.AgeThreshold))
	}

	return errs
}

// peerEdge — проекция одного cross-service ребра для transport-fail-closed gate:
// имя (для сообщения), резолвнутый addr, флаг «защищено» (mTLS ЛИБО one-way TLS),
// и config-ключи для actionable-текста. Зеркалит peerDialSpecs в composition root
// (cmd/kacho-loadbalancer/main.go) — там же строятся реальные conn'ы.
type peerEdge struct {
	name string
	addr string
	// secure — канал защищён хоть чем-то: взаимный либо односторонний TLS.
	secure bool
	// mtls — собеседник ДОКАЗЫВАЕТ СЕБЯ. Отдельное поле, а не вывод из secure:
	// односторонний TLS шифрует канал и молчит о том, кто клиент, поэтому свести
	// две величины в одну значило бы потерять именно то различие, которое строгая
	// посадка и требует.
	mtls    bool
	mtlsKey string
	tlsKey  string
}

// peerEdges — таблица cross-service рёбер для production transport-gate. addr =
// firstNonEmpty(Addr, InternalAddr) (single-addr dev-config тоже покрывается).
// secure = mtls.<edge>.enable || <edge>.tls. iam-register (authz Check edge)
// проверяется отдельно строгим mTLS-требованием выше; здесь — публичное
// iam-project (ProjectService.Get) ребро.
func (c Config) peerEdges() []peerEdge {
	firstNonEmpty := func(a, b string) string {
		if strings.TrimSpace(a) != "" {
			return strings.TrimSpace(a)
		}
		return strings.TrimSpace(b)
	}
	return []peerEdge{
		{
			name:    "vpc",
			addr:    firstNonEmpty(c.ExtAPI.VPC.Addr, c.ExtAPI.VPC.InternalAddr),
			secure:  c.MTLS.VPC.Enable || c.ExtAPI.VPC.TLS,
			mtls:    c.MTLS.VPC.Enable,
			mtlsKey: "vpc", tlsKey: "vpc",
		},
		{
			name:    "compute",
			addr:    firstNonEmpty(c.ExtAPI.Compute.Addr, c.ExtAPI.Compute.InternalAddr),
			secure:  c.MTLS.Compute.Enable || c.ExtAPI.Compute.TLS,
			mtls:    c.MTLS.Compute.Enable,
			mtlsKey: "compute", tlsKey: "compute",
		},
		{
			name:    "geo",
			addr:    firstNonEmpty(c.ExtAPI.Geo.Addr, c.ExtAPI.Geo.InternalAddr),
			secure:  c.MTLS.Geo.Enable || c.ExtAPI.Geo.TLS,
			mtls:    c.MTLS.Geo.Enable,
			mtlsKey: "geo", tlsKey: "geo",
		},
		{
			name:    "iam-project",
			addr:    firstNonEmpty(c.ExtAPI.IAM.Addr, c.ExtAPI.IAM.InternalAddr),
			secure:  c.MTLS.IAMProject.Enable || c.ExtAPI.IAM.TLS,
			mtls:    c.MTLS.IAMProject.Enable,
			mtlsKey: "iam-project", tlsKey: "iam",
		},
	}
}

// validateEndpoint — `tcp://host:port` парсится как url, схема обязательна,
// host:port извлекается. Пустая строка → ошибка.
func validateEndpoint(field, raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("%s: required", field)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%s: parse %q: %w", field, raw, err)
	}
	if u.Scheme != "tcp" {
		return fmt.Errorf("%s: scheme %q (want tcp)", field, u.Scheme)
	}
	host := u.Host
	if host == "" {
		return fmt.Errorf("%s: empty host:port in %q", field, raw)
	}
	// crude port check — net.SplitHostPort returns error if no port present
	if !strings.Contains(host, ":") {
		return fmt.Errorf("%s: %q missing :port", field, raw)
	}
	return nil
}
