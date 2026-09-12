// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// pack_test.go — общая механика: упаковать ревизию как это делает прокси и
// собрать её ПУСТЫМ внешним модулем.
//
// Механика одна на предмет и на его инъекцию намеренно. Проба, воспроизводящая
// упаковку своими словами, проверяла бы свою копию, а не то, что исполняется:
// инъекция обязана гонять ТОТ ЖЕ код, иначе она доказывает падучесть двойника.
//
// # Прокси песочницы выкладывает НАШ зип и зипы наших зависимостей
//
// Наш зип собирается здесь и сейчас — он и есть предмет. Зависимости берутся
// вторым элементом `GOPROXY` из каталога загрузок ОБЩЕГО кэша модулей: он
// уложен как файловый прокси by construction, поэтому годится вторым элементом
// без переупаковки.
//
// Сеть при этом не участвует ни одним элементом: оба `file://`. Что именно
// куплено кэшем, сказано прямо — ЗАВИСИМОСТИ, а не наш модуль; наш зип берётся
// только из первого элемента, потому что версии `v0.0.0-2000…` в чужом кэше
// не бывает. Плата названа: кэш обязан быть населён, и он населён by
// construction — этот тест собран из модуля, который те же зависимости
// требует. Не населён — проба говорит УСЛОВИЕ НЕ СОЗДАНО, а не «модуль не
// собирается».
//
// Прежняя редакция выкладывала ОДИН зип и держалась на том, что потребитель
// импортирует пакет без внешних зависимостей. Такой пакет у платформы был
// (`pkg/ids`) и уехал в общую библиотеку — предпосылка умерла, а гейт остался
// красным, объявляя находкой собственную технику.
package release

import (
	"archive/zip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/mod/module"
	modzip "golang.org/x/mod/zip"
)

// maxZipMiB — предел зипа модуля у прокси Go. Число печатается переписью рядом
// с фактическим размером: «влезаем» обязано быть видно числом, а не выводиться
// из молчания пробы.
const maxZipMiB = 500

type packRequest struct {
	vcsRoot    string // каталог с КАТАЛОГОМ .git — упаковывается отслеживаемое
	modulePath string // путь публикуемого модуля
	importPath string // что импортирует потребитель
	program    string // текст программы потребителя
}

type packResult struct {
	revision   string
	filesInZip int
	zipBytes   int64
	err        error  // отказ упаковки либо сборки у потребителя
	output     string // захваченный вывод инструмента — диагностика, а не пересказ
	depProxy   string // цепочка GOPROXY песочницы — печатается переписью
}

// sharedDownloadProxy — каталог загрузок общего кэша модулей, годный вторым
// элементом `GOPROXY`.
//
// Пустая строка означает «кэш не населён»: тогда цепочка остаётся из одного
// элемента, и потребитель, которому нужна зависимость, получит отказ «нет
// зипа». Это УСЛОВИЕ НЕ СОЗДАНО, и вызывающий обязан назвать его так — молча
// подставлять сеть здесь нельзя, иначе проба меряет её доступность.
func sharedDownloadProxy(t *testing.T, env []string) string {
	t.Helper()
	shared := strings.TrimSpace(runOut(t, env, "", "go", "env", "GOMODCACHE"))
	if shared == "" {
		return ""
	}
	dl := filepath.Join(shared, "cache", "download")
	if st, err := os.Stat(dl); err != nil || !st.IsDir() {
		return ""
	}
	return dl
}

// packAndBuild — упаковать ревизию `vcsRoot` и собрать её пустым модулем.
//
// Сборка идёт в СВОЁМ кэше модулей (`GOMODCACHE` во временном каталоге): общий
// кэш здесь одновременно и загрязняется, и делит состояние с соседними
// сессиями на той же машине. Плата за свой кэш — распаковка зипа на каждый
// прогон; она же даёт герметичность: зелёное не может быть куплено тем, что
// кто-то другой уже что-то скачал.
func packAndBuild(t *testing.T, req packRequest) packResult {
	t.Helper()

	var res packResult
	env := cleanGitEnv()

	// Инструмент — ТОТ ЖЕ, которым собран этот тест. Иначе `GOTOOLCHAIN=local`
	// поднимет системный go, а он в этом дереве старше, чем требует go.mod, и
	// проба покраснела бы на версии инструмента, а не на предмете.
	goroot := strings.TrimSpace(runOut(t, env, "", "go", "env", "GOROOT"))
	goBin := filepath.Join(goroot, "bin", "go")

	res.revision = strings.TrimSpace(runOut(t, env, req.vcsRoot, "git", "rev-parse", "HEAD"))
	if len(res.revision) < 12 {
		t.Fatalf("ревизия не прочитана в %s", req.vcsRoot)
	}
	version := "v0.0.0-20000101000000-" + res.revision[:12]

	work := t.TempDir()
	modcache := filepath.Join(work, "modcache")
	// Кэш модулей раскладывается ТОЛЬКО ДЛЯ ЧТЕНИЯ, и `t.TempDir` его снести не
	// может. Уборка регистрируется ПОСЛЕ `t.TempDir`, поэтому исполняется
	// РАНЬШЕ его собственной (порядок обратен регистрации) — иначе проба
	// краснеет на уборке при исправном предмете.
	t.Cleanup(func() {
		if _, err := os.Stat(modcache); err == nil {
			c := exec.Command(goBin, "clean", "-modcache")
			c.Env = append(cleanGitEnv(), "GOMODCACHE="+modcache, "GOTOOLCHAIN=local", "GOFLAGS=")
			_ = c.Run()
		}
	})

	esc, err := module.EscapePath(req.modulePath)
	if err != nil {
		t.Fatalf("путь модуля не экранируется: %v", err)
	}
	proxyDir := filepath.Join(work, "proxy")
	verDir := filepath.Join(proxyDir, filepath.FromSlash(esc), "@v")
	if err := os.MkdirAll(verDir, 0o755); err != nil {
		t.Fatalf("каталог прокси не создан: %v", err)
	}

	zipPath := filepath.Join(verDir, version+".zip")
	zf, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("зип не создан: %v", err)
	}
	mv := module.Version{Path: req.modulePath, Version: version}
	packErr := modzip.CreateFromVCS(zf, mv, req.vcsRoot, "HEAD", "")
	closeErr := zf.Close()
	if packErr != nil {
		res.err = packErr
		res.output = "упаковка ревизии " + res.revision[:12] + " отвергнута правилами модуля Go"
		return res
	}
	if closeErr != nil {
		t.Fatalf("зип не закрыт: %v", closeErr)
	}

	if st, err := os.Stat(zipPath); err == nil {
		res.zipBytes = st.Size()
	}
	if zr, err := zip.OpenReader(zipPath); err == nil {
		res.filesInZip = len(zr.File)
		zr.Close()
	}

	// `.mod` прокси обязан быть go.mod ИМЕННО ЭТОЙ ревизии: расхождение с тем,
	// что лежит в зипе, инструмент отвергает — и отвергает верно.
	gomod, err := os.ReadFile(filepath.Join(req.vcsRoot, "go.mod"))
	if err != nil {
		t.Fatalf("go.mod ревизии не прочитан: %v", err)
	}
	write(t, filepath.Join(verDir, version+".mod"), string(gomod))
	write(t, filepath.Join(verDir, version+".info"),
		`{"Version":"`+version+`","Time":"2000-01-01T00:00:00Z"}`)
	write(t, filepath.Join(verDir, "list"), version+"\n")

	// Потребитель: пустой модуль. Ни одного `replace` — путь ровно тот, каким
	// пойдёт вынесенный сервис.
	cons := filepath.Join(work, "consumer")
	if err := os.MkdirAll(cons, 0o755); err != nil {
		t.Fatalf("каталог потребителя не создан: %v", err)
	}
	write(t, filepath.Join(cons, "go.mod"), "module consumer.example\n\ngo "+goDirective(string(gomod))+"\n")
	write(t, filepath.Join(cons, "main.go"), req.program)

	// Второй элемент — каталог загрузок общего кэша: раскладка `cache/download`
	// сама есть файловый прокси. Разделитель `,` (а не `|`) выбран намеренно:
	// на «нет такой версии» инструмент переходит к следующему элементу, а на
	// повреждённом зипе обязан отказать, а не тихо уйти в сеть.
	proxyChain := "file://" + filepath.ToSlash(proxyDir)
	if depProxy := sharedDownloadProxy(t, env); depProxy != "" {
		proxyChain += ",file://" + filepath.ToSlash(depProxy)
	}
	res.depProxy = proxyChain

	consEnv := append(env,
		"GOPROXY="+proxyChain,
		"GOFLAGS=-mod=mod",
		"GOSUMDB=off",
		"GONOSUMDB=",
		"GOPRIVATE=",
		"GOWORK=off",
		"GOTOOLCHAIN=local",
		"GOMODCACHE="+modcache,
	)

	var sb strings.Builder
	for _, args := range [][]string{
		{"mod", "edit", "-require=" + req.modulePath + "@" + version},
		{"build", "./..."},
	} {
		cmd := exec.Command(goBin, args...)
		cmd.Dir = cons
		cmd.Env = consEnv
		out, err := cmd.CombinedOutput()
		sb.WriteString("$ go " + strings.Join(args, " ") + "\n")
		sb.Write(out)
		if err != nil {
			res.err = err
			res.output = sb.String()
			return res
		}
	}
	res.output = sb.String()
	return res
}

// goDirective — значение директивы `go` из текста go.mod. Потребитель обязан
// объявить не меньшую версию языка, иначе инструмент отвергнет зависимость по
// причине, к предмету пробы не относящейся.
func goDirective(gomod string) string {
	for _, line := range strings.Split(gomod, "\n") {
		if f := strings.Fields(line); len(f) >= 2 && f[0] == "go" {
			return f[1]
		}
	}
	return "1.21"
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("файл не записан (%s): %v", path, err)
	}
}

func runOut(t *testing.T, env []string, dir string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
	return string(out)
}
