// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// quotaadmitkinds_injection_test.go — ДОКАЗАТЕЛЬСТВО СПОСОБНОСТИ УПАСТЬ у гейта
// сходимости видов учёта, по каждой оси и в обе стороны.
//
// Гейт переустроен целиком (задача продукта #2669): охват выведен из дерева
// вместо двух выписанных координат, распознаватель научен второй форме записи
// вида. Переустройство требует ПОВТОРНОЙ инъекции — совпадение переписи с
// прежней доказывает, что не сузился предмет, и не говорит НИЧЕГО о том,
// сохранилась ли способность падать (`testing.md` §«Гейт на класс», п. 8).
//
// Оси, по каждой — дефект и ЗАКОННЫЙ БЛИЗНЕЦ:
//
//	форма записи вида   литерал · именованная константа · неразрешимый параметр
//	сторона SQL         вид на носителе-проекте · вид на носителе-родителе
//	вердикт             вопрос без списания · списание без вопроса · сходится
//	владелец            дефект у одного не прячется за чистотой остальных
package repohygiene

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// askedKindsInSource — распознаватель, наведённый на текст: инъекция подаёт ему
// исходник, а не файл дерева, поэтому доказательство не зависит от того, что в
// дереве лежит сегодня.
func askedKindsInSource(t *testing.T, src string) ([]string, int) {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "probe.go", src, 0)
	if err != nil {
		t.Fatalf("фикстура не разобрана: %v", err)
	}
	return quotaAskedKinds(file, quotaKindConstants(file))
}

const probeAskedSource = `package probe

const KindVolumes = "storage.volumes"

// Комментарий называет вид "storage.snapshots" — и распознаватель обязан его
// НЕ увидеть: он судит узел разбора, а не текст.
const notAKind = "storage"

func run(ctx, id any, kind string) {
	g.Admit(ctx, id, "storage.images")
	g.Admit(ctx, id, KindVolumes)
	g.AdmitCarrier(ctx, "storage.image", id, quota.KindVolumes)
	g.Admit(ctx, id, kind)
	g.Resolve(ctx, id, "storage.snapshots")
	_ = notAKind
}
`

// TestQuotaAdmitRecognizerReadsBothNotations — распознаватель знает ОБЕ законные
// формы записи вида и не выдумывает третьей.
//
// Прежняя редакция гейта объявляла предпосылкой «в этом дереве виды пишутся
// литералами». Отступление БЫЛО — `storage` подаёт вид именованной константой, —
// и лежало вне охвата, то есть находкой не стало ни для кого.
func TestQuotaAdmitRecognizerReadsBothNotations(t *testing.T) {
	t.Parallel()
	asked, unresolved := askedKindsInSource(t, probeAskedSource)

	got := map[string]int{}
	for _, k := range asked {
		got[k]++
	}
	// Литерал — первая форма.
	if got["storage.images"] != 1 {
		t.Errorf("вид, записанный литералом, не распознан: %v", got)
	}
	// Именованная константа — вторая форма, голым именем и через имя пакета.
	if got["storage.volumes"] != 2 {
		t.Errorf("вид, записанный именованной константой, распознан %d раз(а), ожидалось 2: %v",
			got["storage.volumes"], got)
	}
	// ЗАКОННЫЙ БЛИЗНЕЦ: вид в комментарии и вид в аргументе ЧУЖОГО глагола не
	// вопросы совещательной полосы — распознаватель обязан молчать о них.
	if _, ok := got["storage.snapshots"]; ok {
		t.Errorf("распознаватель принял за вопрос полосы то, чем он не является: %v", got)
	}
	// Вид, приехавший ПАРАМЕТРОМ, не разрешается — и это считается вслух.
	if unresolved != 1 {
		t.Errorf("вызовов с неразрешимым видом %d, ожидался 1", unresolved)
	}
}

// TestQuotaChargedKindsSplitsByCarrier — сторона SQL различает носителя.
//
// Разделение несущее: каталог посадки сверяется с видами на носителе-ПРОЕКТЕ, а
// совещательная полоса спрашивает про оба. Прежняя редакция читала только первый
// аргумент объявления и теряла второй род молча.
func TestQuotaChargedKindsSplitsByCarrier(t *testing.T) {
	t.Parallel()
	const sql = `
CREATE TRIGGER subnets_quota_count
    AFTER INSERT OR DELETE ON kacho_vpc.subnets
    FOR EACH ROW EXECUTE FUNCTION kacho_vpc.kacho_quota_count(
        'vpc.subnet', '', 'network_id', 'vpc.network.subnet');

CREATE TRIGGER networks_quota_carrier_subnet
    AFTER INSERT OR DELETE ON kacho_vpc.networks
    FOR EACH ROW EXECUTE FUNCTION kacho_vpc.kacho_quota_carrier_lifecycle(
        'vpc.network.routeTable');
`
	onProject, nested := quotaChargedKinds(sql)

	if !onProject["vpc.subnet"] || len(onProject) != 1 {
		t.Errorf("виды на носителе-проекте распознаны неверно: %v", onProject)
	}
	if !nested["vpc.network.subnet"] || !nested["vpc.network.routeTable"] || len(nested) != 2 {
		t.Errorf("виды на носителе-родителе распознаны неверно: %v", nested)
	}
	// ЗАКОННЫЙ БЛИЗНЕЦ: имя столбца и пустая строка видами не являются.
	for _, notKind := range []string{"network_id", ""} {
		if onProject[notKind] || nested[notKind] {
			t.Errorf("распознаватель принял %q за вид", notKind)
		}
	}
}

// TestQuotaChargedKindsIsTolerantToLineBreaks — перенос строки ТЕРПИМ.
//
// Объявление триггера переносит вид на следующую строку у двух владельцев из
// пяти, и однострочная форма теряла бы их молча. Замер приёмки, сделанный такой
// формой, давал 17 вместо 19.
func TestQuotaChargedKindsIsTolerantToLineBreaks(t *testing.T) {
	t.Parallel()
	onProject, _ := quotaChargedKinds("EXECUTE FUNCTION kacho_quota_count(\n    'registry.repositories');")
	if !onProject["registry.repositories"] {
		t.Errorf("вид, перенесённый на следующую строку, потерян: %v", onProject)
	}
}

// TestQuotaSidesVerdictIsRedOnEachAxis — вердикт падает по КАЖДОЙ оси и молчит
// на сходящемся владельце.
func TestQuotaSidesVerdictIsRedOnEachAxis(t *testing.T) {
	t.Parallel()
	none := map[string]bool{}

	// ЗАКОННЫЙ БЛИЗНЕЦ: стороны сходятся — вердикт молчит.
	if out := judgeQuotaSides("vpc",
		map[string]bool{"vpc.network": true},
		map[string][]string{"vpc.network": {"create.go"}}, none); len(out) != 0 {
		t.Errorf("сходящийся владелец объявлен находкой: %v", out)
	}

	// Ось «вопрос без списания».
	stray := judgeQuotaSides("nlb",
		map[string]bool{"loadbalancer.listeners": true},
		map[string][]string{"loadbalancer.listenerz": {"create.go"}}, none)
	if len(stray) == 0 || !strings.Contains(strings.Join(stray, "\n"), "loadbalancer.listenerz") {
		t.Errorf("вопрос про несписываемый вид не найден: %v", stray)
	}
	if !strings.Contains(strings.Join(stray, "\n"), "nlb:") {
		t.Errorf("находка не называет владельца: %v", stray)
	}
	if !strings.Contains(strings.Join(stray, "\n"), "create.go") {
		t.Errorf("находка не называет координату вопроса: %v", stray)
	}

	// Ось «списание без вопроса».
	uncovered := judgeQuotaSides("registry",
		map[string]bool{"registry.registries": true, "registry.repositories": true},
		map[string][]string{"registry.registries": {"create.go"}}, none)
	if len(uncovered) == 0 || !strings.Contains(strings.Join(uncovered, "\n"), "registry.repositories") {
		t.Errorf("списание без вопроса не найдено: %v", uncovered)
	}

	// ВЕДОМОСТЬ прощает ровно названное и ровно у названного владельца.
	forgiven := map[string]bool{"registry.repositories": true}
	if out := judgeQuotaSides("registry",
		map[string]bool{"registry.registries": true, "registry.repositories": true},
		map[string][]string{"registry.registries": {"create.go"}}, forgiven); len(out) != 0 {
		t.Errorf("запись ведомости не сработала: %v", out)
	}
	if out := judgeQuotaSides("registry",
		map[string]bool{"registry.registries": true, "registry.tags": true},
		map[string][]string{"registry.registries": {"create.go"}}, forgiven); len(out) == 0 {
		t.Errorf("ведомость простила вид, которого в ней нет: %v", out)
	}
}

// TestQuotaSidesVerdictJudgesEachOwnerSeparately — дефект у одного владельца не
// прячется за чистотой остальных.
//
// Прежняя редакция гейта судила ОДНОГО владельца из пяти, и её «ноль находок»
// читалось как свойство платформы. Ось проверяет, что разбиение по владельцам
// настоящее, а не декоративное.
func TestQuotaSidesVerdictJudgesEachOwnerSeparately(t *testing.T) {
	t.Parallel()
	none := map[string]bool{}
	clean := judgeQuotaSides("compute",
		map[string]bool{"compute.instance": true},
		map[string][]string{"compute.instance": {"create.go"}}, none)
	dirty := judgeQuotaSides("storage",
		map[string]bool{"storage.volumes": true},
		map[string][]string{}, none)

	if len(clean) != 0 {
		t.Errorf("чистый владелец объявлен находкой: %v", clean)
	}
	if len(dirty) == 0 || !strings.Contains(strings.Join(dirty, "\n"), "storage:") {
		t.Errorf("дефект у второго владельца не найден либо не назван по имени: %v", dirty)
	}
}
