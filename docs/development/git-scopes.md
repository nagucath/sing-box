# Git scopes

Scopes name modules and submodules. `repo` is not a scope.
A module topic uses `<module>[/<submodule>]/<type>/<detail>`.
Repository-wide work uses `<type>/<detail>` with an empty scope.
Publication branches remain `main`, `testing`, and `develop`.
No repository prefix is added to these branches.

The active repository-wide extraction branch is `refactor/openconnect-infra`.
It was renamed from `rebase/incus-hotspot-20261119` without rewriting commits.
The reusable provider topic is `network/openconnect/h3c/refactor/extract` in infra.
The provider uses an exact module commit; source adjacency is not dependency authority.
