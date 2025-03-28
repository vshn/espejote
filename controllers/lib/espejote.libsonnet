local context = std.extVar('__internal_use_espejote_lib_context');
local trigger = std.extVar('__internal_use_espejote_lib_trigger');
local admissionRequest = std.extVar('__internal_use_espejote_lib_admissionrequest');

local admission = {
  admissionRequest: function() admissionRequest,

  // allowed returns an admission response that allows the operation.
  allowed: function(msg='') {
    assert std.isString(msg) : 'msg must be a string, is: %s' % std.manifestJsonMinified(msg),
    allowed: true,
    message: msg,
  },

  // denied returns an admission response that denies the operation.
  denied: function(msg='') {
    assert std.isString(msg) : 'msg must be a string, is: %s' % std.manifestJsonMinified(msg),
    allowed: false,
    message: msg,
  },

  // jsonPatchOp returns a JSON patch operation.
  jsonPatchOp: function(op, path, value=null) {
    local allowedOPs = ['add', 'remove', 'replace', 'move', 'copy', 'test'],
    assert std.member(allowedOPs, op) : 'op must be one of %s, is: %s' % [std.manifestJsonMinified(allowedOPs), op],
    assert std.isString(path) : 'path must be a string is: %s' % std.manifestJsonMinified(op),
    op: op,
    path: path,
    value: value,
  },

  // patched returns an admission response that allows the operation and includes a list of JSON patches.
  // The patches should be a list of JSONPatch operations.
  // JSONPatch operations can be created using the jsonPatchOp() function.
  // The patches are applied in order.
  patched: function(msg, patches) {
    assert std.isString(msg) : 'msg must be a string, is: %s' % std.manifestJsonMinified(msg),
    assert std.isArray(patches) : 'patches must be an array, is: %s' % std.manifestJsonMinified(patches),
    allowed: true,
    message: msg,
    patches: patches,
  },
};

local alpha = {
  // Admission contains functions for validating and mutating objects
  admission: admission,
};

{
  // ALPHA is where future features are tested.
  // These features are not yet stable and may change in the future, even without a major version bump.
  ALPHA: alpha,

  // Returns the name of the trigger that caused the template to be called or null if unknown
  triggerName: function() std.get(trigger, 'name'),
  // Gets data added to the trigger by the controller.
  // Currently only available for WatchResource triggers.
  // WatchResource triggers add the full object that triggered the template to the trigger data under the key `resource`.
  triggerData: function() std.get(trigger, 'data'),

  // Gets the context object. Always a non-null object with the `contexts[].name` value as keys.
  context: function() context,

  // Marks an object for deletion.
  // The object will be deleted by the controller.
  // NotFound errors are ignored.
  // Deletion options can be passed as optional arguments:
  // - gracePeriodSeconds: number, the grace period for the deletion
  // - propagationPolicy: string, the deletion propagation policy (Background, Foreground, Orphan)
  // - preconditionUID: string, the UID of the object that must match for deletion
  // - preconditionResourceVersion: string, the resource version of the object that must match for deletion
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
}
