local errors = import 'errors.libsonnet';
local JSONNETUNIT = import 'github.com/bastjan/jsonnunit/lib/jsonnunit.libsonnet';

JSONNETUNIT.describe(
  'ok',
  JSONNETUNIT.it(
    'should create an ok result',
    JSONNETUNIT.expect(errors.ok(42).unwrap()).to.equal(42)
  )
).describe(
  'err',
  JSONNETUNIT.it(
    'should create an err result',
    JSONNETUNIT.expect(errors.err('something went wrong').match(err=function(msg) msg)).to.equal('something went wrong')
  )
).describe(
  'unwrap',
  JSONNETUNIT.it(
    'should unwrap an ok result',
    JSONNETUNIT.expect(errors.ok('success').unwrap()).to.equal('success')
  ).it(
    "SKIPPED - can't currently be tested as jsonnet aborts immediately on error - should throw an error when unwrapping an err result",
    JSONNETUNIT.expect(true).to.be['true']
  )
).describe(
  'unwrapOr',
  JSONNETUNIT.it(
    'should unwrap an ok result',
    JSONNETUNIT.expect(errors.ok('success').unwrapOr('default')).to.equal('success')
  ).it(
    'should return default value when unwrapping an err result',
    JSONNETUNIT.expect(errors.err('failure').unwrapOr('default')).to.equal('default')
  )
).describe(
  'match',
  JSONNETUNIT.it(
    'should match on an ok result',
    JSONNETUNIT.expect(errors.ok(42).match(ok=function(value) value * 2)).to.equal(84)
  ).it(
    'should match on an err result',
    JSONNETUNIT.expect(errors.err('error').match(err=function(msg) 'handled: ' + msg)).to.equal('handled: error')
  )
)
