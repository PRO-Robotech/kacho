// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"google.golang.org/grpc"

	"github.com/PRO-Robotech/kacho/pkg/subjectchange"

	subscriptionv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"

	"github.com/PRO-Robotech/kacho/gateway/internal/clients"
	"github.com/PRO-Robotech/kacho/gateway/internal/config"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
	"github.com/PRO-Robotech/kacho/gateway/internal/proxy"
	"github.com/PRO-Robotech/kacho/gateway/internal/streamrevocation"
	"github.com/PRO-Robotech/kacho/gateway/internal/subscriptionstream"
)

// buildSubscriptionStreamHandler собирает единственную проекцию потока.
//
// # Почему адрес НЕ отдельная ручка конфигурации
//
// Внутренний адрес каждого домена край уже объявляет
// (`KACHO_API_GATEWAY_<ДОМЕН>_INTERNAL_GRPC`). Второе объявление того же адреса
// разошлось бы с первым молча — и разошлось бы именно там, где расхождение не
// видно: оба непусты, оба резолвятся, а ведут в разные места. Поэтому владелец
// называется ИМЕНЕМ ДОМЕНА, а адрес берётся из уже объявленного.
//
// # Почему отказ старта, а не отказ запроса
//
// Владелец, названный посадкой, но не имеющий внутреннего адреса, — ошибка
// посадки, а не вызывающего. Обнаруживать её первым запросом в бою значит
// узнать о ней тогда, когда она уже кому-то стоила ответа.
func buildSubscriptionStreamHandler(
	cfg config.Config,
	backends proxy.Backends,
	logger *slog.Logger,
) (*subscriptionstream.Handler, error) {
	names := cfg.SubscriptionOwnerNames()
	owners := make(subscriptionstream.Owners, len(names))
	missing := make([]string, 0, len(names))

	for _, name := range names {
		// Ключ внутреннего адреса даёт config — карта соединений там же объявлена,
		// и второе его написание разошлось бы с ней молча.
		conn := backends[config.InternalBackendKey(name)]
		if conn == nil {
			missing = append(missing, name)
			continue
		}
		owners[name] = subscriptionv1.NewInternalSubscriptionServiceClient(conn)
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, fmt.Errorf(
			"KACHO_API_GATEWAY_SUBSCRIPTION_OWNERS называет владельцев без внутреннего адреса: %s — "+
				"подписка живёт только на внутреннем слушателе владельца, и дозвониться до названного домена нечем",
			strings.Join(missing, ", "))
	}

	handler, err := subscriptionstream.NewHandler(subscriptionstream.Config{
		Owners:               owners,
		StreamBudget:         cfg.SubscriptionStreamBudget,
		Heartbeat:            cfg.SubscriptionHeartbeat,
		MaxStreams:           cfg.SubscriptionMaxStreams,
		MaxStreamsPerSubject: cfg.SubscriptionMaxStreamsPerSubject,
		Logger:               logger,
	})
	if err != nil {
		return nil, err
	}

	// Самоотчёт процесса называет ЧИСЛО объявленных владельцев и их имена.
	//
	// Ноль остаётся законным ИСХОДОМ (профиль вправе выключить возможность), но
	// перестал быть состоянием поставки: умолчание чарта края называет всех
	// владельцев, служащих глагол (kacho#1388). Поэтому ноль обязан быть ВИДЕН —
	// молчаливый, он неотличим от «всё поднялось», а означает `501` каждому.
	logger.Info("subscription stream projection ready",
		"path", subscriptionstream.Path,
		"owners", owners.Names(),
		"owner_count", len(owners),
		"stream_budget", cfg.SubscriptionStreamBudget.String(),
		"heartbeat", cfg.SubscriptionHeartbeat.String(),
		"max_streams", cfg.SubscriptionMaxStreams,
		"max_streams_per_subject", cfg.SubscriptionMaxStreamsPerSubject)

	return handler, nil
}

// revocationStaleFactor / revocationStaleFloor — во сколько перепросов подряд
// обходится срок, после которого край считает читателя отзыва потерянным.
//
// # Почему величина ВЫВОДИТСЯ, а не объявляется ручкой
//
// Она измеряет отказ НАЗВАННОГО перепроса — и всякий раз, когда его период
// меняют, должна меняться вместе с ним. Ручка, объявленная отдельно, разошлась
// бы с периодом молча и разошлась бы именно там, где расхождение не видно: обе
// непусты, обе выглядят разумно, а fail-closed наступает либо на всякой
// заминке, либо никогда.
//
// Перепросов, отзыв которых доезжает до открытых соединений, ДВА, и величина у
// них общая намеренно: изменения субъекта (kacho#1022) и состояние
// удостоверения (kacho#1410). Их периоды разные — каждый выведен из СВОЕЙ
// объявленной границы, — а правило «сколько подряд пропусков означает потерю
// читателя» одно, и второе его написание разошлось бы с первым молча.
//
// # Почему пять, а не «побольше на всякий случай»
//
// Один пропущенный перепрос — заминка сети, а не потеря читателя; объявить её
// аварией значит закрывать потоки всего флота на каждом чихе соседа. Пять подряд
// заминкой уже не бывают. Пол в десять секунд держит нижнюю сторону: при частом
// перепросе пять периодов складываются в срок, за который сосед не успевает
// даже перезапуститься.
//
// Верхнюю сторону держит не эта константа, а страж старта: срок обязан быть
// заметно меньше срока жизни потока, иначе fail-closed не наступает ни разу.
const (
	revocationStaleFactor = 5
	revocationStaleFloor  = 10 * time.Second
)

// revocationStaleAfter — срок, после которого неподтверждённое чтение отзыва
// само становится решением закрыть потоки.
func revocationStaleAfter(pollInterval time.Duration) time.Duration {
	stale := pollInterval * revocationStaleFactor
	if stale < revocationStaleFloor {
		return revocationStaleFloor
	}
	return stale
}

// buildSubjectChangeWatcher собирает читателя отзыва и связывает его с реестром
// открытых потоков.
//
// # Почему это ФУНКЦИЯ, а не десять строк в точке сборки
//
// Провязку закрывателя нельзя доказать, пока она живёт в `main`: инъекция —
// передать ноль здесь — оставляла ВЕСЬ корпус проб края зелёным и код
// собирающимся, потому что сквозная проба зовёт конструктор читателя напрямую,
// минуя продовую точку сборки. Вынесенная функция даёт пробе тот самый вход,
// который исполняется в бою.
//
// Сам ноль закрывателя отвергает уже [subjectchange.New]; здесь проверяется ВТОРАЯ
// половина того же предмета — что край передаёт туда именно СВОЙ реестр
// открытых потоков, а не что-нибудь непустое.
func buildSubjectChangeWatcher(
	cfg config.Config,
	poller subjectchange.Poller,
	flush func(),
	streams *subscriptionstream.Handler,
	logger *slog.Logger,
) (*subjectchange.Watcher, error) {
	if streams == nil {
		return nil, fmt.Errorf("subject-change watcher: проекция потока не собрана — " +
			"читателю отзыва нечего закрывать, и это ошибка порядка сборки, а не посадки")
	}
	staleAfter := revocationStaleAfter(cfg.SubjectChangePollInterval)
	if staleAfter >= cfg.SubscriptionStreamBudget {
		return nil, fmt.Errorf(
			"subject-change watcher: срок неподтверждённого чтения отзыва %v не меньше срока жизни "+
				"потока %v — fail-closed не наступит ни разу, а закрытие по собственному бюджету "+
				"потока выглядело бы закрытием по отзыву",
			staleAfter, cfg.SubscriptionStreamBudget)
	}
	return subjectchange.New(subjectchange.Config{
		Poller:     poller,
		Flush:      flush,
		Interval:   cfg.SubjectChangePollInterval,
		Closer:     streams,
		StaleAfter: staleAfter,
		Logger:     logger,
	})
}

// credentialRecheckInterval — период перепроса состояния УДОСТОВЕРЕНИЯ на
// открытых потоках, он же ОБЪЯВЛЕННОЕ ОКНО отзыва для длинного соединения.
//
// # Почему величина ВЫВОДИТСЯ из срока кэша интроспекции, а не объявляется своей ручкой
//
// Граница отзыва удостоверения на пути ЗАПРОСА и есть этот срок: каждый запрос
// спрашивает состояние заново, а свежесть ответа ограничена кэшем. Открытое
// соединение обязано honorировать ТУ ЖЕ границу — иначе у одного механизма две
// величины, и вторая, объявленная отдельно, разошлась бы с первой молча: обе
// непусты, обе выглядят разумно, а окно отзыва для потоков тихо становится в
// разы шире объявленного.
//
// Именно это и было предметом kacho#1410: окно потока задавалось сроком жизни
// соединения (`SubscriptionStreamBudget`), то есть было шире объявленной
// границы во столько раз, во сколько бюджет больше срока кэша.
func credentialRecheckInterval(cfg config.Config) time.Duration {
	return time.Duration(cfg.IntrospectionCacheTTLSeconds) * time.Second
}

// buildStreamRevocationSweeper собирает перепрос состояния удостоверения на
// открытых потоках и связывает его с реестром.
//
// # Почему это ФУНКЦИЯ, а не десять строк в точке сборки
//
// По тому же доводу, что и у соседнего читателя: инъекция «передать ноль в
// точке сборки» оставляет ВЕСЬ корпус проб края зелёным и код собирающимся,
// потому что сквозные пробы зовут конструктор напрямую и продовую точку сборки
// минуют. Вынесенная функция даёт пробе тот самый вход, который исполняется в
// бою.
func buildStreamRevocationSweeper(
	cfg config.Config,
	iamInternal *grpc.ClientConn,
	streams *subscriptionstream.Handler,
	logger *slog.Logger,
) (*streamrevocation.Sweeper, error) {
	if streams == nil {
		return nil, fmt.Errorf("streamrevocation: проекция потока не собрана — " +
			"перепросу нечего закрывать, и это ошибка порядка сборки, а не посадки")
	}
	if iamInternal == nil {
		// Контроль, у которого нет МЕХАНИЗМА исполниться. Соседа, у которого
		// спрашивают про отзыв, здесь не существует вовсе — а поднявшийся с этим
		// край выглядел бы исправным и не отказал бы ни разу за всю свою жизнь.
		return nil, fmt.Errorf("streamrevocation: внутренний адрес службы прав не объявлен — " +
			"спросить про отзыв удостоверения открытого потока не у кого, и перепрос " +
			"не отказал бы ни разу просто потому, что никуда не дозванивается")
	}
	interval := credentialRecheckInterval(cfg)
	if interval <= 0 {
		return nil, fmt.Errorf(
			"streamrevocation: KACHO_INTROSPECTION_CACHE_TTL_SECONDS = %d — из него выводится "+
				"объявленное окно отзыва для открытого соединения, и неположительное означает, "+
				"что окна нет вовсе",
			cfg.IntrospectionCacheTTLSeconds)
	}
	staleAfter := revocationStaleAfter(interval)
	if staleAfter >= cfg.SubscriptionStreamBudget {
		return nil, fmt.Errorf(
			"streamrevocation: срок неподтверждённого перепроса %v не меньше срока жизни потока %v — "+
				"fail-closed не наступит ни разу, а закрытие по собственному бюджету потока "+
				"выглядело бы закрытием по отзыву",
			staleAfter, cfg.SubscriptionStreamBudget)
	}
	// Окно обязано honorировать границу КАЖДОЙ полосы, за которую перепрос
	// отвечает, а не только той, из которой выведено (kacho#1450). Полоса
	// базового секрета объявляет свою границу константой; окно шире неё означало
	// бы, что обещание «отозванное отвергается не позже N» действует на пути
	// запроса и тихо не действует на открытом соединении.
	//
	// Отказ, а не минимум из двух: минимум прошёл бы молча и сделал бы
	// объявленную оператором величину невидимой — посадка работала бы не так,
	// как объявлена, и узнать об этом было бы неоткуда.
	if interval > middleware.BasicCredentialVerdictWindow {
		return nil, fmt.Errorf(
			"streamrevocation: окно отзыва %v шире границы %v, объявленной полосой базового "+
				"секрета — на открытом соединении её обещание перестало бы действовать, "+
				"оставаясь верным на пути запроса. Сузьте KACHO_INTROSPECTION_CACHE_TTL_SECONDS "+
				"либо решите расхождение величин осознанно",
			interval, middleware.BasicCredentialVerdictWindow)
	}
	if interval >= cfg.SubscriptionStreamBudget {
		return nil, fmt.Errorf(
			"streamrevocation: окно отзыва %v не меньше срока жизни потока %v — "+
				"перепрос не состоялся бы на потоке НИ РАЗУ, и объявленное окно осталось бы "+
				"объявленным",
			interval, cfg.SubscriptionStreamBudget)
	}
	return streamrevocation.New(streamrevocation.Config{
		Streams:    streams,
		Authority:  clients.NewSessionRevocationsAdapter(iamInternal),
		Interval:   interval,
		StaleAfter: staleAfter,
		Logger:     logger,
	})
}
