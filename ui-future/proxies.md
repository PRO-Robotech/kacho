# kacho-ui-future local proxies

The future host runs on Windows, but the kind cluster is in WSL. Keep the UI dev server on Windows and run `kubectl port-forward` in WSL.

## Required forwards

Run these in WSL:

```bash
chmod +x ./proxies.sh
./proxies.sh
```

The script starts all required port-forwards and stops them when you press `Ctrl+C`.

## После перезапуска Docker/kind

Здесь стояла процедура починки прав скриптом `heal-authz.sh`: перезапустить
загрузку внешнего движка прав, дождаться переката потребителей и переиграть
кортежи из базы iam.

**Ни скрипта, ни его предмета больше нет.** Внешний движок отношений снят
целиком (эпик #747, стадия S6): решение о доступе вычисляет форма поверх
собственной базы iam, эфемерного состояния у неё нет, и «переиграть кортежи»
означало бы переиграть саму базу. Сам скрипт при этом не работал ЗАДОЛГО до
снятия: он звал цель сборки `fga-bootstrap`, которой в дереве нет, и обрывался
на ней (`set -euo pipefail`), не доходя ни до одного кортежа. То есть процедура,
описанная здесь, не исполнялась ни разу с тех пор, как цель исчезла, — а
запускают такое именно тогда, когда доступ уже сломан.

Если после перезапуска kind консоль отвечает отказами вида
`iam.projectses.list`, предмет ищется в состоянии базы iam и в дренаже журнала
`kaname.fga_outbox`, а не в отдельном инструменте починки.

## Linux / WSL: run the whole console locally

`dev-federation.ps1` is PowerShell-only and names **four** remotes while the tree
has **eight**. `dev-federation.sh` derives the list from `host/vite.config.ts`
instead of repeating it, so a new remote cannot be silently left out:

```bash
./proxies.sh          # terminal 1 — port-forwards into kind
./dev-federation.sh   # terminal 2 — builds 8 remotes, serves them, starts the host
```

It builds every remote once, starts `dev:remote:watch` + `preview` per remote and
`npm run dev` for the host, then waits until each `remoteEntry.js` actually
answers before declaring the stand up. A remote that fails to build is a refusal
with its log path, not a partially-started stand.

Ports: host `5174`, remotes `4175…4182` in the order the host declares them.
Logs land in `.dev-logs/`.

Then start the federated UI from Windows PowerShell:

```powershell
cd D:\Repos\job\kacho\kacho-ui-future
npm run dev
```

Open:

```text
http://localhost:5174
```

The host consumes the dashboard through module federation. For this
`@originjs/vite-plugin-federation` setup, the host can run in Vite dev mode, but
the remote must expose built assets. `dev-federation.ps1` therefore runs:

```text
dashboard npm run dev:remote:watch  -> rebuilds dist on source changes
dashboard npm run preview           -> serves http://localhost:4175/assets/remoteEntry.js
host npm run dev                    -> serves http://localhost:5174
```

Do not use dashboard `npm run dev` on port `5175` as the host remote. In that
mode `/assets/remoteEntry.js` is served by Vite dev fallback and is not the
built federation remote entry.

## What Vite proxies

The host app uses relative browser URLs. `host/vite.config.ts` proxies them to the forwarded ports:

```text
/vpc/*                  -> http://localhost:8080
/compute/*              -> http://localhost:8080
/storage/*              -> http://localhost:8080
/geo/*                  -> http://localhost:8080
/nlb/*                  -> http://localhost:8080
/registry/*             -> http://localhost:8080
/iam/v1/*               -> http://localhost:8080
/operations/*           -> http://localhost:8080
/healthz, /readyz       -> http://localhost:8080
/.ory/kratos/public/*   -> http://localhost:4433
/self-service/*         -> http://localhost:4433
/login, /registration,
/recovery, /settings,
/verification, /error,
/consent, /logout       -> http://localhost:4300
/.ory/hydra/public/*    -> http://localhost:4444
/oauth2/*               -> http://localhost:4444
```

Frontend code should keep using relative paths:

```ts
fetch("/iam/v1/me")
fetch("/vpc/v1/networks")
fetch("/compute/v1/instances")
```

## Expected auth behavior

If `/vpc/v1/*` or `/compute/v1/*` returns `401` or `403`, the proxy is still working. That response came from `api-gateway`; it means the request reached the backend but is missing the browser session / access token / permissions.

The future host starts the Kratos browser login flow:

```text
/.ory/kratos/public/self-service/login/browser
```

After the real auth flow is wired into the new UI, protected API calls should use the same relative URLs and include the credentials/token expected by `api-gateway`.

## If Windows cannot reach WSL port-forwards

Usually `localhost:<port>` works from Windows to WSL. If it does not, bind port-forward to all interfaces in WSL:

```bash
kubectl -n kacho port-forward --address 0.0.0.0 svc/api-gateway 8080:8080
kubectl -n kacho port-forward --address 0.0.0.0 svc/kacho-umbrella-kratos-public 4433:80
kubectl -n kacho port-forward --address 0.0.0.0 svc/kratos-selfservice-ui 4300:3000
kubectl -n kacho port-forward --address 0.0.0.0 svc/kacho-umbrella-hydra-public 4444:4444
```

You can override proxy targets before starting Vite:

```powershell
$env:KACHO_API_BASE="http://localhost:8080"
$env:KACHO_KRATOS_BASE="http://localhost:4433"
$env:KACHO_KRATOS_UI_BASE="http://localhost:4300"
$env:KACHO_HYDRA_BASE="http://localhost:4444"
npm run dev
```
