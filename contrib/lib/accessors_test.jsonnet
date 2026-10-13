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
).describe(
  'setOwnerReference',
  JSONNETUNIT.describe(
    'validation',
    JSONNETUNIT.it(
      'should fail if apiVersion is missing',
      (function()
         local res = accessors.setOwnerReference(
           { apiVersion: '', kind: 'Owner', metadata: { name: 'owner', uid: '12345' } },
           { metadata: { ownerReferences: [] } },
         );
         [
           JSONNETUNIT.expect(res[1]).to.be.a.string,
           JSONNETUNIT.expect(res[1]).to.have.string('Error validating owner reference'),
           JSONNETUNIT.expect(res[1]).to.have.string('owner must have apiVersion'),
           JSONNETUNIT.expect(res[0].metadata.ownerReferences).to.equal([]),
         ])(),
    ).it(
      'should fail if kind is missing',
      (function()
         local res = accessors.setOwnerReference(
           { apiVersion: 'v1', kind: '', metadata: { name: 'owner', uid: '12345' } },
           { metadata: { ownerReferences: [] } },
         );
         [
           JSONNETUNIT.expect(res[1]).to.be.a.string,
           JSONNETUNIT.expect(res[1]).to.have.string('Error validating owner reference'),
           JSONNETUNIT.expect(res[1]).to.have.string('owner must have kind'),
           JSONNETUNIT.expect(res[0].metadata.ownerReferences).to.equal([]),
         ])(),
    ).it(
      'should fail if name is missing',
      (function()
         local res = accessors.setOwnerReference(
           { apiVersion: 'v1', kind: 'Owner', metadata: { name: '', uid: '12345' } },
           { metadata: { ownerReferences: [] } },
         );
         [
           JSONNETUNIT.expect(res[1]).to.be.a.string,
           JSONNETUNIT.expect(res[1]).to.have.string('Error validating owner reference'),
           JSONNETUNIT.expect(res[1]).to.have.string('owner must have name'),
           JSONNETUNIT.expect(res[0].metadata.ownerReferences).to.equal([]),
         ])(),
    ).it(
      'should fail if uid is missing',
      (function()
         local res = accessors.setOwnerReference(
           { apiVersion: 'v1', kind: 'Owner', metadata: { name: 'owner', uid: '' } },
           { metadata: { ownerReferences: [] } },
         );
         [
           JSONNETUNIT.expect(res[1]).to.be.a.string,
           JSONNETUNIT.expect(res[1]).to.have.string('Error validating owner reference'),
           JSONNETUNIT.expect(res[1]).to.have.string('owner must have uid'),
           JSONNETUNIT.expect(res[0].metadata.ownerReferences).to.equal([]),
         ])(),
    ).it(
      'should fail if cluster-scoped resource has a namespace-scoped owner',
      (function()
         local res = accessors.setOwnerReference(
           { apiVersion: 'v1', kind: 'Owner', metadata: { name: 'owner', namespace: 'testns', uid: '12345' } },
           { metadata: { ownerReferences: [] } },
         );
         [
           JSONNETUNIT.expect(res[1]).to.be.a.string,
           JSONNETUNIT.expect(res[1]).to.have.string('Error validating owner reference'),
           JSONNETUNIT.expect(res[1]).to.have.string('cluster-scoped resource must not have a namespace-scoped owner'),
           JSONNETUNIT.expect(res[1]).to.have.string('testns'),
           JSONNETUNIT.expect(res[0].metadata.ownerReferences).to.equal([]),
         ])(),
    ).it(
      'should fail if on cross-namespace owner reference',
      (function()
         local res = accessors.setOwnerReference(
           { apiVersion: 'v1', kind: 'Owner', metadata: { name: 'owner', namespace: 'testns', uid: '12345' } },
           { metadata: { namespace: 'otherns', ownerReferences: [] } },
         );
         [
           JSONNETUNIT.expect(res[1]).to.be.a.string,
           JSONNETUNIT.expect(res[1]).to.have.string('Error validating owner reference'),
           JSONNETUNIT.expect(res[1]).to.have.string('cross-namespace owner references are disallowed'),
           JSONNETUNIT.expect(res[1]).to.have.string('testns'),
           JSONNETUNIT.expect(res[1]).to.have.string('otherns'),
           JSONNETUNIT.expect(res[0].metadata.ownerReferences).to.equal([]),
         ])(),
    )
  ).it(
    'should set an owner reference on the object',
    (function()
       local res = accessors.setOwnerReference(
         { apiVersion: 'v1', kind: 'Owner', metadata: { name: 'owner', uid: '12345' } },
         { metadata: {} },
       );
       [
         JSONNETUNIT.expect(res[1]).to.be['null'],
         JSONNETUNIT.expect(res[0].metadata.ownerReferences).to.equal([
           {
             apiVersion: 'v1',
             kind: 'Owner',
             name: 'owner',
             uid: '12345',
           },
         ]),
       ])(),
  ).it(
    'should set a controller reference on the object',
    (function()
       local res = accessors.setOwnerReference(
         { apiVersion: 'v1', kind: 'Owner', metadata: { name: 'owner', uid: '12345' } },
         { metadata: {} },
         true,
       );
       [
         JSONNETUNIT.expect(res[1]).to.be['null'],
         JSONNETUNIT.expect(res[0].metadata.ownerReferences).to.equal([
           {
             apiVersion: 'v1',
             kind: 'Owner',
             name: 'owner',
             uid: '12345',
             blockOwnerDeletion: true,
             controller: true,
           },
         ]),
       ])(),
  ).it(
    'should set an owner reference on the object#2',
    (function()
       local otherRef = {
         apiVersion: 'v1',
         kind: 'Owner',
         name: 'owner2',
         uid: '67890',
       };
       local res = accessors.setOwnerReference(
         { apiVersion: 'v1', kind: 'Owner', metadata: { name: 'owner', uid: '12345' } },
         { metadata: { ownerReferences: [otherRef] } },
       );
       [
         JSONNETUNIT.expect(res[1]).to.be['null'],
         JSONNETUNIT.expect(res[0].metadata.ownerReferences).to.equal([
           {
             apiVersion: 'v1',
             kind: 'Owner',
             name: 'owner2',
             uid: '67890',
           },
           {
             apiVersion: 'v1',
             kind: 'Owner',
             name: 'owner',
             uid: '12345',
           },
         ]),
       ])(),
  )
)
