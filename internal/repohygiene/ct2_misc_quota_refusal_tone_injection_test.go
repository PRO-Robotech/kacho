// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Инъекция для гейта тона отказа по пределу — В ОБЕ СТОРОНЫ.
//
// Гейт, чью способность падать не доказали, неотличим от вечно-зелёного: на
// чистом дереве он выглядит точно так же. Поэтому здесь: (а) возвращённый
// НАСТОЯЩИЙ дефект обязан дать находку и назвать координату; (б) каждая
// законная форма записи обязана МОЛЧАТЬ; (в) пустой обход обязан быть отличим
// от «нарушений нет».
//
// Дефект возвращается настоящий — тот самый закрытый перечень префиксов,
// который стоял в `services/nlb/.../shared/errmap.go` до задачи продукта #1658.

// ct2ToneStripperBodies — тела стриппера: один дефект и три законные формы.
//
// Формы перечислены ВСЕ, какие есть в дереве: распознаватель, знающий одну,
// оставил бы записанное остальными вне наблюдения — не в нарушителях, а в
// невидимости (`testing.md` §«Гейт на класс», п.7).
var ct2ToneStripperBodies = map[string]string{
	// ДЕФЕКТ: перечень префиксов выписан строками, связи с sentinel'ами нет.
	"перечень-строк": `
func stripIt(err error, sentinel error) string {
	msg := err.Error()
	prefixes := []string{"not found: ", "already exists: "}
	for _, p := range prefixes {
		if strings.HasPrefix(msg, p) {
			return msg[len(p):]
		}
	}
	return msg
}`,
	// ЗАКОННАЯ ФОРМА 1 — пять владельцев: префикс от НАЗВАННОГО sentinel'а.
	"склейка-на-месте": `
func stripIt(err error, sentinel error) string {
	msg := err.Error()
	prefix := sentinel.Error() + ": "
	if rest, ok := strings.CutPrefix(msg, prefix); ok {
		if rest == "" {
			return sentinel.Error()
		}
		return rest
	}
	return msg
}`,
	// ЗАКОННАЯ ФОРМА 2 — шестой владелец: перечень SENTINEL'ОВ, префикс всё
	// равно выводится вызовом Error().
	"перечень-sentinel": `
func stripIt(err error, sentinel error) string {
	msg := err.Error()
	for _, s := range []error{errNotFound, errQuota} {
		if rest, ok := strings.CutPrefix(msg, s.Error()+": "); ok {
			if rest == "" {
				return s.Error()
			}
			return rest
		}
	}
	return msg
}`,
	// ЗАКОННАЯ ФОРМА 3 — вывод через переменную, а не склейкой на месте.
	"через-переменную": `
func stripIt(err error, sentinel error) string {
	fallback := ""
	if sentinel != nil {
		fallback = sentinel.Error()
	}
	msg := err.Error()
	if rest, ok := strings.CutPrefix(msg, fallback+": "); ok {
		if rest == "" {
			return fallback
		}
		return rest
	}
	return msg
}`,
	// ЗАКОННАЯ ФОРМА 4 — ограждение остатка сравнением ДЛИНЫ, а не строки.
	"ограждение-длиной": `
func stripIt(err error, sentinel error) string {
	msg := err.Error()
	if rest, ok := strings.CutPrefix(msg, sentinel.Error()+": "); ok {
		if len(rest) == 0 {
			return sentinel.Error()
		}
		return rest
	}
	return msg
}`,
	// НАХОДКА ОСИ 2: префикс выводится, пустой остаток НЕ ограждён.
	"остаток-без-ограждения": `
func stripIt(err error, sentinel error) string {
	msg := err.Error()
	if rest, ok := strings.CutPrefix(msg, sentinel.Error()+": "); ok {
		return rest
	}
	return msg
}`,
	// НАХОДКА ОСИ 2, ЛОЖНЫЙ БЛИЗНЕЦ: сравнение с пустой строкой ЕСТЬ, но
	// относится ко всему сообщению, а не к остатку. Широкий распознаватель
	// объявил бы это огражденным — ошибка в опасную сторону.
	"ограждение-не-того": `
func stripIt(err error, sentinel error) string {
	msg := err.Error()
	if msg == "" {
		return sentinel.Error()
	}
	if rest, ok := strings.CutPrefix(msg, sentinel.Error()+": "); ok {
		return rest
	}
	return msg
}`,
	// НАХОДКА ПЕРВОГО РОДА: стриппер есть, префикса не выводит ниоткуда.
	"префикс-ниоткуда": `
func stripIt(err error, sentinel error) string {
	return err.Error()
}`,
	// ЗАКОННАЯ ФОРМА 5 — вывод и срезка вынесены в ПОМОЩНИКА того же пакета,
	// ограждение осталось у стриппера. Так устроен iam после того, как имя
	// признака стало сниматься и из СЕРЕДИНЫ текста: обход цепочки отказа
	// вынесен в отдельные функции, а `StripSentinel` их зовёт. Распознаватель,
	// смотрящий только в тело названной функции, объявляет это «префиксом
	// ниоткуда» — то есть находкой там, где свойство выполняется.
	"вывод-в-помощнике": `
func stripIt(err error, sentinel error) string {
	msg := err.Error()
	s, rest, ok := cutIt(msg)
	if !ok {
		return msg
	}
	if rest == "" {
		return s.Error()
	}
	return rest
}

func cutIt(msg string) (error, string, bool) {
	for _, s := range []error{errNotFound, errQuota} {
		if rest, ok := strings.CutPrefix(msg, s.Error()+": "); ok {
			return s, rest, true
		}
	}
	return nil, "", false
}`,
	// НАХОДКА, СПРЯТАННАЯ В ПОМОЩНИКЕ. Зеркало формы 5 и обязательная её
	// половина: раз замыкание вызовов признаёт вывод у помощника, оно обязано
	// признавать там же и перечень префиксов-СТРОК. Иначе расширение стало бы
	// способом выйти из-под гейта, перенеся перечень на строку ниже.
	"перечень-строк-в-помощнике": `
func stripIt(err error, sentinel error) string {
	msg := err.Error()
	if rest, ok := strings.CutPrefix(msg, sentinel.Error()+": "); ok {
		if rest == "" {
			return sentinel.Error()
		}
		return legacyCut(rest)
	}
	return msg
}

func legacyCut(msg string) string {
	for _, p := range []string{"not found: ", "already exists: "} {
		if rest, ok := strings.CutPrefix(msg, p); ok {
			return rest
		}
	}
	return msg
}`,
}

// ct2ToneStripperCallingAbsentHelper — тот же стриппер, что у формы 5, но БЕЗ
// объявления помощника: его кладёт в соседний файл `helper` фикстуры.
const ct2ToneStripperCallingAbsentHelper = `
func stripIt(err error, sentinel error) string {
	msg := err.Error()
	s, rest, ok := cutIt(msg)
	if !ok {
		return msg
	}
	if rest == "" {
		return s.Error()
	}
	return rest
}`

// ct2ToneHelperBodies — помощники, вынесенные в ОТДЕЛЬНЫЙ ФАЙЛ того же пакета.
//
// Замыкание вызовов объявлено пакетным, а не файловым, — значит и доказывать
// его надо на разных файлах, иначе заявление шире проверенного.
var ct2ToneHelperBodies = map[string]string{
	// Тот же вывод, что у формы 5, но через границу файла.
	"вывод-в-соседнем-файле": `
func cutIt(msg string) (error, string, bool) {
	for _, s := range []error{errNotFound, errQuota} {
		if rest, ok := strings.CutPrefix(msg, s.Error()+": "); ok {
			return s, rest, true
		}
	}
	return nil, "", false
}`,
}

// ct2ToneFixture — что пишется в синтетическое дерево одного владельца.
type ct2ToneFixture struct {
	owner string
	// body — ключ ct2ToneStripperBodies; пусто → объявления стриппера нет вовсе.
	body string
	// noReason — маппер не несёт признака полосы учёта.
	noReason bool
	// commentOnly — литеральный префикс стоит в КОММЕНТАРИИ, а код законен.
	commentOnly bool
	// bodyLiteral — тело стриппера дословно, минуя ct2ToneStripperBodies.
	bodyLiteral string
	// helper — ключ ct2ToneHelperBodies; помощник кладётся в ОТДЕЛЬНЫЙ файл
	// того же пакета.
	helper string
	// vocabulary — тексты словаря sentinel'ов; пусто → общий словарь.
	// "нет" → файла словаря не будет вовсе.
	vocabulary string
}

func writeCt2ToneTree(t *testing.T, fixtures ...ct2ToneFixture) string {
	t.Helper()
	root := t.TempDir()

	mk := func(rel, body string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	for _, f := range fixtures {
		reason := `"QUOTA_EXCEEDED"`
		if f.noReason {
			reason = `"SOMETHING_ELSE"`
		}
		mk("services/"+f.owner+"/internal/apps/shared/errmap_quota.go", `
package shared

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func refuse(err error, sentinel error) error {
	reason := `+reason+`
	_ = reason
	return status.New(codes.ResourceExhausted, stripIt(err, sentinel)).Err()
}
`)
		switch f.vocabulary {
		case "нет":
			// файла словаря нет — ось 3 обязана назвать это находкой
		default:
			exceeded, notProvisioned := ct2QuotaExceededSentinel, ct2QuotaNotProvisionedSentinel
			if f.vocabulary != "" {
				exceeded, notProvisioned = f.vocabulary, f.vocabulary+" not provisioned"
			}
			mk("services/"+f.owner+"/internal/errors/errors.go", `
package errors

import "errors"

var (
	ErrQuotaExceeded       = errors.New("`+exceeded+`")
	ErrQuotaNotProvisioned = errors.New("`+notProvisioned+`")
)
`)
		}
		if f.helper != "" {
			// Помощник — ОТДЕЛЬНЫЙ файл того же пакета: замыкание вызовов
			// объявлено пакетным, и доказывать его надо через границу файла.
			mk("services/"+f.owner+"/internal/apps/shared/strip_helper.go", `
package shared

import "strings"

`+ct2ToneHelperBodies[f.helper]+`
`)
		}
		body := f.bodyLiteral
		if body == "" {
			if f.body == "" {
				continue
			}
			body = ct2ToneStripperBodies[f.body]
		}
		head := ""
		if f.commentOnly {
			// Литеральный префикс стоит В КОММЕНТАРИИ: гейт судит УЗЛЫ, поэтому
			// обязан молчать. Проверка по подстроке краснела бы здесь.
			head = "// Прежде здесь снимались префиксы \"not found: \" и \"already exists: \".\n"
		}
		mk("services/"+f.owner+"/internal/apps/shared/strip.go", `
package shared

import "strings"

var errNotFound = strings.NewReplacer().Replace
var errQuota = errNotFound

`+head+body+`
`)
	}
	return root
}

func ct2ToneRun(t *testing.T, root string, owners []string) (ct2ToneCensus, []string) {
	t.Helper()
	c, err := collectQuotaRefusalTone(mustSyntheticTree(t, root), owners)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	return c, quotaRefusalToneFindings(c)
}

// (а) НАСТОЯЩИЙ ДЕФЕКТ обязан дать находку и назвать координату.
func TestCt2ToneInjection_PrefixListIsAFinding(t *testing.T) {
	root := writeCt2ToneTree(t, ct2ToneFixture{owner: "nlb", body: "перечень-строк"})
	c, findings := ct2ToneRun(t, root, []string{"nlb"})

	if len(findings) != 1 {
		t.Fatalf("перечень префиксов-строк обязан дать РОВНО одну находку, получено %d: %v",
			len(findings), findings)
	}
	for _, want := range []string{"nlb", "strip.go", "not found: "} {
		if !strings.Contains(findings[0], want) {
			t.Errorf("находка обязана называть %q, а называет: %s", want, findings[0])
		}
	}
	if c.Conforming != 0 {
		t.Errorf("соответствующих обязано быть 0, посчитано %d", c.Conforming)
	}
	if c.Outward != 1 || c.Resolved != 1 {
		t.Errorf("обход обязан был найти маппер и стриппер: мапперов %d, стрипперов %d",
			c.Outward, c.Resolved)
	}
}

// (б) КАЖДАЯ законная форма обязана МОЛЧАТЬ — иначе гейт ловит форму записи, а
// не существо, и первый же ложный срабат его отключит.
func TestCt2ToneInjection_LawfulFormsAreSilent(t *testing.T) {
	for _, form := range []string{
		"склейка-на-месте", "перечень-sentinel", "через-переменную", "вывод-в-помощнике",
	} {
		t.Run(form, func(t *testing.T) {
			root := writeCt2ToneTree(t, ct2ToneFixture{owner: "vpc", body: form})
			c, findings := ct2ToneRun(t, root, []string{"vpc"})
			if len(findings) != 0 {
				t.Fatalf("законная форма %q обязана молчать, получено: %v", form, findings)
			}
			if c.Conforming != 1 {
				t.Errorf("соответствующих обязан быть 1, посчитано %d", c.Conforming)
			}
		})
	}
}

// (б3) ЗАМЫКАНИЕ ВЫЗОВОВ — обе его стороны, и они обязаны стоять рядом.
//
// Расширение области с тела названной функции до её вызовов внутри пакета
// признаёт вывод у помощника — значит обязано признавать там же и перечень
// префиксов-СТРОК. Односторонняя правка сделала бы гейт обходимым переносом
// перечня на строку ниже, и обход был бы НЕ ВИДЕН: гейт остался бы зелёным.
func TestCt2ToneInjection_CallClosureHasBothSides(t *testing.T) {
	t.Run("вывод у помощника в СОСЕДНЕМ ФАЙЛЕ — молчит", func(t *testing.T) {
		root := writeCt2ToneTree(t, ct2ToneFixture{
			owner:       "vpc",
			bodyLiteral: ct2ToneStripperCallingAbsentHelper,
			helper:      "вывод-в-соседнем-файле",
		})
		c, findings := ct2ToneRun(t, root, []string{"vpc"})
		if len(findings) != 0 {
			t.Fatalf("вывод через границу файла того же пакета обязан молчать, получено: %v", findings)
		}
		if c.Conforming != 1 || c.Guarding != 1 {
			t.Errorf("обе оси обязаны сойтись: выводящих %d, ограждающих %d", c.Conforming, c.Guarding)
		}
	})

	t.Run("перечень строк У ПОМОЩНИКА — находка", func(t *testing.T) {
		root := writeCt2ToneTree(t, ct2ToneFixture{owner: "vpc", body: "перечень-строк-в-помощнике"})
		c, findings := ct2ToneRun(t, root, []string{"vpc"})
		if len(findings) != 1 {
			t.Fatalf("перечень префиксов-строк у помощника обязан дать РОВНО одну находку, "+
				"получено %d: %v", len(findings), findings)
		}
		for _, want := range []string{"vpc", "strip.go", "not found: "} {
			if !strings.Contains(findings[0], want) {
				t.Errorf("находка обязана называть %q, а называет: %s", want, findings[0])
			}
		}
		if c.Conforming != 0 {
			t.Errorf("соответствующих обязано быть 0, посчитано %d", c.Conforming)
		}
		if got := ct2ToneOwnerState(c.Facts["vpc"]); got != "перечень префиксов-строк" {
			t.Errorf("перепись назвала состояние %q, ожидалось «перечень префиксов-строк» — "+
				"перепись и находка говорят о владельце разное", got)
		}
	})
}

// (б2) Литеральный префикс В КОММЕНТАРИИ находкой не является: гейт судит узлы.
func TestCt2ToneInjection_PrefixInACommentIsSilent(t *testing.T) {
	root := writeCt2ToneTree(t,
		ct2ToneFixture{owner: "vpc", body: "склейка-на-месте", commentOnly: true})
	_, findings := ct2ToneRun(t, root, []string{"vpc"})
	if len(findings) != 0 {
		t.Fatalf("проза о префиксах префиксом не является, получено: %v", findings)
	}
}

// (в) СЛЕПЫЕ ЗОНЫ названы находкой, а не молчанием: «о владельце ничего не
// известно» и «у владельца всё в порядке» обязаны быть различимы.
func TestCt2ToneInjection_BlindSpotsAreFindingsNotSilence(t *testing.T) {
	cases := []struct {
		name    string
		fixture ct2ToneFixture
		want    string
		// wantState — как ту же слепоту называет ПЕРЕПИСЬ. Утверждается вместе с
		// находкой: перепись и находка обязаны говорить об одном состоянии одно и
		// то же. Без этой оси перепись называла владельца без маппера «стриппер не
		// разрешён» — имя ВТОРОГО симптома, — и разойтись они могли молча.
		wantState string
	}{
		{"маппера нет", ct2ToneFixture{owner: "iam", body: "склейка-на-месте", noReason: true},
			"маппер отказа учёта наружу не найден", "нет маппера"},
		{"стриппер не объявлен", ct2ToneFixture{owner: "iam", body: ""},
			"объявления которого в прод-дереве владельца нет", "стриппер не разрешён"},
		{"префикс ниоткуда", ct2ToneFixture{owner: "iam", body: "префикс-ниоткуда"},
			"префикса из sentinel'а не выводит", "префикс ниоткуда"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := writeCt2ToneTree(t, tc.fixture)
			c, findings := ct2ToneRun(t, root, []string{"iam"})
			if len(findings) != 1 || !strings.Contains(findings[0], tc.want) {
				t.Fatalf("ожидалась находка %q, получено: %v", tc.want, findings)
			}
			if got := ct2ToneOwnerState(c.Facts["iam"]); got != tc.wantState {
				t.Errorf("перепись назвала состояние %q, ожидалось %q — "+
					"перепись и находка говорят о владельце разное", got, tc.wantState)
			}
			if c.Conforming != 0 {
				t.Errorf("соответствующих обязано быть 0, посчитано %d", c.Conforming)
			}
		})
	}
}

// (г) ПУСТОЙ ОБХОД отличим от «нарушений нет»: перепись обязана показать нули,
// на которые гейт и падает своей проверкой предпосылки.
func TestCt2ToneInjection_EmptyWalkIsDistinguishable(t *testing.T) {
	root := t.TempDir()
	c, findings := ct2ToneRun(t, root, []string{"nlb"})
	if c.Files != 0 || c.Outward != 0 {
		t.Fatalf("на пустом дереве обход обязан быть пуст: файлов %d, мапперов %d",
			c.Files, c.Outward)
	}
	// Находки при этом ЕСТЬ — «о владельце ничего не известно», а не молчание, и
	// по КАЖДОЙ оси своя: маппер не найден и словарь не найден.
	if len(findings) != 2 {
		t.Fatalf("пустой обход обязан объявить владельца ненаблюдаемым по обеим "+
			"осям, получено %d: %v", len(findings), findings)
	}
	joined := strings.Join(findings, " | ")
	for _, want := range []string{"вне наблюдения", "словаря sentinel'ов"} {
		if !strings.Contains(joined, want) {
			t.Errorf("среди находок обязано быть %q, получено: %v", want, findings)
		}
	}
}

// (е) ОСЬ 2 — ПУСТОЙ ОСТАТОК. Дефект и ЛОЖНЫЙ БЛИЗНЕЦ судятся раздельно:
// сравнение с пустой строкой, относящееся не к остатку, ограждением НЕ является.
func TestCt2ToneInjection_EmptyRemainderAxis(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		finding bool
	}{
		{"остаток не ограждён", "остаток-без-ограждения", true},
		{"ограждено не то значение", "ограждение-не-того", true},
		{"ограждение строкой", "склейка-на-месте", false},
		{"ограждение длиной", "ограждение-длиной", false},
		{"ограждение в перечне sentinel'ов", "перечень-sentinel", false},
		{"ограждение через переменную", "через-переменную", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := writeCt2ToneTree(t, ct2ToneFixture{owner: "vpc", body: tc.body})
			c, findings := ct2ToneRun(t, root, []string{"vpc"})
			if !tc.finding {
				if len(findings) != 0 {
					t.Fatalf("законная форма %q обязана молчать: %v", tc.body, findings)
				}
				if c.Guarding != 1 {
					t.Errorf("огражденных обязан быть 1, посчитано %d", c.Guarding)
				}
				return
			}
			if len(findings) != 1 || !strings.Contains(findings[0], "ПУСТОЙ остаток") {
				t.Fatalf("ожидалась находка об остатке, получено: %v", findings)
			}
			if !strings.Contains(findings[0], "strip.go") {
				t.Errorf("находка обязана называть координату, а называет: %s", findings[0])
			}
			if c.Guarding != 0 {
				t.Errorf("огражденных обязано быть 0, посчитано %d", c.Guarding)
			}
		})
	}
}

// (ж) ОСЬ 3 — СЛОВАРЬ SENTINEL'ОВ. Расхождение называет ОБА текста, отсутствие
// объявления называется отдельно, совпадение молчит.
func TestCt2ToneInjection_SentinelVocabularyAxis(t *testing.T) {
	t.Run("разошёлся", func(t *testing.T) {
		root := writeCt2ToneTree(t, ct2ToneFixture{
			owner: "compute", body: "склейка-на-месте", vocabulary: "quota exceeded"})
		c, findings := ct2ToneRun(t, root, []string{"compute"})
		if len(findings) != 2 {
			t.Fatalf("разошедшийся словарь обязан дать находку по каждому имени, "+
				"получено %d: %v", len(findings), findings)
		}
		joined := strings.Join(findings, " | ")
		for _, want := range []string{`"quota exceeded"`, `"` + ct2QuotaExceededSentinel + `"`,
			"errors.go", ct2QuotaNotProvisionedVar} {
			if !strings.Contains(joined, want) {
				t.Errorf("находки обязаны называть %q, получено: %v", want, findings)
			}
		}
		if c.Vocabulary != 0 {
			t.Errorf("совпавших словарей обязано быть 0, посчитано %d", c.Vocabulary)
		}
	})
	t.Run("не объявлен", func(t *testing.T) {
		root := writeCt2ToneTree(t, ct2ToneFixture{
			owner: "compute", body: "склейка-на-месте", vocabulary: "нет"})
		_, findings := ct2ToneRun(t, root, []string{"compute"})
		if len(findings) != 1 || !strings.Contains(findings[0], "вне наблюдения") {
			t.Fatalf("отсутствие словаря обязано быть названо находкой, получено: %v", findings)
		}
	})
	t.Run("совпал", func(t *testing.T) {
		root := writeCt2ToneTree(t, ct2ToneFixture{owner: "compute", body: "склейка-на-месте"})
		c, findings := ct2ToneRun(t, root, []string{"compute"})
		if len(findings) != 0 {
			t.Fatalf("совпавший словарь обязан молчать: %v", findings)
		}
		if c.Vocabulary != 1 {
			t.Errorf("совпавших словарей обязан быть 1, посчитано %d", c.Vocabulary)
		}
	})
}

// (д) РАЗДЕЛЬНОСТЬ ВЛАДЕЛЬЦЕВ: дефект одного не красит остальных, и перепись
// показывает обе величины — сколько осмотрено и сколько соответствует.
func TestCt2ToneInjection_OneBadOwnerDoesNotTaintTheRest(t *testing.T) {
	root := writeCt2ToneTree(t,
		ct2ToneFixture{owner: "nlb", body: "перечень-строк"},
		ct2ToneFixture{owner: "vpc", body: "склейка-на-месте"},
		ct2ToneFixture{owner: "iam", body: "перечень-sentinel"},
	)
	c, findings := ct2ToneRun(t, root, []string{"iam", "nlb", "vpc"})
	if len(findings) != 1 || !strings.Contains(findings[0], "nlb") {
		t.Fatalf("находка обязана быть одна и про nlb, получено: %v", findings)
	}
	if c.Outward != 3 || c.Conforming != 2 {
		t.Fatalf("перепись обязана дать 3 осмотренных и 2 соответствующих, получено %d и %d",
			c.Outward, c.Conforming)
	}
}
