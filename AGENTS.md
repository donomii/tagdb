# Agent Notes

Agent workflow:
- Before anything else, read the SPEC.md
- After making changes based on user preferences, update the SPEC.md to reflect the new behavior.


# Coding Style

## File Organization

Group by function, files should contain functions that call each other, and/or do similar things or act on similar data.

unless

functions are part of an object.  Objects should go in their own file, especially if they are large.  Ideally files should stay under 1000 lines

High cohesion within files, loose coupling between files
- 200-400 lines typical, 800 max
- Extract utilities from large components
- Organize by feature/domain, not by type

## Error Handling

return errors if the condition is expected, and execution will normally continue afterwards.  examples:

err = openfile() - expect file not to exist
sendToServer() - server or network is expected to fail sometimes

if the condition is unexpected or severe use a panic or throw an error

if !closeFile() { panic }

## Error reporting

When writing error messages, and log files, errors must be as complete and detailed as possible, returning enough information to debug them just be reading them.  Errors must contain: what the result was, the expected result, the task being attempted, and any other supporting data in the function.

Error messages must be comprehensible, numeric errors are not allowed "Error: code 58" is completely unacceptable.

## Locks

Locks should only be used in pairs, like this

l.Lock()
defer l.Unlock()

Never lock and unlock in different functions.

Locks should only be used to lock datastructures, in functions that don't call other functions.  e.g.

data object

method set
lock
defer unlock

set value
return

method get
lock
defer unlock

get value
return value


----

if the language you are using does not have defer, try using a thunk

method set
lock
f = thunk {
   set value
}
check error
unlock
return


----

Locks should never appear in the main package, or the top level of other packages.

---

If you need a synchronised map, use https://github.com/donomii/genericsyncmap

## Types

Do not use void*, interface{}, or any other "escape type"

----



## Code Quality Checklist

Before marking work complete:
- [ ] Code is readable and well-named
- [ ] Functions are small (<50 lines)
- [ ] Files are focused (<800 lines)
- [ ] No deep nesting (>4 levels)
- [ ] Proper error handling
- [ ] No hardcoded values
- [ ] No void* or similar types
- [ ] SPEC.md updated
