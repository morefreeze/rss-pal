#!/usr/bin/env bash
set -euo pipefail
ROOT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
python3 - "$ROOT_DIR/docker-compose.yml" <<'PY'
import pathlib,sys
text=pathlib.Path(sys.argv[1]).read_text()
migration=text.split('  status-migrate:',1)[1].split('\n  api:',1)[0]
old='/migrations/071_account_onboarding.sql'
new='/migrations/072_summary_generation.sql'
assert old in migration and new in migration, 'summary provenance migration must be in the startup migration sequence'
assert migration.index(old)<migration.index(new), 'summary provenance migration must follow account onboarding'
for name,next_name in [('api','worker'),('worker','status-monitor')]:
 section=text.split('\n  '+name+':',1)[1].split('\n  '+next_name+':',1)[0]
 assert 'status-migrate:' in section and 'condition: service_completed_successfully' in section, name+' must wait for migration success'
print('PASS: summary provenance schema is migrated before API and worker startup')
PY
