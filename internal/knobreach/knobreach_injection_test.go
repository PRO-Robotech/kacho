// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package knobreach

// knobreach_injection_test.go — способность гейта упасть доказана ИНЪЕКЦИЕЙ, а не
// прочтением, и в обе стороны: нечитаемое имя краснит, читаемое молчит.
//
// Вход — синтетический, собранный в t.TempDir(): самопроверка, построенная на
// живой записи дерева, покраснела бы в тот день, когда дерево станет чистым, —
// то есть на достижении цели гейта (testing-verdict §4).
//
// Загрузчики здесь — ОБА вида, которые есть в дереве, с их правилами
// подстановки: envconfig-разбор (`corelib/config.LoadPrefixed`, имя из тега) и
// viper с двумя заменами разделителей — дефис в ключе сохраняется (как у
// одной службы) либо становится подчёркиванием (как у другой). Распознаватель,
// проверенный на одном виде, молчал бы на другом.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"

	corecfg "github.com/PRO-Robotech/corelib/config"
)

// envconfigProbe — поля тех трёх видов, что встречаются у служб: строка,
// логическое с умолчанием «истина», длительность.
type envconfigProbe struct {
	Port    string        `envconfig:"KACHO_KNOBPROBE_GRPC_PORT" default:"9090"`
	Enabled bool          `envconfig:"KACHO_KNOBPROBE_FEATURE_ENABLE" default:"true"`
	Budget  time.Duration `envconfig:"KACHO_KNOBPROBE_BUDGET" default:"30s"`
}

func loadEnvconfig() (any, error) {
	var c envconfigProbe
	err := corecfg.LoadPrefixed("KACHO_KNOBPROBE", &c)
	return c, err
}

type viperProbe struct {
	APIServer struct {
		Endpoint string `mapstructure:"endpoint"`
	} `mapstructure:"api-server"`
	Authz struct {
		TrustAny bool `mapstructure:"trust-any-forwarder"`
	} `mapstructure:"authz"`
}

// loadViper — viper-загрузчик с данной заменой разделителей. Ключ
// `authz.trust-any-forwarder` намеренно НЕ объявлен умолчанием: viper связывает
// с окружением только ключи, которые видел, — ровно так имя в тексте отказа
// одной из служб не доезжало ни в одном написании.
func loadViper(replacer *strings.Replacer) Loader {
	return func() (any, error) {
		v := viper.New()
		v.SetEnvPrefix("KACHO_KNOBPROBE")
		v.SetEnvKeyReplacer(replacer)
		v.AutomaticEnv()
		v.SetDefault("api-server.endpoint", "tcp://0.0.0.0:9090")
		var c viperProbe
		err := v.Unmarshal(&c)
		return c, err
	}
}

func TestResolveKnowsBothLoaderKindsAndTheirSubstitutionRules(t *testing.T) {
	dashKept := loadViper(strings.NewReplacer(".", "__"))
	dashToUnderscore := loadViper(strings.NewReplacer(".", "__", "-", "_"))

	for _, tc := range []struct {
		why  string
		name string
		load Loader
		want Verdict
	}{
		// envconfig: имя — из тега.
		{"тег поля строкой", "KACHO_KNOBPROBE_GRPC_PORT", loadEnvconfig, Reaches},
		{"логическое с умолчанием «истина» меняется только «ложью»", "KACHO_KNOBPROBE_FEATURE_ENABLE", loadEnvconfig, Reaches},
		{"длительность", "KACHO_KNOBPROBE_BUDGET", loadEnvconfig, Reaches},
		{"имя, которого нет ни в одном теге (так было у внутреннего порта)", "KACHO_KNOBPROBE_INTERNAL_GRPC_PORT", loadEnvconfig, DoesNotReach},
		{"дефис вместо подчёркивания в теговом имени", "KACHO_KNOBPROBE_GRPC-PORT", loadEnvconfig, DoesNotReach},

		// viper, дефис сохраняется: читается форма с дефисом.
		{"дефис ключа доезжает как есть", "KACHO_KNOBPROBE_API-SERVER__ENDPOINT", dashKept, Reaches},
		{"форма с подчёркиванием не доезжает", "KACHO_KNOBPROBE_API_SERVER__ENDPOINT", dashKept, DoesNotReach},

		// viper, дефис становится подчёркиванием: всё наоборот.
		{"подчёркивание вместо дефиса", "KACHO_KNOBPROBE_API_SERVER__ENDPOINT", dashToUnderscore, Reaches},
		{"форма с дефисом не доезжает", "KACHO_KNOBPROBE_API-SERVER__ENDPOINT", dashToUnderscore, DoesNotReach},
		{"ключ, не объявленный умолчанием, не доезжает ни в одном написании", "KACHO_KNOBPROBE_AUTHZ__TRUST_ANY_FORWARDER", dashToUnderscore, DoesNotReach},
	} {
		t.Run(tc.why, func(t *testing.T) {
			got, err := Resolve(tc.name, tc.load)
			if got != tc.want {
				t.Fatalf("Resolve(%s) = %v (%v), ждали %v", tc.name, got, err, tc.want)
			}
		})
	}
}

// Третий исход: загрузчик отказывает одним и тем же текстом при любом
// значении — вопрос не задан, и «не читается» здесь было бы ложью.
func TestResolveSaysUndeterminedWhenTheLoaderAlwaysRefusesAlike(t *testing.T) {
	always := func() (any, error) { return nil, os.ErrPermission }
	got, err := Resolve("KACHO_KNOBPROBE_GRPC_PORT", always)
	if got != Undetermined || err == nil {
		t.Fatalf("исход %v (%v), ждали «не определено» с причиной", got, err)
	}
}

// Окружение возвращается в прежнее состояние: гейт спрашивает о сотне имён
// подряд, и подстановка, оставленная одним вопросом, отвечала бы за следующий.
func TestResolveRestoresTheEnvironment(t *testing.T) {
	t.Setenv("KACHO_KNOBPROBE_GRPC_PORT", "1234")
	if _, err := Resolve("KACHO_KNOBPROBE_GRPC_PORT", loadEnvconfig); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("KACHO_KNOBPROBE_GRPC_PORT"); got != "1234" {
		t.Fatalf("после вопроса значение %q, до него было 1234", got)
	}
}

// writeProbeDir кладёт синтетический пакет: файлы проб и файлы текстов.
func writeProbeDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// probeFile — три позиции задания (Setenv, ключ карты, индекс в левой части) и
// одно упоминание. Имя под пробой подставляется в ключ карты.
func probeFile(mapKeyName string) string {
	return `package x

import (
	"strings"
	"testing"
)

func TestX(t *testing.T) {
	t.Setenv("KACHO_KNOBPROBE_GRPC_PORT", "0")
	env := map[string]string{
		"` + mapKeyName + `": "0",
	}
	env["KACHO_KNOBPROBE_BUDGET"] = "1s"
	_ = strings.Contains("отказ называет KACHO_KNOBPROBE_NOT_A_SETTING", "KACHO_KNOBPROBE_NOT_A_SETTING")
	_ = env
}
`
}

func TestJudgeFindsTheUnreadNameWithItsCoordinateAndSparesTheTwin(t *testing.T) {
	t.Run("инъекция: нечитаемое имя в ключе карты", func(t *testing.T) {
		dir := writeProbeDir(t, map[string]string{"x_test.go": probeFile("KACHO_KNOBPROBE_INTERNAL_GRPC_PORT")})
		r, err := Judge(Package{Service: "probe", Dir: dir, Load: loadEnvconfig})
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Findings) != 1 {
			t.Fatalf("находок %d, ждали одну: %+v", len(r.Findings), r.Findings)
		}
		f := r.Findings[0].Setting
		if f.Name != "KACHO_KNOBPROBE_INTERNAL_GRPC_PORT" || f.File != "x_test.go" || f.Line != 11 || f.Form != "map-key" {
			t.Fatalf("находка без верной координаты: %+v", f)
		}
		if len(r.Probes.Settings) != 3 || r.Probes.Mentions != 1 {
			t.Fatalf("перепись не та: заданий %d (ждали 3), упоминаний %d (ждали 1)",
				len(r.Probes.Settings), r.Probes.Mentions)
		}
	})
	t.Run("законный близнец: та же форма, читаемое имя", func(t *testing.T) {
		dir := writeProbeDir(t, map[string]string{"x_test.go": probeFile("KACHO_KNOBPROBE_FEATURE_ENABLE")})
		r, err := Judge(Package{Service: "probe", Dir: dir, Load: loadEnvconfig})
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Findings) != 0 || len(r.NotJudged) != 0 {
			t.Fatalf("близнец обязан молчать: %+v %+v", r.Findings, r.NotJudged)
		}
		if r.Reached != 3 {
			t.Fatalf("доезжает %d имён, ждали 3", r.Reached)
		}
	})
}

// Тексты процесса: имя, названное оператору, обязано читаться. Приставки
// семейства, литералы-имена целиком и теги полей — не текст.
func TestJudgeFindsAProseNameTheLoaderDoesNotRead(t *testing.T) {
	prose := func(name string) string {
		return "package x\n\nimport \"fmt\"\n\n" +
			"const envPrefix = \"KACHO_KNOBPROBE\"\n\n" +
			"type C struct {\n\tPort string `envconfig:\"KACHO_KNOBPROBE_WHATEVER\"`\n}\n\n" +
			"func refuse() error {\n" +
			"\t_ = fmt.Errorf(\"KACHO_KNOBPROBE_ADMISSION_*: разбор\")\n" +
			"\treturn fmt.Errorf(\"порт не задан: выставь " + name + "\")\n}\n"
	}
	probes := probeFile("KACHO_KNOBPROBE_FEATURE_ENABLE")

	t.Run("инъекция: текст называет форму, которой нет", func(t *testing.T) {
		dir := writeProbeDir(t, map[string]string{"x_test.go": probes, "x.go": prose("KACHO_KNOBPROBE_GRPC-PORT")})
		r, err := Judge(Package{Service: "probe", Dir: dir, ProseDirs: []string{dir}, Load: loadEnvconfig})
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Findings) != 1 || r.Findings[0].Setting.Form != "текст" ||
			r.Findings[0].Setting.Name != "KACHO_KNOBPROBE_GRPC-PORT" || r.Findings[0].Setting.Line != 13 {
			t.Fatalf("ждали одну находку в тексте на строке 13: %+v", r.Findings)
		}
		if r.Prose.Files != 1 || len(r.Prose.Names) != 1 || r.Prose.Skipped != 2 {
			t.Fatalf("перепись текстов не та: файлов %d, имён %d, пропущено %d (ждали 1, 1, 2)",
				r.Prose.Files, len(r.Prose.Names), r.Prose.Skipped)
		}
	})
	t.Run("законный близнец: текст называет читаемую форму", func(t *testing.T) {
		dir := writeProbeDir(t, map[string]string{"x_test.go": probes, "x.go": prose("KACHO_KNOBPROBE_GRPC_PORT")})
		r, err := Judge(Package{Service: "probe", Dir: dir, ProseDirs: []string{dir}, Load: loadEnvconfig})
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Findings) != 0 {
			t.Fatalf("близнец обязан молчать: %+v", r.Findings)
		}
	})
	t.Run("каталог текстов без файлов Go — не выполнилось", func(t *testing.T) {
		dir := writeProbeDir(t, map[string]string{"x_test.go": probes})
		_, err := Judge(Package{Service: "probe", Dir: dir, ProseDirs: []string{t.TempDir()}, Load: loadEnvconfig})
		if err == nil || !strings.Contains(err.Error(), "НЕ ВЫПОЛНИЛОСЬ") {
			t.Fatalf("пустой каталог текстов обязан дать «не выполнилось», а не зелёное: %v", err)
		}
	})
}

// Пустой обход — «не выполнилось», а не зелёное: и когда файлов нет, и когда
// в них одни упоминания.
func TestJudgeRefusesAnEmptyWalk(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"файлов проб нет": {},
		"одни упоминания": {"x_test.go": "package x\n\nimport \"strings\"\n\nvar _ = strings.Contains(\"\", \"KACHO_KNOBPROBE_GRPC_PORT\")\n"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Judge(Package{Service: "probe", Dir: writeProbeDir(t, files), Load: loadEnvconfig})
			if err == nil || !strings.Contains(err.Error(), "НЕ ВЫПОЛНИЛОСЬ") {
				t.Fatalf("пустой обход обязан дать «не выполнилось»: %v", err)
			}
		})
	}
}

// recordingT — подставной исполнитель: гейт говорит с ним тем же интерфейсом,
// что с настоящей пробой, и по записи видно, ЧТО он напечатал.
type recordingT struct {
	errors, fatals, logs []string
	env                  map[string]string
}

func (r *recordingT) Helper() {}
func (r *recordingT) Setenv(k, v string) {
	if r.env == nil {
		r.env = map[string]string{}
	}
	r.env[k] = v
}
func (r *recordingT) Errorf(f string, a ...any) { r.errors = append(r.errors, fmt.Sprintf(f, a...)) }
func (r *recordingT) Fatalf(f string, a ...any) { r.fatals = append(r.fatals, fmt.Sprintf(f, a...)) }
func (r *recordingT) Logf(f string, a ...any)   { r.logs = append(r.logs, fmt.Sprintf(f, a...)) }

func TestGateSpeaksThroughItsExecutor(t *testing.T) {
	t.Run("находка — отказ пробы с координатой", func(t *testing.T) {
		rt := &recordingT{}
		dir := writeProbeDir(t, map[string]string{"x_test.go": probeFile("KACHO_KNOBPROBE_INTERNAL_GRPC_PORT")})
		Gate(rt, Package{Service: "probe", Dir: dir, Load: loadEnvconfig})
		if len(rt.errors) != 1 || !strings.Contains(rt.errors[0], "x_test.go:11") ||
			!strings.Contains(rt.errors[0], "KACHO_KNOBPROBE_INTERNAL_GRPC_PORT") {
			t.Fatalf("гейт не назвал находку координатой: %q", rt.errors)
		}
		if len(rt.logs) != 1 || !strings.Contains(rt.logs[0], "уникальных имён 3") {
			t.Fatalf("гейт не напечатал переписи: %q", rt.logs)
		}
	})
	t.Run("пустой обход — остановка с «не выполнилось»", func(t *testing.T) {
		rt := &recordingT{}
		Gate(rt, Package{Service: "probe", Dir: writeProbeDir(t, nil), Load: loadEnvconfig})
		if len(rt.fatals) != 1 || !strings.Contains(rt.fatals[0], "НЕ ВЫПОЛНИЛОСЬ") {
			t.Fatalf("пустой обход не остановил гейт: fatals=%q errors=%q", rt.fatals, rt.errors)
		}
	})
}
