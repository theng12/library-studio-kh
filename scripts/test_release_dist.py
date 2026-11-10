"""Library's post-acceptance command against the shared helper; synthetic DMGs only."""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
PACKAGE = json.loads((ROOT / 'package.json').read_text())
COMMAND = PACKAGE['scripts']['retain:accepted']
HELPER = Path.home() / 'Developer/Shared Tools/portfolio/tools/release-dist.py'
APP = 'Library Studio KH'


class ReleaseDistContract(unittest.TestCase):
    def setUp(self):
        self.assertTrue(HELPER.is_file(), 'Install the accepted shared portfolio helper first')
        self.temporary = tempfile.TemporaryDirectory(prefix='library-release-dist-contract-')
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.project = self.root / 'canonical project'
        self.project.mkdir()
        self.sources = self.root / 'verified sources'
        self.sources.mkdir()
        self.dist = self.project / 'dist'
        self.dist.mkdir()
        self.pins = self.root / 'accepted pins.json'

    def pin(self, version, data):
        source = self.sources / (hashlib.sha256(data).hexdigest()[:12] + '-feed-artifact.dmg')
        source.write_bytes(data)
        evidence = self.root / (version + '-installed.json')
        evidence.write_text(json.dumps({'version': version, 'installedVerified': True}))
        return {'version': version, 'source': str(source), 'sha256': hashlib.sha256(data).hexdigest(), 'bytes': len(data), 'evidence': str(evidence)}

    def run_retention(self, pins, ok=True):
        self.pins.write_text(json.dumps({'schema': 1, 'apps': [{'app': APP, 'project': str(self.project), 'releases': pins}]}))
        # Same shell command and quoted argument passing used by npm run.
        result = subprocess.run(['/bin/sh', '-c', COMMAND + ' "$@"', 'retain:accepted', str(self.pins)], cwd=ROOT, capture_output=True, text=True, timeout=10)
        if not ok:
            self.assertNotEqual(result.returncode, 0)
            return result
        self.assertEqual(result.returncode, 0, result.stderr)
        return json.loads(result.stdout)[APP]

    def ledger(self):
        return json.loads((self.dist / '.release-dist.json').read_text())['apps'][APP]['files']

    def test_exact_accepted_pair_adopts_and_copies_with_provenance(self):
        current = self.pin('0.7.5', b'accepted current fixture')
        previous = self.pin('0.7.4', b'accepted previous fixture')
        target = self.dist / (APP + '-0.7.5.dmg')
        target.write_bytes(Path(current['source']).read_bytes())
        pending = self.dist / (APP + '-0.7.6.dmg')
        pending.write_bytes(b'published candidate, not installed acceptance')
        result = self.run_retention([current, previous])
        self.assertEqual(result['adopted'], [target.name])
        self.assertEqual(result['copied'], [APP + '-0.7.4.dmg'])
        self.assertEqual(result['retired'], [])
        self.assertEqual(set(self.ledger()), {APP + '-0.7.5.dmg', APP + '-0.7.4.dmg'})
        for pin in [current, previous]:
            data = (self.dist / (APP + '-' + pin['version'] + '.dmg')).read_bytes()
            self.assertEqual(hashlib.sha256(data).hexdigest(), pin['sha256'])
            self.assertEqual(self.ledger()[APP + '-' + pin['version'] + '.dmg']['evidence'], pin['evidence'])
            self.assertTrue(Path(pin['source']).is_file())
        self.assertEqual(pending.read_bytes(), b'published candidate, not installed acceptance')
        ledger = (self.dist / '.release-dist.json').read_bytes()
        again = self.run_retention([current, previous])
        self.assertEqual(again['copied'] + again['adopted'] + again['retired'], [])
        self.assertEqual((self.dist / '.release-dist.json').read_bytes(), ledger)

    def test_mismatched_candidate_cannot_displace_working_pair(self):
        current = self.pin('0.7.5', b'current')
        previous = self.pin('0.7.4', b'previous')
        self.run_retention([current, previous])
        ledger = (self.dist / '.release-dist.json').read_bytes()
        candidate = self.pin('0.7.6', b'candidate')
        candidate['sha256'] = '0' * 64
        result = self.run_retention([candidate, current], ok=False)
        self.assertIn('refused', result.stderr)
        self.assertEqual((self.dist / '.release-dist.json').read_bytes(), ledger)
        self.assertTrue((self.dist / (APP + '-0.7.4.dmg')).is_file())
        self.assertFalse((self.dist / (APP + '-0.7.6.dmg')).exists())

    def test_retention_has_no_build_hook_and_future_accepted_pair_retires_only_managed(self):
        scripts = PACKAGE['scripts']
        self.assertEqual(COMMAND, 'python3 -I "$HOME/Developer/Shared Tools/portfolio/tools/release-dist.py"')
        for name, command in scripts.items():
            if name != 'retain:accepted':
                self.assertNotIn('release-dist', command)
                self.assertNotIn('retain:accepted', command)
        policy = json.loads((ROOT / 'test-policy/projects.json').read_text())
        self.assertIn(['python3', '-I', 'scripts/test_release_dist.py'], policy['projects']['library']['lanes']['host']['commands'])
        current = self.pin('0.7.5', b'current')
        previous = self.pin('0.7.4', b'previous')
        self.run_retention([current, previous])
        unmanaged = self.dist / 'legacy-installer.dmg'
        unmanaged.write_bytes(b'preserve unowned file')
        accepted = self.pin('0.7.6', b'now explicitly accepted fixture')
        result = self.run_retention([accepted, current])
        self.assertEqual(len(result['retired']), 1)
        self.assertFalse((self.dist / (APP + '-0.7.4.dmg')).exists())
        self.assertEqual(unmanaged.read_bytes(), b'preserve unowned file')
        retired = list((self.project / '_trash/dist').glob('*'))
        self.assertEqual(len(retired), 1)
        self.assertEqual(retired[0].read_bytes(), b'previous')
        self.assertEqual(set(self.ledger()), {APP + '-0.7.6.dmg', APP + '-0.7.5.dmg'})


if __name__ == '__main__':
    unittest.main()
