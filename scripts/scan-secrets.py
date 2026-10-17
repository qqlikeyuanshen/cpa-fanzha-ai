#!/usr/bin/env python3
"""Heuristic release-tree scan. Never prints matched secret values. Not a Git-history audit."""
from pathlib import Path
import hashlib
import re
import sys

ROOT = Path(__file__).resolve().parents[1]
SKIP_DIRS = {'.git', 'dist', 'build', '__pycache__', 'node_modules', '.venv', 'venv'}
RULES = {
    'private-key': re.compile(r'-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----'),
    'jwt-token': re.compile(r'\beyJ[A-Za-z0-9_-]{16,}\.[A-Za-z0-9_-]{16,}\.[A-Za-z0-9_-]{16,}'),
    'google-api-key': re.compile(r'\bAIza[A-Za-z0-9_-]{30,}'),
    'github-token': re.compile(r'\b(?:gh[pousr]_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{30,})'),
    'api-key': re.compile(r'\bsk-[A-Za-z0-9_-]{24,}'),
    'cloud-access-id': re.compile(r'\b(?:AKIA|ASIA)[A-Z0-9]{16}\b'),
    'authenticated-url': re.compile(r'https?://[^\s/:@]+:[^\s/@]+@'),
    'private-workspace': re.compile(r'/root/[A-Za-z0-9_.-]+|[A-Za-z]:[\\/](?:Users|dsh-home|my)[\\/]'),
    'literal-ipv4-needs-review': re.compile(r'(?<![\d.])(?:\d{1,3}\.){3}\d{1,3}(?![\d.])'),
    'literal-long-credential': re.compile(r'''["'](?:access_token|refresh_token|password|secret-key|api-key|management_key)["']\s*[:=]\s*["']([^"'\r\n]{24,})["']'''),
}
FORBIDDEN_DIRS = {'auth', 'auths', '.cli-proxy-api', 'private', 'logs', 'backups', 'reference', 'browser-test-profile'}
FORBIDDEN_SUFFIXES = {'.pem', '.p12', '.pfx', '.sqlite', '.sqlite3', '.log', '.db', '.so', '.dll', '.exe', '.zip', '.tar', '.gz'}
# Exact, reviewed synthetic fixture: this negative test rejects URL userinfo.
# No general exemption for test files or real authenticated URLs.
MOCK_ALLOWLIST = {('registration_test.go', 'authenticated-url', 'https://user:pass@')}
# Original display asset restored at the maintainer's request; verify exact bytes.
REVIEWED_ASSETS = {'logo.png': '965e0e0e3e9313348db6c482e425d364f6881c6689b71b70f53fb5a791bf98db'}
issues = []
count = 0
reviewed_mocks = 0
for p in sorted(ROOT.rglob('*')):
    rel = p.relative_to(ROOT)
    if any(part in SKIP_DIRS for part in rel.parts):
        continue
    if p.is_symlink():
        issues.append((str(rel), 0, 'symlink-needs-review'))
        continue
    if not p.is_file():
        continue
    count += 1
    if any(part in FORBIDDEN_DIRS for part in rel.parts) or p.suffix.lower() in FORBIDDEN_SUFFIXES or p.name in {'config.yaml', 'config.yml', '.env'} or p.name.startswith(('vertex-', 'fanzha-')) and p.suffix == '.json':
        issues.append((str(rel), 0, 'runtime-or-binary-file'))
    if p.resolve() == Path(__file__).resolve():
        # This script contains detection signatures, not actual credentials.
        continue
    if rel.as_posix() in REVIEWED_ASSETS:
        if hashlib.sha256(p.read_bytes()).hexdigest() != REVIEWED_ASSETS[rel.as_posix()]:
            issues.append((str(rel), 0, 'reviewed-asset-hash-changed'))
        continue
    try:
        text = p.read_text(encoding='utf-8')
    except UnicodeDecodeError:
        issues.append((str(rel), 0, 'unreviewed-binary-file'))
        continue
    for name, pattern in RULES.items():
        for match in pattern.finditer(text):
            if (rel.as_posix(), name, match.group(0)) in MOCK_ALLOWLIST:
                reviewed_mocks += 1
                continue
            issues.append((str(rel), text.count('\n', 0, match.start()) + 1, name))
for filename, line, rule in issues:
    print(f'{filename}:{line}: REVIEW {rule}')
if issues:
    print(f'FAIL: {len(issues)} finding(s); no matched values printed.')
    sys.exit(1)
print(f'PASS: {count} release-tree files scanned; {reviewed_mocks} exact synthetic fixture(s) reviewed; build output and Git internals excluded.')
print('This is a heuristic working-tree check, not proof of absence of secrets or a Git-history scan.')
