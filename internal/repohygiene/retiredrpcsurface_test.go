// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// retiredRPCSurface — НАДГРОБИЕ: имена, снятые с контракта, которые не должны
// вернуться молча.
//
// Почему перепись, а не `reserved` в самом контракте, — см. шапку
// retiredrpcsurface.go: грамматика protobuf не принимает `reserved` внутри
// `service`, а у метода нет номера. Это единственная форма резервирования,
// выразимая для снятой RPC-поверхности в этом дереве.
//
// Запись НЕ истекает. Надгробие — не послабление: послабление живёт, пока у него
// есть предмет, а надгробие обязано пережить любое количество зелёных прогонов,
// иначе имя вернётся ровно тогда, когда про него забудут. Снять запись —
// осознанное решение владельца контракта, а не следствие того, что «давно
// ничего не находило».
var retiredRPCSurface = []RetiredRPC{
	// ── Задача #813: снят поток событий журнала мутаций compute ──────────────
	{
		FQN: "kacho.cloud.compute.v1.InternalWatchService/Watch",
		Reason: "поток событий журнала мутаций, у которого не было потребителя ни одного дня: " +
			"порождённый клиент не звался ни из одного прод-файла дерева. Сам журнал " +
			"(`compute_outbox`) жив и читается восстановлением наблюдаемого состояния — снят " +
			"единственный способ подписаться на него снаружи. Имя не должно вернуться как " +
			"«поток на будущее»: подписка, у которой нет подписчика, не бесплатна — она несёт " +
			"своё сужение прав, свою запись каталога, свой предел одновременных потоков и " +
			"свою поверхность на внутреннем порту.",
	},
	// ── Задача #1024: снято ОБРАТНОЕ РЕБРО «владелец прав зовёт своего потребителя» ──
	{
		FQN: "kacho.cloud.apigateway.v1.InternalAuthzCacheService/InvalidateSubject",
		Reason: "гашение кэша решений края ТОЛЧКОМ ИЗ iam. Имя снято вместе со всем своим " +
			"пакетом контракта, потому что предметом его было НАПРАВЛЕНИЕ, а не механизм: " +
			"владелец прав объявлен листом графа рёбер (его зовут, он не зовёт никого), а " +
			"здесь он дозванивался до своего потребителя. Адрес края был вдобавок " +
			"обязательной ручкой старта — то есть владелец прав не поднимался там, где края " +
			"нет вовсе, и это, а не сам вызов, делало вынос iam отдельным продуктом " +
			"невыразимым. Замена: край САМ читает журнал смены субъекта курсором и гасит " +
			"свой кэш сам; ребро осталось потребитель→владелец. Имя не должно вернуться под " +
			"замысел «а нам нужно быстрее»: сходимость соседних реплик — свойство ЧТЕНИЯ, и " +
			"ускоряется оно окном чтения, а не вторым ребром обратно. Вместе с методом снят " +
			"внутренний gRPC-слушатель края: он жил ради этой одной службы, и порт без " +
			"единого метода есть входная поверхность без предмета.",
	},
	// ── Стадия S6 эпика #747: снят внешний движок прав ────────────────────────
	//
	// Пять имён одной причины: их предметом было ЧУЖОЕ хранилище отношений — его
	// кортежи, его модель, его store id. Хранилища нет, и предмета у них нет тоже.
	{
		FQN: "kaname.cloud.iam.v1.AuthorizeService/ListObjects",
		Reason: "перечисление отвечало ОГРАНИЧЕННЫМ ПРЕФИКСОМ без продолжения: потолок ставила " +
			"чужая сторона, признак усечения отдавался честно, а получить остаток было нельзя " +
			"никак — объекты сверх потолка оставались недостижимы ПРИ ЖИВЫХ ПРАВАХ. Заменителя " +
			"не введено намеренно: «что мне видно» отвечает постраничный List ресурсной службы, " +
			"сужающий СТРАНИЦУ пообъектной проверкой. Имя не должно вернуться под замысел " +
			"«перечисли вселенную → отфильтруй».",
	},
	{
		FQN:    "kaname.cloud.iam.v1.InternalAuthorizeService/ReadTuples",
		Reason: "чтение хранилища кортежей чужого движка. Своя проекция читается своими запросами.",
	},
	{
		FQN: "kaname.cloud.iam.v1.InternalAuthorizeService/ReloadModel",
		Reason: "пин идентификатора модели, который чеканил движок. Модель прав осталась и стала " +
			"источником истины формы, но версии у неё больше не чеканит никто: она встроена в " +
			"образ службы, и «перезагрузить» её означает выкатить образ.",
	},
	{
		FQN: "kaname.cloud.iam.v1.InternalAuthorizeService/GetFGAStoreInfo",
		Reason: "сведения о чужом хранилище: его store id, счётчик кортежей, когда модель была в " +
			"него записана. Ни одной из этих величин не существует.",
	},
	{
		FQN:    "kacho.cloud.vpc.v1.RouteTableService/AddRoutes",
		Reason: "метод отвечал отказом при любом входе: идентичности маршрута в модели нет, адресовать правку было нечем. Имя не должно вернуться под другой замысел — правка набора идёт заменой набора целиком.",
	},
	{
		FQN:    "kacho.cloud.vpc.v1.RouteTableService/RemoveRoutes",
		Reason: "то же: гранулярная правка маршрута без идентичности маршрута невыразима.",
	},
	{
		FQN:    "kacho.cloud.vpc.v1.RouteTableService/UpdateRoute",
		Reason: "то же: гранулярная правка маршрута без идентичности маршрута невыразима.",
	},
	{
		FQN:    "kacho.cloud.vpc.v1.AddressService/GetByValue",
		Reason: "внешняя ветвь была неавторизуема по построению — область бралась из подсети, которой у внешнего адреса нет. Замена: список с сужением по значению (поле ip_address).",
	},
	{
		FQN:    "kacho.cloud.vpc.v1.AddressService/ListBySubnet",
		Reason: "строгое подмножество списка, который уже несёт сужение по подсети: два пути к одному ответу с разными объектами проверки прав.",
	},
	{
		FQN:    "kacho.cloud.vpc.v1.NetworkService/ListSubnets",
		Reason: "второй путь к ответу списка подсетей с ДРУГИМ объектом проверки прав. Замена: список подсетей с сужением по сети.",
	},
	{
		FQN:    "kacho.cloud.vpc.v1.NetworkService/ListSecurityGroups",
		Reason: "второй путь к ответу списка групп правил с другим объектом проверки прав. Замена: список групп с сужением по сети.",
	},
	{
		FQN:    "kacho.cloud.vpc.v1.NetworkService/ListRouteTables",
		Reason: "второй путь к ответу списка таблиц с другим объектом проверки прав. Замена: список таблиц с сужением по сети (поле network_id в белом списке фильтра заведено ДО снятия).",
	},
	{
		FQN: "kacho.cloud.vpc.v1.InternalDataplaneService/WatchIntent",
		Reason: "поток желаемого состояния исполнителю датаплейна. Снят вместе со всем швом " +
			"(kacho#400): исполнителя не существует — вызывающей стороны у отчёта о применении " +
			"не было ни одной, и единственным писателем подтверждений оставался сам приёмный RPC. " +
			"Имя не должно вернуться молча: следующий, кому понадобится доставка намерения, обязан " +
			"сперва предъявить ИСПОЛНИТЕЛЯ, а не поверхность под него",
	},
	{
		FQN: "kacho.cloud.vpc.v1.InternalDataplaneService/ReportIntentApplied",
		Reason: "приём подтверждения применения от исполнителя датаплейна. Снят тем же изменением " +
			"и по той же причине: приём существовал, отчитываться было некому. Именно он делал " +
			"публичное поле состояния применения вечным «не применено» на каждом ресурсе — " +
			"обещанием возможности, которой нет",
	},
	{
		FQN: "kacho.cloud.compute.v1.InternalResourceLifecycleService/Subscribe",
		Reason: "фид жизненного цикла ресурсов был объявлен в трёх доменах, а реализован " +
			"ровно в одном (loadbalancer.v1). Объявление compute не несло ни сервера, ни клиента, " +
			"ни одной неgenerated-ссылки — включая типы сообщений",
	},
	{
		FQN: "kacho.cloud.vpc.v1.InternalResourceLifecycleService/Subscribe",
		Reason: "то же объявление в домене vpc — без сервера, клиента и неgenerated-ссылок. " +
			"ЗАПИСЬ ОСТАЁТСЯ В СИЛЕ и после того, как подписка вернётся: возвращается она " +
			"ОБЩЕЙ формой пакета kacho.cloud.subscription (эпик kacho#1016), а не доменным " +
			"именем. Снятое имя обещало фид жизненного цикла ОДНОГО домена — что случилось с " +
			"ресурсом vpc; общая форма объявляет один язык подписки для всех владельцев сразу: " +
			"оси фильтра, непрозрачную позицию и служебное первое сообщение. Занять доменное " +
			"имя под общий замысел значило бы вернуть его под чужой смысл — ровно то, что " +
			"надгробие и запрещает. Читателю, который снова захочет фид: подписка объявляется " +
			"один раз в общем пакете и импортируется доменом",
	},
	// ── Задача #814: снята единственная РЕАЛИЗОВАННАЯ копия того же фида ──────
	{
		FQN: "kacho.cloud.loadbalancer.v1.InternalResourceLifecycleService/Subscribe",
		Reason: "третья копия фида жизненного цикла и единственная из трёх, у которой был " +
			"сервер. Потребителей у неё не было ни одного: порождённый клиент не звался ни из " +
			"одного прод-файла дерева. Снята отдельной задачей, поэтому в надгробие имя не " +
			"легло вместе с двумя своими копиями и осталось незарезервированным — эту дыру " +
			"закрывает данная запись, а не та задача. Имя не должно вернуться под замысел " +
			"«а нам нужна подписка»: подписка объявляется ОДИН раз общей формой пакета " +
			"kacho.cloud.subscription (эпик kacho#1016) и импортируется доменами, а не " +
			"заводится каждым владельцем под своим именем. Остаток снятой формы — читатель " +
			"журнала без вызывающих — снимается отдельно (kacho#1043), и снимать его надлежит " +
			"ПОСЛЕ переноса написанной в нём техники устоявшегося горизонта: иначе снятие " +
			"мёртвого кода уничтожит единственный написанный в дереве ответ на потерю строки " +
			"при возобновлении",
	},
	{
		FQN: "kacho.cloud.vpc.v1.InternalWatchService/Watch",
		Reason: "поток событий из журнала мутаций: объявление vpc осталось без единой реализации " +
			"и без неgenerated-ссылок. Одноимённая форма compute, которая на момент этого " +
			"снятия была живой и ради которой объявление vpc и признали лишним, снята с тех пор " +
			"задачей kacho#813 — её имя стоит в этом же надгробии. Подписки на журнал в дереве " +
			"не осталось ни одной; она возвращается общей формой пакета kacho.cloud.subscription " +
			"(эпик kacho#1016), а не восстановлением доменного имени",
	},
	// ── Две административные двери в движок отношений (#788) ────────────────
	//
	// Обе писали кортёж в чужое хранилище отношений НАПРЯМУЮ, мимо журнала
	// `kaname.fga_outbox`, на котором стоит проекция `relation_fact`
	// (инвариант миграции 0098). Вызывающих не было ни у одной: соседи давно
	// ушли на `RegisterResource`, эмитирующий намерение в журнал.
	//
	// Расхождения не было только потому, что двери не срабатывали, — это
	// свойство трафика, а не системы. Поэтому исход «снять», а не «перевести
	// на журнал»: перевод дал бы работу без выгодоприобретателя.
	{
		FQN: "kaname.cloud.iam.v1.InternalAuthorizeService/WriteTuples",
		Reason: "административная запись набора кортежей мимо журнала. Вызывающих ноль; " +
			"собственный комментарий контракта называл вызывающим outbox-worker, который " +
			"на самом деле писал своим дренажом, а не этим RPC. Хранилища, мимо которого " +
			"можно было бы писать, больше нет вовсе: намерение кладётся строкой журнала " +
			"`kaname.fga_outbox`, из которой триггер складывает прямой факт в той же " +
			"транзакции. Имя не должно вернуться молча: следующему, кому понадобится " +
			"массовая правка отношений, придётся сперва объяснить, почему её не выразить " +
			"строкой журнала",
	},
	{
		FQN: "kaname.cloud.iam.v1.InternalIAMService/WriteCreatorTuple",
		Reason: "синхронная запись кортежа создателя мимо журнала. Все пять соседей ушли " +
			"на RegisterResource (nlb называет это дословно: «Replaces the former direct " +
			"WriteCreatorTuple»), и вызывающих не осталось ни одного. Живой путь — " +
			"RegisterResource/UnregisterResource: намерение ложится строкой журнала, и " +
			"триггер складывает из неё прямой факт в той же транзакции — дренажа наружу " +
			"больше нет",
	},
	{
		FQN: "kaname.cloud.iam.v1.InternalIamHooksService/TokenHook",
		Reason: "хуки Hydra обслуживаются по HTTP (services/iam/internal/handler/iamhooks), и обслуживаются " +
			"СВОИМИ структурами тела запроса под контракт Hydra — типы этого proto не читает ни одна строка " +
			"неgenerated-кода. gRPC-объявление описывало замысел, который не был реализован",
	},
	{
		FQN: "kaname.cloud.iam.v1.InternalIamHooksService/RefreshTokenHook",
		Reason: "вторая половина того же неreализованного gRPC-объявления хуков Hydra; живой путь — " +
			"HTTP-обработчик refresh_hook_handler.go со своей формой тела",
	},
	// ── Волна 1 модуля вычислений (приёмка COMP-E1a) ───────────────────────
	//
	// Семь методов не несли реализации и отвечали отказом «не реализовано», при
	// этом были выставлены на ТРЁХ поверхностях сразу — маршруты края, список
	// проксирования, каталог прав. Восьмой реализован, но его предмет — свободная
	// карта метаданных — снят той же волной.
	//
	// Резервирование номера и имени здесь невыразимо: грамматика принимает
	// `reserved` только внутри message и enum, а у метода нет номера. Namesake
	// возвращается не молча — эта перепись и есть механизм.
	{
		FQN:    "kacho.cloud.compute.v1.InstanceService/AddOneToOneNat",
		Reason: "трансляция адреса — предмет домена сети; у неё есть публичный путь правки интерфейса",
	},
	{
		FQN:    "kacho.cloud.compute.v1.InstanceService/RemoveOneToOneNat",
		Reason: "парный глагол к предыдущему, тот же владелец возможности",
	},
	{
		FQN:    "kacho.cloud.compute.v1.InstanceService/UpdateNetworkInterface",
		Reason: "свойства интерфейса правит его владелец — домен сети; у потребителя остаётся только привязка",
	},
	{
		FQN: "kacho.cloud.compute.v1.InstanceService/Relocate",
		Reason: "перенос между зонами требует переноса тома, то есть согласия владельца хранилища; " +
			"глагол одного домена этого не выражает",
	},
	{
		FQN:    "kacho.cloud.compute.v1.InstanceService/ListAccessBindings",
		Reason: "привязки доступа — предмет домена управления доступом, а не потребителя",
	},
	{
		FQN:    "kacho.cloud.compute.v1.InstanceService/SetAccessBindings",
		Reason: "то же основание, мутирующая половина",
	},
	{
		FQN:    "kacho.cloud.compute.v1.InstanceService/UpdateAccessBindings",
		Reason: "то же основание, третий глагол той же тройки",
	},
	{
		FQN: "kacho.cloud.compute.v1.InstanceService/UpdateMetadata",
		Reason: "единственный снятый метод, который был РЕАЛИЗОВАН: снят вместе со своим предметом — " +
			"свободной картой метаданных, которая была вторым местом об одном предмете",
	},
}

func retiredRPCSurfaceOptions(t *testing.T) RetiredRPCSurfaceOptions {
	t.Helper()
	return RetiredRPCSurfaceOptions{
		Root:      repoRoot(t),
		APIRoot:   "pkg/api",
		ProtoRoot: "proto",
		CatalogPaths: []string{
			filepath.Join("gateway", "internal", "middleware", "embed", "permission_catalog.json"),
			filepath.Join("services", "iam", "internal", "apps", "kaname", "seed", "embedded", "permission_catalog.json"),
		},
		Retired: retiredRPCSurface,
	}
}

// TestRetiredRPCSurface_NoRetiredNameCameBack — положительная сторона на
// НАСТОЯЩЕМ дереве.
func TestRetiredRPCSurface_NoRetiredNameCameBack(t *testing.T) {
	var log strings.Builder
	findings, census, err := AuditRetiredRPCSurface(retiredRPCSurfaceOptions(t), &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))

	// Премиса: анализатор прочитал все три плеча. Ноль находок обязано быть
	// отличимо от нуля прочитанного.
	if census.DeclaredMethods < 100 || census.DeclaredSvcs < 10 {
		t.Fatalf("из стабов прочитано сервисов %d, методов %d — разбор не нашёл того, что заведомо есть",
			census.DeclaredSvcs, census.DeclaredMethods)
	}
	if census.ProtoFiles < 20 || census.ProtoSvcs < 10 {
		t.Fatalf("из контракта прочитано файлов %d, сервисов %d — разбор не нашёл того, что заведомо есть",
			census.ProtoFiles, census.ProtoSvcs)
	}
	if census.CatalogFiles != 2 || census.CatalogRows < 200 {
		t.Fatalf("копий каталога прочитано %d, строк суммарно %d — прочитаны не обе копии",
			census.CatalogFiles, census.CatalogRows)
	}

	if len(findings) == 0 {
		return
	}
	lines := make([]string, 0, len(findings))
	for _, f := range findings {
		lines = append(lines, "  "+f.String())
	}
	t.Errorf("%d снятых имён вернулись в дерево:\n%s", len(findings), strings.Join(lines, "\n"))
}

// ── доказательство того, что анализатор способен упасть ─────────────────────

// retiredTinyTree материализует минимальное дерево: стабы, контракт и каталог,
// содержимое которых задаёт вызывающий.
func retiredTinyTree(t *testing.T, protoBody, stubBody string, catalogFQNs []string) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	write("pkg/api/kacho/cloud/demo/v1/demo_grpc.pb.go", stubBody)
	write("proto/kacho/cloud/demo/v1/demo.proto", protoBody)
	rows := make([]string, 0, len(catalogFQNs))
	for _, f := range catalogFQNs {
		rows = append(rows, `{"fqn":"`+f+`","permission":"demo.p"}`)
	}
	write("catalog.json", "["+strings.Join(rows, ",")+"]")
	return root
}

func retiredTinyOptions(root string, retired ...RetiredRPC) RetiredRPCSurfaceOptions {
	return RetiredRPCSurfaceOptions{
		Root: root, APIRoot: "pkg/api", ProtoRoot: "proto",
		CatalogPaths: []string{"catalog.json"}, Retired: retired,
	}
}

// Стабы в форме, которую эмитит protoc-gen-go-grpc: два сервиса в одном файле —
// тот случай, в котором текстовый разбор приписал бы методы одного другому.
const retiredTinyStubs = `package demov1

var AlphaService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "kacho.cloud.demo.v1.AlphaService",
	Methods: []grpc.MethodDesc{{MethodName: "Ping"}},
}
var BetaService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "kacho.cloud.demo.v1.BetaService",
	Methods: []grpc.MethodDesc{{MethodName: "Pong"}},
}
`

const retiredTinyProto = `syntax = "proto3";
package kacho.cloud.demo.v1;
service AlphaService {
  rpc Ping (Req) returns (Res);
}
service BetaService {
  rpc Pong (Req) returns (Res);
}
`

// TestRetiredRPCSurface_CatchesEachReturnPath — инъекция НАСТОЯЩИМ входом:
// снятое имя, вернувшееся каждым из трёх путей, обязано быть найдено с
// координатой.
func TestRetiredRPCSurface_CatchesEachReturnPath(t *testing.T) {
	// Имя, объявленное всюду: и в контракте, и в стабах, и в каталоге.
	root := retiredTinyTree(t, retiredTinyProto, retiredTinyStubs,
		[]string{"kacho.cloud.demo.v1.AlphaService/Ping"})
	dead := RetiredRPC{FQN: "kacho.cloud.demo.v1.AlphaService/Ping", Reason: "снято в тесте"}

	findings, _, err := AuditRetiredRPCSurface(retiredTinyOptions(root, dead), nil)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	kinds := map[string]bool{}
	for _, f := range findings {
		kinds[f.Kind] = true
		if !strings.Contains(f.String(), "AlphaService/Ping") {
			t.Errorf("находка не называет координату: %s", f.String())
		}
		if !strings.Contains(f.Reason, "снято в тесте") {
			t.Errorf("находка не несёт причину снятия: %s", f.String())
		}
	}
	for _, want := range []string{"redeclared-stub", "redeclared-proto", "catalog-row"} {
		if !kinds[want] {
			t.Errorf("путь возвращения %q не пойман (найдено: %v)", want, kinds)
		}
	}
}

// TestRetiredRPCSurface_SilentOnLegitimateTwin — вторая сторона контроля:
// ЗАКОННАЯ конструкция той же формы обязана проходить молча.
//
// Близнец подобран так, чтобы срабатывание могло случиться только по существу, а
// не по форме: живой сервис лежит в ТОМ ЖЕ файле контракта и в том же файле
// стабов, что и снятый, и его метод называется так же (`Ping`) — различаются
// только имена сервисов. Гейт, ключующийся на имени метода или на имени файла,
// здесь покраснеет.
func TestRetiredRPCSurface_SilentOnLegitimateTwin(t *testing.T) {
	root := retiredTinyTree(t, retiredTinyProto, retiredTinyStubs,
		[]string{"kacho.cloud.demo.v1.AlphaService/Ping", "kacho.cloud.demo.v1.BetaService/Pong"})

	// Снят метод, которого в дереве действительно нет: у BetaService есть Pong, но
	// нет Ping; у AlphaService есть Ping, но нет Pong.
	dead := RetiredRPC{FQN: "kacho.cloud.demo.v1.BetaService/Ping", Reason: "снято в тесте"}

	findings, census, err := AuditRetiredRPCSurface(retiredTinyOptions(root, dead), nil)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("законная конструкция той же формы вызвала %d находок — гейт ловит форму, а не существо:\n  %s",
			len(findings), findings[0].String())
	}
	// Премиса самого контроля: молчание получено на прочитанном дереве, а не на пустом.
	if census.DeclaredMethods == 0 || census.ProtoSvcs == 0 || census.CatalogRows == 0 {
		t.Fatalf("контроль молчал на пустом входе (методов %d, сервисов контракта %d, строк каталога %d) — "+
			"он ничего не доказывает", census.DeclaredMethods, census.ProtoSvcs, census.CatalogRows)
	}
}

// TestRetiredRPCSurface_EmptyLedgerIsAnError — пустое надгробие не «ноль
// находок», а ошибка: инертный гейт зеленеет на любом дереве.
func TestRetiredRPCSurface_EmptyLedgerIsAnError(t *testing.T) {
	root := retiredTinyTree(t, retiredTinyProto, retiredTinyStubs, []string{"kacho.cloud.demo.v1.AlphaService/Ping"})
	if _, _, err := AuditRetiredRPCSurface(retiredTinyOptions(root), nil); err == nil {
		t.Fatal("пустая перепись прошла как «ноль находок» — гейт инертен и об этом не сообщает")
	}
}

// TestRetiredRPCSurface_ReadsEveryCatalogCopy — копия, которую забыли
// перегенерировать, обязана быть найдена. Именно ради этого случая читаются обе.
func TestRetiredRPCSurface_ReadsEveryCatalogCopy(t *testing.T) {
	root := retiredTinyTree(t, retiredTinyProto, retiredTinyStubs, nil)
	// Первая копия чистая, вторая — со снятым именем.
	if err := os.WriteFile(filepath.Join(root, "catalog.json"),
		[]byte(`[{"fqn":"kacho.cloud.demo.v1.BetaService/Pong","permission":"demo.p"}]`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "catalog2.json"),
		[]byte(`[{"fqn":"kacho.cloud.demo.v1.AlphaService/Ping","permission":"demo.p"}]`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	opts := retiredTinyOptions(root, RetiredRPC{FQN: "kacho.cloud.demo.v1.AlphaService/Ping", Reason: "снято в тесте"})
	opts.CatalogPaths = []string{"catalog.json", "catalog2.json"}

	findings, _, err := AuditRetiredRPCSurface(opts, nil)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	var found bool
	for _, f := range findings {
		if f.Kind == "catalog-row" && f.Where == "catalog2.json" {
			found = true
		}
	}
	if !found {
		t.Errorf("строка снятого имени во ВТОРОЙ копии каталога не найдена (находки: %v)", findings)
	}
}

// TestRetiredRPCSurface_LedgerNamesAreWellFormedAndUnique — сама перепись
// обязана быть переписью: форма имени и отсутствие дублей.
func TestRetiredRPCSurface_LedgerNamesAreWellFormedAndUnique(t *testing.T) {
	seen := map[string]struct{}{}
	for _, r := range retiredRPCSurface {
		if _, _, ok := splitFQN(r.FQN); !ok {
			t.Errorf("запись %q не имеет формы `<сервис>/<метод>`", r.FQN)
		}
		if strings.TrimSpace(r.Reason) == "" {
			t.Errorf("запись %q не несёт причины снятия — надгробие без надписи ничего не сообщает следующему", r.FQN)
		}
		if _, dup := seen[r.FQN]; dup {
			t.Errorf("запись %q задвоена", r.FQN)
		}
		seen[r.FQN] = struct{}{}
	}
	got := make([]string, 0, len(retiredRPCSurface))
	for _, r := range retiredRPCSurface {
		got = append(got, r.FQN)
	}
	if !sort.StringsAreSorted(got) {
		t.Log("перепись не отсортирована — это не ошибка, но затрудняет чтение диффа")
	}
}
