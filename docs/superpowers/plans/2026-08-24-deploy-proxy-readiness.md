# Tencent Deploy Proxy Readiness Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prevent Tencent deployments from fetching through the OCI egress proxy before its restarted listener is ready.

**Architecture:** Keep the existing systemd tunnel and Git proxy configuration. Add a bounded readiness function inside `scripts/auto_deploy.sh`, call it immediately after a successful tunnel restart, and fail before repository mutation if readiness times out.

**Tech Stack:** Bash, curl, GitHub Actions, systemd

---

### Task 1: Add the proxy readiness regression test

**Files:**
- Create: `scripts/tests/auto_deploy_proxy_ready_test.sh`
- Test: `scripts/tests/auto_deploy_proxy_ready_test.sh`

- [ ] **Step 1: Write a shell test that extracts `wait_for_outbound_proxy` from `scripts/auto_deploy.sh`, mocks its dependencies, and checks delayed success plus timeout.**
- [ ] **Step 2: Run `bash scripts/tests/auto_deploy_proxy_ready_test.sh` and verify it fails because the function does not exist.**

### Task 2: Implement the bounded readiness gate

**Files:**
- Modify: `scripts/auto_deploy.sh`
- Test: `scripts/tests/auto_deploy_proxy_ready_test.sh`

- [ ] **Step 1: Add `wait_for_outbound_proxy`, using `DEPLOY_PROXY` or the current default, five attempts, and a one-second interval.**
- [ ] **Step 2: Call the function after a successful `rss-pal-oci-egress.service` restart and exit with an explicit error on timeout.**
- [ ] **Step 3: Run `bash scripts/tests/auto_deploy_proxy_ready_test.sh` and verify both cases pass.**
- [ ] **Step 4: Run `bash -n scripts/auto_deploy.sh scripts/tests/auto_deploy_proxy_ready_test.sh` and `git diff --check`.**

### Task 3: Publish and verify the deployment

**Files:**
- Modify: `scripts/auto_deploy.sh`
- Create: `scripts/tests/auto_deploy_proxy_ready_test.sh`

- [ ] **Step 1: Review the focused diff and commit only the design, plan, script, and test.**
- [ ] **Step 2: Integrate the commit into `master` without rewriting history and push `master`.**
- [ ] **Step 3: Watch the resulting Deploy Tencent workflow to completion and inspect its full failed log if nonzero.**
- [ ] **Step 4: Verify `git -C /opt/rss-pal rev-parse HEAD`, `http://127.0.0.1:8080/api/health`, and the direct Tencent public `/api/health` endpoint.**
