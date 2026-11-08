#!/usr/bin/env python3
"""Verify packager signatures and run the packaged host in an isolated launchd job."""
import argparse
from contextlib import contextmanager
import hashlib
import json
import os
from pathlib import Path
import plistlib
import re
import select
import socket
import subprocess
import tempfile
import time
import urllib.request
import uuid


def designated_requirement(output):
    lines = [line.strip() for line in output.splitlines() if line.strip().startswith('designated =>')]
    if (len(lines) != 1 or 'cdhash ' in lines[0]
            or not re.search(r'\b(certificate|anchor)\b', lines[0])):
        raise ValueError('A certificate-based designated requirement is required')
    return lines[0]


def compare_builds(previous, current):
    if previous['version'] == current['version']:
        raise ValueError('Compare two different release versions')
    if previous['requirements'] != current['requirements']:
        raise ValueError('Designated requirements changed across builds')
    if previous['hashes'] == current['hashes']:
        raise ValueError('The comparison must include changed executable bytes')


def run(command):
    return subprocess.run(command, check=True, text=True, capture_output=True, timeout=30)


def signed_identity(app, expected_authority):
    info = plistlib.loads((app / 'Contents/Info.plist').read_bytes())
    if info['CFBundleIdentifier'] != 'app.librarystudiokh.desktop':
        raise ValueError('Unexpected packaged application identifier')
    run(['codesign', '--verify', '--deep', '--strict', str(app)])
    result = {'version': info['CFBundleShortVersionString'], 'requirements': {}, 'hashes': {}}
    for relative in ['.', 'Contents/MacOS/library-studio-kh', 'Contents/MacOS/library-studio-server']:
        path = app if relative == '.' else app / relative
        description = run(['codesign', '-d', '-vvv', '-r-', str(path)])
        output = description.stdout + description.stderr
        if 'Authority=' + expected_authority not in output.splitlines():
            raise ValueError('Packaged code does not use the configured signing identity')
        result['requirements'][relative] = designated_requirement(output)
        if relative != '.':
            result['hashes'][relative] = hashlib.sha256(path.read_bytes()).hexdigest()
    return result


def launchd_start(app, expected_version, work):
    config = work / 'config'
    config.mkdir(mode=0o700)
    # Blank native connections ensure this test never calls real stock apps or NAS.
    (config / 'settings.json').write_text(json.dumps({
        'qbittorrent': {}, 'sonarr': {}, 'radarr': {}, 'lidarr': {}, 'prowlarr': {},
        'bazarr': {}, 'cleanupOn': False, 'stagedCleanupOn': False,
    }))
    with socket.socket() as reserved:
        reserved.bind(('127.0.0.1', 0))
        port = reserved.getsockname()[1]
    label = 'app.librarystudiokh.signing-test.' + uuid.uuid4().hex
    domain = 'gui/' + str(os.getuid())
    service = domain + '/' + label
    job = work / 'host.plist'
    job.write_bytes(plistlib.dumps({
        'Label': label,
        'ProgramArguments': [str(app / 'Contents/MacOS/library-studio-server'), '-service', '-port', str(port)],
        'EnvironmentVariables': {
            'LIBRARY_STUDIO_HOME': str(config),
            'LIBRARY_STUDIO_LAUNCHAGENT_DIR': str(work / 'test-agents'),
        },
        'RunAtLoad': True,
        'StandardOutPath': str(work / 'host.stdout'),
        'StandardErrorPath': str(work / 'host.stderr'),
    }))
    queue = select.kqueue()
    fd = os.open(config, os.O_RDONLY)
    queue.control([select.kevent(fd, filter=select.KQ_FILTER_VNODE,
        flags=select.KQ_EV_ADD | select.KQ_EV_CLEAR, fflags=select.KQ_NOTE_WRITE)], 0, 0)
    try:
        run(['launchctl', 'bootstrap', domain, str(job)])
        descriptor = config / 'host.json'
        deadline = time.monotonic() + 30
        while not descriptor.exists():
            remaining = deadline - time.monotonic()
            if remaining <= 0 or not queue.control(None, 1, remaining):
                raise RuntimeError('Packaged launchd host did not become ready; retained test logs identify startup failures')
        data = json.loads(descriptor.read_text())
        if data['address'] != '127.0.0.1:' + str(port):
            raise ValueError('Unexpected isolated host listener')
        def get(path):
            req = urllib.request.Request('http://' + data['address'] + path,
                headers={'X-Library-Token': data['token']})
            with urllib.request.urlopen(req, timeout=15) as response:
                return json.load(response)
        health = get('/api/health')
        if not health['ok'] or health['version'] != expected_version:
            raise ValueError('Packaged launchd host version/health mismatch')
        cleanup = get('/api/cleanup/status')
        if cleanup['enabled']:
            raise ValueError('Isolated launchd cleanup must remain disabled')
        # Check survival after startup; a launchctl bootstrap exit alone is insufficient.
        queue.control([select.kevent(fd, filter=select.KQ_FILTER_VNODE,
            flags=select.KQ_EV_DELETE)], 0, 0)
        queue.control([select.kevent(1, filter=select.KQ_FILTER_TIMER,
            flags=select.KQ_EV_ADD | select.KQ_EV_ONESHOT, data=2000)], 0, 0)
        if not queue.control(None, 1, 3):
            raise RuntimeError('Startup survival timer did not complete')
        state = run(['launchctl', 'print', service]).stdout
        if not re.search(r'\bpid = ' + str(int(data['pid'])) + r'\b', state) or get('/api/health') != health:
            raise RuntimeError('Packaged host did not survive launchd startup')
        return {'started': True, 'health': True, 'survivedStartup': True, 'cleanupOff': True}
    finally:
        queue.close()
        os.close(fd)
        found = subprocess.run(['launchctl', 'print', service], capture_output=True)
        if found.returncode == 0:
            run(['launchctl', 'bootout', service])


@contextmanager
def packaged_app(package, work):
    if package.suffix.lower() != '.dmg':
        yield package
        return
    mount = work / 'mounted'
    mount.mkdir()
    attached = run(['hdiutil', 'attach', '-readonly', '-nobrowse', '-plist', '-mountpoint', str(mount), str(package)])
    entities = plistlib.loads(attached.stdout.encode())['system-entities']
    device = next(entity['dev-entry'] for entity in entities
        if re.fullmatch(r'/dev/disk\d+', entity.get('dev-entry', '')))
    try:
        yield mount / 'Library Studio KH.app'
    finally:
        run(['hdiutil', 'detach', device])


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('app', type=Path)
    parser.add_argument('--identity', default='KH Studio Local Signing')
    parser.add_argument('--previous-receipt', type=Path)
    parser.add_argument('--receipt', type=Path, required=True)
    args = parser.parse_args()
    work = Path(tempfile.mkdtemp(prefix='library-signing-test-', dir=args.receipt.parent))
    with packaged_app(args.app.resolve(), work) as app:
        result = signed_identity(app, args.identity)
        if args.previous_receipt:
            compare_builds(json.loads(args.previous_receipt.read_text()), result)
            result['requirementsUnchangedAcrossBuilds'] = True
        result['launchd'] = launchd_start(app, result['version'], work)
    result['testWork'] = str(work)
    args.receipt.write_text(json.dumps(result, indent=2) + '\n')
    print('Packager signature, certificate-based requirements and isolated launchd start: PASS')


if __name__ == '__main__':
    main()
