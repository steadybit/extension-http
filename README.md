# Steadybit extension-http

A [Steadybit](https://www.steadybit.com/) action implementation to check HTTP endpoints.

Learn about the capabilities of this extension in our [Reliability Hub](https://hub.steadybit.com/extension/com.steadybit.extension_http).

## Configuration

The extension supports all environment variables provided by [steadybit/extension-kit](https://github.com/steadybit/extension-kit#environment-variables).

## Installation


### Using Helm in Kubernetes

```sh
helm repo add steadybit-extension-http https://steadybit.github.io/extension-http
helm repo update
helm upgrade steadybit-extension-http \
    --install \
    --wait \
    --timeout 5m0s \
    --create-namespace \
    --namespace steadybit-extension \
    steadybit-extension-http/steadybit-extension-http
```

### Using Docker

This extension is by default deployed using our [outpost.sh docker compose script](https://docs.steadybit.com/install-and-configure/install-outpost-agent-preview/install-as-docker-container).

Or you can run it manually:

```sh
docker run \
  --rm \
  -p 8085 \
  --name steadybit-extension-http \
  ghcr.io/steadybit/extension-http:latest
```

### Linux Package

Please use our [outpost-linux.sh script](https://docs.steadybit.com/install-and-configure/install-outpost-agent-preview/install-on-linux-hosts) to install the extension on your Linux machine.
The script will download the latest version of the extension and install it using the package manager.

## Register the extension

Make sure to register the extension at the steadybit platform. Please refer to
the [documentation](https://docs.steadybit.com/integrate-with-steadybit/extensions/extension-installation) for more information.

## Proxy

A proxy configuration is currently not supported.
