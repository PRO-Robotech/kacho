// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// admission_wiring_test.go — потолок темпа и одновременности на ОБОИХ
// gRPC-слушателях края: не «объявлен», а исполняется.
//
// # Что здесь держится, и почему именно это
//
// Перепись усыновления по дереву (`internal/repohygiene`
// TestFoundationCapabilitiesAreAdoptedOrAccountedFor) отвечает на вопрос
// «доезжает ли возможность до места сборки сервера». Этого мало ровно в трёх
// местах, и каждое здесь закрыто своим случаем:
//
//	ВЕЛИЧИНЫ    молчание посадки обязано разрешаться ПОЛОМ ПЛАТФОРМЫ, а её
//	            ЧАСТИЧНОЕ объявление — отказом старта. Ноль, принятый молча,
//	            означал бы слушателя без потолка, отчитавшегося о посадке.
//	ГРАНИЦА     что покрыто, а что намеренно нет: поверхность арендатора идёт
//	            ЧЕРЕЗ обёртку, служебная — мимо неё. И ни одна служба не идёт
//	            через ОБЕ формы сразу — иначе её поток платил бы дважды.
//	ИСХОД       на настоящем слушателе, поднятом настоящим помощником: поток
//	            сверх объявленного получает отказ с контрактным текстом, а
//	            законный сосед в тот же миг проходит.
//
// # Чего здесь нет и почему
//
// Внешний слушатель собирается в `main()`, вызвать которую случай не может.
// Поэтому его звено проверяется с двух сторон порознь: поведение самой формы —
// в `pkg/grpcsrv/admission_unknown_service_test.go` (проксируемый метод без
// дескриптора отвергается и ключуется личностью, поставленной звеном раньше), а
// МЕСТО звена в цепочке — разбором синтаксиса ниже. Порядок звеньев есть
// свойство исходника, и закрепляется оно гейтом по исходнику, а не пробой,
// которая вызвала бы то же звено вне цепочки и утверждала бы о нём ничего.
package main

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
	"github.com/PRO-Robotech/kacho/gateway/internal/opsproxy"
	apigatewayv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/apigateway/v1"
	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
)

// --- ВЕЛИЧИНЫ ---------------------------------------------------------------

// TestAdmissionLimitsFallToThePlatformFloorWhenThePostureIsSilent — молчание
// посадки означает ПОЛ ПЛАТФОРМЫ, а не отсутствие потолка.
//
// Это положительный контроль ко всему остальному в файле: если бы молчание
// давало пустой набор, конструктор ограничителя отказал бы, край не поднялся бы
// вовсе, и любой случай ниже краснел бы по причине, не имеющей отношения к его
// заголовку.
func TestAdmissionLimitsFallToThePlatformFloorWhenThePostureIsSilent(t *testing.T) {
	var cfg config.Config // посадка молчит по обеим осям

	require.True(t, cfg.AdmissionPublic.IsSilent())
	require.True(t, cfg.AdmissionInternal.IsSilent())

	public, internal, err := admissionLimits(cfg)
	require.NoError(t, err)
	require.Equal(t, grpcsrv.PlatformPublicAdmission(), public,
		"внешний слушатель обязан взять пол платформы: арендатору обещан ОДИН пол на "+
			"весь продукт, и он не должен зависеть от того, во что арендатор упёрся первым")
	require.Equal(t, grpcsrv.PlatformInternalAdmission(), internal,
		"внутренний слушатель обязан взять СВОЙ пол — заведомо более щедрый: запрос "+
			"модуля несёт личности разных арендаторов")
	require.True(t, public.IsDeclared() && internal.IsDeclared(),
		"обе величины обязаны быть ОБЪЯВЛЕНЫ: необъявленный набор конструктор "+
			"ограничителя отвергает, и слушатель поднялся бы без потолка")
}

// TestAdmissionLimitsRefuseAPartiallyDeclaredPosture — частичное объявление есть
// ОТКАЗ, а не «часть защиты».
//
// Самый опасный вход: он выглядит настройкой и не ограничивает по незаполненной
// оси, а оператор считает предел выставленным. Проверяются ОБЕ оси — иначе отказ
// на одной был бы неотличим от отказа вообще на любом непустом наборе.
func TestAdmissionLimitsRefuseAPartiallyDeclaredPosture(t *testing.T) {
	t.Run("внешний", func(t *testing.T) {
		var cfg config.Config
		cfg.AdmissionPublic.ReadPerSec = 50 // темп назван, одновременность забыта
		_, _, err := admissionLimits(cfg)
		require.Error(t, err, "частичный набор обязан ронять старт")
		require.Contains(t, err.Error(), "KACHO_API_GATEWAY_ADMISSION_PUBLIC",
			"отказ обязан назвать ГРУППУ ручек: искать её оператор пойдёт в файл, где, "+
				"по его мнению, всё написано верно")
	})
	t.Run("внутренний", func(t *testing.T) {
		var cfg config.Config
		cfg.AdmissionInternal.InFlight = 8 // одновременность названа, темп забыт
		_, _, err := admissionLimits(cfg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "KACHO_API_GATEWAY_ADMISSION_INTERNAL")
	})
	t.Run("отрицательная величина отвергается в любом режиме", func(t *testing.T) {
		var cfg config.Config
		cfg.AdmissionPublic = grpcsrv.AdmissionKnobs{
			ReadPerSec: -1, MutationPerSec: 20, BurstFactor: 5, InFlight: 16,
		}
		_, _, err := admissionLimits(cfg)
		require.Error(t, err, "негодное объявление негодно вне зависимости от посадки")
	})
}

// --- ГРАНИЦА ПОКРЫТИЯ -------------------------------------------------------

// recordingRegistrar запоминает дескрипторы, которые до него доехали.
type recordingRegistrar struct {
	descs []*grpc.ServiceDesc
}

func (r *recordingRegistrar) RegisterService(sd *grpc.ServiceDesc, _ any) {
	r.descs = append(r.descs, sd)
}

func (r *recordingRegistrar) names() []string {
	out := make([]string, 0, len(r.descs))
	for _, d := range r.descs {
		out = append(out, d.ServiceName)
	}
	return out
}

// TestEdgeSurfaceSplitsTenantTrafficFromTheHostSurface — что покрыто потолком, а
// что намеренно нет.
//
// Обе стороны утверждаются сразу и на одном входе. Одна без другой ничего не
// доказывает: «здоровье не под потолком» зеленеет и на крае, где под потолком
// нет НИЧЕГО, а «опрос операций под потолком» — на крае, где под ним и проверка
// готовности, то есть где нагрузка на API превращается в перезапуск пода.
func TestEdgeSurfaceSplitsTenantTrafficFromTheHostSurface(t *testing.T) {
	// Настоящий сервер — в роли «мимо потолка»; записывающий регистратор — в роли
	// «через потолок». Тогда собственный отчёт сервера показывает РОВНО то, что
	// потолком не покрыто, а перечень записанного — ровно то, что покрыто.
	// Сервер в первой роли нужен настоящий: параметр типизирован *grpc.Server,
	// потому что служебная поверхность регистрируется помощником библиотеки.
	guarded := &recordingRegistrar{}
	srv := grpc.NewServer()
	t.Cleanup(srv.Stop)
	registerExternalGRPCServices(srv, guarded, nil, opsproxy.New(nil))
	unguarded := srv.GetServiceInfo()

	require.NotEmpty(t, unguarded,
		"ПРЕДПОСЫЛКА: мимо потолка не зарегистрировано НИЧЕГО — тогда «здоровье осталось "+
			"снаружи» означало бы «прочитано ноль», а не «прочитано и найдено»")
	require.NotEmpty(t, guarded.names(),
		"ПРЕДПОСЫЛКА: через потолок не зарегистрировано НИЧЕГО — тогда обёртка края "+
			"покрывает пустоту, и утверждение ниже зеленело бы на снятой защите")

	require.Contains(t, unguarded, "grpc.health.v1.Health",
		"проверка здоровья обязана остаться МИМО потолка: отказ ей из-за нагрузки на "+
			"API — единственный ответ, который превращает нагрузку в перезапуск пода")
	require.NotContains(t, guarded.names(), "grpc.health.v1.Health",
		"…и обязана не уехать под обёртку")

	require.Equal(t, []string{"kacho.cloud.operation.OperationService"}, guarded.names(),
		"опрос операций — поток АРЕНДАТОРА, самый частый вызов, на который край "+
			"отвечает сам, и он обязан идти ЧЕРЕЗ потолок")
	require.NotContains(t, unguarded, "kacho.cloud.operation.OperationService",
		"…и не обязан заодно оставаться снаружи: две дороги к одной службе означали бы "+
			"либо двойное списание, либо необъявленный обход")

	// Двойного списания нет by construction: под обёртку не уходит ни одного
	// потокового метода, а значит ни один метод не платит и обёртке, и звену.
	for _, d := range guarded.descs {
		require.Empty(t, d.Streams,
			"служба %q, зарегистрированная через обёртку, несёт потоковый метод. "+
				"Поток на внешнем слушателе проходит ЕЩЁ И через звено допуска — "+
				"значит он списывается дважды, и объявленный предел вдвое ниже "+
				"обещанного. Либо снимите поток, либо разведите формы: обёртка — для "+
				"зарегистрированных унарных служб, звено — для проксируемого потока.",
			d.ServiceName)
	}
}

// --- МЕСТО ЗВЕНА В ЦЕПОЧКЕ ВНЕШНЕГО СЛУШАТЕЛЯ -------------------------------

// TestExternalAdmissionSitsAfterIdentityAndBeforeAuthorization — порядок
// звеньев, прочитанный РАЗБОРОМ СИНТАКСИСА композиционного корня.
//
// Разбором, а не текстом: имена этих звеньев стоят и в объяснениях рядом с ними,
// и предикат по подстроке зеленел бы на собственном комментарии — записанный
// класс (`testing.md` §«Гейт на класс», п. 4).
//
// Порядок несущий в ОБЕ стороны:
//
//	ПОСЛЕ личности — ключом ведра служит она. Ограничитель, ключующийся раньше
//	  звена, её устанавливающего, ложит весь поток в одно безымянное ведро и
//	  снимается подстановкой чужого заголовка, то есть ограничивает только того,
//	  кто не пытается его обойти. Цена измерена в
//	  `pkg/grpcsrv/admission_unknown_service_test.go`.
//	ДО решения о правах — оно есть СЕТЕВОЙ вызов к iam на КАЖДОМ запросе, и все
//	  запросы края идут туда под ОДНОЙ личностью сертификата, то есть в ОДНО
//	  ведро на внутреннем слушателе iam. Поток одного арендатора, не
//	  остановленный здесь, вычерпал бы это общее ведро — и решение о правах
//	  перестало бы приниматься для ВСЕХ.
func TestExternalAdmissionSitsAfterIdentityAndBeforeAuthorization(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", nil, parser.SkipObjectResolution)
	require.NoError(t, err, "композиционный корень обязан разбираться")

	// Позиция ВЫЗОВА, а не упоминания: объявление и комментарий вызовом не
	// являются, поэтому переименование переменной обвалит случай честно, а
	// объяснение рядом со звеном на него не повлияет.
	pos := map[string]token.Pos{}
	want := map[string]string{
		"identity":  "authInterceptor.Stream",
		"admission": "externalAdmission.StreamInterceptor",
		"refusal":   "proxy.StreamRefuseInternalRoute",
		"authz":     "authzMW.Stream",
	}
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		x, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		got := x.Name + "." + sel.Sel.Name
		for role, name := range want {
			if got != name {
				continue
			}
			require.Zero(t, pos[role],
				"звено %q (%s) вызывается в корне больше одного раза — порядок по "+
					"позиции перестал быть однозначным, и случай надо переписать под "+
					"новую раскладку, а не ослабить", role, name)
			pos[role] = call.Lparen
		}
		return true
	})

	for role, name := range want {
		require.NotZero(t, pos[role],
			"ПРЕДПОСЫЛКА: вызова %s в композиционном корне нет. Это либо снятое звено, "+
				"либо переименование — и в обоих случаях порядок ниже утверждался бы о "+
				"пустоте", name)
	}

	require.Less(t, int(pos["identity"]), int(pos["admission"]),
		"звено допуска стоит ДО звена личности: ключ ведра пуст, весь поток края "+
			"ложится в одно безымянное ведро, и предел на арендатора превращается в "+
			"предел на процесс")
	require.Less(t, int(pos["admission"]), int(pos["refusal"]),
		"звено допуска стоит ПОСЛЕ отказа в маршруте: перебор несуществующих методов "+
			"перестал бы стоить перебирающему")
	require.Less(t, int(pos["admission"]), int(pos["authz"]),
		"звено допуска стоит ПОСЛЕ решения о правах: тогда поток одного арендатора "+
			"доходит до iam целиком, а туда все запросы края идут под ОДНОЙ личностью "+
			"сертификата — то есть в одно ведро, вычерпав которое, арендатор лишает "+
			"решения о правах ВСЕХ остальных")
}

// --- ИСХОД НА НАСТОЯЩЕМ ВНУТРЕННЕМ СЛУШАТЕЛЕ --------------------------------

// TestInternalListenerRefusesOverTheDeclaredRate — внутренний слушатель края
// исполняет потолок, а не объявляет его.
//
// «Внутренний — значит доверенный» здесь запрещено ровно так же, как в вопросе о
// правах: сюда ходит толкатель iam, и модуль, ушедший в петлю повторов, обнулял
// бы кэш решений о доступе непрерывно — каждый следующий запрос арендатора снова
// шёл бы в iam за решением, то есть отказ одного соседа превращался бы в
// нагрузку на всех.
func TestInternalListenerRefusesOverTheDeclaredRate(t *testing.T) {
	inv := &fakeInvalidator{}
	externalSrv := grpc.NewServer()
	t.Cleanup(externalSrv.Stop)

	// Крошечный бюджет: предмет — исход на проводе, а не числа посадки. Всплеск
	// мутаций = 1 × 2 = 2, и InvalidateSubject мутация по конвенции имён.
	tiny := grpcsrv.AdmissionLimits{ReadPerSec: 1, MutationPerSec: 1, BurstFactor: 2, InFlight: 4}
	srv, lis, adm, err := startInternalGRPCListener(":0", inv,
		externalSrv, internalListenerSecurity{}, tiny, probeLatency(t), nil)
	require.NoError(t, err)
	require.NotNil(t, adm, "помощник обязан вернуть ограничитель: без него счёт "+
		"допущенных и отвергнутых печатать нечем, и мёртвый потолок был бы невидим")
	require.Equal(t, "internal", adm.Listener())
	require.Equal(t, tiny, adm.Limits())

	ready := make(chan struct{})
	go func() { close(ready); _ = srv.Serve(lis) }()
	<-ready
	t.Cleanup(func() {
		srv.GracefulStop()
		_ = lis.Close()
	})

	conn, err := grpc.NewClient(lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	client := apigatewayv1.NewInternalAuthzCacheServiceClient(conn)

	call := func(subject string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_, err := client.InvalidateSubject(ctx,
			&apigatewayv1.InvalidateSubjectRequest{Subject: subject})
		return err
	}

	// Первые два укладываются во всплеск и обязаны ДОЙТИ до обработчика: без
	// этого положительного контроля отказ ниже неотличим от «потолок отвергает
	// всё подряд».
	for i := 0; i < 2; i++ {
		require.NoError(t, call("usr-drainer"),
			"вызов %d обязан дойти до обработчика", i+1)
	}

	err = call("usr-drainer")
	require.Equal(t, codes.ResourceExhausted, status.Code(err),
		"третий вызов сверх всплеска обязан быть отвергнут потолком")
	require.Equal(t, grpcsrv.MsgMutationRateExceeded, status.Convert(err).Message(),
		"текст отказа — часть контракта и обязан назвать исчерпанную ось")

	st := adm.Stats()
	require.EqualValues(t, 2, st.Admitted)
	require.EqualValues(t, 1, st.RejectedRate)
	require.EqualValues(t, 2, inv.calls,
		"до обработчика обязаны дойти ровно допущенные: отвергнутый потолком вызов "+
			"не должен трогать кэш решений о доступе")
}

// TestInternalListenerRefusesToStartOnUnusableLimits — негодные величины роняют
// СБОРКУ слушателя, а не поднимают его без потолка.
//
// Слушатель, поднявшийся с ограничителем-пустышкой, выглядит и отчитывается
// ровно как защищённый. Отдельно проверяется, что сорванная сборка не оставляет
// занятым порт: иначе следующая попытка старта падала бы на «адрес занят», и
// причина отказа читалась бы не та.
func TestInternalListenerRefusesToStartOnUnusableLimits(t *testing.T) {
	externalSrv := grpc.NewServer()
	t.Cleanup(externalSrv.Stop)

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := lis.Addr().String()
	require.NoError(t, lis.Close())

	srv, gotLis, adm, err := startInternalGRPCListener(addr, &fakeInvalidator{},
		externalSrv, internalListenerSecurity{},
		grpcsrv.AdmissionLimits{ReadPerSec: 10}, probeLatency(t), nil) // объявлена ОДНА ось из четырёх
	require.Error(t, err, "негодный набор обязан ронять сборку слушателя")
	require.Nil(t, srv)
	require.Nil(t, gotLis)
	require.Nil(t, adm)

	// Порт свободен: сорванная сборка убрала за собой.
	probe, err := net.Listen("tcp", addr)
	require.NoError(t, err, "сорванная сборка оставила порт занятым — следующая "+
		"попытка старта отказала бы по другой причине, и оператор искал бы не там")
	require.NoError(t, probe.Close())
}
