---
title: Espejote
description: Welcome to Espejote - an in-cluster templating controller.
showHeader: false
layout: "single"
---

{{< columns count=2 >}}
{{< column >}}
# Espejote - an in-cluster templating controller.

{{< intro >}}
Espejote ('big mirror' in Spanish) manages arbitrary resources in a Kubernetes cluster.
Built from the ground up to take advantage of [Server Side Apply](https://kubernetes.io/docs/reference/using-api/server-side-apply/) and [Jsonnet](https://jsonnet.org/) templating.
{{< /intro >}}

{{< /column >}}
{{< column >}}
{{< spacer >}}
{{< img src="/img/mascot.png" loading="eager" >}}
{{< /column >}}
{{< /columns >}}

{{< columns count=3 >}}

{{< column >}}
### Safe and robust
We built Espejote to be safe and robust for production use in large clusters after growing tired of operators randomly entering reconcile loops and crashing our clusters.

Espejote uses sane rate limiting and backoff strategies.
It builds on the battle-tested [controller-runtime](https://github.com/kubernetes-sigs/controller-runtime) library.

Every resource manager runs with its own ServiceAccount for least privilege.

No implicit watches or triggers -- you are in complete control of what gets reconciled and when.
{{< /column >}}

{{< column >}}
### Swiss Army knife
Espejote helped VSHN replace multiple specialized operators, patch-operator, Kyverno, and Crossplane.

An API builder and WASM plugins are coming soon.
{{< /column >}}

{{< column >}}
### Backed by Jsonnet
Espejote uses Jsonnet as the templating language, arguably the best templating language for Kubernetes.

The use of SSA means Espejote works well with other tools and operators in the cluster.
Manage a single annotation or full resources with ease.
{{< /column >}}

{{< /columns >}}
{{< spacer 20 >}}

{{< cards count=3 >}}
{{< card >}}
#### See all features
Check out our annotated examples to get a feel for what is possible. 🛠️ More examples coming soon.

{{< spacer 5 >}}
{{< button link="https://github.com/vshn/espejote/blob/main/docs/annotated-examples/managedresource/node-disruption-policies.adoc" text="Examples" >}}
{{< /card >}}

{{< card >}}
#### Roadmap
See what's next for Espejote and what features are planned.
<!--- force button alignment with left card -->
<br>

{{< spacer 5 >}}
{{< button link="/roadmap" text="Roadmap" >}}
{{< /card >}}

{{< card >}}
#### Free and open source
Help us shape the future of Espejote by contributing on GitHub.
<!--- force button alignment with left card -->
<br>

{{< spacer 5 >}}
{{< button link="https://github.com/vshn/espejote" text="Contribute" >}}
{{< /card >}}

{{< /cards >}}
{{< spacer 20 >}}
