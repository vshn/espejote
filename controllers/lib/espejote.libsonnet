local context = std.extVar('__internal_use_espejote_lib_context');
local trigger = std.extVar('__internal_use_espejote_lib_trigger');

local triggerTypeWatchResource = 'WatchResource';

{
  TriggerTypeWatchResource: triggerTypeWatchResource,

  // Returns the name of the trigger that caused the template to be called or null if unknown
  triggerType: function() if trigger != null && std.objectHas(trigger, triggerTypeWatchResource) then triggerTypeWatchResource else null,
  // Gets the trigger that caused the template to be called or null if unknown
  getTrigger: function() if trigger != null then std.get(trigger, triggerTypeWatchResource) else null,

  // Gets the context object. Always a non-null object with the `contexts[].def` value as keys.
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
