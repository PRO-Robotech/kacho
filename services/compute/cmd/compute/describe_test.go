// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// describe_test.go — что kacho-compute ОБЪЯВЛЯЕТ о себе носителю контура.
//
// Здесь спрашивается КОНСТРУКТОР дескриптора: он судит поля по себе — объявлена
// ли ось, положительна ли величина, сходится ли форма отказа с производителем,
// объявлено ли ребро решения о доступе. Половина отказов носителя так не
// проверяется — они существуют только там, где есть СЛУЖИМЫЙ НАБОР, снятый у
// самих серверов после регистрации. Их закрывает соседний `carrier_start_test.go`,
// и без него дескриптор мог бы нести ЛОЖНОЕ заявление (ровно так у storage и nlb
// прошло изъятие «скрывать нечего» на сервисе, который скрывает).

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"

	subscriptionv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"
	"github.com/PRO-Robotech/kacho/pkg/authz"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/pkg/outbox/bootgate"
	"github.com/PRO-Robotech/kacho/pkg/servicecontract"

	"github.com/PRO-Robotech/kacho/services/compute/internal/apps/kacho/api/instance"
	"github.com/PRO-Robotech/kacho/services/compute/internal/apps/kacho/api/machinetype"
	"github.com/PRO-Robotech/kacho/services/compute/internal/authzfilter"
	"github.com/PRO-Robotech/kacho/services/compute/internal/config"
	"github.com/PRO-Robotech/kacho/services/compute/internal/handler"
	computerepo "github.com/PRO-Robotech/kacho/services/compute/internal/repo"
)

// describeCfg — конфигурация, на которой дескриптор ОБЯЗАН приниматься.
//
// Посадка dev, и это не поблажка, а разделение предметов. Ручки транспорта
// слушателей здесь НЕ взводятся намеренно: взведённая ручка без файлов
// сертификата роняет сборку креденшелов, и всякая проба падала бы на предмете,
// которого не проверяет. Боевую строгость транспорта судит отдельная проба
// ниже — отказом конструктора, а не молчаливым пропуском.
//
// Величины (окно отзыва, срок вопроса о правах, бюджет отказов, обе границы
// времени) названы здесь литералами: предмет этого файла — что объявлено, а не
// откуда взяты умолчания; их читает `internal/config`.
//
// Порты нулевые: пробы, доходящие до подъёма, обязаны быть детерминированными, а
// фиксированный номер сделал бы их заложницами занятости машины прогона.
func describeCfg() config.Config {
	return config.Config{
		AuthMode:                  "dev",
		DBSSLMode:                 "require",
		GrpcPort:                  "0",
		InternalGrpcPort:          "0",
		AuthZIAMGRPCAddr:          "kaname-internal.kacho.svc:9091",
		AuthZTrustedForwarderSANs: []string{gatewaySAN},
		ListFilterEnabled:         true,
		FGARegisterDrainerEnabled: true,
		AuthZCacheTTL:             5 * time.Second,
		AuthZCheckTimeout:         2 * time.Second,
		AuthZDenyBudgetPerSec:     100,
		HandlingBudget:            30 * time.Second,
		// Величины подписки — те же, что даёт объявление ручек (`config.Config`).
		// Выписаны здесь потому, что проба строит настройку литералом, минуя
		// разбор окружения: пропущенное поле дало бы НОЛЬ, а ноль по этой оси
		// носитель законно отвергает.
		SubscriptionStreamBudget: time.Hour,
		SubscriptionMaxStreams:   16,
		SubscriptionIdlePoll:     2 * time.Second,
	}
}

// describeWith собирает дескриптор на заданной конфигурации.
//
// Сужатель строится ТЕМ ЖЕ `buildListFilter`, что и в композиционном корне:
// вторая сборка здесь разошлась бы с первой молча — обе продолжали бы возвращать
// непустой указатель.
func describeWith(t *testing.T, cfg config.Config) (servicecontract.Descriptor, error) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gate := bootgate.New(bootgate.Config{RequireIAM: cfg.RequireIAM, Service: "kacho-compute"})
	// Сужатель строится ТЕМ ЖЕ `buildListFilter`, что и в композиционном корне:
	// вторая сборка здесь разошлась бы с первой молча — обе продолжали бы
	// возвращать непустой указатель.
	return describe(cfg, logger, buildListFilter(cfg, nil, logger), gate, probeExistence{}, probeAuthzObserve, prometheus.NewRegistry())
}

// probeExistence — порт сверки существования для проб композиционного корня.
//
// Отвечает «объекта нет» на всё: предмет этих проб — отказы старта, которые
// носитель считает ДО первого соединения, и до вопроса к базе дело не доходит.
// Настоящая проба живёт на пуле (`internal/repo`), и подменять её здесь
// поведением было бы подменой предмета: конструктор требует ПРИНЕСЁННЫЙ порт, а
// не работающий.
type probeExistence struct{}

func (probeExistence) ObjectExists(context.Context, string, string) (bool, error) {
	return false, nil
}

// ProbeableTypes — охват ДЕЛЕГИРУЕТСЯ настоящей пробе сервиса.
//
// Подделка не вправе быть снисходительнее продукта: объяви она свой перечень —
// и сверка охвата на старте (`servicehost`, О5в) судила бы фикстуру вместо
// пробы, то есть молчала бы ровно там, где таблица настоящей разошлась с картой
// прав сервиса.
func (probeExistence) ProbeableTypes() []string {
	return (&computerepo.ExistenceProbe{}).ProbeableTypes()
}

// TestDescriptorIsAcceptedForCompute — дескриптор проходит ВСЕ отказы, которые
// являются его собственными свойствами, и несёт объявленные оси.
//
// Проба положительная и обязательна: без неё каждое отрицание ниже зеленело бы на
// дескрипторе, который не принимается вовсе.
func TestDescriptorIsAcceptedForCompute(t *testing.T) {
	desc, err := describeWith(t, describeCfg())
	if err != nil {
		t.Fatalf("дескриптор отвергнут конструктором — процесс не поднялся бы:\n%v", err)
	}
	spec := desc.Spec()

	if spec.Service != "kacho-compute" {
		t.Errorf("имя процесса %q — отказ, не называющий сервиса, на стенде из семи процессов бесполезен", spec.Service)
	}
	if spec.Mode != servicecontract.ModeDev {
		t.Errorf("посадка %v не соответствует конфигурации (dev) — режим в дескриптор не доехал", spec.Mode)
	}
	if budget, ok := spec.DenyBudget.Get(); !ok || budget != 100 {
		t.Errorf("бюджет отказов %v (объявлен=%v): величина обязана доехать из конфигурации, "+
			"иначе отсечка шторма молча выключается, а её счётчик становится навсегда нулевым", budget, ok)
	}
	if spec.HandlingBudget != 30*time.Second {
		t.Errorf("граница обработки вызова %v — величина не доехала из конфигурации", spec.HandlingBudget)
	}
	if gate, ok := spec.BootGate.Get(); !ok || gate == nil {
		t.Error("загрузочный гейт мутаций не объявлен величиной: в окне, пока путь доставки " +
			"намерений не поднят, машина создавалась бы без владельца")
	}
	if spec.Existence == nil {
		t.Error("порт сверки существования не провязан, хотя скрытие объявлено")
	}
}

// TestDescriptorWiresANarrowerForEveryScopeFilteredMethod — проводка сужателя
// сходится с каталогом прав, и сходится ИМЕНЕМ.
//
// Это ЗАМЕНА `TestDescriptorDeclaresNoNarrowersSinceTheStreamIsGone`. Прежняя
// проба закрепляла схождение ПУСТОГО с пустым: сужаемый метод у сервиса был один
// — собственный поток журнала изменений, — и он был снят. Сервис служит поток
// снова, уже общий, поэтому утверждение «проводок ноль» стало ложным. Заменена, а
// не ослаблена: снять из неё требование значило бы оставить ось без наблюдения
// вовсе, а носитель сверяет проводку с каталогом В ОБЕ СТОРОНЫ — лишний сужатель
// роняет старт так же, как потерянный.
//
// Утверждается не «карта непуста», а ИМЕННО ТОТ метод: непустая карта с чужим
// ключом сошлась бы по размеру и разошлась бы с каталогом — то есть дала бы
// ровно тот отказ старта, который проба обязана предупредить.
func TestDescriptorWiresANarrowerForEveryScopeFilteredMethod(t *testing.T) {
	desc, err := describeWith(t, describeCfg())
	if err != nil {
		t.Fatalf("дескриптор отвергнут: %v", err)
	}
	wired, ok := desc.Spec().Narrowers.Get()
	if !ok {
		t.Fatal("ось проводки сужателя объявлена НЕ величиной")
	}
	if len(wired) != 1 {
		t.Fatalf("проводок сужателя %d, ожидалась одна — общий поток изменений. "+
			"Ноль означает метод `scope_filtered` БЕЗ рубежа (пообъектной проверки "+
			"на крае за ним нет вовсе); больше одной — метод, чьей записи в каталоге "+
			"не соответствует ничего: %v", len(wired), wired)
	}
	n, ok := wired[subscriptionSubscribeFQN]
	if !ok {
		t.Fatalf("проводка есть, но не у %s: карта сошлась бы по размеру и разошлась "+
			"с каталогом — то есть дала бы отказ старта, %v", subscriptionSubscribeFQN, wired)
	}
	if n == nil {
		t.Fatal("проводка объявлена НУЛЕВЫМ сужателем: за этим методом пообъектной " +
			"проверки на крае нет вовсе, откатываться не на что, и поток ушёл бы " +
			"целиком под видом сужённого")
	}

	// Проводка обязана быть ТЕМ ЖЕ ЭКЗЕМПЛЯРОМ, что принесён корнем, а не вторым,
	// собранным внутри. Носитель сверяет с каталогом объект; собери дескриптор
	// свой — он объявил бы один сужатель, а на пути запроса стоял бы другой, и оба
	// остались бы непустыми указателями, то есть расхождение было бы молчаливым.
	//
	// Сужает ли он на самом деле — вопрос НЕ дескриптора, а сборки сервера: её
	// судит `subscription.NewServer`, отвергая подвешенный и не сужающий. Требовать
	// это здесь значило бы требовать от пробы живого соседа.
	brought := buildListFilter(describeCfg(), nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	desc2, err := describe(describeCfg(), slog.New(slog.NewTextHandler(io.Discard, nil)), brought,
		bootgate.New(bootgate.Config{Service: "kacho-compute"}), probeExistence{},
		probeAuthzObserve, prometheus.NewRegistry())
	if err != nil {
		t.Fatalf("дескриптор отвергнут: %v", err)
	}
	wired2, _ := desc2.Spec().Narrowers.Get()
	if wired2[subscriptionSubscribeFQN] != brought {
		t.Fatalf("в проводке НЕ тот сужатель, что принесён корнем: дескриптор объявил "+
			"бы один, а строки сужал бы другой — расхождение молчаливое, оба непустые "+
			"(объявлен %p, принесён %p)", wired2[subscriptionSubscribeFQN], brought)
	}
}

// TestHideExistenceFormsComeFromTheProducer — объявленная форма отказа совпадает
// с той, которой РЕАЛЬНО отвечает звено решения о доступе.
//
// Отвечает всегда таблица промахов владельцев, поэтому выписанная копия разошлась
// бы с действительностью и не покраснела бы нигде: объявление говорило бы одно, а
// вызывающий видел бы другое. Носитель эту сверку делает сам (О5); проба
// переносит её в прогон.
func TestHideExistenceFormsComeFromTheProducer(t *testing.T) {
	desc, err := describeWith(t, describeCfg())
	if err != nil {
		t.Fatalf("дескриптор отвергнут: %v", err)
	}
	forms, ok := desc.Spec().HideExistence.Get()
	if !ok || len(forms) == 0 {
		t.Fatal("ось скрытия существования объявлена изъятием либо пуста, а рантайм compute " +
			"скрывает ПО ФОРМЕ (чтение объекта глаголом v_get у типа с голосом владельца) — " +
			"заявление было бы ложным ровно так же, как оно было ложным у storage и nlb")
	}
	form, present := forms[servicecontract.ObjectType(authzfilter.ResourceTypeInstance)]
	if !present {
		t.Fatalf("форма отказа для %q не объявлена: ресурс ответил бы своей, отличимой формой — "+
			"то есть оракулом существования", authzfilter.ResourceTypeInstance)
	}
	owner, known := authz.OwnerNotFoundFormat(authzfilter.ResourceTypeInstance)
	if !known {
		t.Fatalf("у типа %q нет голоса владельца — предпосылка объявления исчезла", authzfilter.ResourceTypeInstance)
	}
	if string(form) != owner {
		t.Fatalf("объявленная форма %q расходится с той, которой ответит звено (%q): скрытие "+
			"работает ровно настолько, насколько текст отказа неотличим от промаха владельца", form, owner)
	}
}

// TestStreamBudgetIsDeclaredWithItsSubject — ось объявлена ВЕЛИЧИНОЙ, и величина
// переживает границу обработки одиночного вызова.
//
// Это ЗАМЕНА пробы, судившей ИЗЪЯТИЕ. Изъятие было верным ровно пока сервис не
// служил ни одного серверного стрима; с провязкой общего сервера подписки
// предмет у оси появился, и «стримов не служу» стало ложью о дереве.
// Ослабить прежнюю пробу было нельзя: она утверждала ОТСУТСТВИЕ, и снятие её
// утверждения оставило бы ось без наблюдения вовсе.
//
// Величина судится по СУЩЕСТВУ, а не по наличию: срок жизни потока обязан
// заметно превосходить границу обработки одиночного вызова, иначе поток
// закрывался бы раньше, чем успевает доехать первое событие догона, и подписчик
// читал бы штатный обрыв как «изменений нет».
func TestStreamBudgetIsDeclaredWithItsSubject(t *testing.T) {
	desc, err := describeWith(t, describeCfg())
	if err != nil {
		t.Fatalf("дескриптор отвергнут: %v", err)
	}
	spec := desc.Spec()
	if !spec.StreamBudget.Declared() {
		t.Fatal("ось срока жизни стрима НЕ объявлена: носитель роняет старт на " +
			"необъявленной оси")
	}
	if why, waived := spec.StreamBudget.NotApplicableBecause(); waived {
		t.Fatalf("ось объявлена ИЗЪЯТИЕМ (%q), а сервис служит поток подписки: "+
			"изъятие стало ложью о дереве — величина не названа никем", why)
	}
	budget, ok := spec.StreamBudget.Get()
	if !ok {
		t.Fatal("ось объявлена, но величины не несёт")
	}
	handling := spec.HandlingBudget
	if handling <= 0 {
		t.Fatal("граница обработки одиночного вызова не объявлена — сравнивать не с чем")
	}
	if budget <= handling {
		t.Fatalf("срок жизни потока %v не превосходит границы обработки %v: поток "+
			"закрывался бы раньше первого события догона, и подписчик читал бы "+
			"штатный обрыв как «изменений нет»", budget, handling)
	}
	t.Logf("срок жизни потока %v, граница обработки одиночного вызова %v", budget, handling)
}

// TestComputeServesTheSubscriptionStream — предмет ОБЪЯВЛЕННОЙ величины: поток
// подписки служится, и служится РОВНО ОДИН.
//
// Это ЗАМЕНА `TestComputeServesNoServerStreams`. Прежняя проба стерегла изъятие
// оси и требовала НУЛЯ серверных стримов; с провязкой общего сервера
// её утверждение стало ложным, а предмет — противоположным. Заменена, а не
// ослаблена: убери из неё требование, и ось осталась бы без наблюдения.
//
// Утверждается ТРОЙКА, и каждая часть закрывает свой промах:
//
//  1. стрим ЕСТЬ — иначе величина оси пережила бы свой предмет ровно так же,
//     как до этого его переживало изъятие;
//  2. стрим РОВНО ОДИН и это общий глагол — второй стрим означал бы второй язык
//     подписки, ради запрета которого весь эпик и заведён;
//  3. он на ВНУТРЕННЕМ слушателе и НЕ на публичном — `Internal.*` на внешнюю
//     поверхность не выходит (запретом на публикацию внутренних служб наружу), а «internal = доверенный» здесь не
//     допущение: оба слушателя обходятся порознь.
func TestComputeServesTheSubscriptionStream(t *testing.T) {
	const subscribeVerb = "/kacho.cloud.subscription.InternalSubscriptionService/Subscribe"

	seen := map[string][]string{}
	methods := 0
	for listener, reg := range registrarsByListener() {
		srv := grpc.NewServer()
		reg(srv)
		for name, info := range srv.GetServiceInfo() {
			for _, m := range info.Methods {
				methods++
				if m.IsServerStream {
					seen[listener] = append(seen[listener], "/"+name+"/"+m.Name)
				}
			}
		}
	}
	if methods == 0 {
		t.Fatal("ни один метод не зарегистрирован — утверждение о составе стримов " +
			"было бы вакуумным на пустом наборе")
	}
	if got := seen["public"]; len(got) != 0 {
		t.Fatalf("на ПУБЛИЧНОМ слушателе служатся стримы %v: поток подписки — "+
			"Internal-глагол, на внешнюю поверхность он не выходит (запретом на публикацию внутренних служб наружу)", got)
	}
	internal := seen["internal"]
	if len(internal) != 1 || internal[0] != subscribeVerb {
		t.Fatalf("на внутреннем слушателе служимые стримы %v, ожидался ровно один — %s.\n"+
			"Ноль означает, что общий сервер потока не провязан и объявленная величина "+
			"срока жизни ни к чему не относится; больше одного — второй язык подписки.",
			internal, subscribeVerb)
	}
	t.Logf("осмотрено служимых методов: %d, серверных стримов: публичный %d, внутренний %d",
		methods, len(seen["public"]), len(internal))
}

// TestDescriptorRefusesInsecureListenersInProduction — боевая посадка судится
// ОТВЕТОМ САМОГО ТРАНСПОРТА, а не ручкой конфигурации.
//
// Предмет назван у самого конструктора: сборщик креденшелов на невзведённой ручке
// отдаёт незашифрованные креды БЕЗ ошибки, поэтому процесс поднимался бы,
// отчитывался «проверка прав включена», и переданная личность пользователя ходила
// бы по открытому каналу. Ручка при этом могла выглядеть как угодно.
//
// Проба обязана называть ОБА слушателя: «internal = доверенный» — запрещённое
// допущение, и освобождение внутреннего было бы ровно им.
func TestDescriptorRefusesInsecureListenersInProduction(t *testing.T) {
	cfg := describeCfg()
	cfg.AuthMode = "production"

	_, err := describeWith(t, cfg)
	if err == nil {
		t.Fatal("дескриптор ПРИНЯТ в боевой посадке с незашифрованным транспортом обоих слушателей")
	}
	for _, field := range []string{"PublicCreds", "InternalCreds"} {
		if !strings.Contains(err.Error(), field) {
			t.Errorf("отказ не называет %s:\n%v", field, err)
		}
	}
}

// TestDescriptorRefusesWithoutTheDecisionEdge — отказ старта, когда ребро решения
// о доступе не задано.
//
// Это ЗАМЕНА прежнего `authzWiringDecision` и она СТРОГО СИЛЬНЕЕ. Прежняя ветка
// различала режимы: в боевом отсутствие звена было фатальным, вне боевого —
// процесс поднимался и обслуживал запросы БЕЗ единой проверки прав, о чём
// сообщал одним предупреждением. У носителя такой ветки не существует вовсе:
// «цепочка без звена решения» им не выражается, а ребро обязано быть объявлено
// ЯВНО, поэтому пустой адрес — отказ в ЛЮБОМ режиме, включая dev.
func TestDescriptorRefusesWithoutTheDecisionEdge(t *testing.T) {
	for _, mode := range []string{"dev", "production", "production-strict"} {
		t.Run(mode, func(t *testing.T) {
			cfg := describeCfg()
			cfg.AuthMode = mode
			cfg.AuthZTrustAnyForwarder = mode == "dev"
			cfg.AuthZIAMGRPCAddr = ""

			_, err := describeWith(t, cfg)
			if err == nil {
				t.Fatal("дескриптор ПРИНЯТ без ребра решения о доступе: процесс поднялся бы и " +
					"обслуживал запросы, не спрашивая ни о чьих правах")
			}
			if !strings.Contains(err.Error(), "CheckEdge") {
				t.Fatalf("отказ не называет незаполненного поля:\n%v", err)
			}
		})
	}
}

// TestDescriptorCarriesTheConfiguredCircle — круг доверенных отправителей доезжает
// до дескриптора ИЗ конфигурации, и разный вход даёт разный круг.
//
// Это ЗАМЕНА прежнего текстового стража, искавшего в `main.go` четыре вызова
// конструктора пары извлечения личности. Оба его предмета исчезли вместе с
// собственной сборкой цепочки: пару строит носитель, и строит её ОДИН раз на оба
// слушателя. Текстовый страж на такое дерево не переносится — он либо падал бы на
// верном коде, либо (после «починки» строки) утверждал бы про текст, а не про
// значение.
//
// Здесь утверждается ЗНАЧЕНИЕ, и второй случай обязателен: утверждение «круг
// равен ожидаемому» на ОДНОМ входе зеленело бы и на литерале, случайно совпавшем
// с ожиданием.
func TestDescriptorCarriesTheConfiguredCircle(t *testing.T) {
	circle := func(t *testing.T, sans ...string) []string {
		t.Helper()
		cfg := describeCfg()
		cfg.AuthZTrustedForwarderSANs = sans
		desc, err := describeWith(t, cfg)
		if err != nil {
			t.Fatalf("дескриптор не принят на круге %v: %v", sans, err)
		}
		circle, _ := desc.Spec().Forwarders.Get()
		return circle.SANs()
	}

	const otherSAN = "spiffe://kacho.cloud/ns/kacho-system/sa/kacho-nlb"

	both := circle(t, gatewaySAN, otherSAN)
	if len(both) != 2 || both[0] != gatewaySAN || both[1] != otherSAN {
		t.Fatalf("дескриптор несёт круг %v, конфигурация давала [%s %s]", both, gatewaySAN, otherSAN)
	}
	one := circle(t, gatewaySAN)
	if len(one) != 1 || one[0] != gatewaySAN {
		t.Fatalf("на сужённой конфигурации дескриптор несёт %v — значение не зависит от входа, "+
			"то есть в дескриптор уезжает не конфигурация", one)
	}
	// Значение отфильтровано так же, как фильтрует общий фундамент: иначе список из
	// одних пустых записей считался бы сужением, а транспорт видел бы пустой круг —
	// то есть «доверяем любому предъявившему сертификат».
	filtered := circle(t, " ", gatewaySAN+" ", "")
	if len(filtered) != 1 || filtered[0] != gatewaySAN {
		t.Fatalf("круг дескриптора = %#v, ожидался ровно [%q]", filtered, gatewaySAN)
	}
}

// registrarsOfBothListeners — регистраторы ОБОИХ слушателей, собранные так же,
// как их собирает `runServe`, включая рубеж слушателя.
//
// Use-cases собираются с нулевыми портами: ни один обработчик здесь не
// вызывается, предмет — только СОСТАВ зарегистрированного. Место одно на весь
// пакет намеренно: копия этой сборки в соседней пробе разошлась бы с первой
// молча — обе продолжали бы возвращать непустой набор.
func registrarsOfBothListeners() []func(grpc.ServiceRegistrar) {
	by := registrarsByListener()
	return []func(grpc.ServiceRegistrar){by["public"], by["internal"]}
}

// registrarsByListener — то же, но с ИМЕНЕМ слушателя.
//
// Имя нужно там, где предмет пробы — не состав вообще, а РАСПРЕДЕЛЕНИЕ служб по
// слушателям: «Internal.* не на публичном» неотличимо от «Internal.* нигде», если
// оба набора сложить в один. Сборка одна на весь пакет намеренно — копия
// разошлась бы с первой молча, и обе продолжали бы возвращать непустой набор.
//
// Сервер подписки собирается ЗАГЛУШКОЙ (`subscriptionProbeServer`), а не
// настоящим: предмет здесь — СОСТАВ зарегистрированного, и настоящий потребовал
// бы базы, сужателя и объявления посадки. Что провязан именно настоящий,
// утверждает проба подъёма носителя, где его пропажа красит прогон.
func registrarsByListener() map[string]func(grpc.ServiceRegistrar) {
	svcs := &services{
		machineType: machinetype.NewMachineTypeService(nil, nil),
		instance:    instance.NewInstanceService(nil, nil, nil, nil, nil, nil, nil, nil),
	}
	opsRepo := operations.NewRepo(nil, "public")

	return map[string]func(grpc.ServiceRegistrar){
		"public": func(r grpc.ServiceRegistrar) {
			registerPublicServices(handler.PublicRegistrar(r, false), svcs, opsRepo, nil)
		},
		"internal": func(r grpc.ServiceRegistrar) {
			registerInternalServices(handler.InternalRegistrar(r, false), svcs,
				subscriptionv1.UnimplementedInternalSubscriptionServiceServer{})
		},
	}
}

// probeAuthzObserve — приёмник величин кеша вердиктов для проб КОНСТРУКТОРА.
//
// Заглушка здесь законна: предмет этих проб — что судит конструктор дескриптора,
// а не куда уезжают величины. Настоящий приёмник, чей вызов носителем
// утверждается, стоит в пробе подъёма (`carrier_start_test.go`): там его пропажа
// красит пробу, здесь — не может по построению.
func probeAuthzObserve(func() authz.Metrics) {}
