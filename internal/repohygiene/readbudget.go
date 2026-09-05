// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// readbudget.go — анализатор класса «мутация, купившая читательский бюджет».
//
// # Предмет
//
// Ограничитель допуска (`pkg/grpcsrv`) относит вызов к чтениям по префиксу имени
// (`Get`/`List` — конвенция продукта), а всё остальное считает мутацией. У этой
// полярности одна ОПАСНАЯ сторона: метод, который на самом деле мутирует, но
// назван по-читательски, получает читательский бюджет — впятеро более щедрый при
// втрое большей стоимости запроса.
//
// # Почему анализатор ЗДЕСЬ, а не рядом с композиционным корнем
//
// Прежде это свойство держал один тест в каталоге vpc, обходивший дескрипторы,
// слинкованные в бинарь vpc. Потолок допуска после #771 провязан у СЕМИ сервисов,
// а страж оставался один: свойство держалось не переписью дерева, а тем, у кого
// случайно оказался страж. Здесь оно требуется от ДЕРЕВА — от всех объявленных
// контрактом пакетов сразу, включая тот, чей сервис ещё никем не смонтирован.
//
// # Дискриминатор — контракт, а не список имён
//
// «Мутация» имеет машинно-проверяемый признак: мутации асинхронны и возвращают
// `Operation` (правило #9). Выписанный перечень методов отстал бы от следующего
// RPC — то есть ровно от того, ради которого страж и нужен.
//
// # Дискриминатор НЕСОСТОЯТЕЛЕН внутри пакета самого конверта, и это записано
//
// В `kacho.cloud.operation` `Operation` — не конверт асинхронной мутации, а САМ
// РЕСУРС: `OperationService/Get` возвращает его потому, что это чтение операции,
// которое клиент и поллит до `done=true` (`api-conventions.md` §«Форма ресурса»).
// Признак «возвращает Operation ⇒ мутирует» там означает обратное, поэтому пакет
// исключён ЯВНО и с причиной. Исключение не бессрочно: запись, которой больше
// нечего исключать, — находка (см. ReadBudgetFinding.KindStaleExemption), иначе
// она унаследует следующую слепую зону.
//
// # Предпосылка проверяется
//
// Перечень пакетов, чьи дескрипторы гейт видит, зависит от пустых импортов в
// тесте — то есть от РУКОПИСНОГО списка. Поэтому анализатор сверяет увиденное с
// тем, что объявляет ДЕРЕВО (`proto/kacho/**/*.proto`): пакет, объявленный
// деревом и не попавший в реестр, — отказ, а не тишина. Ноль осмотренных методов
// — тоже отказ: «ноль находок» обязано быть отличимо от «ноль прочитанного».
package repohygiene

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"

	"google.golang.org/protobuf/reflect/protoreflect"
)

// FileRanger — источник дескрипторов. Интерфейс, а не *protoregistry.Files,
// затем что инъекция обязана подать анализатору НАСТОЯЩИЙ вход, которого в
// дереве нет: синтетический контракт с мутацией, названной по-читательски.
type FileRanger interface {
	RangeFiles(f func(protoreflect.FileDescriptor) bool)
}

// CallClass — класс вызова в терминах ограничителя допуска. Собственный тип, а не
// импорт `pkg/grpcsrv`: анализатору нужен ОТВЕТ классификатора, а не его дом, и
// зависимость гигиены дерева от фундамента здесь ничего не добавляет.
type CallClass int

const (
	// ClassRead — читательский бюджет (щедрый).
	ClassRead CallClass = iota
	// ClassMutation — бюджет мутации (узкий).
	ClassMutation
)

// ReadBudgetFindingKind — вид находки.
type ReadBudgetFindingKind string

const (
	// KindMutationBuysReadBudget — метод возвращает конверт асинхронной мутации и
	// при этом отнесён классификатором к чтениям.
	KindMutationBuysReadBudget ReadBudgetFindingKind = "MUTATION-BUYS-READ-BUDGET"
	// KindStaleExemption — исключённому пакету больше нечего исключать.
	KindStaleExemption ReadBudgetFindingKind = "STALE-EXEMPTION"
)

// ReadBudgetFinding — одна находка с координатой.
type ReadBudgetFinding struct {
	Kind    ReadBudgetFindingKind
	Package string
	Method  string // "/<пакет>.<Сервис>/<Метод>"; пусто у STALE-EXEMPTION
	Detail  string
}

func (f ReadBudgetFinding) String() string {
	if f.Method == "" {
		return fmt.Sprintf("%s %s — %s", f.Kind, f.Package, f.Detail)
	}
	return fmt.Sprintf("%s %s — %s", f.Kind, f.Method, f.Detail)
}

// ReadBudgetCensus — объём осмотренного. Печатается ВСЕГДА.
type ReadBudgetCensus struct {
	PackagesDeclared   int // объявлено деревом (proto/kacho/**/*.proto)
	PackagesSeen       int // из них найдено в реестре дескрипторов
	PackagesExempt     int
	Services           int
	Methods            int
	OperationReturning int
	ReadNamed          int
}

func (c ReadBudgetCensus) String() string {
	return fmt.Sprintf(
		"перепись: пакетов объявлено деревом %d, увидено в реестре %d (исключено %d), "+
			"сервисов %d, методов %d, возвращают конверт мутации %d, отнесены к чтениям %d",
		c.PackagesDeclared, c.PackagesSeen, c.PackagesExempt,
		c.Services, c.Methods, c.OperationReturning, c.ReadNamed)
}

// ReadBudgetOptions — вход анализатора.
type ReadBudgetOptions struct {
	// Files — источник дескрипторов.
	Files FileRanger
	// Classify — тот же классификатор, что исполняется на листенере. Берётся
	// вызывающим, чтобы гейт судил РАБОТАЮЩИЙ код, а не свою копию правила.
	Classify func(fullMethod string) CallClass
	// OperationMessage — полное имя конверта асинхронной мутации.
	OperationMessage string
	// DeclaredPackages — пакеты, объявленные деревом. Предпосылка полноты.
	DeclaredPackages []string
	// Exempt — пакет → причина исключения. Пустая причина запрещена: исключение
	// без записанной причины неотличимо от забытого.
	Exempt map[string]string
}

// AuditReadBudgetClassification — перепись и находки.
//
// Ошибка возвращается на НЕВЫПОЛНЕННОЙ ПРЕДПОСЫЛКЕ (обход смотрит не туда,
// дискриминатор не нашёл предмета, объявленный деревом пакет не виден): молчание
// такого гейта ничего не доказывает, и оно обязано быть отличимо от чистоты.
func AuditReadBudgetClassification(opts ReadBudgetOptions, out io.Writer) ([]ReadBudgetFinding, ReadBudgetCensus, error) {
	var c ReadBudgetCensus
	if opts.Files == nil {
		return nil, c, fmt.Errorf("readbudget: не задан источник дескрипторов")
	}
	if opts.Classify == nil {
		return nil, c, fmt.Errorf("readbudget: не задан классификатор — гейт судил бы свою копию правила, а не работающий код")
	}
	if strings.TrimSpace(opts.OperationMessage) == "" {
		return nil, c, fmt.Errorf("readbudget: не названо имя конверта мутации — дискриминатор не имеет предмета")
	}
	for pkg, why := range opts.Exempt {
		if strings.TrimSpace(why) == "" {
			return nil, c, fmt.Errorf("readbudget: у исключения %q не записана причина", pkg)
		}
	}
	c.PackagesDeclared = len(opts.DeclaredPackages)
	c.PackagesExempt = len(opts.Exempt)

	declared := make(map[string]struct{}, len(opts.DeclaredPackages))
	for _, p := range opts.DeclaredPackages {
		declared[p] = struct{}{}
	}
	seen := make(map[string]struct{}, len(declared))
	// exemptUsed — у исключения должен быть предмет: пакет, который БЕЗ него дал бы
	// находку. Иначе запись истекла и её пора снять.
	exemptUsed := make(map[string]int, len(opts.Exempt))

	var findings []ReadBudgetFinding
	opts.Files.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		pkg := string(fd.Package())
		if _, ok := declared[pkg]; !ok {
			return true // чужой дескриптор, попавший в реестр транзитивно
		}
		seen[pkg] = struct{}{}
		_, isExempt := opts.Exempt[pkg]
		for i := 0; i < fd.Services().Len(); i++ {
			svc := fd.Services().Get(i)
			c.Services++
			for j := 0; j < svc.Methods().Len(); j++ {
				m := svc.Methods().Get(j)
				c.Methods++
				full := "/" + string(svc.FullName()) + "/" + string(m.Name())
				isRead := opts.Classify(full) == ClassRead
				if isRead {
					c.ReadNamed++
				}
				if string(m.Output().FullName()) != opts.OperationMessage {
					continue
				}
				c.OperationReturning++
				if !isRead {
					continue
				}
				if isExempt {
					exemptUsed[pkg]++
					continue
				}
				findings = append(findings, ReadBudgetFinding{
					Kind:    KindMutationBuysReadBudget,
					Package: pkg,
					Method:  full,
					Detail: fmt.Sprintf("возвращает %s (то есть мутирует по правилу #9), "+
						"но назван по-читательски и потому купит ЧИТАТЕЛЬСКИЙ бюджет", opts.OperationMessage),
				})
			}
		}
		return true
	})
	c.PackagesSeen = len(seen)

	for pkg := range opts.Exempt {
		if exemptUsed[pkg] == 0 {
			findings = append(findings, ReadBudgetFinding{
				Kind:    KindStaleExemption,
				Package: pkg,
				Detail: "исключению больше нечего исключать: ни один его метод не даёт находки. " +
					"Снимите запись — она унаследует следующую слепую зону",
			})
		}
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Kind != findings[j].Kind {
			return findings[i].Kind < findings[j].Kind
		}
		return findings[i].Method+findings[i].Package < findings[j].Method+findings[j].Package
	})

	if out != nil {
		_, _ = fmt.Fprintln(out, c.String())
	}

	// ── предпосылки ──────────────────────────────────────────────────────────
	if c.PackagesDeclared == 0 {
		return findings, c, fmt.Errorf("readbudget: дерево не объявило НИ ОДНОГО пакета контрактов — " +
			"перепись беспредметна, молчание гейта ничего не доказывает")
	}
	var missing []string
	for p := range declared {
		if _, ok := seen[p]; !ok {
			missing = append(missing, p)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return findings, c, fmt.Errorf("readbudget: пакет(ы) объявлены деревом, но их дескрипторов нет в реестре: %s. "+
			"Не хватает пустого импорта сгенерённого пакета в тесте — без него методы этого домена "+
			"НЕ осматриваются, а гейт молчит так же, как на чистом дереве", strings.Join(missing, ", "))
	}
	if c.Methods == 0 {
		return findings, c, fmt.Errorf("readbudget: не осмотрено НИ ОДНОГО метода — обход смотрит не туда")
	}
	if c.OperationReturning == 0 {
		return findings, c, fmt.Errorf("readbudget: ни один метод не возвращает %q — дискриминатор мутации "+
			"не нашёл своего предмета (имя конверта разошлось с деревом), поэтому гейт зелёный на всём",
			opts.OperationMessage)
	}
	if c.ReadNamed == 0 {
		return findings, c, fmt.Errorf("readbudget: ни один метод не отнесён к чтениям — классификатор " +
			"отвечает одинаково на всё, и опасное направление проверять не на чем")
	}
	return findings, c, nil
}

// DeclaredProtoPackages — пакеты, объявленные ДЕРЕВОМ.
//
// Перечень выводится обходом, а не выписывается: рукописный список разошёлся бы
// с деревом молча, и разошёлся бы именно там, где расхождение не видно, — на
// новом домене.
//
// Состав берётся у ИНДЕКСА (`pkg/treecorpus`), а не с диска: обход диска
// подбирает то, что лежит у разработчика и не отслеживается, — распаковки
// чартов, сборочные каталоги, отчёты прогонов. Два обхода поддерева в этом
// дереве уже оказались дефектными по этой самой причине.
func DeclaredProtoPackages(root string) ([]string, error) {
	// Пути возвращаются АБСОЛЮТНЫМИ — так объявлено контрактом treecorpus.Under,
	// поэтому join с корнем здесь был бы вторым корнем в пути.
	files, err := treecorpus.UnderWithSuffix(filepath.Join(root, "proto", "kacho"), ".proto")
	if err != nil {
		return nil, err
	}
	set := map[string]struct{}{}
	for _, path := range files {
		// #nosec G304 -- путь пришёл из индекса git (treecorpus), а не от вызывающего:
		// вход этой функции — корень репозитория, и никакая часть пути не строится
		// из данных запроса. Диалект намеренный: подавление другого вида в этом
		// дереве не читает НИКТО, и такая строка была бы формой без действия.
		b, rErr := os.ReadFile(path)
		if rErr != nil {
			return nil, rErr
		}
		// Выражение — ОБЩЕЕ с надгробием снятой поверхности (retiredrpcsurface.go):
		// два места об одном предмете разъехались бы на первом же уточнении формы.
		if m := protoPackageRe.FindStringSubmatch(string(b)); m != nil {
			set[m[1]] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}
