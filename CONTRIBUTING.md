# Contributing to Tango

We are happy to accept your patches and contributions to this project. There are
just a few small guidelines you need to follow.

## Contributor License Agreement

Contributions to this project must be accompanied by a Contributor License
Agreement. You (or your employer) retain the copyright to your contribution;
this simply gives us permission to use and redistribute your contributions as
part of the project.

When you open a pull request at <https://github.com/bytedance/tango>, the
automated CLA check will guide you through the signing process. You generally
only need to submit the CLA once.

## Changes Accepted

Please file issues before doing substantial work; this will ensure that others
don't duplicate the work and that there's a chance to discuss any design issues.

In general, changes should be isolated at compile time using build tags or
at runtime using conditional branches.
This ensures better clarity and makes it easier to backport upstream changes.

## Testing

Run the tests relevant to your change and check that the standard Tango build
continues to work with optional enhancements disabled.

## Issues

We use GitHub issues to track public bugs. Please ensure your description is
clear and has sufficient instructions to be able to reproduce the issue.

File Tango issues at <https://github.com/bytedance/tango/issues/new>. The
`go bug` command opens the upstream Go issue tracker and should be used only
when the issue also reproduces with the corresponding upstream Go release.
When filing a Tango issue, answer these questions:

1. What Tango version are you using (`go version` and `go bversion`)?
2. What operating system and processor architecture are you using?
3. Which Tango compilation options, experiments, or runtime settings are
   enabled?
4. What did you do?
5. What did you expect to see?
6. What did you see instead?
7. Does the issue reproduce with the corresponding upstream Go release?

Unless otherwise noted, source files in this repository are distributed under
the BSD-style license found in the [LICENSE](LICENSE) file.
