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
* [`fn setOwnerReference(owner, obj, controller=false)`](#fn-setownerreference)

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


### fn setOwnerReference

```ts
setOwnerReference(owner, obj, controller=false)
```

Sets an owner reference on an object.
The owner reference is validated and an error message is returned if it's invalid.

This function assumes the owner reference is a static patch and thus does not check for existing owner references on the object.

If `controller` is true, the owner reference will be set as a controller reference by setting the `controller` and `blockOwnerDeletion` fields to true.

Returns an array of `[updatedObject, errorString]`.
If the owner reference is valid, `errorString` will be null.


```jsonnet
  local obj = errors.fromTuple(accessors.setOwnerReference(
    owner,
    {
      apiVersion: 'v1',
      kind: 'ConfigMap',
      metadata: {
        name: 'object',
      },
    },
  )).unpack();
```
