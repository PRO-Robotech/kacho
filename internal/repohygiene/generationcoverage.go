// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// generationCoverage.go — контракт, который объявление генерации не назвало,
// не порождает ничего, и оба молчат зелёным.
//
// # Предмет
//
// Входы генерации объявлены ПЕРЕЧНЕМ, и перечень закрыт:
//
//	inputs:
//	  - directory: .
//	    paths:
//	      - kacho
//
// Контракт, лежащий вне названного, buf не компилирует, стабов не эмитит и
// выходит УСПЕХОМ, не напечатав ни строки. То есть отсутствие во входах — не
// отказ, а молчание.
//
// # Почему это не ловит сверка порождённого с деревом
//
// Замерено на синтетике (одно-фактная инъекция: менялся только перечень
// `paths`; формы сверки воспроизведены дословно). У сверки в дереве ДВЕ
// реализации, и они видят разное:
//
//	состояние                                     форма CI   форма локальная
//	                                          (rm -rf+diff)  (status --porcelain)
//	корень имел закоммиченные стабы и выпал      КРАСНОЕ         зелёное
//	корень НОВЫЙ, стабов не было никогда         зелёное         зелёное
//
// Вторая строка и есть живой случай — первый переезд домена в собственный
// корень контрактов: стабов у него нет, сносить нечего, порождать некому,
// сравнивать не с чем. Обе формы зелены, и ни одна не солгала: они сверяют
// ИЗМЕНЁННОЕ и УДАЛЁННОЕ, а предмет здесь — НЕПОРОЖДЁННОЕ, которого в выходе
// не было и не появилось.
//
// # Единица счёта — ФАЙЛ, а не корень
//
// Требование задачи сформулировано про корни, и по корням печатается перепись.
// Но судить только их значило бы объявить «ноль находок» шире измеренного:
// путь во входах бывает глубже корня, и тогда корень назван, а часть его
// контрактов по-прежнему не порождается. Поэтому покрытие считается по
// КАЖДОМУ отслеживаемому контракту, а корни остаются единицей ОТЧЁТА.
//
// # Чужое отделяется тем, куда оно порождает
//
// Дерево контрактов несёт и вендорное (`google/api`, `google/rpc`): его стабы
// приходят готовыми модулями Go, и во входах генерации ему делать нечего.
// Отделяется оно НЕ именем каталога и не записью в конфигурации, а собственным
// объявлением `go_package`: контракт, порождающий в ОДИН ИЗ модулей этого
// дерева, обязан быть во входах; контракт, порождающий наружу, — нет.
//
// Модулей в дереве больше одного, и множество ВЫВОДИТСЯ из индекса git, а не
// выписывается: рукописный перечень разошёлся бы с деревом молча — ровно в тот
// день, когда служба получает собственный модуль.
//
// Дискриминатор выбран содержательный, а не путевой, намеренно. Запись в
// конфигурации правит тот же человек, который забыл вход, — и «починить»
// красное можно было бы дописав каталог в игнор. Объявить контракт чужим так
// же дёшево не выйдет: у корня с потребителями смена `go_package` немедленно
// рвёт сборку каждому импортёру, у нового корня стабов не появится вовсе — и
// сборка сломается у первого, кто их позовёт. Отсрочка есть, безнаказанности
// нет.
//
// Контракт БЕЗ `go_package` считается нашим (fail-closed): молчание объявления
// не есть заявление о чужом происхождении.
//
// # Обе стороны, и вторая — самоистечение
//
// Корень без входа — находка. Вход, которому в дереве ничего не соответствует,
// — ТОЖЕ находка: перечень, переживший свой предмет, ослепляет проверку на
// целом пути и не истекает сам.
//
// # Граница названа
//
// Проверка судит ПОЛНОТУ ВХОДОВ, и только её. Она не утверждает, что
// порождённое совпадает с контрактами (это сверка генерации), не судит
// контракты вне каталога с объявлением генерации (анкер плагинов каталога прав
// живёт под `gateway/proto/` вне buf-модуля намеренно — причина в шапке самого
// файла) и не разбирает входы вида, которого не знает: такой вход — находка, а
// не тишина.

// generationContractFile — один контракт дерева, как его видит эта проверка.
//
// Rel — путь ОТНОСИТЕЛЬНО каталога с объявлением генерации: именно в этой
// системе координат buf сопоставляет `paths`/`exclude_paths`.
type generationContractFile struct {
	Rel  string
	Ours bool
}

// generationCoverageCensus — объём осмотренного. Печатается всегда: без него
// «ноль находок» неотличимо от «ноль прочитанного».
type generationCoverageCensus struct {
	Files        int // контрактов подано
	Ours         int // из них порождающих в один из модулей дерева
	Vendored     int // из них порождающих наружу
	Roots        int // корней первого уровня
	RootsOurs    int // корней, несущих хотя бы один наш контракт
	RootsCovered int // наших корней, покрытых входами полностью
	Inputs       int // входов в объявлении
	Judged       int // входов, которые разбор судит
}

// generationInputTypeKeys — виды входов buf v2. Разбор судит ровно один из них
// (каталог), остальные обязан НАЗВАТЬ, а не пропустить: вход, о котором
// проверка ничего не знает, — это не «покрыто», а «не судили».
var generationInputTypeKeys = []string{
	"module", "directory", "proto_file", "tarball", "zip_archive",
	"binary_image", "json_image", "txt_image", "yaml_image", "git_repo",
}

// generationDecl — то немногое из объявления генерации, что нужно этой проверке.
//
// Вход читается картой узлов, а не жёстким типом: у входа десяток законных
// видов, и жёсткий тип отверг бы девять из них ещё на разборе — то есть
// превратил бы «не судили» в «не разобрали».
type generationDecl struct {
	Inputs []map[string]yaml.Node `yaml:"inputs"`
}

// generationStringList — список строк из узла; одиночная строка даёт список из
// одного элемента.
func generationStringList(n yaml.Node) []string {
	switch n.Kind {
	case yaml.ScalarNode:
		if n.Value == "" {
			return nil
		}
		return []string{n.Value}
	case yaml.SequenceNode:
		var out []string
		for _, item := range n.Content {
			if item.Value != "" {
				out = append(out, item.Value)
			}
		}
		return out
	default:
		return nil
	}
}

// generationPathCovers — покрывает ли объявленный путь этот контракт.
//
// Семантика buf: путь либо называет файл дословно, либо является каталогом,
// внутри которого файл лежит. Префиксного сравнения строк недостаточно —
// `kacho` не покрывает `kachonaked/x.proto`, поэтому разделитель обязателен.
func generationPathCovers(declared, rel string) bool {
	declared = strings.Trim(declared, "/")
	if declared == "" || declared == "." {
		return true
	}
	return rel == declared || strings.HasPrefix(rel, declared+"/")
}

// generationRootOf — корень первого уровня, к которому относится контракт.
func generationRootOf(rel string) string {
	if i := strings.IndexByte(rel, '/'); i > 0 {
		return rel[:i]
	}
	return rel
}

// generationInputType — вид входа и признак «вид разбору известен».
func generationInputType(in map[string]yaml.Node) (string, bool) {
	for _, k := range generationInputTypeKeys {
		if _, ok := in[k]; ok {
			return k, true
		}
	}
	return "", false
}

// checkGenerationCoverage — находки одного объявления генерации.
//
// files — отслеживаемые контракты каталога, в котором лежит объявление.
// Пустой набор здесь ОТКАЗ, а не чистое дерево: судить не о чем, и молчание
// было бы «ноль прочитанного», выданным за «ноль находок».
func checkGenerationCoverage(declPath, raw string, files []generationContractFile) ([]string, generationCoverageCensus) {
	var census generationCoverageCensus

	if len(files) == 0 {
		return []string{declPath + ": под каталогом объявления не найдено ни одного " +
			"контракта — обход сломан, а не дерево чисто"}, census
	}

	var doc generationDecl
	if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
		return []string{declPath + ": не разобран YAML: " + err.Error() +
			" — объявление НЕ проверено"}, census
	}

	census.Files = len(files)
	roots := map[string]bool{}
	rootsOurs := map[string]bool{}
	for _, f := range files {
		roots[generationRootOf(f.Rel)] = true
		if f.Ours {
			census.Ours++
			rootsOurs[generationRootOf(f.Rel)] = true
		} else {
			census.Vendored++
		}
	}
	census.Roots = len(roots)
	census.RootsOurs = len(rootsOurs)
	census.Inputs = len(doc.Inputs)

	var findings []string

	// Покрытие. `inputs` отсутствует — законная форма buf: входом становится
	// сам модуль, то есть покрыто ВСЁ. Это не послабление, а его семантика.
	covered := make(map[string]bool, len(files))
	if len(doc.Inputs) == 0 {
		for _, f := range files {
			covered[f.Rel] = true
		}
	}

	for i, in := range doc.Inputs {
		where := fmt.Sprintf("%s: вход #%d", declPath, i+1)

		kind, known := generationInputType(in)
		if !known {
			findings = append(findings, where+" — вида, которого разбор не знает "+
				"(ключи: "+strings.Join(generationInputKeys(in), ", ")+"). О полноте "+
				"порождения по нему сказать нечего: научи разбор этому виду либо "+
				"приведи вход к каталогу")
			continue
		}
		if kind != "directory" {
			findings = append(findings, where+" — вида «"+kind+"», а разбор судит "+
				"только каталог. Пока вид не разобран, контракты, которые он "+
				"порождает, вне наблюдения — это «не судили», а не «покрыто»")
			continue
		}

		dirNode := in["directory"]
		dir := strings.Trim(dirNode.Value, "/")
		if dir != "" && dir != "." {
			findings = append(findings, where+" — каталог «"+dirNode.Value+"» не есть "+
				"корень модуля: пути этого входа отсчитываются от него, и разбор "+
				"свёл бы их к чужой системе координат. Вход вне наблюдения")
			continue
		}
		census.Judged++

		declared := generationStringList(in["paths"])
		excluded := generationStringList(in["exclude_paths"])

		// Самоистечение перечня: путь, которому в дереве ничего не
		// соответствует, ослепляет проверку и не истекает сам.
		for _, p := range declared {
			if !generationAnyMatch(p, files) {
				findings = append(findings, where+" — во `paths` назван путь «"+p+
					"», которому в дереве не соответствует НИ ОДИН контракт. "+
					"Перечень пережил свой предмет: снимите запись либо верните путь")
			}
		}
		for _, p := range excluded {
			if !generationAnyMatch(p, files) {
				findings = append(findings, where+" — в `exclude_paths` назван путь «"+p+
					"», которому в дереве не соответствует НИ ОДИН контракт. "+
					"Исключению нечего исключать — снимите запись")
			}
		}

		for _, f := range files {
			hit := len(declared) == 0
			for _, p := range declared {
				if generationPathCovers(p, f.Rel) {
					hit = true
					break
				}
			}
			for _, p := range excluded {
				if generationPathCovers(p, f.Rel) {
					hit = false
					break
				}
			}
			if hit {
				covered[f.Rel] = true
			}
		}
	}

	// Находка отчитывается по КОРНЮ: правка одна на корень, и перечень из
	// сотни путей читателя не двигает.
	uncovered := map[string][]string{}
	for _, f := range files {
		if f.Ours && !covered[f.Rel] {
			r := generationRootOf(f.Rel)
			uncovered[r] = append(uncovered[r], f.Rel)
		}
	}
	for r := range rootsOurs {
		if len(uncovered[r]) == 0 {
			census.RootsCovered++
		}
	}

	var rootNames []string
	for r := range uncovered {
		rootNames = append(rootNames, r)
	}
	sort.Strings(rootNames)
	for _, r := range rootNames {
		list := uncovered[r]
		sort.Strings(list)
		total := 0
		for _, f := range files {
			if generationRootOf(f.Rel) == r {
				total++
			}
		}
		findings = append(findings, fmt.Sprintf(
			"%s: корень контрактов «%s» не назван во входах генерации — %d из %d его "+
				"контрактов не порождают НИЧЕГО, и это молчание, а не отказ: buf выходит "+
				"успехом и не печатает ни строки, сверка порождённого сравнивает "+
				"несуществующее с несуществующим. Пример: %s. Исходов два: назвать корень "+
				"в `inputs.paths` либо объявить его чужим — `go_package` вне модулей этого "+
				"дерева, как у вендорного",
			declPath, r, len(list), total, list[0]))
	}

	sort.Strings(findings)
	return findings, census
}

// generationAnyMatch — соответствует ли объявленному пути хоть один контракт.
func generationAnyMatch(declared string, files []generationContractFile) bool {
	for _, f := range files {
		if generationPathCovers(declared, f.Rel) {
			return true
		}
	}
	return false
}

// generationInputKeys — ключи входа, отсортированные: попадают в текст находки,
// поэтому обязаны быть детерминированы.
func generationInputKeys(in map[string]yaml.Node) []string {
	out := make([]string, 0, len(in))
	for k := range in {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
