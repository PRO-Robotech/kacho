// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package servicehost — НОСИТЕЛЬ входящего пути: одна точка входа, поднимающая
// оба слушателя сервиса с одним и тем же контуром работы с владельцем прав.
//
// # Что здесь механизм, а что правило
//
// [Serve] возвращает `error`, а НЕ сервер, и регистрация получает
// `grpc.ServiceRegistrar` — интерфейс с единственным методом. Поэтому у
// вызывающего не остаётся объекта, к которому можно приделать своё звено:
// восьмой сервис, желающий собственный порядок цепочки, не «нарушит правило» —
// ему не на чем его записать. Это свойство ПОСТРОЕНИЯ.
//
// Порядок звеньев внутри самой функции построением не держится — его держит то,
// что функция ОДНА и правка её видна как правка контура, а не как правка
// сервиса. Это честная граница, и выдавать её за невозможность нельзя.
//
// # Чем этот пакет отличается от `pkg/servicecontract`
//
// Тот решает, ЧТО МОЖНО ЗАПИСАТЬ, и его отказы — свойства самого дескриптора
// (О1, О6, О7, О8, О10). Здесь живут отказы, которым нужен СЛУЖИМЫЙ НАБОР RPC и
// выведенный из него каталог прав: О2, О3, О4, О5, О9. Разделение не
// косметическое: половину отказов невозможно вычислить, пока не известно, что
// процесс реально служит.
package servicehost

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"sync/atomic"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/reflect/protoreflect"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/pkg/auth"
	"github.com/PRO-Robotech/kacho/pkg/authz"
	"github.com/PRO-Robotech/kacho/pkg/authz/catalogderive"
	"github.com/PRO-Robotech/kacho/pkg/grpcclient"
	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
	"github.com/PRO-Robotech/kacho/pkg/servicecontract"
)

// Registrar приносит ТОЛЬКО обработчики.
//
// Тип не позволяет добавить звено — это и есть фиксированный слот. Аргумент —
// `grpc.ServiceRegistrar`, интерфейс с единственным методом `RegisterService`;
// сервера вызывающий не получает ни в каком виде.
//
// Регистратор вызывается РОВНО ОДИН раз на слушатель. «Позовём дважды, он же
// чистый» контрактом не является: чистоту чужой функции проверить нечем, а
// побочный эффект во втором вызове проявился бы как удвоение фоновой работы.
type Registrar func(grpc.ServiceRegistrar)

// Serve поднимает ОБА слушателя и обслуживает их до отмены ctx.
//
// Порядок, и он несущий: собрать серверы → зарегистрировать → снять СЛУЖИМЫЙ
// набор у самих серверов → вывести каталог и карту прав → прогнать отказы
// старта → и только потом слушать. Ни одно соединение не принимается раньше,
// чем отработали все десять отказов.
func Serve(ctx context.Context, d servicecontract.Descriptor, public, internal Registrar) error {
	if !d.Accepted() {
		return errors.New("servicehost: дескриптор не принят конструктором (servicecontract.New). " +
			"Нулевое значение собирается литералом и не проходило ни одного отказа старта — " +
			"поднимать по нему процесс значит не проверить ничего")
	}
	spec := d.Spec()
	log := spec.Logger

	// ── звено решения о доступе: слот, заполняемый ПОСЛЕ отказов старта ──────
	var slot decisionSlot

	publicSrv := grpcsrv.NewServer(
		grpc.Creds(spec.PublicCreds),
		grpc.ChainUnaryInterceptor(unaryChain(spec, &slot)...),
		grpc.ChainStreamInterceptor(streamChain(spec, &slot)...),
	)
	internalSrv := grpcsrv.NewServer(
		grpc.Creds(spec.InternalCreds),
		grpc.ChainUnaryInterceptor(unaryChain(spec, &slot)...),
		grpc.ChainStreamInterceptor(streamChain(spec, &slot)...),
	)

	public(publicSrv)
	internal(internalSrv)

	served := mergeServed(servedOf(publicSrv), servedOf(internalSrv))
	domains, err := domainsOf(served)
	if err != nil {
		return err
	}
	rpcMap, err := catalogderive.Derive(domains...)
	if err != nil {
		return fmt.Errorf("servicehost: %s не поднимается — карта прав не выводится из аннотаций "+
			"дескрипторов, слинкованных в этот бинарь: %w", spec.Service, err)
	}
	cat := catalogOf(domains)

	c, err := audit(spec, served, cat, rpcMap)
	if err != nil {
		return err
	}
	// Перепись печатается ВСЕГДА, а не только при находках: «ноль находок»
	// обязано быть отличимо от «ноль прочитанного». Процесс, поднявшийся молча,
	// неотличим от процесса, чьи отказы ничего не осмотрели.
	log.Info("servicehost: start refusals passed",
		"service", string(spec.Service),
		"mode", spec.Mode.String(),
		"census", c.String(),
		"domains", domains)

	intr, closeEdge, err := decisionLink(spec, rpcMap)
	if err != nil {
		return err
	}
	if closeEdge != nil {
		defer closeEdge()
	}
	slot.install(intr)

	return listenAndServe(ctx, spec, publicSrv, internalSrv)
}

// decisionLink строит звено решения о доступе — ОДНО на семь сервисов.
//
// До носителя каждый сервис держал свой пакет-обёртку над одним и тем же
// клиентом владельца модели: семь фабрик, расходившихся тем, какие поля они
// прокидывают, а какие оставляют умолчанию библиотеки. Здесь их одна, и
// величины приезжают полями дескриптора, а не берутся молча.
func decisionLink(spec servicecontract.Spec, m authz.RPCMap) (*authz.Interceptor, func(), error) {
	opts := authz.InterceptorOptions{
		ServiceName:  string(spec.Service),
		Map:          m,
		Cache:        authz.NewCache(spec.CacheWindow),
		Logger:       spec.Logger,
		CheckTimeout: spec.ClientBudget,
	}
	switch spec.Authz {
	case servicecontract.AuthzSelf:
		// Владелец модели решает у себя: клиента приносит он сам, ребра к себе
		// не бывает. Порт непуст — это проверил конструктор дескриптора.
		opts.Client = spec.SelfCheck
		return authz.NewInterceptor(opts), nil, nil

	case servicecontract.AuthzViaIAM:
		conn, err := grpc.NewClient(spec.CheckEdge.Addr(),
			grpc.WithTransportCredentials(spec.CheckEdge.Creds()),
			grpcclient.KeepaliveDialOption(true))
		if err != nil {
			return nil, nil, fmt.Errorf("servicehost: %s не поднимается — ребро решения о доступе "+
				"на %s не собирается: %w", spec.Service, spec.CheckEdge.Addr(), err)
		}
		opts.Client = &iamCheckClient{cli: iamv1.NewInternalIAMServiceClient(conn)}
		return authz.NewInterceptor(opts), func() { _ = conn.Close() }, nil

	default:
		// Недостижимо: конструктор дескриптора отвергает незаполненный источник.
		// Ветка остаётся отказом, а не пропуском: умолчание здесь было бы
		// решением о доступе, принятым никем.
		return nil, nil, fmt.Errorf("servicehost: %s не поднимается — источник решения о доступе "+
			"не назван", spec.Service)
	}
}

// iamCheckClient — адаптер клиента владельца модели под порт проверки.
//
// Исходящий контекст оборачивается [auth.PropagateOutgoing], чтобы на стороне
// владельца извлечение личности видело РЕАЛЬНОГО вызывающего, а не сервис,
// задающий вопрос.
type iamCheckClient struct {
	cli iamv1.InternalIAMServiceClient
}

func (c *iamCheckClient) Check(ctx context.Context, subjectID, relation, object string) (bool, error) {
	resp, err := c.cli.Check(auth.PropagateOutgoing(ctx), &iamv1.CheckRequest{
		SubjectId: subjectID,
		Relation:  relation,
		Object:    object,
	})
	if err != nil {
		return false, err
	}
	return resp.GetAllowed(), nil
}

var _ authz.CheckClient = (*iamCheckClient)(nil)

// listenAndServe поднимает оба слушателя и гасит их по отмене ctx.
//
// Ошибка ВНУТРЕННЕГО слушателя учитывается в исходе процесса наравне с
// публичной: при его крахе отмена контекста гасит публичный, и тот по контракту
// grpc-go возвращает nil. Считай исход только по публичной ошибке — крах
// внутренней плоскости дал бы нулевой код возврата, оркестратор не перезапустил
// бы процесс, и весь админ-путь тихо стал бы недоступен.
func listenAndServe(ctx context.Context, spec servicecontract.Spec, publicSrv, internalSrv *grpc.Server) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	publicLis, err := net.Listen("tcp", spec.PublicAddr)
	if err != nil {
		return fmt.Errorf("servicehost: публичный слушатель %s: %w", spec.PublicAddr, err)
	}
	internalLis, err := net.Listen("tcp", spec.InternalAddr)
	if err != nil {
		_ = publicLis.Close()
		return fmt.Errorf("servicehost: внутренний слушатель %s: %w", spec.InternalAddr, err)
	}
	spec.Logger.Info("servicehost: listening",
		"service", string(spec.Service),
		"public", spec.PublicAddr,
		"internal", spec.InternalAddr)

	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		<-ctx.Done()
		internalSrv.GracefulStop()
		publicSrv.GracefulStop()
	}()

	internalErr := make(chan error, 1)
	go func() {
		serr := internalSrv.Serve(internalLis)
		if serr != nil && !errors.Is(serr, grpc.ErrServerStopped) {
			spec.Logger.Error("servicehost: internal listener stopped; tearing down process", "err", serr)
			cancel()
			internalErr <- serr
			return
		}
		internalErr <- nil
	}()

	publicErr := publicSrv.Serve(publicLis)
	cancel()
	<-stopped
	return serveResult(publicErr, <-internalErr)
}

// serveResult сводит исход процесса из ошибок ДВУХ слушателей.
//
// Ошибка публичного приоритетна — это первичный сигнал отказа краевой
// поверхности. Но ошибка внутреннего обязана дойти до исхода, когда публичной
// нет: при крахе внутреннего слушателя отмена контекста гасит публичный, и тот
// по контракту grpc-go возвращает nil. Считай исход только по публичной
// ошибке — крах админ-плоскости дал бы нулевой код возврата, оркестратор не
// перезапустил бы процесс, и недоступность осталась бы тихой.
func serveResult(publicErr, internalErr error) error {
	if publicErr != nil {
		return publicErr
	}
	return internalErr
}

// decisionSlot — место звена решения о доступе в цепочке.
//
// # Почему слот, а не готовое звено
//
// Звено спрашивает карту прав, карта выводится из СЛУЖИМОГО набора RPC, а
// служимое известно только после регистрации — то есть после того, как сервер
// уже собран со своей цепочкой. Слот разрывает этот круг, не заводя второго
// прохода регистрации.
//
// # Почему пустой слот отказывает, а не пропускает
//
// Состояние посадки разрешением не бывает. Слот пуст ровно между сборкой
// сервера и концом отказов старта, и в этом окне ни одно соединение не принято —
// [Serve] слушает позже. Но ветка обязана существовать и быть fail-closed: она
// есть единственное, что стоит между «порядок однажды переставили» и «процесс
// обслуживает запросы, не спрашивая ни о чьих правах». Производитель у неё
// есть и назван пробой `TestChainRefusesBeforeTheDecisionLinkIsInstalled`, а не
// оставлен умозрительным.
type decisionSlot struct {
	p atomic.Pointer[authz.Interceptor]
}

func (s *decisionSlot) install(i *authz.Interceptor) { s.p.Store(i) }

// errNoDecisionLink — отказ пустого слота. Текст называет предмет прямо: это
// рантайм-диагностика, которую читает оператор, а не публичный рассказ о том,
// где было открыто.
func errNoDecisionLink() error {
	return status.Error(codes.Unavailable,
		"authorization decision link is not installed yet; refusing the call")
}

func (s *decisionSlot) unary() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
		i := s.p.Load()
		if i == nil {
			return nil, errNoDecisionLink()
		}
		return i.Unary()(ctx, req, info, h)
	}
}

func (s *decisionSlot) stream() grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, h grpc.StreamHandler) error {
		i := s.p.Load()
		if i == nil {
			return errNoDecisionLink()
		}
		return i.Stream()(srv, ss, info, h)
	}
}

// servedOf снимает служимый набор У САМОГО СЕРВЕРА.
//
// Источник — `grpc.Server.GetServiceInfo`, а не записывающая обёртка над
// регистратором. Разница в том, что обёртка видит лишь то, что через неё
// прошло: регистрация в обход (в том числе та, что делает сам конструктор
// сервера — здоровье и рефлексия) осталась бы ей невидимой, и «служить RPC и не
// отдать его дескриптор» перестало бы быть одной операцией.
func servedOf(srv *grpc.Server) servedSet {
	var out servedSet
	for name, info := range srv.GetServiceInfo() {
		for _, m := range info.Methods {
			out.methods = append(out.methods, servicecontract.MethodFQN("/"+name+"/"+m.Name))
		}
	}
	sort.Slice(out.methods, func(i, j int) bool { return out.methods[i] < out.methods[j] })
	return out
}

// mergeServed сводит наборы обоих слушателей. Оба судятся ОДНИМИ отказами:
// «internal = доверенный» — запрещённое допущение, и освобождать внутренний
// слушатель от проверки объявленного было бы ровно им.
func mergeServed(sets ...servedSet) servedSet {
	seen := map[servicecontract.MethodFQN]struct{}{}
	var out servedSet
	for _, s := range sets {
		for _, m := range s.methods {
			if _, dup := seen[m]; dup {
				continue
			}
			seen[m] = struct{}{}
			out.methods = append(out.methods, m)
		}
	}
	sort.Slice(out.methods, func(i, j int) bool { return out.methods[i] < out.methods[j] })
	return out
}

// domainsOf выводит proto-пакеты из служимого набора (К-2: домен ВЫВОДИТСЯ).
//
// Платформенные регистрации изымаются поимённо: домена Kachō у них нет, и
// спрашивать про них аннотации не у чего.
func domainsOf(served servedSet) ([]string, error) {
	seen := map[string]struct{}{}
	var out []string
	for _, m := range served.methods {
		if _, exempt := platformServices[serviceOf(m)]; exempt {
			continue
		}
		dom, err := domainOf(m)
		if err != nil {
			return nil, fmt.Errorf("servicehost: домен не выводится из имени служимого метода: %w", err)
		}
		if _, dup := seen[dom]; dup {
			continue
		}
		seen[dom] = struct{}{}
		out = append(out, dom)
	}
	if len(out) == 0 {
		return nil, errors.New("servicehost: из служимого набора не вывелось ни одного домена. " +
			"Ноль целей есть ОТКАЗ, а не успех: процесс без домена неотличим от исправного")
	}
	sort.Strings(out)
	return out, nil
}

// catalogOf собирает строки каталога из АННОТАЦИЙ дескрипторов, слинкованных в
// бинарь, — из того же источника, из которого генерируется каталог края.
// Второго объявления не существует, поэтому расходиться нечему.
func catalogOf(domains []string) catalogView {
	out := catalogView{rows: map[servicecontract.MethodFQN]catalogRow{}}
	catalogderive.RangeAnnotated(domains, func(fullMethod string, _ protoreflect.MethodDescriptor, a catalogderive.Annotations) {
		m := servicecontract.MethodFQN(fullMethod)
		dom, err := domainOf(m)
		if err != nil {
			// Недостижимо: имя собрано самим обходом из дескриптора. Строку всё
			// равно не пропускаем молча — иначе метод исчез бы из каталога и О2
			// доложил бы о нём как об отсутствующем, послав чинить не туда.
			dom = ""
		}
		out.rows[m] = catalogRow{
			Method:        m,
			Domain:        dom,
			ScopeFiltered: a.ScopeFiltered,
			HideExistence: a.HideExistence,
			ObjectType:    servicecontract.ObjectType(a.ScopeObjectType),
		}
	})
	return out
}

// unaryChain / streamChain — ПОРЯДОК ЗВЕНЬЕВ, один на все сервисы.
//
// Позиции и причина на каждую:
//
//  1. восстановление после паники — ПЕРВЫМ (внешним): ловит панику всего
//     нижележащего, включая сами звенья. Иначе одно разыменование nil на пути
//     запроса ОДНОГО тенанта прекращает обслуживание ВСЕХ;
//  2. личность пира по сертификату — чей это пир;
//  3. переданная личность, сужённая кругом — вправе ли пир говорить за другого.
//     Порядок 2→3 обязателен: решение о доверии по ещё не извлечённой личности
//     решением не является;
//  4. решение о доступе — читает уже извлечённого субъекта.
//
// # Чего в этой цепочке ПОКА НЕТ, и почему это названо, а не умолчано
//
// Полный порядок несёт ещё шесть позиций: запись об исходе, метрики и
// идентификатор запроса, верхняя граница обработки, слот ограничителя
// одновременности, загрузочный гейт мутаций и журнал исхода. Здесь их нет.
//
// Причина не «руки не дошли», а разная природа: верхняя граница обработки стоит
// сегодня у одного сервиса из семи и, будучи введённой разом, СДЕЛАЕТ ВИДИМЫМ
// то, что сегодня молчит, — поэтому её e2e-кейс обязан быть написан ДО правки,
// иначе «стало иначе» неотличимо от «сломалось». Ограничитель одновременности
// требует ИЗМЕРЕННОЙ ёмкости модели прав: потолок, выведенный неверно,
// превращает нагрузку в отказы на ПОЛОЖИТЕЛЬНОМ пути.
//
// Позиции при этом зафиксированы здесь, а не оставлены на усмотрение: когда
// звено введут, оно встанет на своё место, а не туда, где окажется удобно.
func unaryChain(spec servicecontract.Spec, slot *decisionSlot) []grpc.UnaryServerInterceptor {
	chain := []grpc.UnaryServerInterceptor{grpcsrv.UnaryPanicRecovery(spec.Logger)}
	chain = append(chain, grpcsrv.PrincipalExtractUnary(spec.Forwarders)...)
	return append(chain, slot.unary())
}

func streamChain(spec servicecontract.Spec, slot *decisionSlot) []grpc.StreamServerInterceptor {
	chain := []grpc.StreamServerInterceptor{grpcsrv.StreamPanicRecovery(spec.Logger)}
	chain = append(chain, grpcsrv.PrincipalExtractStream(spec.Forwarders)...)
	return append(chain, slot.stream())
}
