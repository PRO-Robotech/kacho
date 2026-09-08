// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Инъекция вердикта «у держателя окна есть СВОЯ ручка» — В ОБЕ СТОРОНЫ.
//
// # Почему инъекцией, а не наблюдением
//
// Утверждение сегодня МОЛЧИТ: все восемь держателей дерева свою ручку имеют,
// «держателей без ручки» и «ручек без держателя» по нулю. Зелёный прогон над
// таким деревом не отличим от прогона над мёртвой проверкой — оба печатают ноль
// находок. Поэтому способность падать доказывается поданным входом, а не
// наблюдением, и по КАЖДОЙ стороне отдельно: односторонняя проверка здесь
// переживает свой предмет молча.
//
// # Что именно доказывается, и чего доказать нельзя
//
// Доказывается: держатель без ручки — находка; тот же держатель с ручкой —
// молчание (законный близнец, дельта в ОДИН факт); ручка без держателя —
// находка; и чистый мир — молчание обеих сторон сразу. Плюс — что находка
// называет ФОРМУ владения и что выход, который она предлагает, исполним.
//
// Не доказывается и не может: что обход дерева находит всех держателей. Это
// предмет ScanWindowOwnership и его собственной инъекции; здесь вход подан
// напрямую, и вопрос уже другой.
package revocationwindowgate

import (
	"strings"
	"testing"
)

// ctor / desc — две формы владения окном. Инъекция гоняется по обеим: вердикт от
// формы не зависит, и это утверждение тоже надо проверить, а не подразумевать.
var (
	ctor = WindowOwnership{ByConstructor: true}
	desc = WindowOwnership{ByDescriptor: true}
)

// TestJudgeOwnKnobs_HolderWithoutKnobIsAFinding — держатель, чьей ручки политика
// не объявляет, назван находкой.
//
// Это ровно то состояние, которое до 2026-09-08 карта Inherited объявляла
// ЗАКОННЫМ, оставаясь находкой здесь. Теперь у него один вердикт, и вот он.
func TestJudgeOwnKnobs_HolderWithoutKnobIsAFinding(t *testing.T) {
	held := map[string]WindowOwnership{"alpha": ctor, "api-gateway": desc}
	v := JudgeOwnKnobs(held, map[string]bool{"api-gateway": true})

	if len(v.HoldersWithoutKnob) != 1 || v.HoldersWithoutKnob[0] != "alpha" {
		t.Fatalf("держатель окна без своей ручки НЕ назван находкой: %v.\n"+
			"Окно такого процесса принадлежит платформе — оператор не может сузить его на "+
			"своей посадке, и в конфигурации процесса о нём нет ни строки",
			v.HoldersWithoutKnob)
	}
	if v.Clean() {
		t.Errorf("вердикт объявлен чистым при непустой находке")
	}
}

// TestJudgeOwnKnobs_DeclaredHolderIsSilent — законный близнец предыдущего мира.
//
// Дельта против него — ОДИН факт: у «alpha» появилась объявленная ручка. Всё
// остальное совпадает дословно; без этой пары отрицание выше зеленело бы и на
// вердикте, объявляющем находкой каждого.
func TestJudgeOwnKnobs_DeclaredHolderIsSilent(t *testing.T) {
	held := map[string]WindowOwnership{"alpha": ctor, "api-gateway": desc}
	v := JudgeOwnKnobs(held, map[string]bool{"alpha": true, "api-gateway": true})

	if !v.Clean() {
		t.Fatalf("держатель со СВОЕЙ ручкой назван находкой: без ручки=%v, без держателя=%v",
			v.HoldersWithoutKnob, v.KnobsWithoutHolder)
	}
}

// TestJudgeOwnKnobs_VerdictDoesNotDependOnTheFormOfOwnership — вердикт один для
// обеих форм владения.
//
// Шесть площадок дерева из восьми не строят кеша вовсе — они называют величину
// дескриптору носителя. Вердикт, умеющий судить лишь строящих, был бы слеп к
// большинству, и слеп молча.
func TestJudgeOwnKnobs_VerdictDoesNotDependOnTheFormOfOwnership(t *testing.T) {
	for name, own := range map[string]WindowOwnership{
		OwnershipFormNames()[0]: ctor,
		OwnershipFormNames()[1]: desc,
	} {
		t.Run(name, func(t *testing.T) {
			v := JudgeOwnKnobs(map[string]WindowOwnership{"beta": own}, map[string]bool{})
			if len(v.HoldersWithoutKnob) != 1 || v.HoldersWithoutKnob[0] != "beta" {
				t.Fatalf("держатель формы «%s» без ручки не назван находкой: %v",
					name, v.HoldersWithoutKnob)
			}
		})
	}
}

// TestJudgeOwnKnobs_KnobWithoutHolderIsAFinding — обратная сторона: перепись,
// пережившая свой предмет.
//
// Без неё запись Windows, чей процесс переименован или лишился кеша, осталась бы
// зелёной, описывая дерево, которого нет.
func TestJudgeOwnKnobs_KnobWithoutHolderIsAFinding(t *testing.T) {
	v := JudgeOwnKnobs(
		map[string]WindowOwnership{"alpha": ctor},
		map[string]bool{"alpha": true, "ушедший": true},
	)
	if len(v.KnobsWithoutHolder) != 1 || v.KnobsWithoutHolder[0] != "ушедший" {
		t.Fatalf("объявленная ручка без держателя НЕ названа находкой: %v", v.KnobsWithoutHolder)
	}
	if len(v.HoldersWithoutKnob) != 0 {
		t.Errorf("сторона «держатель без ручки» сработала на чужом мире: %v", v.HoldersWithoutKnob)
	}
}

// TestJudgeOwnKnobs_CleanWorldIsSilent — положительный контроль обеих сторон
// сразу: сошедшиеся переписи молчат.
//
// Он и есть тот мир, который дерево показывает сегодня, — и именно поэтому
// зелёный прогон гейта ничего не доказывает без остальных проб этого файла.
func TestJudgeOwnKnobs_CleanWorldIsSilent(t *testing.T) {
	held := map[string]WindowOwnership{"alpha": ctor, "beta": desc}
	v := JudgeOwnKnobs(held, map[string]bool{"alpha": true, "beta": true})
	if !v.Clean() {
		t.Fatalf("чистый мир объявлен находкой: без ручки=%v, без держателя=%v",
			v.HoldersWithoutKnob, v.KnobsWithoutHolder)
	}
}

// TestJudgeOwnKnobs_EmptyInputIsNotAVerdict — пустой вход НЕ выдаётся за «чисто».
//
// Пустая перепись держателей означает, что обход ничего не измерил, и вердикт о
// ней беспредметен. Функция об этом не судит намеренно — предпосылку обхода
// держит holderCensusVacuity на стороне пробы; проба здесь закрепляет ШОВ, чтобы
// следующий не перенёс предпосылку сюда и не завёл два места об одном предмете.
func TestJudgeOwnKnobs_EmptyInputIsNotAVerdict(t *testing.T) {
	v := JudgeOwnKnobs(map[string]WindowOwnership{}, map[string]bool{})
	if !v.Clean() {
		t.Fatalf("вердикт на пустом входе непуст: %v / %v",
			v.HoldersWithoutKnob, v.KnobsWithoutHolder)
	}
	// Читается так: «чисто» здесь не значит «проверено». Отличать обязан
	// вызывающий, и он это делает — assertHolderCensusIsNotVacuous.
}

// TestHolderWithoutKnobFinding_NamesTheFormAndAnExecutableWayOut — находка
// называет форму владения и выход, который ИСПОЛНИМ.
//
// Обе половины были дефектны разом. Прежний текст не называл формы — читатель
// шёл искать вызов конструктора там, где его нет вовсе. А выход он предлагал
// двойной: «либо ручка, либо записать исключение осознанно», — при том что
// ведомости исключений не существовало ни одной, а единственное место, куда
// «исключение» указывало (карта Inherited), этим же гейтом делало площадку
// находкой. Текст отказа, советующий недостижимое, читают ровно в ту минуту,
// когда ищут, что делать дальше.
func TestHolderWithoutKnobFinding_NamesTheFormAndAnExecutableWayOut(t *testing.T) {
	msg := HolderWithoutKnobFinding("alpha", OwnershipFormNames()[1])

	if !strings.Contains(msg, OwnershipFormNames()[1]) {
		t.Errorf("находка не называет ФОРМУ владения: %q", msg)
	}
	if !strings.Contains(msg, "alpha") {
		t.Errorf("находка не называет процесс: %q", msg)
	}
	if strings.Contains(msg, "Inherited") {
		t.Errorf("находка предлагает снятую карту Inherited — выход, которого нет: %q", msg)
	}
	if !strings.Contains(msg, "ведомости исключений") {
		t.Errorf("находка не говорит, что второго выхода нет; читатель пойдёт искать "+
			"ведомость исключений, которой у этого правила не заведено: %q", msg)
	}
}
