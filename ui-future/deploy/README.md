# kacho-ui-future Helm chart

The chart is named `uif` (not `ui`) — see the note in `Chart.yaml`: a second
same-named dependency of the umbrella made helm's dependency render
non-deterministic. Inside `deploy/helm/umbrella` the console is therefore
configured under the top-level key `uif:`. Helm does not reject an unknown
top-level key, so values written under `ui:` are silently ignored — the example
below is rendered by `deploy/console_readme_example_applies_test.go`, which
fails when any of its values does not reach the console.

It installs nine components, each a Deployment and a Service:

| component | values key | serves |
|---|---|---|
| `ui` | (root) | the module federation host and entrypoint used by ingress and port-forwarding |
| `ui-dashboard` | `dashboard` | `/dashboard-remote/assets/remoteEntry.js` |
| `ui-vpc` | `vpc` | `/vpc-remote/assets/remoteEntry.js` |
| `ui-iam` | `iam` | `/iam-remote/assets/remoteEntry.js` |
| `ui-nlb` | `nlb` | `/nlb-remote/assets/remoteEntry.js` |
| `ui-registry` | `registry` | `/registry-remote/assets/remoteEntry.js` |
| `ui-system` | `system` | `/system-remote/assets/remoteEntry.js` |
| `ui-compute` | `compute` | `/compute-remote/assets/remoteEntry.js` |
| `ui-storage` | `storage` | `/storage-remote/assets/remoteEntry.js` |

Build images from the `ui-future` directory, one per component:

```bash
for c in host dashboard vpc iam nlb registry system compute storage; do
  docker build -f "$c/Dockerfile" -t "kacho-ui-future-$c:dev" .
done
```

Standalone install:

```bash
helm upgrade --install uif ./deploy -n kacho --create-namespace
```

Under `deploy/helm/umbrella` (the console is off by default — `uif.enabled`):

```yaml
uif:
  enabled: true
  image: registry.example.com/kacho-ui-future-host:v0.1.0
  ingress:
    host: console.example.com
    tls:
      - hosts:
          - console.example.com
        secretName: console-example-tls
```

A remote's image, when empty, is derived from the host image by replacing
`host` with the component name (`kacho-ui-future-host:<tag>` →
`kacho-ui-future-dashboard:<tag>`). If your image names do not follow that
convention, set `uif.<component>.image` explicitly for each of the eight remotes.

The console requires TLS in front of it: the form cookies are `Secure`, and a
browser does not keep them on a plain-http origin. Declare `uif.ingress.tls`
or serve the console through its own TLS front (`uif.publicFront`).
