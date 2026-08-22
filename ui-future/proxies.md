# kacho-ui-future local proxies

The future host runs on Windows, but the kind cluster is in WSL. Keep the UI dev server on Windows and run `kubectl port-forward` in WSL.

## Required forwards

Run these in WSL:

```bash
chmod +x ./proxies.sh
./proxies.sh
```

The script starts all required port-forwards and stops them when you press `Ctrl+C`.

## Здесь был раздел о восстановлении прав — предмета у него нет

Раздел объяснял, как «починить OpenFGA» после перезапуска стенда, и звал
`heal-authz.sh`. **Внешнего движка прав в дереве нет**: он снят целиком — ни
чарта, ни базы, ни цели бутстрапа. Скрипт восстанавливал состояние, которого не
бывает.

Отдельно он был мёртв и по построению: путь к каталогу развёртывания выводился
из имени времён полирепо (`../kacho-deploy`), и `cd` в несуществующий каталог под
`set -e` ронял его первой же строкой. То есть умолчание не работало ни при каком
состоянии стенда.

Снят вместе с предметом, а не починен: чинить координату у инструмента, чья
работа больше не существует, значит оставить в дереве живой на вид путь
восстановления, которого нет.


## What Vite proxies

The host app uses relative browser URLs. `host/vite.config.ts` proxies them to the forwarded ports:

```text
/vpc/*                  -> http://localhost:8080
/compute/*              -> http://localhost:8080
/nlb/*                  -> http://localhost:8080
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
