// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"sort"
	"strings"
	"testing"
)

// clientTruthRequestBodyDomains — реестр доменов под наблюдением.
//
// # Почему перечень ВЫПИСАН, а полнота его ВЫВЕДЕНА
//
// Пара «каталог сервиса → пакет контракта» из дерева не выводится: у
// балансировщика каталог `services/nlb`, а контракт — `kacho.cloud.loadbalancer.v1`.
// Совпадение имён у остальных шести — совпадение, а не свойство дерева, и
// вывести пару по нему значило бы завести правило, которое ломается ровно на
// исключении.
//
// Зато ПОЛНОТА перечня выводится и проверяется ниже: всякий сервис, у которого в
// дереве есть клиентская документация, обязан стоять здесь, а всякая запись —
// иметь страницы. Первое не даёт новому домену уйти из-под наблюдения молча,
// второе — записи пережить свой предмет.
var clientTruthRequestBodyDomains = []ClientTruthRequestBodyDomain{
	// Здесь стояла запись про службу доступа (`kaname.cloud.iam.v1`). Она ушла
	// вместе со своим предметом: служба вынесена отдельным продуктом, и ни
	// страниц, ни use-case-каталогов под `services/iam/` в дереве нет. Снятия
	// потребовала проба полноты реестра — «домен стоит в реестре, но страниц
	// под ним ноль». Контракт службы в дереве остался (им пользуются соседи и
	// край), но предмет ЭТОГО гейта — тело запроса, СВЕРЯЕМОЕ СО СТРАНИЦЕЙ, а
	// страницы уехали вместе со службой.
	{Name: "vpc", ProtoPackage: "kacho.cloud.vpc.v1",
		DocsDirs:    []string{"services/vpc/docs/content", "services/vpc/docs/engineering"},
		UseCaseDirs: []string{"services/vpc/internal/apps/kacho/api"}},
	{Name: "compute", ProtoPackage: "kacho.cloud.compute.v1",
		DocsDirs:    []string{"services/compute/docs/content", "services/compute/docs/engineering"},
		UseCaseDirs: []string{"services/compute/internal/apps/kacho/api"}},
	{Name: "storage", ProtoPackage: "kacho.cloud.storage.v1",
		DocsDirs:    []string{"services/storage/docs/content", "services/storage/docs/engineering"},
		UseCaseDirs: []string{"services/storage/internal/apps/kacho/api"}},
	// Каталог сервиса и пакет контракта здесь РАСХОДЯТСЯ — единственный такой
	// случай в дереве, и ради него перечень выписан, а не выведен.
	{Name: "nlb", ProtoPackage: "kacho.cloud.loadbalancer.v1",
		DocsDirs:    []string{"services/nlb/docs/content", "services/nlb/docs/engineering"},
		UseCaseDirs: []string{"services/nlb/internal/apps/kacho/api"}},
	{Name: "registry", ProtoPackage: "kacho.cloud.registry.v1",
		DocsDirs:    []string{"services/registry/docs/content", "services/registry/docs/engineering"},
		UseCaseDirs: []string{"services/registry/internal/apps/kacho/api"}},
	{Name: "geo", ProtoPackage: "kacho.cloud.geo.v1",
		DocsDirs:    []string{"services/geo/docs/content", "services/geo/docs/engineering"},
		UseCaseDirs: []string{"services/geo/internal/apps/kacho/api"}},
}

func clientTruthRequestBodyOptions(t *testing.T) ClientTruthRequestBodyOptions {
	t.Helper()
	return ClientTruthRequestBodyOptions{
		Tree:    clientTruthRepoTree(t),
		Domains: clientTruthRequestBodyDomains,
		DocExts: []string{".mdx", ".md"},
	}
}

// TestClientTruthRequestBodyRosterCoversEveryDocumentedService — полнота реестра.
//
// Перечень доменов выписан (пара «каталог → пакет» из дерева не выводится),
// поэтому его полнота обязана держаться предикатом, а не вниманием: иначе новый
// сервис с клиентской документацией окажется вне наблюдения, и «находок ноль»
// станет неотличимо от «не смотрели».
func TestClientTruthRequestBodyRosterCoversEveryDocumentedService(t *testing.T) {
	t.Parallel()
	tree := clientTruthRepoTree(t)

	documented := map[string]bool{}
	for _, rel := range tree.SortedFiles() {
		if !strings.HasPrefix(rel, "services/") {
			continue
		}
		parts := strings.Split(rel, "/")
		if len(parts) < 4 || parts[2] != "docs" {
			continue
		}
		if !strings.HasSuffix(rel, ".mdx") && !strings.HasSuffix(rel, ".md") {
			continue
		}
		documented[parts[1]] = true
	}
	if len(documented) == 0 {
		t.Fatal("ни одного сервиса с клиентской документацией не найдено — обход пуст, " +
			"и полнота реестра доказывалась бы даром")
	}

	inRoster := map[string]bool{}
	for _, d := range clientTruthRequestBodyDomains {
		inRoster[d.Name] = true
		// Запись обязана иметь предмет: каталог без страниц означает переезд
		// документации, а не покрытие.
		pages := 0
		for _, dir := range d.DocsDirs {
			pages += len(clientTruthTreeFiles(tree, dir, true, ".mdx", ".md"))
		}
		if pages == 0 {
			t.Errorf("домен %s стоит в реестре, но страниц под %v ноль — запись пережила свой "+
				"предмет: документация переехала либо сервис снят", d.Name, d.DocsDirs)
		}
	}
	var missing []string
	for svc := range documented {
		if !inRoster[svc] {
			missing = append(missing, svc)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("сервисы с клиентской документацией вне реестра: %s.\n"+
			"Их примеры не судятся ничем, и «находок ноль» по дереву означает «не смотрели». "+
			"Добавьте запись в clientTruthRequestBodyDomains: имя каталога, пакет контракта "+
			"(он может НЕ совпадать с именем каталога), каталоги страниц и use-case'ов.",
			strings.Join(missing, ", "))
	}
	t.Logf("перепись реестра: сервисов с документацией %d, записей в реестре %d",
		len(documented), len(clientTruthRequestBodyDomains))
}

// TestClientTruthRequestBodyKeysExistInTheRequestMessage — вердикт о НАСТОЯЩЕМ
// дереве по всем доменам реестра; их число печатает перепись.
//
// Способность падать доказывает не этот прогон, а инъекция
// (`clienttruth_requestbody_injection_test.go`): здесь только вердикт.
func TestClientTruthRequestBodyKeysExistInTheRequestMessage(t *testing.T) {
	t.Parallel()
	var log strings.Builder
	findings, census, err := AuditClientTruthRequestBody(clientTruthRequestBodyOptions(t), &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))

	// Премиса: прочитано то, что заведомо есть.
	//
	// ЗДЕСЬ СТОЯЛ ПЛОСКИЙ ПОРОГ (`census.Methods < 100`). Он был выбран на
	// популяции из семи доменов и сработал на ШТАТНОМ состоянии, когда служба
	// доступа уехала отдельным продуктом: методов стало 81 при целом разборе.
	// Порог, срабатывающий на штатном, перестают читать — а вместе с ним
	// перестают читать настоящую находку.
	//
	// Заменён на признак, который НЕ надо перенастраивать при смене состава
	// реестра и который строже плоского числа в главном: он требует, чтобы
	// дескрипторы прочитались У КАЖДОГО домена. Плоский порог этого не видел —
	// сумма в сотню набиралась, даже если один домен не дал ни одного метода.
	if census.Methods == 0 {
		t.Fatal("методов с телом не выведено НИ ОДНОГО — дескрипторы не прочитаны, " +
			"судить не по чему")
	}
	for _, d := range census.Domains {
		if d.Methods == 0 {
			t.Errorf("домен %s: методов с телом выведено 0 — его дескрипторы не "+
				"прочитаны, и «находок нет» о нём сказать нельзя", d.Name)
		}
	}
	if census.DocFiles < 100 {
		t.Fatalf("страниц документации %d — обход пуст, вердикт беспредметен", census.DocFiles)
	}
	// Вердикт выносится ТОЛЬКО о сопоставленных телах. Ноль сопоставленных либо
	// ноль рассуженных ключей означал бы, что он не вынесен ни разу.
	if census.BodiesMatched == 0 || census.KeysJudged == 0 {
		t.Fatalf("тел сопоставлено %d, ключей рассужено %d — сверка не состоялась",
			census.BodiesMatched, census.KeysJudged)
	}
	// ЗДЕСЬ СТОЯЛ ОТКАЗ на нулевую сумму невходных полей по дереву. Он снят
	// вместе со своим предметом: конвенционную форму отказа
	// (`Illegal argument <поле> (<пометка>)`) производили три места, и все три
	// жили в службе доступа, вынесенной отдельным продуктом. Тревое-широкий ноль
	// стал фактом О ДЕРЕВЕ, и проба падала бы на достижении своей цели —
	// зелёной её можно было бы удержать, только удержав в дереве отказ ради
	// самого отказа.
	//
	// Живость распознавателя держит ИНЪЕКЦИЯ, а не этот счёт: три её прогона
	// (`SilentOnALegalPage` · `SilentWhenTheRejectLosesItsMarker` ·
	// `RedOnKeyTheCodeRejects`) требуют выведения поля, снятия его вместе с
	// пометкой и образования находки — на СОБРАННОМ входе, не зависящем от
	// состава дерева.
	//
	// Величина названа переписью ВСЕГДА и рядом со своей зеркальной половиной:
	// расхождение «конвенционных ноль, а вне конвенции сколько-то» и есть
	// признак сломанного распознавателя, которого одно число не даёт.
	t.Logf("перепись невходных полей: в конвенционной форме %d, вне её %d "+
		"(ноль в первом числе при ненулевом втором — распознаватель ослеп; "+
		"ноль в обоих — таких отказов в дереве нет)",
		census.RejectedFields, census.RejectedFreeForm)
	// Каждый домен обязан быть ОСМОТРЕН. Ноль страниц у записи реестра — это
	// «не смотрели», а не «чисто».
	for _, d := range census.Domains {
		if d.DocFiles == 0 {
			t.Errorf("домен %s: страниц прочитано 0 — его примеры не судились ничем", d.Name)
		}
	}

	// Находки разведены по предикатам: у них разный исход для клиента и разная
	// починка. Слить их в один перечень значило бы вернуть тот вид отчёта, из
	// которого не видно, что чинить.
	var unrouted, keys []ClientTruthRequestBodyFinding
	for _, f := range findings {
		if f.Unrouted {
			unrouted = append(unrouted, f)
			continue
		}
		keys = append(keys, f)
	}

	if len(unrouted) > 0 {
		t.Errorf("пример клиентской документации показывает путь, который не резолвится ни в один "+
			"объявленный маршрут (%d из %d распознанных адресов):\n%s\n\n"+
			"Клиенту это дороже неверного ключа: неверный ключ край отбрасывает молча, "+
			"а неверный путь даёт `404` без тела — отказ, не называющий верного написания. "+
			"Маршруты выводятся из `google.api.http` контрактов всех семи доменов; "+
			"правьте страницу, а не этот список.",
			len(unrouted), census.BodiesMatched+census.BodiesUnrouted, describeBodyFindings(unrouted))
	}
	if len(keys) > 0 {
		t.Errorf("пример запроса в клиентской документации несёт ключ, которого нет в сообщении "+
			"(тел сопоставлено %d, ключей рассужено %d):\n%s\n\n"+
			"Край выбрасывает неизвестное поле МОЛЧА, поэтому клиент получает не отказ на ключе, "+
			"а отказ на другом поле — либо «<поле> is required» о том, что он, по его мнению, "+
			"прислал. Сообщение выводится из дескрипторов контракта; правьте пример, а не список.",
			census.BodiesMatched, census.KeysJudged, describeBodyFindings(keys))
	}
}

func describeBodyFindings(fs []ClientTruthRequestBodyFinding) string {
	lines := make([]string, 0, len(fs))
	for _, f := range fs {
		lines = append(lines, "  "+f.String())
	}
	return strings.Join(lines, "\n")
}
