# Tencent Deploy Proxy Readiness Design

## Problem

The Tencent deployment runner invokes `scripts/auto_deploy.sh` through a privileged wrapper. The script restarts `rss-pal-oci-egress.service` and immediately runs `git fetch`. The `ubuntu` account has Git configured to use `http://172.18.0.1:3128`; systemd can report the tunnel restarted before that forwarded proxy accepts connections, so `git fetch` intermittently fails with connection refused.

## Design

After restarting the egress tunnel, `auto_deploy.sh` will poll the configured deploy proxy with a short bounded retry loop. Deployment proceeds only after a request through the proxy succeeds. If readiness is not reached, the script exits with a precise error before touching the repository or containers.

The check will use `DEPLOY_PROXY` when set and otherwise retain the existing `http://172.18.0.1:3128` default. A shell regression test will extract and execute the readiness function with mocked `curl`, `sleep`, and logging, proving both delayed success and bounded failure without contacting production.

## Alternatives Considered

- Fixed `sleep`: simple but either races on slow starts or wastes time on fast starts.
- Clear Git's proxy configuration: avoids this race but breaks the Tencent host's intended GitHub egress route.
- Retry `git fetch`: masks the dependency and gives less useful diagnostics than validating the proxy boundary directly.

## Verification

Run the shell regression test, `bash -n` on the changed scripts, and `git diff --check`. After pushing the focused commit to `master`, verify the triggered GitHub Actions run, deployed revision, local API health, and Tencent public API health.
