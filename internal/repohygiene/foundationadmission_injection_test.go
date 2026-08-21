// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// foundationadmission_injection_test.go — доказательство того, что перепись
// усыновления краснеет и молчит ИМЕННО НА ПОТОЛКЕ ТЕМПА.
//
// # Зачем отдельно от соседней инъекции
//
// Соседний файл доказывает МЕХАНИЗМ переписи и выбирает возможность по
// содержанию набора — то есть первую, считаемую по месту сборки. Сегодня это
// восстановление после паники. Значит про потолок темпа он не утверждает
// ничего: возможность можно было бы вынуть из набора целиком, и там всё
// осталось бы зелёным.
//
// Предмет здесь — ровно тот, ради которого заведена задача #692: слушатель,
// потолка НЕ несущий, обязан быть находкой, а не тишиной. Слушатель этот —
// БУДУЩИЙ: тот, кого заведут следующим. Поэтому вход синтетический, а не
// вырезанный из дерева: настоящее дерево отвечает на вопрос «а сейчас все ли
// провязали», и на него отвечает сама перепись; здесь спрашивается «а поймает ли
// она следующего».
//
// # Инъекция в обе стороны, по каждому исходу клетки — семь утверждений
//
//	краснеет · новый слушатель без потолка — находка НАЗЫВАЕТ его координату
//	молчит   · сосед, потолок провязавший, находкой не становится
//	молчит   · непровязавший под ЗАПИСАННЫМ пропуском с номером задачи
//	молчит   · каталог, где потолка нет НИ У ОДНОГО места, — под записанным
//	           отсутствием предмета
//	краснеет · пропуск, чья возможность УЖЕ усыновлена всеми названными им
//	           местами, — истёкшая запись
//	краснеет · запись «предмета нет», опровергнутая усыновлением
//	краснеет · пропуск БЕЗ номера задачи — долг без задачи неотличим от забытого
package repohygiene

import (
	"strings"
	"testing"
)

// admCapability — возможность потолка, взятая ИЗ НАСТОЯЩЕГО НАБОРА и опознанная
// по СИМВОЛУ МЕХАНИЗМА, а не по отображаемому имени.
//
// Имя — то, что правят при первой же перефразировке; символ — то, чем провязка
// делается. Если возможность из набора уберут, эта проба обязана упасть громко:
// её предмет исчез, и молчание означало бы, что потолок больше никем не
// наблюдается, — ровно то состояние, из которого #692 и заводилась.
func admCapability(t *testing.T) FoundationCapability {
	t.Helper()
	const mechanism = "grpcsrv.NewAdmission"
	for _, c := range foundationRoster().Capabilities {
		for _, sym := range c.Symbols {
			if sym == mechanism {
				return c
			}
		}
	}
	t.Fatalf("в наборе обязательных возможностей нет ни одной, опознаваемой символом %q: "+
		"потолок темпа на арендатора перестал наблюдаться переписью. Это не повод пропустить "+
		"пробу — либо возможность переименовали (тогда правьте символ здесь), либо её сняли "+
		"с набора, и тогда следующий слушатель поднимется без потолка молча", mechanism)
	return FoundationCapability{}
}

// admTree — синтетическое дерево с ДВУМЯ независимо собранными серверами в
// ОДНОМ каталоге; какой из них провязал потолок, задаётся флагами.
//
// Пара в одном каталоге не случайна: она и проверяет, что единица счёта — место
// сборки, а не каталог. По каталогу «провязал один из двух» читалось бы как
// «усыновили оба» — ровно та слепота, из-за которой единицу счёта и меняли.
//
// Тела подаются как есть — перепись разбирает их синтаксисом, а не текстом.
func admTree(t *testing.T, wireFirst, wireSecond bool) string {
	t.Helper()
	first := "\tpubSrv := grpc.NewServer()\n"
	if wireFirst {
		first = "\tpubAdm, _ := grpcsrv.NewAdmission(\"public\", nil, nil)\n" +
			"\tpubSrv := grpc.NewServer()\n" +
			"\tregisterPublic(pubAdm.Registrar(pubSrv))\n"
	}
	second := "\tintSrv := grpc.NewServer()\n"
	if wireSecond {
		second = "\tintAdm, _ := grpcsrv.NewAdmission(\"internal\", nil, nil)\n" +
			"\tintSrv := grpc.NewServer()\n" +
			"\tregisterInternal(intAdm.Registrar(intSrv))\n"
	}
	return injTree(t, map[string]string{
		// Фундамент: механизм объявлен здесь. Каталог обязан существовать —
		// перепись проверяет это отдельной предпосылкой.
		"pkg/grpcsrv/admission.go": "package grpcsrv\n\nfunc NewAdmission(a string, b, c any) (any, error) { return nil, nil }\n",
		// Новый слушатель: два независимо собранных сервера в одном каталоге.
		"services/newbie/main.go": "package main\n\nimport (\n\t\"google.golang.org/grpc\"\n" +
			"\t\"x/pkg/grpcsrv\"\n)\n\n" +
			"func registerPublic(r any)   {}\nfunc registerInternal(r any) {}\n\n" +
			"func main() {\n" +
			first +
			second +
			"\t_, _ = pubSrv, intSrv\n}\n",
	})
}

// admRoster — набор из ОДНОЙ возможности (настоящей) без посредников и обёрток:
// на синтетическом дереве остальные четыре дали бы находки, не относящиеся к
// предмету этой пробы, и вердикт перестал бы быть о потолке.
func admRoster(t *testing.T, ledger []FoundationLedgerEntry, noSubject []FoundationNoSubject) FoundationRoster {
	t.Helper()
	return FoundationRoster{
		Capabilities: []FoundationCapability{admCapability(t)},
		Ledger:       ledger,
		NoSubject:    noSubject,
	}
}

// admCensus — вердикт переписи по синтетическому дереву с проверкой предпосылки:
// мест сборки обязано быть два. Ноль или одно означало бы, что вход построен не
// тот, и любое утверждение ниже говорило бы не о том, о чём заявлено.
func admCensus(t *testing.T, wireFirst, wireSecond bool, ledger []FoundationLedgerEntry,
	noSubject []FoundationNoSubject) FoundationCensus {

	t.Helper()
	r := admRoster(t, ledger, noSubject)
	cen := injRun(t, admTree(t, wireFirst, wireSecond), r)
	if len(cen.Sites) != 2 {
		t.Fatalf("вход построен неверно: мест сборки %d вместо двух (%v). Без ПАРЫ мест "+
			"в одном каталоге проба не отличает «поймала виновника» от «покраснела на "+
			"каталоге»", len(cen.Sites), cen.Sites)
	}
	return cen
}

// TestAdmissionGateRedensOnANewListenerWithoutTheCeiling — КРАСНАЯ сторона.
//
// Новый слушатель поднимает два сервера; потолок провязан у одного. Ожидание
// точное: ровно одна находка, и она называет ПОСТРАДАВШЕЕ место с координатой
// файла — иначе среди двух серверов одного каталога виновника не найти.
func TestAdmissionGateRedensOnANewListenerWithoutTheCeiling(t *testing.T) {
	cen := admCensus(t, true, false, nil, nil)

	if len(cen.Findings) != 1 {
		t.Fatalf("слушатель без потолка дал %d находок вместо одной: %s\n%v",
			len(cen.Findings), cen, cen.Findings)
	}
	f := cen.Findings[0]
	if !strings.Contains(f.Listener, "intSrv") {
		t.Fatalf("находка называет %q, а потолка нет у ВТОРОГО сервера (intSrv): "+
			"координата уводит не туда", f.Listener)
	}
	if !strings.Contains(f.Detail, "main.go:") {
		t.Fatalf("текст находки не несёт координаты файла и строки: %q", f.Detail)
	}
	if cen.Carried != 1 {
		t.Fatalf("уцелевшее место перестало считаться усыновившим: %s — проба "+
			"покраснела на всём и виновника не различает", cen)
	}

	// ЗАКОННЫЙ БЛИЗНЕЦ на том же входе: провязали второй — находок ноль.
	// Без него красное выше было бы неотличимо от «перепись краснеет на всяком
	// синтетическом дереве».
	twin := admCensus(t, true, true, nil, nil)
	if len(twin.Findings) != 0 || twin.Carried != 2 {
		t.Fatalf("перепись краснеет на ЗАКОННОМ входе (потолок у обоих мест): %s\n%v",
			twin, twin.Findings)
	}
	t.Logf("без потолка: %s | с потолком у обоих: %s", f.Detail, twin)
}

// TestAdmissionGateStaysSilentOnARecordedExemption — МОЛЧАЩАЯ сторона.
//
// У клетки три законных исхода, и оба неусыновляющих проверяются здесь: пропуск
// с номером задачи и записанное отсутствие предмета. Проверять надо ОБА — они
// значат разное («работы ещё нет» против «работы не предвидится»), и запись,
// принятая переписью не по своему смыслу, стала бы маской.
func TestAdmissionGateStaysSilentOnARecordedExemption(t *testing.T) {
	cap := admCapability(t)

	t.Run("пропуск с номером задачи", func(t *testing.T) {
		cen := admCensus(t, true, false, []FoundationLedgerEntry{{
			Capability: cap.Name, Listener: "services/newbie", Issue: 692,
			Why: "новый слушатель ещё не провязал потолок; работа заведена задачей",
		}}, nil)
		if len(cen.Findings) != 0 || len(cen.Stale) != 0 {
			t.Fatalf("записанный пропуск не признан: %s\nнаходки: %v\nистёкшие: %v",
				cen, cen.Findings, cen.Stale)
		}
		if cen.Excused != 1 {
			t.Fatalf("пропуском учтено %d клеток вместо одной: %s — запись, названная "+
				"каталогом, обязана покрывать ИМЕННО непровязавшее место, а не оба",
				cen.Excused, cen)
		}
	})

	t.Run("записанное отсутствие предмета", func(t *testing.T) {
		// Запись «предмета нет» названа КАТАЛОГОМ и опровергается усыновлением
		// ХОТЯ БЫ ОДНОГО его места — и это верно по существу: «работы не
		// предвидится» несовместимо с «сосед уже сделал». Поэтому вход здесь —
		// каталог, где потолка нет НИ У ОДНОГО места.
		cen := admCensus(t, false, false, nil, []FoundationNoSubject{{
			Capability: cap.Name, Listener: "services/newbie",
			Why: "слушатель не принимает запросов арендатора — ограничивать нечего",
		}})
		if len(cen.Findings) != 0 || len(cen.Stale) != 0 {
			t.Fatalf("записанное отсутствие предмета не признано: %s\nнаходки: %v\nистёкшие: %v",
				cen, cen.Findings, cen.Stale)
		}
	})
}

// TestAdmissionExemptionSelfExpires — послабление истекает САМО.
//
// Обе стороны: запись, которой больше нечего исключать, — находка; запись без
// номера задачи — тоже. Первая не даёт ведомости пережить свой предмет и ставить
// в очередь работу, которой не требуется (ровно так пропуск края и жил бы после
// #692, если бы его не сняли). Вторая не даёт долгу быть неотличимым от забытого.
func TestAdmissionExemptionSelfExpires(t *testing.T) {
	cap := admCapability(t)

	t.Run("нечего исключать", func(t *testing.T) {
		// Потолок провязан у ОБОИХ мест, а пропуск на каталог всё ещё записан.
		cen := admCensus(t, true, true, []FoundationLedgerEntry{{
			Capability: cap.Name, Listener: "services/newbie", Issue: 692,
			Why: "предмет закрыт, а запись осталась",
		}}, nil)
		if len(cen.Stale) != 1 {
			t.Fatalf("пропуск, чью возможность усыновили ВСЕ названные им места, обязан "+
				"быть находкой: %s\nистёкшие: %v", cen, cen.Stale)
		}
		if !strings.Contains(cen.Stale[0], "нечего исключать") {
			t.Fatalf("текст находки не называет причину: %q", cen.Stale[0])
		}
	})

	t.Run("отсутствие предмета опровергнуто деревом", func(t *testing.T) {
		cen := admCensus(t, true, true, nil, []FoundationNoSubject{{
			Capability: cap.Name, Listener: "services/newbie",
			Why: "якобы ограничивать нечего",
		}})
		if len(cen.Stale) != 1 || !strings.Contains(cen.Stale[0], "опровергнута деревом") {
			t.Fatalf("запись «предмета нет», опровергнутая усыновлением, обязана быть "+
				"находкой: %s\nистёкшие: %v", cen, cen.Stale)
		}
	})

	t.Run("пропуск без номера задачи", func(t *testing.T) {
		cen := admCensus(t, true, false, []FoundationLedgerEntry{{
			Capability: cap.Name, Listener: "services/newbie",
			Why: "потом провяжем",
		}}, nil)
		if len(cen.Stale) != 1 || !strings.Contains(cen.Stale[0], "не называет задачи") {
			t.Fatalf("пропуск без номера задачи обязан быть находкой: %s\nистёкшие: %v",
				cen, cen.Stale)
		}
	})
}
