// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// gatedverdictproducesred_test.go — зелёное задание обязано ДОКАЗАТЬ, что
// исполнило объявленное: отметка, гасящая вердиктные шаги, обязана иметь
// производителя красного.
//
// # Норма владельца продукта, дословно
//
//	«сделай так, чтобы можно было гарантировать зелёный;
//	 если что-то не так — в красный»
//
// Отсюда два связывающих следствия, и оба держит этот гейт:
//
//  1. ЗЕЛЁНОЕ — ГАРАНТИЯ, А НЕ ОТСУТСТВИЕ ПАДЕНИЯ. Задание вправе быть зелёным
//     только тогда, когда исполнило то, что обязано было исполнить, и получило
//     от этого результат. «Ничего не упало» зелёным не является.
//  2. ЧТО-ТО НЕ ТАК ⇒ КРАСНОЕ. Несозданное условие, погашенный вердиктный шаг,
//     пустой обход — красное. Различение исходов при этом ОСТАЁТСЯ, но живёт в
//     ТЕКСТЕ отказа: он обязан говорить, «условие не создано» это или дефект
//     дерева. Красный при честном тексте — то, что просил владелец; красный без
//     объяснения — нет.
//
// # Предмет — механизм, а не случай
//
// Владелец материализации стенда объявляет третью категорию ОТМЕТКОЙ: пишет
// имя в `$GITHUB_ENV` и выходит НУЛЁМ. Шаги ниже гасятся условием на эту
// отметку — и задание доходит до конца зелёным, не вынеся НИ ОДНОГО утверждения
// о своём предмете. Сводка запроса на слияние показывает `success`; человек
// читает это как «пробы прошли». Так читали дважды, поэтому человек на этот
// признак не годится.
//
// Замер, из которого гейт выведен (перепись одного прогона, 49 заданий):
// ложно-зелёных 5, вердиктных шагов погашено 22 в трёх процессах.
//
// # Что гейт требует, и по КАКИМ ДВУМ признакам берётся задание
//
// Пара «задание · отметка» требует ПРОИЗВОДИТЕЛЯ КРАСНОГО — шага или ветви,
// дающей ненулевой код при выставленной отметке, — если верно ХОТЬ ОДНО:
//
//	признак «ГАСИТ»      — отметка гасит в задании хотя бы один ВЕРДИКТНЫЙ шаг;
//	признак «ПРОИЗВОДИТ» — задание САМО ставит суппрессивную отметку (пишет её в
//	                       `$GITHUB_ENV` своим телом либо зовёт отслеживаемого
//	                       писателя не его самопробой).
//
// ВТОРОЙ ПРИЗНАК НЕ ЗАМЕНЯЕТ ПЕРВОГО И НЕ ВЫВОДИТСЯ ИЗ НЕГО. Работа, которая
// отметку производит и НЕ ЧИТАЕТ ни одним условием, признаку «гасит» невидима —
// а это тот же ложно-зелёный: третья категория объявлена и выброшена, шаги ниже
// отработали на несозданном условии и вышли нулём. Замерено на базе линии:
// работ-производителей 4, работ-гасящих 3, и разница ровно одна —
// `production-posture.yml::stand`, где читателей отметки было НОЛЬ при двух
// производителях в ней же (`lane/posture-verdict`, исполнение настоящих тел
// шагов на отсутствующем кластере).
//
// Суппрессивность отметки ВЫВОДИТСЯ ИЗ ДЕРЕВА (гасит ли она вердиктный шаг хоть
// в одном задании), а не из имени: без этого признак «производит» потребовал бы
// красноты от КАЖДОЙ записи в `$GITHUB_ENV` — в дереве это адрес настроек
// материализации и образ браузера, где гасить нечего.
//
// # Форм производителя ЧЕТЫРЕ, и каждая доказана инъекцией
//
// В `gatedverdictproducesred_injection_test.go`, в обе стороны: форма есть —
// молчит, форма снята — находка с координатой.
//
//	форма 1 — шаг, чьё УСЛОВИЕ ссылается на отметку в полярности «выставлена»,
//	          и чьё тело способно выйти ненулевым (в том числе телом скрипта);
//	форма 2 — всегда-исполняемый шаг, чьё ТЕЛО или блок `env:` называет отметку,
//	          и чьё ТЕЛО способно выйти ненулевым;
//	форма 3 — всегда-исполняемый шаг, называющий отметку и ПЕРЕДАЮЩИЙ её
//	          значение подстановкой в вызов отслеживаемого скрипта, который
//	          способен выйти ненулевым;
//	форма 4 — делегирование нижестоящему заданию: оно зависит от нашего
//	          (`needs`), исполняется не только на падении, и зовёт владельца
//	          сводного вердикта из [gvRunLevelVerdictOwners].
//
// ФОРМА 3 БЫЛА ПОТЕРЯНА ПЕРВОЙ РЕДАКЦИЕЙ, и цена этого измерена: ею в дереве
// записаны ВСЕ ТРИ производителя переписи исходов (`ci.yaml::helm`,
// `console-e2e.yml::probes`, `production-posture.yml::stand`). Шаг переписи
// обязан быть всегда-исполняемым — перепись, гасимая тем, о чём обязана
// доложить, промолчала бы ровно в своём случае, — поэтому полярности в его
// условии нет и быть не может, а прежняя редакция требовала именно её. Форма,
// о которой распознаватель не знает, даёт не красное и не зелёное, а МОЛЧАНИЕ.
//
// Формы 1, 2 и 3 требуют, чтобы отказ был причинно связан с ОТМЕТКОЙ. Без этого
// производителем оказался бы любой шаг задания, способный упасть по любой
// причине, — то есть требование выполнялось бы у всех и не отвергало никого.
//
// `continue-on-error: true` у производителя снимает его с учёта: отказ,
// проглоченный послаблением, задание не роняет.
//
// # ВЕДОМОСТИ ПРОЩЁННЫХ У ГЕЙТА НЕТ
//
// Первая редакция несла её: два известных ложно-зелёных лежали в процессах
// соседних полос, править которые этой полосе было нельзя. Записи истекли —
// оба задания несут производителя, — и ведомость снята ЦЕЛИКОМ вместе с
// предикатом её истечения. Заводить заново не надо ни в какой форме: всякая
// запись в ней — место, куда ложно-зелёное вносят незамеченным.
//
// # Признак ВЕРДИКТНОГО шага — исполним разбором и ошибается в сторону строгости
//
// Вердиктный шаг — тот, чьё отсутствие не оставляет в задании ни одного
// утверждения о том же предмете. Машинно это читается так: шаг СЛУЖЕБНЫЙ, если
// он распознан одной из перечисленных служебных ФОРМ (кэш · артефакт ·
// установка инструмента · вход в реестр образов · уборка и опись); всякий
// нераспознанный — ВЕРДИКТНЫЙ. Неясное считается вердиктным намеренно: обратное
// умолчание превратило бы каждую новую форму в молчание.
//
// # Отметки ВЫВОДЯТСЯ ИЗ ДЕРЕВА, а не выписываются
//
// Отметкой считается имя, которое дерево пишет в `$GITHUB_ENV` (прямо в теле
// шага, в отслеживаемом скрипте, либо через переменную, куда `$GITHUB_ENV`
// положен). Такое имя наблюдаемо последующими шагами ТОЛЬКО если пишущий вышел
// нулём — иначе задание обрывается, и гасить уже нечего. То есть всякая такая
// запись есть канал «вышел нулём, а работа не сделана» ПО ПОСТРОЕНИЮ.
//
// Рукописный перечень отметок пришлось бы дописывать руками ровно тогда же,
// когда о новой забывают. Поэтому перечня нет, а есть предикат.
//
// Запись в `$GITHUB_ENV`, из которой имя и значение НЕ разобраны, — находка, а
// не пропуск: неполный словарь отметок молча сужает гейт.
//
// # Читается РАЗОБРАННЫЙ документ
//
// Имя отметки и слова про условия стоят в этом файле и в комментариях
// процессов; гейт, ищущий их подстрокой в сыром тексте, покраснел бы на
// собственном объяснении. Поэтому задания и шаги берутся из разобранного YAML,
// где комментария не существует как узла, а `$GITHUB_ENV` ищется только в
// строках, не начинающихся с решётки.
//
// # Перепись
//
// «Ноль находок» обязано отличаться от «ноль прочитанного»: гейт печатает
// процессов · заданий · шагов с условием · из них погашенных отметкой · из них
// вердиктных и служебных · пар, потребовавших производителя · из них тех, где
// задание САМО производит отметку · пар без производителя · закрытых
// делегированием · отметок дерева и суппрессивных среди них. Пустой обход —
// провал, и отдельно провал — ноль суппрессивных отметок: тогда признак
// «производит» не применяется ни к чему.
//
// # Чего гейт НЕ закрывает, сказано прямо
//
// Он судит только отметки в `$GITHUB_ENV`. Условие на `steps.<id>.outcome` не
// его предмет: гасящее значение там — «шаг упал или пропущен», а упавший шаг
// задание и так роняет (послабление `continue-on-error` объявляется словом и
// ловится отдельно), а пропущенный шаг пропущен своим СОБСТВЕННЫМ условием —
// то есть пара «задание · та отметка» уже в предмете этого гейта, и канал
// закрыт у источника, а не у эха.
//
// Он не судит и ИСТИННОСТЬ красноты у форм 3 и 4: структура отвечает «кому
// поручено», а не «краснеет ли исполнитель». Для формы 4 вопрос закрыт
// ИСПОЛНЕНИЕМ владельца в `TestRunLevelVerdictOwnerRedsOnTheMark`. Для формы 3
// он закрыт САМОПРОБОЙ скрипта переписи
// (`.github/scripts/assert-declared-verdicts-ran.py --self-test`, случай 2:
// «отметка выставлена → красное, причина названа»), которую исполняет
// `deploy/scripts/run-gate-self-tests.sh`. Гейт этого НЕ проверяет и на такую
// проверку не претендует — сказано прямо, чтобы не читалось шире сделанного.
package repohygiene

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/gitenv"
	"gopkg.in/yaml.v3"
)

// gvGateTestName — имя проверки по дереву. Стоит здесь, чтобы страж провязки
// сверял ОДНО имя с объявлением конвейера, а не два литерала друг с другом.
const gvGateTestName = "TestGatedVerdictStepsHaveAProducerOfRed"

// gvRunLevelVerdictOwners — владельцы сводного вердикта, чья краснота от
// отметки ДОКАЗАНА исполнением (`TestRunLevelVerdictOwnerRedsOnTheMark`).
// Форма 4 принимается только через них: делегирование кому угодно означало бы,
// что достаточно объявить нижестоящее задание.
var gvRunLevelVerdictOwners = []string{
	".github/scripts/aggregate-shard-verdicts.py",
}

// gvHousekeepingActions — служебные действия целиком по имени.
var gvHousekeepingActions = map[string]bool{
	"actions/cache":              true,
	"actions/cache/save":         true,
	"actions/cache/restore":      true,
	"actions/upload-artifact":    true,
	"actions/download-artifact":  true,
	"actions/checkout":           true,
	"docker/login-action":        true,
	"docker/setup-buildx-action": true,
	"docker/setup-qemu-action":   true,
	"bufbuild/buf-setup-action":  true,
}

// gvHousekeepingActionPrefixes — семейства установки инструментов.
var gvHousekeepingActionPrefixes = []string{"actions/setup-", "azure/setup-"}

// gvHousekeepingCommands — команды, которые печатают или убирают и ничего не
// утверждают. Шаг служебен, только если КАЖДАЯ его команда отсюда.
var gvHousekeepingCommands = []string{
	"kind delete cluster", "kubectl get", "kubectl describe", "kubectl logs",
	"kubectl events", "helm list", "docker logout", "docker image ls",
	"df", "du", "free", "ls", "cat", "echo", "printf", "true", ":",
}

// gvMark — отметка третьей категории: имя, которое дерево пишет в `$GITHUB_ENV`.
type gvMark struct {
	Name    string   // имя переменной
	Value   string   // значение, при котором шаги гасятся
	Known   bool     // значение выведено из дерева, а не угадано
	Writers []string // координаты производителей — для текста отказа

	// Suppresses — отметка ГАСИТ вердиктный шаг хоть в одном задании дерева,
	// то есть она суппрессивная по ФАКТУ ДЕРЕВА, а не по имени.
	//
	// Этим признаком отделяется отметка третьей категории от обычной передачи
	// значения через `$GITHUB_ENV`. Без него требование «работа, производящая
	// отметку, обязана иметь производителя красного» распространилось бы на
	// КАЖДУЮ запись в `$GITHUB_ENV` — в дереве это, например, адрес настроек
	// материализации и образ браузера, — и гейт потребовал бы красноты там, где
	// гасить нечего.
	Suppresses bool
}

// gvCensus — объём осмотренного. Печатается всегда: «ноль находок» обязано быть
// отличимо от «ноль прочитанного».
type gvCensus struct {
	Workflows    int
	Jobs         int
	StepsWithIf  int
	Suppressed   int // шагов, погашенных отметкой
	Verdict      int // из них вердиктных
	Housekeeping int // из них служебных
	Demanding    int // пар «задание · отметка», потребовавших производителя
	WithoutRed   int // из них без производителя
	Delegations  int // закрытых формой 4
	Producing    int // из требовавших — те, где задание САМО производит отметку
}

func (c *gvCensus) add(o gvCensus) {
	c.Workflows += o.Workflows
	c.Jobs += o.Jobs
	c.StepsWithIf += o.StepsWithIf
	c.Suppressed += o.Suppressed
	c.Verdict += o.Verdict
	c.Housekeeping += o.Housekeeping
	c.Demanding += o.Demanding
	c.WithoutRed += o.WithoutRed
	c.Delegations += o.Delegations
	c.Producing += o.Producing
}

// gvStep — то немногое из шага, что нужно этому гейту.
type gvStep struct {
	Name            string            `yaml:"name"`
	Uses            string            `yaml:"uses"`
	If              string            `yaml:"if"`
	Run             string            `yaml:"run"`
	Env             map[string]string `yaml:"env"`
	ContinueOnError string            `yaml:"continue-on-error"`
}

// namesMark — шаг называет отметку в одном из ТРЁХ законных мест: условие ·
// тело · собственный блок `env:`. Третье не косметика: отметка доезжает до тела
// и переименованной — `UNMET: ${{ env.STAND_PRECONDITION_UNMET }}`, — и без
// этого места такой производитель читался бы как не связанный с отметкой.
func (st gvStep) namesMark(name string) (inCond, inBody bool) {
	inCond = strings.Contains(st.If, "env."+name)
	inBody = strings.Contains(st.Run, name)
	for _, v := range st.Env {
		if strings.Contains(v, name) {
			inBody = true
		}
	}
	return inCond, inBody
}

// gvJob — задание. `needs` площадка принимает и строкой, и списком.
type gvJob struct {
	If    string    `yaml:"if"`
	Needs yaml.Node `yaml:"needs"`
	Steps []gvStep  `yaml:"steps"`
}

// needs — имена заданий, от которых зависит это, в обеих законных формах.
func (j gvJob) needs() []string {
	switch j.Needs.Kind {
	case yaml.ScalarNode:
		var s string
		if j.Needs.Decode(&s) == nil && s != "" {
			return []string{s}
		}
	case yaml.SequenceNode:
		var s []string
		if j.Needs.Decode(&s) == nil {
			return s
		}
	}
	return nil
}

type gvDoc struct {
	Jobs map[string]gvJob `yaml:"jobs"`
}

// ───────────────────────── трёхзначное вычисление условия ─────────────────────

type gvTri int

const (
	gvUnknown gvTri = iota
	gvTrue
	gvFalse
)

var gvEnvCmp = regexp.MustCompile(`^env\.([A-Za-z_][A-Za-z0-9_]*)\s*(==|!=)\s*'([^']*)'$`)
var gvEnvCmpRev = regexp.MustCompile(`^'([^']*)'\s*(==|!=)\s*env\.([A-Za-z_][A-Za-z0-9_]*)$`)

// gvEval — вычисляет условие шага при гипотезе «отметка name равна val».
//
// Читаются только сравнения отметки с литералом; всё остальное — НЕИЗВЕСТНО.
// Это намеренно: судья вправе утверждать, что шаг погашен, лишь когда это
// следует из формы условия, а не из предположения о значении чужого факта.
func gvEval(cond, name, val string) gvTri {
	c := strings.TrimSpace(cond)
	c = strings.TrimPrefix(c, "${{")
	c = strings.TrimSuffix(strings.TrimSpace(c), "}}")
	c = strings.TrimSpace(c)
	if c == "" {
		return gvUnknown
	}
	p := &gvParser{src: c, name: name, val: val}
	v := p.parseOr()
	return v
}

type gvParser struct {
	src  string
	pos  int
	name string
	val  string
}

func (p *gvParser) skip() {
	for p.pos < len(p.src) && (p.src[p.pos] == ' ' || p.src[p.pos] == '\t' || p.src[p.pos] == '\n') {
		p.pos++
	}
}

func (p *gvParser) peek(tok string) bool {
	p.skip()
	return strings.HasPrefix(p.src[p.pos:], tok)
}

func (p *gvParser) parseOr() gvTri {
	v := p.parseAnd()
	for p.peek("||") {
		p.pos += 2
		r := p.parseAnd()
		v = gvOr(v, r)
	}
	return v
}

func gvOr(a, b gvTri) gvTri {
	if a == gvTrue || b == gvTrue {
		return gvTrue
	}
	if a == gvFalse && b == gvFalse {
		return gvFalse
	}
	return gvUnknown
}

func (p *gvParser) parseAnd() gvTri {
	v := p.parseUnary()
	for p.peek("&&") {
		p.pos += 2
		r := p.parseUnary()
		v = gvAnd(v, r)
	}
	return v
}

func gvAnd(a, b gvTri) gvTri {
	if a == gvFalse || b == gvFalse {
		return gvFalse
	}
	if a == gvTrue && b == gvTrue {
		return gvTrue
	}
	return gvUnknown
}

func (p *gvParser) parseUnary() gvTri {
	if p.peek("!") && !p.peek("!=") {
		p.pos++
		v := p.parseUnary()
		switch v {
		case gvTrue:
			return gvFalse
		case gvFalse:
			return gvTrue
		}
		return gvUnknown
	}
	return p.parsePrimary()
}

func (p *gvParser) parsePrimary() gvTri {
	p.skip()
	if p.pos < len(p.src) && p.src[p.pos] == '(' {
		p.pos++
		v := p.parseOr()
		p.skip()
		if p.pos < len(p.src) && p.src[p.pos] == ')' {
			p.pos++
		}
		return v
	}
	start := p.pos
	depth := 0
	inStr := false
	for p.pos < len(p.src) {
		ch := p.src[p.pos]
		if ch == '\'' {
			inStr = !inStr
			p.pos++
			continue
		}
		if !inStr {
			if ch == '(' {
				depth++
			} else if ch == ')' {
				if depth == 0 {
					break
				}
				depth--
			} else if depth == 0 && (strings.HasPrefix(p.src[p.pos:], "&&") || strings.HasPrefix(p.src[p.pos:], "||")) {
				break
			}
		}
		p.pos++
	}
	return p.leaf(strings.TrimSpace(p.src[start:p.pos]))
}

func (p *gvParser) leaf(text string) gvTri {
	var envName, op, lit string
	if m := gvEnvCmp.FindStringSubmatch(text); m != nil {
		envName, op, lit = m[1], m[2], m[3]
	} else if m := gvEnvCmpRev.FindStringSubmatch(text); m != nil {
		lit, op, envName = m[1], m[2], m[3]
	} else {
		return gvUnknown
	}
	if envName != p.name {
		return gvUnknown
	}
	eq := lit == p.val
	if op == "!=" {
		eq = !eq
	}
	if eq {
		return gvTrue
	}
	return gvFalse
}

// ───────────────────────── распознаватели форм ────────────────────────────────

// gvNonZeroExit — тело способно выйти НЕНУЛЕВЫМ кодом в оболочечной форме.
//
// `exit 0` не считается, и аннотация без отказа тоже: `::error` печатает
// красную строку в журнал и оставляет шаг зелёным — это ровно тот класс,
// который гейт и ловит, только на уровень ниже.
var gvNonZeroExit = regexp.MustCompile(`(^|[;&|{(\n]|\bthen\b|\belse\b|\bdo\b)\s*(exit\s+([1-9][0-9]*|"?\$)|false\b|return\s+[1-9][0-9]*)`)

// gvNonZeroCall — тот же отказ, объявленный ВЫЗОВОМ, а не словом оболочки.
// Отслеживаемые скрипты дерева написаны на трёх языках, и в двух из них
// ненулевой выход не имеет оболочечной формы вовсе.
//
// ЗАМЕР, А НЕ ПРЕДПОЛОЖЕНИЕ: скриптов, которые умеют ненулевой выход ТОЛЬКО
// этой формой, в дереве 24 из 439 отслеживаемых. Нераспознанный отказ даёт
// ЛОЖНУЮ НАХОДКУ (производитель есть, а гейт его не видит) — то есть слепота
// здесь стоит не молчания, а красноты на исправном дереве.
var gvNonZeroCall = regexp.MustCompile(
	`(sys\.exit\(\s*[1-9]|raise\s+SystemExit\(\s*[1-9]|process\.exit\(\s*[1-9]|os\.Exit\([1-9])`)

func gvCanExitNonZero(body string) bool {
	return gvNonZeroExit.MatchString(body) || gvNonZeroCall.MatchString(body)
}

// gvActionOf — имя действия без ссылки на версию.
func gvActionOf(uses string) string {
	u := strings.TrimSpace(uses)
	if i := strings.IndexByte(u, '@'); i >= 0 {
		u = u[:i]
	}
	return u
}

// gvIsHousekeeping — шаг распознан служебной формой. Нераспознанный —
// вердиктный: умолчание ошибается в сторону строгости намеренно.
func gvIsHousekeeping(st gvStep) bool {
	if a := gvActionOf(st.Uses); a != "" {
		if gvHousekeepingActions[a] {
			return true
		}
		for _, p := range gvHousekeepingActionPrefixes {
			if strings.HasPrefix(a, p) {
				return true
			}
		}
		return false
	}
	body := strings.TrimSpace(st.Run)
	if body == "" {
		return false
	}
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSuffix(line, "\\")
		for _, part := range gvSplitCommands(line) {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if strings.HasPrefix(part, "set -") {
				continue
			}
			if !gvIsHousekeepingCommand(part) {
				return false
			}
		}
	}
	return true
}

func gvSplitCommands(line string) []string {
	repl := strings.NewReplacer("&&", "\x00", "||", "\x00", ";", "\x00", "|", "\x00")
	return strings.Split(repl.Replace(line), "\x00")
}

func gvIsHousekeepingCommand(cmd string) bool {
	for _, h := range gvHousekeepingCommands {
		if cmd == h || strings.HasPrefix(cmd, h+" ") {
			return true
		}
	}
	return false
}

// gvTruthy — условие послабления объявлено словом «истина».
func gvTruthy(v string) bool {
	s := strings.ToLower(strings.TrimSpace(v))
	return s == "true" || strings.Contains(s, "true")
}

// gvSelfTestFlag — флаг собственной пробы скрипта. Литерал ЗАКОНЕН здесь
// потому, что он не наш: его объявляет и разбирает сам скрипт, а состав
// скриптов с самопробой держит `deploy/scripts/run-gate-self-tests.sh`.
const gvSelfTestFlag = "--self-test"

// gvSelfTestInvocation — шаг зовёт скрипт его СОБСТВЕННОЙ самопробой, а не по
// делу.
//
// Признак нужен потому, что самопроба владельца подъёма обрывается ДО записи
// отметки (`exit` в конце блока самопробы — проверено чтением скрипта), то есть
// отметку такой шаг не производит НИКОГДА. Счесть его производством значило бы
// требовать производителя красного у работы, которая отметку не поднимает: на
// дереве это дало бы 9 шагов-производителей вместо 7.
//
// Флаг обязан стоять в ТОЙ ЖЕ строке, что вызов: иначе самопроба одного скрипта
// прощала бы настоящее производство другим в том же теле.
func gvSelfTestInvocation(st gvStep, script, scriptBody string) bool {
	if !strings.Contains(scriptBody, gvSelfTestFlag) {
		return false
	}
	for _, line := range strings.Split(st.Run, "\n") {
		if strings.Contains(line, script) && strings.Contains(line, gvSelfTestFlag) {
			return true
		}
	}
	return false
}

// gvWriterFiles — файлы, в которых стоят производители отметки. Координата
// писателя — «путь:строка», и для записи из тела шага процесса путём оказывается
// сам процесс; такой писатель отслеживаемым скриптом не является и ловится
// признаком (а) в [gvJobProducesMark].
func gvWriterFiles(mark gvMark) []string {
	seen := map[string]bool{}
	var out []string
	for _, w := range mark.Writers {
		if i := strings.LastIndexByte(w, ':'); i > 0 {
			if f := w[:i]; !seen[f] {
				seen[f] = true
				out = append(out, f)
			}
		}
	}
	sort.Strings(out)
	return out
}

// gvStepWritesMark — тело шага САМО пишет отметку в `$GITHUB_ENV`.
func gvStepWritesMark(st gvStep, name string) bool {
	aliases := map[string]bool{"GITHUB_ENV": true}
	for _, m := range gvEnvAlias.FindAllStringSubmatch(st.Run, -1) {
		aliases[m[1]] = true
	}
	for _, raw := range strings.Split(st.Run, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		i := strings.Index(line, ">>")
		if i < 0 {
			continue
		}
		w := gvEnvWriteLine.FindStringSubmatch(line)
		if w == nil || !aliases[w[1]] {
			continue
		}
		for _, n := range gvEnvAssign.FindAllStringSubmatch(line[:i], -1) {
			if n[1] == name {
				return true
			}
		}
	}
	return false
}

// gvJobProducesMark — координаты шагов задания, ПРОИЗВОДЯЩИХ отметку.
//
// ЗАЧЕМ ЭТОТ ПРИЗНАК СУЩЕСТВУЕТ РЯДОМ С «ГАСИТ», А НЕ ВМЕСТО НЕГО. Признак
// «гасит» видит работу только тогда, когда она читает отметку условием. Работа,
// которая отметку ПРОИЗВОДИТ и не читает ни одним шагом, ему невидима — а это
// не безобидный случай, а тот же ложно-зелёный: третья категория объявлена и
// выброшена, шаги ниже отработали на несозданном условии и вышли нулём.
//
// ЗАМЕРЕНО НА БАЗЕ ЛИНИИ, а не предположено: работ-производителей было 4,
// работ-гасящих 3, и разница ровно одна — `production-posture.yml::stand`, где
// читателей отметки было НОЛЬ при двух производителях в ней же. Работа зеленела
// без единого вердикта о своём предмете, и признак «гасит» её не видел.
func gvJobProducesMark(job gvJob, mark gvMark, scripts map[string]string) []string {
	var out []string
	writers := gvWriterFiles(mark)
	for i, st := range job.Steps {
		label := "шаг #" + itoa(i+1)
		if st.Name != "" {
			label += " («" + st.Name + "»)"
		}
		// (а) тело шага пишет отметку своими руками
		if gvStepWritesMark(st, mark.Name) {
			out = append(out, label)
			continue
		}
		// (б) шаг зовёт отслеживаемого писателя — и не его самопробой
		for _, w := range writers {
			body, tracked := scripts[w]
			if !tracked || !strings.Contains(st.Run, w) {
				continue
			}
			if gvSelfTestInvocation(st, w, body) {
				continue
			}
			out = append(out, label)
			break
		}
	}
	return out
}

// gvMarkAliases — имена, под которыми отметка доезжает до ТЕЛА шага: само её
// имя и всякий ключ блока `env:`, чьё значение её называет. Переименование при
// доставке (`UNMET: ${{ env.STAND_PRECONDITION_UNMET }}`) — законная форма, и
// без неё доставка читалась бы как несвязанная с отметкой.
func gvMarkAliases(st gvStep, name string) []string {
	var out []string
	if strings.Contains(st.Run, name) {
		out = append(out, name)
	}
	keys := make([]string, 0, len(st.Env))
	for k := range st.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if strings.Contains(st.Env[k], name) {
			out = append(out, k)
		}
	}
	return out
}

// gvMarkReachesScript — значение отметки ПОДСТАВЛЕНО в тело шага, то есть
// доезжает до вызванного им скрипта.
//
// Это и есть причинная связь для формы 3: без неё производителем оказался бы
// любой шаг, который где-то называет отметку и зовёт скрипт, где-то умеющий
// падать, — то есть требование выполнялось бы почти всюду и не отвергало никого.
func gvMarkReachesScript(st gvStep, name string) bool {
	for _, a := range gvMarkAliases(st, name) {
		if regexp.MustCompile(`\$\{?` + regexp.QuoteMeta(a) + `\b`).MatchString(st.Run) {
			return true
		}
	}
	return false
}

// gvScriptsInvoked — отслеживаемые скрипты, которые зовёт тело шага.
func gvScriptsInvoked(body string, scripts map[string]string) []string {
	var out []string
	for path := range scripts {
		if strings.Contains(body, path) {
			out = append(out, path)
		}
	}
	sort.Strings(out)
	return out
}

// ───────────────────────── сам судья ──────────────────────────────────────────

// judgeGatedVerdict — находки одного объявления процесса плюс его перепись.
//
// Вынесено отдельно от обхода дерева, чтобы способность упасть и способность
// смолчать доказывались инъекцией на входе, а не пересказом цикла.
func judgeGatedVerdict(path, raw string, marks map[string]gvMark,
	scripts map[string]string) ([]string, gvCensus) {
	var census gvCensus
	census.Workflows = 1
	var doc gvDoc
	if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
		return []string{path + ": не разобран YAML: " + err.Error() + " — файл НЕ проверен"},
			census
	}
	census.Jobs = len(doc.Jobs)

	names := make([]string, 0, len(doc.Jobs))
	for n := range doc.Jobs {
		names = append(names, n)
	}
	sort.Strings(names)

	var findings []string
	for _, jobName := range names {
		job := doc.Jobs[jobName]
		for _, st := range job.Steps {
			if strings.TrimSpace(st.If) != "" {
				census.StepsWithIf++
			}
		}
		// ОТМЕТКИ, ОТНОСЯЩИЕСЯ К ЭТОМУ ЗАДАНИЮ, — ПО ДВУМ ПРИЗНАКАМ.
		// Второй не заменяет первого: работа может гасить отметку, которую
		// ставит другая (шард гасит отметку своего подъёма), и может
		// производить отметку, которую не читает ничем.
		relevant := map[string]bool{}
		for name := range marks {
			for _, st := range job.Steps {
				if strings.Contains(st.If, "env."+name) {
					relevant[name] = true
					break
				}
			}
		}
		produced := map[string][]string{}
		for name, mark := range marks {
			if !mark.Suppresses {
				continue // обычная передача значения, а не третья категория
			}
			if coords := gvJobProducesMark(job, mark, scripts); len(coords) > 0 {
				produced[name] = coords
				relevant[name] = true
			}
		}
		observed := make([]string, 0, len(relevant))
		for name := range relevant {
			observed = append(observed, name)
		}
		sort.Strings(observed)

		for _, name := range observed {
			mark := marks[name]
			where := path + ": задание " + jobName + ", отметка " + name
			if !mark.Known {
				findings = append(findings, where+" — значение отметки из дерева НЕ выведено "+
					"(производители: "+strings.Join(mark.Writers, ", ")+"). Пока оно неизвестно, "+
					"неизвестно и какие шаги гасятся, то есть гейт судить не может. "+
					"Сделай запись в $GITHUB_ENV разбираемой: имя и литеральное значение в одной строке")
				continue
			}
			var suppressed, verdicts []string
			for i, st := range job.Steps {
				if gvEval(st.If, name, mark.Value) != gvFalse {
					continue
				}
				census.Suppressed++
				label := "#" + itoa(i+1)
				if st.Name != "" {
					label += " («" + st.Name + "»)"
				}
				suppressed = append(suppressed, label)
				if gvIsHousekeeping(st) {
					census.Housekeeping++
					continue
				}
				census.Verdict++
				verdicts = append(verdicts, label)
			}
			makers := produced[name]
			if len(verdicts) == 0 && len(makers) == 0 {
				continue // законный близнец: гасится только служебное, и не производит
			}
			census.Demanding++
			if len(makers) > 0 {
				census.Producing++
			}
			if gvHasProducerOfRed(doc, jobName, job, name, mark, scripts, &census) {
				continue
			}
			census.WithoutRed++
			// ПРИЧИНА НАЗЫВАЕТСЯ РАЗНЫМИ СЛОВАМИ, потому что чинят их по-разному:
			// у погашенных вердиктов чинить нечего кроме красноты, а у работы без
			// читателей отметка выброшена — и это надо увидеть из текста.
			var why string
			switch {
			case len(verdicts) > 0 && len(makers) > 0:
				why = "она гасит вердиктные шаги " + strings.Join(verdicts, ", ") +
					", и это же задание её ПРОИЗВОДИТ (" + strings.Join(makers, ", ") + ")"
			case len(verdicts) > 0:
				why = "она гасит вердиктные шаги " + strings.Join(verdicts, ", ")
			default:
				why = "это задание её ПРОИЗВОДИТ (" + strings.Join(makers, ", ") +
					"), а НЕ ЧИТАЕТ ни одним условием: третья категория объявлена и " +
					"ВЫБРОШЕНА — гасить некого, шаги ниже отработают на несозданном " +
					"условии и выйдут нулём"
			}
			findings = append(findings, where+" — "+why+
				", а производителя красного у задания НЕТ. "+
				"Значит при выставленной отметке задание дойдёт до конца ЗЕЛЁНЫМ, не вынеся "+
				"ни одного утверждения о своём предмете: сводка покажет success, и человек "+
				"прочтёт это как «проверено и прошло». Отметку ставит "+
				strings.Join(mark.Writers, ", ")+" — и выходит нулём. "+
				"Заведи производителя красного: шаг с условием на отметку в полярности "+
				"«выставлена» и ненулевым выходом (форма 1); либо ветвь в всегда-исполняемом "+
				"шаге, называющую отметку (форма 2); либо всегда-исполняемый шаг, ПЕРЕДАЮЩИЙ "+
				"значение отметки отслеживаемому скрипту, который умеет выйти ненулевым "+
				"(форма 3); либо делегирование нижестоящему заданию с владельцем сводного "+
				"вердикта (форма 4). Текст отказа обязан называть, что условие НЕ СОЗДАНО, — "+
				"различение исходов остаётся в тексте, а не в цвете")
		}
	}
	sort.Strings(findings)
	return findings, census
}

// gvHasProducerOfRed — есть ли у задания производитель красного по любой из
// ЧЕТЫРЁХ форм.
func gvHasProducerOfRed(doc gvDoc, jobName string, job gvJob, name string, mark gvMark,
	scripts map[string]string, census *gvCensus) bool {
	// Формы 1, 2 и 3 — в самом задании.
	for _, st := range job.Steps {
		if gvEval(st.If, name, mark.Value) == gvFalse {
			continue // сам погашен той же отметкой
		}
		if gvTruthy(st.ContinueOnError) {
			continue // отказ проглочен послаблением
		}
		// ПРИЧИННОСТЬ УСТАНАВЛИВАЕТ САМ ШАГ, А НЕ ВЫЗВАННЫЙ ИМ СКРИПТ.
		//
		// Прежняя редакция принимала за производителя всякий шаг, чей скрипт
		// НАЗЫВАЕТ отметку и где-то умеет выходить ненулевым. На дереве это
		// сделало производителем сам ВЛАДЕЛЕЦ ПОДЪЁМА: он называет отметку
		// (он её и ставит) и умеет падать — но на пути с отметкой выходит
		// НУЛЁМ, в том и весь дефект. Гейт от этого молчал на всех трёх
		// заданиях замера. Поэтому связь обязана быть объявлена в самом шаге:
		// либо полярностью в условии, либо именем отметки в его теле.
		condRefs, bodyRefs := st.namesMark(name)
		if !condRefs && !bodyRefs {
			continue
		}
		if gvCanExitNonZero(st.Run) {
			return true // формы 1 и 2: отказ объявлен в самом теле шага
		}
		// ФОРМА 3 — ОТКАЗ ПРОИЗВОДИТ ВЫЗВАННЫЙ СКРИПТ, А НЕ ТЕЛО ШАГА.
		//
		// Причинность даёт одно из двух: полярность в условии («отметка
		// выставлена» — тогда всякий ненулевой выход вызван ею) либо ДОСТАВКА
		// значения отметки в вызов (`--flag "$UNMET"` при
		// `env: UNMET: ${{ env.STAND_PRECONDITION_UNMET }}`).
		//
		// Прежняя редакция требовала ТОЛЬКО полярности в условии — и теряла
		// вторую форму, которой в дереве записаны ВСЕ ТРИ производителя
		// переписи исходов (работы `ci.yaml::helm`, `console-e2e.yml::probes`,
		// `production-posture.yml::stand`). Шаг переписи обязан быть
		// всегда-исполняемым — перепись, гасимая тем, о чём обязана доложить,
		// промолчала бы ровно в своём случае, — поэтому полярности в его
		// условии нет и быть не может, и требование её означало молчание гейта
		// на трёх настоящих производителях сразу.
		if condRefs || gvMarkReachesScript(st, name) {
			for _, s := range gvScriptsInvoked(st.Run, scripts) {
				if gvCanExitNonZero(scripts[s]) {
					return true
				}
			}
		}
	}
	// Форма 4 — делегирование нижестоящему заданию.
	others := make([]string, 0, len(doc.Jobs))
	for n := range doc.Jobs {
		others = append(others, n)
	}
	sort.Strings(others)
	for _, dn := range others {
		if dn == jobName {
			continue
		}
		d := doc.Jobs[dn]
		var depends bool
		for _, n := range d.needs() {
			if n == jobName {
				depends = true
			}
		}
		if !depends {
			continue
		}
		// Делегат, исполняемый ТОЛЬКО на падении, отметкой не поднимается:
		// задание с погашенными вердиктами зелёное.
		cond := strings.TrimSpace(d.If)
		if strings.Contains(cond, "failure()") &&
			!strings.Contains(cond, "always()") && !strings.Contains(cond, "cancelled()") {
			continue
		}
		for _, st := range d.Steps {
			for _, owner := range gvRunLevelVerdictOwners {
				if strings.Contains(st.Run, owner) {
					census.Delegations++
					return true
				}
			}
		}
	}
	return false
}

// ───────────────────────── словарь отметок из дерева ──────────────────────────

var gvEnvWriteLine = regexp.MustCompile(`>>\s*"?\$\{?([A-Za-z_][A-Za-z0-9_]*)\}?`)
var gvEnvAssign = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)=([^\s"']*|"[^"]*"|'[^']*')`)
var gvEnvAlias = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)=\s*"?\$\{?GITHUB_ENV[:}\-"]`)

// gvCollectMarks — отметки дерева и тела отслеживаемых скриптов.
//
// Обходится ИНДЕКС git, а не диск: файл, лежащий рядом и не отслеживаемый, до
// ранера не доедет, и судить по нему значило бы судить чужую рабочую копию.
func gvCollectMarks(t *testing.T, root string) (map[string]gvMark, map[string]string) {
	t.Helper()
	out, err := gitenv.Command(root, "ls-files", "-z", "--",
		"*.sh", "*.bash", "*.py", "*.mjs", "*.js").Output()
	if err != nil {
		t.Fatalf("git ls-files: %v — состав дерева не установлен, и «ноль отметок» "+
			"стало бы неотличимо от «ноль прочитанного»", err)
	}
	scripts := map[string]string{}
	marks := map[string]gvMark{}

	addWrites := func(coord, body string) {
		aliases := map[string]bool{"GITHUB_ENV": true}
		for _, m := range gvEnvAlias.FindAllStringSubmatch(body, -1) {
			aliases[m[1]] = true
		}
		for i, raw := range strings.Split(body, "\n") {
			line := strings.TrimSpace(raw)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			w := gvEnvWriteLine.FindStringSubmatch(line)
			if w == nil || !aliases[w[1]] {
				continue
			}
			names := gvEnvAssign.FindAllStringSubmatch(line[:strings.Index(line, ">>")], -1)
			var took bool
			for _, n := range names {
				if aliases[n[1]] || n[1] == "GITHUB_OUTPUT" {
					continue
				}
				val := strings.Trim(n[2], `"'`)
				known := val != "" && !strings.ContainsAny(val, "$`")
				prev, seen := marks[n[1]]
				mk := gvMark{Name: n[1], Value: val, Known: known,
					Writers: []string{coord + ":" + itoa(i+1)}}
				if seen {
					mk.Writers = append(prev.Writers, mk.Writers...)
					if prev.Known && (!known || prev.Value != val) {
						mk.Value, mk.Known = prev.Value, prev.Known
					}
				}
				marks[n[1]] = mk
				took = true
			}
			if !took {
				t.Errorf("%s:%d — запись в $GITHUB_ENV, из которой имя не разобрано: %q. "+
					"Словарь отметок остался бы неполон, а неполный словарь МОЛЧА сужает "+
					"этот гейт: пара «задание · отметка» не попала бы в предмет вовсе. "+
					"Напиши имя и значение литералом в той же строке", coord, i+1, line)
			}
		}
	}

	for _, rel := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if rel == "" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Errorf("%s не прочитан: %v — файл НЕ осмотрен", rel, err)
			continue
		}
		scripts[rel] = string(b)
		if strings.Contains(scripts[rel], "GITHUB_ENV") {
			addWrites(rel, scripts[rel])
		}
	}
	// Прямые записи в телах шагов процессов — та же форма, другое место.
	docs := map[string]gvDoc{}
	for _, wf := range listWorkflows(t, root) {
		b, err := os.ReadFile(filepath.Join(root, wf))
		if err != nil {
			t.Errorf("%s не прочитан: %v — файл НЕ осмотрен", wf, err)
			continue
		}
		var doc gvDoc
		if err := yaml.Unmarshal(b, &doc); err != nil {
			t.Errorf("%s не разобран: %v — файл НЕ осмотрен", wf, err)
			continue
		}
		docs[wf] = doc
		jobs := make([]string, 0, len(doc.Jobs))
		for n := range doc.Jobs {
			jobs = append(jobs, n)
		}
		sort.Strings(jobs)
		for _, jn := range jobs {
			for i, st := range doc.Jobs[jn].Steps {
				if strings.Contains(st.Run, "GITHUB_ENV") {
					addWrites(wf+" ("+jn+", шаг #"+itoa(i+1)+")", st.Run)
				}
			}
		}
	}
	// СУППРЕССИВНОСТЬ ВЫВОДИТСЯ ИЗ ДЕРЕВА, а не из имени отметки: отметка
	// суппрессивна, если ГАСИТ вердиктный шаг хоть в одном задании. Признак
	// считается ТЕМИ ЖЕ [gvEval] и [gvIsHousekeeping], которыми судит судья, —
	// второе определение того же предиката разошлось бы с первым молча.
	//
	// Обход идёт по ВСЕМУ дереву, а не по файлу: отметку ставит один процесс, а
	// гасить её может другой, и судья видит по одному файлу за раз.
	for name, mark := range marks {
		if !mark.Known {
			continue // пока значение неизвестно, неизвестно и что гасится
		}
		var suppresses bool
		for _, doc := range docs {
			for _, job := range doc.Jobs {
				for _, st := range job.Steps {
					if gvEval(st.If, name, mark.Value) == gvFalse && !gvIsHousekeeping(st) {
						suppresses = true
					}
				}
			}
		}
		if suppresses {
			mark.Suppresses = true
			marks[name] = mark
		}
	}
	return marks, scripts
}

// gvTreeCollectionCount — коллекций сквозных проб в дереве. Спрашивается у
// дерева, а не выписывается числом.
func gvTreeCollectionCount(t *testing.T, root string) int {
	t.Helper()
	out, err := gitenv.Command(root, "ls-files", "--",
		"services/*/tests/newman/collections/*.postman_collection.json",
		"gateway/tests/newman/collections/*.postman_collection.json").Output()
	if err != nil {
		t.Fatalf("git ls-files: %v — число коллекций дерева не установлено", err)
	}
	n := 0
	for _, l := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(l) != "" {
			n++
		}
	}
	return n
}

// gvPackageRel — путь этого пакета от корня. ВЫВОДИТСЯ из места файла, а не
// выписывается: переезд пробы в другой пакет страж по литералу пережил бы
// зелёным.
func gvPackageRel(t *testing.T, root string) string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("место этого файла не определено — имя пакета для провязки выводить неоткуда")
	}
	rel, err := filepath.Rel(root, filepath.Dir(self))
	if err != nil {
		t.Fatalf("путь пакета относительно корня: %v", err)
	}
	return filepath.ToSlash(rel)
}

// TestGatedVerdictStepsHaveAProducerOfRed — по дереву.
func TestGatedVerdictStepsHaveAProducerOfRed(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	files := listWorkflows(t, root)
	if len(files) == 0 {
		t.Fatalf("в %s не найдено ни одного процесса — обход сломан, а не дерево чисто", workflowsDir)
	}
	marks, scripts := gvCollectMarks(t, root)
	if len(marks) == 0 {
		t.Fatal("в дереве не найдено НИ ОДНОЙ записи в $GITHUB_ENV — предмета у гейта " +
			"не осталось бы вовсе, и его молчание было бы молчанием об непрочитанном. " +
			"Либо распознаватель формы записи сломан, либо владелец материализации " +
			"перестал ставить отметку — оба случая обязаны быть красными")
	}

	var total gvCensus
	for _, f := range files {
		raw, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			t.Errorf("%s не прочитан: %v — файл НЕ проверен", f, err)
			continue
		}
		findings, census := judgeGatedVerdict(f, string(raw), marks, scripts)
		total.add(census)
		for _, msg := range findings {
			t.Error(msg)
		}
	}
	names := make([]string, 0, len(marks))
	for n := range marks {
		names = append(names, n)
	}
	sort.Strings(names)
	suppressive := make([]string, 0, len(names))
	for _, n := range names {
		if marks[n].Suppresses {
			suppressive = append(suppressive, n)
		}
	}
	t.Logf("осмотрено: процессов %d, заданий %d, шагов с условием %d; "+
		"погашено отметкой %d, из них вердиктных %d и служебных %d; "+
		"пар «задание · отметка», потребовавших производителя красного, %d "+
		"(из них задание САМО производит отметку в %d), из них без производителя %d; "+
		"закрыто делегированием %d; отметок дерева %d (%s), из них суппрессивных %d (%s)",
		total.Workflows, total.Jobs, total.StepsWithIf,
		total.Suppressed, total.Verdict, total.Housekeeping,
		total.Demanding, total.Producing, total.WithoutRed,
		total.Delegations, len(marks), strings.Join(names, ", "),
		len(suppressive), strings.Join(suppressive, ", "))
	if len(suppressive) == 0 {
		t.Error("ни одна отметка дерева не признана суппрессивной — признак «работа " +
			"производит отметку» тогда не применяется НИ К ЧЕМУ, и его молчание было бы " +
			"молчанием о непрочитанном")
	}
	if total.Suppressed == 0 {
		t.Error("ни один шаг дерева не погашен отметкой — либо вычислитель условий " +
			"перестал их узнавать, либо отметки больше не гасят ничего. «Ноль находок» " +
			"обязано быть отличимо от «ноль прочитанного»")
	}
}
