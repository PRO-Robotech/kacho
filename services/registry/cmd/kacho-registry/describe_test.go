// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// describe_test.go — дескриптор процесса собирается ЗДЕСЬ, и здесь же он обязан
// проходить конструктор.
//
// # Предмет
//
// Отказ конструктора дескриптора РАНТАЙМОВЫЙ: сборка его не видит, прогон
// носителя тоже (композиционный корень сервиса вне его области). Обязательное
// поле, которого корень не объявил, ломало бы подъём сервиса молча — и узнать об
// этом было бы неоткуда, кроме поднятого стенда.
//
// Поэтому у дескриптора обязан быть потребитель в прогоне: тот же конструктор,
// который решает его судьбу на старте, и на той же конфигурации, с какой
// поднимается процесс. Свойство «каждый композиционный корень проходит
// конструктор» держится не этим файлом, а гейтом по дереву
// (`internal/repohygiene`, TestEveryDescriptorHasAProbe): один файл закрывает
// один сервис, гейт — тех, кого ещё не написали.
//
// # Что здесь ещё утверждается
//
// Оси, которые носитель СВЕРЯЕТ с каталогом прав (сужатели и формы сокрытия),
// закрепляются не перечнем из памяти автора, а тем же выводом из аннотаций,
// каким их читает носитель. Появится в каталоге одиннадцатая сужаемая строка или
// второй скрывающий тип — эти пробы покраснеют и назовут метод, а не оставят
// расхождение до первого поднятого стенда.

import (
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"go/parser"
	"go/token"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/PRO-Robotech/kacho/pkg/authz"
	"github.com/PRO-Robotech/kacho/pkg/authz/catalogderive"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/pkg/operations/operationspb"
	"github.com/PRO-Robotech/kacho/pkg/servicecontract"
	"github.com/PRO-Robotech/kacho/pkg/servicehost"

	"github.com/PRO-Robotech/kacho/services/registry/internal/apps/kacho/config"
	"github.com/PRO-Robotech/kacho/services/registry/internal/check"
	"github.com/PRO-Robotech/kacho/services/registry/internal/handler"
	"github.com/PRO-Robotech/kacho/services/registry/internal/repo/kacho/pg"
)

// bootConfig — конфигурация, загруженная ТЕМ ЖЕ вызовом, что и на старте
// (`config.Load` из переменных окружения).
//
// Литерал `config.Config{…}` здесь был бы ДРУГОЙ величиной: он обошёл бы
// умолчания, а половина полей дескриптора приезжает именно из них (граница
// обработки, бюджет отказов, окно кэша) — и проба утверждала бы про
// конфигурацию, которой не бывает ни на одной посадке.
func bootConfig(t *testing.T, env map[string]string) config.Config {
	t.Helper()
	base := map[string]string{
		"KACHO_REGISTRY_DB_PASSWORD":                  "secret",
		"KACHO_REGISTRY_AUTHZ_IAM_GRPC_ADDR":          "kaname-internal.kacho.svc:9091",
		"KACHO_REGISTRY_AUTHZ_TRUSTED_FORWARDER_SANS": gatewaySAN,
		// Домен доверия — величина установки: без неё дескриптор не принимается,
		// потому что процесс, не назвавший домена, своим не признаёт никого.
		"KACHO_REGISTRY_AUTHZ_TRUST_DOMAIN": "kacho.cloud",
		// dev, потому что боевая посадка требует ФАЙЛОВ сертификатов на обоих
		// слушателях: собрать транспорт из несуществующих путей нельзя, а
		// подставлять сюда самодельные — значит проверять свою фикстуру. Всё, что
		// зависит от боевого режима, судится отдельно: круг отправителей —
		// `cfg.Validate()` в trusted_forwarders_test.go, mTLS обоих слушателей в
		// ЛЮБОМ режиме — `validateSecurityConfig` в serve_test.go.
		"KACHO_REGISTRY_AUTH_MODE": "dev",
	}
	for k, v := range env {
		base[k] = v
	}
	for k, v := range base {
		t.Setenv(k, v)
	}
	c, err := config.Load()
	if err != nil {
		t.Fatalf("конфигурация не загрузилась: %v", err)
	}
	return c
}

// probePorts — порты в тех же типах, какие приносит композиционный корень.
//
// Без живых соединений: конструктор дескриптора судит их ПРИСУТСТВИЕ (порт
// сверки существования обязателен, потому что объявлено сокрытие; проводка
// сужателя обязана быть непустой на каждом сужаемом методе), а не то, что они
// отвечают. Подставлять сюда свои заглушки было бы фикстурой снисходительнее
// продукта — берём ровно те конструкторы, что зовёт корень.
func probePorts() servePorts {
	return servePorts{
		existence:    pg.NewExistenceProbe(nil),
		narrower:     check.NewIAMCheckClient(nil).Narrower(),
		authzObserve: func(func() authz.Metrics) {},
		// Свой реестр на КАЖДЫЙ вызов: общий на пакет связал бы пробы через
		// состояние процесса, и вторая падала бы на «серия уже зарегистрирована».
		metricsReg: prometheus.NewRegistry(),
	}
}

func probeLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(&strings.Builder{}, nil))
}

// probeMode — посадка фикстуры. Разбирается тем же местом, что и в
// композиционном корне: там она уезжает и в профиль не-gRPC поверхностей, и в
// дескриптор процесса, поэтому проба обязана ходить через тот же разбор, а не
// подставлять константу.
func probeMode(t *testing.T, cfg config.Config) servicecontract.Mode {
	t.Helper()
	mode, err := servicecontract.ParseMode(cfg.AuthMode)
	if err != nil {
		t.Fatalf("посадка фикстуры не разобралась: %v", err)
	}
	return mode
}

// TestDescribeIsAcceptedByTheConstructor — ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ, и он первым:
// пока он красный, процесс не поднимается вовсе, а всякое отрицание ниже
// зеленеет по чужой причине.
func TestDescribeIsAcceptedByTheConstructor(t *testing.T) {
	desc, err := describe(bootConfig(t, nil), probeMode(t, bootConfig(t, nil)), probeLogger(), probePorts())
	if err != nil {
		t.Fatalf("дескриптор kacho-registry отвергнут конструктором — процесс НЕ ПОДНИМЕТСЯ:\n%v", err)
	}
	if !desc.Accepted() {
		t.Fatal("дескриптор собран литералом в обход конструктора: носитель откажется по нему поднимать процесс")
	}
	// Перепись: три оси, ставшие обязательными последними, объявлены ЯВНО, а не
	// проскочили нулевым значением. Утверждается не «принят», а «принят по
	// названным причинам».
	s := desc.Spec()
	if s.HandlingBudget <= 0 {
		t.Fatalf("верхняя граница обработки не объявлена (%v)", s.HandlingBudget)
	}
	if budget, ok := s.DenyBudget.Get(); !ok || budget <= 0 {
		t.Fatalf("бюджет отказов объявлен изъятием либо нулём (%v, ok=%v), а решение о доступе "+
			"реестр принимает вопросом к соседу — шторм отказов есть кому ронять", budget, ok)
	}
	if !s.BootGate.Declared() {
		t.Fatal("ось загрузочного гейта не объявлена вовсе")
	}
	if s.CacheWindow <= 0 {
		t.Fatalf("окно кэша вердиктов не объявлено (%v)", s.CacheWindow)
	}
	if s.ClientBudget <= 0 {
		t.Fatalf("срок вопроса владельцу модели не объявлен (%v)", s.ClientBudget)
	}
	t.Logf("дескриптор принят: граница обработки %v, окно отзыва %v, срок вопроса %v, "+
		"бюджет отказов объявлен, гейт мутаций объявлен", s.HandlingBudget, s.CacheWindow, s.ClientBudget)
}

// TestDescribeProbeCanFail — контроль того, что проба выше СПОСОБНА упасть.
//
// Без него «дескриптор принят» неотличимо от «конструктор ничего не проверяет»:
// положительное утверждение, у которого нет отрицательного близнеца, зеленеет
// одинаково на исправном и на выключенном.
func TestDescribeProbeCanFail(t *testing.T) {
	t.Run("ребро решения о доступе не названо", func(t *testing.T) {
		cfg := bootConfig(t, map[string]string{"KACHO_REGISTRY_AUTHZ_IAM_GRPC_ADDR": ""})
		_, err := describe(cfg, probeMode(t, cfg), probeLogger(), probePorts())
		if err == nil {
			t.Fatal("дескриптор без ребра решения о доступе принят — конструктор не судит ничего, " +
				"и положительная проба выше вакуумна")
		}
		if !strings.Contains(err.Error(), "CheckEdge") {
			t.Fatalf("отказ не называет предмета — чинить по нему нечего:\n%v", err)
		}
	})
	t.Run("граница обработки снята", func(t *testing.T) {
		cfg := bootConfig(t, map[string]string{"KACHO_REGISTRY_HANDLING_BUDGET": "0s"})
		_, err := describe(cfg, probeMode(t, cfg), probeLogger(), probePorts())
		if err == nil {
			t.Fatal("дескриптор без верхней границы обработки принят: вызов без срока держал бы " +
				"соединение из ограниченного пула сколько угодно")
		}
		if !strings.Contains(err.Error(), "HandlingBudget") {
			t.Fatalf("отказ не называет предмета:\n%v", err)
		}
	})
	t.Run("порт сверки существования не принесён", func(t *testing.T) {
		ports := probePorts()
		ports.existence = nil
		_, err := describe(bootConfig(t, nil), probeMode(t, bootConfig(t, nil)), probeLogger(), ports)
		if err == nil {
			t.Fatal("дескриптор со скрытием существования и без порта сверки принят: отказ на " +
				"существующем чужом реестре пришёл бы формой, отличимой от промаха владельца")
		}
		if !strings.Contains(err.Error(), "Existence") {
			t.Fatalf("отказ не называет предмета:\n%v", err)
		}
	})
}

// ── оси, которые носитель сверяет с каталогом прав ──────────────────────────

// servedMethods — что процесс РЕАЛЬНО служит, снятое у самих серверов после
// регистрации, а не выписанное списком.
func servedMethods(t *testing.T) []string {
	t.Helper()
	registryHandler := handler.NewRegistryHandler(nil, nil, 0)
	internalHandler := handler.NewInternalRegistryHandler(nil)
	opHandler := operationspb.NewHandler(operations.NewRepo(nil, "kacho_registry"))

	var served []string
	for _, reg := range []func(grpc.ServiceRegistrar){
		func(r grpc.ServiceRegistrar) {
			registerPublic(r, registryHandler, handler.NewQuotaHandler(nil), opHandler)
		},
		func(r grpc.ServiceRegistrar) {
			registerInternal(r, internalHandler, opHandler, stubSubscriptionServer{})
		},
	} {
		srv := grpc.NewServer()
		reg(srv)
		for name, info := range srv.GetServiceInfo() {
			for _, m := range info.Methods {
				served = append(served, "/"+name+"/"+m.Name)
			}
		}
	}
	sort.Strings(served)
	if len(served) == 0 {
		t.Fatal("ни один метод не зарегистрирован — всякое утверждение ниже было бы верно " +
			"и на пустом наборе, то есть не отличало бы исправное от сломанного")
	}
	return served
}

// domainsOfServed — proto-пакеты служимого набора, ВЫВЕДЕННЫЕ из имён (тем же
// правилом, каким их выводит носитель), минус платформенные регистрации
// конструктора сервера, у которых домена Kachō нет.
func domainsOfServed(t *testing.T, served []string) []string {
	t.Helper()
	platform := map[string]bool{
		"grpc.health.v1.Health":                    true,
		"grpc.reflection.v1.ServerReflection":      true,
		"grpc.reflection.v1alpha.ServerReflection": true,
	}
	seen := map[string]bool{}
	var out []string
	for _, m := range served {
		svc, _, _ := strings.Cut(strings.TrimPrefix(m, "/"), "/")
		if platform[svc] {
			continue
		}
		pkg := svc[:strings.LastIndex(svc, ".")]
		if !seen[pkg] {
			seen[pkg] = true
			out = append(out, pkg)
		}
	}
	sort.Strings(out)
	return out
}

// TestNarrowersMatchTheCatalogInBothDirections — О3/О4 на дереве реестра, до
// подъёма процесса.
//
// Носитель сверяет проводку сужателя с каталогом прав в обе стороны и роняет
// старт на расхождении. Проба повторяет ту же сверку ЗДЕСЬ — не ради дубля, а
// ради момента: отказ носителя рантаймовый, и без неё одиннадцатая сужаемая
// строка каталога стала бы известна только на поднятом стенде.
//
// Перечень ожидаемого не выписан: он ВЫВОДИТСЯ из аннотаций тех же дескрипторов,
// из которых его читает носитель.
func TestNarrowersMatchTheCatalogInBothDirections(t *testing.T) {
	served := servedMethods(t)
	inServed := map[string]bool{}
	for _, m := range served {
		inServed[m] = true
	}

	declared := map[string]bool{}
	catalogderive.RangeAnnotated(domainsOfServed(t, served),
		func(fullMethod string, _ protoreflect.MethodDescriptor, a catalogderive.Annotations) {
			if a.ScopeFiltered && inServed[fullMethod] {
				declared[fullMethod] = true
			}
		})
	if len(declared) == 0 {
		t.Fatal("каталог не объявил сужаемым НИ ОДНОГО служимого метода — предпосылка пробы " +
			"исчезла, и «расхождений нет» означало бы «ничего не прочитано»")
	}

	desc, err := describe(bootConfig(t, nil), probeMode(t, bootConfig(t, nil)), probeLogger(), probePorts())
	if err != nil {
		t.Fatalf("дескриптор отвергнут: %v", err)
	}
	wired, ok := desc.Spec().Narrowers.Get()
	if !ok {
		t.Fatal("ось сужателей объявлена неприменимой, а каталог называет сужаемые методы: " +
			"за такими методами пообъектной проверки на крае нет вовсе, поэтому отсутствующая " +
			"проводка означает не «строже», а «без рубежа»")
	}

	var missing, extra []string
	for m := range declared {
		if n, has := wired[servicecontract.MethodFQN(m)]; !has || n == nil {
			missing = append(missing, m)
		}
	}
	for m, n := range wired {
		if n == nil {
			missing = append(missing, string(m)+" (проводка объявлена пустой)")
			continue
		}
		if !declared[string(m)] {
			extra = append(extra, string(m))
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 {
		t.Errorf("каталог объявляет метод сужаемым, а дескриптор проводки на него не несёт (%d):\n  %s",
			len(missing), strings.Join(missing, "\n  "))
	}
	if len(extra) > 0 {
		t.Errorf("дескриптор несёт проводку на метод, которого каталог сужаемым не объявлял (%d) — "+
			"либо метод перестал быть сужаемым и проводка пережила свой предмет, либо строку каталога "+
			"потеряли:\n  %s", len(extra), strings.Join(extra, "\n  "))
	}
	t.Logf("осмотрено служимых методов: %d, сужаемых по каталогу: %d, проводок в дескрипторе: %d",
		len(served), len(declared), len(wired))
}

// TestHideExistenceFormsSpeakInTheOwnersVoice — О5 на дереве реестра.
//
// Скрытие существования работает ровно настолько, насколько текст отказа
// неотличим от настоящего промаха владельца. Объявленная форма поэтому сверяется
// не с ожиданием автора, а с ТОЙ, которой реально ответит звено решения о
// доступе (`authz.OwnerNotFoundFormat`) — и множество скрывающих типов берётся из
// каталога, а не из памяти.
func TestHideExistenceFormsSpeakInTheOwnersVoice(t *testing.T) {
	served := servedMethods(t)
	inServed := map[string]bool{}
	for _, m := range served {
		inServed[m] = true
	}

	hidden := map[string]bool{}
	catalogderive.RangeAnnotated(domainsOfServed(t, served),
		func(fullMethod string, _ protoreflect.MethodDescriptor, a catalogderive.Annotations) {
			if a.HideExistence && a.ScopeObjectType != "" && inServed[fullMethod] {
				hidden[a.ScopeObjectType] = true
			}
		})
	if len(hidden) == 0 {
		t.Fatal("каталог не называет скрывающим НИ ОДИН тип служимых методов — предпосылка пробы исчезла")
	}

	desc, err := describe(bootConfig(t, nil), probeMode(t, bootConfig(t, nil)), probeLogger(), probePorts())
	if err != nil {
		t.Fatalf("дескриптор отвергнут: %v", err)
	}
	forms, ok := desc.Spec().HideExistence.Get()
	if !ok {
		t.Fatal("ось скрытия объявлена неприменимой, а каталог называет скрывающие типы: " +
			"ресурс ответил бы своей, отличимой формой — то есть оракулом существования")
	}

	for ot := range hidden {
		form, has := forms[servicecontract.ObjectType(ot)]
		if !has {
			t.Errorf("тип %s каталог называет скрывающим, а формы отказа для него дескриптор не несёт", ot)
			continue
		}
		owner, known := authz.OwnerNotFoundFormat(ot)
		if !known {
			t.Errorf("у типа %s нет голоса владельца: звено ответило бы нейтральным «not found» — "+
				"текстом, не похожим ни на один ответ владельца", ot)
			continue
		}
		if string(form) != owner {
			t.Errorf("объявленная форма %q расходится с той, которой ответит звено решения о доступе (%q); "+
				"отвечает вторая", string(form), owner)
		}
	}
	for ot := range forms {
		if !hidden[string(ot)] {
			t.Errorf("объявлена форма отказа для типа %s, который каталог скрывающим не называет: "+
				"запись пережила свой предмет", string(ot))
		}
	}
	t.Logf("осмотрено скрывающих типов по каталогу: %d, форм в дескрипторе: %d", len(hidden), len(forms))
}

// TestRegistryServesNoGatedMutationBeyondItsOwn — предпосылка изъятия по
// загрузочному гейту с той стороны, где она СЕГОДНЯ верна не по построению.
//
// Реестр служит гейтируемую мутацию (`RegistryService/Create`), поэтому изъятие
// обосновано НЕ отсутствием предмета, а отсутствием самого гейта в дереве.
// Проба спрашивает тем же предикатом, которым гейт исполняется, и печатает
// найденное: перечень гейтируемых мутаций обязан быть ВИДЕН тому, кто однажды
// решит гейт завести.
func TestRegistryServesNoGatedMutationBeyondItsOwn(t *testing.T) {
	served := servedMethods(t)
	var gated []string
	for _, m := range served {
		if servicehost.IsGatedMutation(m) {
			gated = append(gated, m)
		}
	}
	sort.Strings(gated)
	t.Logf("осмотрено служимых методов: %d, гейтируемых мутаций среди них: %d (%s)",
		len(served), len(gated), strings.Join(gated, ", "))
	if len(gated) == 0 {
		t.Fatal("гейтируемых мутаций не найдено, а изъятие BootGate в describe() обосновано " +
			"ОТСУТСТВИЕМ ГЕЙТА, а не отсутствием предмета. Ноль здесь означает, что предикат " +
			"перестал читать служимый набор, либо у реестра больше нет тенантского Create — " +
			"в обоих случаях причину изъятия надо переписать, а не оставить")
	}
}

// TestRegistryBringsNoBootGateYet — САМОИСТЕЧЕНИЕ изъятия по загрузочному гейту.
//
// Изъятие сказано так: «гейта у реестра в дереве нет». Это утверждение о ДЕРЕВЕ,
// и проверяется оно деревом: композиционный корень не импортирует пакет гейта.
// Появится импорт — проба покраснеет и потребует объявить гейт величиной, а не
// изъятием. Предикат внешний: состояние дерева, а не память автора.
func TestRegistryBringsNoBootGateYet(t *testing.T) {
	const gatePkg = "github.com/PRO-Robotech/kacho/pkg/outbox/bootgate"

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("каталог композиционного корня не читается: %v", err)
	}
	fset := token.NewFileSet()
	var read int
	var importers []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, perr := parser.ParseFile(fset, filepath.Join(".", name), nil, parser.ImportsOnly)
		if perr != nil {
			t.Fatalf("%s не разбирается: %v", name, perr)
		}
		read++
		for _, imp := range f.Imports {
			if strings.Trim(imp.Path.Value, `"`) == gatePkg {
				importers = append(importers, name)
			}
		}
	}
	if read == 0 {
		t.Fatal("не прочитано ни одного не-тестового файла корня — «импорта нет» здесь означало бы " +
			"«ничего не прочитано», а не исправное дерево")
	}
	t.Logf("осмотрено не-тестовых файлов композиционного корня: %d", read)
	if len(importers) > 0 {
		t.Fatalf("композиционный корень импортирует пакет загрузочного гейта (%s), а дескриптор "+
			"объявляет ось BootGate ИЗЪЯТИЕМ с причиной «гейта в дереве нет». Заявление истекло: "+
			"объявите гейт величиной через servicecontract.Value", strings.Join(importers, ", "))
	}
}
