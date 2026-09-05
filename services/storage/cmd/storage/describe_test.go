// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// describe_test.go — дескриптор процесса собирается ЗДЕСЬ, и здесь же он обязан
// проходить конструктор.
//
// # Предмет
//
// Отказ конструктора дескриптора рантаймовый: сборка о нём не знает, прогон
// носителя тоже (корень сервиса вне его области). Значит поле, ставшее
// обязательным, ломает подъём сервиса, и сказать об этом некому — до самого
// развёртывания. Правило, которое этот файл исполняет: у дескриптора обязан быть
// потребитель в прогоне — тот же конструктор, который решает его судьбу на старте,
// и на том же конфиге, с каким процесс поднимается. Свойство «каждый
// композиционный корень проходит конструктор» держится не этим файлом, а гейтом по
// дереву (`internal/repohygiene`, TestEveryDescriptorHasAProbe).
//
// # Что сюда ПЕРЕЕХАЛО вместе со своим предметом
//
// Замки «боевая посадка отказывает на plaintext-DB / на невзведённом транспорте
// слушателей / на неназванном ребре решения о доступе» стояли в
// `internal/config/config_test.go` и судили собственную стражу storage. Стража эта
// сократилась до измерений, которых носитель не знает, а перечисленные измерения
// теперь судит конструктор дескриптора — один отказ на все сервисы. Замки поехали
// за предметом и в переезде стали строже: транспорт спрашивается у САМОГО
// ТРАНСПОРТА, а ребро решения о доступе обязано быть объявлено на ЛЮБОЙ посадке.

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"

	subscriptionv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"
	"github.com/PRO-Robotech/kacho/pkg/listnarrow/narrowtest"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/pkg/operations/operationspb"
	"github.com/PRO-Robotech/kacho/pkg/servicecontract"
	"github.com/PRO-Robotech/kacho/pkg/treecorpus"

	"github.com/PRO-Robotech/kacho/pkg/authz"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/disktype"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/disktypebinding"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/image"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/snapshot"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/storagebackend"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/volume"
	"github.com/PRO-Robotech/kacho/services/storage/internal/config"
	"github.com/PRO-Robotech/kacho/services/storage/internal/handler"
	storagepg "github.com/PRO-Robotech/kacho/services/storage/internal/repo/pg"
)

// discard — журнал пробы. Дескриптор его хранит, но ни одно утверждение здесь не
// зависит от вывода.
func discard() *slog.Logger { return slog.New(slog.NewTextHandler(&strings.Builder{}, nil)) }

// bootConfig — конфигурация, загруженная ТЕМ ЖЕ вызовом, что и на старте
// (`config.Load` из переменных окружения).
//
// Литерал `config.Config{…}` здесь был бы ДРУГОЙ величиной: он обошёл бы
// умолчания, а половина полей дескриптора приезжает именно из них (окно отзыва,
// срок вопроса о правах, бюджет отказов, граница обработки) — и проба утверждала бы
// про конфигурацию, которой не бывает ни на одной посадке.
func bootConfig(t *testing.T, env map[string]string) config.Config {
	t.Helper()
	base := map[string]string{
		"KACHO_STORAGE_DB_PASSWORD":                  "secret",
		"KACHO_STORAGE_AUTHZ_IAM_GRPC_ADDR":          "kaname-internal:9091",
		"KACHO_STORAGE_AUTHZ_TRUSTED_FORWARDER_SANS": gatewaySAN + "," + computeSAN,
		"KACHO_STORAGE_AUTH_MODE":                    "dev",
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

// describeWith собирает дескриптор на данной конфигурации с тем же сужателем, что
// уезжает в use-cases.
func describeWith(t *testing.T, cfg config.Config) (servicecontract.Descriptor, error) {
	t.Helper()
	log := discard()
	return describe(cfg, log, buildListFilter(cfg, nil, log), probeExistence{}, probeAuthzObserve, prometheus.NewRegistry())
}

// TestDescribeIsAcceptedByTheConstructor — ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ, и он первым:
// пока он красный, процесс не поднимается вовсе, а всякое отрицание ниже зеленеет
// по чужой причине.
func TestDescribeIsAcceptedByTheConstructor(t *testing.T) {
	desc, err := describeWith(t, bootConfig(t, nil))
	if err != nil {
		t.Fatalf("дескриптор storage отвергнут конструктором — процесс НЕ ПОДНИМЕТСЯ:\n%v", err)
	}
	if !desc.Accepted() {
		t.Fatal("дескриптор собран литералом в обход конструктора: носитель откажется по нему поднимать процесс")
	}

	// Перепись: утверждается не «принят», а «принят по НАЗВАННЫМ значениям» —
	// иначе проба зеленела бы и на дескрипторе, у которого оси отвалились вместе
	// со своими отказами.
	s := desc.Spec()
	if s.HandlingBudget <= 0 {
		t.Fatalf("верхняя граница обработки не объявлена (%v)", s.HandlingBudget)
	}
	if budget, ok := s.DenyBudget.Get(); !ok || budget <= 0 {
		t.Fatalf("бюджет отказов объявлен изъятием либо нулём (%v, ok=%v), а решение о доступе storage "+
			"принимает вопросом к kaname — шторм отказов есть кому ронять", budget, ok)
	}
	if !s.BootGate.Declared() {
		t.Fatal("ось загрузочного гейта не объявлена вовсе")
	}
	if s.CacheWindow <= 0 || s.ClientBudget <= 0 {
		t.Fatalf("окно отзыва (%v) и срок вопроса о правах (%v) обязаны быть выбраны, а не взяты "+
			"умолчанием библиотеки", s.CacheWindow, s.ClientBudget)
	}

	// Оси, у которых значение НЕПУСТО, — они и есть то, что storage делает с
	// моделью прав. Пустая клетка здесь читалась бы как «нечего эмитировать» и
	// прошла бы конструктор: пустое ЗНАЧЕНИЕ — законное объявление.
	emits, ok := s.Emits.Get()
	if !ok || len(emits) == 0 {
		t.Fatalf("ось эмиссии пуста (%v, ok=%v): storage пишет owner-tuple на каждый созданный "+
			"том, снимок и образ — пустая ось означала бы, что ресурсы остаются без владельца", emits, ok)
	}
	regs, ok := s.Registers.Get()
	if !ok || len(regs) != 3 {
		t.Fatalf("ось регистрируемых типов = %v (ok=%v), ожидались три типа storage", regs, ok)
	}
	narrowers, ok := s.Narrowers.Get()
	if !ok || narrowers[listAttachmentsMethod] == nil {
		t.Fatalf("проводки сужателя на %s нет: каталог объявляет метод сужаемым, и без проводки "+
			"за ним не остаётся рубежа вовсе — носитель откажет в старте (О3)", listAttachmentsMethod)
	}
	if d, ok := s.Delivery.Get(); !ok || !d.IsProven() {
		t.Fatalf("происхождение доставки = %v (ok=%v): намерение регистрации storage пишется в той же "+
			"writer-транзакции, что и ресурс, — это доказанное происхождение, и объявлено оно должно "+
			"быть им", d, ok)
	}

	t.Logf("дескриптор принят: граница обработки %v, окно отзыва %v, срок вопроса %v, "+
		"эмитируемых отношений %d, регистрируемых типов %d, проводок сужателя %d",
		s.HandlingBudget, s.CacheWindow, s.ClientBudget, len(emits), len(regs), len(narrowers))
}

// TestJournalOfThisProcessGoesToItsOwnLogger — журнал доступа storage существует и
// пишется ЖУРНАЛОМ ЭТОГО ПРОЦЕССА.
//
// # Что здесь на кону, и почему проба именно такая
//
// storage был последним сервисом дерева, который вёл собственный журнал доступа, и
// вёл его ОДНОСТОРОННЕ: звено стояло только в unary-цепочке, поэтому стрим-вызов не
// оставлял в журнале ни строки. Носитель ведёт журнал на ОБЕИХ полосах и ВТОРЫМ
// звеном — снаружи него стоит только измеритель задержки, а восстановление паники
// идёт следом (`измеритель задержки → журнал доступа → восстановление паники → …`),
// поэтому журнал видит и паниковавший вызов, который прежде оставался единственным
// ненаблюдаемым исходом.
//
// Само это свойство держат пробы носителя, а не storage: `TestAccessLogRecordsThePanickingCall`,
// `TestAccessLogRecordsAnOrdinaryCallToo`, `TestAccessLogRecordsAStreamCallToo`,
// `TestAccessLogRecordsThePanickingStreamCall` и
// `TestLatencyIsOutermostAccessLogNextAndDecisionIsLast` в `pkg/servicehost`.
// Дублировать их здесь значило бы завести седьмое место об одном предмете.
//
// От storage требуется единственное, чего носитель за него сделать не может, —
// принести СВОЙ журнал. Нулевое поле конструктор молча резолвит в журнал по
// умолчанию, и тогда строки доступа уехали бы мимо настроенного вывода процесса,
// а «журнал есть» и «журнал этого процесса» разошлись бы незаметно.
func TestJournalOfThisProcessGoesToItsOwnLogger(t *testing.T) {
	mine := discard()
	cfg := bootConfig(t, nil)
	desc, err := describe(cfg, mine, buildListFilter(cfg, nil, mine), probeExistence{}, probeAuthzObserve, prometheus.NewRegistry())
	if err != nil {
		t.Fatalf("дескриптор отвергнут: %v", err)
	}
	if desc.Spec().Logger != mine {
		t.Fatal("дескриптор несёт не тот журнал, который принёс процесс: строки доступа уедут " +
			"в журнал по умолчанию, и «журнал есть» перестанет означать «журнал этого процесса»")
	}
}

// TestDescribeProbeCanFail — контроль того, что проба выше СПОСОБНА упасть.
//
// Без него «дескриптор принят» неотличимо от «конструктор ничего не проверяет»:
// положительное утверждение, у которого нет отрицательного близнеца, зеленеет
// одинаково на исправном и на выключенном.
func TestDescribeProbeCanFail(t *testing.T) {
	// Ребро решения о доступе не названо — отказ О6, и он БЕЗУСЛОВНЫЙ: посадка
	// здесь dev, а требование объявить ребро от режима не зависит. Прежняя стража
	// storage требовала адрес только в боевом режиме — это и есть усиление,
	// приехавшее вместе с переездом.
	_, err := describeWith(t, bootConfig(t, map[string]string{"KACHO_STORAGE_AUTHZ_IAM_GRPC_ADDR": ""}))
	if err == nil {
		t.Fatal("дескриптор без ребра решения о доступе принят — конструктор не судит ничего, " +
			"и положительная проба выше вакуумна")
	}
	if !strings.Contains(err.Error(), "CheckEdge") {
		t.Fatalf("отказ не называет предмета — чинить по нему нечего:\n%v", err)
	}
}

// TestProductionRefusesInsecurePosture — ПЕРЕЕХАВШИЙ замок (#56): боевая посадка с
// plaintext-DB и невзведённым транспортом обоих слушателей ОБЯЗАНА отказать в
// старте.
//
// Прежде это судила `config.Validate` по ручкам `Enable`; теперь судит конструктор
// дескриптора по ОТВЕТУ САМОГО ТРАНСПОРТА. Разница несущая: сборщик креденшелов на
// невзведённой ручке отдаёт незашифрованные креды БЕЗ ошибки, поэтому процесс
// поднимался бы, отчитываясь «проверка прав включена», а каждый вызов уходил бы по
// открытому каналу.
func TestProductionRefusesInsecurePosture(t *testing.T) {
	cfg := bootConfig(t, map[string]string{
		"KACHO_STORAGE_AUTH_MODE":  "production",
		"KACHO_STORAGE_DB_SSLMODE": "disable",
		// Ручки транспорта слушателей не взводятся: сборщик отдаст
		// незашифрованные креды без ошибки — ровно тот случай, ради которого
		// отказ читает транспорт, а не ручку.
	})
	_, err := describeWith(t, cfg)
	if err == nil {
		t.Fatal("боевая посадка с plaintext-DB и невзведённым транспортом обоих слушателей принята")
	}
	msg := err.Error()
	for _, want := range []string{"DBSSLMode", "PublicCreds", "InternalCreds"} {
		if !strings.Contains(msg, want) {
			t.Errorf("отказ не называет измерение %q — оператор не узнает, что чинить:\n%s", want, msg)
		}
	}
}

// TestProductionRefusesWeakDBSSLMode — та же посадка, ослаблено РОВНО ОДНО
// измерение. Перечень допустимых значений закрыт: перечислять запрещённые значило
// бы пропускать всякое, которого в перечне нет, то есть любую опечатку.
func TestProductionRefusesWeakDBSSLMode(t *testing.T) {
	for _, mode := range []string{"disable", "", "allow", "prefer"} {
		t.Run("sslmode="+mode, func(t *testing.T) {
			cfg := bootConfig(t, map[string]string{
				"KACHO_STORAGE_AUTH_MODE":  "production",
				"KACHO_STORAGE_DB_SSLMODE": mode,
			})
			_, err := describeWith(t, cfg)
			if err == nil || !strings.Contains(err.Error(), "DBSSLMode") {
				t.Fatalf("боевая посадка с sslmode=%q принята либо отказ не называет измерения: %v", mode, err)
			}
		})
	}
	// Положительный близнец: то же измерение в законном значении отказа не даёт.
	// Без него отрицание выше зеленело бы и на конструкторе, отвергающем всё
	// подряд.
	cfg := bootConfig(t, map[string]string{
		"KACHO_STORAGE_AUTH_MODE":  "production",
		"KACHO_STORAGE_DB_SSLMODE": "verify-full",
	})
	_, err := describeWith(t, cfg)
	if err != nil && strings.Contains(err.Error(), "DBSSLMode") {
		t.Fatalf("законный sslmode=verify-full назван находкой: %v", err)
	}
}

// TestUnnarrowedForwarderCircleRefusesStart — круг отправителей чужой личности.
//
// Стража круга переехала в конструктор дескриптора и там срабатывает на ЛЮБОМ
// старте, а не только в боевом: контроль, чья ветка на локальном стенде не
// исполняется ни разу, находит «забыл выставить круг» только на боевом профиле, где
// цена ошибки максимальна.
//
// Что именно предотвращает этот отказ, показано ПОВЕДЕНИЕМ в
// trusted_forwarders_test.go: на несужённом круге переданная личность принимается
// от любого проверенного пира.
func TestUnnarrowedForwarderCircleRefusesStart(t *testing.T) {
	t.Run("пустой круг — отказ, и он называет ручку", func(t *testing.T) {
		_, err := describeWith(t, bootConfig(t, map[string]string{
			"KACHO_STORAGE_AUTHZ_TRUSTED_FORWARDER_SANS": "",
		}))
		if err == nil {
			t.Fatal("пустой круг отправителей принят: несужённый круг означает не «никому», " +
				"а «любому пиру с проверенным сертификатом»")
		}
		if !strings.Contains(err.Error(), "KACHO_STORAGE_AUTHZ_TRUSTED_FORWARDER_SANS") {
			t.Fatalf("отказ не называет ручку — стенд останется неподнятым и непонятным:\n%v", err)
		}
	})

	// Список из одних пустых записей кругом НЕ является: транспорт их отбрасывает,
	// и страж обязан читать ТОТ ЖЕ предикат. Иначе «сузили» и «сузили на самом
	// деле» разъезжаются молча, и `SANS=","` проходит гейт, возвращая дыру.
	t.Run("круг из одних пустых записей сужением не является", func(t *testing.T) {
		if _, err := describeWith(t, bootConfig(t, map[string]string{
			"KACHO_STORAGE_AUTHZ_TRUSTED_FORWARDER_SANS": ",  ,",
		})); err == nil {
			t.Fatal("круг из одних пустых записей принят как сужение — страж считает не то, что транспорт")
		}
	})

	// Боевая посадка не мягче: страже нельзя разъехаться по режимам.
	for _, mode := range []string{"production", "production-strict"} {
		t.Run(mode+": пустой круг — отказ", func(t *testing.T) {
			_, err := describeWith(t, bootConfig(t, map[string]string{
				"KACHO_STORAGE_AUTH_MODE":                    mode,
				"KACHO_STORAGE_DB_SSLMODE":                   "require",
				"KACHO_STORAGE_AUTHZ_TRUSTED_FORWARDER_SANS": "",
			}))
			if err == nil || !strings.Contains(err.Error(), "Forwarders") {
				t.Fatalf("%s с пустым кругом принят: %v", mode, err)
			}
		})
	}

	// Вне боевого режима опт-ин — ЯВНАЯ просьба, а не умолчание: он переводит
	// пустой круг из «так вышло» в «так попросили». Положительный контроль
	// обязателен: без него отрицания выше зеленели бы и на «отказывать всегда».
	t.Run("локальная фикстура с явным опт-ином поднимается", func(t *testing.T) {
		if _, err := describeWith(t, bootConfig(t, map[string]string{
			"KACHO_STORAGE_AUTHZ_TRUSTED_FORWARDER_SANS": "",
			"KACHO_STORAGE_AUTHZ_TRUST_ANY_FORWARDER":    "true",
		})); err != nil {
			t.Fatalf("локальная фикстура с ЯВНЫМ опт-ином не поднимается: %v", err)
		}
	})

	// И тот же опт-ин в БОЕВОМ режиме защиты не снимает — иначе это была бы
	// ручка, выключающая контроль на развёрнутом стенде.
	t.Run("в боевом режиме опт-ин защиты не снимает", func(t *testing.T) {
		_, err := describeWith(t, bootConfig(t, map[string]string{
			"KACHO_STORAGE_AUTH_MODE":                    "production",
			"KACHO_STORAGE_DB_SSLMODE":                   "require",
			"KACHO_STORAGE_AUTHZ_TRUSTED_FORWARDER_SANS": "",
			"KACHO_STORAGE_AUTHZ_TRUST_ANY_FORWARDER":    "true",
		}))
		if err == nil || !strings.Contains(err.Error(), "Forwarders") {
			t.Fatalf("боевая посадка с испрошенным опт-ином на пустом круге принята: %v", err)
		}
	})
}

// TestStorageBringsNoBootGateYet — САМОИСТЕЧЕНИЕ изъятия по загрузочному гейту.
//
// Дескриптор объявляет гейт мутаций неприменимым, и причина — «решение отвергать
// создание до подъёма очереди регистраций не принято». Утверждение проверяемое, и
// предикат его снятия ВНЕШНИЙ: состояние дерева, а не память автора. Появится в
// дереве storage первый вызов `bootgate.*` — проба покраснеет и назовёт файл,
// потребовав принести гейт в дескриптор либо переписать причину.
//
// Читается ИСПОЛНЯЕМАЯ часть (разбор AST), а не текст: имя пакета стоит в
// комментарии самого изъятия, и текстовый предикат краснел бы на собственном
// объяснении. Состав дерева берётся у git, а не с диска: вердикт обязан быть
// свойством коммита, а не рабочего каталога с чужими распаковками.
func TestStorageBringsNoBootGateYet(t *testing.T) {
	const bootGatePkg = "github.com/PRO-Robotech/kacho/pkg/outbox/bootgate"

	root, err := filepath.Abs("../..") // services/storage
	if err != nil {
		t.Fatalf("абсолютный путь дерева сервиса: %v", err)
	}
	files, err := treecorpus.UnderWithSuffix(root, ".go")
	if err != nil {
		t.Fatalf("предпосылка пробы нарушена: состав дерева %s не читается: %v", root, err)
	}

	read := 0
	var carriers []string
	fset := token.NewFileSet()
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, perr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if perr != nil {
			t.Fatalf("разбор %s: %v", path, perr)
		}
		read++
		local := ""
		for _, imp := range f.Imports {
			if imp.Path.Value != `"`+bootGatePkg+`"` {
				continue
			}
			local = "bootgate"
			if imp.Name != nil {
				local = imp.Name.Name
			}
		}
		if local == "" {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if id, isIdent := sel.X.(*ast.Ident); isIdent && id.Name == local {
				rel, _ := filepath.Rel(root, path)
				carriers = append(carriers, fmt.Sprintf("%s:%d", rel, fset.Position(sel.Pos()).Line))
				return false
			}
			return true
		})
	}

	if read == 0 {
		t.Fatal("предпосылка пробы нарушена: не прочитано ни одного не-тестового файла дерева " +
			"storage — «ноль носителей» здесь означало бы «ноль прочитанного»")
	}
	t.Logf("осмотрено не-тестовых файлов дерева storage: %d, вызовов загрузочного гейта: %d",
		read, len(carriers))

	if len(carriers) > 0 {
		t.Fatalf("storage несёт загрузочный гейт мутаций (%s), а дескриптор объявляет его "+
			"НЕПРИМЕНИМЫМ.\nИзъятие пережило свой предмет: принесите гейт в describe() полем "+
			"BootGate либо перепишите причину — оставленное, оно означает, что провязанный гейт "+
			"никто не спрашивает", strings.Join(carriers, ", "))
	}
}

// registrarsOfBothListeners — регистраторы ОБОИХ слушателей, собранные так же,
// как их собирает `serve`.
//
// Use-cases собираются с нулевыми портами: ни один обработчик здесь не
// вызывается, предмет — только СОСТАВ зарегистрированного. Место одно на весь
// пакет намеренно: копия этой сборки в соседней пробе разошлась бы с первой
// молча — ровно там, где расхождение не видно, потому что обе продолжали бы
// возвращать непустой набор.
func registrarsOfBothListeners(t *testing.T) []func(grpc.ServiceRegistrar) {
	t.Helper()
	volumeUC := volume.New(nil, nil, nil, nil, nil, nil)
	snapshotUC := snapshot.New(nil, nil, nil, nil)
	imageUC := image.New(nil, nil, nil, nil, nil, nil)
	diskTypeUC := disktype.New(nil)
	opHandler := operationspb.NewHandler(operations.NewRepo(nil, "kacho_storage"))
	// Чтение квот — с НЕнулевым обработчиком: на поднятом стенде оно
	// зарегистрировано, и перепись обслуживаемого обязана описывать стенд, а не
	// вырожденную сборку. Нулевой указатель здесь молча вывел бы сервис из-под
	// каждой пробы, которая выводит поверхность из этой сборки.
	quotaHandler := handler.NewQuotaHandler(nil)

	return []func(grpc.ServiceRegistrar){
		func(r grpc.ServiceRegistrar) {
			registerPublic(r, volumeUC, snapshotUC, imageUC, diskTypeUC, quotaHandler, opHandler)
		},
		func(r grpc.ServiceRegistrar) {
			registerInternal(r, volumeUC, imageUC, diskTypeUC,
				storagebackend.New(nil), disktypebinding.New(nil, nil), opHandler,
				probeSubscriptionServer(t))
		},
	}
}

// probeSubscriptionServer — сервер потока, собранный БОЕВЫМ конструктором корня.
//
// Не заглушка: перепись обслуживаемого обязана описывать стенд, а нулевой
// указатель молча вывел бы подписку из-под каждой пробы, которая выводит
// поверхность из этой сборки. Соединения конструктор не открывает — он судит
// объявление, — поэтому строка соединения здесь законна и до базы дело не идёт.
func probeSubscriptionServer(t *testing.T) subscriptionv1.InternalSubscriptionServiceServer {
	t.Helper()
	cfg := config.Config{
		SubscriptionMaxStreams:   4,
		SubscriptionStreamBudget: time.Hour,
		SubscriptionIdlePoll:     2 * time.Second,
	}
	srv, err := buildSubscriptionServer(cfg, narrowtest.AllowingAll(),
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("сервер потока не собрался боевым конструктором: %v", err)
	}
	return srv
}

// TestStorageServesExactlyTheSubscriptionStream — служимый серверный стрим ОДИН,
// и это подписка; дескриптор накрывает его величиной.
//
// # Здесь стояла проба ОБРАТНОГО утверждения, и она заменена, а не ослаблена
//
// До провязки подписки (`#1414`) дескриптор объявлял ось `StreamBudget`
// НЕПРИМЕНИМОЙ с причиной «серверных стримов storage не служит», а эта проба
// стерегла самоистечение изъятия: появится первый стрим — покраснеет и назовёт
// метод. Изъятие истекло ровно так, как задумано. Ослабить пробу (снять
// утверждение о стримах) значило бы снять наблюдение за осью целиком; поэтому она
// утверждает ту же ось с ДРУГОЙ стороны.
//
// Утверждается ПАРА, и обе половины нужны:
//
//	стрим ровно один и это подписка — второй серверный стрим есть второй язык
//	                                  потока у того же сервиса;
//	величина объявлена               — «не применимо» при служимом стриме
//	                                  означало бы поток без срока жизни.
//
// Источник признака назван честно: здесь читается самоописание сервера
// (`grpc.ServiceInfo`), носитель на старте читает дескриптор метода. Оба
// порождены одним `.proto`; расхождение между ними было бы дефектом генерации.
func TestStorageServesExactlyTheSubscriptionStream(t *testing.T) {
	methods, streams := 0, []string{}
	for _, reg := range registrarsOfBothListeners(t) {
		srv := grpc.NewServer()
		reg(srv)
		for name, info := range srv.GetServiceInfo() {
			for _, m := range info.Methods {
				methods++
				if m.IsServerStream {
					streams = append(streams, "/"+name+"/"+m.Name)
				}
			}
		}
	}
	if methods == 0 {
		t.Fatal("ни один метод не зарегистрирован — утверждение о составе стримов было бы " +
			"верно и на пустом наборе, то есть проба не отличала бы исправное от сломанного")
	}
	const subscribeVerb = "/kacho.cloud.subscription.InternalSubscriptionService/Subscribe"
	if len(streams) != 1 || streams[0] != subscribeVerb {
		t.Fatalf("служимые серверные стримы: %v; ожидался ровно один — %s.\nВторой стрим "+
			"означал бы второй язык потока у одного сервиса; ноль — что подписка не "+
			"зарегистрирована, а дескриптор объявил ей срок жизни", streams, subscribeVerb)
	}
	// Вторая сторона той же оси, и на СВОЁМ дескрипторе: срок жизни объявлен
	// величиной. Носитель откажет в несоответствии и сам (О11), но на СТАРТЕ
	// ПРОЦЕССА — то есть при развёртывании; проба переносит отказ в прогон.
	desc, err := describeWith(t, bootConfig(t, nil))
	if err != nil {
		t.Fatalf("дескриптор отвергнут: %v", err)
	}
	budget, ok := desc.Spec().StreamBudget.Get()
	if !ok {
		t.Fatalf("дескриптор объявляет срок жизни подписки НЕПРИМЕНИМЫМ, а служимый стрим %s "+
			"есть: поток жил бы без срока, и обрыв перестал бы быть штатным событием", subscribeVerb)
	}
	if budget <= 0 {
		t.Fatalf("срок жизни подписки объявлен величиной %v", budget)
	}
	t.Logf("осмотрено служимых методов: %d, серверных стримов среди них: %d (%v), "+
		"срок жизни потока: %v", methods, len(streams), streams, budget)
}

// probeExistence — порт сверки существования для проб композиционного корня.
//
// Отвечает «объекта нет» на всё: предмет этих проб — отказы старта, которые
// носитель считает ДО первого соединения, и до вопроса к базе дело не доходит.
// Настоящая проба живёт на пуле (`internal/repo/pg`), и подменять её здесь
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
// прав сервиса (задача продукта #1931).
func (probeExistence) ProbeableTypes() []string {
	return (&storagepg.ExistenceProbe{}).ProbeableTypes()
}

// probeAuthzObserve — приёмник величин кеша вердиктов для проб КОНСТРУКТОРА.
//
// Заглушка здесь законна: предмет этих проб — что судит конструктор дескриптора,
// а не куда уезжают величины. Настоящий приёмник, чей вызов носителем
// утверждается, стоит в пробе подъёма (`carrier_start_test.go`): там его пропажа
// красит пробу, здесь — не может по построению.
func probeAuthzObserve(func() authz.Metrics) {}
