<p align="center">
  <img width=256px src="./assets/mascot.png" alt="a goopher standing in front of a big mirror" /><br />
  The Espejote tool ('big mirror' in Spanish) manages arbitrary resources in a Kubernetes cluster.<br />
  It allows a GitOps workflow while still being able to depend on in-cluster resources.<br /><br />
  <a href="https://kb.vshn.ch/oc4/references/architecture/espejote-in-cluster-templating-controller.html">Espejote: An in-cluster templating controller</a>
</p>

## Installation

### In-cluster using `kubectl`

```sh
kubectl apply -k config/crd
kubectl apply -k config/default
```

#### Image signature verification

Espejote images are signed using [Cosign](https://github.com/sigstore/cosign). Espejote uses the [Keyless signing](https://docs.sigstore.dev/cosign/signing/overview/) feature of Cosign using the GitHub Action token for attestation.

You can verify the image signatures using the Cosign CLI:

```sh
TAG=vX.X.X
cosign verify "ghcr.io/vshn/espejote:${TAG}" \
  --certificate-identity "https://github.com/vshn/espejote/.github/workflows/release.yml@refs/tags/${TAG}" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com"
```

### CLI using Homebrew

Works on macOS and Linux.

```sh
brew install vshn/tap/espejote
```

### CLI using `go get`

```sh
go install github.com/vshn/espejote@latest
```

## Usage

Espejote manages resources by server-side applying rendered Jsonnet manifests to the cluster.
It allows fine-grained control over external context used to rendering the resources and the triggers that cause the resources to be applied.

`espejote` CLI docs are available [here](./docs/cli/espejote.md).

API (CRD) documentation is available [here](./docs/api.adoc).

`espejote.libsonnet` documentation is available [here](./docs/lib/README.md).

Annotated examples are available:
- [Admission: OpenShift 4 Cluster Autoscaler Patch](./docs/annotated-examples/admission/ocp-cluster-autoscaler-patch.adoc)
- [ManagedResource: Sync secrets between Kubernetes namespaces](./docs/annotated-examples/managedresource/sync-secrets.adoc)
- [ManagedResource: OpenShift 4 Node Disruption Policies](./docs/annotated-examples/managedresource/node-disruption-policies.adoc)
- We're working on more examples, stay tuned!

The original idea and design document is available [here](https://kb.vshn.ch/oc4/references/architecture/espejote-in-cluster-templating-controller.html).
