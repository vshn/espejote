local context = std.extVar('__internal_use_espejote_lib_context');
local trigger = std.extVar('__internal_use_espejote_lib_trigger');

local triggerTypeWatchResource = 'WatchResource';

{
  TriggerTypeWatchResource: triggerTypeWatchResource,

  // Returns the name of the trigger that caused the template to be called or null if unknown
  triggerType: function() if trigger != null && std.objectHas(trigger, triggerTypeWatchResource) then triggerTypeWatchResource else null,
  // Gets the trigger that caused the template to be called or null if unknown
  getTrigger: function() if trigger != null then std.get(trigger, triggerTypeWatchResource) else null,

  // Gets the context object. Always a non-null object with the definition as keys.
  context: function() context,

  // Marks an object for deletion.
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
