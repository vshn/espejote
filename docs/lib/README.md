---
permalink: /
---

# espejote

```jsonnet
local espejote = import "espejote.libsonnet"
```

`espejote` implements accessors for espejote features.

## Index

* [`fn context()`](#fn-context)
* [`fn markForDelete(obj, gracePeriodSeconds, propagationPolicy, preconditionUID, preconditionResourceVersion)`](#fn-markfordelete)
* [`fn triggerData()`](#fn-triggerdata)
* [`fn triggerName()`](#fn-triggername)
* [`obj ALPHA`](#obj-alpha)
  * [`obj ALPHA.admission`](#obj-alphaadmission)
    * [`fn admissionRequest()`](#fn-alphaadmissionadmissionrequest)
    * [`fn allowed(msg='')`](#fn-alphaadmissionallowed)
    * [`fn assertPatch(msg, obj='#admissionRequest().object')`](#fn-alphaadmissionassertpatch)
    * [`fn denied(msg='')`](#fn-alphaadmissiondenied)
    * [`fn jsonPatchOp(op, path, value)`](#fn-alphaadmissionjsonpatchop)
    * [`fn patched(msg, patches)`](#fn-alphaadmissionpatched)

## Fields

### fn context

```ts
context()
```

Gets the context object. Always a non-null object with the `context[].name` value as keys.


### fn markForDelete

```ts
markForDelete(obj, gracePeriodSeconds, propagationPolicy, preconditionUID, preconditionResourceVersion)
```

Marks an object for deletion.
The object will be deleted by the controller.
NotFound errors are ignored.
Deletion options can be passed as optional arguments:
- gracePeriodSeconds: number, the grace period for the deletion
- propagationPolicy: string, the deletion propagation policy (Background, Foreground, Orphan)
- preconditionUID: string, the UID of the object that must match for deletion
- preconditionResourceVersion: string, the resource version of the object that must match for deletion

```jsonnet
esp.markForDelete({
  apiVersion: 'v1',
  kind: 'ConfigMap',
  metadata: {
    name: 'cm-to-delete',
    namespace: 'target-namespace',
  },
})
```


### fn triggerData

```ts
triggerData()
```

Gets data added to the trigger by the controller.
Currently only available for WatchResource triggers.
WatchResource triggers add the full object that triggered the template to the trigger data under the key `resource`.


### fn triggerName

```ts
triggerName()
```

Returns the name of the trigger that caused the template to be called or null if unknown


## obj ALPHA

`ALPHA` is where future features are tested.
These features are not yet stable and may change in the future, even without a major version bump.


## obj ALPHA.admission

`admission` contains functions for validating and mutating objects.


### fn ALPHA.admission.admissionRequest

```ts
admissionRequest()
```

`admissionRequest` allows access to the admission request object.
This object contains information about the request, including the operation, user info, and object.
Reference: https://pkg.go.dev/k8s.io/api/admission/v1#AdmissionRequest

This example shows the data contained in an AdmissionReview object for a request to update the scale subresource of an apps/v1 Deployment:

```jsonnet
{
  apiVersion: 'admission.k8s.io/v1',
  kind: 'AdmissionReview',
  request: {
    // Random uid uniquely identifying this admission call
    uid: '705ab4f5-6393-11e8-b7cc-42010a800002',

    // Fully-qualified group/version/kind of the incoming object
    kind: {
      group: 'autoscaling',
      version: 'v1',
      kind: 'Scale',
    },

    // Fully-qualified group/version/kind of the resource being modified
    resource: {
      group: 'apps',
      version: 'v1',
      resource: 'deployments',
    },

    // Subresource, if the request is to a subresource
    subResource: 'scale',

    // Fully-qualified group/version/kind of the incoming object in the original request to the API server
    // This only differs from `kind` if the webhook specified `matchPolicy: Equivalent` and the original
    // request to the API server was converted to a version the webhook registered for
    requestKind: {
      group: 'autoscaling',
      version: 'v1',
      kind: 'Scale',
    },

    // Fully-qualified group/version/kind of the resource being modified in the original request to the API server
    // This only differs from `resource` if the webhook specified `matchPolicy: Equivalent` and the original
    // request to the API server was converted to a version the webhook registered for
    requestResource: {
      group: 'apps',
      version: 'v1',
      resource: 'deployments',
    },

    // Subresource, if the request is to a subresource
    // This only differs from `subResource` if the webhook specified `matchPolicy: Equivalent` and the original
    // request to the API server was converted to a version the webhook registered for
    requestSubResource: 'scale',

    // Name of the resource being modified
    name: 'my-deployment',

    // Namespace of the resource being modified, if the resource is namespaced (or is a Namespace object)
    namespace: 'my-namespace',

    // operation can be CREATE, UPDATE, DELETE, or CONNECT
    operation: 'UPDATE',

    userInfo: {
      // Username of the authenticated user making the request to the API server
      username: 'admin',

      // UID of the authenticated user making the request to the API server
      uid: '014fbff9a07c',

      // Group memberships of the authenticated user making the request to the API server
      groups: [
        'system:authenticated',
        'my-admin-group',
      ],

      // Arbitrary extra info associated with the user making the request to the API server
      // This is populated by the API server authentication layer
      extra: {
        'some-key': [
          'some-value1',
          'some-value2',
        ],
      },
    },

    // object is the new object being admitted. It is null for DELETE operations
    object: {
      apiVersion: 'autoscaling/v1',
      kind: 'Scale',
    },

    // oldObject is the existing object. It is null for CREATE and CONNECT operations
    oldObject: {
      apiVersion: 'autoscaling/v1',
      kind: 'Scale',
    },

    // options contain the options for the operation being admitted, like meta.k8s.io/v1 CreateOptions,
    // UpdateOptions, or DeleteOptions. It is null for CONNECT operations
    options: {
      apiVersion: 'meta.k8s.io/v1',
      kind: 'UpdateOptions',
    },

    // dryRun indicates the API request is running in dry run mode and will not be persisted
    // Webhooks with side effects should avoid actuating those side effects when dryRun is true
    dryRun: false,
  },
}
```


### fn ALPHA.admission.allowed

```ts
allowed(msg='')
```

`allowed` returns an admission response that allows the operation.
Takes an optional message.


### fn ALPHA.admission.assertPatch

```ts
assertPatch(msg, obj='#admissionRequest().object')
```

`assertPatch` applies a JSON patch to an object and asserts that the patch was successful.
It expects an array of JSONPatch operations.
Returns the patch.


### fn ALPHA.admission.denied

```ts
denied(msg='')
```

`denied` returns an admission response that denies the operation.
Takes an optional message.


### fn ALPHA.admission.jsonPatchOp

```ts
jsonPatchOp(op, path, value)
```

`jsonPatchOp` returns a JSON patch operation.
The operation is one of: add, remove, replace, move, copy, test.
The path is a JSON pointer to the location in the object to apply the operation.
The value is the value to set for add and replace operations.


### fn ALPHA.admission.patched

```ts
patched(msg, patches)
```

`patched` returns an admission response that allows the operation and includes a list of JSON patches.
The patches should be a list of JSONPatch operations.
JSONPatch operations can be created using the `#jsonPatchOp()` function.
The patches are applied in order.
It is highly recommended to use `#assertPatch()` to test the patches before returning them.
