"""Bounded validation with durable JSON evidence, using only standard-library Python."""
import concurrent.futures
import json
import os
from pathlib import Path
import secrets
import statistics
import subprocess
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[3]
REPORT = Path(os.environ['CHECK_REPORT_DIR'])
CHECK_DIR = Path(os.environ['CHECK_DIR'])
PROJECT = os.environ['CHECK_PROJECT']
API, OTLP = os.environ['API_URL'], os.environ['OTLP_URL']
COMPOSE = ['docker', 'compose', '--project-name', PROJECT, '--env-file', str(CHECK_DIR / 'env'),
           '-f', str(ROOT / 'docker-compose.yml'), '-f', str(ROOT / 'compose.build.yaml'), '-f', str(ROOT / 'deploy/tests/production/compose.yaml')]
if (CHECK_DIR / 'images.yaml').exists():
    COMPOSE += ['-f', str(CHECK_DIR / 'images.yaml')]
RESULTS = []


def command(args, input=None, timeout=90, check=True):
    return subprocess.run(args, input=input, capture_output=True, text=True, timeout=timeout, check=check)


def compose(*args, **kwargs):
    return command(COMPOSE + list(args), **kwargs)


def ch(sql):
    return compose('exec', '-T', 'clickhouse', 'sh', '-c',
                   'clickhouse-client --password "$CLICKHOUSE_PASSWORD" --query "$1"', 'sh', sql).stdout.strip()


def request(base, path, payload=None, token=None, timeout=30, headers=None):
    h = {'Content-Type': 'application/json', **(headers or {})}
    if token:
        h['Authorization'] = 'Bearer ' + token
    data = json.dumps(payload).encode() if payload is not None else None
    req = urllib.request.Request(base + path, data, h)
    started = time.monotonic()
    try:
        with urllib.request.urlopen(req, timeout=timeout) as response:
            raw = response.read()
            return response.status, json.loads(raw) if raw else {}, time.monotonic() - started
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode()[:500], time.monotonic() - started
    except (OSError, TimeoutError) as e:
        return 0, type(e).__name__, time.monotonic() - started


def wait_for(fn, timeout=90):
    until = time.monotonic() + timeout
    while time.monotonic() < until:
        try:
            if fn():
                return True
        except Exception:
            pass
        time.sleep(1)
    return False


def record(name, passed, **evidence):
    item = {'check': name, 'passed': passed, **evidence}
    RESULTS.append(item)
    (REPORT / 'operational.json').write_text(json.dumps(RESULTS, indent=2))
    print(json.dumps(item), flush=True)


def attr(key, value):
    return {'key': key, 'value': {'stringValue': value}}


def ingest(prefix, start, count, content_size=256):
    now = time.time_ns()
    spans = []
    for i in range(start, start + count):
        trace_id = prefix + f'{i:016x}'
        spans.append({'traceId': trace_id, 'spanId': f'{i+1:016x}', 'name': 'chat gpt-4o-mini', 'kind': 3,
            'startTimeUnixNano': str(now-1_000_000), 'endTimeUnixNano': str(now),
            'attributes': [attr('gen_ai.system', 'openai'), attr('gen_ai.request.model', 'gpt-4o-mini'),
                           attr('gen_ai.input.messages', secrets.token_hex(content_size//2)),
                           {'key': 'gen_ai.usage.input_tokens', 'value': {'intValue': '100'}},
                           {'key': 'gen_ai.usage.output_tokens', 'value': {'intValue': '20'}}]})
    body = {'resourceSpans': [{'resource': {'attributes': [attr('service.name', 'production-check'),
             attr('tracium.workspace.id', WS)]}, 'scopeSpans': [{'spans': spans}]}]}
    return request(OTLP, '/v1/traces', body)


def rows(prefix):
    return int(ch(f"SELECT count() FROM tracium.spans WHERE startsWith(trace_id, '{prefix}')"))


def measure(label, duration=120, batch=100, workers=2):
    prefix = secrets.token_hex(8)
    submitted = accepted = 0
    latencies, errors = [], []
    started = time.monotonic()
    # A paced 500 spans/s workload, with short bursts of concurrent requests.
    with concurrent.futures.ThreadPoolExecutor(max_workers=workers) as pool:
        while time.monotonic() - started < duration:
            tick = time.monotonic()
            futures = [pool.submit(ingest, prefix, submitted + i*batch, batch) for i in range(workers)]
            submitted += workers*batch
            for f in futures:
                status, response, elapsed = f.result()
                latencies.append(elapsed)
                if status == 200 and not response.get('partialSuccess', {}).get('rejectedSpans', 0):
                    accepted += batch
                else:
                    errors.append({'status': status, 'response': response})
            time.sleep(max(0, workers*batch/500 - (time.monotonic()-tick)))
    elapsed = time.monotonic()-started
    stored = wait_for(lambda: rows(prefix) >= accepted)
    actual = rows(prefix)
    unique = int(ch(f"SELECT uniqExact(trace_id) FROM tracium.spans WHERE startsWith(trace_id, '{prefix}')"))
    record(label, stored and actual == accepted == submitted and unique == actual,
           duration_s=round(elapsed,2), submitted=submitted, accepted=accepted, stored=actual, unique=unique,
           spans_per_s=round(accepted/elapsed,1), http_p95_ms=round(sorted(latencies)[int(.95*(len(latencies)-1))]*1000,2),
           errors=errors[:5], note='256-byte random content, one LLM span per trace, 500 spans/s target')


def outage_restart():
    prefix = secrets.token_hex(8)
    outage_start = time.monotonic()
    compose('stop', 'clickhouse')
    statuses = [ingest(prefix, i*100, 100)[0] for i in range(10)]
    accepted = statuses.count(200)*100
    time.sleep(8)  # Exceeds batch timeout so accepted batches should be queued.
    readiness = request(API, '/v1/ready')[0]
    compose('kill', '-s', 'SIGKILL', 'collector')
    time.sleep(3)
    # Restart while the backend is still absent; test startup dependencies too.
    compose('start', 'collector')
    time.sleep(5)
    state = compose('ps', '--all', '--format', 'json', 'collector').stdout
    # Stay unavailable beyond the upstream five-minute retry default.
    while time.monotonic() - outage_start < 360:
        time.sleep(min(30, 360 - (time.monotonic() - outage_start)))
    compose('start', 'clickhouse')
    recovered = wait_for(lambda: rows(prefix) >= accepted, timeout=120)
    actual = rows(prefix)
    record('database_outage_and_collector_crash', recovered and actual == accepted,
           accepted=accepted, stored=actual, api_ready_status_during_db_outage=readiness,
           collector_state_while_db_down={k: json.loads(state).get(k) for k in ('State', 'Status', 'ExitCode')}, outage_seconds=round(time.monotonic()-outage_start, 1))


def immediate_crash():
    global OTLP
    OTLP = 'http://' + compose('port', 'collector', '4318').stdout.strip()
    if not wait_for(lambda: request(OTLP, '/v1/traces', {'resourceSpans': []})[0] == 200, timeout=40):
        raise RuntimeError('Collector not ready before crash test')
    prefix = secrets.token_hex(8)
    status, response, _ = ingest(prefix, 0, 1)
    if status != 200 or response.get('partialSuccess', {}).get('rejectedSpans', 0):
        record('crash_immediately_after_otlp_ack', None, status=status, response=response, note='Inconclusive: span was not accepted')
        return
    # A successful OTLP response may precede the batch processor's flush.
    compose('kill', '-s', 'SIGKILL', 'collector')
    compose('start', 'collector')
    OTLP = 'http://' + compose('port', 'collector', '4318').stdout.strip()
    wait_for(lambda: request(OTLP, '/v1/traces', {'resourceSpans': []})[0] == 200, timeout=40)
    time.sleep(8)
    actual = rows(prefix)
    record('crash_immediately_after_otlp_ack', actual == 1, accepted=status == 200, stored=actual,
           note='Single underfilled batch; kill before the configured 5-second batch timeout')


def auth_checks():
    status, _, _ = request(API, '/v1/auth/register', {'email': 'invalid-email', 'password': 'a'})
    outcomes = [request(API, '/v1/auth/login', {'email': EMAIL, 'password': 'wrong'})[0] for _ in range(30)]
    oversized_status, _, _ = request(API, '/v1/auth/register', {'email': 'long-pass@example.test', 'password': 'a'*100})
    forged, _, _ = request(API, '/v1/workspaces', token=TOKEN[:-4]+'abcd')
    record('authentication_boundary', forged == 401, modified_jwt_status=forged)
    record('public_auth_abuse_controls', status == 400 and 429 in outcomes and oversized_status == 400,
           malformed_email_one_character_password_status=status,
           failed_login_status_counts={str(s): outcomes.count(s) for s in set(outcomes)},
           password_over_bcrypt_limit_status=oversized_status)


def query_scale():
    for total in (100_000, 1_000_000):
        # Isolated synthetic dataset; this tests reads separately from OTLP throughput.
        existing = int(ch("SELECT count() FROM tracium.spans WHERE workflow_name='scale-check'"))
        remaining = total-existing
        ch(f"""INSERT INTO tracium.spans (trace_id,span_id,name,start_time_ms,end_time_ms,model,model_normalized,user_id,workspace_id,workflow_name,source,cost_usd,input_tokens,output_tokens,schema_version)
          SELECT concat('scale',toString(number+{existing})),toString(number),'chat gpt-4o-mini',
          toUnixTimestamp64Milli(now64())-toInt64((number+{existing})%7776000)*1000,
          toUnixTimestamp64Milli(now64())-toInt64((number+{existing})%7776000)*1000+100,
          'gpt-4o-mini','gpt-4o-mini',concat('user-',toString(number%100)),'{WS}','scale-check','span',0.001,100,20,1
          FROM numbers({remaining}) SETTINGS max_threads=1,max_insert_threads=1,max_block_size=10000""")
        measurements = {}
        for endpoint in ('/v1/traces?range=24h&page_size=50', '/v1/metrics/kpis?range=24h', '/v1/metrics/kpis?range=1y'):
            samples = [request(API, endpoint+'&workspace_id='+WS, token=TOKEN) for _ in range(12)]
            durations = sorted(s[2]*1000 for s in samples)
            measurements[endpoint] = {'statuses': [s[0] for s in samples], 'errors': [s[1] for s in samples if s[0] != 200][:3], 'median_ms':round(statistics.median(durations),2), 'p95_ms':round(durations[10],2)}
        record('query_scale_'+str(total), all(all(s==200 for s in m['statuses']) for m in measurements.values()),
               synthetic_rows=total, endpoints=measurements,
               note='Direct SQL population; small one-span traces, 100 user labels, sequential requests; not a collector throughput test')


wait_for(lambda: request(API, '/v1/health')[0] == 200)
EMAIL, PASSWORD = 'production-check@example.test', secrets.token_hex(16)
status, account, _ = request(API, '/v1/auth/register', {'email': EMAIL, 'password': PASSWORD})
if status != 201:
    raise RuntimeError('Could not create isolated test account')
TOKEN = account['token']
status, workspace, _ = request(API, '/v1/workspaces', {'name':'Production check','slug':'production-check','env':'development'}, token=TOKEN)
WS = workspace['id']
checks = {'auth': auth_checks, 'load': lambda: measure('sustained_ingestion'), 'outage': outage_restart, 'crash': immediate_crash, 'scale': query_scale}
for name in os.environ.get('CHECK_NAMES', ','.join(checks)).split(','):
    check = checks[name]
    try:
        check()
    except Exception as error:
        record(name, False, error=str(error)[:2000], stderr=getattr(error, 'stderr', '')[-4000:])
# Capture database-side failures before backup shuts down the services.
(REPORT/'clickhouse-errors.log').write_text(compose('exec', '-T', 'clickhouse', 'sh', '-c', 'tail -n 200 /var/log/clickhouse-server/clickhouse-server.err.log', check=False).stdout)
# Used only by subsequent backup tests in this same isolated environment.
(CHECK_DIR/'account.json').write_text(json.dumps({'email':EMAIL,'password':PASSWORD,'workspace':WS,'token':TOKEN}))
print('Operational checks finished', flush=True)
