package httpgateway

import (
	"encoding/json"
	"strings"

	"github.com/davveo/lumo/internal/constants"
)

// Inject the Go protocol values directly so the workbench also works when the
// configured frontend directory does not contain the generated browser enums.
var consoleHTML = func() string {
	protocol, _ := json.Marshal(struct {
		LoginService   string `json:"login_service"`
		LoginAction    string `json:"login_action"`
		DefaultService string `json:"default_service"`
		DefaultAction  string `json:"default_action"`
		TokenType      string `json:"token_type"`
	}{
		LoginService: constants.ServiceIAM, LoginAction: constants.ActionLogin,
		DefaultService: constants.ServiceCatalog, DefaultAction: constants.ActionList,
		TokenType: constants.IAMTokenTypeBearer,
	})
	// The fixed struct contains only strings, which json.Marshal always supports.
	// Its default HTML escaping keeps the embedded script boundary intact.
	return strings.Replace(consoleHTMLTemplate, "/*__LUMO_CONSOLE_PROTOCOL__*/", string(protocol), 1)
}()

// The API workbench is intentionally separate from the existing Site prototype.
const consoleHTMLTemplate = `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>LUMO 后端联调台</title><style>body{font:16px system-ui;max-width:1040px;margin:40px auto;padding:0 24px;background:#f5f7fb;color:#17314d}h1{font-size:30px}section{background:white;border:1px solid #dbe4ee;border-radius:14px;padding:24px;margin:18px 0}button,input,select,textarea{font:inherit;border:1px solid #ccd7e4;border-radius:7px;padding:10px}button{background:#246cd1;color:white;cursor:pointer}textarea{display:block;width:calc(100% - 24px);height:160px;margin:14px 0}pre{white-space:pre-wrap;overflow-wrap:anywhere;font:13px ui-monospace;background:#102337;color:#ddedf8;padding:20px;border-radius:10px}.muted{color:#677f96}a{color:#246cd1}</style><h1>LUMO 后端联调台</h1><p class="muted">HTTP → gRPC → transport → usecase → repository → SQL。每次请求返回 trace_id，可到 Jaeger 查看完整调用关系。</p><section><h2>登录</h2><input id="username" value="demo" aria-label="用户名"><input id="password" type="password" value="Lumo@2026" aria-label="密码"><button id="login">登录</button><span id="who" class="muted"></span><p class="muted">演示用户 demo / Lumo@2026；后台用户 admin / Admin@2026。仅在 DEMO_MODE 开启时提供演示账号。Token 仅保留在此页面内存。</p></section><section><h2>RPC 业务动作</h2><input id="service" aria-label="服务名"><input id="action" aria-label="操作名"><button id="send">发送请求</button><textarea id="payload" aria-label="请求体">{"data":{}}</textarea><p><a href="http://localhost:16686" target="_blank" rel="noreferrer">打开 Jaeger</a> · 全部业务动作和示例见 docs/api.md</p><pre id="output">等待请求</pre></section><script>const protocol=/*__LUMO_CONSOLE_PROTOCOL__*/;let token='';const q=id=>document.getElementById(id);q('service').value=protocol.default_service;q('action').value=protocol.default_action;async function call(s,a,body){const headers={'Content-Type':'application/json'};if(token)headers.Authorization=protocol.token_type+' '+token;const r=await fetch('/api/v1/'+encodeURIComponent(s)+'/'+encodeURIComponent(a),{method:'POST',headers,body:JSON.stringify(body)});const data=await r.json();q('output').textContent=JSON.stringify({http_status:r.status,...data},null,2);return data}q('login').onclick=async()=>{const r=await call(protocol.login_service,protocol.login_action,{data:{username:q('username').value,password:q('password').value}});if(r.data&&r.data.token){token=r.data.token;q('who').textContent=' 已登录 '+r.data.user_id}};q('send').onclick=()=>{try{return call(q('service').value,q('action').value,JSON.parse(q('payload').value))}catch(e){q('output').textContent=e.message}};</script></html>`
