from pathlib import Path
import json
import re

root = Path(__file__).resolve().parent
files = ['README.md', '01-architecture.md', '02-domain-security.md', '03-execution-integrations.md', '04-product-delivery.md', '05-operations-assurance.md']
texts = {name: (root / name).read_text() for name in files}
errors = []
for name, text in texts.items():
    if len(re.findall(r'^```', text, re.M)) % 2:
        errors.append(f'{name}: unbalanced code fences')
ops = texts['05-operations-assurance.md']
scenario_ids = re.findall(r'^#### (AT-\d{3})\b', ops, re.M)
req_ids = re.findall(r'^\| (OA-\d{2}) \|', ops, re.M)
expected_scenarios = [f'AT-{i:03d}' for i in range(1, 53)]
expected_reqs = [f'OA-{i:02d}' for i in range(1, 21)]
if scenario_ids != expected_scenarios:
    errors.append('scenario IDs missing, duplicated or out of sequence')
if sorted(set(req_ids)) != expected_reqs:
    errors.append('requirement definition set mismatch')
for match in re.finditer(r'^#### (AT-\d{3})[^\n]*\n(.*?)(?=^#### |^## |\Z)', ops, re.M | re.S):
    if not re.search(r'OA-\d{2}', match.group(0).splitlines()[0]):
        errors.append(f'{match.group(1)}: missing invariant trace')
    if not match.group(2).strip():
        errors.append(f'{match.group(1)}: empty scenario')
for ref in set(re.findall(r'\bOA-\d{2}\b', ops)):
    if ref not in expected_reqs:
        errors.append(f'unknown invariant {ref}')
roadmap = texts['04-product-delivery.md']
blocks = re.findall(r'^### (PD-\d{2})[^\n]*\n(.*?)(?=^### PD-|^## |\Z)', roadmap, re.M | re.S)
graph = {}
for mid, body in blocks:
    dep = re.search(r'\*\*Dependencies:\*\*([^\n]*)', body)
    if not dep:
        errors.append(f'{mid}: missing dependency declaration')
    graph[mid] = sorted(set(re.findall(r'PD-\d{2}', dep.group(1) if dep else '')))
    for field in ['Owner', 'Deliver', 'Accept']:
        if f'**{field}:' not in body:
            errors.append(f'{mid}: missing {field}')
if sorted(graph) != [f'PD-{i:02d}' for i in range(13)]:
    errors.append('milestone set mismatch')
visiting, visited = set(), set()
def visit(node):
    if node in visiting:
        errors.append(f'milestone dependency cycle at {node}')
        return
    if node in visited:
        return
    visiting.add(node)
    for dep in graph[node]:
        if dep not in graph:
            errors.append(f'{node}: undefined dependency {dep}')
        else:
            visit(dep)
    visiting.remove(node)
    visited.add(node)
for node in graph:
    visit(node)
races = re.findall(r'^\| \*\*(R\d{2})\*\*', texts['02-domain-security.md'], re.M)
if races != [f'R{i:02d}' for i in range(1, 11)]:
    errors.append('race protocol set mismatch')
result = {'status': 'pass' if not errors else 'fail', 'scope': 'document structure and references only; runtime scenarios NOT RUN', 'documents': len(files), 'scenario_count': len(scenario_ids), 'invariant_count': len(set(req_ids)), 'race_protocol_count': len(races), 'milestone_count': len(graph), 'milestone_dependencies': graph, 'errors': errors}
(root / 'validation.json').write_text(json.dumps(result, indent=2) + '\n')
print(json.dumps(result, indent=2))
raise SystemExit(bool(errors))
