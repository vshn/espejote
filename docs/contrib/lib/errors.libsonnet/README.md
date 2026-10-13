---
permalink: /
---

# errors

```jsonnet
local errors = import "errors.libsonnet"
```

`errors` Implements error handling utilities.

Jsonnet errors immediately terminate the program, which makes it difficult to handle errors gracefully.
This library provides a `result` type that can be used to represent either a successful result or an error,
allowing you to handle errors without crashing the entire program.

It is inspired by Rust's `Result` type.

```jsonnet
local errors = import 'errors.libsonnet';

local divide = function(a, b)
  if b == 0 then
    errors.err('division by zero')
  else
    errors.ok(a / b)
  ;

local result1 = divide(10, 2); // ok(5)
local result2 = divide(10, 0); // err('division by zero')

local value1 = result1.unwrapOr(0); // 5
local value2 = result2.unwrapOr(0); // 0

local value1 = result1.unwrap(); // 5
local value2 = result2.unwrap(); // throws an error with message 'division by zero'

local message1 = result1.match(
  ok=function(value) 'Result is ' + std.toString(value),
  err=function(msg) 'Error: ' + msg,
); // 'Result is 5'
```


## Index

* [`fn err(msg)`](#fn-err)
* [`fn fromTuple(tuple)`](#fn-fromtuple)
* [`fn ok(any)`](#fn-ok)
* [`obj result`](#obj-result)
  * [`fn match(ok, err)`](#fn-resultmatch)
  * [`fn unwrap()`](#fn-resultunwrap)
  * [`fn unwrapOr(or)`](#fn-resultunwrapor)

## Fields

### fn err

```ts
err(msg)
```

Err creates an error result containing the given error message.


### fn fromTuple

```ts
fromTuple(tuple)
```

Creates a result from a tuple of the form `[value, error]`.
If the error is `null`, it returns an `ok` result with the value.
Otherwise, it returns an `err` result with the error message.

```jsonnet
  local val = errors.fromTuple([42, null]); // ok(42)
  local err = errors.fromTuple([42, 'something went wrong']); // err('something went wrong')
```


### fn ok

```ts
ok(any)
```

Ok creates a successful result containing the given value.


## obj result

A result can be either a successful value or an error message. It provides methods to handle both cases. Should be created using `ok`, `err`, or `fromTuple` functions.

### fn result.match

```ts
match(ok, err)
```

Matches on the result, calling `ok(value)` if it's `ok`, or `err(error)` if it's `err`.

A new result can be returned from the match functions to chain operations, or any value can be returned to break the chain.
Both functions can be omitted.
`ok` defaults to returning a ok result with the value, and `err` defaults to returning an err result with the error message.


### fn result.unwrap

```ts
unwrap()
```

Unwraps the result, returning the contained value if it's `ok`, or throwing an error if it's `err`.


### fn result.unwrapOr

```ts
unwrapOr(or)
```

Unwraps the result, returning the contained value if it's `ok`, or returning a default value if it's `err`.
