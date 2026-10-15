// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package artifactgates

import (
	"strings"
	"testing"
)

// TestNewmanSentAddressInjection_QueryOnlyInRawIsFound — инъекция в обе
// стороны: строка запроса только в `raw` (дефект kacho#3230) — находка с
// координатой; та же строка, разложенная в `query`, и та же строка в сегменте
// пути — законные близнецы, молчат.
func TestNewmanSentAddressInjection_QueryOnlyInRawIsFound(t *testing.T) {
	const head = `{"item":[{"name":"case","item":[{"name":"csrf","request":{"method":"GET","url":`
	const tail = `}}]}]}`
	cases := []struct {
		name, url string
		want      int
	}{
		{"строка запроса только в raw", `{"raw":"{{baseUrl}}/iam/v1/auth/csrf?form=step-up",` +
			`"host":["{{baseUrl}}"],"path":["iam","v1","auth","csrf"]}`, 1},
		{"строка запроса разложена в query", `{"raw":"{{baseUrl}}/iam/v1/auth/csrf?form=step-up",` +
			`"host":["{{baseUrl}}"],"path":["iam","v1","auth","csrf"],"query":[{"key":"form","value":"step-up"}]}`, 0},
		{"строка запроса в последнем сегменте пути", `{"raw":"{{baseUrl}}/geo/v1/regions?pageSize=1",` +
			`"host":["{{baseUrl}}"],"path":["geo","v1","regions?pageSize=1"]}`, 0},
		{"разложенное значение расходится с raw", `{"raw":"{{baseUrl}}/iam/v1/auth/csrf?form=step-up",` +
			`"host":["{{baseUrl}}"],"path":["iam","v1","auth","csrf"],"query":[{"key":"form","value":"login"}]}`, 1},
		{"адрес без строки запроса", `{"raw":"{{baseUrl}}/iam/v1/auth/csrf",` +
			`"host":["{{baseUrl}}"],"path":["iam","v1","auth","csrf"]}`, 0},
		{"хвостовая косая черта — вне предиката", `{"raw":"{{baseUrl}}/vpc/v1/networks/",` +
			`"host":["{{baseUrl}}"],"path":["vpc","v1","networks"]}`, 0},
		{"адрес строкой", `"{{baseUrl}}/iam/v1/auth/csrf?form=step-up"`, 0},
	}
	for _, c := range cases {
		var census urlAddressCensus
		got, err := urlAddressFindings("synthetic.postman_collection.json", []byte(head+c.url+tail), &census)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if len(got) != c.want {
			t.Errorf("%s: находок %d, ждали %d: %v", c.name, len(got), c.want, got)
			continue
		}
		if c.want > 0 && !strings.Contains(got[0], "synthetic.postman_collection.json /case/csrf") {
			t.Errorf("%s: находка без координаты элемента: %q", c.name, got[0])
		}
		if census.collections != 1 || census.requests != 1 {
			t.Errorf("%s: перепись коллекций %d, запросов %d — ждали 1 и 1", c.name, census.collections,
				census.requests)
		}
	}
}
