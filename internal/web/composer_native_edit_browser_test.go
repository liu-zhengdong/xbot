package web

import (
	"testing"
)

// Native editing on the shipped page, driven with real browser edit commands
// (document.execCommand), not value assignment: at the 16000 cap an insertion
// is refused whole so the existing draft survives no matter where the caret
// was — the old textarea's maxlength, not post-hoc slicing (review R1) — and
// undo/redo, ours because every re-render retires the native history,
// round-trips typed and deleted text (review R2). These assertions fail on
// the pre-fix HEAD, which accepted the insertion and dropped the tail.
func TestComposerNativeEditBrowser(t *testing.T) {
	runPageInChrome(t, `
const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const waitFor=async(fn,msg)=>{for(let i=0;i<600;i++){if(fn())return;await new Promise(r=>setTimeout(r,10))}throw Error(msg)};
const box=$('#input');
const set=s=>{box.textContent=s;box.dispatchEvent(new Event('input'));return box};
const caretAt=where=>{box.focus();const r=document.createRange();r.selectNodeContents(box);r.collapse(where==='start');getSelection().removeAllRanges();getSelection().addRange(r)};
const undo=()=>box.dispatchEvent(new KeyboardEvent('keydown',{key:'z',ctrlKey:true,metaKey:true,bubbles:true}));
const redo=()=>box.dispatchEvent(new KeyboardEvent('keydown',{key:'z',keyCode:90,ctrlKey:true,metaKey:true,shiftKey:true,bubbles:true}));
(async()=>{
 await waitFor(()=>!$('#shell').hidden&&$('#title').textContent.includes('2号小伙子'),'app did not boot');
 // R2 first, on the fresh composer: a typed word undoes with the z key and
 // redoes with z+shift.
 caretAt('start');
 document.execCommand('insertText',false,'hello');
 assert(box.textContent==='hello','native insertText failed');
 undo();
 assert(box.textContent==='','undo did not clear the typed text: '+JSON.stringify(box.textContent));
 redo();
 assert(box.textContent==='hello','redo did not bring the text back: '+JSON.stringify(box.textContent));
 undo();
 undo(); // already at the empty baseline: a further undo must stay put
 assert(box.textContent==='','undo went past the baseline: '+JSON.stringify(box.textContent));
 // Each typed character is one step: undo peels them back, redo replays.
 caretAt('end');
 document.execCommand('insertText',false,'w');
 document.execCommand('insertText',false,'o');
 document.execCommand('insertText',false,'w');
 assert(box.textContent==='wow','three native inserts failed');
 undo();
 assert(box.textContent==='wo','first undo wrong: '+JSON.stringify(box.textContent));
 undo();
 assert(box.textContent==='w','second undo wrong: '+JSON.stringify(box.textContent));
 undo();
 assert(box.textContent==='','third undo wrong: '+JSON.stringify(box.textContent));
 redo();
 redo();
 redo();
 assert(box.textContent==='wow','redo chain wrong: '+JSON.stringify(box.textContent));
 // A fresh edit abandons the redo tail and undoes on its own.
 document.execCommand('insertText',false,'!');
 assert(box.textContent==='wow!','append after redo failed');
 undo();
 assert(box.textContent==='wow','undo after append wrong: '+JSON.stringify(box.textContent));
 // R1: at the cap, inserting at the start is refused — the trailing Z, far
 // from the caret, survives.
 set('a'.repeat(15999)+'Z');
 caretAt('start');
 document.execCommand('insertText',false,'X');
 assert(box.textContent.length===16000,'cap length changed: '+box.textContent.length);
 assert(box.textContent.startsWith('aaa'),'over-cap insertion was accepted: '+box.textContent.slice(0,3));
 assert(box.textContent.endsWith('aaZ'),'existing last character was lost: '+box.textContent.slice(-3));
 // Deleting still works at the cap, and undo brings the character back.
 caretAt('end');
 document.execCommand('delete');
 assert(box.textContent==='a'.repeat(15999),'delete at the cap broke: '+box.textContent.length);
 undo();
 assert(box.textContent==='a'.repeat(15999)+'Z','undo did not restore the deleted character: '+box.textContent.slice(-3));
 document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`)
}
