// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	apiv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/api"
	"github.com/PRO-Robotech/kacho/pkg/contractroot"
	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// secretbearingsurface_test.go — ОСЬ 2 ГЕЙТА BAT-1-73: НЕПОМЕЧЕННЫХ НОСИТЕЛЕЙ НА
// ПОВЕРХНОСТИ НЕТ (задача #1217, приёмка BAT-1 §4.3.3, сценарий BAT-1-73).
//
// # ПОЧЕМУ ОДНОЙ ОСИ БЫЛО МАЛО
//
// Ось 1 (`services/iam/cmd/kaname/secret_bearing_ledger_test.go`) сверяет
// ПОМЕЧЕННЫЕ поля с перечнем подметальщика. На НЕПОМЕЧЕННОМ поле она молчит by
// construction: помеченных ноль — находок ноль, гейт зелен. То есть одна ось
// держала согласие двух объявлений между собой, а не отсутствие секретов на
// поверхности.
//
// Случай «завели поле-носитель и забыли пометить» не ловился ВОВСЕ. Ось 2 его и
// закрывает: поле, ЧЬЁ ИМЯ ВЫГЛЯДИТ СЕКРЕТОМ, обязано быть либо помечено, либо
// названо в ведомости исключений с причиной.
//
// # ПОЧЕМУ ЗДЕСЬ, А НЕ РЯДОМ С ОСЬЮ 1
//
// Оси судят РАЗНЫЕ множества и потому имеют разный охват. Оси 1 нужен перечень
// подметальщика — он живёт в композиционном корне iam, и ось живёт там же. Оси 2
// нужен ВЕСЬ контракт: непомеченный носитель может появиться в любом домене, и
// проверка, видящая только контракты, слинкованные в один бинарь, объявила бы
// «находок ноль» о половине дерева.
//
// Набор дескрипторов ЭТОГО пакета покрывает все контракты `proto/kacho`, и это
// не принято на веру, а ПРОВЕРЯЕТСЯ (см. предпосылку ниже): файл на диске,
// которого нет в наборе, — слепая зона, а не «находок нет».
//
// # ЧИТАЮТСЯ ДЕСКРИПТОРЫ, А НЕ ТЕКСТ
//
// Поиск по образцу над `.proto` не отличает объявление поля от строкового
// литерала и от комментария, который эту же пометку объясняет, — и остаётся
// зелёным при СНЯТОЙ пометке. Здесь и пометка, и имя, и вид поля берутся из
// дескрипторов.
//
// # ОБРАЗЦЫ АДЪЮДИЦИРОВАНЫ, А НЕ ВЫДУМАНЫ (§4.3.3)
//
// Единица адъюдикации — уникальное имя поля среди `string`/`bytes`-полей
// контрактов `kacho`. Отвергнутые образцы названы вместе с числом: голый `*key*`
// давал восемь ложных на одну истинную, голый `*token*` — шесть на одну.
// Инструмент, у которого почти все находки ложные, перестают читать, а перестав
// читать, возвращаются к отсутствию проверки.
//
// # ЧЕГО ОСЬ 2 НЕ ДЕРЖИТ — названо, чтобы её не сочли полным покрытием
//
//	поле-носитель, ЧЬЁ ИМЯ НИ НА ЧТО НЕ ПОХОЖЕ и которое не пометили: ось 1
//	  молчит (не помечено), ось 2 молчит (имя не совпало). Механического
//	  держателя нет — держит обзор изменения контракта. Живой пример в дереве:
//	  `ResolveBasicCredentialRequest.presented` помечен и НИ ОДНОМУ образцу не
//	  отвечает; не пометь его автор — не увидел бы никто;
//	ПРАВИЛЬНОСТЬ пометки: помеченное поле, секретом не являющееся, даёт лишнюю
//	  запись подметальщику; последствие безвредно, различить это гейт не может.

// secretNamePattern — один адъюдицированный образец имени.
type secretNamePattern struct {
	name  string
	match func(string) bool
}

// secretNamePatterns — перечень образцов §4.3.3.
//
// Образец смотрит ВПЕРЁД: `*password*` сегодня не ловит ничего, и это НЕ повод
// его снять — в отличие от записи ведомости, которой предмет нужен сейчас.
// Разница не педантская: образец запрещает завести поле, ведомость прощает
// заведённое.
var secretNamePatterns = []secretNamePattern{
	{"*secret*", func(n string) bool { return strings.Contains(n, "secret") }},
	{"*private_key*", func(n string) bool { return strings.Contains(n, "private_key") }},
	{"*password*", func(n string) bool { return strings.Contains(n, "password") }},
	{"*passwd*", func(n string) bool { return strings.Contains(n, "passwd") }},
	{"*credential*", func(n string) bool { return strings.Contains(n, "credential") }},
	{"*_pem", func(n string) bool { return strings.HasSuffix(n, "_pem") }},
	{"*_token", func(n string) bool { return strings.HasSuffix(n, "_token") }},
}

// paginationCursorNames — КОНВЕНЦИЯ ПЛАТФОРМЫ, вычитаемая СТРУКТУРНО.
//
// Курсор постраничного чтения (`api-conventions.md` §Pagination) кончается на
// `_token` и потому попадает под образец. Его вычитают ИМЕНЕМ КОНВЕНЦИИ, а не
// записями ведомости: курсор встречается в сотнях сообщений, и ведомость в
// сотни строк перестают читать — а перестав читать, возвращаются к отсутствию
// проверки.
//
// Вычет — тоже исключение, поэтому он тоже обязан иметь ПРЕДМЕТ: перепись
// печатает, сколько полей он снял, и ноль здесь — находка, а не тишина.
var paginationCursorNames = map[string]bool{"page_token": true, "next_page_token": true}

// secretSurfaceExclusion — запись ведомости: поле, чьё имя совпало с образцом, а
// носителем секрета НЕ является.
//
// ЗАПИСЬ ЗАКОННА ТОЛЬКО ДЛЯ НЕ-НОСИТЕЛЯ. Носитель закрывается ПОМЕТКОЙ и ничем
// иным — иначе ведомость проглатывает ровно то поле, ради которого фаза
// заведена: имя носителя под образец попадает by construction, запись «исключено,
// потому что …» выглядит законной, ось 1 отрабатывает на ПУСТОМ множестве, и
// требование §4.3.2 остаётся держаться одной прозой. Поле, одновременно
// помеченное и названное здесь, — находка: одно из двух объявлений ложно.
type secretSurfaceExclusion struct {
	// message / field — координата ровно в той форме, в какой её печатает обход.
	message, field string
	// why — почему это НЕ секрет. Причина, а не отсылка: запись без причины
	// неотличима от прощения.
	why string
}

// id — ключ сверки записи с деревом.
func (e secretSurfaceExclusion) id() string { return e.message + "." + e.field }

// secretSurfaceExclusions — ВЕДОМОСТЬ ЛОЖНЫХ СРАБАТЫВАНИЙ ОБРАЗЦА.
//
// Каждая запись обязана иметь предмет СЕГОДНЯ. Поле снято, переименовано либо
// помечено — запись потеряла предмет и становится НАХОДКОЙ: иначе первая же
// запись переживёт свой предмет и унаследует слепую зону следующему.
var secretSurfaceExclusions = []secretSurfaceExclusion{
	{"kaname.cloud.iam.v1.IssueSAKeyResponse", "public_key_pem",
		"публичная половина ключевой пары; каноническая копия у издателя, раскрытие безвредно"},
	{"kaname.cloud.iam.v1.IssueUserTokenResponse", "public_key_pem",
		"публичная половина ключевой пары; ею проверяют подпись утверждения, а не подписывают"},
	{"kaname.cloud.iam.v1.TrustedSubject", "public_key_pem",
		"публичная половина доверенного субъекта федерации — вход проверки, не секрет"},
	{"kaname.cloud.iam.v1.UserOAuthClient", "public_key_pem",
		"публичная половина строки реестра удостоверений"},
	{"kaname.cloud.iam.v1.ResolveBasicCredentialResponse", "credential_id",
		"ИДЕНТИФИКАТОР удостоверения, которым адресуется отзыв; секрета не несёт"},
	{"kaname.cloud.iam.v1.CheckBasicCredentialLiveRequest", "credential_id",
		"тот же ИДЕНТИФИКАТОР на ВХОДЕ вопроса о живости (#1450). Входная сторона " +
			"опаснее выходной: сюда вызывающий волен прислать что угодно, в том числе " +
			"полную предъявленную строку. Поэтому «секрета не несёт» здесь не обещание, " +
			"а ЭНФОРСМЕНТ: значение, разбираемое как предъявленная строка, отвергается " +
			"единым отказом ДО вопроса авторитету и не логируется"},
	{"kacho.cloud.storage.v1.StorageBackend", "credentials_ref",
		"ССЫЛКА на учётные данные в хранилище секретов (`<схема>://<путь>`); сам секрет " +
			"через API не проходит и в нашей БД не лежит"},
	{"kacho.cloud.storage.v1.CreateStorageBackendRequest", "credentials_ref",
		"та же ссылка на входе; значение, не похожее на ссылку (в том числе сам секрет), " +
			"отвергается INVALID_ARGUMENT"},
	{"kacho.cloud.storage.v1.UpdateStorageBackendRequest", "credentials_ref",
		"та же ссылка при ротации ССЫЛКИ, не секрета"},
}

// secretSurfaceField — одно поле контракта, каким его видит ось 2.
type secretSurfaceField struct {
	message string
	name    string
	marked  bool
	pattern string // совпавший образец; "" — ни один
	cursor  bool   // курсор постраничного чтения (структурный вычет)
}

// id — координата поля одной строкой.
func (f secretSurfaceField) id() string { return f.message + "." + f.name }

// secretSurfaceCensus — объём осмотренного. Печатается ЦЕЛИКОМ: «находок ноль»
// обязано быть отличимо от «прочитано ноль».
type secretSurfaceCensus struct {
	messages, fields, stringFields, uniqueNames int
	marked, matched, cursor, excluded           int
}

// TestBAT1_73_Axis2_NoUnmarkedSecretOnTheSurface — сама ось.
func TestBAT1_73_Axis2_NoUnmarkedSecretOnTheSurface(t *testing.T) {
	assertDescriptorSetCoversTheContractTree(t)

	fields, census := collectSecretSurfaceFields()
	findings, stale, exCensus := secretSurfaceVerdict(fields, secretSurfaceExclusions)
	census.excluded = exCensus

	t.Logf("осмотрено: сообщений %d · полей %d · строковых/байтовых %d · уникальных имён %d · "+
		"помеченных %d · совпавших с образцом %d (из них курсор %d) · записей ведомости %d",
		census.messages, census.fields, census.stringFields, census.uniqueNames,
		census.marked, census.matched, census.cursor, len(secretSurfaceExclusions))

	// ПРЕДПОСЫЛКИ. Ноль здесь — слепота предиката, а не благополучие дерева.
	if census.stringFields == 0 {
		t.Fatal("строковых полей НОЛЬ — набор дескрипторов пуст, судить нечего")
	}
	if census.marked == 0 {
		t.Fatal("помеченных полей НОЛЬ — производитель входа исчез: либо опцию сняли со " +
			"всех полей, либо чтение расширения сломано. Ось 1 в этом состоянии тоже " +
			"отрабатывает на пустом множестве")
	}
	if census.matched == 0 {
		t.Fatal("совпавших с образцом полей НОЛЬ — перечень образцов ослеп; ось отработала " +
			"на пустом множестве и покраснеть не могла")
	}
	// Структурный вычет — тоже исключение, и он тоже обязан иметь предмет.
	if census.cursor == 0 {
		t.Fatal("структурный вычет курсора постраничного чтения снял НОЛЬ полей — конвенция " +
			"платформы сменилась либо имена разошлись. Вычет без предмета наследует слепую " +
			"зону следующему: пересмотрите его")
	}

	if len(stale) > 0 {
		sort.Strings(stale)
		t.Errorf("записи ведомости ПОТЕРЯЛИ ПРЕДМЕТ: %v.\n"+
			"Поле снято, переименовано либо помечено — прощать нечего. Снимите запись: "+
			"иначе она переживёт свой предмет и унаследует слепую зону следующему.", stale)
	}
	if len(findings) > 0 {
		sort.Strings(findings)
		t.Fatalf("поля поверхности, чьё имя выглядит секретом, без пометки и без записи "+
			"ведомости: %v.\n"+
			"Носитель закрывается ПОМЕТКОЙ `(kacho.cloud.api.secret_bearing) = true` и ничем "+
			"иным; поле, секретом не являющееся, — записью ведомости С ПРИЧИНОЙ "+
			"(§4.3.3 приёмки BAT-1).", findings)
	}
}

// secretSurfaceVerdict — суждение, отделённое от обхода: инъекция подаёт сюда
// поля, собранные ею самой, и получает те же находки тем же кодом.
func secretSurfaceVerdict(
	fields []secretSurfaceField,
	ledger []secretSurfaceExclusion,
) (findings, stale []string, excluded int) {
	forgiven := map[string]secretSurfaceExclusion{}
	for _, e := range ledger {
		forgiven[e.id()] = e
	}
	used := map[string]bool{}

	for _, f := range fields {
		if f.pattern == "" || f.cursor {
			continue
		}
		e, listed := forgiven[f.id()]
		switch {
		case f.marked && listed:
			// ПРОТИВОРЕЧИЕ: одно из двух объявлений ложно.
			//
			// Это И ЕСТЬ форма самоистечения «поле помечено», но названная
			// точнее. Отдельной строкой «запись просрочена» она НЕ дублируется:
			// два отчёта об одном предмете заставили бы чинить дважды и
			// подсказали бы ровно один из двух исходов, тогда как гейт
			// различить их не может. Поэтому исходы названы ОБА — в самой
			// находке.
			used[f.id()] = true
			findings = append(findings, fmt.Sprintf(
				"%s — ОДНОВРЕМЕННО помечено носителем и названо в ведомости (%q). "+
					"Одно из двух объявлений ложно: поле — носитель ⇒ снимите ЗАПИСЬ "+
					"(носитель закрывается пометкой и ничем иным); поле носителем не "+
					"является ⇒ снимите ПОМЕТКУ", f.id(), e.why))
		case f.marked:
			// Носитель закрыт пометкой — так и должно быть.
		case listed:
			used[f.id()] = true
			excluded++
		default:
			findings = append(findings, fmt.Sprintf("%s (образец %s)", f.id(), f.pattern))
		}
	}

	for _, e := range ledger {
		if !used[e.id()] {
			stale = append(stale, fmt.Sprintf("%s (%s)", e.id(), e.why))
		}
	}
	return findings, stale, excluded
}

// collectSecretSurfaceFields — поля НАШИХ контрактов, какими их видят
// дескрипторы.
//
// Отбор идёт по ОБЪЯВЛЕННОМУ множеству корней, а не по литералу `kacho/`.
// Литерал молча выбросил бы из обхода весь контракт, переехавший под второй
// корень (KAN-PKG-1), — а помеченные поля живут именно там: перепись напечатала
// бы «помеченных 0», и ось 1 в этом состоянии тоже отработала бы на пустом
// множестве. Поймал это самопроверочный отказ ниже, а не находка.
func collectSecretSurfaceFields() (out []secretSurfaceField, census secretSurfaceCensus) {
	names := map[string]bool{}
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if !underDeclaredContractRoot(string(fd.Path())) {
			return true
		}
		var walk func(ms protoreflect.MessageDescriptors)
		walk = func(ms protoreflect.MessageDescriptors) {
			for i := 0; i < ms.Len(); i++ {
				md := ms.Get(i)
				census.messages++
				fl := md.Fields()
				for j := 0; j < fl.Len(); j++ {
					f := fl.Get(j)
					census.fields++
					// Секрет живёт в строке либо в байтах. Число, отметка
					// времени и вложенное сообщение носителем не бывают, и
					// расширять предмет ради полноты значило бы разбавлять
					// перепись величинами, к делу не относящимися.
					if f.Kind() != protoreflect.StringKind && f.Kind() != protoreflect.BytesKind {
						continue
					}
					census.stringFields++
					name := string(f.Name())
					names[name] = true

					rec := secretSurfaceField{
						message: string(md.FullName()),
						name:    name,
						marked:  secretBearingMarked(f),
						cursor:  paginationCursorNames[name],
					}
					for _, p := range secretNamePatterns {
						if p.match(name) {
							rec.pattern = p.name
							break
						}
					}
					if rec.marked {
						census.marked++
					}
					if rec.pattern != "" {
						census.matched++
						if rec.cursor {
							census.cursor++
						}
					}
					out = append(out, rec)
				}
				walk(md.Messages())
			}
		}
		walk(fd.Messages())
		return true
	})
	census.uniqueNames = len(names)
	sort.Slice(out, func(i, j int) bool { return out[i].id() < out[j].id() })
	return out, census
}

// secretBearingMarked читает САМУ ОПЦИЮ расширения, а не совпадение имени: имя —
// это ось 2, и она про другое.
func secretBearingMarked(f protoreflect.FieldDescriptor) bool {
	v, ok := proto.GetExtension(f.Options(), apiv1.E_SecretBearing).(bool)
	return ok && v
}

// assertDescriptorSetCoversTheContractTree — ПРОВЕРКА СОБСТВЕННОЙ ПРЕДПОСЫЛКИ.
//
// Ось судит по дескрипторам, а в набор попадает только то, что слинковано в этот
// прогон. Контракт, лежащий на диске и в набор не попавший, — слепая зона, и
// «находок ноль» о нём означает «не читали». Новый домен, чьи стабы никто здесь
// не импортирует, роняет ось по имени файла — то есть предпосылка истекает сама.
func assertDescriptorSetCoversTheContractTree(t *testing.T) {
	t.Helper()
	inSet := map[string]bool{}
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		inSet[string(fd.Path())] = true
		return true
	})

	// Состав дерева берётся у ИНДЕКСА, а не с диска: под `proto/` на машине, где
	// собирали образы, лежат распаковки и отчёты, а вердикт обязан быть свойством
	// коммита, а не диска. Этого требует гейт обходов дерева
	// (`TestTreeWalkersAskTheIndex`), и он же поймал первую редакцию этой
	// предпосылки — она ходила `filepath.Walk`.
	root := filepath.Join(repoRoot(t), "proto")
	// Обход идёт по ОБЪЯВЛЕННЫМ корням, а не по литералу: домен, переехавший под
	// второй корень, выпал бы из популяции, и гейт напечатал бы «помеченных 0» —
	// молчание, неотличимое от «опцию сняли со всех полей».
	var onDiskPaths []string
	var err error
	for _, r := range contractroot.Roots {
		sub, serr := treecorpus.UnderWithSuffix(filepath.Join(root, r), ".proto")
		if serr != nil {
			err = serr
			break
		}
		onDiskPaths = append(onDiskPaths, sub...)
	}
	if err != nil {
		t.Fatalf("состав дерева контрактов: %v", err)
	}
	var onDisk, missing []string
	for _, p := range onDiskPaths {
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			t.Fatalf("относительный путь %s: %v", p, rerr)
		}
		rel = filepath.ToSlash(rel)
		onDisk = append(onDisk, rel)
		if !inSet[rel] {
			missing = append(missing, rel)
		}
	}
	if len(onDisk) == 0 {
		t.Fatal("контрактов в индексе НОЛЬ — предпосылка проверяется на пустом дереве")
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("контракты дерева НЕ ПОПАЛИ в набор дескрипторов этого прогона: %v.\n"+
			"Ось судит по дескрипторам; о таком контракте она сказала бы «находок ноль», "+
			"не прочитав его. Провяжите импорт его стабов в этот пакет.", missing)
	}
	t.Logf("предпосылка: контрактов в индексе %d, все в наборе дескрипторов (в наборе всего %d)",
		len(onDisk), len(inSet))
}

// underDeclaredContractRoot — лежит ли файл дескриптора под одним из объявленных
// корней дерева контрактов.
func underDeclaredContractRoot(path string) bool {
	for _, r := range contractroot.Roots {
		if strings.HasPrefix(path, r+"/") {
			return true
		}
	}
	return false
}
