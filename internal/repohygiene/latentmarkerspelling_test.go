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
//   - `services/iam/internal/authzmap/verb_type_materializable_test.go` —
//     модуль службы доступа (`github.com/PRO-Robotech/kaname`), константа
//     `latentTypeMarker`.
//
// Производителей тоже три файла: обе байт-идентичные копии модели прав
// (`proto/kaname/cloud/iam/v1/fga_model.fga` и
// `services/iam/internal/authzmodel/fga_model.fga`) и `services/vpc/manifest.yaml`.
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
//	sed -n '1072,1075p' proto/kaname/cloud/iam/v1/fga_model.fga
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

// TestLatentMarkerSpellingIsSingleValued — написаний ровно одно.
func TestLatentMarkerSpellingIsSingleValued(t *testing.T) {
	t.Parallel()

	tt := newTrackedTree(t, repoRoot(t))
	hits, filesRead, filesWith := collectLatentSpellings(t, tt)
	t.Logf("перепись: %s", latentCensus(hits, filesRead, filesWith))

	if v := latentSpellingVerdict(hits, filesRead); v != "" {
		t.Error(v)
	}
}
