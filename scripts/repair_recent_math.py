#!/usr/bin/env python3
"""Server-only formula audit. Payloads and fsynced backups stay on the DB host.

Use two math_content_tool builds from the before/after revisions. Audit reads a
fixed seven-day snapshot, checks each source feed and probes candidate pages.
Apply only the generated exact-match plan, with URL/content CAS and a full-row
backup before every write. Stdout contains counts and IDs, never article bodies.
"""
import argparse
import concurrent.futures
import datetime
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import threading


def sql(query):
    p = subprocess.run(['docker', 'exec', '-i', 'rss-pal-postgres-1', 'psql', '-X', '-U', 'postgres', '-d', 'rsspal', '-At', '-v', 'ON_ERROR_STOP=1'], input=query, text=True, capture_output=True, check=True)
    return p.stdout.strip()


def literal(value):
    return "convert_from(decode('%s','hex'),'UTF8')" % value.encode().hex()


def durable(path, value):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, 'w', encoding='utf-8') as f:
        json.dump(value, f, ensure_ascii=False)
        f.flush()
        os.fsync(f.fileno())


def tool(binary, op, url='', raw=''):
    p = subprocess.run([binary], input=json.dumps(dict(Op=op, URL=url, Raw=raw))+'\n', text=True, capture_output=True, timeout=35, check=True)
    result = json.loads(p.stdout)
    if result.get('error'):
        raise ValueError(result['error'])
    return result


def candidate(content):
    return bool(re.search(r'\$|\\[a-zA-Z([]|math/tex|<math|katex|RSSPALMATH|(?:equation|theorem|lemma|proof|公式|定理)', content, re.I))


def compare(row, raw, op, args):
    old = tool(args.old, op, row['url'], raw).get('content', '')
    new = tool(args.new, op, row['url'], raw).get('content', '')
    if old == new:
        return None, 'same_conversion' if (row['content'] or '').strip() in (old.strip(), raw.strip()) else 'source_mismatch'
    # These changes must be mathematical, not unrelated HTML normalization.
    if not re.search(r'\$|\\[([]|math/tex|<math|katex|RSSPALMATH', raw, re.I):
        return None, 'non_math_difference'
    current = row['content'] or ''
    if current.strip() == new.strip():
        return None, 'already_fixed'
    if current.strip() not in (old.strip(), raw.strip()):
        return None, 'source_mismatch'
    if not new.strip() or len(new) < len(current)*0.5:
        return None, 'short_result'
    return dict(row, source_raw=raw, source_op=op, new_content=new, old_md5=hashlib.md5(current.encode()).hexdigest(), method=op), 'repairable'


def audit(args):
    output = Path(args.output)
    output.mkdir(mode=0o700, parents=True, exist_ok=True)
    snapshot = output/'snapshot.json'
    if snapshot.exists():
        rows = json.loads(snapshot.read_text())
    else:
        data = sql("""SELECT json_build_object('kind','article','id',a.id,'url',a.url,'content',a.content,'feed_url',f.url,'fetched_at',a.fetched_at)
FROM articles a JOIN feeds f ON f.id=a.feed_id WHERE a.fetched_at>=now()-interval '7 days'
UNION ALL SELECT json_build_object('kind','explore','id',a.id,'url',a.url,'content',a.content,'feed_url',f.url,'fetched_at',a.fetched_at)
FROM explore_articles a JOIN recommended_feeds f ON f.id=a.source_id WHERE a.fetched_at>=now()-interval '7 days' OR a.id=37518;""")
        rows = [json.loads(line) for line in data.split('\n') if line.startswith('{')]
        durable(snapshot, rows)
    groups = {}
    for row in rows:
        groups.setdefault(row['feed_url'], []).append(row)
    print(json.dumps({'stage':'snapshot','rows':len(rows),'sources':len(groups),'candidates':sum(candidate(r['content'] or '') for r in rows)}), flush=True)
    plans, results, lock = [], [], threading.Lock()
    checked = set()

    def feed_job(pair):
        url, entries = pair
        try:
            raw = tool(args.new,'fetch',url).get('raw','')
            items = tool(args.new,'feed_items',url,raw).get('items',[])
            by_url = {i['URL'].rstrip('/'):i['Raw'] for i in items}
            for row in entries:
                source = by_url.get(row['url'].rstrip('/'))
                if source is None:
                    continue
                plan, reason = compare(row,source,'fragment',args)
                with lock:
                    if reason in ('repairable', 'already_fixed', 'same_conversion'):
                        checked.add((row['kind'],row['id']))
                    results.append(dict(kind=row['kind'],id=row['id'],stage='feed',status=reason))
                    if plan: plans.append(plan)
        except Exception as exc:
            with lock: results.append(dict(stage='feed',status='unavailable',url=url,error=str(exc)[:250]))
    with concurrent.futures.ThreadPoolExecutor(max_workers=6) as pool:
        for i,_ in enumerate(pool.map(feed_job,groups.items()),1):
            if i%20==0: print(json.dumps({'stage':'feeds','done':i,'total':len(groups),'planned':len(plans)}),flush=True)
    # Fallback only for rows with formula evidence or same-host evidence, plus
    # the reported row. Feed scan also detects dropped display-only formulas.
    affected_hosts = {re.sub(r'^https?://([^/]+).*',r'\1',p['url']) for p in plans}
    pending = [r for r in rows if (r['kind'],r['id']) not in checked and (candidate(r['content'] or '') or r['id']==37518 or re.sub(r'^https?://([^/]+).*',r'\1',r['url']) in affected_hosts)]
    pages = {}
    for row in pending: pages.setdefault(row['url'],[]).append(row)
    def page_job(pair):
        url,entries=pair
        try:
            raw=tool(args.new,'fetch',url).get('raw','')
            for row in entries:
                plan,reason=compare(row,raw,'page',args)
                with lock:
                    results.append(dict(kind=row['kind'],id=row['id'],stage='page',status=reason))
                    if plan:plans.append(plan)
        except Exception as exc:
            with lock:
                for row in entries: results.append(dict(kind=row['kind'],id=row['id'],stage='page',status='unavailable',error=str(exc)[:250]))
    with concurrent.futures.ThreadPoolExecutor(max_workers=6) as pool:
        for i,_ in enumerate(pool.map(page_job,pages.items()),1):
            if i%20==0:print(json.dumps({'stage':'pages','done':i,'total':len(pages),'planned':len(plans)}),flush=True)
    durable(output/'plan.json',plans)
    durable(output/'results.json',results)
    counts={}
    for r in results: counts[r['stage']+':'+r['status']]=counts.get(r['stage']+':'+r['status'],0)+1
    print(json.dumps({'stage':'audit_complete','rows':len(rows),'planned':len(plans),'article':sum(p['kind']=='article' for p in plans),'explore':sum(p['kind']=='explore' for p in plans),'results':counts}),flush=True)


def apply(args):
    output=Path(args.output)
    plans=json.loads((output/'plan.json').read_text())
    stats={'updated':0,'conflict':0,'already_applied':0}
    for p in plans:
        table={'article':'articles','explore':'explore_articles'}[p['kind']]
        where=f"id={int(p['id'])} AND url={literal(p['url'])} AND content={literal(p['content'])}"
        row=sql(f'SELECT to_jsonb(t) FROM {table} t WHERE {where};')
        if not row:
            already=sql(f"SELECT count(*) FROM {table} WHERE id={int(p['id'])} AND url={literal(p['url'])} AND content={literal(p['new_content'])};")
            stats['already_applied' if already=='1' else 'conflict']+=1
            continue
        backup=output/f"before-{p['kind']}-{p['id']}.json"
        if not backup.exists():durable(backup,dict(before=json.loads(row),new_content=p['new_content']))
        elif json.loads(backup.read_text())['before']['content'] != p['content']:
            raise ValueError('existing backup does not match planned content')
        result=sql(f"SET lock_timeout='5s'; WITH changed AS (UPDATE {table} SET content={literal(p['new_content'])} WHERE {where} RETURNING id) SELECT count(*) FROM changed;")
        stats['updated' if result.splitlines()[-1]=='1' else 'conflict']+=1
    durable(output/'apply-result.json',stats)
    print(json.dumps(stats),flush=True)


if __name__=='__main__':
    parser=argparse.ArgumentParser()
    parser.add_argument('--output',required=True)
    parser.add_argument('--old')
    parser.add_argument('--new')
    parser.add_argument('--apply',action='store_true')
    args=parser.parse_args()
    if args.apply:apply(args)
    else:
        if not args.old or not args.new:parser.error('--old and --new required for audit')
        audit(args)
