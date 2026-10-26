package web

import "testing"

// Exercise actual DOM selections on the shipped page. Element offsets are
// child indexes (including the editor root), not character offsets. Native
// edits, IME commits and deletion can all leave these boundary selections.
func TestComposerCaretBrowser(t *testing.T) {
	runPageInChrome(t, `
const $=s=>document.querySelector(s),assert=(ok,msg)=>{if(!ok)throw Error(msg)};
const waitFor=async(fn,msg)=>{for(let i=0;i<600;i++){if(fn())return;await new Promise(r=>setTimeout(r,10))}throw Error(msg)};
const box=$('#input');
const at=(node,offset)=>{box.focus();getSelection().collapse(node,offset)};
// Read the resulting caret independently of the application's offset helper.
const caret=()=>{const r=document.createRange(),s=getSelection();r.selectNodeContents(box);r.setEnd(s.focusNode,s.focusOffset);return r.toString().length};
const edit=s=>document.execCommand('insertText',false,s);
const undo=()=>box.dispatchEvent(new KeyboardEvent('keydown',{key:'z',ctrlKey:true,bubbles:true}));
const redo=()=>box.dispatchEvent(new KeyboardEvent('keydown',{key:'z',ctrlKey:true,shiftKey:true,bubbles:true}));
const refresh=()=>box.dispatchEvent(new InputEvent('input',{inputType:'insertText',bubbles:true}));
(async()=>{
 await waitFor(()=>!$('#shell').hidden&&$('#title').textContent.includes('2号小伙子'),'app did not boot');
 // Type, select all, delete, then type and edit in the middle again.
 at(box,0);edit('若干字 **粗体** https://example.com');
 const all=document.createRange();all.selectNodeContents(box);getSelection().removeAllRanges();getSelection().addRange(all);
 document.execCommand('delete');
 assert(box.textContent.trim()==='','delete-all left text');
 edit('甲乙');
 at(box.firstChild,1);edit('中');
 assert(box.textContent.trim()==='甲中乙','retyping after delete-all inserted in the wrong place');
 assert(caret()===2,'middle insertion after delete-all jumped to end: '+caret());
 // A committed edit may leave the caret at a root child boundary. Keep
 // trailing text so a mistaken end-of-draft restore is observable.
 box.replaceChildren(document.createTextNode('甲'),document.createTextNode('乙'));
 at(box,1);refresh();
 assert(caret()===1,'root boundary jumped to end: '+caret());
 edit('中');
 assert(box.textContent==='甲中乙'&&caret()===2,'next character did not follow the root insertion point');
 undo();redo();
 assert(box.textContent==='甲中乙'&&caret()===2,'redo lost the middle caret');
 // A nested highlight boundary counts its preceding children, not zero.
 const span=document.createElement('span');span.textContent='**粗**';
 box.replaceChildren(document.createTextNode('前'),span,document.createTextNode('后'));
 at(span,1);refresh();
 assert(caret()===6,'highlight element end mapped to its start: '+caret());
 edit('中');
 assert(box.textContent==='前**粗**中后'&&caret()===7,'highlight boundary insertion moved');
 assert(box.querySelector('.md-bold'),'Markdown highlighting lost');
 // Residual empty nodes, BR and DIV from native editing use the same
 // newline accounting for text and selection, including after a block.
 const div=document.createElement('div');div.textContent='甲';
 box.replaceChildren(document.createTextNode(''),document.createElement('br'),div,document.createTextNode('乙'));
 at(box,3);refresh();
 assert(box.textContent==='\n甲\n乙'&&caret()===3,'block boundary did not round-trip');
 edit('中');
 assert(box.textContent==='\n甲\n中乙'&&caret()===4,'block boundary insertion jumped');
 // IME owns its DOM until compositionend; then the same boundary mapping
 // must apply once, even if the browser sends a final input afterwards.
 box.dispatchEvent(new CompositionEvent('compositionstart'));
 box.replaceChildren(document.createTextNode('中文'),document.createTextNode('尾'));
 at(box,1);refresh();
 assert(getSelection().focusNode===box,'input repainted during composition');
 box.dispatchEvent(new CompositionEvent('compositionend',{data:'中文'}));
 refresh();
 assert(box.textContent==='中文尾'&&caret()===2,'IME commit jumped to end');
 edit('好');
 assert(box.textContent==='中文好尾'&&caret()===3,'typing after IME moved');
 // Plain typing needs no highlighting changes: retain the browser's text
 // node and selection, rather than tearing them down on every input.
 const nativeNode=getSelection().focusNode;
 edit('！');
 assert(box.textContent==='中文好！尾'&&caret()===4,'plain typing moved the caret');
 assert(getSelection().focusNode===nativeNode,'plain typing unnecessarily replaced the selected node');
 // A refused over-cap edit uses the pre-edit root boundary, not Infinity.
 box.textContent='a'.repeat(15999)+'Z';refresh();at(box,0);
 box.dispatchEvent(new InputEvent('beforeinput',{inputType:'insertText',data:'X',bubbles:true,cancelable:true}));
 edit('X');
 assert(box.textContent==='a'.repeat(15999)+'Z'&&caret()===0,'refused root-start edit jumped to end');
 document.body.dataset.testResult='passed';
})().catch(e=>{document.body.dataset.testResult='failed: '+e.message});
`)
}
