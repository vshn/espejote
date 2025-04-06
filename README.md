<p align="center">
  <img width=256px src="./assets/logo.png" alt="a goopher standing in front of a big mirror" /><br />
  The Espejote tool ('big mirror' in Spanish) manages arbitrary resources in a Kubernetes cluster.<br />
  It allows a GitOps workflow while still being able to depend on in-cluster resources.<br /><br />
  <a href="https://kb.vshn.ch/oc4/references/architecture/espejote-in-cluster-templating-controller.html">Espejote: An in-cluster templating controller</a>
</p>

## Installation

```sh
kubectl apply -k config/crd
kubectl apply -k config/default
```

## Usage

Espejote manages resources by server-side applying rendered Jsonnet manifests to the cluster.
It allows fine-grained control over external context used to rendering the resources and the triggers that cause the resources to be applied.

`espejote` CLI docs are available [here](./docs/cli/espejote.md).

API (CRD) documentation is available [here](./docs/api.adoc).

`espejote.libsonnet` documentation is available [here](./docs/lib/README.md).

Annotated examples are available:
- [Admission: OpenShift 4 Cluster Autoscaler Patch](./docs/annotated-examples/admission/ocp-cluster-autoscaler-patch.adoc)
- [ManagedResource: OpenShift 4 Node Disruption Policies](./docs/annotated-examples/managedresource/node-disruption-policies.adoc)
- We're working on more examples, stay tuned!
