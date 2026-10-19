#!/usr/bin/env python3
"""Atlas audit: meta, anchors, cross-repo links, orphans, staleness, size budget, unmapped docs, unabsorbed ADRs, guard entries.

Run from the directory that holds the tree (repo root or materials dir).
--atlas DIR: where the tree lives, default docs/atlas.
"""
import re, subprocess, pathlib, argparse

ap = argparse.ArgumentParser()
ap.add_argument('--atlas', default='docs/atlas')
args = ap.parse_args()

ROOT = pathlib.Path('.').resolve()
ENTRY = ROOT / 'AGENTS.md'
ATLAS = ROOT / args.atlas
TREE_ROOT = ATLAS / 'README.md'
GUARD = ROOT / 'docs/guard'  # 已经做好的旅程，一条一个文件；由根节点的「已有旅程」节指向
BUDGET = {'entry': 60, 'node': 250}
nodes = [p for p in [ENTRY, *sorted(ATLAS.rglob('*.md'))] if p.exists()]
SKIP_DIRS = {'.git', '.github', 'node_modules', 'vendor', 'dist', 'build', '.venv', 'venv'}  # .github：issue/PR 模板是平台文件
SKIP_FILES = {'CLAUDE.md', 'AGENTS.md'}  # CLAUDE.md 只含 @AGENTS.md；目录级 AGENTS.md 由 Agent 自动加载
linked, issues = set(), []


def front(p):
    m = re.match(r'---\n(.*?)\n---', p.read_text(encoding='utf-8'), re.S)
    return m.group(1) if m else ''


def field_list(head, key):
    m = re.search(rf'^{key}:\s*\[(.*?)\]', head, re.M)
    return [a.strip() for a in m.group(1).split(',') if a.strip()] if m else []


def field(head, key):
    m = re.search(rf'^{key}:\s*([^\s#]+)', head, re.M)
    return m.group(1) if m else None


for p in nodes:
    text = p.read_text(encoding='utf-8')
    head = front(p)
    rel = p.relative_to(ROOT)

    if p != ENTRY and not field(head, 'kind'):
        issues.append(f'[meta]   {rel}: missing kind in frontmatter')
    if re.search(r'^(<{7} |={7}$|>{7} )', text, re.M):
        issues.append(f'[merge]  {rel}: leftover merge conflict markers')
    budget = BUDGET['entry' if p == ENTRY else 'node']
    lines = text.count('\n')
    if lines > budget:
        issues.append(f'[size]   {rel}: {lines} lines > {budget}')

    # code / source 指向本仓库的代码和资料，相对树根所在目录
    code = field_list(head, 'code')
    for a in [*code, *field_list(head, 'source')]:
        if not (ROOT / a).exists():
            issues.append(f'[anchor] {rel}: missing {a}')

    # atlas / up 指向另一个仓库的节点，相对本文件，且必须互相指向
    for a in field_list(head, 'atlas'):
        t = (p.parent / a).resolve()
        if not t.exists():
            issues.append(f'[atlas]  {rel}: missing {a}')
        elif (t.parent / (field(front(t), 'up') or '')).resolve() != p.resolve():
            issues.append(f'[atlas]  {rel}: {a} has no up: pointing back here')
    up = field(head, 'up')
    if up:
        t = (p.parent / up).resolve()
        if not t.exists():
            issues.append(f'[atlas]  {rel}: missing up {up}')
        elif p.resolve() not in {(t.parent / a).resolve() for a in field_list(front(t), 'atlas')}:
            issues.append(f'[atlas]  {rel}: up {up} does not list this tree in atlas:')

    ver = re.search(r'^verified:.*@\s*([0-9a-f]{7,40})', head, re.M)
    if code and ver:
        r = subprocess.run(['git', 'rev-list', '--count', f'{ver.group(1)}..HEAD', '--', *code],
                           capture_output=True, text=True)
        if r.returncode:
            issues.append(f'[stale?] {rel}: verified commit {ver.group(1)} not found')
        elif int(r.stdout.strip() or 0) > 0:
            issues.append(f'[stale?] {rel}: {r.stdout.strip()} commits touched its code since verified')
    elif code:
        issues.append(f'[stale?] {rel}: no verified commit')

    for link in re.findall(r'\]\(((?!https?:|mailto:|<)[^)\s#]+)', text):
        target = (p.parent / link).resolve()
        if not target.exists():
            issues.append(f'[link]   {rel}: broken link {link}')
        elif target.suffix == '.md' or target.is_dir():
            linked.add(target)

for p in nodes:
    if p not in (ENTRY, TREE_ROOT) and p.resolve() not in linked:
        issues.append(f'[orphan] {p.relative_to(ROOT)}: no node links here')

# 已经做好的旅程：docs/guard/ 一条一个文件。步骤和「怎样算对」只有主人能改，
# 冒烟用例和级别由 AI 同步；这里只查机械能核的部分。
guard_files = sorted(GUARD.glob('*.md')) if GUARD.is_dir() else []
if guard_files:
    if 'docs/guard' not in TREE_ROOT.read_text(encoding='utf-8'):
        issues.append('[guard]  docs/guard/ exists but the tree root does not mention it')
    linked.add(GUARD.resolve())
    for p in guard_files:
        text = p.read_text(encoding='utf-8')
        rel = p.relative_to(ROOT)
        smoke = re.search(r'^\*\*冒烟用例\*\*[：:]\s*(\S+)', text, re.M)
        level = re.search(r'^\*\*级别\*\*[：:]\s*(\S+)', text, re.M)
        if not smoke:
            issues.append(f'[guard]  {rel}: no 冒烟用例')
        elif smoke.group(1) not in ('待写', '无'):
            for one in smoke.group(1).split('、'):
                if not (ROOT / one).exists():
                    issues.append(f'[guard]  {rel}: 冒烟用例 {one} does not exist')
        if not level:
            issues.append(f'[guard]  {rel}: no 级别')
        if not re.search(r'^\*\*怎样算对\*\*', text, re.M):
            issues.append(f'[guard]  {rel}: no 怎样算对')
        if re.search(r'^kind:', front(p), re.M):
            issues.append(f'[guard]  {rel}: guard entries carry no kind frontmatter')

# 开发态设计：docs/dev/ 放还没落地的设计，落地就删。这里只查机械能核的部分：
# 有没有写「还没做的」，以及里面提到的代码是否已经存在（可能已经落地该删了）。
DEV = ROOT / 'docs/dev'
dev_files = sorted(DEV.glob('*.md')) if DEV.is_dir() else []
for p in dev_files:
    rel = p.relative_to(ROOT)
    text = p.read_text(encoding='utf-8')
    if not re.search(r'^##\s*还没做的', text, re.M):
        issues.append(f'[dev]    {rel}: no 「还没做的」section; a design with nothing left to do should be deleted into the atlas')

# 每条有效 ADR 都要被某个 atlas 节点直接引用（链到 docs/adr/ 目录不算）
for p in sorted((ROOT / 'docs/adr').glob('*.md')):
    if re.search(r'^status:\s*(Superseded|Deprecated|Rejected)', p.read_text(encoding='utf-8'), re.M | re.I):
        continue
    if p.resolve() not in linked:
        issues.append(f'[unabsorbed] {p.relative_to(ROOT)}: no atlas node cites this decision')

# 每份文档都要能从 atlas 走到：直接链接到它，或链接到它所在的目录
node_set = {n.resolve() for n in nodes}
for p in sorted(ROOT.rglob('*.md')):
    rp = p.resolve()
    if SKIP_DIRS & set(p.relative_to(ROOT).parts) or p.name in SKIP_FILES or rp in node_set:
        continue
    if rp not in linked and not any(d in rp.parents for d in linked if d.is_dir()):
        issues.append(f'[unmapped] {p.relative_to(ROOT)}: not reachable from atlas document map')

print('\n'.join(issues) or 'atlas OK')
