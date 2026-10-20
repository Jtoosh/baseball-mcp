package app

import (
	"net/http/httptest"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

func TestBrowserRateLimitKeepsPreviousAnswerAndStopsLoading(t *testing.T) {
	_, app := testApp(t, &scriptedModel{})
	asset := httptest.NewRecorder()
	app.ServeHTTP(asset, httptest.NewRequest("GET", "/assets/app.js", nil))
	// Execute the served browser program against its DOM/fetch boundary. No
	// provider, browser account or server internals are replaced.
	fixture := `
const vm=require('node:vm');
class Node {
 constructor(){this.children=[];this.listeners={};this.hidden=true;this.textContent='';this.attributes={};}
 append(...nodes){this.children.push(...nodes);}
 replaceChildren(...nodes){this.children=nodes;}
 hasChildNodes(){return this.children.length>0;}
 addEventListener(event,callback){this.listeners[event]=callback;}
 setAttribute(name,value){this.attributes[name]=value;}
}
const nodes=new Map();
const get=id=>{if(!nodes.has(id))nodes.set(id,new Node());return nodes.get(id)};
const button=new Node();
const oldAnswer=new Node();oldAnswer.textContent='Yordan Alvarez led MLB.';
get('#results').append(oldAnswer);
get('#question').value='Who led MLB in OPS?';
const context={
 console,
 document:{querySelector:get,querySelectorAll:()=>[button],createElement:()=>new Node(),createDocumentFragment:()=>new Node()},
 marked:{Marked:class{}},DOMPurify:{},
 fetch:async path=>path==='/api/client-config'?{ok:true,json:async()=>({mode:'local'})}:{ok:false,status:429,headers:{get:()=> '125'},json:async()=>({kind:'error',message:'Someone on your network already has a question loading.'})}
};
vm.runInNewContext(SOURCE,context);
get('#question-form').listeners.submit({preventDefault(){}});
setImmediate(()=>{
 const text=node=>node.textContent+' '+node.children.map(text).join(' ');
 if(!text(get('#request-error')).includes('125 seconds'))throw Error('Retry-After guidance missing: '+text(get('#request-error')));
 if(get('#results').children[0]!==oldAnswer)throw Error('Previous answer lost');
 if(get('#request-error').hidden || get('#previous-label').hidden)throw Error('Error or previous-answer label hidden');
 if(!get('#loading-ball').hidden || get('#status-text').textContent!=='' || button.disabled || get('#results').attributes['aria-busy']!=='false')throw Error('Loading state stuck');
});
`
	fixture = strings.Replace(fixture, "SOURCE", strconv.Quote(asset.Body.String()), 1)
	command := exec.Command("node", "-")
	command.Stdin = strings.NewReader(fixture)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("browser rate-limit behavior failed: %v\n%s", err, output)
	}
}
