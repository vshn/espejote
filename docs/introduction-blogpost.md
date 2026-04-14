# Espejote: A GitOps journey

_**Espejote (big mirror) manages arbitrary resources in a Kubernetes cluster. Built from the ground up to take advantage of Server-Side Apply and Jsonnet templating.**_

VSHN manages a large fleet of Kubernetes clusters for our customers, and we try to automate as much as possible to keep our operations efficient and sustainable.
We use GitOps principles, but sometimes external state needs to be merged into the desired state defined in Git.
This GitOps journey took us from Ansible playbooks directly applying YAML, to various operators, to bash "reconcilers", and finally to Espejote, our shiny new GitOps operator.

## Chapter 1: The Ansible era and first operator attempts

In the beginning, we used Ansible playbooks and custom roles to manage our OpenShift 3 Kubernetes clusters. We had a set of YAML files that defined the desired state of our clusters, and we would run Ansible playbooks to apply those YAML files to the clusters. This worked, but it was not very efficient. We had to run the playbooks manually, and if we forgot to run them, the clusters would drift from the desired state.

The collection of roles was nicknamed "mungg", the Swiss German word for "marmot". Nobody seems to know why, but it stuck.

We were just getting into writing operators and developed [espejo](https://github.com/vshn/espejo) to quickly sync resources between namespaces.
It was the very early days of our operator journey.

## Chapter 2: The sea of operators and tears

To solve the problem of manual intervention (and because we migrated to OpenShift 4, where the install procedure doesn't use Ansible anymore), we started looking into Kubernetes operators.
It can't be that hard to patch a Kubernetes manifest. Right? Wrong.
Some of the operators were buggy, some of them were not flexible enough, some of them loved to randomly go into reconcile loops, and most of them used too many resources. Some of them crashed our API servers. We started with [resource-locker-operator](https://github.com/redhat-cop/resource-locker-operator), migrated to [patch-operator](https://github.com/redhat-cop/patch-operator), generated outages with [Kyverno](https://kyverno.io/), and tested all other policy engines we could find. Kubewarden was the only one we really liked, but the cluster context API was not yet flexible enough for our use cases.

Espejo had been a good start, but we did not yet have the experience to build well-designed operators.
It showed.
Every event triggered a full reconciliation of every resource, so syncing slowed down dramatically on larger clusters.
We missed a lot of flexibility.

## Chapter 3: Getting desperate for safe landings

We were fed up with the constant bugs and breaking changes in Kyverno, and patch-operator was barely maintained. Espejo was at its limits.

Desperate times called for desperate measures, so we started using an amalgamation of [bash "reconcilers"](https://github.com/appuio/component-openshift4-console/blob/740628ebf3822ea82a64fade1e42eb9ff52f67c7/component/scripts/reconcile-console-secret.sh)—hacks with cron jobs, tiny custom controllers, and pre-processing resources in Project Syn.

We were using Jsonnet more and more. [Project Syn](https://syn.tools/syn/index.html) components primarily use Jsonnet. We use Jsonnet for our [cloudscale machine-api provider](https://github.com/appuio/machine-api-provider-cloudscale), for our [SSO solution](https://github.com/projectsyn/lieutenant-keycloak-idp-controller), and many other projects.

A growing issue were our heavily patched OpenShift alerting rules.
We curate upstream rules and only enable the ones we need. Some are heavily patched.
Every OpenShift release the upstream definitions are moved around and are sometimes only available embedded into Go code.
We needed something that was able to patch rules already deployed in the cluster, as this was the only stable interface we had.

## Chapter 4: Espejote, the shiny new GitOps operator

Bolstered by our growing operator experience and our love for Jsonnet, we decided to build our own operator to rule them all.
We wanted something that was flexible, efficient, and easy to use. We wanted something that could handle all our use cases, from syncing resources between namespaces to patching OpenShift alerting rules.

Espejote is the result of that journey. It merges cluster state with GitOps principles, using Jsonnet to define the desired state of our clusters. It efficiently caches cluster state, and the reconcile trigger logic is explicitly defined. Sane controller-runtime rate limits apply. Jsonnet allows a huge amount of flexibility, and native server-side apply makes adding and removing keys a breeze. Every Espejote "resource manager" - the dynamic controller spawned for a config unit - uses its own ServiceAccount for least privilege.

Espejote is the operator we always wanted, and we are excited to share it with the world.

## What is Espejote?

Espejote is a Kubernetes operator allowing you to manage arbitrary resources in a Kubernetes cluster.
It can mix GitOps principles with in-cluster state.

## Why Espejote?

There are plenty of similar tools (and policy engines), but Espejote sets itself apart by focusing on three core pillars:

### 1. Powered by Jsonnet

Espejote uses Jsonnet as its templating engine.
Unlike YAML combined with Go templates, Jsonnet treats the configuration as a data structure.
It understands objects, arrays, and strings.
It can’t accidentally generate broken YAML because Jsonnet ensures the internal data structure is valid before it ever exports the final file.

### 2. Native Server-Side Apply

Espejote is built from the ground up to leverage server-side apply (SSA).
This means Espejote plays nicely with other controllers and operators.
It can manage a single annotation or an entire resource; SSA ensures that the changes are merged without stomping on other tools.

### 3. Reliability

Reliability isn't an afterthought.
Espejote was born out of the frustration of watching operators enter infinite reconcile loops or crash clusters. It features:
* Sane rate limiting and backoff strategies.
* Every configuration unit or "resource manager" runs its own dynamically spawned controller, so a misbehaving unit won't affect others.
* Least privilege: Every resource manager runs with its own ServiceAccount.
* Explicit control: There are no implicit watches or "magic" triggers. You have complete control over what gets reconciled and when.

## Real-World Use Cases

What can you actually do with Espejote? Here are a few ways VSHN is using it in production:

* Secret Syncing: Automatically replicate specific secrets (like image pull secrets or certificates) across multiple namespaces.
* Autoscaler Patching: Patching the OpenShift Cluster Autoscaler using Admission Webhooks.
* Alerting Rule Management: Curate and patch OpenShift alerting rules across different cluster versions.

## The Future: WASM and Beyond

The roadmap includes a `kro`-like API builder for easy custom resource creation and support for WebAssembly plugins, which will allow developers to write custom logic in almost any language and run it safely within the Espejote controller.

## Getting Started

* **Website:** [espejote.io](https://espejote.io)
* **GitHub:** [vshn/espejote](https://github.com/vshn/espejote)
* **Documentation:** [Check out the annotated examples](https://github.com/vshn/espejote/tree/main/docs/annotated-examples) to see how Espejote can be used in real-world scenarios.
* **Real-World Use Cases:** [Network policy management](https://github.com/projectsyn/component-networkpolicy/blob/master/tests/golden/defaults/networkpolicy/networkpolicy/03_managedresource.yaml), a [Gateway listener manager](https://github.com/appuio/component-airlock-microgateway-operator/blob/9bca8eceab66bde0d49614d58fc4cb5d6bd5e337/tests/golden/defaults/airlock-microgateway-operator/airlock-microgateway-operator/02_resources/80_gateway_listener_manager_managedresource.yaml), [HTTPRoute certificate management](https://github.com/appuio/component-airlock-microgateway-operator/blob/9bca8eceab66bde0d49614d58fc4cb5d6bd5e337/tests/golden/defaults/airlock-microgateway-operator/airlock-microgateway-operator/02_resources/80_httproute_certificate_manager_managedresource.yaml), and [many more](https://github.com/search?q=%22kind%3A+ManagedResource%22+%28org%3Avshn+OR+org%3Aappuio+OR+org%3Aprojectsyn%29+language%3AYAML&type=code).

### Example

This example `ManagedResource` patches the RedHat OperatorHub config singleton to disable all default sources.
It shows the simplest usecase of unconditionally patching a static manifest.
More complex use cases can be found in the above getting started section.

```yaml
apiVersion: espejote.io/v1alpha1
kind: ManagedResource
metadata:
  annotations:
  name: disable-default-sources
  namespace: openshift-marketplace
spec:
  serviceAccountRef:
    name: disable-default-sources
  triggers:
    - name: operatorhub
      watchResource:
        apiVersion: config.openshift.io/v1
        kind: OperatorHub
        name: cluster
  template: |-
    {
        "apiVersion": "config.openshift.io/v1",
        "kind": "OperatorHub",
        "metadata": {
            "name": "cluster"
        },
        "spec": {
            "disableAllDefaultSources": true
        }
    }
```
