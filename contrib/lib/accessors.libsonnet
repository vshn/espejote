local d = import 'github.com/jsonnet-libs/docsonnet/doc-util/main.libsonnet';

local getName =
  function(obj) std.get(std.get(obj, 'metadata', {}), 'name', '');

local getNamespace =
  function(obj) std.get(std.get(obj, 'metadata', {}), 'namespace', '');

// validateOwner checks that the owner reference is valid and returns an error message if it's not, or null if it is.
local validateOwner = function(owner, obj)
  local errors = std.filter(
    function(c) !std.isNull(c),
    [
      if std.get(owner, 'apiVersion', '') == '' then
        'owner must have apiVersion',
      if std.get(owner, 'kind', '') == '' then
        'owner must have kind',
      if getName(owner) == '' then
        'owner must have name',
      if std.get(std.get(owner, 'metadata', {}), 'uid', '') == '' then
        'owner must have uid',
      local ownerNamespace = getNamespace(owner);
      local objNamespace = getNamespace(obj);
      if ownerNamespace != '' then
        if objNamespace == '' then
          "cluster-scoped resource must not have a namespace-scoped owner, owner's namespace %s" % ownerNamespace
        else if ownerNamespace != objNamespace then
          "cross-namespace owner references are disallowed, owner's namespace %s, obj's namespace %s" % [ownerNamespace, objNamespace],
    ]
  );

  if std.length(errors) > 0 then
    'Error validating owner reference:\n' + std.join('', std.map(function(e) '* %s\n' % e, errors))
  else
    null
;


{
  '#': d.pkg(
    name='accessors',
    url='accessors.libsonnet',
    help='`accessors` implements accessors for Kubernetes manifests/ objects.',
  ),

  '#inDelete': d.fn(|||
    Checks if an object is in the process of being deleted.
  |||, [
    d.arg('obj', d.T.object),
  ]),
  inDelete: function(obj) std.get(std.get(obj, 'metadata', {}), 'deletionTimestamp', '') != '',

  '#getName': d.fn(|||
    Gets the name of an object. Empty string is returned if the name is not set.
  |||, [
    d.arg('obj', d.T.object),
  ]),
  getName: getName,

  '#getNamespace': d.fn(|||
    Gets the namespace of an object. Empty string is returned if the namespace is not set.
  |||, [
    d.arg('obj', d.T.object),
  ]),
  getNamespace: getNamespace,

  '#getLabel': d.fn(|||
    Gets the value of a label from an object. Null is returned if the label is not set.
  |||, [
    d.arg('obj', d.T.object),
    d.arg('label', d.T.string),
  ]),
  getLabel: function(obj, label) std.get(std.get(std.get(obj, 'metadata', {}), 'labels', {}), label, null),

  '#getAnnotation': d.fn(|||
    Gets the value of an annotation from an object. Null is returned if the annotation is not set.
  |||, [
    d.arg('obj', d.T.object),
    d.arg('annotation', d.T.string),
  ]),
  getAnnotation: function(obj, annotation) std.get(std.get(std.get(obj, 'metadata', {}), 'annotations', {}), annotation, null),

  '#setOwnerReference': d.fn(|||
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
  |||, [
    d.arg('owner', d.T.object),
    d.arg('obj', d.T.object),
    d.arg('controller', d.T.boolean, false),
  ]),
  setOwnerReference: function(owner, obj, controller=false)
    local validationError = validateOwner(owner, obj);
    if !std.isNull(validationError) then
      [obj, validationError]
    else
      [
        obj {
          metadata+: {
            ownerReferences+: [{
              apiVersion: owner.apiVersion,
              kind: owner.kind,
              name: owner.metadata.name,
              uid: owner.metadata.uid,
              [if controller then 'blockOwnerDeletion']: true,
              [if controller then 'controller']: true,
            }],
          },
        },
        null,
      ],
}
