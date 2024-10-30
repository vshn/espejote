<p align="center">
  <img width=256px src="./assets/logo.png" alt="a goopher standing in front of a big mirror" /><br />
  The Espejote tool (which means 'big mirror' in Spanish) manages arbitrary resources in a Kubernetes cluster.
</p>

## Custom Resource Definition

The operator introduces a CRD called `ManagedResource` including a cluster-wide version called `ClusterManagedResource`.
The CRD defines a template for a Jsonnet snipped that gets evaluated and the resulting objects are applied to the cluster.
For the cluster-wide CRD an optional namespace selector will define the targeted namespaces.

### `ManagedResource`

The following example will copy all `ConfigMaps` in the namespace.

```yaml
apiVersion: espejo.appuio.io/v1alpha1
kind: ManagedResource
metadata:
  name: copy-configmap
  namespace: my-namespace
spec:
  context:
  - alias: cm
    apiVersion: v1
    kind: ConfigMap
    namespace: openshift-config
  template: |
    local cm = std.extVar('cm');
    [
      c {
        metadata: {
          name: 'copy-of-' + c.metadata.name,
        }
      },
      for c in cm.items
    ]
```

### `ClusterManagedResource`

The following example will copy all `ConfigMaps` in the selected namespaces.

```yaml
apiVersion: espejo.appuio.io/v1alpha1
kind: ClusterManagedResource
metadata:
  name: alice
spec:
  context:
  - alias: cm
    apiVersion: v1
    kind: ConfigMap
  namespaceSelector:
    matchNames:
    - openshift.*
  template: |
    local cm = std.extVar('cm');
    local ns = std.extVar('namespaces');

    local copy(n) = [
      c {
        metadata: {
          name: 'copy-of-' + c.metadata.name,
          namespace: n,
        }
      },
      for c in cm.items
      if c.metadata.namespace == n
    ];

    std.flattenArrays([
      copy(n.metadata.name)
      for n in ns.items
    ])
```

## Validating

For validating a created `ManagedResource` or `ClusterManagedResource` there are 3 commands.

* Validating parsed context:
  ```
  espejote validate context my_managed_resource.yaml

  ALIAS   KIND        NAMESPACE          NAME
  cm      configmap   openshift-config   admin-acks
  cm      configmap   openshift-config   admin-kubeconfig-client-ca
  cm      configmap   openshift-config   console-logo
  ...
  ```

* Validating parsed namespace selector:
  ```
  espejote validate selector my_managed_resource.yaml

  ALIAS       NAMESPACE
  namespace   openshift-controller-manager
  namespace   openshift-monitoring
  namespace   openshift-kni-infra
  ...
  ```

* Validating the Jsonnet snippet:
  ```
  espejote validate template my_managed_resource.yaml

  {
     "apiVersion": "v1",
     "data": {
        "ack-4.11-kube-1.25-api-removals-in-4.12": "true",
        "ack-4.12-kube-1.26-api-removals-in-4.13": "true",
        "ack-4.13-kube-1.27-api-removals-in-4.14": "true",
        "ack-4.14-kube-1.28-api-removals-in-4.15": "true"
     },
     "kind": "ConfigMap",
     "metadata": {
        "name": "copy-of-admin-acks",
        "namespace": "openshift-config"
     }
  }
  ...
  ```
