// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: Apache-2.0

package grpcsrv_test

// tls_guard_test.go — страж: TLS-креды межсервисного gRPC собираются в
// фундаменте ТОЛЬКО помощниками `grpcsrv.TLSServerCreds` /
// `grpcclient.TLSClientCreds`. Прямой `credentials.NewTLS(…)` или собранный
// руками `tls.Config` в прод-коде фундамента роняет этот страж.
//
// # Область — фундамент, и это сказано вслух
//
// Осматривается каталог `pkg/`. За его пределами тот же признак срабатывает на
// десяти файлах (предикат: `git grep -l -e 'credentials.NewTLS(' -e 'tls.Config{'
// -- '*.go' | grep -v '_test\.go$' | grep -vc '^pkg/'` → 10 на 6b1293713), из них
// два — упоминания в комментариях SDK, то есть настоящих точек сборки восемь.
// Расхождение названо намеренно: признак этого стража ТЕКСТОВЫЙ (подстрока по
// содержимому файла), поэтому он не отличает код от комментария и строкового
// литерала, и число, полученное им, читать как «столько мест собирают креды»
// нельзя. Распространение запрета за пределы фундамента — отдельная работа со
// своим разбором; молча расширить область значило бы получить десять находок
// разом и отключить страж первым же прогоном. Числа названы, чтобы «страж
// зелёный» не читалось шире, чем есть: он утверждает свойство ФУНДАМЕНТА, а не
// всего дерева.
//
// # Освобождение ключуется ПУТЁМ, а не базовым именем файла
//
// Прежняя редакция держала перечень освобождений с ключом `filepath.Base(path)`,
// то есть выдавала освобождение по ИМЕНИ файла. Следствие: любой новый `tls.go`
// в любом каталоге фундамента наследовал освобождение МОЛЧА — освобождение
// раздавалось по имени, а не по предмету. Проверено внесением: файл `tls.go` в
// другом каталоге, собирающий креды напрямую, страж пропускал зелёным.
//
// # Освобождение обязано истекать само
//
// Запись перечня, у которой больше нет предмета, — находка, а не порядок: она
// переживает свой файл и достаётся по наследству следующему, кто назовёт файл так
// же. Поэтому у каждой записи проверяется предпосылка: файл обязан быть в
// осмотренном составе И обязан нести запретную конструкцию. Плюс причина: запись
// без обоснования неотличима от упущения.
//
// # Состав берётся у индекса, а не с диска
//
// `pkg/treecorpus` — то же множество файлов, что увидит свежий клон и CI.
// Обход диска читал бы и то, чего в репозитории нет: рабочие копии агентов под
// `.claude/worktrees/`, сборочные и сгенерированные каталоги, отчёты прогонов, —
// и вердикт стал бы свойством чужого рабочего каталога, а не коммита. Следствие
// ходит в обе стороны: красный на файле, которого в репозитории нет, и молчание
// в свежем checkout там, где страж обязан говорить.
//
// # Что вне запрета
//
// `_test.go` — генерация тестовых CA/сертификатов вправе обращаться к
// `crypto/tls` напрямую.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// tlsGuardScopeDir — каталог фундамента, о котором говорит страж, от корня
// репозитория.
const tlsGuardScopeDir = "pkg"

// tlsGuardCensusFloor — сколько прод-файлов страж обязан осмотреть, чтобы его
// «ноль находок» вообще что-то значило. На 6b1293713 под `pkg/` отслеживается 385
// файлов `.go`, из них 263 не тестовые (предикат:
// `git ls-files -- pkg/ | grep '\.go$' | grep -vc '_test\.go$'`). Порог занижен
// более чем вдвое: он ловит обвал переписи (пустой состав, чужой каталог,
// обрезанный вывод), а не нормальный рост или усадку фундамента.
const tlsGuardCensusFloor = 100

// tlsHelperExemptions — освобождения стража. Ключ — ПУТЬ ОТ КОРНЯ РЕПОЗИТОРИЯ,
// слэш-разделённый; значение — ПРИЧИНА, по которой файлу позволено собирать креды
// напрямую. Пустая причина запрещена: запись без обоснования неотличима от
// упущения, и снять её потом будет некому.
//
// Перечень закрыт для пополнения по смыслу запрета: смысл в том, что сборка кред
// живёт в ДВУХ местах. Третья запись здесь означала бы, что запрет отменён, а не
// что у него появилось исключение.
var tlsHelperExemptions = map[string]string{
	"pkg/grpcsrv/tls.go": "единственная серверная сборка TLS-кред фундамента: " +
		"grpcsrv.TLSServerCreds. Ей нужен предмет запрета целиком, иначе запрещать нечего",
	"pkg/grpcclient/tls.go": "единственная клиентская сборка TLS-кред фундамента: " +
		"grpcclient.TLSClientCreds. Тот же случай с другой стороны соединения",
}

// bannedTLSTokens — прямая сборка кред, которой вне помощников быть не должно.
var bannedTLSTokens = []string{
	"credentials.NewTLS(",
	"tls.Config{",
}

// tlsScanTarget — один осматриваемый файл: путь от корня репозитория и его
// содержимое. Вход предиката задаётся ЗНАЧЕНИЯМИ, а не каталогом, поэтому
// инъекция подаёт синтетический состав, не трогая дерево.
type tlsScanTarget struct {
	Rel  string
	Body []byte
}

// tlsGuardVerdict — исход осмотра. Перепись (Scanned/Skipped) — отдельное
// утверждение: «ноль находок» обязано быть отличимо от «ноль прочитанного».
type tlsGuardVerdict struct {
	Violations  []string // "<путь> содержит <конструкция>"
	Scanned     int      // прод-файлов осмотрено
	Skipped     int      // тестовых файлов пропущено
	WithSubject []string // освобождения, у которых предмет НАЙДЕН
	Absent      []string // освобождения, чьего файла в осмотренном составе нет
	Pointless   []string // файл есть, запретной конструкции в нём нет
	Reasonless  []string // запись без причины
}

// auditTLSCredentialAssembly — сам предикат. Вынесен отдельно от чтения дерева,
// чтобы инъекция могла подать состав, которого в репозитории нет, и проверить
// исход в ОБЕ стороны.
func auditTLSCredentialAssembly(targets []tlsScanTarget, exemptions map[string]string) tlsGuardVerdict {
	v := tlsGuardVerdict{}
	// present: освобождение встретилось в осмотренном составе;
	// armed: у него нашёлся предмет запрета. Два РАЗНЫХ вопроса, и разъезжаются
	// они по-разному: переехавший файл и выхолощенный файл — разные находки.
	present := map[string]bool{}
	armed := map[string]bool{}

	for _, t := range targets {
		rel := filepath.ToSlash(t.Rel)
		if !strings.HasSuffix(rel, ".go") {
			continue
		}
		if strings.HasSuffix(rel, "_test.go") {
			v.Skipped++
			continue
		}
		v.Scanned++

		var hits []string
		for _, tok := range bannedTLSTokens {
			if strings.Contains(string(t.Body), tok) {
				hits = append(hits, tok)
			}
		}
		if _, exempt := exemptions[rel]; exempt {
			present[rel] = true
			if len(hits) > 0 {
				armed[rel] = true
			}
			continue
		}
		for _, tok := range hits {
			v.Violations = append(v.Violations, rel+" содержит "+tok)
		}
	}

	for rel, why := range exemptions {
		if strings.TrimSpace(why) == "" {
			v.Reasonless = append(v.Reasonless, rel)
		}
		switch {
		case !present[rel]:
			v.Absent = append(v.Absent, rel)
		case !armed[rel]:
			v.Pointless = append(v.Pointless, rel)
		default:
			v.WithSubject = append(v.WithSubject, rel)
		}
	}

	sort.Strings(v.Violations)
	sort.Strings(v.WithSubject)
	sort.Strings(v.Absent)
	sort.Strings(v.Pointless)
	sort.Strings(v.Reasonless)
	return v
}

// tlsGuardRepoRoot — корень репозитория: подъём по каталогам до `go.mod`.
// Освобождения ключуются путём ОТ КОРНЯ, поэтому корень обязан быть назван, а не
// подразумеваться относительным `..`.
func tlsGuardRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("корень репозитория не найден (нет go.mod ни в одном каталоге вверх) — " +
				"страж не может назвать дерево, о котором он говорит, а путь освобождения " +
				"без корня не с чем сравнивать")
		}
		dir = parent
	}
}

func TestSECB19_NoDirectTLSConfigOutsideHelper(t *testing.T) {
	root := tlsGuardRepoRoot(t)
	scope := filepath.Join(root, tlsGuardScopeDir)

	files, err := treecorpus.UnderWithSuffix(scope, ".go")
	require.NoError(t, err, "состав фундамента взять неоткуда — это отказ, а не «ноль находок»")

	targets := make([]tlsScanTarget, 0, len(files))
	for _, abs := range files {
		rel, relErr := filepath.Rel(root, abs)
		require.NoError(t, relErr)
		body, readErr := os.ReadFile(abs)
		// Файл индекса, которого нет на диске, — не находка и не тишина: он не
		// прочитан, поэтому в перепись не идёт, а страж об этом отказывается.
		require.NoError(t, readErr, "%s: файл индекса не прочитан — непрочитанное не есть «чисто»", rel)
		targets = append(targets, tlsScanTarget{Rel: filepath.ToSlash(rel), Body: body})
	}

	v := auditTLSCredentialAssembly(targets, tlsHelperExemptions)

	t.Logf("осмотрено: индекс %d файлов .go под %s/ -> прод %d, тестовых пропущено %d; "+
		"освобождений %d, с предметом %d",
		len(files), tlsGuardScopeDir, v.Scanned, v.Skipped, len(tlsHelperExemptions), len(v.WithSubject))

	// Порог переписи стоит ДО проверки самоистечения, и это не вкусовщина: на
	// обвалившемся составе каждое освобождение выглядит потерявшим предмет, потому
	// что его файла в осмотренном нет. Тогда страж напечатал бы поток ложных
	// «устаревших записей» и отправил чинить перечень вместо обхода. Сначала
	// устанавливается, что смотреть было на что.
	require.GreaterOrEqual(t, v.Scanned, tlsGuardCensusFloor,
		"перепись обвалилась: осмотрено %d прод-файлов при пороге %d — вердикт «ноль находок» "+
			"на таком объёме означал бы «ноль прочитанного», а не «чисто». Проверка "+
			"самоистечения на таком составе тоже недействительна: у каждой записи предмет "+
			"пропал бы вместе с обходом, а не в дереве",
		v.Scanned, tlsGuardCensusFloor)

	for _, rel := range v.Absent {
		t.Errorf("устаревшее освобождение: «%s» — такого файла в осмотренном составе %s/ нет.\n"+
			"  Освобождение переживает свой файл и достанется по наследству следующему, кто "+
			"назовёт файл так же. Перечень обязан только сокращаться.\n"+
			"  ЧТО ДЕЛАТЬ: сними запись либо назови новый путь помощника.", rel, tlsGuardScopeDir)
	}
	for _, rel := range v.Pointless {
		t.Errorf("устаревшее освобождение: «%s» есть в составе, но запретной конструкции не "+
			"несёт (ни одной из %v).\n"+
			"  Значит либо сборка кред оттуда уехала, либо запрет обессмыслен: записи, "+
			"которой нечего исключать, в перечне не место.\n"+
			"  ЧТО ДЕЛАТЬ: сними запись.", rel, bannedTLSTokens)
	}
	for _, rel := range v.Reasonless {
		t.Errorf("освобождение без причины: «%s». Запись без обоснования неотличима от "+
			"упущения — назови, почему этому файлу позволено собирать креды напрямую.", rel)
	}

	require.Empty(t, v.Violations,
		"TLS-креды фундамента собираются только через grpcsrv.TLSServerCreds / "+
			"grpcclient.TLSClientCreds; найдено %d:\n  %s\n\n"+
			"Освобождение ключуется ПУТЁМ файла, а не его именем: `tls.go` в другом каталоге "+
			"освобождения не наследует. Если это действительно третья точка сборки кред — "+
			"это решение об отмене запрета, а не строчка в перечне.",
		len(v.Violations), strings.Join(v.Violations, "\n  "))
}

// TestTLSGuardExemptionIsKeyedOnPathAndExpiresItself — доказательство стража
// инъекцией в обе стороны.
//
// Три внесения, каждое — настоящий дефект, и рядом законный близнец той же формы:
//
//	(а) файл с ТЕМ ЖЕ базовым именем в другом каталоге, собирающий креды напрямую,
//	    — обязан быть назван; законный близнец — тот же самый помощник на своём
//	    пути и тестовый файл с той же конструкцией — обязаны молчать;
//	(б) освобождение, чей файл из состава ушёл, и освобождение, чей файл предмета
//	    больше не несёт, — обязаны быть названы как устаревшие; законный близнец —
//	    запись с файлом И предметом — обязана молчать;
//	(в) освобождение без причины — обязано быть названо.
//
// Убери ключ по пути — покраснеет (а) наоборот: близнец в другом каталоге станет
// невидим. Убери проверку предпосылки — (б) перестанет краснеть.
func TestTLSGuardExemptionIsKeyedOnPathAndExpiresItself(t *testing.T) {
	const (
		helper   = "pkg/grpcsrv/tls.go"       // законное освобождение
		intruder = "pkg/observability/tls.go" // ТО ЖЕ базовое имя, другой каталог
		neighbor = "pkg/grpcsrv/server.go"    // прод-файл без запретной конструкции
		testFile = "pkg/grpcsrv/tls_bufconn_test.go"
	)
	// Инъекция обязана воспроизводить ИМЕННО тот дефект, ради которого написана.
	// Разойдись базовые имена — проба стала бы проверять что-то другое, оставаясь
	// зелёной.
	require.Equal(t, filepath.Base(helper), filepath.Base(intruder),
		"инъекция не воспроизводит дефект: базовые имена обязаны СОВПАДАТЬ — "+
			"именно на совпадении имени прежняя редакция раздавала освобождение")

	targets := []tlsScanTarget{
		{Rel: helper, Body: []byte("return credentials.NewTLS(tlsCfg), nil\n")},
		{Rel: intruder, Body: []byte("tr.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}\n")},
		{Rel: neighbor, Body: []byte("func New() *Server { return &Server{} }\n")},
		{Rel: testFile, Body: []byte("cfg := &tls.Config{InsecureSkipVerify: true}\n")},
	}

	// ── (а) ключ — путь, а не имя ────────────────────────────────────────────
	v := auditTLSCredentialAssembly(targets, map[string]string{helper: "законный помощник"})
	require.Equal(t, 3, v.Scanned, "осмотрено не то число прод-файлов")
	require.Equal(t, 1, v.Skipped, "тестовый файл не отнесён к пропущенным")
	require.Equal(t, []string{intruder + " содержит tls.Config{"}, v.Violations,
		"страж обязан назвать РОВНО внесённый дефект.\n"+
			"Пусто — освобождение раздаётся по базовому имени и новый файл наследует его молча.\n"+
			"Больше одного — под находку попал законный помощник или тестовый файл, и страж "+
			"снимут первым же ложным срабатыванием")
	require.Equal(t, []string{helper}, v.WithSubject, "законное освобождение обязано числиться с предметом")
	require.Empty(t, v.Absent)
	require.Empty(t, v.Pointless)
	require.Empty(t, v.Reasonless)

	// ── (б) освобождение без предмета — ДВЕ разные формы ─────────────────────
	//
	// Формы различаются намеренно: переехавший файл и выхолощенный файл требуют
	// разных действий (назвать новый путь либо снять запись), поэтому и находки
	// у них разные. Слив их в одну, страж отвечал бы «что-то не так» вместо
	// «что именно».
	const (
		moved = "pkg/grpcclient/tls.go" // пути нет в осмотренном составе
		// тот же файл, что neighbor выше: в составе есть, запретной конструкции не несёт
		hollowed = neighbor
	)
	stale := auditTLSCredentialAssembly(targets, map[string]string{
		helper:   "законный помощник",
		moved:    "помощник, которого в составе нет",
		hollowed: "файл есть, запретной конструкции в нём нет",
	})
	require.Equal(t, []string{moved}, stale.Absent,
		"освобождение, чьего файла в составе нет, обязано быть названо устаревшим — "+
			"иначе оно достанется следующему файлу с этим путём")
	require.Equal(t, []string{hollowed}, stale.Pointless,
		"освобождение, чей файл больше не несёт запретной конструкции, обязано быть названо: "+
			"записи, которой нечего исключать, в перечне не место")
	require.Equal(t, []string{helper}, stale.WithSubject,
		"законная запись обязана МОЛЧАТЬ — иначе самоистечение снимут как ложное срабатывание")
	require.Equal(t, []string{intruder + " содержит tls.Config{"}, stale.Violations,
		"устаревшие записи не должны менять сам запрет")

	// ── (в) освобождение без причины ─────────────────────────────────────────
	reasonless := auditTLSCredentialAssembly(targets, map[string]string{
		helper:   "законный помощник",
		intruder: "   ",
	})
	require.Equal(t, []string{intruder}, reasonless.Reasonless,
		"запись без обоснования неотличима от упущения и обязана быть названа")
	require.Empty(t, reasonless.Violations,
		"освобождение с пустой причиной всё же освобождает — иначе одна находка "+
			"печаталась бы дважды и причина расхождения читалась бы неверно")

	// ── предпосылка запрета: конструкции обязаны быть непустыми ──────────────
	require.NotEmpty(t, bannedTLSTokens, "предмета запрета нет — страж не может упасть ни на чём")
}
