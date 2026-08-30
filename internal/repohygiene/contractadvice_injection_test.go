// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

// Доказательство способности гейта `TestContractAdviceNamesADeclaredVerb`
// падать — и молчать (`testing.md` §«Гейт на класс», п.2).
//
// ИНЪЕКЦИЯ ИДЁТ В ПАМЯТИ, а не по диску: корпус контрактов подаётся значением,
// поэтому проба не заводит ни репозитория, ни временного индекса и не может
// повредить чужую рабочую копию (`multi-agent-flow.md` §13 —
// «НЕПРИКОСНОВЕННОСТЬ ЧУЖОГО СОСТОЯНИЯ»). Что тот же вердикт выносится и по
// настоящему дереву, доказывает отдельное утверждение ниже: оно берёт
// НАСТОЯЩИЙ корпус и добавляет к нему один синтетический контракт.
//
// КАЖДАЯ форма записи совета инъецируется ОТДЕЛЬНО, и у каждой рядом законный
// близнец той же формы. Форма, о которой распознаватель не знает, даёт не
// красное и не зелёное, а молчание — поэтому «доказано на одной форме» здесь
// ничего не значит для остальных (`testing.md` §«Гейт на класс», п.7).

// adviceOwnerContract — контракт-сосед: он объявляет службу с глаголами, чтобы
// у инъекции был и словарь, и место, куда законно указать.
const adviceOwnerContract = `syntax = "proto3";
package kacho.cloud.probe.v1;

service NeighbourService {
  rpc Revoke (RevokeRequest) returns (Operation) {}
  rpc ListMembers (ListMembersRequest) returns (ListMembersResponse) {}
}
`

// adviceSubjectContract собирает подопытный контракт с одной строкой прозы.
func adviceSubjectContract(prose string) string {
	var b strings.Builder
	b.WriteString("syntax = \"proto3\";\npackage kacho.cloud.probe.v1;\n\nservice SubjectService {\n")
	for _, ln := range strings.Split(prose, "\n") {
		b.WriteString("  // " + ln + "\n")
	}
	b.WriteString("  rpc Block (BlockRequest) returns (Operation) {}\n")
	b.WriteString("  rpc Unblock (UnblockRequest) returns (Operation) {}\n}\n\n")
	// Сообщения объявляются НАСТОЯЩИМ образом: различение «сообщение против
	// глагола» опирается на объявление `message`, и корпус, где его нет, эту
	// ось не проверял бы вовсе.
	b.WriteString("message BlockRequest {\n  string id = 1;\n}\n")
	b.WriteString("message UnblockRequest {\n  string id = 1;\n}\n")
	return b.String()
}

func adviceRun(t *testing.T, prose string) adviceCensus {
	t.Helper()
	return auditContractAdvice([]adviceSource{
		{Rel: "proto/probe/neighbour.proto", Body: adviceOwnerContract},
		{Rel: "proto/probe/subject.proto", Body: adviceSubjectContract(prose)},
	})
}

// adviceCase — одна ось инъекции: форма записи совета, дефект и его законный
// близнец той же формы.
type adviceCase struct {
	form string
	// bad — совет, называющий глагол, которого у SubjectService нет.
	bad string
	// named — имя, которое обязано быть названо в находке.
	named string
	// good — тот же оборот, но на глаголе, который у SubjectService ЕСТЬ.
	// Без него гейт ловил бы ФОРМУ, а не существо, и первый же ложный срабат
	// его отключил бы.
	good string
}

var adviceCases = []adviceCase{
	{
		form:  "F1-en-directive",
		bad:   "A PENDING invitation cannot be blocked. Use Revoke on this service.",
		named: "Revoke",
		good:  "A PENDING invitation cannot be blocked. Use Unblock on this service.",
	},
	{
		form:  "F2-en-instead",
		bad:   "There is nothing to refuse here. Revoke the invitation instead.",
		named: "Revoke",
		good:  "There is nothing to refuse here. Unblock the membership instead.",
	},
	{
		form:  "F3-ru-directive",
		bad:   "Приглашение снимается иначе: вызовите Revoke этой же службы.",
		named: "Revoke",
		good:  "Приглашение снимается иначе: вызовите Unblock этой же службы.",
	},
	{
		form:  "F4-ru-vmesto",
		bad:   "Состояние не позволяет отказать: вместо Revoke здесь нечего снимать.",
		named: "Revoke",
		good:  "Состояние не позволяет отказать: вместо Unblock здесь нечего снимать.",
	},
	{
		form:  "F5-ru-glagol",
		bad:   "Полный состав берётся собственным пагинированным глаголом (`ListMembers`).",
		named: "ListMembers",
		good:  "Полный состав берётся собственным пагинированным глаголом (`Unblock`).",
	},
}

// Каждая форма совета: дефект — находка с координатой и именем; законный
// близнец той же формы — молчание.
func TestContractAdviceInjectionPerForm(t *testing.T) {
	// Контроль: корпус без прозы вовсе. Молчание здесь обязано быть, иначе
	// красное на инъекции пришло бы не от инъекции.
	if c := adviceRun(t, "Blocks the membership."); len(c.Findings) != 0 {
		t.Fatalf("контроль: на корпусе без совета найдено %d — красное на инъекции "+
			"пришло бы не от неё: %v", len(c.Findings), c.Findings)
	}

	for _, tc := range adviceCases {
		t.Run(tc.form, func(t *testing.T) {
			bad := adviceRun(t, tc.bad)
			if len(bad.Findings) != 1 {
				t.Fatalf("форма %s: дефект внесён, находок %d (ожидалась одна); "+
					"советов распознано %d, глаголов проверено %d, по формам %v",
					tc.form, len(bad.Findings), bad.AdviceSentences, bad.Checked, bad.ByForm)
			}
			f := bad.Findings[0]
			if f.Named != tc.named {
				t.Fatalf("форма %s: находка называет %q, а внесён совет на %q",
					tc.form, f.Named, tc.named)
			}
			if f.Form != tc.form {
				t.Fatalf("форма %s: находка приписана форме %q — сработала не та "+
					"форма, и доказательство относится не к этой оси", tc.form, f.Form)
			}
			// Координата обязана указывать в файл: находка, называющая начало
			// блока вместо строки имени, посылает читателя перечитывать разбор.
			if f.Rel != "proto/probe/subject.proto" || f.Line <= 0 {
				t.Fatalf("форма %s: находка без годной координаты: %s:%d",
					tc.form, f.Rel, f.Line)
			}
			body := adviceSubjectContract(tc.bad)
			line := strings.Split(body, "\n")[f.Line-1]
			if !strings.Contains(line, tc.named) {
				t.Fatalf("форма %s: координата %d указывает на строку %q, в которой "+
					"имени %q нет", tc.form, f.Line, line, tc.named)
			}

			good := adviceRun(t, tc.good)
			if len(good.Findings) != 0 {
				t.Fatalf("форма %s: законный близнец той же формы (совет на "+
					"объявленный глагол) объявлен находкой — гейт ловит форму, а не "+
					"существо: %v", tc.form, good.Findings[0].Describe())
			}
			// Близнец обязан быть ОСМОТРЕН, а не пропущен: молчание по причине
			// «распознаватель его не увидел» неотличимо от молчания по существу.
			if good.Checked == 0 {
				t.Fatalf("форма %s: законный близнец молчит, но и не осмотрен "+
					"(глаголов проверено 0) — это не доказательство", tc.form)
			}
		})
	}
}

// Законные близнецы, ради которых предикат сужен до совета: каждый из них
// широкий предикат «всякое имя обязано резолвиться» объявил бы находкой.
func TestContractAdviceStaysSilentOnLawfulProse(t *testing.T) {
	cases := []struct {
		name  string
		prose string
	}{
		{
			// Надгробие: автор ЧЕСТНО сказал, что глагола нет. Объявить это
			// находкой значило бы краснеть на исправленном месте.
			name: "надгробие снятого глагола",
			prose: "Поле 8 держало свободную карту данных машины — снято вместе с приёмом.\n" +
				"Отказ, стоявший здесь, отсылал к RPC `UpdateMetadata`, которого в\n" +
				"контракте нет: вызывающему называли адрес возможности, которой нет.",
		},
		{
			// Сравнительная проза: «X instead of Y» — не совет.
			name:  "instead of — сравнение, а не совет",
			prose: "Use the project-default subnet instead of network_interface_specs.",
		},
		{
			// Имя кода отказа в написании Go читается как имя глагола формой,
			// но им не является.
			name:  "имя кода gRPC в прозе",
			prose: "Create RPC — DEPRECATED; возвращает FailedPrecondition «Use Block».",
		},
		{
			// Акроним без строчных букв именем глагола в этом дереве не бывает.
			name:  "акроним",
			prose: "Do not treat it as the created resource: use the ID only when error is unset.",
		},
		{
			// Тон отказа `"<field> is immutable after <Resource>.Create"`:
			// слева ресурс, а не служба.
			name:  "Resource.Verb в тексте отказа",
			prose: "Маска с `kind` — «kind is immutable after StorageBackend.Create».",
		},
		{
			// Совет НА СОСЕДА с явно названной службой — законная и
			// применяемая в дереве форма.
			name:  "совет на соседа с названной службой",
			prose: "Полный состав берётся её глаголом (`NeighbourService.ListMembers`) — он для того и есть.",
		},
		{
			// Кириллический маркер обязан стоять на границе слова: без
			// ограждения `звать` совпадал бы внутри «назвать».
			name:  "маркер внутри другого слова",
			prose: "Это ответ на вопрос «что вообще можно назвать в `SubscriptionRequest.kinds`».",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := adviceRun(t, tc.prose)
			if len(c.Findings) != 0 {
				t.Fatalf("законная проза объявлена находкой: %s", c.Findings[0].Describe())
			}
		})
	}
}

// Имя объявленного СООБЩЕНИЯ — не имя глагола, и это различение обязано быть
// проверено С ОБЕИХ сторон.
//
// Замер по сегодняшнему дереву: это исключение не снимает НИ ОДНОГО кандидата —
// перепись с ним и без него совпадает (советов 22, глаголов проверено 18).
// Поэтому оно держится не переписью, а инъекцией: расширение, у которого нет
// доказанного входа, — «на всякий случай» и подлежит снятию (`testing.md`
// §«Гейт на класс», п.7), а расширение с доказанным входом — нет.
func TestContractAdviceTellsAMessageFromAVerb(t *testing.T) {
	// Объявленное сообщение в том же обороте — молчание.
	silent := adviceRun(t, "Состояние не позволяет отказать: вместо BlockRequest здесь нечего снимать.")
	if len(silent.Findings) != 0 {
		t.Fatalf("имя объявленного сообщения объявлено находкой: %s",
			silent.Findings[0].Describe())
	}
	// Зеркало: имя той же формы, которого дерево не объявляет НИ КАК СООБЩЕНИЕ,
	// НИ КАК ГЛАГОЛ, — находка. Без него молчание выше означало бы «эта форма
	// имени не наблюдается вовсе», а не «сообщение отличили от глагола».
	loud := adviceRun(t, "Состояние не позволяет отказать: вместо BlockRequests здесь нечего снимать.")
	if len(loud.Findings) != 1 || loud.Findings[0].Named != "BlockRequests" {
		t.Fatalf("зеркало не сработало: находок %d %v — различение «сообщение против "+
			"глагола» не доказано, оно могло просто ничего не видеть",
			len(loud.Findings), loud.Findings)
	}
}

// Имя выделяют ДВУМЯ способами, и распознаватель обязан знать оба.
//
// Форма записи ИМЕНИ ортогональна форме записи СОВЕТА: один и тот же оборот
// «use X» пишут с обратными кавычками, с квадратными скобками и голым. Скобочная
// форма — соглашение о перекрёстных ссылках в комментариях контрактов, и в этом
// дереве она ЧАСТОТНЕЕ кавычек.
//
// Цена незнания измерена, а не предположена: пока распознаватель скобок не знал,
// вне наблюдения оставались 25 советов при 22 наблюдаемых — слепая зона БОЛЬШЕ
// всей видимой полосы. Ни красного, ни зелёного она не давала: только молчание
// (`testing.md` §«Гейт на класс», п.7).
//
// Проверяется КАЖДОЕ обрамление в обе стороны: дефект — находка, законный
// близнец того же обрамления — молчание. Односторонняя проба зеленела бы на
// распознавателе, который скобки просто выбрасывает.
func TestContractAdviceReadsBothWaysOfDelimitingAName(t *testing.T) {
	cases := []struct {
		delim string
		bad   string // совет на глагол, которого у SubjectService нет
		good  string // тот же оборот на объявленном глаголе
	}{
		{"обратные кавычки", "Use `Revoke` on this service.", "Use `Unblock` on this service."},
		{"квадратные скобки", "Use [Revoke] on this service.", "Use [Unblock] on this service."},
		{"без обрамления", "Use Revoke on this service.", "Use Unblock on this service."},
	}
	for _, tc := range cases {
		t.Run(tc.delim, func(t *testing.T) {
			bad := adviceRun(t, tc.bad)
			if len(bad.Findings) != 1 || bad.Findings[0].Named != "Revoke" {
				t.Fatalf("обрамление %q: дефект внесён, находок %d %v — имя в этом "+
					"обрамлении не читается, и всё записанное так вне наблюдения",
					tc.delim, len(bad.Findings), bad.Findings)
			}
			good := adviceRun(t, tc.good)
			if len(good.Findings) != 0 {
				t.Fatalf("обрамление %q: законный близнец объявлен находкой: %s",
					tc.delim, good.Findings[0].Describe())
			}
			if good.Checked == 0 {
				t.Fatalf("обрамление %q: близнец молчит, но и не осмотрен — это не "+
					"доказательство", tc.delim)
			}
		})
	}

	// Совет на соседа в скобочной форме — законен и обязан молчать; он же
	// обязан быть ЗАЧТЁН как совет на соседа, иначе ось переписи ничего не
	// измеряет.
	near := adviceRun(t, "To get the id, use a [NeighbourService.ListMembers] request.")
	if len(near.Findings) != 0 {
		t.Fatalf("совет на соседа в скобках объявлен находкой: %s",
			near.Findings[0].Describe())
	}
	if near.CrossService != 1 {
		t.Fatalf("совет на соседа в скобках не зачтён осью переписи (CrossService=%d) "+
			"— ось молчала бы и на дереве, где такой формы нет вовсе", near.CrossService)
	}
	// Зеркало оси: тот же оборот на СВОЕЙ службе соседним советом не является.
	own := adviceRun(t, "To unblock it, use a [SubjectService.Unblock] request.")
	if own.CrossService != 0 {
		t.Fatalf("совет на СВОЮ службу зачтён как совет на соседа (CrossService=%d) — "+
			"ось не различает своё и чужое", own.CrossService)
	}
}

// Тот же вердикт — по НАСТОЯЩЕМУ дереву, а не только по корпусу в памяти.
//
// Инъекция в память доказывает суждение; она ничего не говорит о том, читает ли
// гейт дерево и тем ли образцом. Здесь берётся настоящий корпус и к нему
// добавляется ОДИН синтетический контракт: находка обязана появиться ровно одна
// и ровно в нём.
func TestContractAdviceInjectionOnTheRealCorpus(t *testing.T) {
	root := repoRoot(t)
	sources, err := loadContractAdviceSources(root)
	if err != nil {
		t.Fatalf("обход контрактов: %v", err)
	}
	if len(sources) == 0 {
		t.Fatal("настоящий корпус пуст — инъекция в него ничего не доказала бы")
	}

	before := auditContractAdvice(sources)
	t.Logf("настоящее дерево: контрактов %d, советов %d, глаголов проверено %d, "+
		"находок %d", before.Files, before.AdviceSentences, before.Checked,
		len(before.Findings))

	injected := append(append([]adviceSource{}, sources...), adviceSource{
		Rel: "proto/kacho/cloud/probe/v1/injected_service.proto",
		Body: adviceSubjectContract(
			"There is nothing to refuse here. Revoke the invitation instead."),
	})
	after := auditContractAdvice(injected)

	if after.Files != before.Files+1 {
		t.Fatalf("синтетический контракт не попал в корпус: было %d, стало %d",
			before.Files, after.Files)
	}
	if len(after.Findings) != len(before.Findings)+1 {
		t.Fatalf("инъекция в настоящее дерево не дала ровно одной новой находки: "+
			"было %d, стало %d", len(before.Findings), len(after.Findings))
	}
	var got *adviceFinding
	for i := range after.Findings {
		if after.Findings[i].Rel == "proto/kacho/cloud/probe/v1/injected_service.proto" {
			got = &after.Findings[i]
		}
	}
	if got == nil {
		t.Fatal("новая находка есть, но не в инъецированном контракте — красное " +
			"пришло не от инъекции")
	}
	if got.Named != "Revoke" {
		t.Fatalf("находка называет %q вместо %q", got.Named, "Revoke")
	}
}

// Предпосылки самого гейта: обход, который ничего не прочитал, обязан РОНЯТЬ, а
// не молчать.
//
// Без этого «ноль находок» неотличимо от «ноль прочитанного» — тот самый класс,
// который весь корпус и ловит.
func TestContractAdviceRefusesAnEmptyTraversal(t *testing.T) {
	t.Run("корпус пуст", func(t *testing.T) {
		c := auditContractAdvice(nil)
		if c.Files != 0 || c.Blocks != 0 {
			t.Fatalf("пустой корпус дал непустую перепись: %+v", c)
		}
	})
	t.Run("только чужие контракты", func(t *testing.T) {
		c := auditContractAdvice([]adviceSource{{
			Rel:  "proto/google/api/http.proto",
			Body: "syntax = \"proto3\";\npackage google.api;\n// Use SomeVerb instead.\n",
		}})
		if c.Files != 0 {
			t.Fatalf("чужой контракт зачтён в свои: %d", c.Files)
		}
		if len(c.Foreign) != 1 {
			t.Fatalf("чужой контракт не назван в переписи: %v", c.Foreign)
		}
		if len(c.Findings) != 0 {
			t.Fatalf("чужой контракт судится: %v", c.Findings)
		}
	})
	t.Run("свой контракт без служб опирается на пакет каталога", func(t *testing.T) {
		// Файл без служб (`user.proto`, `role.proto`) охватывающей службы не
		// имеет; без этой ступени всякое голое имя в нём стало бы находкой.
		c := auditContractAdvice([]adviceSource{
			{Rel: "proto/probe/neighbour.proto", Body: adviceOwnerContract},
			{Rel: "proto/probe/message_only.proto", Body: "syntax = \"proto3\";\n" +
				"package kacho.cloud.probe.v1;\n\n" +
				"message Thing {\n  // Use Revoke on the owning service.\n  string id = 1;\n}\n"},
		})
		if len(c.Findings) != 0 {
			t.Fatalf("файл без служб судится в отрыве от своего каталога: %s",
				c.Findings[0].Describe())
		}
		if c.Checked == 0 {
			t.Fatal("файл без служб не осмотрен вовсе — молчание здесь ничего не значит")
		}
	})
}
