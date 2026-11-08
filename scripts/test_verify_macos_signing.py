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

    def test_differing_builds_keep_requirements(self):
        first = {'version': '0.7.4', 'requirements': {'app': 'anchor H"abc"'}, 'hashes': {'server': 'one'}}
        second = {**first, 'version': '0.7.5', 'hashes': {'server': 'two'}}
        verify.compare_builds(first, second)
        for invalid in [first, {**second, 'requirements': {'app': 'anchor H"other"'}}, {**second, 'hashes': first['hashes']}]:
            with self.subTest(invalid=invalid), self.assertRaises(ValueError):
                verify.compare_builds(first, invalid)


if __name__ == '__main__':
    unittest.main()
