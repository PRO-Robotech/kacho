// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package blockbackend_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/services/storage/internal/blockbackend"
)

// Приёмка STOR-P-21 (имя объекта детерминировано и выводится),
// STOR-P-22 (префикс установки обязателен), STOR-P-60 (классификация без корзины).

func TestObjectName_DeterministicAndPrefixed(t *testing.T) {
	t.Parallel()

	const prefix, id = "kc7f", "vol0a7b3c9d2e5f8g1hj"

	first := blockbackend.ObjectName(prefix, id)
	second := blockbackend.ObjectName(prefix, id)
	if first != second {
		t.Fatalf("имя обязано быть детерминированным: %q против %q", first, second)
	}
	if !strings.HasPrefix(first, prefix+"-") {
		t.Errorf("имя обязано нести префикс установки: %q", first)
	}
	if !strings.Contains(first, id) {
		t.Errorf("имя обязано выводиться из неизменяемого идентификатора: %q", first)
	}

	// Разные развёртывания на одном кластере обязаны получать РАЗНЫЕ имена —
	// иначе они усыновят объекты друг друга: сверщик каждого посчитает чужие
	// объекты своей утечкой, а удаление в одном снесёт данные в другом.
	other := blockbackend.ObjectName("prod01", id)
	if other == first {
		t.Error("два развёртывания обязаны выводить разные имена для одного ресурса")
	}
}

func TestValidateInstallPrefix_PairedControls(t *testing.T) {
	t.Parallel()

	for _, good := range []string{"kc", "kc7f", "prod01", "a1"} {
		if err := blockbackend.ValidateInstallPrefix(good); err != nil {
			t.Errorf("законный префикс %q отвергнут: %v", good, err)
		}
	}
	for _, bad := range []string{
		"",                    // пусто — предмет отдельной пробы ниже
		"K",                   // одна буква и заглавная
		"kc_7f",               // подчёркивание
		"7kc",                 // начинается с цифры
		"kacho-storage-block", // длиннее допустимого
		"КЦ",                  // не ASCII
	} {
		if err := blockbackend.ValidateInstallPrefix(bad); err == nil {
			t.Errorf("префикс %q не соответствует форме и обязан отвергаться", bad)
		}
	}
}

func TestValidateInstallPrefix_EmptyNamesTheConsequence(t *testing.T) {
	t.Parallel()

	err := blockbackend.ValidateInstallPrefix("")
	if err == nil {
		t.Fatal("пустой префикс обязан отвергаться")
	}
	// Сообщение обязано называть ПОСЛЕДСТВИЕ, а не только факт: иначе следующий
	// читатель снимет требование как непонятное.
	if !strings.Contains(err.Error(), "adopt") {
		t.Errorf("отказ обязан объяснять, чем это кончится: %v", err)
	}
}

func TestNamespaceOfProject(t *testing.T) {
	t.Parallel()

	const project = "prj-4kq2n8xr1vm0d3c7f"

	// Пустой шаблон — сам идентификатор проекта: изоляция всё равно есть.
	if got := blockbackend.NamespaceOfProject("", project); got != project {
		t.Errorf("пустой шаблон обязан давать идентификатор проекта, получено %q", got)
	}
	if got := blockbackend.NamespaceOfProject("t-{projectId}", project); got != "t-"+project {
		t.Errorf("подстановка не выполнена: %q", got)
	}
	// Шаблон без подстановки — законен: администратор вправе задать общее
	// пространство. Молча дописывать в него идентификатор нельзя.
	if got := blockbackend.NamespaceOfProject("shared", project); got != "shared" {
		t.Errorf("шаблон без подстановки обязан оставаться собой, получено %q", got)
	}
	// Несколько вхождений подставляются все: частичная подстановка дала бы
	// пространство, в имени которого остался незаполненный шаблон.
	if got := blockbackend.NamespaceOfProject("{projectId}-{projectId}", project); got != project+"-"+project {
		t.Errorf("подставлены не все вхождения: %q", got)
	}
}

func TestOutcome_ClosedSetWithoutCatchAll(t *testing.T) {
	t.Parallel()

	// Нулевое значение — СОСТОЯНИЕ «не классифицировано», а не молчаливый выбор
	// политики повтора.
	var zero blockbackend.Outcome
	if zero != blockbackend.OutcomeUnclassified {
		t.Fatal("нулевое значение обязано означать «не классифицировано»")
	}
	if zero.Retryable() {
		t.Error("неклассифицированный исход не повторяется: политика не выводится из незнания")
	}

	// Повторяем ровно один исход. Положительный и отрицательный контроль рядом.
	if !blockbackend.OutcomeUnavailable.Retryable() {
		t.Error("недоступность обязана быть повторяемой")
	}
	for _, o := range []blockbackend.Outcome{
		blockbackend.OutcomeRejected,
		blockbackend.OutcomeCapacityExhausted,
		blockbackend.OutcomeNotFound,
		blockbackend.OutcomeConflict,
		blockbackend.OutcomeDenied,
		blockbackend.OutcomeMisconfigured,
	} {
		if o.Retryable() {
			t.Errorf("исход %s обязан быть терминальным: повтор идентичного запроса даст тот же ответ", o)
		}
	}

	// Отказ в правах — НЕ временный. Трактовка его как временного уже стоила
	// продукту очереди, в которой за всю жизнь не доехало ни одной строки.
	if blockbackend.OutcomeDenied.Retryable() {
		t.Error("отказ в правах не может быть временным")
	}

	// Значение вне набора — находка, а не «что-то новое».
	if blockbackend.Outcome(99).Known() {
		t.Error("значение вне объявленного набора обязано опознаваться как неизвестное")
	}
	if !blockbackend.OutcomeMisconfigured.Known() {
		t.Error("объявленное значение обязано опознаваться")
	}
}

func TestOutcomeOf_AbsentClassificationIsNotAnAssumption(t *testing.T) {
	t.Parallel()

	// Ошибка без полосы не превращается в допущение о полосе.
	if got := blockbackend.OutcomeOf(errors.New("что-то пошло не так")); got != blockbackend.OutcomeUnclassified {
		t.Errorf("непомеченная ошибка обязана быть неклассифицированной, получено %s", got)
	}
	if got := blockbackend.OutcomeOf(nil); got != blockbackend.OutcomeUnclassified {
		t.Errorf("nil обязан давать неклассифицированное, получено %s", got)
	}

	// Полоса извлекается сквозь обёртку: классификация не теряется от того, что
	// вызывающий добавил контекст.
	inner := blockbackend.Errorf(blockbackend.OutcomeCapacityExhausted, "CreateVolume", "kc7f-vol1", nil)
	wrapped := fmt.Errorf("создание тома: %w", inner)
	if got := blockbackend.OutcomeOf(wrapped); got != blockbackend.OutcomeCapacityExhausted {
		t.Errorf("полоса обязана извлекаться сквозь обёртку, получено %s", got)
	}
}

func TestError_CarriesBackendTextForOperatorNotForTenant(t *testing.T) {
	t.Parallel()

	backendSaid := errors.New("pool kacho-block-balanced is full at 98%")
	err := blockbackend.Errorf(blockbackend.OutcomeCapacityExhausted, "CreateVolume", "kc7f-vol1", backendSaid)

	// Исходный текст СОХРАНЁН — он нужен оператору в журнале.
	if !errors.Is(err, backendSaid) {
		t.Error("исходная ошибка обязана быть доступна разворачиванием: она адресована оператору")
	}
	// …и он обязан остаться внутри: наружу арендатору текст выдаёт край по полосе,
	// из фиксированной таблицы. Здесь проверяется лишь то, что полоса опознаётся —
	// сам запрет на выдачу держится на крае и проверяется его пробами.
	if blockbackend.OutcomeOf(err) != blockbackend.OutcomeCapacityExhausted {
		t.Error("полоса обязана быть машинно читаемой, иначе край не сможет выбрать текст")
	}
}

func TestObservedState_UnknownIsNotAbsent(t *testing.T) {
	t.Parallel()

	// Разница несущая: недоступность бэкенда не является утверждением об
	// отсутствии объекта. Слив их в одно значение, сверщик объявил бы живой том
	// пропавшим на первой же сетевой неполадке.
	if blockbackend.ObservedUnknown == blockbackend.ObservedAbsent {
		t.Fatal("«не установлено» и «отсутствует» обязаны быть разными состояниями")
	}
	if blockbackend.ObservedUnknown.String() != "UNKNOWN" || blockbackend.ObservedAbsent.String() != "ABSENT" {
		t.Error("имена состояний обязаны совпадать с теми, что принимает колонка БД")
	}
}
