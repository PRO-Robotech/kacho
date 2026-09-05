// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// subscriptionnonowners_test.go — гейт «домен, не служащий глагол подписки, несёт
// РЕШЕНИЕ ОБ ЭТОМ В ДЕРЕВЕ».
//
// # Предмет — решение, живущее только в ленте задачи
//
// Глагол подписки служат пять доменов из семи. Разница закрыта решениями, но пока
// эти решения живут в комментариях задач, следующий читатель видит «5 из 7» и
// читает его как незакрытую работу. Ровно это и произошло: предикат задачи #1023
// требовал семи и оставался красным при верном дереве — то есть был не проверяем
// НИ ПРИ КАКОМ дереве, потому что двух владельцев не будет никогда.
//
// Отличие «решено» от «не сделано» снаружи неразличимо by construction: и то и
// другое выглядит как отсутствие провязки. Различает только запись — и она обязана
// быть в дереве, а не в переписке.
//
// # Почему счёт владельцев БЕРЁТСЯ, а не заводится заново
//
// Кто служит глагол, уже считает [subscriptionOwners] — разбором вызова
// регистратора, а не подстрокой. Второй счётчик того же предмета разошёлся бы с
// первым молча, и разошёлся бы там, где расхождение не ищут. Здесь берётся ТА ЖЕ
// функция.
//
// # Что требуется от записи — ЧЕТЫРЕ вещи, и каждая закрывает свой исход
//
//  1. ПРИЧИНА. Пустая причина объявлением не является: «решили не делать» без
//     основания следующий снимет как непонятное.
//  2. НОМЕР ЗАДАЧИ. Обоснование объясняет, почему так СЕЙЧАС, и молчит о том, кто
//     это пересмотрит. Отсрочка без ответственного — ban #11 через заднюю дверь.
//     Тот же довод и та же форма, что у [deliveryColumnsWithoutAdvancer].
//  3. ДОКУМЕНТ. Запись в коде — не запись в дереве: её прочтёт тот, кто и так
//     правит гейт. Решение обязано лежать прозой там, где его ищет ЧЕЛОВЕК, и
//     документ обязан называть адрес ручки — иначе он про что-то другое.
//  4. ПРЕДПОСЫЛКА, где она машинная. Запись geo опирается на то, что у домена НЕТ
//     собственных типов в модели прав; появление типа предпосылку убивает, и гейт
//     обязан это заметить, а не пережить.
//
// # Как запись истекает — ТРЕМЯ способами, и все три роняют гейт
//
//  1. ПО УСПЕХУ: домен стал владельцем — записи больше нечего исключать;
//  2. ПО ИСЧЕЗНОВЕНИЮ ПРЕДМЕТА: домена нет в дереве;
//  3. ПО СМЕРТИ ПРЕДПОСЫЛКИ: у домена появился свой тип в модели прав, и «сужать
//     нечем» перестало быть правдой.
//
// Четвёртого исхода нет: домен, не попавший ни в владельцы, ни в ведомость, —
// находка.
//
// # Пустой обход — отказ
//
// Ноль сервисов, ноль прочитанных типов модели: «ноль находок» стало бы
// неотличимо от «ноль прочитанного».

// subscriptionNonOwner — домен, у которого поверхности подписки нет ПО РЕШЕНИЮ.
type subscriptionNonOwner struct {
	// Domain — имя каталога сервиса (`services/<Domain>`).
	Domain string
	// Because — причина и предикат пересмотра. Пустая — находка.
	Because string
	// Issue — задача, где решение разобрано. Ноль — находка.
	Issue int
	// Record — документ дерева, где решение записано ПРОЗОЙ. Обязан
	// существовать и называть адрес ручки.
	Record string
	// NoRightsModelTypes — запись опирается на отсутствие у домена собственных
	// типов в модели прав. Проверяется; появление типа роняет гейт.
	NoRightsModelTypes bool
}

// subscriptionNonOwners — ведомость решений.
//
// Прозой решения записаны в документах, названных полем `Record`; здесь стоит
// ровно то, что машина умеет проверить.
var subscriptionNonOwners = []subscriptionNonOwner{
	{
		Domain: "geo",
		Issue:  1023,
		Record: "services/geo/docs/engineering/architecture/known-divergences.md",
		// Предпосылка МАШИННАЯ: появится тип `geo_*` — запись обязана быть
		// пересмотрена, а не пережить свой довод.
		NoRightsModelTypes: true,
		Because: "виды ленты geo — Region и Zone, а типов geo_* в модели прав ноль: " +
			"каталог размещения намеренно вынесен из пообъектной авторизации, и вопрос " +
			"о видимости строки имеет один ответ для всех. Словарь видов без типа объекта " +
			"общая форма отвергает, поэтому владелец не поднялся бы вовсе",
	},
	{
		Domain: "iam",
		Issue:  1397,
		Record: "services/iam/docs/engineering/architecture/known-divergences.md",
		// Предпосылка НЕ машинная намеренно: у iam шесть собственных типов модели
		// прав, и их число меняется от чужих работ. Пиннить его значило бы завести
		// тревогу, звонящую на посторонней правке, — такую отключают первой.
		Because: "у iam нет ленты изменений ресурсов: четыре его очереди служат самой " +
			"платформе (кортежи прав, реконсиляция зеркала, смена субъекта, аудит). " +
			"У журнала смены субъекта построчное сужение fail-open — оно снимает эффект, " +
			"ради которого журнал заведён. Развилка «чем читать» разобрана в #1397 и РЕШЕНА " +
			"2026-08-29: чтение курсором через унарный PollSubjectChanges; запись решения — " +
			"docs/architecture/subject-change-journal-is-not-a-resource-stream.md, там же " +
			"внешний предикат пересмотра",
	},
}

// subscriptionRightsModelRel — ИСПОЛНЯЕМАЯ копия модели прав.
//
// Читается она, а не каноническая копия контракта: решение о видимости строки
// считается по той модели, которая встроена в службу. Байт-идентичность двух
// копий держит своя цель (`make -C gateway permission-catalog-check` и проба
// тождества в iam), поэтому здесь она не переспрашивается.
//
// Величина повторена — соседний гейт называет тот же путь своей константой, но
// живёт во ВНЕШНЕМ пакете проб и отсюда не виден. Расхождение здесь безопасно by
// construction: разойдясь, путь перестанет читаться, и гейт скажет об этом
// отказом, а не молчанием.
const subscriptionRightsModelRel = "services/iam/internal/authzmodel/fga_model.fga"

// rightsModelTypesByDomain — сколько типов модели прав объявляет каждый домен.
//
// Читается ИСПОЛНЯЕМАЯ копия модели ([subscriptionRightsModelRel]), а не каноническая:
// решение о видимости строки считается по той, которая встроена в службу. Обе
// держит байт-идентичными своя цель.
func rightsModelTypesByDomain(root string) (byDomain map[string]int, total int, err error) {
	path := filepath.Join(root, subscriptionRightsModelRel)
	f, err := os.Open(path) // #nosec G304 -- обход собственного дерева
	if err != nil {
		return nil, 0, fmt.Errorf("модель прав %s: %w", subscriptionRightsModelRel, err)
	}
	defer func() { _ = f.Close() }()

	byDomain = map[string]int{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		// Объявление типа стоит с начала строки; внутри блока оно с отступом,
		// а в комментариях — после `#`. Судится объявление, а не упоминание.
		if !strings.HasPrefix(line, "type ") {
			continue
		}
		name := strings.TrimSpace(strings.TrimPrefix(line, "type "))
		if name == "" {
			continue
		}
		total++
		if idx := strings.Index(name, "_"); idx > 0 {
			byDomain[name[:idx]]++
		}
	}
	if scErr := sc.Err(); scErr != nil {
		return nil, 0, fmt.Errorf("чтение модели прав: %w", scErr)
	}
	return byDomain, total, nil
}

// subscriptionOwnershipFindings — суждение о доменах и ведомости.
//
// Вынесено отдельной функцией, чтобы доказательство способности гейта упасть
// прогоняло ЕЁ, а не свою копию.
func subscriptionOwnershipFindings(
	services, owners []string,
	ledger []subscriptionNonOwner,
	modelTypes map[string]int,
	recordNames func(path string) (bool, error),
) []string {
	isOwner := make(map[string]bool, len(owners))
	for _, o := range owners {
		isOwner[o] = true
	}
	inService := make(map[string]bool, len(services))
	for _, s := range services {
		inService[s] = true
	}
	recorded := make(map[string]subscriptionNonOwner, len(ledger))

	out := make([]string, 0, 4)
	for _, e := range ledger {
		recorded[e.Domain] = e

		if !inService[e.Domain] {
			out = append(out, fmt.Sprintf(
				"ведомость называет домен %q, которого под services/ нет — записи больше "+
					"нечего исключать, снимите её", e.Domain))
			continue
		}
		if isOwner[e.Domain] {
			out = append(out, fmt.Sprintf(
				"домен %q СЛУЖИТ глагол подписки, а ведомость объявляет его исключённым — "+
					"запись пережила свой предмет и разрешит следующему не провязывать "+
					"домен, который уже провязан", e.Domain))
			continue
		}
		if strings.TrimSpace(e.Because) == "" {
			out = append(out, fmt.Sprintf(
				"запись о домене %q без причины — «решили не делать» без основания "+
					"следующий снимет как непонятное", e.Domain))
		}
		if e.Issue == 0 {
			out = append(out, fmt.Sprintf(
				"запись о домене %q не называет задачи — обоснование объясняет, почему так "+
					"сейчас, и молчит о том, кто это пересмотрит", e.Domain))
		}
		if e.NoRightsModelTypes && modelTypes[e.Domain] > 0 {
			out = append(out, fmt.Sprintf(
				"запись о домене %q опирается на отсутствие у него типов модели прав, а их "+
					"стало %d — предпосылка умерла: сужать теперь ЕСТЬ чем, и решение "+
					"надлежит пересмотреть, а не пережить", e.Domain, modelTypes[e.Domain]))
		}
		if e.Record == "" {
			out = append(out, fmt.Sprintf(
				"запись о домене %q не называет документа — решение в коде ведомости "+
					"прочтёт лишь тот, кто и так правит гейт", e.Domain))
			continue
		}
		names, err := recordNames(e.Record)
		if err != nil {
			out = append(out, fmt.Sprintf(
				"документ решения %s (домен %q) не читается: %v — решения в дереве нет",
				e.Record, e.Domain, err))
			continue
		}
		if !names {
			out = append(out, fmt.Sprintf(
				"документ решения %s (домен %q) не называет адрес ручки %q — он про "+
					"что-то другое, и решением о подписке не является",
				e.Record, e.Domain, subscriptionHandlePath))
		}
	}

	for _, svc := range services {
		if isOwner[svc] || recorded[svc].Domain != "" {
			continue
		}
		out = append(out, fmt.Sprintf(
			"домен %q не служит глагол подписки, и решения об этом в дереве нет. Снаружи "+
				"«решено не заводить» и «ещё не сделано» неразличимы: и то и другое выглядит "+
				"как отсутствие провязки. Либо провяжите, либо запишите решение — третьего "+
				"исхода нет", svc))
	}
	sort.Strings(out)
	return out
}

// TestEveryDomainEitherServesSubscriptionOrRecordsWhyNot — гейт.
func TestEveryDomainEitherServesSubscriptionOrRecordsWhyNot(t *testing.T) {
	root := repoRoot(t)
	list := subscriptionDocsLister(treecorpus.UnderWithSuffix)

	owners, filesRead, unparsed, err := subscriptionOwners(root, list)
	if err != nil {
		t.Fatalf("состав исходников сервисов у корпуса дерева: %v", err)
	}
	for _, path := range unparsed {
		t.Errorf("исходник %s не разбирается — гейт судит по узлам дерева, и неосмотренный "+
			"файл его молчания не оправдывает", path)
	}

	var services []string
	for _, dir := range serviceDirs(t, root) {
		services = append(services, strings.TrimPrefix(dir, "services/"))
	}

	modelTypes, totalTypes, err := rightsModelTypesByDomain(root)
	if err != nil {
		t.Fatalf("%v — предпосылку записей проверять нечем", err)
	}

	recordNames := func(rel string) (bool, error) {
		body, rerr := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 -- из ведомости этого дерева
		if rerr != nil {
			return false, rerr
		}
		return strings.Contains(string(body), subscriptionHandlePath), nil
	}

	perDomain := make([]string, 0, len(services))
	for _, s := range services {
		perDomain = append(perDomain, fmt.Sprintf("%s:%d", s, modelTypes[s]))
	}
	t.Logf("перепись: исходников сервисов осмотрено %d · доменов %d %v · владельцев глагола %d %v · "+
		"записей ведомости %d · типов модели прав %d, по доменам %v",
		filesRead, len(services), services, len(owners), owners,
		len(subscriptionNonOwners), totalTypes, perDomain)

	if filesRead == 0 {
		t.Fatal("не осмотрено ни одного исходника сервисов — прочитано ноль, и зелёное " +
			"здесь неотличимо от пустого обхода")
	}
	if totalTypes == 0 {
		t.Fatal("в модели прав не прочитано ни одного типа — предпосылка записей не " +
			"проверяется ничем, и её молчание утверждением не является")
	}

	for _, finding := range subscriptionOwnershipFindings(
		services, owners, subscriptionNonOwners, modelTypes, recordNames,
	) {
		t.Error(finding)
	}
}
