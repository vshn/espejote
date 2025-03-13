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
}
