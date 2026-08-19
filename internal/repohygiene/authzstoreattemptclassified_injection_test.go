// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import "testing"

// Проба инъекции: гейт «обращение к хранилищу прав объявляет причину исхода»
// обязан УМЕТЬ покраснеть — с координатой — и обязан МОЛЧАТЬ на законных
// конструкциях. Без обеих сторон он ловил бы форму, а не существо: первый
// ложный срабат такой гейт и выключает.

const injDefectPlainCall = `package clients

func check(req *http.Request) error {
	resp, err := fgaHTTPClient.Do(req)
	if err != nil {
		return err
	}
	_ = resp
	return nil
}
`

const injDefectInsideLiteral = `package clients

func applyBatch(ctx context.Context) error {
	// Объемлющая функция классифицирует СВОЙ вызов; литерал внутри — нет.
	// Свойство требуется от ближайшей функции, иначе «классификатор где-то
	// рядом» засчитывался бы за наблюдение чужого вызова.
	_ = classifyFGAAttempt(nil, 0, nil)
	return apply(func(req *http.Request) error {
		resp, err := fgaHTTPClient.Do(req)
		_ = resp
		return err
	})
}
`

const injLegalClassified = `package clients

func check(req *http.Request) error {
	tr := &fgaConnTrace{}
	resp, err := fgaHTTPClient.Do(req)
	status := 0
	if resp != nil {
		status = resp.StatusCode
	}
	outcome := classifyFGAAttempt(tr, status, err)
	_ = outcome
	return err
}
`

const injLegalOtherReceivers = `package clients

// Другая КОНСТРУКЦИЯ, а не копия предыдущей: обращения к иным клиентам и через
// цепочку селекторов транспортом хранилища прав не являются и классификации от
// них никто не требует.
func neighbours(req *http.Request) {
	_, _ = hydraHTTPClient.Do(req)
	_, _ = r.inner.Do(req)
	_, _ = c.do(nil, "POST", "", nil)
}
`

const injLegalOnlyProse = `package clients

// Здесь fgaHTTPClient.Do упомянут ПРОЗОЙ вместе с classifyFGAAttempt — ровно
// так, как это написано в шапках адаптера и самого гейта. Проверка по тексту
// краснела бы на собственном объяснении.
const note = "fgaHTTPClient.Do без classifyFGAAttempt — находка"
`

func TestAuthzStoreGate_RedOnInjectedDefect(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		src      string
		wantFunc string
		wantLine int
	}{
		"вызов без классификации":  {injDefectPlainCall, "check", 4},
		"вызов в литерале без неё": {injDefectInsideLiteral, "applyBatch (литерал)", 9},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			findings, census, err := FindUnclassifiedAuthzStoreCalls(map[string]string{"x.go": tc.src})
			if err != nil {
				t.Fatalf("разбор: %v", err)
			}
			if census.Files != 1 || census.Calls != 1 {
				t.Fatalf("перепись разошлась с содержимым: файлов=%d обращений=%d, ждали 1 и 1",
					census.Files, census.Calls)
			}
			if len(findings) != 1 {
				t.Fatalf("гейт не покраснел на внесённом дефекте: находок %d", len(findings))
			}
			got := findings[0]
			if got.Line != tc.wantLine || got.Func != tc.wantFunc {
				t.Fatalf("координата находки неверна: %s:%d в %q; ждали строку %d в %q",
					got.File, got.Line, got.Func, tc.wantLine, tc.wantFunc)
			}
		})
	}
}

func TestAuthzStoreGate_SilentOnLegalTwins(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		src       string
		wantCalls int
	}{
		"исход классифицирован":     {injLegalClassified, 1},
		"другие получатели .Do":     {injLegalOtherReceivers, 0},
		"упоминание только в прозе": {injLegalOnlyProse, 0},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			findings, census, err := FindUnclassifiedAuthzStoreCalls(map[string]string{"x.go": tc.src})
			if err != nil {
				t.Fatalf("разбор: %v", err)
			}
			if len(findings) != 0 {
				t.Fatalf("гейт покраснел на законной конструкции: %+v", findings)
			}
			if census.Calls != tc.wantCalls {
				t.Fatalf("перепись разошлась с содержимым: обращений=%d, ждали %d",
					census.Calls, tc.wantCalls)
			}
			if census.Classified != tc.wantCalls {
				t.Fatalf("классифицированных %d, а обращений %d — перепись сама себе противоречит",
					census.Classified, tc.wantCalls)
			}
		})
	}
}
