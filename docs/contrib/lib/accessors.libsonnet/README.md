---
permalink: /
---

# accessors

```jsonnet
local accessors = import "accessors.libsonnet"
```

`accessors` implements accessors for Kubernetes manifests/ objects.

## Index

* [`fn getAnnotation(obj, annotation)`](#fn-getannotation)
* [`fn getLabel(obj, label)`](#fn-getlabel)
* [`fn getName(obj)`](#fn-getname)
* [`fn getNamespace(obj)`](#fn-getnamespace)
* [`fn inDelete(obj)`](#fn-indelete)

## Fields

### fn getAnnotation

```ts
getAnnotation(obj, annotation)
```

Gets the value of an annotation from an object. Null is returned if the annotation is not set.


### fn getLabel

```ts
getLabel(obj, label)
```

Gets the value of a label from an object. Null is returned if the label is not set.


### fn getName

```ts
getName(obj)
```

Gets the name of an object. Empty string is returned if the name is not set.


### fn getNamespace

```ts
getNamespace(obj)
```

Gets the namespace of an object. Empty string is returned if the namespace is not set.


### fn inDelete

```ts
inDelete(obj)
```

Checks if an object is in the process of being deleted.
