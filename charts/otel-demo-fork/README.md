# otel-demo-fork Helm Chart

This chart installs [ryanfaircloth/opentelemetry-demo](https://github.com/ryanfaircloth/opentelemetry-demo),
a fork of the [OpenTelemetry Demo](https://github.com/open-telemetry/opentelemetry-demo),
in a Kubernetes cluster. It is derived from the upstream
[opentelemetry-demo chart](https://github.com/open-telemetry/opentelemetry-helm-charts)
and is versioned independently of it.

## Prerequisites

- Kubernetes 1.24+
- Helm 4.0+
- A [Gateway API](https://gateway-api.sigs.k8s.io/) controller and `Gateway`
  (optional, only needed to expose components externally — see
  [examples/public-hosted-httproute](examples/public-hosted-httproute)). With
  neither, use `kubectl port-forward` per service; see `templates/NOTES.txt`.

## Installing the Chart

The chart is published as an OCI artifact to GHCR. To install the chart with
the release name my-otel-demo, run the following command:

```console
helm install my-otel-demo oci://ghcr.io/ryanfaircloth/charts/otel-demo-fork --version 0.5.1
```

## Upgrading

See [UPGRADING.md](UPGRADING.md).

## OpenShift

Installing the chart on OpenShift requires the following additional steps:

1. Create a new project:

    ```console
    oc new-project otel-demo-fork
    ```

2. Create a new service account:

    ```console
    oc create sa otel-demo-fork
    ```

3. Add the service account to the `anyuid` SCC (may require cluster admin):

    ```console
    oc adm policy add-scc-to-user anyuid -z otel-demo-fork
    ```

4. Install the chart with the following command:

    ```console
    helm install my-otel-demo oci://ghcr.io/ryanfaircloth/charts/otel-demo-fork \
        --version 0.5.1 \
        --namespace otel-demo-fork \
        --set serviceAccount.create=false \
        --set serviceAccount.name=otel-demo-fork
    ```

## Chart Parameters

Chart parameters are separated in 3 general sections:

- Default - Used to specify defaults applied to all demo components
- Components - Used to configure the individual components (microservices) for
the demo
- Sub-charts - Configuration for the OpenTelemetry Collector sub-chart

### Default parameters (applied to all demo components)

| Property                               | Description                                                                               | Default                                              |
|----------------------------------------|-------------------------------------------------------------------------------------------|------------------------------------------------------|
| `default.env`                          | Environment variables added to all components                                             | Array of several OpenTelemetry environment variables |
| `default.envOverrides`                 | Used to override individual environment variables without re-specifying the entire array. | `[]`                                                 |
| `default.image.repository`             | Demo components image name                                                                | `otel/demo`                                          |
| `default.image.tag`                    | Demo components image tag (leave blank to use app version)                                | `nil`                                                |
| `default.image.pullPolicy`             | Demo components image pull policy                                                         | `IfNotPresent`                                       |
| `default.image.pullSecrets`            | Demo components image pull secrets                                                        | `[]`                                                 |
| `default.replicas`                     | Number of replicas for each component                                                     | `1`                                                  |
| `default.schedulingRules.nodeSelector` | Node labels for pod assignment                                                            | `{}`                                                 |
| `default.schedulingRules.affinity`     | Man of node/pod affinities                                                                | `{}`                                                 |
| `default.schedulingRules.tolerations`  | Tolerations for pod assignment                                                            | `[]`                                                 |
| `default.securityContext`              | Demo components container security context                                                | `{}`                                                 |
| `serviceAccount.annotations`           | Annotations for the serviceAccount                                                        | `{}`                                                 |
| `serviceAccount.create`                | Whether to create a serviceAccount or use an existing one                                 | `true`                                               |
| `serviceAccount.name`                  | The name of the ServiceAccount to use for demo components                                 | `""`                                                 |

### Component parameters

The OpenTelemetry demo contains several components (microservices). Each
component is configured with a common set of parameters. All components will
be defined within `components.[NAME]` where `[NAME]` is the name of the demo
component.

This chart has no reverse proxy and no Ingress support; the only way to
expose a component externally is a Gateway API `HTTPRoute`. Each
component's HTTPRoute always targets that component's own Service, so
exposing several components under one hostname (the way `frontend-proxy`
used to, including the path-based routing that makes the webstore's images
resolve) means giving each of their HTTPRoutes the same `parentRefs` and
`hostnames` with different path matches — Gateway API merges routes
attached to the same Gateway/hostname the same way multiple Ingress objects
merge on one host. See
[examples/public-hosted-httproute](examples/public-hosted-httproute) for a
worked example, and `templates/NOTES.txt` for the default per-service
port-forward commands.

> **Note**
> The following parameters require a `components.[NAME].` prefix where `[NAME]`
> is the name of the demo component

| Parameter                               | Description                                                                              | Default                                                       |
|-----------------------------------------|------------------------------------------------------------------------------------------|---------------------------------------------------------------|
| `enabled`                               | Is this component enabled                                                                | `true`                                                        |
| `useDefault.env`                        | Use the default environment variables in this component                                  | `true`                                                        |
| `imageOverride.repository`              | Name of image for this component                                                         | Defaults to the overall default image repository              |
| `imageOverride.tag`                     | Tag of the image for this component                                                      | Defaults to the overall default image tag                     |
| `imageOverride.pullPolicy`              | Image pull policy for this component                                                     | `IfNotPresent`                                                |
| `imageOverride.pullSecrets`             | Image pull secrets for this component                                                    | `[]`                                                          |
| `service.type`                          | Service type used for this component                                                     | `ClusterIP`                                                   |
| `service.port`                          | Service port used for this component                                                     | `nil`                                                         |
| `service.nodePort`                      | Service node port used for this component                                                | `nil`                                                         |
| `service.annotations`                   | Annotations to add to the component's service                                            | `{}`                                                          |
| `ports`                                 | Array of ports to open for deployment and service of this component                      | `[]`                                                          |
| `env`                                   | Array of environment variables added to this component                                   | Each component will have its own set of environment variables |
| `envOverrides`                          | Used to override individual environment variables without re-specifying the entire array | `[]`                                                          |
| `replicas`                              | Number of replicas for this component                                                    | `1` for kafka, and redis ; `nil` otherwise                    |
| `resources`                             | CPU/Memory resource requests/limits                                                      | Each component will have a default memory limit set           |
| `schedulingRules.nodeSelector`          | Node labels for pod assignment                                                           | `{}`                                                          |
| `schedulingRules.affinity`              | Man of node/pod affinities                                                               | `{}`                                                          |
| `schedulingRules.tolerations`           | Tolerations for pod assignment                                                           | `[]`                                                          |
| `securityContext`                       | Container security context                                                               | `{}`                                                          |
| `podSecurityContext`                    | Pod security context s                                                                   | `{}`                                                          |
| `podLabels`                             | Pod labels for this component                                                            | `{}`                                                          |
| `podAnnotations`                        | Pod annotations for this component                                                       | `{}`                                                          |
| `httpRoute.enabled`                     | Enable the creation of a Gateway API HTTPRoute                                           | `false`                                                       |
| `httpRoute.annotations`                 | Annotations to add to the HTTPRoute                                                      | `{}`                                                          |
| `httpRoute.parentRefs`                  | Array of Gateway(s) this HTTPRoute attaches to                                           | `[]`                                                          |
| `httpRoute.hostnames`                   | Array of hostnames this HTTPRoute matches                                                | `[]`                                                          |
| `httpRoute.rules`                       | Array of routing rules; each backendRef targets this component's Service on `.port`      | `[]`                                                          |
| `httpRoute.rules[].rewritePath`         | Optional path-prefix rewrite applied before forwarding to the backend                    | `nil`                                                         |
| `httpRoute.additionalHTTPRoutes`        | Array of additional HTTPRoutes to add                                                    | `[]`                                                          |
| `httpRoute.additionalHTTPRoutes[].name` | Each additional HTTPRoute needs to have a unique name                                    | `nil`                                                         |
| `command`                               | Command & arguments to pass to the container being spun up for this service              | `[]`                                                          |
| `additionalVolumeMounts`                | Array of Volumes that will be mounted                                                    | `[]`                                                          |
| `mountedConfigMaps[].name`              | Name of the Volume that will be used for the ConfigMap mount                             | `nil`                                                         |
| `mountedConfigMaps[].mountPath`         | Path where the ConfigMap data will be mounted                                            | `nil`                                                         |
| `mountedConfigMaps[].subPath`           | SubPath within the mountPath. Used to mount a single file into the path.                 | `nil`                                                         |
| `mountedConfigMaps[].existingConfigMap` | Name of the existing ConfigMap to mount                                                  | `nil`                                                         |
| `mountedConfigMaps[].data`              | Contents of a ConfigMap. Keys should be the names of the files to be mounted.            | `{}`                                                          |
| `mountedEmptyDir[].name`                | Name of the EmptyDir volume that will be used for the volume mount                       | `nil`                                                         |
| `mountedEmptyDir[].mountPath`           | Path where the EmptyDir data will be mounted                                             | `nil`                                                         |
| `mountedEmptyDir[].subPath`             | SubPath within the mountPath. Used to mount a single file into the path.                 | `nil`                                                         |
| `initContainers`                        | Array of init containers to add to the pod                                               | `[]`                                                          |
| `initContainers[].name`                 | Name of the init container                                                               | `nil`                                                         |
| `initContainers[].image`                | Image to use for the init container                                                      | `nil`                                                         |
| `initContainers[].command`              | Command to run for the init container                                                    | `nil`                                                         |
| `sidecarContainers`                     | Array of sidecar containers to add to the pod                                            | `[]`                                                          |
| `additionalVolumes`                     | Array of additional volumes to add to the pod                                            | `[]`                                                          |

### Sub-charts

This chart depends on a single sub-chart, the OpenTelemetry Collector, which
runs as the app's own instrumentation agent. Parameters for it can be
specified within its top-level `opentelemetry-collector` key. This chart
overrides some of the sub-chart's parameters by default; the overridden
parameters are specified below.

#### OpenTelemetry Collector

> **Note**
> The following parameters have a `opentelemetry-collector.` prefix.

| Parameter      | Description                                     | Default                         |
|----------------|-------------------------------------------------|---------------------------------|
| `enabled`      | Install the OpenTelemetry collector             | `true`                          |
| `nameOverride` | Name that will be used by the sub-chart release | `otel-collector`                |
| `mode`         | The Deployment or Daemonset mode                | `deployment`                    |
| `resources`    | CPU/Memory resource requests/limits             | 200Mi memory limit              |
| `service.type` | Service Type to use                             | `ClusterIP`                     |
| `config`       | OpenTelemetry Collector configuration           | Configuration required for demo |

This chart intentionally does not bundle an observability backend (tracing,
metrics, or log storage/UI) — only the app and its own collector agent.
`opentelemetry-collector.config.exporters."otlp/observability-backend".endpoint`
has no default and is **required** — `values.schema.json` fails
`helm lint`/`template`/`install` if it's left blank. Point it at your
platform's existing backend; see
[examples/bring-your-own-observability](examples/bring-your-own-observability).

#### OpenTelemetry Collector HTTPRoute

The `opentelemetry-collector` sub-chart has no native Gateway API support, so
this chart adds its own HTTPRoute for it (`templates/collector-httproute.yaml`),
configured independently via a top-level `otelCollectorHTTPRoute` key (same
shape as `components.[NAME].httpRoute`, including `rewritePath`). This is how
a browser reaches the collector's `otlp-http` receiver directly for the
frontend's client-side trace export; see
[examples/public-hosted-httproute](examples/public-hosted-httproute).

#### OpenTelemetry Collector via the Operator

As an alternative to the sub-chart's own Deployment/DaemonSet, setting
`otelCollectorOperatorCR.enabled: true` (and `opentelemetry-collector.enabled:
false`, since the two are mutually exclusive - the chart fails fast if both
are on) renders an `OpenTelemetryCollector` custom resource instead
(`templates/opentelemetrycollector-cr.yaml`) and lets the
[OpenTelemetry Operator](https://github.com/open-telemetry/opentelemetry-operator)
reconcile the actual collector workload from it. This requires the Operator
(and its CRDs) to already be installed in the cluster - the same one this
chart already assumes provides the `Instrumentation` CR used by
ad/cart/fraud-detection's auto-instrumentation. The CR reuses
`opentelemetry-collector.config`, `.image`, `.mode` and `.resources`, so
pipeline/exporter overrides work identically in either mode; only the
deployment mechanism changes. The sub-chart's presets (`hostMetrics`,
`kubernetesAttributes`, `kubeletMetrics`, `clusterMetrics`,
`annotationDiscovery`) have no CRD equivalent, so switching to this mode means
configuring any receivers/RBAC they provided yourself. See
[examples/operator-managed-collector](examples/operator-managed-collector).
