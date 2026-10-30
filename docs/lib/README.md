---
permalink: /
---

# espejote

```jsonnet
local espejote = import "espejote.libsonnet"
```

`espejote` implements accessors for espejote features.

## Index

* [`fn applyGroup(objs, errorPolicy='')`](#fn-applygroup)
* [`fn applyOptions(obj, fieldManager, fieldManagerSuffix, force, fieldValidation, subResource)`](#fn-applyoptions)
* [`fn context()`](#fn-context)
* [`fn markForDelete(obj, gracePeriodSeconds, propagationPolicy, preconditionUID, preconditionResourceVersion)`](#fn-markfordelete)
* [`fn triggerData()`](#fn-triggerdata)
* [`fn triggerName()`](#fn-triggername)
* [`obj ALPHA`](#obj-alpha)
  * [`fn bcrypt(password, salt)`](#fn-alphabcrypt)
  * [`obj ALPHA.admission`](#obj-alphaadmission)
    * [`fn admissionRequest()`](#fn-alphaadmissionadmissionrequest)
    * [`fn allowed(msg='')`](#fn-alphaadmissionallowed)
    * [`fn assertPatch(msg, obj='#admissionRequest().object')`](#fn-alphaadmissionassertpatch)
    * [`fn denied(msg='')`](#fn-alphaadmissiondenied)
    * [`fn jsonPatchOp(op, path, value)`](#fn-alphaadmissionjsonpatchop)
    * [`fn namespaceObject()`](#fn-alphaadmissionnamespaceobject)
    * [`fn patched(msg, patches)`](#fn-alphaadmissionpatched)

## Fields

### fn applyGroup

```ts
applyGroup(objs, errorPolicy='')
```

`applyGroup` allows configuring a group of apply and delete operations.
Groups can be deeply nested.
Returning an array from the template is like returning a group without any options.
Groups inherit the options of their parent group unless they override them.

Currently the only supported option is `errorPolicy` which controls how errors are handled when applying the group.
The options are:
- errorPolicy: string, controls how errors are handled when applying the group.
  Valid values are:
  - Continue: continue applying the remaining objects in the group if an error occurs.
  - Abort: stop applying the remaining objects in the group if an error occurs.
  Defaults to "Continue".

```jsonnet
local esp = import 'espejote.libsonnet';

local abortGroup = function(objs) esp.applyGroup(objs, errorPolicy='Abort');

local manageResourceFromCM = function(cm)
  if inDelete(cm) then
    // Remove finalizer only if cleanup succeeds
    abortGroup([
      cleanup(cm),
      removeFinalizer(cm),
    ])
  else
    // Only start managing the resource after adding the finalizer was successful
    abortGroup([
      applyFinalizer(cm),
      resource(cm),
    ]);

// Groups are allowed to be deeply nested and an array is a valid group without any options.
// The default error policy is 'Continue' which we want here to keep managing other resources
// even if one sub-group fails.
[
  manageResourceFromCM(child)
  for child in esp.context().configmaps
]
```


### fn applyOptions

```ts
applyOptions(obj, fieldManager, fieldManagerSuffix, force, fieldValidation, subResource)
```

`applyOptions` allows configuring apply options for an object.
These options override the `spec.applyOptions` fields of the managed resource.

The options are used by the controller to determine how to apply the object.
The options are:
- fieldManager: string, the field manager to use when applying the ManagedResource.
  If not set, the field manager is set to the name of the resource with `managed-resource` prefix
- fieldManagerSuffix: string, a suffix to append to the field manager.
  This is useful if different code paths in the template apply differing parts of a resource.
- force: boolean, is going to "force" Apply requests.
  It means user will re-acquire conflicting fields owned by other people.
- (DEPRECATED) fieldValidation: string, instructs the managed resource on how to handle
  objects containing unknown or duplicate fields.
  Server side apply always uses strict validation, this field is ignored.
- subResource: string, allows patching sub-resources. A commonly used sub-resource is `status`.
  See the example for approving a CSR.
```jsonnet
esp.applyOptions(
  {
    apiVersion: 'v1',
    kind: 'ConfigMap',
    metadata: {
      name: 'cm-to-apply-options',
      namespace: 'target-namespace',
      annotations: {
        'my.tool/status': 'Success',
      },
    },
  },
  fieldManagerSuffix=':status-reporter',
)

esp.applyOptions(
  {
    apiVersion: 'certificates.k8s.io/v1',
    kind: 'CertificateSigningRequest',
    metadata: {
      name: 'node-xxx',
    },
    status: {
      conditions: [{
        message: 'Approved by espejote',
        reason: 'ApprovedByMyEspejotePolicy',
        type: 'Approved',
        status: 'True',
      }],
    },
  },
  subResource='approval',
)
```


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
WatchResource triggers add the full object that triggered the template to the trigger data under the key `resource` and
the raw trigger event - a flat dictionary consisting of `apiVersion`, `kind`, `name`, and `namespace` - under the key `resourceEvent`.
The `resource` object might be null if the object was deleted and can't be retrieved.


### fn triggerName

```ts
triggerName()
```

Returns the name of the trigger that caused the template to be called or null if unknown


## obj ALPHA

`ALPHA` is where future features are tested.
These features are not yet stable and may change in the future, even without a major version bump.


### fn ALPHA.bcrypt

```ts
bcrypt(password, salt)
```

`bcrypt` generates the bcrypt hash for the provided password and salt
strings.

The function returns an array with two elements. The first element is
the bcrypt hash for the passed password and salt. The second element is
`null` if the native function completed successfully, or a string
containing the error message if the native function failed.

Espejote implements the `bcrypt` native function using a vendored copy
of `golang.org/x/crypto/bcrypt`. In contrast to the upstream library,
the vendored copy requires an user-supplied salt. This ensures that
calling the function with the same inputs will result in the same output
on every template evaluation.

Currently the native function uses the library's DefaultCost when
computing the bcrypt hash.

Example ManagedResource template:

```
local esp = import 'espejote.libsonnet';

local users =
  local user_ctx = esp.context().users;
  // ... context validation omitted ...
  // assumption: data.users of the secret in the context has shape
  // {
  //   username: password,
  //   ...
  // }
  std.parseJson(std.base64Decode(user_ctx[0].data.users));

// Espejote registers the native function to return a tuple which
// contains the return value as the first element and the native
// function's error (if any) as the second element. For this example, we
// simply make any errors returned by the native function hard template
// errors, but in some cases the template may want to do something else
// instead.
local bcrypt_err(password, salt) =
  local bhash = espn.bcrypt(password, salt);
  if bhash[1] != null then
    error bhash[1]
  else
    bhash[0];

local entries = [
  // This example uses the last 16 bytes of the user name's sha256 hash
  // as the salt, which ensures that we generate a stable bcrypt hash
  // for the same username. Note that this reduces the security of the
  // bcrypt hash significantly, since an attacker could now determine
  // the salt from a leaked (or known) username.
  // For better security, random salts for each user should be provided
  // through an Espejote context.
  '%s:%s' % [ u, bcrypt_err(users[u], std.sha256(u)[-16:]) ]
  for u in std.objectFields(users)
];

{
  apiVersion: 'v1',
  kind: 'Secret',
  metadata: {
    name: 'htpasswd',
  },
  stringData: {
    htpasswd: std.join('\n', entries),
  },
}
```


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


### fn ALPHA.admission.namespaceObject

```ts
namespaceObject()
```

`namespaceObject` allows access to the namespace object that the incoming object belongs to. The value is null for cluster-scoped resources.
Reference: https://pkg.go.dev/k8s.io/api/core/v1#Namespace


### fn ALPHA.admission.patched

```ts
patched(msg, patches)
```

`patched` returns an admission response that allows the operation and includes a list of JSON patches.
The patches should be a list of JSONPatch operations.
JSONPatch operations can be created using the `#jsonPatchOp()` function.
The patches are applied in order.
It is highly recommended to use `#assertPatch()` to test the patches before returning them.
