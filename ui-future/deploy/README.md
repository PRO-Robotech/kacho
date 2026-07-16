# kacho-ui-future Helm chart

This chart is intentionally named `ui` so `kacho-deploy/helm/umbrella` can use it
as a drop-in replacement for the legacy `kacho-ui/deploy` dependency.

It installs:

- `ui` Deployment and Service: the module federation host and compatibility
  entrypoint used by ingress, port-forwarding, and existing `kacho-deploy`
  values.
- `ui-dashboard` Deployment and Service: the dashboard remote consumed at
  `/dashboard/assets/remoteEntry.js`.
- `ui-vpc` Deployment and Service: the VPC remote consumed at
  `/vpc-remote/assets/remoteEntry.js`.
- `ui-iam` Deployment and Service: the IAM remote consumed at
  `/iam-remote/assets/remoteEntry.js`.

Build images from this repository root:

```bash
docker build -f host/Dockerfile -t kacho-ui-future-host:dev .
docker build -f dashboard/Dockerfile -t kacho-ui-future-dashboard:dev .
docker build -f vpc/Dockerfile -t kacho-ui-future-vpc:dev .
docker build -f iam/Dockerfile -t kacho-ui-future-iam:dev .
```

Standalone install:

```bash
helm upgrade --install ui ./deploy -n kacho --create-namespace
```

`kacho-deploy` compatibility:

```yaml
ui:
  image: docker.io/prorobotech/kacho-ui-future-host:<tag>
  ingress:
    host: localhost
```

If your image names do not follow the `host` -> `dashboard` / `vpc` / `iam`
convention, set `ui.dashboard.image`, `ui.vpc.image`, and `ui.iam.image` explicitly.
