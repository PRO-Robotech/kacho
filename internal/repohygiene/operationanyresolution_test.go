// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// operationanyresolution_test.go — ПАРА гейтов о разрешимости типов, которые
// владельцы кладут в `Operation.response` / `Operation.metadata`.
//
// # Предмет (полностью — в шапке `internal/operationany`)
//
// `Any` — это «адрес типа + байты». Упаковка у владельца реестра не спрашивает;
// спрашивает РАСПАКОВКА, и происходит она в ДРУГОМ процессе — на крае, где
// protojson идёт в реестр типов процесса. Туда тип попадает единственным
// способом: пакет с ним ВЛИНКОВАН в бинарь. Типа нет — маршаллинг падает, и
// вызывающий получает 500 на штатном пути.
//
// Ровно так это отказало: `google.protobuf.Empty` линковался в край ПОБОЧНО,
// импортом файла, обслуживавшего чужой предмет. Файл сняли вместе с его
// предметом — разрешение уехало с ним. Сборка не сломалась, ни одна проба не
// покраснела: связь нигде не была объявлена.
//
// # Почему гейта ДВА и как они делят предмет
//
//	ПОЛНОТА     TestEdgeResolvesEveryProtoPackageItsOwnersCanProduce
//	            край обязан линковать всякий proto-пакет, влинкованный во
//	            владельца. О ФОРМЕ УПАКОВКИ не спрашивает вовсе — потому и не
//	            имеет слепой зоны: тип, которого во владельце нет, тот и
//	            упаковать не может, ни литералом, ни переменной, ни рефлексией.
//
//	НАМЕРЕННОСТЬ TestForeignTypesPackedInTheTreeAreDeclaredByTheEdge
//	            всякий ЧУЖОЙ (не из наших стабов) тип, упакованный владельцем в
//	            дереве, обязан стоять в объявлении края. Полнота этого не даёт:
//	            она зелена и на СЛУЧАЙНОЙ линковке — ровно той, что однажды и
//	            уехала. Объявление делает разрешение решением, а не совпадением.
//
// Порознь каждый зелен на дефекте другого; вместе они держат обе половины.
package repohygiene

import (
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/operationany"
)

// Пакеты-маркеры, по которым выводится РОЛЬ бинаря. Перечня бинарей в гейте нет
// намеренно: рукописный список разошёлся бы с деревом молча, и новый сервис
// оказался бы вне наблюдения, ничего об этом не сказав.
const (
	// ownerMarker — кто ПРОИЗВОДИТ `Operation` с `Any` внутри.
	ownerMarker = "github.com/PRO-Robotech/kacho/pkg/operations"
	// edgeMarker — кто ОТОБРАЖАЕТ `Operation` в JSON, то есть распаковывает
	// `Any` через реестр типов своего процесса.
	edgeMarker = "github.com/PRO-Robotech/kacho/gateway/internal/restmux"
)

// TestEdgeResolvesEveryProtoPackageItsOwnersCanProduce — ПОЛНОТА.
//
// Требование: множество proto-регистрирующих пакетов, влинкованных в бинарь
// ВЛАДЕЛЬЦА, обязано быть подмножеством того же множества у КРАЯ.
//
// # Почему по линковке, а не по местам упаковки
//
// Место упаковки записывается многими законными формами, и часть из них
// синтаксически неразрешима: `anypb.New(protoconv.Instance(x))` не называет тип
// вовсе. Замер по дереву: мест упаковки 137, из них тип НАПИСАН в 45, а в 92
// назван переменной или вызовом. Распознаватель по форме имел бы слепую зону
// ровно там, где её не видно, — то есть отвечал бы «ноль находок» вместо «не
// прочитано».
//
// Линковка о форме не спрашивает: чтобы упаковать тип, владелец обязан его
// СОБРАТЬ, а собрать он может только то, что влинковано. Поэтому надмножество у
// края закрывает КАЖДУЮ форму, включая те, которых ещё не написали.
//
// # Что признаётся proto-регистрирующим пакетом и куда ошибается признак
//
// Пакет с файлом `*.pb.go`. Признак шире предмета в БЕЗОПАСНУЮ сторону: пакет с
// одними gRPC-заглушками сообщений не регистрирует, но будет засчитан — лишнее
// требование к краю, а не пропущенный тип.
//
// # Роли выводятся, а не выписываются
//
// Владелец — тот, кто линкует `pkg/operations`; край — тот, кто линкует
// `gateway/internal/restmux`. Новый сервис попадает под гейт в тот же день, что
// заводится, и без правки этого файла.
func TestEdgeResolvesEveryProtoPackageItsOwnersCanProduce(t *testing.T) {
	root := repoRoot(t)

	mains, err := goListMainPackages(root)
	if err != nil {
		t.Fatalf("перечень исполняемых пакетов: %v", err)
	}
	if len(mains) == 0 {
		t.Fatal("исполняемых пакетов НОЛЬ — обход беспредметен, вердикт недействителен")
	}

	var (
		edges   []binaryProtoSurface
		owners  []binaryProtoSurface
		markers = []string{ownerMarker, edgeMarker}
	)
	for _, main := range mains {
		surface, err := readBinaryProtoSurface(root, main, markers)
		if err != nil {
			t.Fatalf("%s: %v", main, err)
		}
		if surface.Links[edgeMarker] {
			edges = append(edges, surface)
			continue // край сам себе надмножество
		}
		if surface.Links[ownerMarker] {
			owners = append(owners, surface)
		}
	}

	switch {
	case len(edges) == 0:
		t.Fatalf("ни один бинарь не линкует %s — гейт не нашёл края, о котором "+
			"говорит. Это отказ, а не пропуск: молчание здесь означало бы, что "+
			"сравнивать не с чем, и было бы неотличимо от исправного дерева", edgeMarker)
	case len(edges) > 1:
		t.Fatalf("краёв больше одного (%d) — предмет гейта предполагает ОДИН "+
			"процесс, отображающий Operation в JSON; при нескольких надмножество "+
			"надо требовать от каждого, и это решение, а не умолчание", len(edges))
	case len(owners) == 0:
		t.Fatalf("ни один бинарь не линкует %s — владельцев ноль, сравнивать "+
			"нечего", ownerMarker)
	}
	edge := edges[0]

	findings := auditProtoSurfaces(edge, owners)
	for _, f := range findings {
		t.Errorf("владелец %s линкует proto-пакеты, которых НЕТ у края %s:\n  %s\n"+
			"Владелец способен положить такой тип в Operation.response/metadata, а "+
			"край не сможет его разрешить: protojson уронит маршаллинг, и вызывающий "+
			"получит 500 на штатном пути. Чинится ЯКОРЕМ в internal/operationany, а "+
			"не исключением здесь.",
			f.Owner, edge.Command, strings.Join(f.Missing, "\n  "))
	}

	t.Logf("перепись: исполняемых пакетов %d, из них край 1 (%d proto-пакетов), "+
		"владельцев %d; находок %d",
		len(mains), len(edge.Proto), len(owners), len(findings))
}

// TestForeignTypesPackedInTheTreeAreDeclaredByTheEdge — НАМЕРЕННОСТЬ.
//
// Требование: всякий ЧУЖОЙ тип, который владелец упаковывает в `Any`, стоит в
// объявлении края (`internal/operationany`).
//
// # Что значит «чужой» и почему граница именно там
//
// Свои стабы (`pkg/api/...`) край линкует ПО СВОЕЙ РАБОТЕ: он обслуживает
// маршруты всех доменов, и без этих пакетов не соберётся. Их разрешение —
// следствие его собственного предмета, объявлять его отдельно нечего.
// Всё остальное — `google.protobuf.*`, `google.rpc.*`, любой сторонний proto —
// край линкует только если кто-то об этом позаботился. Ровно эта линковка и
// уехала однажды, будучи побочной.
//
// # Область обхода — код ВЛАДЕЛЬЦЕВ, не всё дерево
//
// `services/` и `pkg/`. Упаковка внутри самого края границы процесса не
// пересекает: что он упаковал, то и разрешит своим же реестром. Это НЕ
// послабление, а предмет: гейт о том, что переживает передачу между процессами.
//
// # Единый источник вместо сверки текста
//
// Объявление читается ЗНАЧЕНИЯМИ через `operationany.AnchoredGoCoordinates()`:
// одно и то же значение даёт и адрес типа для края, и пару «Go-пакет + Go-имя»
// для сверки с местом упаковки. Разойтись им негде. Чтения чужого исходника как
// текста здесь нет: место упаковки разбирается по синтаксическому дереву,
// объявление — рефлексией.
func TestForeignTypesPackedInTheTreeAreDeclaredByTheEdge(t *testing.T) {
	tt := newTrackedTree(t, repoRoot(t))

	var files []string
	for rel := range tt.files {
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			continue
		}
		if strings.HasPrefix(rel, "services/") || strings.HasPrefix(rel, "pkg/") {
			files = append(files, rel)
		}
	}
	sort.Strings(files)
	if len(files) == 0 {
		t.Fatal("файлов владельцев ноль — обход беспредметен, вердикт недействителен")
	}

	census := collectAnyPackSites(tt.root, files)
	if len(census.ParseFailed) > 0 {
		t.Fatalf("не разобрано %d файлов, вердикт недействителен:\n  %s",
			len(census.ParseFailed), strings.Join(census.ParseFailed, "\n  "))
	}
	if census.CallsSeen == 0 {
		t.Fatal("мест упаковки в Any НОЛЬ — распознаватель ничего не нашёл там, где " +
			"упаковка заведомо есть. Это отказ: «ноль находок» здесь неотличимо от " +
			"«ноль прочитанного»")
	}

	declared := map[string]string{} // "<go-пакет>.<Имя>" → адрес типа
	for _, c := range operationany.AnchoredGoCoordinates() {
		declared[c.Package+"."+c.Name] = c.TypeURL
	}
	if len(declared) == 0 {
		t.Fatal("край не объявил ни одного якоря — сверять нечего, гейт беспредметен")
	}
	required := map[string]bool{}
	for _, u := range operationany.RequiredResponseTypeURLs() {
		required[u] = true
	}

	ownStubs, foreign, findings := 0, 0, 0
	for _, site := range census.Written {
		if strings.HasPrefix(site.Package, "github.com/PRO-Robotech/kacho/pkg/api/") {
			ownStubs++
			continue
		}
		foreign++
		url, isDeclared := declared[site.Package+"."+site.Name]
		if !isDeclared {
			findings++
			t.Errorf("%s:%d упаковывает в Any ЧУЖОЙ тип %s (%s, форма %s), которого "+
				"край не объявлял.\nКрай разрешает такой тип только если пакет "+
				"%s влинкован в его бинарь, а линковка, о которой никто не "+
				"позаботился, уезжает вместе с чужим файлом — так это уже "+
				"отказывало.\nЛибо заведи якорь и намерение в internal/operationany, "+
				"либо не упаковывай этот тип.",
				site.File, site.Line, site.GoCoordinate(), site.Package, site.Form, site.Package)
			continue
		}
		if !required[url] {
			findings++
			t.Errorf("%s:%d упаковывает %s: якорь для него есть, а НАМЕРЕНИЯ (%s) в "+
				"RequiredResponseTypeURLs нет — край держит тип, ничего о нём не "+
				"обещая, и снимет якорь как непонятный",
				site.File, site.Line, site.GoCoordinate(), url)
		}
	}

	t.Logf("перепись: файлов владельцев прочитано %d, мест упаковки %d "+
		"(тип написан %d: своих стабов %d, чужих %d; тип НЕ написан %d — граница "+
		"распознавателя, её держит гейт полноты); объявлено якорей %d, намерений %d; "+
		"судимых форм записи %d [%s]; находок %d",
		census.FilesRead, census.CallsSeen,
		len(census.Written), ownStubs, foreign, census.Unwritten,
		len(declared), len(required),
		len(anyPackFormNames), strings.Join(anyPackFormNames, ", "),
		findings)
}

// TestTerraformProviderIsOutOfThisSubjectAndSaysWhy — РЕШЕНИЕ по провайдеру
// Terraform, записанное так, чтобы оно истекало само.
//
// # Решение: провайдер в предмет этой пары гейтов НЕ ВХОДИТ
//
// Провайдер — потребитель контрактов и, как всякий потребитель, мог бы попасть в
// тот же класс: он линкует `emptypb` (транзитивно, прямых импортов ноль) ровно в
// той форме, что однажды отказала на крае.
//
// Но предмет класса — не линковка сама по себе, а РАЗРЕШЕНИЕ типа по адресу
// через реестр процесса. Провайдер этого не делает вовсе и делает это осознанно:
// `Operation` он разбирает `encoding/json` в `map[string]any`
// (`terraform/internal/client/operation.go`), а причину написал у себя в шапке —
// разбор типизированным сообщением молча зависел бы от того, импортировал ли
// кто-то в этой сборке нужный пакет, и забытый импорт ронял бы ожидание в
// рантайме сообщением не по делу. То есть провайдер УЖЕ вынес это решение, и
// вынес его в сторону, при которой класс к нему неприменим by construction.
//
// Поэтому роль «владельца» выводится линковкой `pkg/operations`, которую
// провайдер не линкует, — он выпадает из обхода механизмом, а не исключением.
//
// # Почему это проба, а не абзац
//
// Решение опирается на ФАКТ о дереве, а факт стареет молча. Проба сторожит
// именно его: как только провайдер начнёт трогать `Any`, она покраснеет и
// потребует решение пересмотреть, а не унаследовать.
func TestTerraformProviderIsOutOfThisSubjectAndSaysWhy(t *testing.T) {
	tt := newTrackedTree(t, repoRoot(t))

	var files []string
	for rel := range tt.files {
		if strings.HasPrefix(rel, "terraform/") && strings.HasSuffix(rel, ".go") &&
			!strings.HasSuffix(rel, "_test.go") {
			files = append(files, rel)
		}
	}
	sort.Strings(files)
	if len(files) == 0 {
		t.Fatal("файлов провайдера ноль — предпосылка решения не прочитана, " +
			"вердикт недействителен")
	}

	census := collectAnyPackSites(tt.root, files)
	if len(census.ParseFailed) > 0 {
		t.Fatalf("не разобрано %d файлов провайдера: %s",
			len(census.ParseFailed), strings.Join(census.ParseFailed, "; "))
	}
	if census.CallsSeen != 0 {
		t.Errorf("провайдер упаковывает в Any (%d мест) — предпосылка решения "+
			"«он с Any не работает» БОЛЬШЕ НЕ ВЕРНА. Решение о его исключении из "+
			"предмета надо пересмотреть, а не унаследовать", census.CallsSeen)
	}

	if resolvers := census.Resolutions; resolvers != 0 {
		t.Errorf("провайдер разрешает типы через реестр (%d мест) — он стал "+
			"потребителем того же предмета, что и край, и решение о его "+
			"исключении надо пересмотреть", resolvers)
	}

	t.Logf("перепись: файлов провайдера прочитано %d, мест упаковки в Any %d, "+
		"мест разрешения типа через реестр %d — предпосылка решения в силе",
		census.FilesRead, census.CallsSeen, census.Resolutions)
}
