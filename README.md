# Steadybit extension-http

A [Steadybit](https://www.steadybit.com/) action implementation to check HTTP endpoints.

Learn about the capabilities of this extension in our [Reliability Hub](https://hub.steadybit.com/extension/com.steadybit.extension_http).

## Configuration

| Environment Variable                            | Helm value                | Meaning                                                                                                                                                                                              | required | default |
|-------------------------------------------------|---------------------------|------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|----------|---------|
| `STEADYBIT_EXTENSION_ENABLE_LOCATION_SELECTION` | `enableLocationSelection` | By default, the platform will select a random instance when executing actions from this extension. If you enable location selection, users can optionally specify the location via target selection. | no       | false   |

The extension supports all environment variables provided by [steadybit/extension-kit](https://github.com/steadybit/extension-kit#environment-variables).

## Installation

### Kubernetes

Detailed information about agent and extension installation in kubernetes can also be found in
our [documentation](https://docs.steadybit.com/install-and-configure/install-agent/install-on-kubernetes).

#### Recommended (via agent helm chart)

All extensions provide a helm chart that is also integrated in the
[helm-chart](https://github.com/steadybit/helm-charts/tree/main/charts/steadybit-agent) of the agent.

The extension is installed by default when you install the agent.

You can provide additional values to configure this extension.

Additional configuration options can be found in
the [helm-chart](https://github.com/steadybit/extension-http/blob/main/charts/steadybit-extension-http/values.yaml) of the
extension.

#### Alternative (via own helm chart)

If you need more control, you can install the extension via its
dedicated [helm-chart](https://github.com/steadybit/extension-http/blob/main/charts/steadybit-extension-http).

```bash
helm repo add steadybit-extension-http https://steadybit.github.io/extension-http
helm repo update
helm upgrade steadybit-extension-http \
    --install \
    --wait \
    --timeout 5m0s \
    --create-namespace \
    --namespace steadybit-agent \
    steadybit-extension-http/steadybit-extension-http
```

### Linux Package

Please use
our [agent-linux.sh script](https://docs.steadybit.com/install-and-configure/install-agent/install-on-linux-hosts)
to install the extension on your Linux machine. The script will download the latest version of the extension and install
it using the package manager.

After installing, configure the extension by editing `/etc/steadybit/extension-http` and then restart the service.

## Extension registration

Make sure that the extension is registered with the agent. In most cases this is done automatically. Please refer to
the [documentation](https://docs.steadybit.com/install-and-configure/install-agent/extension-discovery) for more
information about extension registration and how to verify.

## Proxy

A proxy configuration is currently not supported.

## Location Selection
When multiple HTTP extensions are deployed in different subsystems (e.g., multiple Kubernetes clusters), it can be tricky to ensure that the HTTP check is performed from the right location when testing cluster-internal URLs.
To solve this, you can activate the location selection feature.
Once you do that, the HTTP extension discovers itself as a client execution location.
When configuring the experiment, you can optionally define which extension's deployment should execute the HTTP check.
Also, the execution locations are part of Steadybit's environment concept, so you can assign permissions for execution locations.

### Migration Guideline
Before activating the location selection feature, be sure to follow these steps:
1. The installed agent version needs to be >= 2.0.47, and - only for on-prem customers - the platform version needs to be >=2.2.2
2. Activate the location selection via environment or helm variable when deploying the latest extension version (see [configuration options](#configuration).
3. Configure every environment that should be able to run HTTP checks by including the HTTP client location in the environment configuration.
	 One option is to add the statement `or target via the query language.type="com.steadybit.extension_http.client-location"` to your existing query.
	 You can also filter the available execution locations down, e.g., via the clustername by using `(target.type="com.steadybit.extension_http.client-location" and k8s.cluster-name="CLUSTER-NAME")`

