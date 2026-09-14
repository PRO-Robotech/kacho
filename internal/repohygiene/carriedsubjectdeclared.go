// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/PRO-Robotech/kacho/internal/productnaming"
)

// carriedsubjectdeclared.go — разбор класса «надгробие утверждает „предмета
// нет“ после того, как предмет УЕХАЛ».
//
// Живёт в НЕ-тестовом файле намеренно: инъекция обязана звать ТУ ЖЕ функцию, что
// и гейт, иначе она доказывает свойство своей копии.
//
// # Предмет: запись надгробия отвечает на вопрос, ради которого её читают
//
// Запись [GateCarrierRetirement] читают затем, чтобы узнать, ПОЧЕМУ проверки
// больше нет. Ответ «снято вместе с носителем» означает «свойство умерло,
// судить нечего». Для носителя, чей предмет уехал в репозиторий службы и
// стережётся там, этот ответ ложен, и ложь особого рода: следующий, кто придёт
// восстанавливать проверку, восстановит ВТОРУЮ — и получит два места об одном
// предмете, из которых верно одно.
//
// Замер, из которого гейт выведен (задача продукта #2602): из 173 записей
// надгробия 71 утверждала ровно «снято вместе с носителем». Сопоставление
// утверждений с утверждениями гейтов службы показало, что часть этих предметов
// не исчезла, а переехала.
//
// # Почему прозы поля [GateCarrierRetirement.Successor] НЕ ХВАТАЕТ
//
// «Уехало» и «исчезло» до этого гейта выражались ОДНОЙ строкой прозы, поэтому
// машинно не различались — и ровно поэтому дефект возник молча. Проза остаётся:
// она объясняет ЧИТАТЕЛЮ. Но судьба предмета объявляется отдельным полем из
// ЗАКРЫТОГО словаря ([GateCarrierRetirement.Fate]), а координата — полем
// [GateCarrierRetirement.CarriedTo]. Отсутствие поля есть отдельное состояние —
// «никто не думал», — и оно ловится, а не читается как «предмета нет».
//
// Значений ТРИ, и третье добыто замером, а не симметрией: [subjectFateGone],
// [subjectFateCarried], [subjectFateUnguarded]. Двух не хватило пятнадцати
// предметам, которые уехали и не получили держателя ни здесь, ни там: `gone`
// отрицал бы живое свойство, `carried` обещал бы несуществующего стража.
//
// # Почему судьба НЕ ВЫВОДИТСЯ из прозы: предикат посчитал бы собственный предмет
//
// Соблазн — искать в прозе имя репозитория-преемника и краснеть на «gone» с ним
// рядом. Замер отверг: имя службы стоит в прозе 14 записей, и часть этих
// упоминаний — ИМЯ САМОГО НОСИТЕЛЯ, названное причиной снятия
// (`clienttruth_kaname_exclusion_form`). Предикат по подстроке считал бы своё
// же объяснение — тот самый класс, против которого корпус и написан.
//
// # Почему словарь репозиториев-преемников БЕРЁТСЯ У ДЕРЕВА, а не выписывается
//
// Литерал имени чужого репозитория в этом файле разошёлся бы с деревом молча.
// Словарь собирается из ДВУХ объявлений, и оба принадлежат дереву:
//
//   - [productnaming.ExternallySourcedServices] — части продукта, чьи ИСХОДНИКИ
//     живут в другом репозитории; там же сказано, в каком;
//   - `go.mod` — модули ТОГО ЖЕ владельца, от которых дерево зависит. Разрез
//     монорепо увёл не только службу: фундамент (`pkg/treecorpus`,
//     `pkg/dropguard`) уехал в общую библиотеку, и она названа деревом ровно
//     здесь — строкой зависимости, а не чьей-то памятью.
//
// Вторая половина словаря добыта ЗАМЕРОМ, а не предусмотрительностью: разбор
// ведомости дал четыре записи, чей предмет уехал НЕ в репозиторий службы, и на
// первом прогоне гейт назвал их находкой «репозиторий не объявлен деревом».
// Находка была верна о словаре, а не о записях.
//
// Репозиторий, не названный ни одним из двух объявлений, в надгробии недопустим:
// строка ссылалась бы в никуда, а «уехало куда-то» не восстанавливает следующий
// шаг читателя. Ведомость самоистекает: уйдёт зависимость — перестанет
// приниматься и координата в неё.
//
// # СЕМЕЙНАЯ СВЯЗНОСТЬ — несущее правило, и оно выведено из формы дефекта
//
// Половина надгробия — не гейты, а их производные: `*_injection_test.go`
// (доказательство падучести) и файл-годок, которым пользовался один гейт. У
// производной записи НЕТ своего предмета: её предмет — предмет её семейства.
// Значит гейт и его производные не могут объявлять разную судьбу иначе как
// ложью, и это ровно та форма, которой дефект и наблюдался: гейт говорил
// «уехало», его инъекция — «предмета нет».
//
// Семья опознаётся по [carrierFamily]: каталог плюс имя без суффиксов носителя.
// Судятся [GateCarrierRetirement.Fate] и репозиторий; координата у каждого члена
// семьи СВОЯ (у гейта — гейт службы, у инъекции — инъекция службы), поэтому она
// не сверяется.
//
// # Границы названы, а не умолчаны
//
// - ГЕЙТ НЕ ЧИТАЕТ ДЕРЕВО СЛУЖБЫ и не может: оно вне этого репозитория.
// Сопоставление утверждения с утверждением сделано ОДИН раз — чтением, и его
// результат лежит теперь в надгробии. Гейт держит объявление связным и не даёт
// следующей записи промолчать о судьбе.
//
// - КООРДИНАТА ПРОВЕРЯЕТСЯ, КОГДА ДЕРЕВО-ПРЕЕМНИК ДОСТУПНО. Ручка
// [carriedSubjectTreesEnv] называет корень клона; тогда каждая координата
// резолвится файлом, и висячая координата — находка. Ручки нет — координаты не
// проверялись, и перепись говорит это ПРЯМО: пропуск, названный числом, а не
// пустой успех.
//
// - ВЫХОЛАЩИВАНИЕ ЗАПИСИ НАЗВАННЫМ СЛОВОМ. Запись, объявившая `carried` и
// назвавшая координату файла, который в службе ничего не стережёт, здесь не
// ловится: судить содержимое чужого дерева этот гейт не вправе. Это соседний
// класс, и держится он ручкой выше плюс чтением при заведении записи.
//
// - ЗАПИСИ-ПРИСТАВКИ ([GateCarrierScopeRetirement]) судьбы не несут и здесь НЕ
// судятся. Причина не в объёме работы: их единица — приставка пути, поэтому
// координатой преемника был бы КАТАЛОГ, а предикат «каталог в чужом дереве
// есть» выполняется подстановкой почти любого имени и не сужает ничего. Сверх
// того единственная такая запись в дереве уже различает две мысли прозой прямо
// («предмет уехал ВМЕСТЕ с носителями»), то есть предмета у машинного поля там
// нет. Судит их перепись снятых путей (removedpathcensus.go).

// subjectFateGone — предмета больше нет: ни здесь, ни в дереве службы.
const subjectFateGone = "gone"

// subjectFateCarried — предмет уехал и стережётся в репозитории-преемнике;
// координата держателя названа полем [GateCarrierRetirement.CarriedTo].
const subjectFateCarried = "carried"

// subjectFateUnguarded — предмет уехал и НЕ стережётся НИКЕМ: ни здесь, ни там.
//
// Третье значение заведено ЗАМЕРОМ, а не симметрией: разбор 173 записей дал
// пятнадцать таких предметов. Двух значений им не хватает, и оба соврали бы —
// `gone` отрицает живое свойство, `carried` обещает держателя, которого нет.
// Названный остаток тем и отличается от прощённого, что он СЧИТАЕТСЯ: перепись
// печатает его число, и восстанавливать проверку пойдут туда, где предмет живёт.
const subjectFateUnguarded = "unguarded"

// subjectFateRegained — предмет ОСТАЛСЯ здесь и снова стережётся: держатель в
// ЭТОМ дереве, и его координата названа полем [GateCarrierRetirement.GuardedHere].
//
// Четвёртое значение заведено ЗАМЕРОМ, а не симметрией (задача
// PRO-Robotech/kacho#2613). Две семьи носителей — провязка побайтовой сверки
// модели и провязка судьи ФОРМЫ манифеста — стерегли предмет, который вместе со
// службой НЕ УЕХАЛ: пять документов `services/*/manifest.yaml` как лежали здесь,
// так и лежат; уехал их ИСПОЛНИТЕЛЬ. Обе семьи объявляли `unguarded`, и после
// того как держатели завелись здесь заново, все три прежних значения стали
// ложью: `gone` отрицает живой предмет, `carried` обещает стража в чужом дереве,
// `unguarded` утверждает, что стража нет вовсе.
//
// ПОЧЕМУ ЭТО НЕ «ПРОСТО СНЯТЬ ЗАПИСЬ». Носитель снят — это факт, и надгробие
// объявляет именно снятие. Снять запись значило бы сделать прежнее снятие
// молчаливым: гейт-сосед (gatecarrierremoval.go) нашёл бы его находкой заново.
// Меняется не факт снятия, а судьба ПРЕДМЕТА — ровно то, ради чего поле и
// заведено.
const subjectFateRegained = "regained"

// subjectFates — ЗАКРЫТЫЙ словарь судеб. Значение — что запись этим объявляет;
// оно идёт в текст находки, чтобы сообщение называло предмет, а не имя ключа.
var subjectFates = map[string]string{
	subjectFateGone:      "предмета больше нет — судить нечего",
	subjectFateCarried:   "предмет уехал и стережётся в репозитории-преемнике",
	subjectFateUnguarded: "предмет уехал и не стережётся никем — названный остаток",
	subjectFateRegained:  "предмет остался здесь и снова стережётся — держатель в этом дереве",
}

// carriedSubjectTreesEnv — ручка, которой вызывающий даёт корни клонов
// репозиториев-преемников. Форма: `repo=path[,repo=path]`.
//
// Нужна тому, у кого оба дерева под рукой: конвейеру службы и человеку с двумя
// клонами. Без неё координаты не резолвятся, и перепись это называет.
const carriedSubjectTreesEnv = "KACHO_CARRIED_SUBJECT_TREES"

// carrierSuffixes — суффиксы имён носителей, по которым узнаётся СЕМЬЯ.
//
// Порядок значим: длинный суффикс проверяется раньше короткого, иначе
// `_injection_test.go` срезался бы как `_test.go` и инъекция уехала бы в чужую
// семью.
var carrierSuffixes = []string{
	"_injection_test.go",
	"_boundary_test.go",
	"_test.go",
	".go",
}

// carriedSubjectCensus — объём осмотренного. Печатается держателем целиком:
// одного числа мало ровно в том случае, ради которого гейт заведён.
type carriedSubjectCensus struct {
	// Rows — записей надгробия прочитано.
	Rows int
	// Gone, Carried, Unguarded, Regained — сколько записей какую судьбу объявили.
	Gone, Carried, Unguarded, Regained int
	// Families — семей носителей среди прочитанных записей.
	Families int
	// Repos — репозиториев-преемников в словаре владельца имён.
	Repos int
	// Checked — координат, СВЕРЕННЫХ с деревом-преемником.
	Checked int
	// Unchecked — координат, которые не сверялись: дерева-преемника нет под
	// рукой. Печатается ОТДЕЛЬНО от Checked: пропуск не зачитывается в проход.
	Unchecked int
}

// successorRepos — репозитории, в которые предмет мог уехать.
//
// Берётся у дерева двумя объявлениями, а не выписывается: см. шапку, §«Почему
// словарь репозиториев-преемников БЕРЁТСЯ У ДЕРЕВА».
func successorRepos(root string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, repo := range productnaming.ExternallySourcedServices() {
		out[repo] = true
	}
	deps, err := sameOwnerModuleRepos(root)
	if err != nil {
		return nil, err
	}
	for _, repo := range deps {
		out[repo] = true
	}
	return out, nil
}

// goModName — имя описания модуля. Читается, а не угадывается: имя владельца
// берётся из строки `module`, поэтому переименование владельца не оставляет
// здесь второго написания.
const goModName = "go.mod"

// moduleRequireRe — строка зависимости: путь модуля и его версия.
var moduleRequireRe = regexp.MustCompile(`(?m)^\s*(\S+)\s+v\S+`)

// moduleLineRe — объявление собственного пути модуля.
var moduleLineRe = regexp.MustCompile(`(?m)^module\s+(\S+)`)

// sameOwnerModuleRepos — репозитории ТОГО ЖЕ владельца, от которых дерево
// зависит, в форме `<владелец>/<имя>`.
//
// Свой репозиторий исключён: запись, называющая преемником это же дерево,
// объявляла бы переезд туда, откуда носитель снят.
func sameOwnerModuleRepos(root string) ([]string, error) {
	// #nosec G304 — путь собран из корня, названного вызывающим, и КОНСТАНТЫ.
	b, err := os.ReadFile(filepath.Join(root, goModName))
	if err != nil {
		return nil, fmt.Errorf("%s: %w — словарь репозиториев-преемников собрать не из чего, "+
			"и это отказ, а не пустой успех", goModName, err)
	}
	body := string(b)
	self := moduleLineRe.FindStringSubmatch(body)
	if self == nil {
		return nil, fmt.Errorf("%s не объявляет собственного пути модуля — "+
			"владельца, чьи репозитории считаются своими, взять неоткуда", goModName)
	}
	owner, selfRepo, ok := ownerAndRepo(self[1])
	if !ok {
		return nil, fmt.Errorf("%s: путь модуля %q не даёт пары «владелец/репозиторий»",
			goModName, self[1])
	}
	seen := map[string]bool{}
	var repos []string
	for _, m := range moduleRequireRe.FindAllStringSubmatch(body, -1) {
		o, r, ok := ownerAndRepo(m[1])
		if !ok || o != owner || r == selfRepo || seen[o+"/"+r] {
			continue
		}
		seen[o+"/"+r] = true
		repos = append(repos, o+"/"+r)
	}
	sort.Strings(repos)
	return repos, nil
}

// ownerAndRepo — владелец и репозиторий из пути модуля `<хост>/<владелец>/<имя>[/…]`.
func ownerAndRepo(modulePath string) (owner, repo string, ok bool) {
	parts := strings.Split(modulePath, "/")
	if len(parts) < 3 {
		return "", "", false
	}
	return parts[1], parts[2], true
}

// carrierFamily — семья носителя: каталог плюс имя без суффикса носителя.
//
// Годок, его держатель, его доказательство падучести и его граничная проба дают
// ОДНУ семью: суффикс срезается, остаётся общий корень вместе с каталогом.
// Проверено пробой предпосылки в доказательстве падучести этого разбора.
func carrierFamily(carrier string) string {
	dir, base := path.Split(carrier)
	for _, suf := range carrierSuffixes {
		if strings.HasSuffix(base, suf) {
			base = strings.TrimSuffix(base, suf)
			break
		}
	}
	return dir + base
}

// carriedSubjectTrees — значение ручки [carriedSubjectTreesEnv] из среды прогона.
func carriedSubjectTrees() (map[string]string, string, error) {
	return parseCarriedSubjectTrees(os.Getenv(carriedSubjectTreesEnv))
}

// parseCarriedSubjectTrees — разбор ЗНАЧЕНИЯ ручки.
//
// Возвращает корни по репозиториям и словесное описание для переписи. Пустое
// значение — НЕ ошибка: это законное «деревьев-преемников под рукой нет», и
// перепись назовёт несверенные координаты числом.
//
// Разведено с добычей из среды намеренно: проба разбора, правящая среду
// прогона, обязана быть последовательной, а пакет — единица бюджета времени
// (probeparallelism_test.go). Здесь разбор судится значениями, и проба
// параллельна.
func parseCarriedSubjectTrees(value string) (map[string]string, string, error) {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return nil, fmt.Sprintf("%s не задана — координаты в дереве-преемнике НЕ проверялись",
			carriedSubjectTreesEnv), nil
	}
	trees := map[string]string{}
	var named []string
	for _, pair := range strings.Split(raw, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		repo, root, ok := strings.Cut(pair, "=")
		repo, root = strings.TrimSpace(repo), strings.TrimSpace(root)
		if !ok || repo == "" || root == "" {
			return nil, "", fmt.Errorf(
				"%s=%q: форма записи `repo=path`, а эта пара её не даёт — "+
					"это отказ, а не пустой успех", carriedSubjectTreesEnv, pair)
		}
		if !filepath.IsAbs(root) {
			return nil, "", fmt.Errorf(
				"%s: корень %q для %s не абсолютен — путь, зависящий от рабочего каталога, "+
					"дал бы вердикт о разных деревьях в разных прогонах",
				carriedSubjectTreesEnv, root, repo)
		}
		// #nosec G703 — корень называет ТОТ, КТО ЗАПУСКАЕТ прогон: это ручка
		// разработчика и конвейера, а не вход арендатора. Значение уже отвергнуто
		// выше, если не абсолютно; читается здесь только вид записи каталога.
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			return nil, "", fmt.Errorf(
				"%s: корень %q для %s не каталог — сравнить не с чем, и это отказ, "+
					"а не пустой успех", carriedSubjectTreesEnv, root, repo)
		}
		trees[repo] = root
		named = append(named, repo+"="+root)
	}
	sort.Strings(named)
	return trees, strings.Join(named, " "), nil
}

// carriedCoordinateResolver — «есть ли в дереве-преемнике этот файл».
//
// Передаётся, а не зовётся напрямую, чтобы суждение осталось проверяемым без
// второго клона. Второй возврат — проверялась ли координата вообще: пропуск
// обязан быть отличим от проходa.
type carriedCoordinateResolver func(repo, rel string) (exists, checked bool)

// resolveInTrees — резолвер по деревьям, названным ручкой.
func resolveInTrees(trees map[string]string) carriedCoordinateResolver {
	return func(repo, rel string) (bool, bool) {
		root, ok := trees[repo]
		if !ok {
			return false, false
		}
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
		return err == nil && !info.IsDir(), true
	}
}

// judgeCarriedSubjects — ЧИСТОЕ суждение: связно ли надгробие объявляет судьбу
// каждого предмета.
//
// Разведено с добычей входа намеренно: инъекция гоняет эту функцию на настоящей
// ведомости, а не на её копии.
// root — корень ЭТОГО дерева: по нему резолвится координата держателя у судьбы
// [subjectFateRegained]. Она единственная проверяется всегда, поэтому корень
// здесь обязателен, а не факультативен, как клон соседа.
func judgeCarriedSubjects(
	root string, rows []GateCarrierRetirement, repos map[string]bool,
	resolve carriedCoordinateResolver,
) ([]string, carriedSubjectCensus) {
	census := carriedSubjectCensus{Rows: len(rows), Repos: len(repos)}
	var findings []string

	sorted := append([]GateCarrierRetirement(nil), rows...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Carrier < sorted[j].Carrier })

	families := map[string][]GateCarrierRetirement{}
	for _, r := range sorted {
		if r.Carrier == "" {
			continue // запись без носителя судит соседний гейт (gatecarrierremoval.go)
		}
		fam := carrierFamily(r.Carrier)
		families[fam] = append(families[fam], r)

		findings = append(findings, judgeOneSubjectFate(root, r, repos, resolve, &census)...)
	}
	census.Families = len(families)
	findings = append(findings, judgeFamilyAgreement(families)...)

	sort.Strings(findings)
	return findings, census
}

// judgeOneSubjectFate — судьба ОДНОЙ записи: объявлена ли, из словаря ли, и
// сходится ли с координатой.
//
// Запись, чья судьба не объявлена, в сверку по семье не идёт (см.
// [judgeFamilyAgreement]): иначе одна ошибка дала бы находку на каждом члене
// семьи, и предмет находки сместился бы с записи на её родню.
func judgeOneSubjectFate(
	root string, r GateCarrierRetirement, repos map[string]bool,
	resolve carriedCoordinateResolver, census *carriedSubjectCensus,
) []string {
	var findings []string
	switch {
	case strings.TrimSpace(r.Fate) == "":
		return append(findings, fmt.Sprintf(
			"%s: %q снят, но судьба его ПРЕДМЕТА не объявлена полем %q. "+
				"Проза поля %q объясняет читателю и машинно не различает «уехало» от «исчезло» — "+
				"именно поэтому запись и могла солгать молча. Словарь ЗАКРЫТ: %s",
			gateCarrierLedgerName, r.Carrier, "fate", "successor", knownSubjectFates()))
	case subjectFates[r.Fate] == "":
		return append(findings, fmt.Sprintf(
			"%s: %q объявил судьбу %q, которой в словаре нет. Словарь ЗАКРЫТ: %s",
			gateCarrierLedgerName, r.Carrier, r.Fate, knownSubjectFates()))
	}

	if r.Fate == subjectFateGone {
		census.Gone++
		if r.CarriedTo != nil {
			findings = append(findings, fmt.Sprintf(
				"%s: %q объявил %q — предмета нет, — и при этом назвал координату преемника %s. "+
					"Два утверждения об одном предмете, и верно одно: либо предмет уехал, "+
					"либо координате взяться неоткуда",
				gateCarrierLedgerName, r.Carrier, subjectFateGone, r.CarriedTo.describe()))
		}
		return findings
	}

	if r.Fate == subjectFateRegained {
		census.Regained++
		return append(findings, judgeRegainedSubject(r, root)...)
	}

	// Координата держателя В ЭТОМ дереве принадлежит ровно одной судьбе.
	// Названная при любой другой, она обещает стража там, где запись же и
	// объявила его отсутствие либо отъезд.
	if strings.TrimSpace(r.GuardedHere) != "" {
		findings = append(findings, fmt.Sprintf(
			"%s: %q объявил судьбу %q и назвал держателя В ЭТОМ дереве (%q). "+
				"Поле %q принадлежит судьбе %q и только ей: при остальных запись утверждает, "+
				"что здесь предмета либо стража нет",
			gateCarrierLedgerName, r.Carrier, r.Fate, r.GuardedHere, "guarded_here",
			subjectFateRegained))
	}

	if r.Fate == subjectFateUnguarded {
		census.Unguarded++
		return append(findings, judgeUnguardedSubject(r, repos)...)
	}

	census.Carried++
	return append(findings, judgeCarriedCoordinate(r, repos, resolve, census)...)
}

// judgeRegainedSubject — предмет остался здесь и снова стережётся.
//
// Координата держателя ОБЯЗАТЕЛЬНА и, в отличие от координаты в чужом дереве,
// ПРОВЕРЯЕТСЯ ВСЕГДА: она лежит в этом же репозитории, и клона соседа для неё не
// требуется. Поэтому у неё нет состояния «не сверялась» — назвав несуществующий
// файл, запись объявила бы стража, которого нет, тем же способом, каким лгала
// прежняя редакция.
//
// Координата чужого репозитория здесь запрещена: предмет никуда не уезжал, и
// назвать преемника значило бы поставить рядом два утверждения об одном
// предмете.
func judgeRegainedSubject(r GateCarrierRetirement, root string) []string {
	var findings []string
	rel := strings.TrimSpace(r.GuardedHere)
	if rel == "" {
		return append(findings, fmt.Sprintf(
			"%s: %q объявил %q, но не назвал держателя В ЭТОМ дереве. «Снова стережётся» "+
				"без координаты неотличимо от «не стережётся никем» — то есть от той самой лжи, "+
				"против которой поле и заведено",
			gateCarrierLedgerName, r.Carrier, subjectFateRegained))
	}
	if r.CarriedTo != nil {
		findings = append(findings, fmt.Sprintf(
			"%s: %q объявил %q — предмет остался здесь, — и назвал координату преемника %s. "+
				"Два утверждения об одном предмете, и верно одно",
			gateCarrierLedgerName, r.Carrier, subjectFateRegained, r.CarriedTo.describe()))
	}
	if why := judgeCarriedPathForm(r.Carrier, rel); why != "" {
		return append(findings, why)
	}
	if st, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil || !st.Mode().IsRegular() {
		findings = append(findings, fmt.Sprintf(
			"%s: %q называет держателем %q, а такого файла В ЭТОМ дереве НЕТ. Это единственная "+
				"координата надгробия, которую дерево проверяет само, и висячей она быть не "+
				"вправе: читатель уйдёт по ней в пустоту",
			gateCarrierLedgerName, r.Carrier, rel))
	}
	return findings
}

// judgeUnguardedSubject — названный остаток: предмет уехал, держателя нет.
//
// Репозиторий обязателен — он говорит, ГДЕ предмет живёт и куда идти тому, кто
// возьмётся его стеречь. Координата держателя запрещена: назвать её значило бы
// назвать того, кого запись же и объявила отсутствующим.
func judgeUnguardedSubject(r GateCarrierRetirement, repos map[string]bool) []string {
	var findings []string
	if r.CarriedTo == nil || strings.TrimSpace(r.CarriedTo.Repo) == "" {
		return append(findings, fmt.Sprintf(
			"%s: %q объявил %q, но не назвал репозиторий, где предмет живёт. "+
				"Остаток без координаты неотличим от «предмета нет» — то есть от той самой лжи, "+
				"против которой поле и заведено",
			gateCarrierLedgerName, r.Carrier, subjectFateUnguarded))
	}
	repo := strings.TrimSpace(r.CarriedTo.Repo)
	if !repos[repo] {
		findings = append(findings, fmt.Sprintf(
			"%s: %q называет преемником %q, а дерево такого репозитория не объявляет. "+
				"Владельцы объявления — productnaming.ExternallySourcedServices и %s",
			gateCarrierLedgerName, r.Carrier, repo, goModName))
	}
	if strings.TrimSpace(r.CarriedTo.Path) != "" {
		findings = append(findings, fmt.Sprintf(
			"%s: %q объявил %q — держателя нет, — и тут же назвал координату держателя %s. "+
				"Два утверждения об одном предмете, и верно одно",
			gateCarrierLedgerName, r.Carrier, subjectFateUnguarded, r.CarriedTo.describe()))
	}
	return findings
}

// judgeCarriedCoordinate — координата предмета, уехавшего вместе со службой.
func judgeCarriedCoordinate(
	r GateCarrierRetirement, repos map[string]bool,
	resolve carriedCoordinateResolver, census *carriedSubjectCensus,
) []string {
	var findings []string
	if r.CarriedTo == nil {
		return append(findings, fmt.Sprintf(
			"%s: %q объявил %q, но координаты преемника не назвал. «Уехало куда-то» не "+
				"восстанавливает следующий шаг читателя — ровно за этим запись и заводится",
			gateCarrierLedgerName, r.Carrier, subjectFateCarried))
	}
	repo, rel := strings.TrimSpace(r.CarriedTo.Repo), strings.TrimSpace(r.CarriedTo.Path)
	switch {
	case repo == "":
		findings = append(findings, fmt.Sprintf(
			"%s: %q объявил %q без репозитория-преемника",
			gateCarrierLedgerName, r.Carrier, subjectFateCarried))
	case !repos[repo]:
		findings = append(findings, fmt.Sprintf(
			"%s: %q называет преемником %q, а дерево такого репозитория не объявляет. "+
				"Владельцы объявления — productnaming.ExternallySourcedServices и %s; "+
				"репозиторий, не названный ни одним, есть ссылка в никуда",
			gateCarrierLedgerName, r.Carrier, repo, goModName))
	}
	if f := judgeCarriedPathForm(r.Carrier, rel); f != "" {
		return append(findings, f)
	}
	if repo == "" || resolve == nil {
		census.Unchecked++
		return findings
	}
	exists, checked := resolve(repo, rel)
	switch {
	case !checked:
		census.Unchecked++
	case exists:
		census.Checked++
	default:
		census.Checked++
		findings = append(findings, fmt.Sprintf(
			"%s: %q называет координату %s, а в дереве-преемнике такого файла НЕТ — "+
				"координата висячая, и читатель уйдёт по ней в пустоту",
			gateCarrierLedgerName, r.Carrier, r.CarriedTo.describe()))
	}
	return findings
}

// judgeCarriedPathForm — форма координаты внутри дерева-преемника.
//
// Пустая строка означает «форма годна». Координата обязана быть путём ОТ КОРНЯ
// чужого дерева: абсолютный путь и восхождение ссылаются на машину того, кто
// писал запись, а не на дерево.
func judgeCarriedPathForm(carrier, rel string) string {
	var why string
	switch {
	case rel == "":
		why = "координата пуста"
	case strings.HasPrefix(rel, "/"):
		why = "координата абсолютна — путь чужой машины, а не чужого дерева"
	case strings.Contains(rel, `\`):
		why = "координата с обратной косой — путями git она не записывается"
	case rel != path.Clean(rel), strings.HasPrefix(rel, "../"):
		why = "координата не приведена к виду git: восхождение или лишние звенья"
	}
	if why == "" {
		return ""
	}
	return fmt.Sprintf("%s: %q — %s (%q)", gateCarrierLedgerName, carrier, why, rel)
}

// judgeFamilyAgreement — гейт и его производные объявляют ОДНУ судьбу.
//
// Почему это несущее правило и почему разойтись они могут только ложью — шапка,
// §«СЕМЕЙНАЯ СВЯЗНОСТЬ».
func judgeFamilyAgreement(families map[string][]GateCarrierRetirement) []string {
	var findings []string
	fams := make([]string, 0, len(families))
	for fam := range families {
		fams = append(fams, fam)
	}
	sort.Strings(fams)

	for _, fam := range fams {
		seen := map[string]string{} // судьба (с репозиторием) → первый носитель, её объявивший
		for _, r := range families[fam] {
			if subjectFates[r.Fate] == "" {
				continue // судьба не объявлена — это уже находка выше
			}
			key := r.Fate
			if r.Fate == subjectFateCarried && r.CarriedTo != nil {
				key += " " + strings.TrimSpace(r.CarriedTo.Repo)
			}
			if _, ok := seen[key]; !ok {
				seen[key] = r.Carrier
			}
		}
		if len(seen) < 2 {
			continue
		}
		var said []string
		for key, carrier := range seen {
			said = append(said, fmt.Sprintf("%q → %s", carrier, key))
		}
		sort.Strings(said)
		findings = append(findings, fmt.Sprintf(
			"%s: семья носителей %s объявляет РАЗНУЮ судьбу одного предмета: %s. "+
				"У производной записи (инъекция, годок) своего предмета нет — её предмет есть "+
				"предмет семьи, поэтому разойтись они могут только ложью",
			gateCarrierLedgerName, fam, strings.Join(said, "; ")))
	}
	return findings
}

// knownSubjectFates — словарь судеб в виде текста для сообщения находки.
func knownSubjectFates() string {
	keys := make([]string, 0, len(subjectFates))
	for k := range subjectFates {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for i, k := range keys {
		keys[i] = fmt.Sprintf("%q — %s", k, subjectFates[k])
	}
	return strings.Join(keys, "; ")
}

// describe — координата одной строкой для текста находки.
func (c *CarriedCoordinate) describe() string {
	if c == nil {
		return "(координаты нет)"
	}
	repo, rel := strings.TrimSpace(c.Repo), strings.TrimSpace(c.Path)
	switch {
	case repo == "" && rel == "":
		return "(координата пуста)"
	case repo == "":
		return rel
	case rel == "":
		return repo
	}
	return repo + ":" + rel
}

// carriedSubjectRows — вход гейта: записи надгробия с ОТКАЗОМ на пустом обходе.
//
// Три исхода разведены намеренно. Надгробия нет либо строк в нём ноль — это
// «не выполнилось», а не чистое дерево: судить было нечего, и «ноль находок» на
// непрочитанном входе неотличимо от связной ведомости.
//
// Граница названа прямо: в дереве, из которого носителей не снимали, надгробия
// нет законно — и тогда у этого гейта нет предмета, поэтому он снимается вместе
// с ведомостью ОДНИМ изменением, а не терпит пустоту. Соседний гейт
// (gatecarrierremoval.go) на пустом надгробии обязан молчать, и это не
// расхождение: его предмет — молчаливое снятие носителя, а не связность записи.
func carriedSubjectRows(root string) ([]GateCarrierRetirement, error) {
	l, err := readGateCarrierLedger(root)
	if err != nil {
		return nil, err
	}
	if l == nil {
		return nil, fmt.Errorf(
			"%s в корпусе %s не найдено — читать было нечего; это отказ, а не пустой успех: "+
				"проверь, не переехала ли ведомость",
			gateCarrierLedgerName, gateCorpusDir)
	}
	if len(l.Retired) == 0 {
		return nil, fmt.Errorf(
			"%s не дало ни одной записи — судить было нечего; это отказ, а не пустой успех",
			gateCarrierLedgerName)
	}
	return l.Retired, nil
}
