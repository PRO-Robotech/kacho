// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// latentmarkerspelling_test.go — написание пометки спящего отношения ОДНО на
// всё дерево: производители, распознаватели и проза о ней обязаны совпадать.
//
// # Предмет и почему он завёлся
//
// Пометка `# kacho:latent` объявляет, что у структурного отношения производителя
// нет и это РЕШЕНИЕ, а не пропуск. Читают её три места, и они лежат в РАЗНЫХ
// Go-модулях:
//
//   - `internal/repohygiene/modelrelationproducer_test.go` — модуль платформы
//     (`github.com/PRO-Robotech/kacho`), константа `latentMarker`;
//   - `services/vpc/internal/subscriptionjournal/exclusion_ground_test.go` —
//     модуль платформы, строковый литерал в вызове;
//   - `internal/authzmap/verb_type_materializable_test.go` РЕПОЗИТОРИЯ
//     `PRO-Robotech/kaname` — модуль службы доступа
//     (`github.com/PRO-Robotech/kaname`), константа `latentTypeMarker`.
//
// Производителей тоже три файла: обе байт-идентичные копии модели прав
// (`kaname/cloud/iam/v1/fga_model.fga` — дерево контрактов, и
// `internal/authzmodel/fga_model.fga` репозитория `PRO-Robotech/kaname`) и
// `services/vpc/manifest.yaml`.
//
// # Где эти файлы лежат ПОСЛЕ выноса службы, и что из этого следует для гейта
//
// Решением владельца kacho#2616 (исход C, 2026-09-13) служба доступа и её
// контракты уехали в свой репозиторий; прежняя редакция этой шапки называла их
// координатами ЭТОГО дерева (`services/iam/…`, `proto/kaname/…`) — утверждение
// ОТМЕНЕНО. Раскладка сегодня такая:
//
//   - в индексе этого дерева — два распознавателя и один производитель
//     (`services/vpc/manifest.yaml`): 6 вхождений в 5 файлах, все `kacho`;
//   - в дереве КОНТРАКТОВ, приезжающем модулем `github.com/PRO-Robotech/kaname`
//     и резолвимом через `internal/contractsource`, — канон модели: 2 вхождения,
//     тоже `kacho`. Этот корпус гейт читает, и ниже названа отдельная величина;
//   - ВНЕ ПОПУЛЯЦИИ гейта — внутренности службы (`internal/authzmodel/…`,
//     `internal/authzmap/…` того же репозитория): 5 вхождений. Их этот гейт НЕ
//     ВИДИТ, и односторонний переезд написания ТАМ он не покажет. Это граница
//     владения: `contractsource` резолвит дерево контрактов, а не внутренности
//     чужого продукта, и читать их платформе незачем. Замер оттуда получают
//     командой над каталогом модуля:
//     `grep -rhoE '#[ \t]*[a-z][a-z0-9_.-]*:latent' "$(go list -m -f '{{.Dir}}' github.com/PRO-Robotech/kaname)" | sort | uniq -c`
//     → `7 kacho:latent` на 2026-09-13 (те же 2 контрактных плюс 5
//     внутренних). Остаток назван числом, а не умолчан.
//
// # Чем опасен ОДНОСТОРОННИЙ переезд
//
// Распознаватель, разошедшийся с производителем, не краснеет — он ЗАМОЛКАЕТ:
// пометку перестают видеть, и помеченное отношение либо становится находкой не
// по делу, либо (там, где тип вне объёма гейта) уходит из наблюдения целиком.
// Отличить это от исправной работы нельзя ничем: обход цел, перепись растёт,
// вердикт зелёный. Класс — `testing.md` §«Гейт на класс», п. 9.
//
// # Почему пометка НЕ переезжает на имя службы (задача продукта #2557)
//
// Разрез #2076 предполагал, что пометка — координата службы доступа и обязана
// переехать вместе с её распознавателем. Замер говорит обратное, и он
// повторяется одной командой:
//
//	sed -n '1084,1088p' "$(go list -m -f '{{.Dir}}' github.com/PRO-Robotech/kaname)/proto/kaname/cloud/iam/v1/fga_model.fga"
//
// Путь назван через каталог МОДУЛЯ, а не как `proto/kaname/…` этого дерева:
// после kacho#2616 такого файла здесь нет, и прежняя команда давала ТРЕТИЙ
// исход — «не выполнилось», — то есть замер, на который ссылается решение,
// перестал быть воспроизводимым.
//
// Пометка стоит на типе `vpc_address_pool` — РЕСУРСЕ ДОМЕНА VPC, а сама модель
// требует её «on a consumer-owned type». Второй производитель — манифест vpc.
// Два распознавателя из трёх живут в модуле платформы. По критерию разреза
// («поставь службу доступа одну — кто заведёт объект, который имя называет»)
// объект заводит ПЛАТФОРМА: без неё нет ни vpc, ни пула адресов, ни пометки.
// Поэтому написание остаётся, а этот гейт стережёт его от расщепления.
package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/contractsource"
)

// latentSpellingRe — пометка в ЛЮБОЙ из форм, которыми дерево её записывает.
//
// Формы перечислены ОБХОДОМ дерева, а не по памяти (`git grep -ohE '.{0,14}:latent'`),
// и их шесть: отступной комментарий модели · та же пометка в обратных кавычках
// внутри комментария модели · объявление константы Go · строковый литерал в
// вызове Go · русская проза в комментарии Go · отступной комментарий манифеста.
// Все шесть накрываются одним выражением, потому что различает их окружение, а
// не сама пометка.
var latentSpellingRe = regexp.MustCompile(`#[ \t]*([a-z][a-z0-9_.-]*):latent`)

// latentHit — одно вхождение пометки.
type latentHit struct {
	File     string
	Line     int
	Spelling string
}

// collectLatentSpellings обходит СОСТАВ дерева и собирает каждое вхождение
// пометки. Возвращает находки и перепись: файлов состава прочитано, файлов с
// пометкой, вхождений — чтобы «ноль находок» было отличимо от «ноль
// прочитанного».
func collectLatentSpellings(t *testing.T, tt *trackedTree) ([]latentHit, int, int) {
	t.Helper()

	rels := make([]string, 0, len(tt.files))
	for rel := range tt.files {
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	var hits []latentHit
	filesWith := 0
	for _, rel := range rels {
		body, err := os.ReadFile(filepath.Join(tt.root, rel))
		if err != nil {
			continue // нечитаемое (подмодуль, символьная ссылка) — не предмет
		}
		found := false
		for i, line := range strings.Split(string(body), "\n") {
			for _, m := range latentSpellingRe.FindAllStringSubmatch(line, -1) {
				hits = append(hits, latentHit{File: rel, Line: i + 1, Spelling: m[1]})
				found = true
			}
		}
		if found {
			filesWith++
		}
	}
	return hits, len(rels), filesWith
}

// latentSpellingVerdict — вердикт по собранным вхождениям. Пустая строка = сошлось.
// Вынесен из теста, чтобы инъекция подавала СВОИ вхождения и проверяла ту же
// функцию, которой судится настоящее дерево, а не её копию.
func latentSpellingVerdict(hits []latentHit, filesRead int) string {
	if filesRead == 0 {
		return "состав дерева пуст — гейт не прочитал НИ ОДНОГО файла, вердикт беспредметен"
	}
	if len(hits) == 0 {
		return fmt.Sprintf("прочитано %d файлов состава, пометка спящего отношения не найдена НИ РАЗУ. "+
			"Либо она снята вместе со своим предметом — тогда снимите и этот гейт вместе с его "+
			"инъекцией и распознавателями, — либо изменилась её форма записи, и гейт ослеп "+
			"(`testing.md` §«Гейт на класс», п. 7)", filesRead)
	}

	spellings := map[string][]latentHit{}
	for _, h := range hits {
		spellings[h.Spelling] = append(spellings[h.Spelling], h)
	}
	if len(spellings) == 1 {
		return ""
	}

	names := make([]string, 0, len(spellings))
	for s := range spellings {
		names = append(names, s)
	}
	sort.Strings(names)

	var b strings.Builder
	fmt.Fprintf(&b, "написаний пометки спящего отношения %d, обязано быть одно: %s.\n",
		len(spellings), strings.Join(names, ", "))
	b.WriteString("Односторонний переезд НЕ краснеет сам — он ЗАМОЛКАЕТ: распознаватель " +
		"перестаёт видеть пометку, и помеченное уходит из наблюдения при зелёном вердикте.\n")
	for _, s := range names {
		fmt.Fprintf(&b, "  написание %q — %d вх.:\n", s, len(spellings[s]))
		for _, h := range spellings[s] {
			fmt.Fprintf(&b, "    %s:%d\n", h.File, h.Line)
		}
	}
	b.WriteString("Исходов два: свести все стороны к одному написанию ОДНИМ изменением, " +
		"либо снять пометку вместе с её предметом. Оставить как есть — не исход.")
	return b.String()
}

// latentCensus — перепись строкой: печатается всегда, чтобы «ноль находок» было
// отличимо от «ноль прочитанного».
func latentCensus(hits []latentHit, filesRead, filesWith int) string {
	byExt := map[string]int{}
	for _, h := range hits {
		ext := filepath.Ext(h.File)
		if ext == "" {
			ext = "(без расширения)"
		}
		byExt[ext]++
	}
	exts := make([]string, 0, len(byExt))
	for e := range byExt {
		exts = append(exts, e)
	}
	sort.Strings(exts)
	parts := make([]string, 0, len(exts))
	for _, e := range exts {
		parts = append(parts, fmt.Sprintf("%s %d", e, byExt[e]))
	}
	return fmt.Sprintf("файлов состава прочитано %d · файлов с пометкой %d · вхождений %d · по расширениям: %s",
		filesRead, filesWith, len(hits), strings.Join(parts, ", "))
}

// collectLatentSpellingsInExternalContracts — вхождения пометки в деревьях
// контрактов, ЧЬИХ ИСХОДНИКОВ В ЭТОМ ДЕРЕВЕ НЕТ.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЗАЧЕМ ОТДЕЛЬНЫЙ СБОР, А НЕ РАСШИРЕНИЕ ОБХОДА ИНДЕКСА
//
// Канон модели прав — один из трёх производителей пометки, и после kacho#2616
// (исход C) он лежит вне индекса git этого дерева: приезжает модулем
// `github.com/PRO-Robotech/kaname`, координата резолвится
// `internal/contractsource`. Обход индекса его не видит и НЕ КРАСНЕЕТ — он
// перестаёт читать целый вид производителя, честно печатая перепись по тому, что
// прочитал. Ровно тот класс, который этот гейт и стережёт, только в применении к
// самому гейту.
//
// Берутся ТОЛЬКО внешние корни (`contractsource.ExternalRootModules`): корни,
// лежащие в `proto/` этого дерева, обход индекса уже прочитал, и второй проход
// удвоил бы их вхождения в переписи.
//
// Пустой состав внешнего корня — ОТКАЗ, и его даёт сам `contractsource.Files`.
func collectLatentSpellingsInExternalContracts(t *testing.T, root string) (hits []latentHit, filesRead int) {
	t.Helper()
	roots := make([]string, 0, len(contractsource.ExternalRootModules))
	for r := range contractsource.ExternalRootModules {
		roots = append(roots, r)
	}
	sort.Strings(roots)
	if len(roots) == 0 {
		t.Fatal("внешних корней дерева контрактов объявлено НОЛЬ — либо их больше нет " +
			"(тогда снимите этот сбор вместе с его предметом), либо перечень опустел, " +
			"и канон модели ушёл из наблюдения молча")
	}
	for _, r := range roots {
		dir, err := contractsource.Dir(root, r)
		if err != nil {
			t.Fatalf("каталог дерева контрактов корня %q: %v", r, err)
		}
		files, err := contractsource.Files(root, r)
		if err != nil {
			t.Fatalf("состав дерева контрактов корня %q: %v", r, err)
		}
		base := filepath.Dir(dir)
		for _, abs := range files {
			rel, rerr := filepath.Rel(base, abs)
			if rerr != nil {
				t.Fatalf("путь %s относительно %s: %v", abs, base, rerr)
			}
			body, rerr := os.ReadFile(abs) // #nosec G304 -- путь из кэша модулей
			if rerr != nil {
				t.Fatalf("чтение %s: %v", abs, rerr)
			}
			filesRead++
			coord := "proto/" + filepath.ToSlash(rel) + " [модуль " + contractsource.ExternalRootModules[r] + "]"
			for i, line := range strings.Split(string(body), "\n") {
				for _, m := range latentSpellingRe.FindAllStringSubmatch(line, -1) {
					hits = append(hits, latentHit{File: coord, Line: i + 1, Spelling: m[1]})
				}
			}
		}
	}
	return hits, filesRead
}

// TestLatentMarkerSpellingIsSingleValued — написаний ровно одно.
//
// Популяция СОСТАВНАЯ, и части названы порознь: индекс этого дерева плюс деревья
// контрактов, приезжающие модулем. Одно число по их сумме скрыло бы ровно тот
// случай, ради которого гейт заведён, — сторону, ушедшую из наблюдения целиком.
func TestLatentMarkerSpellingIsSingleValued(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	tt := newTrackedTree(t, root)
	hits, filesRead, filesWith := collectLatentSpellings(t, tt)
	t.Logf("перепись индекса дерева: %s", latentCensus(hits, filesRead, filesWith))

	extHits, extFiles := collectLatentSpellingsInExternalContracts(t, root)
	t.Logf("перепись внешних деревьев контрактов: файлов прочитано %d · вхождений %d",
		extFiles, len(extHits))
	if len(extHits) == 0 {
		t.Errorf("во внешних деревьях контрактов прочитано %d файлов и не найдено НИ ОДНОГО "+
			"вхождения пометки — а канон модели прав есть её ПЕРВЫЙ производитель. Ноль здесь "+
			"означает одно из двух, и оба суть находка: пометку сняли в чужом репозитории, "+
			"оставив производителей и распознавателей этого дерева утверждать то, чего канон "+
			"не объявляет; либо закреплённая версия модуля перестала нести канон", extFiles)
	}

	if v := latentSpellingVerdict(append(hits, extHits...), filesRead+extFiles); v != "" {
		t.Error(v)
	}
}
