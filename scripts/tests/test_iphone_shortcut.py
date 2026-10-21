import importlib.util
import json
from pathlib import Path
import threading
import unittest
from urllib.error import HTTPError
from urllib.request import Request, urlopen

SCRIPTS = Path(__file__).resolve().parents[1]


def load(name):
    spec = importlib.util.spec_from_file_location(name, SCRIPTS / (name + '.py'))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


builder = load('build_iphone_shortcut')
fixture = load('shortcut_capture_native_fixture')


class NativeIsolationTests(unittest.TestCase):
    def test_isolated_copy_never_uses_real_connection_or_server(self):
        original = builder.workflow()
        result = builder.native_smoke_workflow(original, 'http://127.0.0.1:8877', 'rss-pal-native-smoke-test-connection', 'rss-pal-native-smoke-test-draft')
        text = json.dumps(result)
        self.assertNotIn('rss-pal-connection.txt', text)
        self.assertNotIn('https://rss.morefreeze.top', text)
        self.assertIn('rss-pal-native-smoke-test-connection.txt', text)
        self.assertIn('rss-pal-connection.txt', json.dumps(original))

    def test_only_localhost_and_test_names_are_allowed(self):
        for origin in ['https://rss.morefreeze.top', 'http://example.com', 'http://127.0.0.1/path', 'http://user@localhost']:
            with self.assertRaises(ValueError):
                builder.native_smoke_workflow({}, origin, 'rss-pal-native-smoke-test', 'rss-pal-native-smoke-test')
        for name in ['rss-pal-connection.txt', '../rss-pal-native-smoke-test', 'rss-pal-native-smoke-test/file']:
            with self.assertRaises(ValueError):
                builder.native_smoke_workflow({}, 'http://localhost:8877', name, name)

    def test_fixture_requires_note_and_preserves_retry_contract(self):
        server = fixture.Fixture(('127.0.0.1', 0), 'retry')
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            def send(note):
                request = Request('http://127.0.0.1:' + str(server.server_port) + '/api/shortcuts/capture', data=json.dumps({'url': fixture.TEST_URL, 'capture_note': note}).encode(), headers={'Authorization': 'Bearer ' + fixture.FAKE_TOKEN, 'Content-Type': 'application/json'})
                return json.load(urlopen(request))
            with self.assertRaises(HTTPError) as failure:
                send('想法第一行\n第二行')
            self.assertEqual(503, failure.exception.code)
            failure.exception.close()
            self.assertTrue(send('想法第一行\n第二行')['note_saved'])
            self.assertTrue(send('想法第一行\n第二行')['note_saved'])
            self.assertEqual('想法第一行\n第二行', server.saved_note)
            self.assertFalse(send('')['note_saved'])
            self.assertEqual('想法第一行\n第二行', server.saved_note)
            server.scenario = 'ack-false'
            self.assertFalse(send('尚未确认')['note_saved'])
        finally:
            server.shutdown()
            server.server_close()
            thread.join()


if __name__ == '__main__':
    unittest.main()
