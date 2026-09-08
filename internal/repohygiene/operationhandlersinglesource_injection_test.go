// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Доказательство того, что гейт единственного источника арендаторской полосы
// операции СПОСОБЕН упасть — и что падает он на существе, а не на форме.
//
// Все пробы гоняют ТУ ЖЕ функцию, что и гейт по дереву
// (`auditOperationSingleSource`): проба, повторяющая логику гейта своей копией,
// доказывала бы свойство копии.
//
// ЧЕТЫРЕ ПЕРВЫХ ПРОБЫ — ЭТО ЧЕТЫРЕ ОБХОДА, КОТОРЫЕ ВНЕШНИЙ АУДИТ ПРОВЁЛ ЧЕРЕЗ
// ПРЕЖНЮЮ РЕДАКЦИЮ. Она искала три литерала (`func NewOperationHandler(`,
// `func [Oo]perationToProto(`, `operationpb.`) и на каждом из четырёх форков
// печатала «конструкторов 0, преобразователей 0» и выходила зелёной. Здесь они
// закреплены поимённо: гейт, который однажды обошли, обязан нести пробу на этот
// обход, иначе следующая переделка вернёт его молча.
//
// ОСИ РАЗВЕДЕНЫ, и у каждого отрицания есть парный положительный контроль:
// «находок ноль» само по себе неотличимо от «распознаватель ослеп».
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// opInjectAudit — прогон судящей функции по синтетическому дереву.
func opInjectAudit(t *testing.T, files map[string]string) ([]opSourceFinding, opSourceCensus) {
	t.Helper()
	dir := t.TempDir()
	var rels []string
	for rel, src := range files {
		abs := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
		rels = append(rels, rel)
	}
	f, c, err := auditOperationSingleSource(dir, rels, nil)
	if err != nil {
		t.Fatalf("обход синтетики: %v", err)
	}
	return f, c
}

const opImports = `package handler

import (
	%s "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/pkg/operations/operationspb"
)

var _ = operationspb.ToProto
`

func opSrc(alias, body string) string {
	return strings.Replace(opImports, "%s", alias, 1) + body
}

// ─── ОБХОД A: чужой алиас стабов ─────────────────────────────────────────────

// Алиасный обход не выдумка: стабы контракта импортируются в дереве под
// разными именами, и распознаватель, прибитый к одному литералу, не видит
// остальных. Число форм здесь не выписано — оно зависит от популяции и
// печатается переписью гейта на каждом прогоне.
func TestInjectionAliasDoesNotHideTheSubject(t *testing.T) {
	for _, alias := range []string{"operationpb", "oppb", "operationv1", "opv1"} {
		t.Run(alias, func(t *testing.T) {
			src := opSrc(alias, `
type OpsHandler struct{ repo operations.Repo }

func NewOpsHandler(repo operations.Repo) *OpsHandler { return &OpsHandler{repo: repo} }

func (h *OpsHandler) Get(c context.Context, in *`+alias+`.GetOperationRequest) (*`+alias+`.Operation, error) {
	return nil, nil
}
`)
			f, cen := opInjectAudit(t, map[string]string{"services/x/internal/handler/h.go": src})
			if len(f) == 0 {
				t.Fatalf("форк с алиасом %q не пойман — распознаватель прибит к литералу, "+
					"и обходится переименованием импорта. перепись: %+v", alias, cen)
			}
			if !strings.Contains(f[0].What, "запрос контракта") {
				t.Fatalf("находка называет не тот предмет: %q", f[0].What)
			}
		})
	}
}

// ─── ОБХОД D: чужие имена параметров ─────────────────────────────────────────

// Прежняя редакция требовала дословных `ctx`/`req`. Имена параметров — привычка,
// а не контракт.
func TestInjectionParameterNamesDoNotHideTheSubject(t *testing.T) {
	src := opSrc("operationpb", `
type H struct{}

func (h *H) Cancel(whatever context.Context, anything *operationpb.CancelOperationRequest) (*operationpb.Operation, error) {
	return nil, nil
}
`)
	f, cen := opInjectAudit(t, map[string]string{"services/x/internal/handler/h.go": src})
	if len(f) == 0 {
		t.Fatalf("форк с чужими именами параметров не пойман: %+v", cen)
	}
}

// ─── ОБХОД B1: чужое имя преобразователя ─────────────────────────────────────

// Прежняя редакция искала `func [Oo]perationToProto(`. Имя переименовывается,
// пара «принимает доменную строку → возвращает контракт» — нет.
func TestInjectionConverterNameDoesNotHideTheSubject(t *testing.T) {
	src := opSrc("operationpb", `
func toProtoOperation(op *operations.Operation) *operationpb.Operation {
	out := &operationpb.Operation{Id: op.ID}
	return out
}
`)
	f, cen := opInjectAudit(t, map[string]string{"services/x/internal/handler/c.go": src})
	if len(f) == 0 {
		t.Fatalf("преобразователь с чужим именем не пойман — распознаватель судит ИМЯ: %+v", cen)
	}
	if !strings.Contains(f[0].What, "своим телом") {
		t.Fatalf("находка называет не тот предмет: %q", f[0].What)
	}
}

// ─── ОБХОД B2: делегирование в СВОЮ функцию ──────────────────────────────────

// Прежняя редакция принимала за прослойку любой одиночный возврат, содержащий
// подстроку `ToProto(`. Своя `legacyToProto` со своим телом проезжала — отстояв
// от отрицательной фикстуры гейта на одну букву.
func TestInjectionDelegationMustLeadToTheSharedLayer(t *testing.T) {
	src := opSrc("operationpb", `
func operationToProto(op *operations.Operation) *operationpb.Operation {
	return legacyToProto(op)
}

func legacyToProto(op *operations.Operation) *operationpb.Operation {
	return &operationpb.Operation{Id: op.ID}
}
`)
	f, cen := opInjectAudit(t, map[string]string{"services/x/internal/handler/c.go": src})
	if len(f) < 2 {
		t.Fatalf("делегирование в СВОЮ реализацию принято за прослойку — гейт судит форму "+
			"возврата, а не то, КУДА он ведёт. находок %d, перепись: %+v", len(f), cen)
	}
}

// ─── полоса владения ─────────────────────────────────────────────────────────

func TestInjectionOwnershipLaneIsFound(t *testing.T) {
	src := opSrc("operationpb", `
func lane(ctx context.Context, repo operations.Repo) {
	owned, _ := operations.AsOwned(repo)
	_ = owned
	owner, _ := operations.OwnerFromContext(ctx)
	_ = owner
}
`)
	f, _ := opInjectAudit(t, map[string]string{"services/x/internal/handler/l.go": src})
	if len(f) == 0 {
		t.Fatal("полоса владения в сервисе не поймана")
	}
	if !strings.Contains(f[0].What, "ключ владения") {
		t.Fatalf("находка называет не тот предмет: %q", f[0].What)
	}
}

// ─── ЗАКОННЫЕ БЛИЗНЕЦЫ: гейт обязан молчать ─────────────────────────────────

func TestLegitimateShapesAreSilent(t *testing.T) {
	cases := map[string]string{
		"прослойка в общий слой": `
func operationToProto(op *operations.Operation) *operationpb.Operation {
	return operationspb.ToProto(op)
}
`,
		"мутирующий RPC возвращает операцию": `
type H struct{}

func (h *H) Create(ctx context.Context, req *SomeRequest) (*operationpb.Operation, error) {
	return nil, nil
}

type SomeRequest struct{}
`,
		"чужой контракт с теми же именами методов": `
type H struct{}

func (h *H) Get(ctx context.Context, req *ComputeGetRequest) (*ComputeInstance, error) {
	return nil, nil
}

type ComputeGetRequest struct{}
type ComputeInstance struct{}
`,
		"упоминание в комментарии": `
// Обработчик переехал в pkg/operations/operationspb; здесь про
// operationpb.GetOperationRequest только этот комментарий.
func nothing() {}
`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			f, cen := opInjectAudit(t, map[string]string{"services/x/internal/handler/ok.go": opSrc("operationpb", body)})
			if len(f) != 0 {
				t.Fatalf("гейт краснеет на законной форме — он ловит форму, а не существо, "+
					"и первый же ложный срабат его отключит: %+v", f)
			}
			if cen.FilesRead != 1 {
				t.Fatalf("перепись не подтверждает, что файл прочитан: %+v", cen)
			}
		})
	}
}

// ─── самоистечение послабления ───────────────────────────────────────────────

// Запись, которой больше нечего исключать, наследует следующую слепую зону,
// поэтому она — находка, а не мелочь.
func TestExemptionWithoutSubjectIsAFinding(t *testing.T) {
	f, _ := opInjectAudit2(t,
		map[string]string{"services/x/internal/handler/ok.go": opSrc("operationpb", "\nfunc nothing() {}\n")},
		map[string]string{"gateway/internal/opsproxy": "предмет давно снят"})
	if len(f) == 0 {
		t.Fatal("исключение без предмета не найдено — послабление не истекает само")
	}
	if !strings.Contains(f[0].What, "нечего исключать") {
		t.Fatalf("находка называет не тот предмет: %q", f[0].What)
	}
}

func opInjectAudit2(t *testing.T, files, exempt map[string]string) ([]opSourceFinding, opSourceCensus) {
	t.Helper()
	dir := t.TempDir()
	var rels []string
	for rel, src := range files {
		abs := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
		rels = append(rels, rel)
	}
	f, c, err := auditOperationSingleSource(dir, rels, exempt)
	if err != nil {
		t.Fatalf("обход синтетики: %v", err)
	}
	return f, c
}

// ─── ОБХОДЫ ЧЕТВЁРТОГО КРУГА ─────────────────────────────────────────────────
//
// Внешний аудит провёл через прежнюю редакцию десять форков из тринадцати
// попыток. Ниже закреплены те, что были структурными: гейт объявлял три оси и
// охранял одну, тревогу «я ослеп» глушил исключённый путь, послабление не
// истекало, а прослойка засчитывалась без взгляда на аргумент.

// Псевдоним типа контракта: `type getReq = operationpb.GetOperationRequest`.
// Форк компилируется, регистрируется сервером операций, а перепись прежней
// редакции совпадала с чистым деревом ПОБАЙТОВО — сигнала не было никакого.
func TestInjectionTypeAliasDoesNotHideTheSubject(t *testing.T) {
	src := opSrc("operationpb", `
type getReq = operationpb.GetOperationRequest
type unimpl = operationpb.UnimplementedOperationServiceServer

type H struct{ unimpl }

func (h *H) Get(ctx context.Context, req *getReq) (*operationpb.Operation, error) {
	return nil, nil
}
`)
	f, cen := opInjectAudit(t, map[string]string{"services/x/internal/handler/h.go": src})
	if len(f) == 0 {
		t.Fatalf("форк за псевдонимом типа не пойман — распознаватель смотрит только на "+
			"селектор пакета, а имя типа можно спрятать за своим: %+v", cen)
	}
}

// Точечный импорт: типы контракта попадают в местное пространство имён и стоят
// голыми идентификаторами.
func TestInjectionDotImportDoesNotHideTheSubject(t *testing.T) {
	src := `package handler

import (
	. "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	"github.com/PRO-Robotech/kacho/pkg/operations"
)

var _ = operations.ErrNotFound

type H struct{ UnimplementedOperationServiceServer }

func (h *H) Cancel(ctx context.Context, req *CancelOperationRequest) (*Operation, error) {
	return nil, nil
}
`
	f, cen := opInjectAudit(t, map[string]string{"services/x/internal/handler/h.go": src})
	if len(f) == 0 {
		t.Fatalf("форк за точечным импортом не пойман: %+v", cen)
	}
}

// Прослойка, ПОДМЕНИВШАЯ аргумент: одна строка, возврат, вызов общего слоя — и
// расхождение полосы внутри (владелец стёрт). Прежняя редакция засчитывала это
// как образцовое делегирование.
func TestInjectionDelegationMustPassItsOwnArgumentUnchanged(t *testing.T) {
	src := opSrc("operationpb", `
func operationToProto(op *operations.Operation) *operationpb.Operation {
	return operationspb.ToProto(stripOwner(op))
}

func stripOwner(op *operations.Operation) *operations.Operation {
	op.Principal = operations.Principal{}
	return op
}
`)
	f, cen := opInjectAudit(t, map[string]string{"services/x/internal/handler/c.go": src})
	if len(f) == 0 {
		t.Fatalf("прослойка с подменённым аргументом принята за законную — гейт смотрит "+
			"на вызов и не смотрит, ЧТО в него передали: %+v", cen)
	}
}

// Исключение, у которого предмет исчез, а ФАЙЛЫ на месте.
//
// Прежняя проба подавала синтетику, где каталога под префиксом нет вовсе, то
// есть доказывала «каталог исчез», а называлась «предмет исчез» — заголовок шире
// тела. Отметка «запись сработала» ставилась при совпадении ПУТИ, поэтому запись
// переживала исчезновение предмета, пока в каталоге лежал хоть один файл.
func TestExemptionSurvivingItsSubjectIsAFinding(t *testing.T) {
	// Файл под исключённым префиксом ЕСТЬ, но предмета в нём нет.
	f, _ := opInjectAudit2(t,
		map[string]string{"gateway/internal/opsproxy/proxy.go": opSrc("operationpb", "\nfunc nothing() {}\n")},
		map[string]string{"gateway/internal/opsproxy": "предмет когда-то был"})
	if len(f) == 0 {
		t.Fatal("исключение пережило свой предмет и не найдено: отметка ставится по наличию " +
			"ФАЙЛА, а не по подавлению находки — послабление не истекает")
	}
	if !strings.Contains(f[0].What, "нечего исключать") {
		t.Fatalf("находка называет не тот предмет: %q", f[0].What)
	}
}

// Положительный контроль к предыдущей: пока предмет есть, запись молчит.
func TestExemptionWithLiveSubjectIsSilent(t *testing.T) {
	src := opSrc("operationpb", `
type H struct{}

func (h *H) Get(ctx context.Context, req *operationpb.GetOperationRequest) (*operationpb.Operation, error) {
	return nil, nil
}
`)
	f, cen := opInjectAudit2(t,
		map[string]string{"gateway/internal/opsproxy/proxy.go": src},
		map[string]string{"gateway/internal/opsproxy": "живой предмет"})
	if len(f) != 0 {
		t.Fatalf("запись с живым предметом объявлена истёкшей: %+v", f)
	}
	if cen.Exempted == 0 {
		t.Fatalf("подавленное не сосчитано — перепись не отличит «подавлено» от «не найдено»: %+v", cen)
	}
	// Исключённое НЕ засчитывается в ось: иначе тревога «ось ослепла» глушится
	// ровно тем местом, которое из суждения выведено.
	if cen.Handlers != 0 {
		t.Fatalf("исключённое попало в ось обработчика (%d) — страж предпосылки станет "+
			"вакуумным: %+v", cen.Handlers, cen)
	}
}

// ─── ОБХОДЫ ПЯТОГО КРУГА ─────────────────────────────────────────────────────
//
// Внешний аудит провёл через прежнюю редакцию семь форков из восьми попыток.
// Несущий — межфайловый псевдоним: он снимал ВСЕ ТРИ оси разом.

// Псевдонимы объявлены в СОСЕДНЕМ файле пакета. Файл-потребитель стабов не
// импортирует, поэтому пофайловая таблица оставляла `stubs == ""`, и все три оси
// пропускали его молча. Это законный Go и в эффекте неотличимо от объявления
// рядом.
func TestInjectionCrossFileAliasDoesNotHideTheSubject(t *testing.T) {
	alias := `package handler

import operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"

type getReq = operationpb.GetOperationRequest
type unimpl = operationpb.UnimplementedOperationServiceServer
`
	consumer := `package handler

type H struct{ unimpl }

func (h *H) Get(ctx context.Context, req *getReq) (interface{}, error) { return nil, nil }
`
	f, cen := opInjectAudit(t, map[string]string{
		"services/x/internal/handler/alias.go": alias,
		"services/x/internal/handler/h.go":     consumer,
	})
	if len(f) == 0 {
		t.Fatalf("форк за межфайловым псевдонимом не пойман — таблица псевдонимов "+
			"пофайловая, и объявление в соседнем файле пакета снимает ВСЕ оси: %+v", cen)
	}
}

// Преобразователь, принимающий доменную строку ПО ЗНАЧЕНИЮ. `operations.New`
// возвращает значение, поэтому такая форма естественнее принятой.
func TestInjectionValueParameterDoesNotHideTheSubject(t *testing.T) {
	src := opSrc("operationpb", `
func toPB(op operations.Operation) *operationpb.Operation {
	return &operationpb.Operation{Id: op.ID}
}
`)
	f, cen := opInjectAudit(t, map[string]string{"services/x/internal/handler/c.go": src})
	if len(f) == 0 {
		t.Fatalf("преобразователь с приёмом по значению не пойман: %+v", cen)
	}
}

// Псевдоним типа в ВОЗВРАТЕ и псевдоним ДОМЕННОГО типа во входе.
func TestInjectionAliasedTypesDoNotHideTheConverter(t *testing.T) {
	for name, body := range map[string]string{
		"псевдоним возврата": `
type opPB = operationpb.Operation

func toPB(op *operations.Operation) *opPB {
	return &operationpb.Operation{Id: op.ID}
}
`,
		"псевдоним доменного типа": `
type domOp = operations.Operation

func toPB(op *domOp) *operationpb.Operation {
	return &operationpb.Operation{Id: op.ID}
}
`,
	} {
		t.Run(name, func(t *testing.T) {
			f, cen := opInjectAudit(t, map[string]string{"services/x/internal/handler/c.go": opSrc("operationpb", body)})
			if len(f) == 0 {
				t.Fatalf("преобразователь за псевдонимом не пойман: %+v", cen)
			}
		})
	}
}

// ЗАКОННЫЙ близнец к сужению: метод use-case, принимающий операцию среди прочих
// входов, преобразователем НЕ является. Без этой пробы сужение выглядело бы
// произволом, а его отсутствие давало две ложные находки из двух новых.
func TestUseCaseMethodTakingAnOperationIsNotAConverter(t *testing.T) {
	src := opSrc("operationpb", `
type UC struct{}

func (uc *UC) finish(ctx context.Context, op operations.Operation, extra string) (*operationpb.Operation, error) {
	return &operationpb.Operation{Id: op.ID}, nil
}
`)
	f, _ := opInjectAudit(t, map[string]string{"services/x/internal/apps/uc.go": src})
	if len(f) != 0 {
		t.Fatalf("метод use-case объявлен преобразователем — распознаватель ловит форму "+
			"«принимает операцию, возвращает контракт», а не предмет: %+v", f)
	}
}

// ─── САМ СТРАЖ ПРЕДПОСЫЛКИ ───────────────────────────────────────────────────
//
// До этой пробы страж не был покрыт ничем: весь набор инъекций звал судящую
// функцию, а страж жил в драйвере — и, как показал аудит, свойства не имел.

// Ослепший распознаватель обязан быть виден: синтетический предмет каждой оси
// находится. Это ровно то, что проверяет страж, и здесь оно закреплено отдельно
// от него — иначе «страж есть» неотличимо от «страж вакуумен».
func TestGuardSyntheticProbesAreFoundOnEveryAxis(t *testing.T) {
	for axis, src := range map[string]string{
		"обработчик": opSrc("operationpb", `
type H struct{}

func (h *H) Get(ctx context.Context, req *operationpb.GetOperationRequest) (*operationpb.Operation, error) {
	return nil, nil
}
`),
		"преобразователь": opSrc("operationpb", `
func toPB(op *operations.Operation) *operationpb.Operation {
	return &operationpb.Operation{Id: op.ID}
}
`),
		"полоса владения": opSrc("operationpb", `
func lane(ctx context.Context, repo operations.Repo) {
	owned, _ := operations.AsOwned(repo)
	_ = owned
}
`),
	} {
		t.Run(axis, func(t *testing.T) {
			f, cen := opInjectAudit(t, map[string]string{"services/probe/x.go": src})
			if len(f) == 0 {
				t.Fatalf("синтетический предмет оси «%s» не найден — страж предпосылки "+
					"вакуумен, и «ноль находок» по дереву не утверждает ничего: %+v", axis, cen)
			}
		})
	}
}

// Страж НЕ зависит от состава дерева и от ведомости.
//
// Прежняя редакция считала находки в дереве и требовала непустоты. Это негодно
// в принципе: ось обработчика в сведённом дереве законно даёт НОЛЬ, потому что
// единственный обработчик лежит в общем слое, выведенном по построению. Такой
// страж покраснел бы на достижении цели — закрылась бы задача #1370, опустела
// ведомость, и гейт упал бы, ничего не найдя.
func TestGuardDoesNotDependOnTreeContents(t *testing.T) {
	// Дерево без единого предмета — законное состояние полностью сведённого
	// продукта. Находок ноль, и это НЕ повод падать.
	f, cen := opInjectAudit(t, map[string]string{
		"services/x/internal/handler/ok.go": opSrc("operationpb", "\nfunc nothing() {}\n"),
	})
	if len(f) != 0 {
		t.Fatalf("гейт краснеет на полностью сведённом дереве — это красное на достижении "+
			"цели: %+v", f)
	}
	if cen.FilesRead != 1 {
		t.Fatalf("перепись не подтверждает чтение: %+v", cen)
	}
}

// ─── ОБХОДЫ ШЕСТОГО КРУГА ────────────────────────────────────────────────────

// Преобразователь-МЕТОД со своим телом.
//
// Прежняя редакция отсекала его клаузой `d.Recv != nil`, введённой ради ложных
// находок на методах use-case. Замер снятием показал, что клауза не покупает
// ничего (перепись не меняется ни на единицу — их отсекает число входов), а
// открывает обычную форму Go. Условие, ничего не покупающее и не покрытое
// пробой, — слепая зона, выданная вперёд.
func TestInjectionConverterAsMethodDoesNotHideTheSubject(t *testing.T) {
	src := opSrc("operationpb", `
type mapper struct{}

func (m mapper) toPB(op *operations.Operation) *operationpb.Operation {
	return &operationpb.Operation{Id: op.ID}
}
`)
	f, cen := opInjectAudit(t, map[string]string{"services/x/internal/handler/c.go": src})
	if len(f) == 0 {
		t.Fatalf("преобразователь-метод не пойман: %+v", cen)
	}
}

// Цепочка псевдонимов: `type wire = opPB` при `type opPB = pb.Operation`.
// Одного лишнего звена было довольно, чтобы снять обе оси.
func TestInjectionAliasChainIsResolvedToFixpoint(t *testing.T) {
	src := opSrc("operationpb", `
type opPB = operationpb.Operation
type wire = opPB

type getReq = operationpb.GetOperationRequest
type req2 = getReq

type H struct{}

func (h *H) Get(ctx context.Context, r *req2) (*wire, error) { return nil, nil }

func toPB(op *operations.Operation) *wire {
	return &operationpb.Operation{Id: op.ID}
}
`)
	f, cen := opInjectAudit(t, map[string]string{"services/x/internal/handler/h.go": src})
	if len(f) < 2 {
		t.Fatalf("цепочка псевдонимов снимает оси: найдено %d, ожидалось ≥2 (обработчик и "+
			"преобразователь). перепись: %+v", len(f), cen)
	}
}

// Ведомость обязана прощать СВОЙ каталог, а не всё, что начинается так же.
func TestExemptionRespectsPathBoundary(t *testing.T) {
	src := opSrc("operationpb", `
type H struct{}

func (h *H) Get(ctx context.Context, req *operationpb.GetOperationRequest) (*operationpb.Operation, error) {
	return nil, nil
}
`)
	f, _ := opInjectAudit2(t,
		map[string]string{"gateway/internal/opsproxyv2/proxy.go": src},
		map[string]string{"gateway/internal/opsproxy": "живой предмет"})
	if len(f) == 0 {
		t.Fatal("послабление расползлось на СОСЕДНИЙ каталог: сопоставление без границы " +
			"пути прощает `opsproxyv2`, о котором автор записи не решал")
	}
	// Парный положительный контроль: свой каталог по-прежнему прощается.
	g, cen := opInjectAudit2(t,
		map[string]string{"gateway/internal/opsproxy/proxy.go": src},
		map[string]string{"gateway/internal/opsproxy": "живой предмет"})
	if len(g) != 0 {
		t.Fatalf("граница пути сломала законное прощение своего каталога: %+v", g)
	}
	if cen.Exempted == 0 {
		t.Fatalf("подавленное не сосчитано: %+v", cen)
	}
}

// ─── СТРАЖ СПОСОБЕН ПОКРАСНЕТЬ ───────────────────────────────────────────────

// До этой пробы в дереве стоял только ПОЛОЖИТЕЛЬНЫЙ контроль стража —
// продублированный в двух файлах: и страж, и проба делали одно и то же и
// зеленели вместе. Отрицательная сторона не проверялась ничем, а `testing.go`
// требует инъекции именно после переустройства, — страж же переустроен целиком.
//
// Здесь распознаватель ослепляется по существу: путь стабов рассинхронизирован
// с деревом, то есть воспроизведён ровно тот случай, ради которого страж и
// заведён.
func TestGuardGoesRedWhenTheRecogniserGoesBlind(t *testing.T) {
	// Синтетика с ПЕРЕИМЕНОВАННЫМ путём стабов: распознаватель, ключующийся на
	// `operationStubsPath`, не увидит в ней ничего.
	blind := `package handler

import (
	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operationXX"
	"github.com/PRO-Robotech/kacho/pkg/operations"
)

var _ = operations.ErrNotFound

type H struct{}

func (h *H) Get(ctx context.Context, req *operationpb.GetOperationRequest) (*operationpb.Operation, error) {
	return nil, nil
}
`
	f, cen := opInjectAudit(t, map[string]string{"services/x/internal/handler/h.go": blind})
	if len(f) != 0 {
		t.Fatalf("фикстура ослепления сама себя опровергает — она обязана быть НЕвидимой "+
			"для распознавателя, иначе проба ничего не доказывает: %+v", f)
	}
	if cen.Handlers != 0 {
		t.Fatalf("распознаватель увидел переименованный путь — фикстура вырождена: %+v", cen)
	}
	// А вот тот же предмет по КАНОНИЧЕСКОМУ пути обязан находиться. Пара
	// доказывает, что молчание выше — свойство фикстуры, а не сломанного гейта.
	good := strings.Replace(blind, "cloud/operationXX", "cloud/operation", 1)
	g, _ := opInjectAudit(t, map[string]string{"services/x/internal/handler/h.go": good})
	if len(g) == 0 {
		t.Fatal("канонический путь не распознаётся — гейт сломан, и страж обязан был бы " +
			"покраснеть на настоящем дереве")
	}
}

// Законная прослойка с ДВУМЯ возвращаемыми значениями — не находка.
func TestTwoValueDelegationIsLegitimate(t *testing.T) {
	src := opSrc("operationpb", `
func toPB(op *operations.Operation) (*operationpb.Operation, error) {
	return operationspb.ToProto(op), nil
}
`)
	f, _ := opInjectAudit(t, map[string]string{"services/x/internal/handler/c.go": src})
	if len(f) != 0 {
		t.Fatalf("прослойка с двумя возвращаемыми объявлена находкой — ложное срабатывание: %+v", f)
	}
	// Парный отрицательный: второе значение НЕ пустое — это уже логика, а не перевод.
	bad := opSrc("operationpb", `
func toPB(op *operations.Operation) (*operationpb.Operation, error) {
	return operationspb.ToProto(op), errSomething
}

var errSomething error
`)
	g, _ := opInjectAudit(t, map[string]string{"services/x/internal/handler/d.go": bad})
	if len(g) == 0 {
		t.Fatal("возврат с непустым вторым значением принят за прослойку — это уже не перевод")
	}
}

// ─── ПРОКСИ ВЫВЕДЕН ИЗ ОСИ ОБРАБОТЧИКА — И ТОЛЬКО ПРОКСИ ────────────────────

// opProxySrc — синтетический ПРОКСИ: вкладывает серверную заглушку контракта и
// держит полем КЛИЕНТ того же контракта. `withClient=false` снимает ВТОРУЮ
// половину пары и оставляет первую — то есть даёт законного близнеца-обработчика
// той же формы.
func opProxySrc(withClient bool) string {
	client := ""
	if withClient {
		client = "\n\tbackends map[string]operationpb.OperationServiceClient"
	}
	return opSrc("operationpb", `
type P struct {
	operationpb.UnimplementedOperationServiceServer`+client+`
}

func (p *P) Get(ctx context.Context, req *operationpb.GetOperationRequest) (*operationpb.Operation, error) {
	return nil, nil
}
`)
}

// ПРОКСИ — не форк обработчика, и выведен он ПО ПОСТРОЕНИЮ, а не ведомостью.
//
// Пара обязательна: молчание на прокси само по себе неотличимо от мёртвой оси
// обработчика. Близнец отличается ОДНИМ полем — тем самым, которое и делает тип
// прокси, — поэтому инъекция роняет ровно проверяемое и ничего кроме.
func TestProxyIsDerivedOutOfTheHandlerAxisButOnlyProxy(t *testing.T) {
	// Прокси: пара «серверная заглушка + клиент того же контракта» — молчит.
	f, cen := opInjectAudit(t, map[string]string{"gateway/internal/x/p.go": opProxySrc(true)})
	if len(f) != 0 {
		t.Fatalf("прокси объявлен форком обработчика — ложная находка: %+v", f)
	}
	if cen.Proxies == 0 {
		t.Fatalf("прокси не опознан вовсе: перепись обязана его НАЗВАТЬ, иначе «обработчиков 0» "+
			"не отличить от «ось мертва»: %+v", cen)
	}
	if cen.Handlers != 0 {
		t.Fatalf("прокси засчитан в ось обработчика: %+v", cen)
	}

	// Законный близнец: та же форма БЕЗ клиента — обычный форк обработчика,
	// и он обязан находиться. Без этой половины «прокси молчит» доказывало бы
	// только то, что ось перестала работать.
	g, gcen := opInjectAudit(t, map[string]string{"gateway/internal/x/p.go": opProxySrc(false)})
	if len(g) == 0 {
		t.Fatalf("форк обработчика без клиента контракта НЕ найден — вывод прокси снял ось "+
			"целиком, а не одного её участника: %+v", gcen)
	}
	if gcen.Proxies != 0 {
		t.Fatalf("тип без клиента контракта опознан как прокси — дискриминатор вырожден: %+v", gcen)
	}
}

// ─── ОСЬ ПРОЧИТАННОГО ВЛАДЕЛЬЦА ─────────────────────────────────────────────

// opRecordedOwnerSrc — тело функции, читающей владельца с самой строки.
func opRecordedOwnerSrc(body string) string {
	return opSrc("operationpb", `
type P struct {
	operationpb.UnimplementedOperationServiceServer
	backends map[string]operationpb.OperationServiceClient
}
`+body)
}

// Полоса, решающая по прочитанной строке САМА, — находка.
func TestInjectionRecordedOwnerLaneDecidingItselfIsAFinding(t *testing.T) {
	src := opRecordedOwnerSrc(`
func check(callerID string, op *operationpb.Operation) error {
	if callerID != op.GetPrincipalId() {
		return errDenied
	}
	return nil
}

var errDenied error
`)
	f, cen := opInjectAudit(t, map[string]string{"gateway/internal/x/p.go": src})
	if len(f) == 0 {
		t.Fatalf("самодельная полоса владения по прочитанной строке НЕ найдена: %+v", cen)
	}
	if cen.RecordedOwnership == 0 {
		t.Fatalf("перепись не назвала осмотренного по этой оси: %+v", cen)
	}
	if !strings.Contains(f[0].Why, "прочитанного владельца") {
		t.Fatalf("находка не называет ось, по которой найдена: %+v", f[0])
	}
}

// Законный близнец: та же функция ОТДАЁТ решение санкционированному глаголу.
func TestRecordedOwnerLaneDelegatingIsSilent(t *testing.T) {
	src := opRecordedOwnerSrc(`
func check(callerType, callerID string, op *operationpb.Operation) error {
	return operations.CheckRecordedOwnership(
		operations.Principal{Type: callerType, ID: callerID},
		operations.Principal{Type: op.GetPrincipalType(), ID: op.GetPrincipalId()},
	)
}
`)
	f, cen := opInjectAudit(t, map[string]string{"gateway/internal/x/p.go": src})
	if len(f) != 0 {
		t.Fatalf("делегирующая полоса объявлена находкой — ложное срабатывание: %+v", f)
	}
	if cen.RecordedOwnership == 0 {
		t.Fatalf("делегирующая полоса не осмотрена вовсе — молчание пустое: %+v", cen)
	}
}

// Обход «свой выход, потом общий глагол» — тоже находка: требуется КАЖДЫЙ выход.
//
// Присутствия вызова недостаточно, и это не педантизм: собственный ранний
// возврат и есть та форма, ради которой полосы расходятся.
func TestInjectionEarlyReturnBesideTheSanctionedVerbIsAFinding(t *testing.T) {
	src := opRecordedOwnerSrc(`
func check(callerType, callerID string, op *operationpb.Operation) error {
	if callerType == "system" {
		return nil
	}
	return operations.CheckRecordedOwnership(
		operations.Principal{Type: callerType, ID: callerID},
		operations.Principal{Type: op.GetPrincipalType(), ID: op.GetPrincipalId()},
	)
}
`)
	f, _ := opInjectAudit(t, map[string]string{"gateway/internal/x/p.go": src})
	if len(f) == 0 {
		t.Fatal("свой ранний выход рядом с санкционированным глаголом принят за делегирование — " +
			"обход, ради которого ось и заведена, проезжает")
	}
}

// Те же имена геттеров у ЧУЖОГО контракта — не предмет этой оси.
//
// Ответ фасада личности несёт `GetPrincipalType`/`GetPrincipalId` и к операциям
// отношения не имеет. Без этого контроля ось краснела бы на соседнем предмете,
// и её сняли бы первым же прочтением.
func TestRecordedOwnerAxisIgnoresForeignContracts(t *testing.T) {
	src := `package middleware

import (
	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kaname/cloud/iam/v1"
)

func lane(resp *iamv1.ResolveSubjectResponse) (string, string) {
	return resp.GetPrincipalType(), resp.GetPrincipalId()
}
`
	f, cen := opInjectAudit(t, map[string]string{"gateway/internal/middleware/m.go": src})
	if len(f) != 0 {
		t.Fatalf("ось прочитанного владельца сработала на чужом контракте: %+v", f)
	}
	if cen.RecordedOwnership != 0 {
		t.Fatalf("чужой контракт засчитан в перепись этой оси: %+v", cen)
	}
}
