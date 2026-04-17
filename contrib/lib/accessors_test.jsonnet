local accessors = import 'accessors.libsonnet';
local JSONNETUNIT = import 'github.com/bastjan/jsonnunit/lib/jsonnunit.libsonnet';

JSONNETUNIT.describe(
  'inDelete',
  JSONNETUNIT.it(
    'should return true if deletionTimestamp is set',
    JSONNETUNIT.expect(accessors.inDelete({ metadata: { deletionTimestamp: '2024-01-01T00:00:00Z' } })).to.be['true']
  ).it(
    'should return false if deletionTimestamp is not set', [
      JSONNETUNIT.expect(accessors.inDelete({})).to.be['false'],
      JSONNETUNIT.expect(accessors.inDelete({ metadata: {} })).to.be['false'],
      JSONNETUNIT.expect(accessors.inDelete({ metadata: { deletionTimestamp: '' } })).to.be['false'],
    ],
  )
).describe(
  'getName',
  JSONNETUNIT.it(
    'should return the name of the object',
    JSONNETUNIT.expect(accessors.getName({ metadata: { name: 'my-object' } })).to.equal('my-object')
  ).it(
    'should return empty string if name is not set', [
      JSONNETUNIT.expect(accessors.getName({ metadata: {} })).to.equal(''),
      JSONNETUNIT.expect(accessors.getName({})).to.equal(''),
    ],
  )
).describe(
  'getNamespace',
  JSONNETUNIT.it(
    'should return the namespace of the object',
    JSONNETUNIT.expect(accessors.getNamespace({ metadata: { namespace: 'my-namespace' } })).to.equal('my-namespace')
  ).it(
    'should return empty string if namespace is not set', [
      JSONNETUNIT.expect(accessors.getNamespace({ metadata: {} })).to.equal(''),
      JSONNETUNIT.expect(accessors.getNamespace({})).to.equal(''),
    ],
  )
).describe(
  'getLabel',
  JSONNETUNIT.it(
    'should return the value of a label',
    JSONNETUNIT.expect(accessors.getLabel({ metadata: { labels: { app: 'my-app' } } }, 'app')).to.equal('my-app')
  ).it(
    'should return null if label is not set', [
      JSONNETUNIT.expect(accessors.getLabel({ metadata: { labels: {} } }, 'app')).to.be['null'],
      JSONNETUNIT.expect(accessors.getLabel({ metadata: {} }, 'app')).to.be['null'],
      JSONNETUNIT.expect(accessors.getLabel({}, 'app')).to.be['null'],
    ],
  )
).describe(
  'getAnnotation',
  JSONNETUNIT.it(
    'should return the value of an annotation',
    JSONNETUNIT.expect(accessors.getAnnotation({ metadata: { annotations: { description: 'my-object' } } }, 'description')).to.equal('my-object')
  ).it(
    'should return null if annotation is not set', [
      JSONNETUNIT.expect(accessors.getAnnotation({ metadata: { annotations: {} } }, 'description')).to.be['null'],
      JSONNETUNIT.expect(accessors.getAnnotation({ metadata: {} }, 'description')).to.be['null'],
      JSONNETUNIT.expect(accessors.getAnnotation({}, 'description')).to.be['null'],
    ],
  )
)
