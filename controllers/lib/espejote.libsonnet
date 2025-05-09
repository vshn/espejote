local d = import 'github.com/jsonnet-libs/docsonnet/doc-util/main.libsonnet';

local context = std.extVar('__internal_use_espejote_lib_context');
local trigger = std.extVar('__internal_use_espejote_lib_trigger');
local admissionRequest = std.extVar('__internal_use_espejote_lib_admissionrequest');

local admission = {
  '#admissionRequest': d.fn(
    |||
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
    |||,
  ),
  admissionRequest: function() admissionRequest,

  '#allowed': d.fn(
    |||
      `allowed` returns an admission response that allows the operation.
      Takes an optional message.
    |||,
    [d.arg('msg', d.T.string, '')],
  ),
  allowed: function(msg='') {
    assert std.isString(msg) : 'msg must be a string, is: %s' % std.manifestJsonMinified(msg),
    allowed: true,
    message: msg,
  },

  '#denied': d.fn(
    |||
      `denied` returns an admission response that denies the operation.
      Takes an optional message.
    |||,
    [d.arg('msg', d.T.string, '')],
  ),
  denied: function(msg='') {
    assert std.isString(msg) : 'msg must be a string, is: %s' % std.manifestJsonMinified(msg),
    allowed: false,
    message: msg,
  },

  '#jsonPatchOp': d.fn(
    |||
      `jsonPatchOp` returns a JSON patch operation.
      The operation is one of: add, remove, replace, move, copy, test.
      The path is a JSON pointer to the location in the object to apply the operation.
      The value is the value to set for add and replace operations.
    |||,
    [
      d.arg('op', d.T.string),
      d.arg('path', d.T.string),
      d.arg('value', d.T.any, null),
    ],
  ),
  jsonPatchOp: function(op, path, value=null) {
    local allowedOPs = ['add', 'remove', 'replace', 'move', 'copy', 'test'],
    assert std.member(allowedOPs, op) : 'op must be one of %s, is: %s' % [std.manifestJsonMinified(allowedOPs), op],
    assert std.isString(path) : 'path must be a string is: %s' % std.manifestJsonMinified(op),
    op: op,
    path: path,
    value: value,
  },

  '#assertPatch': d.fn(
    |||
      `assertPatch` applies a JSON patch to an object and asserts that the patch was successful.
      It expects an array of JSONPatch operations.
      Returns the patch.
    |||,
    [d.arg('msg', d.T.array), d.arg('obj', d.T.object, '#admissionRequest().object')],
  ),
  assertPatch: function(patches, obj=self.admissionRequest().object)
    assert std.isArray(patches) : 'patches must be an array, is: %s' % std.manifestJsonMinified(patches);
    local applied = std.native('__internal_use_espejote_lib_function_apply_json_patch')(obj, patches);
    assert applied[1] == null : 'JSON patch failed: %s' % applied[1];
    patches,

  '#patched': d.fn(
    |||
      `patched` returns an admission response that allows the operation and includes a list of JSON patches.
      The patches should be a list of JSONPatch operations.
      JSONPatch operations can be created using the `#jsonPatchOp()` function.
      The patches are applied in order.
      It is highly recommended to use `#assertPatch()` to test the patches before returning them.
    |||,
    [d.arg('msg', d.T.string), d.arg('patches', d.T.array)],
  ),
  patched: function(msg, patches) {
    assert std.isString(msg) : 'msg must be a string, is: %s' % std.manifestJsonMinified(msg),
    assert std.isArray(patches) : 'patches must be an array, is: %s' % std.manifestJsonMinified(patches),
    allowed: true,
    message: msg,
    patches: patches,
  },
};

local alpha = {
  '#admission': d.obj(|||
    `admission` contains functions for validating and mutating objects.
  |||),
  admission: admission,
};

{
  '#': d.pkg(
    name='espejote',
    url='espejote.libsonnet',
    help='`espejote` implements accessors for espejote features.',
  ),

  '#ALPHA': d.obj(|||
    `ALPHA` is where future features are tested.
    These features are not yet stable and may change in the future, even without a major version bump.
  |||),
  ALPHA: alpha,

  '#triggerName': d.fn(|||
    Returns the name of the trigger that caused the template to be called or null if unknown
  |||),
  triggerName: function() std.get(trigger, 'name'),

  '#triggerData': d.fn(|||
    Gets data added to the trigger by the controller.
    Currently only available for WatchResource triggers.
    WatchResource triggers add the full object that triggered the template to the trigger data under the key `resource`.
  |||),
  triggerData: function() std.get(trigger, 'data'),

  '#context': d.fn(|||
    Gets the context object. Always a non-null object with the `context[].name` value as keys.
  |||),
  context: function() context,

  '#markForDelete': d.fn(
    |||
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
    |||,
    [
      d.arg('obj', d.T.object),
      d.arg('gracePeriodSeconds', d.T.number, null),
      d.arg('propagationPolicy', d.T.string, null),
      d.arg('preconditionUID', d.T.string, null),
      d.arg('preconditionResourceVersion', d.T.string, null),
    ],
  ),
  markForDelete:
    function(
      obj,
      gracePeriodSeconds=null,
      propagationPolicy=null,
      preconditionUID=null,
      preconditionResourceVersion=null,
    ) obj {
      local allowedDeletionPropagations = [null, 'Background', 'Foreground', 'Orphan'],
      assert std.member(allowedDeletionPropagations, propagationPolicy) : 'propagationPolicy must be one of %s, is: %s' % [std.manifestJsonMinified(allowedDeletionPropagations), propagationPolicy],
      __internal_use_espejote_lib_deletion: std.prune({
        delete: true,
        gracePeriodSeconds: gracePeriodSeconds,
        propagationPolicy: propagationPolicy,
        preconditionUID: preconditionUID,
        preconditionResourceVersion: preconditionResourceVersion,
      }),
    },

  '#applyOptions': d.fn(
    |||
      `applyOptions` allows configuring apply options for an object.
      These options override the `spec.applyOptions` fields of the managed resource.

      The options are used by the controller to determine how to apply the object.
      The options are:
      - fieldManager: string, the field manager to use when applying the ManagedResource.
        If not set, the field manager is set to the name of the resource with `managed-resource` prefix
      - force: boolean, is going to "force" Apply requests.
        It means user will re-acquire conflicting fields owned by other people.
      - fieldValidation: string, instructs the managed resource on how to handle
        objects containing unknown or duplicate fields. Valid values are:
        - Ignore: This will ignore any unknown fields that are silently
        dropped from the object, and will ignore all but the last duplicate
        field that the decoder encounters.
        Note that Jsonnet won't allow you to add duplicate fields to an object
        and most unregistered fields will error out in the server-side apply
        request, even with this option set.
        - Strict: This will fail the request with a BadRequest error if
        any unknown fields would be dropped from the object, or if any
        duplicate fields are present. The error returned will contain
        all unknown and duplicate fields encountered.
        Defaults to "Strict".

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
        fieldManager='my-tool-status-reporter',
      )
      ```
    |||,
    [
      d.arg('obj', d.T.object),
      d.arg('fieldManager', d.T.string, null),
      d.arg('force', d.T.boolean, null),
      d.arg('fieldValidation', d.T.string, null),
    ],
  ),
  applyOptions:
    function(
      obj,
      fieldManager=null,
      force=null,
      fieldValidation=null,
    ) obj {
      local allowedFieldValidation = [null, 'Ignore', 'Strict'],
      assert std.member(allowedFieldValidation, fieldValidation) : 'fieldValidation must be one of %s, is: %s' % [std.manifestJsonMinified(allowedFieldValidation), fieldValidation],
      __internal_use_espejote_lib_apply_options: std.prune({
        fieldManager: fieldManager,
        force: force,
        fieldValidation: fieldValidation,
      }),
    },
}
