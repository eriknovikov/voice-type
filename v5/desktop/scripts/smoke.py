# Native Linux smoke test: private config/socket and synthetic credentials.
# It opens a window briefly, tests audio-level capture, then removes no user data.
import os, pathlib, subprocess, tempfile, time, json, socket, http.client
root=pathlib.Path(tempfile.mkdtemp(prefix='voice-type-smoke-'))
env=os.environ.copy()
env.update(HOME=str(root),XDG_STATE_HOME=str(root/'state'),XDG_CONFIG_HOME=str(root/'config'),XDG_DATA_HOME=str(root/'data'),OPENAI_API_KEY='',OPENROUTER_API_KEY='')
runtime=env.get('XDG_RUNTIME_DIR',f'/run/user/{os.getuid()}')
if env.get('WAYLAND_DISPLAY') and not env['WAYLAND_DISPLAY'].startswith('/'):
 env['WAYLAND_DISPLAY']=runtime+'/'+env['WAYLAND_DISPLAY']
env['PULSE_SERVER']='unix:'+runtime+'/pulse/native'
env['XDG_RUNTIME_DIR']=str(root/'runtime');(root/'runtime').mkdir(mode=0o700)
(root/'.voice-type-config.jsonc').write_text(json.dumps({'provider':'openai','port':33232}))
exe=pathlib.Path(__file__).resolve().parents[1]/'build'
class UnixHTTP(http.client.HTTPConnection):
 def connect(self):
  self.sock=socket.socket(socket.AF_UNIX,socket.SOCK_STREAM);self.sock.settimeout(10);self.sock.connect(str(root/'runtime/voice-type/desktop.sock'))
def request(method,path,data=None):
 c=UnixHTTP('localhost');c.request(method,path,json.dumps(data) if data is not None else None,{'Content-Type':'application/json'});r=c.getresponse();body=json.loads(r.read());c.close();return r.status,body
log=open(root/'app.log','w')
p=subprocess.Popen([str(exe/'voice-type-app')],env=env,stdout=log,stderr=log)
try:
 for i in range(100):
  if p.poll() is not None: raise RuntimeError('desktop exited '+str(p.returncode))
  try: status,body=request('GET','/status');break
  except OSError: time.sleep(.1)
 else: raise RuntimeError('management never started')
 print('Desktop running, status:',body['state'])
 status,s=request('GET','/settings');assert status==200
 update={k:s[k] for k in ['revision','provider','port','microphone','vocabulary']};update['api_keys']={'openai':'fake-test-key'}
 status,s=request('PUT','/settings',update);assert status==200,s
 assert 'fake-test-key' not in json.dumps(s)
 status,sources=request('GET','/microphones');assert status==200,sources
 print('Microphone sources:',len(sources))
 status,result=request('POST','/microphone-test',{'id':''});assert status==200,result
 time.sleep(.4)
 status,s=request('GET','/status');assert s['state']=='testing',s
 status,result=request('POST','/toggle');assert status==409,result
 status,result=request('DELETE','/microphone-test');assert status==200,result
 status,s=request('GET','/status');assert s['state']=='idle',s
 print('Microphone test starts, blocks dictation and releases capture')
 second=subprocess.run([str(exe/'voice-type-app')],env=env,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=10)
 assert second.returncode==0,second.stderr.decode()
 print('Second instance exits cleanly')
 time.sleep(2)
 assert p.poll() is None
 print('Native desktop smoke passed; isolated data:',root)
finally:
 try:request('POST','/quit')
 except OSError:pass
 p.terminate()
 try:p.wait(timeout=10)
 except subprocess.TimeoutExpired:p.kill();p.wait()
 log.close()
 print((root/'app.log').read_text()[-3500:])
