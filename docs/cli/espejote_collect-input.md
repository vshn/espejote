## espejote collect-input

<span style="color:yellow">ALPHA</span> Creates a input file for the <span style="background-color:blue"> render </span> command.

### Synopsis

<span style="color:yellow">ALPHA</span> Creates a input file for the <span style="background-color:blue"> render </span> command.

Connects to the the Kubernetes cluster in the current kubeconfig to collect the necessary data.

```
espejote collect-input path [flags]
```

### Examples

```
espejote collect-input path/to/managedresource.yaml > input.yaml
```

### Options

```
  -h, --help                             help for collect-input
      --jsonnet-library-namespace lib/   The namespace to look for shared (lib/) Jsonnet libraries in. (default "default")
  -n, --namespace string                 Namespace to use as the context for the ManagedResource. Overrides the given ManagedResource's namespace.
```

### SEE ALSO

* [espejote](espejote.md)	 - Espejote manages arbitrary resources in a Kubernetes cluster.

###### Auto generated by spf13/cobra
