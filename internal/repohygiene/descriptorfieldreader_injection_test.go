// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Доказательство того, что гейт мёртвого поля дескриптора СПОСОБЕН упасть — и
// что падает он на существе, а не на форме.
//
// Обе стороны гоняют ТУ ЖЕ функцию, что и гейт по дереву
// (`auditDescriptorFieldReaders`), на синтетическом дереве: проба, повторяющая
// логику гейта своей копией, доказывала бы свойство копии.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// synthContract — пакет дескриптора синтетического дерева. Формы полей взяты те
// же, что в настоящем: обязательная величина, порт (интерфейс) и ось.
const synthContract = `package servicecontract

type ExistenceProbe interface {
	ObjectExists(objectType, objectID string) (bool, error)
}

type Axis[T any] struct {
	value    T
	hasValue bool
}

func (a Axis[T]) Get() (T, bool)  { return a.value, a.hasValue }
func (a Axis[T]) Declared() bool  { return a.hasValue }

type Spec struct {
	Service        string
	HandlingBudget int
	Existence      ExistenceProbe
	Emits          Axis[[]string]
}

func New(s Spec) error {
	if s.Service == "" {
		return errRefused
	}
	if !s.Emits.Declared() {
		return errRefused
	}
	if s.Existence == nil {
		return errRefused
	}
	return nil
}
`

// synthCarrierWired — носитель, который читает ВСЁ: величину, порт и значение
// оси. Это законный близнец.
const synthCarrierWired = `package servicehost

import "github.com/PRO-Robotech/kacho/pkg/servicecontract"

func Serve(s servicecontract.Spec) {
	_ = s.HandlingBudget
	_, _ = s.Existence.ObjectExists("t", "id")
	if v, ok := s.Emits.Get(); ok {
		_ = v
	}
	_ = s.Service
}
`

// synthCarrierUnwired — носитель, у которого от трёх полей не осталось ни одного
// читателя: величина не читается вовсе, порт только проверялся дескриптором на
// наличие, ось — только на объявленность. Это и есть дефект, причём во всех трёх
// клетках сразу.
const synthCarrierUnwired = `package servicehost

import "github.com/PRO-Robotech/kacho/pkg/servicecontract"

func Serve(s servicecontract.Spec) {
	_ = s.Service
}
`

// synthCarrierDecoy — ЛОВУШКА ТЕМЫ, неудобная для гейта нарочно.
//
// Имена всех трёх полей стоят здесь в комментарии и в строковом литерале — то
// есть ровно там, где текстовый предикат нашёл бы их и объявил читателями,
// оставшись зелёным на снятой провязке. Исполняемая часть при этом ни одного из
// них не читает.
const synthCarrierDecoy = `package servicehost

import "github.com/PRO-Robotech/kacho/pkg/servicecontract"

// Историческая справка: носитель когда-то читал s.HandlingBudget, звал
// s.Existence.ObjectExists и разворачивал s.Emits.Get(). Теперь ничего этого нет.
func Serve(s servicecontract.Spec) string {
	_ = s.Service
	return "HandlingBudget + Existence + Emits.Get"
}
`

// synthDescriptorTree строит минимальное дерево с обоими пакетами носителя.
//
// Файлы кладутся в индекс git: гейт берёт состав у `pkg/treecorpus`, то
// есть у git, а не у диска. Фикстура, которая этого не делает, оставляет гейт с
// пустым составом — и он молчит не потому, что нарушения нет, а потому, что
// смотреть было не на что.
func synthDescriptorTree(t *testing.T, carrier string) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	write("go.mod", "module github.com/PRO-Robotech/kacho\n\ngo 1.25\n")
	write(contractPkgRel+"/contract.go", synthContract)
	write(carrierPkgRel+"/serve.go", carrier)
	synthTrack(t, root)
	return root
}

func joinFindings(res fieldReaderResult) string {
	var b []string
	for _, f := range res.findings {
		b = append(b, f.field+" "+f.where+" "+f.what)
	}
	return strings.Join(b, "\n")
}

// TestDescriptorFieldGateRedOnEachOfTheThreeCells — направление (а): гейт
// краснеет на КАЖДОЙ из трёх клеток и НАЗЫВАЕТ КООРДИНАТУ объявления. Без
// координаты находка не является действием.
func TestDescriptorFieldGateRedOnEachOfTheThreeCells(t *testing.T) {
	root := synthDescriptorTree(t, synthCarrierUnwired)
	res := auditDescriptorFieldReaders(t, root)
	t.Log(res.summary)

	if len(res.findings) == 0 {
		t.Fatalf("три поля остались без читателя, а гейт молчит — он не способен упасть.\n%s", res.summary)
	}
	all := joinFindings(res)
	for _, want := range []string{
		"HandlingBudget", // клетка 1: не читается нигде
		"Existence",      // клетка 2: порт проверен на наличие и не вызван
		"Emits",          // клетка 3: ось читается только предикатом объявленности
	} {
		if !strings.Contains(all, want) {
			t.Fatalf("гейт не назвал поле %q:\n%s", want, all)
		}
	}
	if !strings.Contains(all, contractPkgRel+"/contract.go:") {
		t.Fatalf("находка не называет координату объявления — по ней нечего чинить:\n%s", all)
	}
	// Клетки обязаны быть названы ПОРОЗНЬ: «не читается нигде», «порт не вызван»
	// и «значение оси декоративно» чинятся по-разному.
	for _, want := range []string{"не читается НИГДЕ", "порт", "предикатом объявленности"} {
		if !strings.Contains(all, want) {
			t.Fatalf("гейт не различил клетки — нет формулировки %q:\n%s", want, all)
		}
	}
	t.Logf("направление (а): гейт покраснел на всех трёх клетках:\n%s", all)
}

// TestDescriptorFieldGateSilentOnAWiredCarrier — направление (б): тот же
// дескриптор с провязанным носителем гейта не задевает.
func TestDescriptorFieldGateSilentOnAWiredCarrier(t *testing.T) {
	root := synthDescriptorTree(t, synthCarrierWired)
	res := auditDescriptorFieldReaders(t, root)
	t.Log(res.summary)

	if len(res.findings) != 0 {
		t.Fatalf("провязанный носитель объявлен находкой — гейт ловит форму, а не существо:\n%s",
			joinFindings(res))
	}
	if res.fields == 0 || res.files == 0 {
		t.Fatalf("гейт ничего не осмотрел — молчание означает «не нашёл», а не «всё чисто».\n%s",
			res.summary)
	}
}

// TestDescriptorFieldGateIgnoresProseDecoy — прямая проверка того, что чтение
// идёт по исполняемой части: имена полей в комментарии и в строковом литерале
// читателями не считаются.
//
// Без этой пробы «гейт читает AST, а не текст» осталось бы утверждением о
// намерении: приманка — единственный вход, на котором два предиката расходятся.
func TestDescriptorFieldGateIgnoresProseDecoy(t *testing.T) {
	root := synthDescriptorTree(t, synthCarrierDecoy)
	res := auditDescriptorFieldReaders(t, root)
	t.Log(res.summary)

	all := joinFindings(res)
	for _, want := range []string{"HandlingBudget", "Existence", "Emits"} {
		if !strings.Contains(all, want) {
			t.Fatalf("имя поля в комментарии и в строковом литерале засчитано за читателя — "+
				"гейт читает текст, а не исполняемую часть; %q не назван:\n%s", want, all)
		}
	}
}

// TestDecorativeAxisExceptionsAllStillHaveASubject — самоистечение перечня
// декоративных осей, доказанное ИНЪЕКЦИЕЙ, а не прочтением.
//
// Ось, попавшая в перечень и при этом уже разворачиваемая носителем, обязана
// дать находку: иначе запись переживает свой предмет и укрывает следующую
// декоративную ось, которую здесь заведут.
func TestDecorativeAxisExceptionsAllStillHaveASubject(t *testing.T) {
	// Имя берётся из САМОГО перечня, а не выписывается: выписанное пережило бы
	// правку перечня, и проба стала бы вакуумной.
	var excused string
	for field := range decorativeAxisExceptions {
		excused = field
		break
	}
	if excused == "" {
		t.Skip("перечень декоративных осей пуст — истекать нечему")
	}

	res := auditDescriptorFieldReaders(t, repoRoot(t))
	if len(res.readers[excused]) == 0 {
		t.Fatalf("ось %q не читается вообще ничем — исключение выдано не той клетке: "+
			"перечень покрывает ТОЛЬКО «читается предикатом объявленности», а это «не читается нигде»",
			excused)
	}
	if !res.decorative(excused) {
		t.Fatalf("ось %q уже разворачивают (%s) — исключение потеряло предмет и обязано быть снято",
			excused, strings.Join(res.readers[excused], ", "))
	}
	t.Logf("вход самоистечения построен: ось %q читается только предикатом объявленности (%d читателей) — "+
		"гейт по дереву обязан покраснеть, как только её значение развернут",
		excused, len(res.readers[excused]))
}
