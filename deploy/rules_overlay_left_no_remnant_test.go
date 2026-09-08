// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

// rules_overlay_left_no_remnant_test.go — МЕХАНИЗМ НЕ ПЕРЕЖИВАЕТ СВОЕГО
// ПОТРЕБИТЕЛЯ: наложение правил снято со стороны службы, значит его нет и в
// поставке.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Служба объявляет в собственном исходнике, что решение о доступе принимает
// ЕДИНСТВЕННЫЙ механизм — модель прав, — а наложение правил и провязка пакета
// сняты. Поставка при этом продолжала возить пять файлов правил, шаблоны
// бокового контейнера и его карт, ручки в профилях и сетевые политики, чей
// предмет — «выдача пакета правил» и «поды с боковым контейнером».
//
// Обещание арендатору и оператору («правила применяются») не имело исполнителя.
// Класс описан в корпусе: контроль, присутствующий и не исполняющийся, ХУЖЕ
// отсутствующего — отсутствующий однажды заведут, а этот считают действующим.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО БЫЛО ИЗМЕРЕНО, А НЕ ПРЕДПОЛОЖЕНО — И ГДЕ ПРИЗНАК ЗАДАЧИ ОКАЗАЛСЯ ШИРЕ ФАКТА
//
// Задача утверждала, что боевой профиль ВКЛЮЧАЕТ боковой контейнер. Замер по
// рендеру четырёх стендов: контейнера с этим именем нет НИ В ОДНОМ развёртывании
// — ручка выключена и умолчанием чарта, и каждым профилем. То есть ресурса он не
// занимал и никуда не ходил.
//
// Что при этом было верно и оказалось дороже: на боевом стенде рендерились ДВЕ
// сетевые политики мёртвого механизма. Одна выбирает поды по метке, которую при
// выключенной ручке не несёт НИ ОДИН под, — политика без предмета. Вторая
// выбирает поды службы и объявляет им единственный впускной порт, тогда как под
// служит семью, и другой политики, выбирающей эти поды, в рендере НЕТ. Комментарий
// рядом утверждал обратное («публичный порт ведёт отдельная политика подчарта») —
// два места об одном предмете, из которых верно то, у которого есть читатель.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ГЕЙТ ЧИТАЕТ ПРЕДПОСЫЛКУ, А НЕ ТОЛЬКО ОСТАТКИ
//
// Запрет обоснован фактом о ДРУГОЙ половине дерева: у наложения правил нет
// потребителя. Факт может измениться — механизм вправе вернуться вместе со своим
// читателем, и тогда поставка обязана его возить. Гейт, знающий только про
// остатки, в этот день начал бы требовать снятия того, что вернули осознанно.
//
// Поэтому проверка сначала спрашивает предпосылку и, если та изменилась, ОТКАЗЫВАЕТ
// с другим текстом: «потребитель вернулся — этот гейт обязан быть пересмотрен», а
// не «в поставке остатки». Послабление истекает от появления предмета, а не от
// чьей-то памяти.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ЭТА ПРОВЕРКА НЕ ДЕЛАЕТ — сказано прямо
//
//   - она НЕ судит соседние службы: их собственные чарты и их наложения — не её
//     предмет, и вердикт о них она не выносит;
//   - она НЕ судит рендер: ей довольно объявлений, а рендер требует собранных
//     зависимостей и умеет ПРОПУСТИТЬСЯ, а пропустившаяся проверка неотличима от
//     прошедшей;
//   - она НЕ судит прозу документации о прошлом: предметом являются ФАЙЛЫ
//     поставки, ключи значений и метки шаблонов, а не воспоминание о механизме.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// rulesOverlayRoots — что считается ПОСТАВКОЙ этой службы. Соседние чарты сюда
// не входят намеренно.
var rulesOverlayRoots = []string{
	filepath.Join(umbrellaDir, "charts", "kaname"),
	filepath.Join(umbrellaDir, "templates"),
	filepath.Join("..", "services", "iam", "deploy"),
}

// rulesOverlayValuesFiles — файлы значений, объявляющие ручки поставки.
func rulesOverlayValuesFiles(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(umbrellaDir)
	require.NoError(t, err)
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "values") || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		out = append(out, filepath.Join(umbrellaDir, e.Name()))
	}
	out = append(out, filepath.Join(umbrellaDir, "charts", "kaname", "values.yaml"))
	sort.Strings(out)
	return out
}

// rulesOverlayCensus — объём осмотренного. Печатается всегда.
type rulesOverlayCensus struct {
	filesWalked    int // файлов поставки прочитано
	valuesRead     int // файлов значений разобрано
	consumerFiles  int // прод-файлов службы осмотрено на предмет потребителя
	consumerHits   int // из них зовущих вердикт наложения
	policyArtifact int // файлов правил в поставке
	deadTemplates  int // шаблонов мёртвого механизма
	deadKeys       int // ключей значений мёртвого механизма
	deadLabels     int // упоминаний метки мёртвого механизма в шаблонах
}

// rulesOverlayFinding — одна находка с координатой.
type rulesOverlayFinding struct{ path, what string }

// hasRulesOverlayKey ищет ключ ручки в РАЗОБРАННОМ дереве значений: ключ
// встречается и в комментариях, и проверка по подстроке краснела бы на
// собственном объяснении.
func hasRulesOverlayKey(node any, path []string, key string, out *[]string) {
	switch v := node.(type) {
	case map[string]any:
		names := make([]string, 0, len(v))
		for k := range v {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, k := range names {
			if k == key {
				*out = append(*out, strings.Join(append(path, k), "."))
			}
			hasRulesOverlayKey(v[k], append(path, k), key, out)
		}
	case []any:
		for i, item := range v {
			hasRulesOverlayKey(item, append(path, fmt.Sprintf("[%d]", i)), key, out)
		}
	}
}

// executableLines отдаёт строки без комментариев шаблона и YAML: распознаватель
// обязан судить исполняемую часть, а не прозу о ней.
func executableLines(text string) []string {
	var out []string
	inTplComment := false
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "{{/*") {
			inTplComment = true
		}
		if inTplComment {
			if strings.Contains(trimmed, "*/}}") {
				inTplComment = false
			}
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		out = append(out, line)
	}
	return out
}

// TestRulesOverlayLeftNoRemnantInTheDelivery — гейт класса.
func TestRulesOverlayLeftNoRemnantInTheDelivery(t *testing.T) {
	var (
		census   rulesOverlayCensus
		findings []rulesOverlayFinding
	)

	// ── ПРЕДПОСЫЛКА: у наложения правил нет потребителя ───────────────────
	//
	// Признак потребителя — обращение к вердикту наложения по его адресу
	// (`/v1/data/…`). Это то, что делает КОД, а не то, о чём он вспоминает,
	// поэтому комментарии из осмотра исключены.
	serviceRoot := filepath.Join("..", "services", "iam")
	err := filepath.WalkDir(serviceRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		census.consumerFiles++
		for _, line := range executableLines(string(raw)) {
			if strings.Contains(line, "/v1/data/") {
				census.consumerHits++
				break
			}
		}
		return nil
	})
	require.NoError(t, err)
	require.NotZero(t, census.consumerFiles,
		"прод-файлов службы не прочитано ни одного — предпосылка не измерена, "+
			"а вердикт был бы беспредметен")

	// ── ОСТАТКИ В ПОСТАВКЕ ────────────────────────────────────────────────
	for _, root := range rulesOverlayRoots {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			census.filesWalked++
			base := filepath.Base(path)
			switch {
			case strings.Contains(filepath.ToSlash(path), "/opa-policies/"):
				census.policyArtifact++
				findings = append(findings, rulesOverlayFinding{path,
					"файл правил в поставке: обещание, что правила применяются, — применять их некому"})
				return nil
			case strings.HasPrefix(base, "opa-") && strings.HasSuffix(base, ".yaml"):
				census.deadTemplates++
				findings = append(findings, rulesOverlayFinding{path,
					"шаблон мёртвого механизма"})
				return nil
			case base == "networkpolicy-authz.yaml":
				census.deadTemplates++
				findings = append(findings, rulesOverlayFinding{path,
					"сетевая политика, чей предмет — выдача пакета правил и поды с боковым " +
						"контейнером: ни того, ни другого не существует"})
				return nil
			}
			if !strings.HasSuffix(base, ".yaml") && !strings.HasSuffix(base, ".tpl") {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, line := range executableLines(string(raw)) {
				if strings.Contains(line, "kacho.cloud/opa-sidecar") {
					census.deadLabels++
					findings = append(findings, rulesOverlayFinding{path,
						"метка «в этом поде есть боковой контейнер правил» — контейнера нет ни на одной посадке"})
					break
				}
			}
			return nil
		})
		require.NoError(t, err)
	}

	// ── ОСТАТКИ В ЗНАЧЕНИЯХ: ключ РАЗОБРАННОГО дерева, не подстрока ───────
	for _, vf := range rulesOverlayValuesFiles(t) {
		census.valuesRead++
		var paths []string
		hasRulesOverlayKey(readYAML(t, vf), nil, "opaSidecar", &paths)
		for _, p := range paths {
			census.deadKeys++
			findings = append(findings, rulesOverlayFinding{vf,
				fmt.Sprintf("ручка мёртвого механизма: %s", p)})
		}
	}

	t.Logf("перепись: файлов поставки %d · файлов значений %d · прод-файлов службы %d "+
		"(зовущих вердикт наложения %d) · остатков: файлов правил %d, шаблонов %d, "+
		"ключей значений %d, меток %d",
		census.filesWalked, census.valuesRead, census.consumerFiles, census.consumerHits,
		census.policyArtifact, census.deadTemplates, census.deadKeys, census.deadLabels)

	require.NotZero(t, census.filesWalked, "обход поставки пуст — вердикт беспредметен")
	require.NotZero(t, census.valuesRead, "файлов значений не прочитано — вердикт беспредметен")

	// Предпосылка изменилась — это ДРУГОЙ отказ, и текст у него другой.
	if census.consumerHits > 0 {
		t.Fatalf("предпосылка гейта изменилась: у наложения правил снова есть потребитель "+
			"(%d прод-файлов службы обращаются к вердикту по его адресу). Этот гейт требует "+
			"отсутствия механизма в поставке и обязан быть ПЕРЕСМОТРЕН вместе с решением "+
			"вернуть потребителя — а не обойдён", census.consumerHits)
	}

	if len(findings) > 0 {
		sort.Slice(findings, func(i, j int) bool { return findings[i].path < findings[j].path })
		lines := make([]string, 0, len(findings))
		for _, f := range findings {
			lines = append(lines, fmt.Sprintf("%s — %s", f.path, f.what))
		}
		t.Fatalf("механизм пережил своего потребителя — %d остатков в поставке:\n  %s\n\n"+
			"исходов два: снять вместе с потребителем ЛИБО вернуть потребителя и закрепить "+
			"его пробой, утверждающей ИСХОД обращения. «Оставить как есть» исходом не является",
			len(findings), strings.Join(lines, "\n  "))
	}
}
