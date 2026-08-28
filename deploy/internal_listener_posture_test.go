// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// internal_listener_posture_test.go — измерение internal_mtls обязано принимать
// «внутреннего листенера НЕТ» и по-прежнему отвергать «листенер есть и не защищён».
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Задача #1024 сняла у края единственную службу его внутреннего gRPC-листенера,
// а вместе с ней и сам листенер. Процесс без листенера отчитывается величиной
// "n/a" (pkg/observability, InternalMTLSNotApplicable) — ровно тем идиомом, каким
// в этом же самоотчёте уже объявлены «базы нет» (db_sslmode) и «человека не
// проверяем» (identity_provider).
//
// Послабление узкое, и узость — весь его смысл:
//
//	"n/a"  — листенера НЕТ ВОВСЕ: входной поверхности не существует, защищать
//	         нечего. Проходит.
//	"false"— листенер ЕСТЬ и работает БЕЗ mTLS. ОТКАЗ, как и до задачи #1024.
//	ключа нет — так выглядит процесс со СТАРЫМ образом. ОТКАЗ: неотчитанное не
//	         считается сделанным.
//	""     — поле объявлено пустым: величина не из трёх объявленных. ОТКАЗ.
//
// Схлопнуть "n/a" и "false" значило бы разрешить незащищённый листенер молчанием
// — то есть снять контроль, а не сузить его предмет.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ПРОГРАММА ВЕРДИКТА ВЫНИМАЕТСЯ ИЗ ГЕЙТА, А НЕ ПЕРЕПИСЫВАЕТСЯ ЗДЕСЬ
//
// Копия разъехалась бы с оригиналом молча, и проверка снова стала бы формой без
// содержания. Тот же приём, что у deploy/tests/helm/{iam,registry}-trusted-
// forwarder-test.sh. Предпосылка извлечения проверяется явно: пустая или не
// судящая internal_mtls программа — ОТКАЗ этой пробы, а не её молчание.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЗДЕСЬ НЕ ПРОВЕРЯЕТСЯ (чтобы «зелено» не читалось шире, чем есть)
//
// Проба судит ПРОГРАММУ ВЕРДИКТА, а не весь гейт: обход подов, чтение живого
// лога и независимый свидетель со стороны СУБД требуют поднятого кластера и
// остаются за самим гейтом. Здесь — ровно решение «этот самоотчёт годен или нет».
package deploy_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// postureVerdictProgram вынимает из гейта программу jq, которая выносит вердикт
// по одной строке самоотчёта.
func postureVerdictProgram(t *testing.T) string {
	t.Helper()
	gate := filepath.Join("scripts", "assert-production-posture.sh")
	raw, err := os.ReadFile(gate)
	if err != nil {
		t.Fatalf("гейт посадки не прочитан (%s): %v", gate, err)
	}
	lines := strings.Split(string(raw), "\n")
	start := -1
	for i, l := range lines {
		if strings.Contains(l, `verdict="$(printf`) {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("в %s не найдено начало программы вердикта — гейт сменил форму, "+
			"чинить надо это извлечение, а не молчать", gate)
	}
	end := -1
	for i := start; i < len(lines); i++ {
		if strings.Contains(lines[i], `join(", ")`) {
			end = i
			break
		}
	}
	if end < 0 {
		t.Fatalf("в %s не найден конец программы вердикта — гейт сменил форму", gate)
	}
	block := strings.Join(lines[start:end+1], "\n")
	// Срезается ровно шелл-обвязка вызова jq, а не «до первой кавычки»: в этой
	// строке апострофов три, и наивный срез оставил бы кусок printf внутри
	// программы. Тот же срез, что в извлечении соседних проверок.
	head := regexp.MustCompile(`(?s)^.*?jq -r --argjson need_fwd "\$need_fwd" '`)
	if !head.MatchString(block) {
		t.Fatalf("вызов jq в %s записан иначе, чем ожидает извлечение — "+
			"гейт сменил форму:\n%s", gate, block)
	}
	block = head.ReplaceAllString(block, "")
	block = strings.TrimSuffix(strings.TrimSpace(block), `')"`)
	if strings.TrimSpace(block) == "" {
		t.Fatalf("программа вердикта вынулась пустой из %s", gate)
	}
	return block
}

var shownDim = regexp.MustCompile(`shown\("([a-z_]+)"\)`)

// judgedDimensions — измерения, которые программа реально судит. ВЫВОДЯТСЯ из
// программы, а не выписываются: выписанный перечень был бы второй копией и
// разошёлся бы с ней молча — тем же способом, каким однажды разошлась фикстура
// круга отправителей, не знавшая про identity_provider.
func judgedDimensions(prog string) []string {
	seen := map[string]bool{}
	for _, m := range shownDim.FindAllStringSubmatch(prog, -1) {
		seen[m[1]] = true
	}
	out := make([]string, 0, len(seen))
	for d := range seen {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

// runVerdict прогоняет строку самоотчёта через вынутую программу.
// Пустой вердикт = посадка принята.
func runVerdict(t *testing.T, prog string, line map[string]any) string {
	t.Helper()
	body, err := json.Marshal(line)
	if err != nil {
		t.Fatalf("фикстура самоотчёта не сериализовалась: %v", err)
	}
	cmd := exec.Command("jq", "-r", "--argjson", "need_fwd", "false", prog)
	cmd.Stdin = strings.NewReader(string(body))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("jq не отработал на входе %s: %v\n%s", body, err, out)
	}
	return strings.TrimSpace(string(out))
}

// controlLine — самоотчёт, годный по КАЖДОМУ измерению, которое судит программа.
// Без него отрицательные случаи ниже краснели бы по чужому измерению и говорили
// бы об этом голым текстом вердикта, из которого не видно, чего не хватает.
func controlLine(t *testing.T, prog string) map[string]any {
	t.Helper()
	line := map[string]any{
		"msg":                "boot security posture",
		"service":            "api-gateway",
		"auth_mode":          "production",
		"db_sslmode":         "n/a",
		"public_mtls":        true,
		"internal_mtls":      "true",
		"authz_check":        true,
		"trusted_forwarders": true,
		"identity_provider":  "own",
	}
	var missing []string
	for _, d := range judgedDimensions(prog) {
		if _, ok := line[d]; !ok {
			missing = append(missing, d)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("положительный контроль не несёт измерений, которые судит гейт: %v — "+
			"добавьте их сюда, иначе отрицательные случаи краснеют по чужому измерению",
			missing)
	}
	return line
}

func withInternalMTLS(base map[string]any, v any) map[string]any {
	out := make(map[string]any, len(base))
	for k, val := range base {
		out[k] = val
	}
	if v == nil {
		delete(out, "internal_mtls")
	} else {
		out["internal_mtls"] = v
	}
	return out
}

func requireJQ(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("jq"); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("jq не на PATH в CI — проверка вердикта обязана исполняться, а не пропускаться")
		}
		t.Skip("jq не на PATH — проверка вердикта пропущена")
	}
}

// TestPostureVerdict_InternalMTLSThreeStates — послабление узкое, и обе стороны
// утверждаются одним прогоном: без средних случаев «n/a проходит» означало бы
// «проходит что угодно».
func TestPostureVerdict_InternalMTLSThreeStates(t *testing.T) {
	requireJQ(t)
	prog := postureVerdictProgram(t)

	// ПРЕДПОСЫЛКА: программа вообще судит это измерение. Если перестанет — все
	// случаи ниже станут зелёными, и проба будет утверждать о пустоте.
	if !strings.Contains(prog, "internal_mtls") {
		t.Fatalf("вынутая программа вердикта не судит internal_mtls вовсе — "+
			"измерение исчезло из гейта, и эта проба утверждала бы о пустоте:\n%s", prog)
	}
	control := controlLine(t, prog)

	// Положительный контроль: годная посадка проходит целиком. Без него отказы
	// ниже неотличимы от «программа бракует всё».
	if v := runVerdict(t, prog, control); v != "" {
		t.Fatalf("годная посадка забракована вердиктом: %q — отрицательные случаи "+
			"ниже красили бы по чужому измерению", v)
	}

	for _, tc := range []struct {
		name    string
		give    any
		wantBad bool
		why     string
	}{
		{
			name: "листенер есть и защищён",
			give: "true", wantBad: false,
			why: "нормальная production-посадка сервиса с внутренним листенером",
		},
		{
			name: "внутреннего листенера НЕТ ВОВСЕ",
			give: "n/a", wantBad: false,
			why: "входной поверхности не существует — защищать нечего (край, задача #1024)",
		},
		{
			name: "листенер есть и НЕ защищён",
			give: "false", wantBad: true,
			why: "несущее различие: послабление под «нет листенера» не смеет накрыть " +
				"незащищённый листенер, иначе оно снимает контроль, а не сужает его предмет",
		},
		{
			name: "поля нет вовсе (старый образ)",
			give: nil, wantBad: true,
			why: "неотчитанное не считается сделанным",
		},
		{
			name: "поле объявлено пустым",
			give: "", wantBad: true,
			why: "нулевое значение структуры — величина не из трёх объявленных",
		},
		{
			name: "величина булева (образ до смены типа)",
			give: true, wantBad: true,
			why: "наполовину перекатившийся флот обязан быть виден: измерение стало " +
				"строковым, и булево true означает процесс со старым образом",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := runVerdict(t, prog, withInternalMTLS(control, tc.give))
			bad := strings.Contains(v, "internal_mtls")
			if bad != tc.wantBad {
				t.Fatalf("internal_mtls=%#v: вердикт %q; ждали %s — %s",
					tc.give, v,
					map[bool]string{true: "ОТКАЗ", false: "проход"}[tc.wantBad], tc.why)
			}
			// Отказ обязан называть ИМЕННО это измерение, а не любое: вердикт,
			// покрасневший по соседнему полю, читался бы как находка про листенер.
			if tc.wantBad && v != "" && !strings.HasPrefix(v, "internal_mtls=") {
				t.Fatalf("отказ по internal_mtls смешан с чужими измерениями: %q", v)
			}
		})
	}
}

// TestPostureVerdict_DevProfileStillSkipsTheDimension — граница послабления с
// другой стороны: в dev-профиле измерение не судится вовсе, и правка выше этого
// не изменила. Иначе «n/a проходит» могло бы означать «программа перестала
// градуировать», а не «величина принята».
func TestPostureVerdict_DevProfileStillSkipsTheDimension(t *testing.T) {
	requireJQ(t)
	prog := postureVerdictProgram(t)
	control := controlLine(t, prog)

	cmd := exec.Command("jq", "-r", "--argjson", "need_fwd", "false", prog)
	body, _ := json.Marshal(withInternalMTLS(control, "false"))
	cmd.Stdin = strings.NewReader(string(body))
	cmd.Env = append(os.Environ(), "POSTURE_PROFILE=dev")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("jq не отработал: %v\n%s", err, out)
	}
	if strings.Contains(string(out), "internal_mtls") {
		t.Fatalf("dev-профиль стал судить internal_mtls: %q — гейт красил бы каждый "+
			"dev-стенд, и его перестали бы читать", strings.TrimSpace(string(out)))
	}
}
