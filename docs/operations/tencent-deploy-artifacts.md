# Tencent deployment source delivery

The `Deploy Tencent` workflow packages its exact `GITHUB_SHA` on a GitHub-hosted
runner. It uploads a complete Git bundle and SHA-256 checksum, then downloads
them on the Tencent self-hosted runner. The existing root-owned entrypoint
accepts the bundle path and expected commit; Git operations and the deployment
script still execute as `ubuntu` with the existing direct-network environment.

The entrypoint validates the bundle, checks its `deploy` ref against the expected
commit, and rejects a target that would rewind or diverge from the current
checkout. `auto_deploy.sh` reuses that verified commit without fetching GitHub
again, including when it re-executes itself. Service selection, deployment
locking, rollback, and public health checks remain in effect.

This addresses the 2026-09-16 failures before any build: GitHub HTTPS fetches from
Tencent failed with GnuTLS receive errors and connection timeouts. Previous
failures included Docker mirror fake-IP timeouts and a private npm registry in
the lockfile; those are different failure modes. Artifact delivery removes the
Git transport dependency, but builds still require their image/package sources
when these are not cached.

## Bootstrap and recovery

The root-owned `/usr/local/sbin/rss-pal-deploy-from-actions` must first be updated
from `deploy/tencent/rss-pal-deploy-from-actions` with mode `0755`. Keep a backup
of the previous installed file. No additional sudo permission or SSH deploy key
is required. The no-argument entrypoint remains the legacy direct-Git fallback.

Artifacts are retained for three days. For an expired artifact, dispatch a fresh
workflow rather than rerunning only the deploy job. Failed checksum, commit, or
ancestry verification stops before deployment; do not bypass those checks.

Regression checks:

```sh
bash scripts/tests/tencent_deploy_bundle_test.sh
bash scripts/tests/tencent_short_domain_nginx_test.sh
bash scripts/tests/auto_deploy_direct_network_test.sh
bash scripts/tests/auto_deploy_proxy_ready_test.sh
bash scripts/tests/auto_deploy_service_selection_test.sh
```
