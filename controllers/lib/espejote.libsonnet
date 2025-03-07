local context = std.extVar('__internal_use_espejote_lib_context');
local trigger = std.extVar('__internal_use_espejote_lib_trigger');

{
  // Returns true if the template was called by a known trigger
  triggerKnown: function() trigger != null,
  // Gets the trigger that caused the template to be called
  getTrigger: function() trigger,

  // Gets the value of a context variable or returns a default value
  getContext: function(name, default=null) std.get(context, name, default),
}
