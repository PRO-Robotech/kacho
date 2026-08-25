// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// declaredcardinality_test.go — гейт против объявленного предела без исполнителя.
//
// Предмет. Предел кратности повторяемого поля обязан иметь ИСПОЛНИТЕЛЯ:
// объявление, за которым никто не стоит, поле не ограничивает и при этом
// выглядит гарантией — вызывающий читает `<=8` и шлёт восемь, а сервис примет
// восемьсот.
//
// # ОТКУДА ГЕЙТ БЕРЁТ ОБЪЯВЛЕННЫЙ ПРЕДЕЛ (переоснован, задача #1255)
//
// Прежде предел читался из САМОГО контракта — расширения `(size) = "<=N"`
// семейства `kacho.cloud.validation`. Семейство снято с контрактов целиком:
// исполнителя на пути запроса у него не было ни одного, грамматика его значений
// нигде не определена, а на полях имени объявленный образец отвергал законный
// вход. Файл, объявлявший расширения, удалён.
//
// Свойство, которое гейт держал, при этом НЕ снято — снят лишь его источник.
// Величина переехала в поимённую таблицу `declaredCardinality` НИЖЕ, в этом же
// файле; форма уже была в нём же (`labelsMapLimit` с отдельной пробой
// предпосылки). Сверка «объявлено ↔ исполнитель» осталась дословно той же,
// исчезла только сверка с местом, которого больше нет.
//
// Вторым адресом та же величина переехала в ПРОЗАИЧЕСКИЙ комментарий поля — там
// её видит вызывающий, чего частное расширение с частными номерами не давало
// никогда: ни один существующий генератор клиента о нём не знал и знать не мог.
// Что комментарий величину называет, проверяет `TestDeclaredCardinalityLimitIsAlsoStatedInProse`.
//
// ПОЧЕМУ ТАБЛИЦА, А НЕ «ПРОСТО СНЯТЬ ГЕЙТ». Кратность повторяемого поля
// умножает работу за пределами процесса, и это не абстракция: список
// интерфейсов машины стоит ОДНОГО обращения к соседу на элемент, а сосед каждое
// такое обращение ещё и авторизует у себя.
//
// Это особенно дорого там, где кратность повторяемого поля умножает работу за
// пределами процесса: список интерфейсов машины стоит ОДНОГО обращения к соседу на
// элемент, а сосед каждое такое обращение ещё и авторизует у себя.
//
// Чем гейт НЕ является. Он не делает читателя из всего семейства расширений — их в
// дереве больше тысячи, они принадлежат девяти доменам, и общий проверяльщик менял
// бы поведение всех сразу. Гейт закрывает ОДНО измерение (кратность повторяемых
// полей) в ОДНОЙ зоне (машины и блочное хранение) и ЯВНО объявляет свою область:
// см. scannedPackages и утверждение об объёме осмотренного в логе. Остальные домены
// — открытый остаток, а не молчание.
//
// Освобождения. Поле, которого не читает ни одна строка прод-кода, работы не
// гейтит — гейтить там нечего. Но такое освобождение обязано ЖИТЬ, пока живёт его
// предпосылка: у каждой записи есть проверка «читателя по-прежнему нет», и
// появление читателя красит гейт. Иначе список освобождений переживёт свой предмет
// и станет описанием вчерашнего дерева.
package repohygiene

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// scannedPackages — область гейта: домены, за пределы которых он не судит.
//
// Обходом каталогов это больше не является (обходить нечего — объявлений в
// контрактах нет), но ОБЛАСТЬ по-прежнему обязана быть названа: перечень ниже
// печатается в переписи, чтобы «находок ноль» было отличимо от «эта зона вне
// охвата». Остальные домены — открытый остаток, а не молчание.
var scannedPackages = []string{
	"compute/v1",
	"storage/v1",
}

// declaredCardinality — ОБЪЯВЛЕННЫЙ предел кратности: «<сообщение>.<поле>» → предел.
//
// Это то самое место, куда величина переехала с контракта. Единица записи —
// ПАРА «сообщение, поле», а не одно поле: имя `labels` живёт в четырнадцати
// сообщениях, и ключ без сообщения свёл бы их в одну запись, потеряв различие
// пределов, если оно когда-нибудь появится.
//
// Запись здесь — ОБЪЯВЛЕНИЕ. Исполнителя ей даёт либо `cardinalityEnforcer`
// (поимённая константа), либо общий исполнитель меток (`labelsMapLimit`), и
// расхождение между объявлением и исполнителем краснит гейт по имени поля.
// Запись без исполнителя — тоже находка: ровно тот дефект, ради которого гейт
// заведён, только теперь оба конца лежат в дереве, а не один из них в контракте.
var declaredCardinality = map[string]string{
	// Машины.
	"CreateInstanceRequest.guest_access_key_ids":   "<=32",
	"UpdateInstanceRequest.guest_access_key_ids":   "<=32",
	"CreateInstanceRequest.secondary_volume_specs": "<=8",
	"CreateInstanceRequest.labels":                 "<=64",
	"UpdateInstanceRequest.labels":                 "<=64",
	// Блочное хранение.
	"CreateVolumeRequest.labels":   "<=64",
	"UpdateVolumeRequest.labels":   "<=64",
	"CreateSnapshotRequest.labels": "<=64",
	"UpdateSnapshotRequest.labels": "<=64",
	"CopySnapshotRequest.labels":   "<=64",
	"CreateImageRequest.labels":    "<=64",
	"UpdateImageRequest.labels":    "<=64",
	"CopyImageRequest.labels":      "<=64",
	"RegisterImageRequest.labels":  "<=64",
}

// prosePlacement — где та же величина названа ПРОЗОЙ, для вызывающего.
//
// Ключ тот же, что у `declaredCardinality`; значение — файл контракта и имя
// поля, чей комментарий обязан назвать число. Без этой связи «величина переехала
// в комментарий» осталось бы обещанием: комментарий никем не читается и потому
// стареет молча.
var prosePlacement = map[string]string{
	"CreateInstanceRequest.guest_access_key_ids":   "proto/kacho/cloud/compute/v1/instance_service.proto",
	"UpdateInstanceRequest.guest_access_key_ids":   "proto/kacho/cloud/compute/v1/instance_service.proto",
	"CreateInstanceRequest.secondary_volume_specs": "proto/kacho/cloud/compute/v1/instance_service.proto",
	"CreateInstanceRequest.labels":                 "proto/kacho/cloud/compute/v1/instance_service.proto",
	"UpdateInstanceRequest.labels":                 "proto/kacho/cloud/compute/v1/instance_service.proto",
	"CreateVolumeRequest.labels":                   "proto/kacho/cloud/storage/v1/volume_service.proto",
	"UpdateVolumeRequest.labels":                   "proto/kacho/cloud/storage/v1/volume_service.proto",
	"CreateSnapshotRequest.labels":                 "proto/kacho/cloud/storage/v1/snapshot_service.proto",
	"UpdateSnapshotRequest.labels":                 "proto/kacho/cloud/storage/v1/snapshot_service.proto",
	"CopySnapshotRequest.labels":                   "proto/kacho/cloud/storage/v1/snapshot_service.proto",
	"CreateImageRequest.labels":                    "proto/kacho/cloud/storage/v1/image_service.proto",
	"UpdateImageRequest.labels":                    "proto/kacho/cloud/storage/v1/image_service.proto",
	"CopyImageRequest.labels":                      "proto/kacho/cloud/storage/v1/image_service.proto",
	"RegisterImageRequest.labels":                  "proto/kacho/cloud/storage/v1/internal_image_service.proto",
}

// enforcer — где живёт исполнитель объявленного предела и как он называется.
//
// Запись обязана называть КОНСТАНТУ, а не «проверяется где-то»: гейт читает её
// значение и сверяет с объявленным. Разъехавшись, объявление и проверка дают ровно
// тот дефект, ради которого гейт заведён, — только теперь с двумя источниками
// истины вместо одного.
type enforcer struct {
	file  string
	ident string
}

// cardinalityEnforcer — ключ «<сообщение>.<поле>» → исполнитель.
var cardinalityEnforcer = map[string]enforcer{
	"CreateInstanceRequest.secondary_volume_specs": {
		file:  "services/compute/internal/domain/constants.go",
		ident: "MaxSecondaryVolumeSpecsPerInstance",
	},
	// Ключи входа: один исполнитель на оба запроса. Предел проверяется общей
	// функцией, которую зовут и создание, и правка, — поэтому запись здесь одна
	// по существу и две по ключу.
	"CreateInstanceRequest.guest_access_key_ids": {
		file:  "services/compute/internal/domain/constants.go",
		ident: "MaxGuestAccessKeysPerInstance",
	},
	"UpdateInstanceRequest.guest_access_key_ids": {
		file:  "services/compute/internal/domain/constants.go",
		ident: "MaxGuestAccessKeysPerInstance",
	},
}

// unreadFields — освобождения: поле не читает ни одна строка прод-кода, поэтому
// предел ничего не гейтит. Каждая запись несёт ПРОВЕРЯЕМУЮ предпосылку: маркер,
// присутствие которого в прод-дереве означает, что читатель появился и
// освобождение истекло.
var unreadFields = map[string]exemption{
	// Записи про конфигурацию приложения и про перемещение машины СНЯТЫ вместе со
	// своим предметом: сообщения `ContainerSolutionSpec`, `BackupSpec` и
	// `RelocateInstanceRequest` больше не объявлены — они остались от снятых полей
	// и снятого RPC и не были достижимы ни одним путём. Гейт объявил их
	// просроченными сам, в том же прогоне, — послабление истекло от факта.
}

type exemption struct {
	why string
	// reader — фрагмент, появление которого в прод-дереве compute означает, что
	// читатель поля появился и освобождение больше не действует.
	//
	// Маркер обязан быть ПРОИЗВОДИМЫМ в этом дереве: см.
	// TestExemptionRetirementMarkersHaveAProducer. Маркер, которого не эмитит ни
	// одна строка сгенерированных стабов домена, не может появиться никогда —
	// значит освобождение не истекает никогда, оставаясь на вид самоистекающим.
	reader string
}

// computeStubDir — сгенерированные стабы домена, чьи поля освобождает ведомость.
//
// Это ЕДИНСТВЕННОЕ место, где может родиться геттер поля или тип метаданных
// операции: прод-код их не объявляет, он их зовёт. Поэтому производителя маркера
// ищем здесь, а не в прод-дереве, где ищется его ПОТРЕБИТЕЛЬ.
const computeStubDir = "pkg/api/kacho/cloud/compute/v1"

// exemptionMarkerCorpus — тела сгенерированных стабов домена одной строкой.
func exemptionMarkerCorpus(root string) (string, int, error) {
	dir := filepath.Join(root, computeStubDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", 0, err
	}
	var sb strings.Builder
	files := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		body, rerr := os.ReadFile(filepath.Join(dir, e.Name()))
		if rerr != nil {
			return "", 0, rerr
		}
		sb.Write(body)
		sb.WriteByte('\n')
		files++
	}
	return sb.String(), files, nil
}

// auditExemptionMarkers — записи ведомости, чей маркер снятия НЕПРОИЗВОДИМ.
//
// Ключ возвращается вместе с маркером: находка обязана назвать и запись, и то,
// чего в дереве нет, — иначе читатель пойдёт искать не то.
func auditExemptionMarkers(corpus string, roster map[string]exemption) []string {
	var dead []string
	for key, ex := range roster {
		if !strings.Contains(corpus, ex.reader) {
			dead = append(dead, key+" → "+ex.reader)
		}
	}
	sort.Strings(dead)
	return dead
}

// TestExemptionRetirementMarkersHaveAProducer — у маркера снятия КАЖДОГО
// освобождения есть производитель в дереве.
//
// # Предмет
//
// Освобождение живёт, пока живёт его предпосылка, и предпосылку эту проверяет
// маркер: появился в прод-дереве — читатель есть, освобождение истекло. Такой
// предикат молчалив по построению — он говорит только тогда, когда что-то
// нашёл, — поэтому маркер, которого НЕ МОЖЕТ БЫТЬ в дереве, неотличим от маркера,
// которого пока нет. Освобождение с таким маркером самоистекающее только на вид:
// оно не истечёт никогда, что бы с полем ни сделали.
//
// # Что проверяется
//
// Что маркер вообще ПРОИЗВОДИМ: встречается хотя бы раз в сгенерированных стабах
// домена — единственном месте, где рождаются геттеры полей и типы метаданных
// операций. Потребителя маркера ищет сам гейт (hasProductionReader); здесь
// проверяется, что у входа этого гейта есть производитель.
//
// # Что делать, если сработал, — два исхода, третьего нет
//
//  1. маркер назван неверно (геттер другого поля, метод другого сообщения,
//     имя, снятое с контракта) → назвать тот идентификатор, появление которого
//     ДЕЙСТВИТЕЛЬНО означает читателя освобождённого поля;
//  2. поля больше нет в контракте → снять запись целиком, вместе с
//     объявленным пределом.
//
// Правка маркера «чтобы позеленело» исходом не является: маркер — предикат
// снятия, а не украшение записи.
func TestExemptionRetirementMarkersHaveAProducer(t *testing.T) {
	root := repoRoot(t)
	corpus, files, err := exemptionMarkerCorpus(root)
	if err != nil {
		t.Fatalf("стабы домена (%s) не читаются: %v — предпосылка проверки сломана, "+
			"её молчание ничего не доказывает", computeStubDir, err)
	}
	if files == 0 || len(corpus) == 0 {
		t.Fatalf("в %s не прочитано ни одного файла — «все маркеры производимы» здесь означало бы "+
			"«ничего не читал»", computeStubDir)
	}
	// ПУСТАЯ ВЕДОМОСТЬ — ЗАКОННОЕ И ЛУЧШЕЕ СОСТОЯНИЕ, а не поломка.
	//
	// Здесь стоял отказ с доводом «пустая ведомость молчит по той же причине, по
	// какой молчит исправная». Довод верен для гейта, чьё молчание — единственное
	// свидетельство; здесь это не так: способность судьи падать доказывает
	// отдельная проба на синтетике (TestExemptionMarkerProducerJudgeCutsBothWays),
	// а объём осмотренного эта печатает переписью ниже.
	//
	// Опустевшая ведомость означает, что послаблений в дереве не осталось, — то
	// есть ровно ту цель, ради которой ведомость и заведена. Ронять прогон на
	// достижении цели значит подталкивать к тому, чтобы держать в ней запись ради
	// зелёного. Случилось в день, когда с машины сняли мёртвую поверхность и
	// последние три записи истекли сами.
	if len(unreadFields) == 0 {
		t.Logf("перепись: файлов стабов %s прочитано %d; записей ведомости 0 — "+
			"послаблений в дереве нет, проверять производимость нечего", computeStubDir, files)
		return
	}

	dead := auditExemptionMarkers(corpus, unreadFields)
	t.Logf("перепись: файлов стабов %s прочитано %d (%d байт); записей ведомости %d; "+
		"маркеров без производителя %d",
		computeStubDir, files, len(corpus), len(unreadFields), len(dead))

	for _, d := range dead {
		t.Errorf("предикат снятия освобождения непроизводим: %s\n"+
			"Ни одна строка сгенерированных стабов домена этот идентификатор не эмитит, значит "+
			"появиться в прод-дереве он не может НИКОГДА — освобождение самоистекающее только на "+
			"вид. Исходы: назвать идентификатор, появление которого действительно означает "+
			"читателя освобождённого поля, либо снять запись вместе с объявленным пределом.", d)
	}
}

// TestExemptionMarkerProducerJudgeCutsBothWays — инъекция в обе стороны на том же
// корпусе, что читает гейт.
//
// Без стороны «молчит» проверка ловила бы форму («маркер вообще есть в записи»),
// а не существо; без стороны «краснеет» она была бы объявлением.
func TestExemptionMarkerProducerJudgeCutsBothWays(t *testing.T) {
	root := repoRoot(t)
	corpus, files, err := exemptionMarkerCorpus(root)
	if err != nil || files == 0 {
		t.Fatalf("корпус стабов не собран (%v, файлов %d) — инъекции не на чем исполниться", err, files)
	}

	// Сторона дефекта: маркер, которого стабы не эмитят.
	dead := auditExemptionMarkers(corpus, map[string]exemption{
		"SyntheticSpec.field": {why: "проба", reader: "GetNoSuchFieldEverGenerated("},
	})
	if len(dead) != 1 || !strings.Contains(dead[0], "SyntheticSpec.field") {
		t.Fatalf("непроизводимый маркер не пойман: %v", dead)
	}

	// Законный близнец той же формы: геттер, который стабы ТОЧНО эмитят.
	//
	// Здесь стоял геттер освобождённого поля (`GetSecrets(` у ContainerSolutionSpec).
	// Он умер вместе со своим сообщением, когда с машины сняли мёртвую поверхность,
	// и проба покраснела не на дефекте, а на исчезновении собственной фикстуры.
	// Близнец обязан быть привязан к тому, что живёт независимо от послаблений, —
	// иначе он истекает вместе с ними и уносит с собой доказательство, что судья
	// не срабатывает вхолостую.
	alive := auditExemptionMarkers(corpus, map[string]exemption{
		"Instance.zone_id": {why: "проба", reader: "GetZoneId("},
	})
	if len(alive) != 0 {
		t.Fatalf("производимый маркер объявлен непроизводимым: %v — проверка ловит форму, "+
			"а не существо, и будет отключена первым ложным срабатыванием", alive)
	}
}

// labelsMapLimit — объявление `(kacho.cloud.size) = "<=64"` на картах меток.
// Исполнитель у него общий и уже существует (pkg/validate.Labels), поэтому в
// поимённой таблице он не нужен; предпосылка проверяется отдельно
// (TestLabelsLimitPremiseHolds).
const labelsMapLimit = 64

// TestDeclaredCardinalityLimitsHaveAnEnforcer — сам гейт.
func TestDeclaredCardinalityLimitsHaveAnEnforcer(t *testing.T) {
	root := repoRoot(t)

	type decl struct{ key, bound string }
	decls := make([]decl, 0, len(declaredCardinality))
	for key, bound := range declaredCardinality {
		decls = append(decls, decl{key: key, bound: bound})
	}
	sort.Slice(decls, func(i, j int) bool { return decls[i].key < decls[j].key })

	t.Logf("объявленных пределов кратности: %d (области: %s); освобождений (поле без читателя): %d; "+
		"мест, где та же величина названа прозой: %d",
		len(decls), strings.Join(scannedPackages, ", "), len(unreadFields), len(prosePlacement))

	if len(decls) == 0 {
		t.Fatal("таблица объявленных пределов ПУСТА — гейту нечего сверять, и его молчание " +
			"ничего не доказывает. Либо пределы кратности из продукта исчезли (тогда снимите " +
			"гейт вместе с предметом), либо таблицу опустошили, чтобы позеленело")
	}

	var problems []string
	seen := make(map[string]bool, len(decls))
	for _, d := range decls {
		seen[d.key] = true

		// Карты меток — общий исполнитель (предпосылка проверяется отдельно).
		if strings.HasSuffix(d.key, ".labels") {
			if d.bound != "<="+strconv.Itoa(labelsMapLimit) {
				problems = append(problems, d.key+": объявлен предел "+d.bound+
					", а общий исполнитель меток знает "+strconv.Itoa(labelsMapLimit))
			}
			continue
		}
		if ex, ok := unreadFields[d.key]; ok {
			if strings.TrimSpace(ex.why) == "" {
				problems = append(problems, d.key+": освобождение без обоснования")
			}
			if hasProductionReader(t, root, ex.reader) {
				problems = append(problems, d.key+": освобождение истекло — читатель появился ("+
					ex.reader+" встречается в прод-дереве). Поле теперь что-то гейтит: заведи "+
					"константу, проверку в запросе и запись в cardinalityEnforcer")
			}
			continue
		}

		enf, ok := cardinalityEnforcer[d.key]
		if !ok {
			problems = append(problems, d.key+
				": предел объявлен, исполнителя нет. Расширения проверок из объявлений не читает ни одна "+
				"строка прод-кода, поэтому объявление само по себе ничего не ограничивает — "+
				"заведи константу и проверку в запросе, затем запись в cardinalityEnforcer "+
				"(либо, если поля не читает никто, — запись в unreadFields с проверяемой предпосылкой)")
			continue
		}
		got, gerr := goConstValue(root, enf.file, enf.ident)
		if gerr != nil {
			problems = append(problems, d.key+": исполнитель "+enf.file+"."+enf.ident+" не читается: "+gerr.Error())
			continue
		}
		if want := "<=" + strconv.Itoa(got); want != d.bound {
			problems = append(problems, d.key+": объявлено "+d.bound+", исполнитель "+enf.ident+
				" знает "+strconv.Itoa(got)+" — объявление и проверка разъехались")
		}
	}

	// Запись, которой больше нечего защищать, — находка.
	for key := range cardinalityEnforcer {
		if !seen[key] {
			problems = append(problems, key+": запись об исполнителе есть, а объявленного предела больше нет — удали запись")
		}
	}
	for key := range unreadFields {
		if !seen[key] {
			problems = append(problems, key+": освобождение есть, а объявленного предела больше нет — удали запись")
		}
	}

	if len(problems) > 0 {
		sort.Strings(problems)
		t.Errorf("объявленных пределов кратности без работающего исполнителя: %d\n  %s",
			len(problems), strings.Join(problems, "\n  "))
	}
}

// TestEnforcedCardinalityLimitsAreDeclared — обратная сторона: проверка, о которой
// контракт молчит, тоже расхождение. Вызывающий узнаёт о пределе отказом, а не из
// объявления, и не может рассчитать запрос заранее.
//
// Пока предел не объявлен в контракте, запись живёт здесь — с причиной, а не молча.
func TestEnforcedCardinalityLimitsAreDeclared(t *testing.T) {
	root := repoRoot(t)

	// Пределы, которые прод-код ПРОВЕРЯЕТ, но контракт пока не объявляет.
	undeclared := map[string]string{
		"CreateInstanceRequest.network_interface_specs": "" +
			"предел кратности проверяется (domain.MaxNetworkInterfaceSpecsPerInstance), но в объявление " +
			"поля не внесён: правка объявления требует перегенерации кода из объявлений, что задевает " +
			"весь домен и делается отдельно. Запись здесь — чтобы расхождение было видно, а не молчало.",
	}
	for key, why := range undeclared {
		if strings.TrimSpace(why) == "" {
			t.Errorf("%s: запись без причины", key)
		}
	}
	// Предпосылка: исполнитель на месте. Исчезнет — запись потеряет предмет.
	if _, err := goConstValue(root, "services/compute/internal/domain/constants.go",
		"MaxNetworkInterfaceSpecsPerInstance"); err != nil {
		t.Fatalf("исполнитель предела кратности интерфейсов не читается (%v) — запись потеряла предмет", err)
	}
	t.Logf("проверяемых, но не объявленных пределов: %d", len(undeclared))
}

// TestLabelsLimitPremiseHolds — предпосылка освобождения карт меток: общий
// исполнитель существует и знает то же число. Без этой проверки строка «у меток
// исполнитель общий» была бы утверждением без предмета — ровно тем, что ловит гейт.
func TestLabelsLimitPremiseHolds(t *testing.T) {
	root := repoRoot(t)
	body, err := os.ReadFile(filepath.Join(root, "pkg/validate/validate.go"))
	if err != nil {
		t.Fatalf("pkg/validate/validate.go не читается: %v", err)
	}
	if !strings.Contains(string(body), "func Labels(") {
		t.Fatal("общего исполнителя меток (pkg/validate.Labels) больше нет — освобождение карт меток " +
			"в гейте потеряло предмет")
	}
	if !strings.Contains(string(body), strconv.Itoa(labelsMapLimit)) {
		t.Fatalf("общий исполнитель меток не содержит числа %d — объявление и проверка могли разъехаться",
			labelsMapLimit)
	}
}

// hasProductionReader — встречается ли маркер в прод-дереве compute (без тестов и
// без сгенерированного кода).
func hasProductionReader(t *testing.T, root, marker string) bool {
	t.Helper()
	found := false
	err := filepath.Walk(filepath.Join(root, "services/compute"), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", "node_modules", "vendor", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		if strings.Contains(string(body), marker) {
			found = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("обход прод-дерева compute: %v", err)
	}
	return found
}

// goConstValue читает целочисленное значение именованной константы из Go-файла.
func goConstValue(root, rel, ident string) (int, error) {
	body, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		return 0, err
	}
	re := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(ident) + `\s*=\s*(\d+)`)
	m := re.FindStringSubmatch(string(body))
	if m == nil {
		return 0, os.ErrNotExist
	}
	return strconv.Atoi(m[1])
}

// TestDeclaredCardinalityLimitIsAlsoStatedInProse — ВТОРОЙ адрес величины: она
// названа словами в комментарии поля, там где её читает вызывающий.
//
// # Зачем второй адрес, если есть таблица
//
// Таблица `declaredCardinality` держит СВОЙСТВО (у предела есть исполнитель) и
// живёт внутри дерева; вызывающий её не видит. Прежде предел стоял в самом
// контракте — расширением `(size) = "<=N"`, — но и оно вызывающему ничего не
// давало: расширение частное, с частными номерами, и ни один существующий
// генератор клиента о нём не знает. Проза видна ВСЕМ и не требует ничего, кроме
// умения читать контракт.
//
// # Что именно проверяется, и почему число, а не фраза
//
// Что в собственном ведущем комментарии поля встречается ЧИСЛО предела. Требовать
// заданную фразу значило бы диктовать язык описания; требовать число — значит
// требовать факт. Комментарий, потерявший число при правке предела, краснит гейт
// по имени поля: величина, названная в двух местах, обязана быть одной.
func TestDeclaredCardinalityLimitIsAlsoStatedInProse(t *testing.T) {
	root := repoRoot(t)

	if len(prosePlacement) != len(declaredCardinality) {
		t.Fatalf("мест прозы %d, объявленных пределов %d — у части пределов адреса прозы нет вовсе, "+
			"и об этих полях гейт сказал бы «находок ноль», ничего не прочитав",
			len(prosePlacement), len(declaredCardinality))
	}

	var problems []string
	checked := 0
	for key, bound := range declaredCardinality {
		file, ok := prosePlacement[key]
		if !ok {
			problems = append(problems, key+": адрес прозы не назван")
			continue
		}
		dot := strings.LastIndex(key, ".")
		message, field := key[:dot], key[dot+1:]

		body, err := os.ReadFile(filepath.Join(root, file))
		if err != nil {
			t.Fatalf("%s не читается: %v — гейту не на чем исполниться", file, err)
		}
		comment, found := leadingFieldComment(string(body), message, field)
		if !found {
			problems = append(problems, key+": поля нет в "+file+
				" — утверждение о его комментарии беспредметно")
			continue
		}
		checked++
		number := strings.TrimPrefix(bound, "<=")
		if !strings.Contains(comment, number) {
			problems = append(problems, key+": комментарий поля не называет предела "+number+
				" (в "+file+"). Величина обязана быть названа прозой — там её видит вызывающий")
		}
	}

	if checked == 0 {
		t.Fatal("не прочитано НИ ОДНОГО комментария поля — «находок ноль» здесь означало бы " +
			"«ноль прочитанного»")
	}
	t.Logf("перепись: пределов %d, комментариев полей прочитано %d", len(declaredCardinality), checked)

	if len(problems) > 0 {
		sort.Strings(problems)
		t.Errorf("пределов, не названных прозой: %d\n  %s", len(problems), strings.Join(problems, "\n  "))
	}
}

// leadingFieldComment — собственный ведущий комментарий поля внутри названного
// сообщения. Второй результат отличает «поля нет» от «поле без комментария»: на
// первом утверждение о комментарии вакуумно, и молчать об этом нельзя.
func leadingFieldComment(src, message, field string) (string, bool) {
	msgStart := regexp.MustCompile(`(?m)^\s*message\s+` + regexp.QuoteMeta(message) + `\s*\{`).
		FindStringIndex(src)
	if msgStart == nil {
		return "", false
	}
	// Граница сообщения — по балансу фигурных скобок от его открывающей.
	depth, end := 0, len(src)
	for i := msgStart[1] - 1; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				end = i
				i = len(src)
			}
		}
	}
	block := src[msgStart[1]:end]

	decl := regexp.MustCompile(`(?m)^[ \t]*(?:repeated\s+[\w.]+|map<[^>]+>|[\w.]+)\s+` +
		regexp.QuoteMeta(field) + `\s*=\s*\d+`).FindStringIndex(block)
	if decl == nil {
		return "", false
	}

	lines := strings.Split(block[:decl[0]], "\n")
	var comment []string
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if l == "" && len(comment) == 0 {
			continue
		}
		if !strings.HasPrefix(l, "//") {
			break
		}
		comment = append(comment, l)
	}
	return strings.Join(comment, "\n"), true
}
