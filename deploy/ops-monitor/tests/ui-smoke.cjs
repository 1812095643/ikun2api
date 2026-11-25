// Isolated UI acceptance. All API traffic is intercepted; no live notifications.
const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const fs=require('fs');const path=require('path');const assert=require('assert');
(async()=>{
 const out=process.env.OUTPUT_DIR||'/tmp/ops-monitor-ui';fs.mkdirSync(out,{recursive:true});
 const browser=await chromium.launch({headless:true,executablePath:process.env.CHROMIUM_PATH||undefined});
 try{
 const now=new Date().toISOString();
 const profile={queue_warn_seconds:10,queue_fail_seconds:30,first_warn_seconds:30,first_fail_seconds:60,stall_warn_seconds:30,stall_fail_seconds:60,task_sla_seconds:0};
 let config={version:1,actor_id:'admin:1',idempotency_key:'initial',shadow:true,site_id:'test',rules:{},profiles:{default:profile},balance_low_usd:null,subscription_low_ratio:null,feishu_targets:[],email_targets:[],detail_base_url:'',payment_grace_seconds:300,billing_grace_seconds:120};
 let incident={id:'inc_example',fingerprint:'example',rule_id:'U01',rule_version:1,scope:'account:27/group:19/model:claude-sonnet',revision:1,status:'firing',severity:'P2',first_seen:now,last_seen:now,count:6,request_count:4,user_count:2,account_count:1,acknowledged:false,silenced_until:'0001-01-01T00:00:00Z',quality:'fresh',explanation:{conclusion:'上游429或5xx异常',confidence:'medium',facts:['上游尝试返回429；账号27','本请求后续尝试已成功；原账号恢复尚未验证'],inferences:[],missing_evidence:['状态码不能单独证明余额耗尽、封号或供应商内部故障'],advice:['检查供应商状态、账号限流窗口和重试结果']}};
 const page=await browser.newPage({viewport:{width:1365,height:960}});const errors=[];page.on('pageerror',e=>errors.push(e.message));
 await page.route('**/admin/ops/monitor/**',async route=>{
  const url=new URL(route.request().url()).pathname;let data;
  if(url.endsWith('/summary'))data={build:'isolated-ui-fixture',config,incidents:[incident],deliveries:[{id:'d',incident_id:incident.id,revision:1,channel:'feishu',target:'oc_example',kind:'firing',state:'shadow',attempts:0,error_class:'',accepted_at:'0001-01-01T00:00:00Z',next_attempt:now}],sources:[],capabilities:[],rules:[{id:'U01',title:'上游429或5xx异常',advice:'检查各次尝试及最终结果',requires:['attempt_status'],recovery_samples:5,recovery_seconds:300},{id:'Q02',title:'用户额度低于预警水位',advice:'未配置阈值时保持未启用',requires:['configured_threshold'],recovery_samples:1,recovery_seconds:0}],coverage_gaps:['钱包独立金额流水缺失，不能声称全部金额均已核对。','网关观测源尚未配置，实时请求覆盖未知。'],transports:{feishu:false,email:false},checked_at:now,list_limit:1000};
  else if(url.endsWith('/config')){const body=route.request().postDataJSON();assert.equal(body.balance_low_usd,null);config={...body,version:config.version+1};data=config;}
  else if(url.endsWith('/control')){const body=route.request().postDataJSON();assert.equal(body.revision,incident.revision);incident={...incident,revision:incident.revision+1,acknowledged:body.action==='ack'||incident.acknowledged,silenced_until:body.action==='silence'?body.until:incident.silenced_until};data={incident,history:[incident],children:[]};}
  else data={incident,history:[incident],children:[]};
  await route.fulfill({contentType:'application/json',body:JSON.stringify({code:0,message:'success',data})});
 });
 await page.addInitScript(() => localStorage.setItem('auth_token','isolated-test-token'));
 await page.goto(process.env.PREVIEW_URL||'http://127.0.0.1:5191/tests/monitor-preview/index.html');
 await page.getByRole('button',{name:'查看证据'}).waitFor().catch(async e=>{console.error(JSON.stringify({errors,body:await page.locator('body').innerText()}));await page.screenshot({path:path.join(out,'failure.png'),fullPage:true});throw e});await page.screenshot({path:path.join(out,'desktop-events.png'),fullPage:true});
 await page.getByRole('button',{name:'查看证据'}).click();await page.getByText('原账号恢复尚未验证',{exact:false}).waitFor();
 await page.getByRole('button',{name:'静默 1 小时'}).click();await page.getByRole('alert').filter({hasText:'请填写'}).waitFor();
 await page.getByRole('button',{name:'确认已看到'}).click();await page.getByRole('button',{name:'取消确认'}).waitFor();
 await page.screenshot({path:path.join(out,'desktop-detail.png'),fullPage:true});await page.keyboard.press('Escape');
 await page.getByRole('button',{name:'证据覆盖',exact:true}).click();await page.getByText('尚无采集记录，业务状态未知。').waitFor();
 await page.getByRole('button',{name:'通知设置',exact:true}).click();await page.getByRole('button',{name:'保存通知设置'}).click();await page.getByRole('status').filter({hasText:'已保存配置 v2'}).waitFor();
 await page.setViewportSize({width:390,height:844});await page.screenshot({path:path.join(out,'mobile-settings.png'),fullPage:true});
 assert(await page.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth),'mobile horizontal overflow');
 await page.getByRole('button',{name:'异常事件',exact:true}).click();await page.screenshot({path:path.join(out,'mobile-events.png'),fullPage:true});
 assert(await page.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth),'mobile events horizontal overflow');
 assert.deepEqual(errors,[]);console.log(JSON.stringify({status:'passed',scope:'isolated mock UI',checks:['desktop/mobile','detail','acknowledgement','silence reason validation','unknown coverage','configuration save','no page errors'],screenshots:out}));
 }finally{await browser.close()}
})().catch(e=>{console.error(e);process.exitCode=1});
