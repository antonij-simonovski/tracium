"""Exhaust only a 16 MiB private tmpfs used by a disposable collector queue."""
import json,os,secrets,subprocess,time,urllib.request,urllib.error
from pathlib import Path
ROOT=Path(__file__).resolve().parents[3]
DIR=Path(os.environ['CHECK_DIR']);REPORT=Path(os.environ['CHECK_REPORT_DIR']);PROJECT=os.environ['CHECK_PROJECT']
C=['docker','compose','--project-name',PROJECT,'--env-file',str(DIR/'env'),'-f',str(ROOT/'docker-compose.yml'),'-f',str(ROOT/'compose.build.yaml'),'-f',str(ROOT/'deploy/tests/production/compose.yaml')]
if (DIR/'images.yaml').exists():C+=['-f',str(DIR/'images.yaml')]
name=PROJECT+'-full-queue'

def run(args,check=True,input=None):
 return subprocess.run(args,check=check,capture_output=True,text=True,input=input,timeout=120)
def ch(sql):
 return run(C+['exec','-T','clickhouse','sh','-c','clickhouse-client --password "$CLICKHOUSE_PASSWORD" --query "$1"','sh',sql]).stdout.strip()

env=dict(line.split('=',1) for line in (DIR/'env').read_text().splitlines() if '=' in line)
(DIR/'queue.env').write_text('CLICKHOUSE_DSN=clickhouse://default:'+env['CLICKHOUSE_PASSWORD']+'@clickhouse:9000/tracium\n')
ws=json.loads((DIR/'account.json').read_text())['workspace'];prefix=secrets.token_hex(8)
result={'check':'queue_disk_exhaustion','filesystem_limit_bytes':16*1024*1024}
try:
 run(['docker','run','-d','--name',name,'--network',PROJECT+'_default','--memory','192m','--cpus','0.5',
      '--tmpfs','/var/lib/tracium/queue:rw,size=16m','-p','127.0.0.1::4318','--env-file',str(DIR/'queue.env'),
      '-v',str(ROOT/'spec/pricing/pricing.json')+':/etc/tracium/pricing.json:ro',os.environ.get('CHECK_IMAGE_PROJECT',PROJECT)+'-collector:latest'])
 port=run(['docker','port',name,'4318']).stdout.strip();base='http://'+port
 for _ in range(30):
  try:
   req=urllib.request.Request(base+'/v1/traces',b'{"resourceSpans":[]}',{'Content-Type':'application/json'})
   with urllib.request.urlopen(req,timeout=2):break
  except Exception:time.sleep(1)
 run(C+['stop','clickhouse'])
 pressure=run(['docker','exec',name,'sh','-c','dd if=/dev/zero of=/var/lib/tracium/queue/space-pressure bs=1024 count=18000'],check=False)
 result['fill_exit_code']=pressure.returncode;result['fill_output']=pressure.stderr[-500:]
 now=time.time_ns()
 attrs=[{'key':'gen_ai.system','value':{'stringValue':'openai'}},{'key':'gen_ai.request.model','value':{'stringValue':'gpt-4o-mini'}}]
 spans=[{'traceId':prefix+f'{i:016x}','spanId':f'{i+1:016x}','name':'chat gpt-4o-mini','kind':3,
         'startTimeUnixNano':str(now-1000000),'endTimeUnixNano':str(now),'attributes':attrs} for i in range(500)]
 body={'resourceSpans':[{'resource':{'attributes':[{'key':'tracium.workspace.id','value':{'stringValue':ws}}]},'scopeSpans':[{'spans':spans}]}]}
 req=urllib.request.Request(base+'/v1/traces',json.dumps(body).encode(),{'Content-Type':'application/json'})
 try:
  with urllib.request.urlopen(req,timeout=20) as r:result['http_status']=r.status;result['otlp_response']=r.read().decode()
 except urllib.error.HTTPError as e:result['http_status']=e.code
 except Exception as e:result['request_error']=type(e).__name__;result['http_status']=0
 time.sleep(8)
 result['container_state']=json.loads(run(['docker','inspect','--format','{{json .State}}',name]).stdout)
 run(['docker','exec',name,'rm','-f','/var/lib/tracium/queue/space-pressure'],check=False)
 run(C+['start','clickhouse'])
 if not result['container_state']['Running']:run(['docker','start',name])
 actual=0
 for _ in range(60):
  try:actual=int(ch(f"SELECT count() FROM tracium.spans WHERE startsWith(trace_id,'{prefix}')"))
  except Exception:pass
  if actual>=500:break
  time.sleep(1)
 result['stored_after_space_and_database_recover']=actual
 result['passed']=result.get('http_status')!=200 or actual==500
 result['note']='A 200 response must not silently lose spans after queue storage is exhausted. Only a private 16 MiB tmpfs was filled.'
except Exception as error:
 result['passed']=False;result['error']=str(error)[:1000]
finally:
 logs=run(['docker','logs',name],check=False)
 (REPORT/'disk-collector.log').write_text(logs.stdout+logs.stderr)
 run(['docker','rm','-f',name],check=False)
 run(C+['start','clickhouse'],check=False)
 (REPORT/'disk.json').write_text(json.dumps(result,indent=2))
 print(json.dumps(result),flush=True)
