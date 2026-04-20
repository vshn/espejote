local d = import 'github.com/jsonnet-libs/docsonnet/doc-util/main.libsonnet';

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
  getName: function(obj) std.get(std.get(obj, 'metadata', {}), 'name', ''),

  '#getNamespace': d.fn(|||
    Gets the namespace of an object. Empty string is returned if the namespace is not set.
  |||, [
    d.arg('obj', d.T.object),
  ]),
  getNamespace: function(obj) std.get(std.get(obj, 'metadata', {}), 'namespace', ''),

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
}
