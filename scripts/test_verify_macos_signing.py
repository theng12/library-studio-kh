"""Pure receipt checks; never invoke launchd, Keychain or real media services."""
import importlib.util
from pathlib import Path
import unittest
from unittest.mock import patch
from types import SimpleNamespace
import sys

sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location('verify_signing', Path(__file__).with_name('verify-macos-signing.py'))
verify = importlib.util.module_from_spec(spec)
spec.loader.exec_module(verify)


class SigningChecks(unittest.TestCase):
    def test_certificate_requirement(self):
        requirement = 'designated => identifier "app.example" and anchor H"abc123"'
        self.assertEqual(verify.designated_requirement('Executable=test\n' + requirement), requirement)
        for invalid in ['', 'designated => cdhash H"abc"', 'designated => identifier "app.example"', requirement + '\n' + requirement]:
            with self.subTest(invalid=invalid), self.assertRaises(ValueError):
                verify.designated_requirement(invalid)

    def test_signer_description_requests_authority_output(self):
        def codesign(command):
            if '-d' in command:
                self.assertIn('-vvv', command)
                return SimpleNamespace(stdout='', stderr='Authority=KH Studio Local Signing\ndesignated => certificate leaf = H"abc"\n')
            return SimpleNamespace(stdout='', stderr='')
        with patch.object(verify, 'run', side_effect=codesign), patch.object(verify.plistlib, 'loads', return_value={
                'CFBundleIdentifier': 'app.librarystudiokh.desktop', 'CFBundleShortVersionString': '0.7.4'}), patch.object(Path, 'read_bytes', return_value=b'fixture'):
            receipt = verify.signed_identity(Path('fixture.app'), 'KH Studio Local Signing')
        self.assertEqual(len(receipt['requirements']), 3)
        self.assertEqual(len(receipt['hashes']), 2)

    def test_dmg_detaches_by_device_even_when_check_fails(self):
        attached = verify.plistlib.dumps({'system-entities': [
            {'dev-entry': '/dev/disk99'}, {'dev-entry': '/dev/disk99s1', 'mount-point': '/Volumes/Assigned Fixture'}]}).decode()
        with patch.object(verify, 'run', return_value=SimpleNamespace(stdout=attached)) as command:
            with self.assertRaises(RuntimeError):
                with verify.packaged_app(Path('fixture.dmg'), Path('work')) as app:
                    self.assertEqual(app, Path('/Volumes/Assigned Fixture/Library Studio KH.app'))
                    raise RuntimeError('fixture failure')
            self.assertEqual(command.call_args.args[0], ['hdiutil', 'detach', '/dev/disk99'])
            self.assertIn('-readonly', command.call_args_list[0].args[0])
            self.assertNotIn('-mountpoint', command.call_args_list[0].args[0])

    def test_missing_mount_point_still_detaches_device(self):
        attached = verify.plistlib.dumps({'system-entities': [{'dev-entry': '/dev/disk99'}]}).decode()
        with patch.object(verify, 'run', return_value=SimpleNamespace(stdout=attached)) as command:
            with self.assertRaisesRegex(ValueError, 'mount point'):
                with verify.packaged_app(Path('fixture.dmg'), Path('work')):
                    self.fail('missing mount point accepted')
            self.assertEqual(command.call_args.args[0], ['hdiutil', 'detach', '/dev/disk99'])

    def test_differing_builds_keep_requirements(self):
        first = {'version': '0.7.4', 'requirements': {'app': 'anchor H"abc"'}, 'hashes': {'server': 'one'}}
        second = {**first, 'version': '0.7.5', 'hashes': {'server': 'two'}}
        verify.compare_builds(first, second)
        for invalid in [first, {**second, 'requirements': {'app': 'anchor H"other"'}}, {**second, 'hashes': first['hashes']}]:
            with self.subTest(invalid=invalid), self.assertRaises(ValueError):
                verify.compare_builds(first, invalid)

    def test_launchd_submit_uses_isolated_config_env_and_cleans_up_on_failure(self):
        import tempfile
        calls = []
        bootout_called = []

        def fake_run(command):
            calls.append(list(command))
            if command[0] == 'launchctl' and command[1] in ('submit', 'print', 'bootout'):
                if command[1] == 'bootout':
                    bootout_called.append(list(command))
                return SimpleNamespace(returncode=0, stdout='pid = 99999\n', stderr='')
            raise AssertionError('unexpected run command: ' + repr(command))

        def fake_subprocess_run(command, **kwargs):
            calls.append(list(command))
            if command[0] == 'launchctl' and command[1] == 'print':
                return SimpleNamespace(returncode=0, stdout='pid = 99999\n', stderr='')
            raise AssertionError('unexpected subprocess.run command: ' + repr(command))

        # Worktrees and tests live on the internal disk (owner, 2026-11-10); the
        # old ATLAS worktree path no longer exists on every build Mac.
        with tempfile.TemporaryDirectory(prefix='signing-test-') as tmp:
            work = Path(tmp)
            app = Path('/Volumes/Fixture/Library Studio KH.app')
            with patch.object(verify, 'run', side_effect=fake_run), \
                 patch.object(verify.subprocess, 'run', side_effect=fake_subprocess_run), \
                 patch.object(verify.socket, 'socket') as sock, \
                 patch.object(verify.select, 'kqueue') as kq, \
                 patch.object(verify.os, 'open', return_value=42), \
                 patch.object(verify.os, 'close'), \
                 patch.object(verify.uuid, 'uuid4', return_value=SimpleNamespace(hex='abcd1234')):
                sock.return_value.__enter__ = lambda s: s
                sock.return_value.__exit__ = lambda *a: None
                sock.return_value.bind = lambda a: None
                sock.return_value.getsockname = lambda: ('127.0.0.1', 55555)
                kq_inst = kq.return_value
                kq_inst.control = lambda *a, **k: None
                kq_inst.close = lambda: None
                # Force the readiness wait to fail so we exercise the finally cleanup path.
                with patch.object(verify.time, 'monotonic', side_effect=[0.0, 31.0]):
                    with self.assertRaisesRegex(RuntimeError, 'did not become ready'):
                        verify.launchd_start(app, '0.7.6', work)

            submit_call = next(c for c in calls if c[:2] == ['launchctl', 'submit'])
            self.assertIn('-l', submit_call)
            label_idx = submit_call.index('-l') + 1
            self.assertEqual(submit_call[label_idx], 'app.librarystudiokh.signing-test.abcd1234')
            # launchd must not open protected external-volume log files before exec.
            self.assertNotIn('-o', submit_call)
            self.assertNotIn('-e', submit_call)
            self.assertEqual((work / 'launchd-status.txt').read_text(), 'pid = 99999\n')
            env_marker = submit_call.index('/usr/bin/env')
            env_args = submit_call[env_marker:]
            self.assertTrue(any(arg.startswith('LIBRARY_STUDIO_HOME=') and str(work / 'config') in arg for arg in env_args))
            self.assertTrue(any(arg.startswith('LIBRARY_STUDIO_LAUNCHAGENT_DIR=') and str(work / 'test-agents') in arg for arg in env_args))
            self.assertIn(str(app / 'Contents/MacOS/library-studio-server'), submit_call)
            self.assertIn('-service', submit_call)
            self.assertIn('55555', submit_call)
            # No plist file should ever be written; submit replaces bootstrap entirely.
            self.assertFalse((work / 'host.plist').exists())
            # Cleanup must target the exact unique service submitted.
            self.assertEqual(len(bootout_called), 1)
            self.assertEqual(bootout_called[0], ['launchctl', 'bootout', 'gui/' + str(__import__('os').getuid()) + '/app.librarystudiokh.signing-test.abcd1234'])


if __name__ == '__main__':
    unittest.main()
