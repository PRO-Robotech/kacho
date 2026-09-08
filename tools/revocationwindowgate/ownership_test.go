// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// ownership_test.go — инъекция распознавателя форм владения окном отзыва,
// В ОБЕ СТОРОНЫ И ПО КАЖДОЙ ФОРМЕ ОТДЕЛЬНО.
//
// Формы две, и доказывать их надо порознь. Пара «нашёл хоть что-то · смолчал
// хоть на чём-то» тут ничего не измеряет: распознаватель, у которого форма 2
// мертва, эту пару проходит целиком на форме 1 — а именно так он и жил, пока
// вторая форма была написана в теле одной пробы и не звалась второй.
//
// Поэтому по каждой форме утверждается пара: настоящий вход из дерева ⇒ форма
// распознана; законный близнец ТОЙ ЖЕ формы ⇒ молчание. И отдельно — что формы
// не сливаются: вход одной формы не поднимает флаг другой, иначе перепись «по
// формам» печатала бы одно число дважды.
package revocationwindowgate_test

import (
	"testing"

	"github.com/PRO-Robotech/kacho/tools/revocationwindowgate"
)

// ────────────────────────────────────────────────────────────────────────────
// Форма 1 — процесс строит кеш вердиктов сам
// ────────────────────────────────────────────────────────────────────────────

// srcOwnCtor — форма из дерева: собственная дверь iam строит кеш вердиктов
// корелибовским конструктором.
const srcOwnCtor = `package authzguard

func newDoor(opts Options) *Door {
	return &Door{
		Cache: authz.NewCache(opts.PositiveTTL),
	}
}
`

// srcCredentialCtor — законный близнец формы 1: тот же вид вызова, тот же
// параметр-срок, соседний файл. Хранит УДОСТОВЕРЕНИЕ, а не вердикт: его отзыв
// идёт другой полосой и обязан быть немедленным. Засчитав его, мы толкали бы
// сужать окно грантов ради задачи, которой оно не решается.
const srcCredentialCtor = `package presentedcred

func newReader(opts Options) *Reader {
	return &Reader{
		cache: introspection.NewResultCache(opts.PositiveTTL),
	}
}
`

func TestScanWindowOwnership_Form1_FindsTheOwnConstructor(t *testing.T) {
	own, err := revocationwindowgate.ScanWindowOwnership("services/iam/internal/authzguard/own_door.go", srcOwnCtor)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if !own.ByConstructor {
		t.Fatalf("площадка строит кеш вердиктов конструктором, а распознаватель ответил «не строит».\n"+
			"Распознаваемый набор конструкторов: %v", revocationwindowgate.VerdictCacheCtorNames())
	}
	if own.ByDescriptor {
		t.Error("вызов конструктора засчитан ВТОРОЙ формой тоже — формы слились, и перепись " +
			"«по формам» печатала бы одно число дважды, то есть перестала бы различать, какая " +
			"половина распознавателя ослепла")
	}
}

func TestScanWindowOwnership_Form1_SilentOnTheCredentialCache(t *testing.T) {
	own, err := revocationwindowgate.ScanWindowOwnership("services/iam/internal/presentedcred/reader.go", srcCredentialCtor)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if own.ByConstructor {
		t.Error("кеш УДОСТОВЕРЕНИЙ засчитан кешем вердиктов — предикат ловит вид вызова, " +
			"а не то, что в кеше лежит")
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Форма 2 — процесс отдаёт окно дескриптору носителя
// ────────────────────────────────────────────────────────────────────────────

// srcDescriptorWindow — форма из дерева: сервис кеша не строит, он НАЗЫВАЕТ
// величину окна дескриптору, а кеш строит носитель на все сервисы разом.
// Сегодня так и только так держат окно пять площадок из восьми.
const srcDescriptorWindow = `package main

func describe(cfg *config.Config) servicecontract.Spec {
	return servicecontract.Spec{
		HandlingBudget: cfg.Budget,
		CacheWindow:    cfg.AuthZCacheTTL,
	}
}
`

// srcDescriptorNeighbourAxis — законный близнец формы 2: ТОТ ЖЕ дескриптор,
// тот же вид литерала, соседняя ось. Окна отзыва гранта не объявляет.
const srcDescriptorNeighbourAxis = `package main

func describe(cfg *config.Config) servicecontract.Spec {
	return servicecontract.Spec{
		HandlingBudget: cfg.Budget,
		ClientBudget:   cfg.ClientBudget,
	}
}
`

// srcDescriptorFieldInProse — второй законный близнец, и он про РАЗБОР, а не
// про ось: имя поля стоит в комментарии и в строке, объясняющих сам предмет.
// Поиск по образцу засчитал бы этот файл площадкой; разбор обязан молчать.
const srcDescriptorFieldInProse = `package docs

// CacheWindow — окно отзыва: столько субъект, у которого право отобрали,
// продолжает проходить. Здесь оно только объясняется.
const explain = "CacheWindow сообщает окно носителю"
`

func TestScanWindowOwnership_Form2_FindsTheDescriptorWindow(t *testing.T) {
	own, err := revocationwindowgate.ScanWindowOwnership("services/vpc/cmd/vpc/describe.go", srcDescriptorWindow)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if !own.ByDescriptor {
		t.Fatalf("площадка объявляет окно полем %q дескриптору, а распознаватель её не увидел.\n"+
			"  Это НЕ редкая форма: ею и только ею держат окно пять площадок дерева из восьми, "+
			"и именно её не знал обход, который спрашивал «строит ли процесс кеш».\n"+
			"  Формы переписи: %v", revocationwindowgate.DescriptorWindowField,
			revocationwindowgate.OwnershipFormNames())
	}
	if own.ByConstructor {
		t.Error("объявление окна дескриптору засчитано ПЕРВОЙ формой тоже — формы слились")
	}
}

func TestScanWindowOwnership_Form2_SilentOnTheNeighbourAxis(t *testing.T) {
	own, err := revocationwindowgate.ScanWindowOwnership("services/vpc/cmd/vpc/describe.go", srcDescriptorNeighbourAxis)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if own.ByDescriptor {
		t.Errorf("соседняя ось того же дескриптора засчитана окном отзыва — предикат ловит "+
			"литерал дескриптора, а не поле %q; тогда в перепись окон поедет каждый, кто "+
			"вообще собирает дескриптор", revocationwindowgate.DescriptorWindowField)
	}
}

func TestScanWindowOwnership_Form2_SilentOnTheFieldNameInProse(t *testing.T) {
	own, err := revocationwindowgate.ScanWindowOwnership("docs/explain.go", srcDescriptorFieldInProse)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if own.ByDescriptor {
		t.Errorf("имя поля %q, встреченное в комментарии и в строке, засчитано объявлением окна — "+
			"распознаватель читает текст, а не разобранный узел, и потому не отличает предмет "+
			"от рассказа о нём", revocationwindowgate.DescriptorWindowField)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Предпосылки самого распознавателя
// ────────────────────────────────────────────────────────────────────────────

// TestScanWindowOwnership_ReportsAParseFailure — неразбираемый файл обязан
// сообщать об этом, а не отвечать «форм не найдено». Молчание здесь читалось бы
// как «площадка окна не держит».
func TestScanWindowOwnership_ReportsAParseFailure(t *testing.T) {
	if _, err := revocationwindowgate.ScanWindowOwnership("broken.go", "package ((("); err == nil {
		t.Fatal("неразбираемый файл прошёл без ошибки — тогда сломанный исходник неотличим " +
			"от исходника без окна")
	}
}

// TestOwnershipFormNames_MatchesTheNumberOfForms — перепись обязана уметь
// назвать каждую форму. Имя формы — то, чем читатель отличает «форма 1 ослепла»
// от «площадок стало меньше»; безымянная величина этого не даёт.
func TestOwnershipFormNames_MatchesTheNumberOfForms(t *testing.T) {
	if got := len(revocationwindowgate.OwnershipFormNames()); got != 2 {
		t.Fatalf("имён форм %d при двух полях WindowOwnership — перепись назовёт не то, что "+
			"посчитала", got)
	}
}

// TestScanConstructors_IsTheFirstFormOfOwnership — прежний вход и новый
// распознаватель отвечают об ОДНОМ предмете.
//
// Проба существует потому, что расхождение двух реализаций одной формы — это и
// есть класс, ради которого файл заведён: пока форму 1 читали два разных
// обхода, они и разошлись.
func TestScanConstructors_IsTheFirstFormOfOwnership(t *testing.T) {
	for name, src := range map[string]string{
		"конструктор":   srcOwnCtor,
		"дескриптор":    srcDescriptorWindow,
		"удостоверение": srcCredentialCtor,
	} {
		legacy, err := revocationwindowgate.ScanConstructors("x.go", src)
		if err != nil {
			t.Fatalf("%s: разбор: %v", name, err)
		}
		own, oerr := revocationwindowgate.ScanWindowOwnership("x.go", src)
		if oerr != nil {
			t.Fatalf("%s: разбор: %v", name, oerr)
		}
		if legacy != own.ByConstructor {
			t.Errorf("%s: ScanConstructors=%v, ScanWindowOwnership.ByConstructor=%v — две "+
				"реализации одной формы разошлись", name, legacy, own.ByConstructor)
		}
	}
}
