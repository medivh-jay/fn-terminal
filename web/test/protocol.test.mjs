import { test } from 'node:test';
import assert from 'node:assert/strict';
import { chunkInput, parseServerMessage } from '../src/protocol.ts';

test('large unicode paste survives transport chunk boundaries', () => {
  const text = '中文🙂'.repeat(10000);
  const chunks = chunkInput(text);
  assert.ok(chunks.length > 1);
  assert.ok(chunks.every((chunk) => chunk.byteLength <= 32768));
  assert.equal(new TextDecoder().decode(Buffer.concat(chunks)), text);
});
test('invalid server messages cannot mark a terminal as connected', () => {
  for (const value of [
    '{}',
    '{',
    '{"type":"ready","uid":0,"username":"root"}',
    '{"type":"ready","uid":1000}',
    '{"type":"unknown"}',
  ]) {
    assert.equal(parseServerMessage(value), null);
  }
  assert.deepEqual(parseServerMessage('{"type":"ready","uid":1000,"username":"alice"}'), {
    type: 'ready',
    uid: 1000,
    username: 'alice',
  });
});

test('only an explicit shell exit can trigger pane removal', () => {
  assert.equal(
    parseServerMessage('{"type":"exit","message":"done","reason":"shell_exit"}').reason,
    'shell_exit',
  );
  for (const reason of [undefined, 'session_end', 'unknown', null, true]) {
    assert.equal(
      parseServerMessage(JSON.stringify({ type: 'exit', message: 'done', reason })).reason,
      'session_end',
    );
  }
});
